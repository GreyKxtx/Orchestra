package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
)

// batchRelayMax caps the WorkOrders one Lead result can fan out — the same
// cap task_spawn puts on workorders[].
const batchRelayMax = 8

// relayBatchWorkOrders implements spec §3.7 / §5.6: a Dept Lead "returns
// batch_workorders[] in task_result; the runtime checks disjoint target_files
// and spawns the workers in parallel". Until now the array reached the
// Orchestrator as text, and the Orchestrator had to re-emit every WorkOrder
// as its own task_spawn call — paying for each one in context and tokens,
// and free to drop or rewrite them on the way.
//
// The workers are spawned as the Lead (its flows must reach worker, its
// department is theirs) but belong to the Lead's parent, which collects them
// with task_wait{task_ids}. They outlive the Lead's own run — it has finished
// by the time its result is relayed — and are stopped by the parent's wait,
// a cancel, or the end of the turn (TaskRunner.Close).
//
// The Lead's result comes back with batch_workorders[] replaced by a short
// summary and a `relayed` block naming the task IDs and any WorkOrder the
// runtime refused (phase guard, contract epoch, brief gate, a dependency
// cycle), with the reason.
func (r *TaskRunner) relayBatchWorkOrders(ctx context.Context, lead agentScope, target spawnTarget, result string) string {
	if !r.child.Agency.Enabled || !r.child.Agency.RelayWorkOrders || strings.EqualFold(target.role, "worker") {
		return result
	}
	obj, raws := parseBatchWorkOrders(result)
	if len(raws) == 0 {
		return result
	}
	var rejected []string
	if len(raws) > batchRelayMax {
		for i := batchRelayMax; i < len(raws); i++ {
			rejected = append(rejected, fmt.Sprintf("workorders[%d]: over the %d-per-result cap — return it in a later batch", i, batchRelayMax))
		}
		raws = raws[:batchRelayMax]
	}
	order, cycle := orderWorkOrders(raws)
	for _, i := range cycle {
		rejected = append(rejected, fmt.Sprintf("workorders[%d]: depends_on forms a cycle within the batch", i))
	}

	owner := lead
	if len(lead.chain) >= 2 {
		owner = agentScope{address: lead.chain[len(lead.chain)-2], taskID: lead.parentTaskID}
	}
	// The Lead's context ends when this function returns; the workers must
	// not end with it.
	spawnCtx := context.WithoutCancel(ctx)
	var ids []string
	summary := make([]map[string]any, len(raws))
	for i, raw := range raws {
		summary[i] = workOrderSummary(raw, "")
	}
	for _, i := range cycle {
		summary[i]["rejected"] = "depends_on cycle"
	}
	for _, i := range order {
		raw := raws[i]
		id, err := r.spawnFrom(spawnCtx, lead, agent.SubtaskSpawnRequest{
			Goal:         string(raw),
			SubagentType: "worker",
			TimeoutMS:    DefaultTaskTimeoutMS,
		}, spawnExtra{verb: "relay WorkOrders to", owner: &owner})
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("workorders[%d]: %v", i, err))
			summary[i]["rejected"] = clip(err.Error(), 200)
			continue
		}
		ids = append(ids, id)
		summary[i]["task_id"] = id
	}
	if r.child.NotifyAgentEvent != nil {
		r.child.NotifyAgentEvent(map[string]any{
			"type":     "workorders_relayed",
			"agent":    lead.address,
			"task_ids": ids,
			"rejected": len(rejected),
		})
	}
	relayed := map[string]any{"task_ids": ids}
	if len(rejected) > 0 {
		relayed["rejected"] = rejected
	}
	if len(ids) > 0 {
		relayed["next"] = "the runtime spawned these workers; task_wait{task_ids} collects their results and builds their edits together"
	}
	obj["batch_workorders"] = summary
	obj["relayed"] = relayed
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	// "no flow product > worker" must reach the model as written, not as
	// \u003e: it is an instruction, not HTML.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return result
	}
	return strings.TrimSpace(buf.String())
}

// parseBatchWorkOrders reads a Lead result that is a JSON object (optionally
// inside a ```json fence) with a batch_workorders array.
func parseBatchWorkOrders(result string) (map[string]any, []json.RawMessage) {
	s := strings.TrimSpace(result)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") {
		return nil, nil
	}
	var probe struct {
		Batch []json.RawMessage `json:"batch_workorders"`
	}
	if err := json.Unmarshal([]byte(s), &probe); err != nil || len(probe.Batch) == 0 {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return nil, nil
	}
	var out []json.RawMessage
	for _, raw := range probe.Batch {
		t := strings.TrimSpace(string(raw))
		if strings.HasPrefix(t, "{") {
			out = append(out, raw)
		}
	}
	return obj, out
}

// orderWorkOrders sorts a batch so every WorkOrder comes after the batch
// members it depends_on (by task_id); dependencies outside the batch are
// left to the spawn path. Members of a cycle are returned separately.
func orderWorkOrders(raws []json.RawMessage) (order, cycle []int) {
	type node struct {
		key  string
		deps []string
	}
	nodes := make([]node, len(raws))
	index := map[string]int{}
	for i, raw := range raws {
		var wo struct {
			TaskID    string   `json:"task_id"`
			DependsOn []string `json:"depends_on"`
		}
		_ = json.Unmarshal(raw, &wo)
		nodes[i] = node{key: strings.TrimSpace(wo.TaskID), deps: wo.DependsOn}
		if nodes[i].key != "" {
			index[nodes[i].key] = i
		}
	}
	state := make([]int, len(raws)) // 0 new, 1 visiting, 2 done, 3 cyclic
	var visit func(i int) bool
	visit = func(i int) bool {
		switch state[i] {
		case 1, 3:
			return false
		case 2:
			return true
		}
		state[i] = 1
		for _, d := range nodes[i].deps {
			j, ok := index[strings.TrimSpace(d)]
			if !ok || j == i {
				continue
			}
			if !visit(j) {
				state[i] = 3
				return false
			}
		}
		state[i] = 2
		order = append(order, i)
		return true
	}
	for i := range raws {
		if state[i] == 0 {
			visit(i)
		}
	}
	for i, st := range state {
		if st != 2 {
			cycle = append(cycle, i)
		}
	}
	return order, cycle
}

// workOrderSummary is the one-line view of a relayed WorkOrder kept in the
// Lead's result.
func workOrderSummary(raw json.RawMessage, taskID string) map[string]any {
	var wo WorkOrder
	_ = json.Unmarshal(raw, &wo)
	out := map[string]any{}
	if taskID != "" {
		out["task_id"] = taskID
	}
	if wo.TaskID != "" {
		out["key"] = wo.TaskID
	}
	if wo.Intent != "" {
		out["intent"] = clip(wo.Intent, 160)
	}
	if files := EditScopePaths(&wo); len(files) > 0 {
		out["target_files"] = files
	}
	if len(wo.DependsOn) > 0 {
		out["depends_on"] = wo.DependsOn
	}
	return out
}
