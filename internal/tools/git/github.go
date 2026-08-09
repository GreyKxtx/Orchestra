package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const ghOutputLimit = 512 * 1024 // 512 KB

var (
	ghOnce  sync.Once
	ghFound bool
	ghBin   string
)

// GHAvailable reports whether the gh CLI is available in PATH.
// The result is cached after the first call.
func GHAvailable() bool {
	ghOnce.Do(func() {
		p, err := exec.LookPath("gh")
		if err == nil {
			ghBin = p
			ghFound = true
		}
	})
	return ghFound
}

// runGH runs a gh CLI command in the workspace root and returns stdout bytes.
// stderr is captured and included in errors. Timeout defaults to 30 s.
func runGH(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	if !GHAvailable() {
		return nil, fmt.Errorf("gh CLI not available in PATH")
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(tctx, ghBin, args...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 512 {
			msg = msg[:512]
		}
		subcmd := ""
		if len(args) > 0 {
			subcmd = args[0]
		}
		return nil, fmt.Errorf("gh %s: %s", subcmd, msg)
	}

	out := stdout.Bytes()
	if len(out) > ghOutputLimit {
		out = out[:ghOutputLimit]
	}
	return out, nil
}

// ─── gh.pr.list ──────────────────────────────────────────────────────────────

// GHPRListRequest is the input for the gh.pr.list tool.
type GHPRListRequest struct {
	State string `json:"state,omitempty"` // "open" (default), "closed", "merged", "all"
	Limit int    `json:"limit,omitempty"` // default 20
	Base  string `json:"base,omitempty"`  // filter by base branch
}

// GHPRListItem is one entry in the PR list.
type GHPRListItem struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	Base      string `json:"base"`
	Head      string `json:"head"`
	UpdatedAt string `json:"updated_at"`
}

// GHPRListResponse is the output of the gh.pr.list tool.
type GHPRListResponse struct {
	PRs []GHPRListItem `json:"prs"`
}

// GHPRList lists pull requests in the current repo.
func (c *Client) GHPRList(ctx context.Context, req GHPRListRequest) (*GHPRListResponse, error) {
	args := []string{
		"pr", "list",
		"--json", "number,title,state,author,url,baseRefName,headRefName,updatedAt",
	}
	state := req.State
	if state == "" {
		state = "open"
	}
	args = append(args, "--state", state)

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	if req.Base != "" {
		args = append(args, "--base", req.Base)
	}

	out, err := runGH(ctx, c.root, args...)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		URL       string `json:"url"`
		Base      string `json:"baseRefName"`
		Head      string `json:"headRefName"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: parse JSON: %w", err)
	}

	items := make([]GHPRListItem, len(raw))
	for i, p := range raw {
		items[i] = GHPRListItem{
			Number:    p.Number,
			Title:     p.Title,
			State:     p.State,
			Author:    p.Author.Login,
			URL:       p.URL,
			Base:      p.Base,
			Head:      p.Head,
			UpdatedAt: p.UpdatedAt,
		}
	}
	return &GHPRListResponse{PRs: items}, nil
}

// ─── gh.pr.create ────────────────────────────────────────────────────────────

// GHPRCreateRequest is the input for the gh.pr.create tool.
type GHPRCreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Base  string `json:"base,omitempty"` // target branch; defaults to repo default
	Draft bool   `json:"draft,omitempty"`
}

// GHPRCreateResponse is the output of the gh.pr.create tool.
type GHPRCreateResponse struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// GHPRCreate creates a pull request from the current branch.
func (c *Client) GHPRCreate(ctx context.Context, req GHPRCreateRequest) (*GHPRCreateResponse, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("gh pr create: title is required")
	}

	args := []string{
		"pr", "create",
		"--title", req.Title,
		"--json", "number,url,title",
	}
	args = append(args, "--body", req.Body)
	if req.Base != "" {
		args = append(args, "--base", req.Base)
	}
	if req.Draft {
		args = append(args, "--draft")
	}

	out, err := runGH(ctx, c.root, args...)
	if err != nil {
		return nil, err
	}

	var resp GHPRCreateResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("gh pr create: parse JSON: %w", err)
	}
	return &resp, nil
}

// ─── gh.pr.view ──────────────────────────────────────────────────────────────

// GHPRViewRequest is the input for the gh.pr.view tool.
type GHPRViewRequest struct {
	Number int `json:"number,omitempty"` // 0 = current branch's PR
}

// GHPRComment is a single comment on a PR.
type GHPRComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// GHPRViewResponse is the output of the gh.pr.view tool.
type GHPRViewResponse struct {
	Number   int           `json:"number"`
	Title    string        `json:"title"`
	Body     string        `json:"body"`
	State    string        `json:"state"`
	URL      string        `json:"url"`
	Author   string        `json:"author"`
	Base     string        `json:"base"`
	Head     string        `json:"head"`
	Comments []GHPRComment `json:"comments"`
}

// GHPRView returns details of a pull request. Number=0 uses the current branch's PR.
func (c *Client) GHPRView(ctx context.Context, req GHPRViewRequest) (*GHPRViewResponse, error) {
	args := []string{"pr", "view"}
	if req.Number > 0 {
		args = append(args, fmt.Sprintf("%d", req.Number))
	}
	args = append(args, "--json", "number,title,body,state,url,author,baseRefName,headRefName,comments")

	out, err := runGH(ctx, c.root, args...)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Base     string `json:"baseRefName"`
		Head     string `json:"headRefName"`
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body string `json:"body"`
			URL  string `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh pr view: parse JSON: %w", err)
	}

	comments := make([]GHPRComment, len(raw.Comments))
	for i, c := range raw.Comments {
		comments[i] = GHPRComment{Author: c.Author.Login, Body: c.Body, URL: c.URL}
	}
	return &GHPRViewResponse{
		Number:   raw.Number,
		Title:    raw.Title,
		Body:     raw.Body,
		State:    raw.State,
		URL:      raw.URL,
		Author:   raw.Author.Login,
		Base:     raw.Base,
		Head:     raw.Head,
		Comments: comments,
	}, nil
}

// ─── gh.issue.list ───────────────────────────────────────────────────────────

// GHIssueListRequest is the input for the gh.issue.list tool.
type GHIssueListRequest struct {
	State  string   `json:"state,omitempty"`  // "open" (default), "closed", "all"
	Labels []string `json:"labels,omitempty"` // filter by label names
	Limit  int      `json:"limit,omitempty"`  // default 20
}

// GHIssueListItem is one entry in the issue list.
type GHIssueListItem struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	URL       string   `json:"url"`
	Labels    []string `json:"labels"`
	UpdatedAt string   `json:"updated_at"`
}

// GHIssueListResponse is the output of the gh.issue.list tool.
type GHIssueListResponse struct {
	Issues []GHIssueListItem `json:"issues"`
}

// GHIssueList lists issues in the current repo.
func (c *Client) GHIssueList(ctx context.Context, req GHIssueListRequest) (*GHIssueListResponse, error) {
	args := []string{
		"issue", "list",
		"--json", "number,title,state,author,url,labels,updatedAt",
	}
	state := req.State
	if state == "" {
		state = "open"
	}
	args = append(args, "--state", state)

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	for _, label := range req.Labels {
		args = append(args, "--label", label)
	}

	out, err := runGH(ctx, c.root, args...)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		URL    string `json:"url"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue list: parse JSON: %w", err)
	}

	items := make([]GHIssueListItem, len(raw))
	for i, issue := range raw {
		labels := make([]string, len(issue.Labels))
		for j, l := range issue.Labels {
			labels[j] = l.Name
		}
		items[i] = GHIssueListItem{
			Number:    issue.Number,
			Title:     issue.Title,
			State:     issue.State,
			Author:    issue.Author.Login,
			URL:       issue.URL,
			Labels:    labels,
			UpdatedAt: issue.UpdatedAt,
		}
	}
	return &GHIssueListResponse{Issues: items}, nil
}

// ─── gh.issue.view ───────────────────────────────────────────────────────────

// GHIssueViewRequest is the input for the gh.issue.view tool.
type GHIssueViewRequest struct {
	Number int `json:"number"` // required
}

// GHIssueComment is a single comment on an issue.
type GHIssueComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// GHIssueViewResponse is the output of the gh.issue.view tool.
type GHIssueViewResponse struct {
	Number   int              `json:"number"`
	Title    string           `json:"title"`
	Body     string           `json:"body"`
	State    string           `json:"state"`
	URL      string           `json:"url"`
	Author   string           `json:"author"`
	Labels   []string         `json:"labels"`
	Comments []GHIssueComment `json:"comments"`
}

// GHIssueView returns details of a specific issue.
func (c *Client) GHIssueView(ctx context.Context, req GHIssueViewRequest) (*GHIssueViewResponse, error) {
	if req.Number <= 0 {
		return nil, fmt.Errorf("gh issue view: number is required and must be > 0")
	}

	args := []string{
		"issue", "view", fmt.Sprintf("%d", req.Number),
		"--json", "number,title,body,state,url,author,labels,comments",
	}

	out, err := runGH(ctx, c.root, args...)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body string `json:"body"`
			URL  string `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh issue view: parse JSON: %w", err)
	}

	labels := make([]string, len(raw.Labels))
	for i, l := range raw.Labels {
		labels[i] = l.Name
	}
	comments := make([]GHIssueComment, len(raw.Comments))
	for i, c := range raw.Comments {
		comments[i] = GHIssueComment{Author: c.Author.Login, Body: c.Body, URL: c.URL}
	}
	return &GHIssueViewResponse{
		Number:   raw.Number,
		Title:    raw.Title,
		Body:     raw.Body,
		State:    raw.State,
		URL:      raw.URL,
		Author:   raw.Author.Login,
		Labels:   labels,
		Comments: comments,
	}, nil
}

