package tasks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
)

// The agency's bus (audit 4.5): every note an agent leaves another through
// agent_post is a typed post with its provenance — who wrote it, from which
// task at what depth, having read what — routed by its kind. A kind says
// what a post of it needs and who hears of it besides its recipient: a
// contract change request reaches the Orchestrator whoever it was sent to;
// a question to the root is a question for the user, which the runtime puts
// through the Question Barrier and answers at once, on the record in
// decisions.md. The recipient reads the note escaped, under its provenance
// (agent.FitAgentMessages), never as the user's words.

// postKind is one kind of note.
type postKind string

const (
	postNote           postKind = "note"
	postQuestion       postKind = "question"
	postContractChange postKind = "contract_change_request"
	postFinding        postKind = "finding"
	postHandoff        postKind = "handoff"
)

// kindSpec is what the bus knows about a kind.
type kindSpec struct {
	// needsArtifact: the post names the artifact it is about.
	needsArtifact bool
	// toHub copies the post to the Orchestrator whoever it was addressed to.
	toHub bool
	// viaBarrier: addressed to the root, the post is a question for the
	// user — asked through the barrier, answered in the receipt.
	viaBarrier bool
}

// postKindSpecs is the one table of kinds; postKindNames lists them for the
// model, in this order.
var postKindSpecs = map[postKind]kindSpec{
	postNote:           {},
	postQuestion:       {viaBarrier: true},
	postContractChange: {needsArtifact: true, toHub: true},
	postFinding:        {},
	postHandoff:        {},
}

var postKindOrder = []postKind{postNote, postQuestion, postContractChange, postFinding, postHandoff}

func postKindNames() string {
	names := make([]string, 0, len(postKindOrder))
	for _, k := range postKindOrder {
		names = append(names, string(k))
	}
	return strings.Join(names, "|")
}

// postMessageMaxBytes caps one note; longer material belongs in a file the
// note points at.
const postMessageMaxBytes = 4000

// post is one note on the bus, with its provenance.
type post struct {
	kind     postKind
	from     agentScope
	to       string
	message  string
	artifact string
	// tainted names the untrusted source the sender had read, if any.
	tainted string
	at      time.Time
}

// inboxMessage is the post as its recipient reads it.
func (p post) inboxMessage() agent.InboxMessage {
	return agent.InboxMessage{
		From:       p.from.address,
		FromTaskID: p.from.taskID,
		Depth:      p.from.depth,
		To:         p.to,
		Kind:       string(p.kind),
		Message:    p.message,
		Artifact:   p.artifact,
		At:         p.at.UTC().Format(time.RFC3339),
		Tainted:    p.tainted,
	}
}

// artifactBus routes posts: to their recipient — live when it runs, its
// inbox otherwise — and to whoever subscribed to the kind.
type artifactBus struct {
	r *TaskRunner

	mu          sync.Mutex
	subscribers map[postKind][]func(post)
}

// newArtifactBus is the runner's bus with the built-in subscriptions: the
// Orchestrator hears every contract change request.
func newArtifactBus(r *TaskRunner) *artifactBus {
	b := &artifactBus{r: r, subscribers: map[postKind][]func(post){}}
	b.subscribe(postContractChange, func(p post) {
		if p.to != config.AgencyRootName && p.from.depth > 0 {
			r.leaveForRoot(p.inboxMessage())
		}
	})
	return b
}

// subscribe runs fn after every post of kind has reached its recipient.
func (b *artifactBus) subscribe(kind postKind, fn func(post)) {
	b.mu.Lock()
	b.subscribers[kind] = append(b.subscribers[kind], fn)
	b.mu.Unlock()
}

func (b *artifactBus) subscribersOf(kind postKind) []func(post) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]func(post), len(b.subscribers[kind]))
	copy(out, b.subscribers[kind])
	return out
}

// parsePost checks an agent_post request and makes the post: the address,
// the kind and what it needs, the message and its size.
func (b *artifactBus) parsePost(from agentScope, req agent.AgentPostRequest) (post, error) {
	to := strings.ToLower(strings.TrimSpace(req.To))
	switch to {
	case "lead", "root", "parent":
		to = config.AgencyRootName
	}
	if to == "" || (!config.ValidAgencyName(to) && !taskIDRe.MatchString(to)) {
		return post{}, fmt.Errorf("agent_post: %q is not an agent address or task_id", req.To)
	}
	// Posting to your own address is how a worker reaches its siblings in
	// the department (delivery skips the sender); only the root and a task
	// addressing itself by ID are talking to themselves.
	if (from.depth == 0 && to == config.AgencyRootName) || (from.taskID != "" && to == from.taskID) {
		return post{}, fmt.Errorf("agent_post: you are %s", to)
	}
	kind := postKind(strings.ToLower(strings.TrimSpace(req.Kind)))
	if kind == "" {
		kind = postNote
	}
	spec, ok := postKindSpecs[kind]
	if !ok {
		return post{}, fmt.Errorf("agent_post: kind %q (want %s)", req.Kind, postKindNames())
	}
	text := strings.TrimSpace(req.Message)
	if text == "" {
		return post{}, fmt.Errorf("agent_post: message is empty")
	}
	if len(text) > postMessageMaxBytes {
		return post{}, fmt.Errorf("agent_post: message is %d bytes (max %d) — write the detail to a file and point at it", len(text), postMessageMaxBytes)
	}
	artifact := strings.TrimSpace(req.Artifact)
	if spec.needsArtifact && artifact == "" {
		return post{}, fmt.Errorf("agent_post: %s needs artifact (the contract file the delta applies to)", kind)
	}
	return post{kind: kind, from: from, to: to, message: text, artifact: artifact, tainted: req.Tainted, at: time.Now()}, nil
}

// publish routes p. A question to the root goes to the user through the
// barrier when there is one to ask; everything else reaches its recipient
// live or in its inbox, then the kind's subscribers.
func (b *artifactBus) publish(ctx context.Context, p post) (*agent.AgentPostReceipt, error) {
	r := b.r
	if err := r.takeMessage(); err != nil {
		return nil, err
	}
	spec := postKindSpecs[p.kind]
	if spec.viaBarrier && p.to == config.AgencyRootName {
		if receipt, ok := b.askThroughBarrier(ctx, p); ok {
			r.emitAgentMessage("post", p.from.address, "user", string(p.kind), p.message, "")
			return receipt, nil
		}
	}
	m := p.inboxMessage()
	receipt := &agent.AgentPostReceipt{To: p.to, Delivered: "live"}
	switch {
	case p.to == config.AgencyRootName:
		r.leaveForRoot(m)
		if spec.viaBarrier {
			receipt.Note = "no interactive channel in this run: the Orchestrator has your question; proceed on an assumption you state until it answers"
		}
	case r.deliverLive(p.to, p.from.taskID, m) > 0:
	default:
		if taskIDRe.MatchString(p.to) {
			return nil, fmt.Errorf("agent_post: task %s is not running", p.to)
		}
		if err := r.appendInbox(p.to, m); err != nil {
			return nil, fmt.Errorf("agent_post: %w", err)
		}
		receipt.Delivered = "inbox"
		receipt.Note = p.to + " is not running; it reads the note when it is next started"
	}
	for _, fn := range b.subscribersOf(p.kind) {
		fn(p)
	}
	r.emitAgentMessage("post", p.from.address, p.to, string(p.kind), p.message, "")
	return receipt, nil
}

// askThroughBarrier puts a question to the user under the barrier's rules
// — one round at a time, a question already answered this turn answered
// from that answer, the clarification budget — records it in decisions.md
// and answers the sender at once. Not ok when nobody can be asked: the
// question then goes to the Orchestrator like any note.
func (b *artifactBus) askThroughBarrier(ctx context.Context, p post) (*agent.AgentPostReceipt, bool) {
	r := b.r
	if r.child.QuestionAsker == nil || r.child.RelayViaLLM {
		return nil, false
	}
	root := r.toolRunner.WorkspaceRoot()
	if _, found, err := orchestrastate.Load(root); err != nil || !found {
		return nil, false
	}
	dept := p.from.dept
	if dept == "" {
		dept = p.from.address
	}
	q := OpenQuestion{Dept: dept, Text: p.message}
	answers, asked, exhausted, err := r.askUser(ctx, root, []OpenQuestion{q})
	if err != nil {
		return nil, false
	}
	if exhausted {
		_ = decisions.Append(root, []decisions.Entry{{
			Kind: "assumption", Dept: dept, Question: p.message,
			Answer: "clarification budget exhausted — proceed on a documented assumption",
		}})
		return &agent.AgentPostReceipt{
			To:        "user",
			Delivered: "unanswered",
			Note:      "max_clarification_rounds reached: the user is not asked again. Choose the safest assumption, record it in assumptions[], and proceed (" + decisions.FileRel + ").",
		}, true
	}
	answer := answers[0]
	if len(asked) > 0 {
		_ = decisions.Append(root, []decisions.Entry{{Kind: "qa", Dept: dept, Question: p.message, Answer: answer}})
	}
	// The hub hears what was asked and answered on its behalf.
	r.leaveForRoot(agent.InboxMessage{
		From:       p.from.address,
		FromTaskID: p.from.taskID,
		Depth:      p.from.depth,
		To:         config.AgencyRootName,
		Kind:       string(postNote),
		Origin:     agent.InboxOriginRuntime,
		Message:    fmt.Sprintf("%s asked the user: %s — answer: %s (recorded in %s)", p.from.address, p.message, answer, decisions.FileRel),
		At:         time.Now().UTC().Format(time.RFC3339),
	})
	return &agent.AgentPostReceipt{To: "user", Delivered: "answered", Answer: answer, Note: "recorded in " + decisions.FileRel}, true
}
