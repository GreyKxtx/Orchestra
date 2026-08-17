package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TraversalDirection selects which incident edges BFS/DFS follow.
type TraversalDirection int

const (
	DirectionDownstream TraversalDirection = iota // Callees: whom the node calls
	DirectionUpstream                             // Callers: who calls the node
	DirectionBoth                                 // Neighborhood
)

// TraversalOptions caps expansion. Zero values: MaxDepth=2, MaxNodes=50,
// IncludeTypes=true (calls + instantiates). IncludeImports stays off unless set.
type TraversalOptions struct {
	MaxDepth       int      // default 2
	MaxNodes       int      // hard cap, default 50
	IncludeImports bool     // follow relation=imports
	IncludeTypes   bool     // follow relation=instantiates (default true)
	StopAtFQN      []string // include but do not expand
}

type GraphPath struct {
	Nodes []*Node
	Edges []*Edge
}

// Subgraph is a bounded, cycle-safe neighborhood around Root.
// Nodes is keyed by FQN; Depth is the BFS distance of the first visit
// (so a diamond A→B→D, A→C→D records D at depth 2, once).
type Subgraph struct {
	Root  *Node
	Nodes map[string]*Node // FQN → Node
	Edges []*Edge
	Depth map[string]int // FQN → depth from root
}

func (o TraversalOptions) normalized() TraversalOptions {
	if o.MaxDepth <= 0 {
		o.MaxDepth = 2
	}
	if o.MaxDepth > 8 {
		o.MaxDepth = 8
	}
	if o.MaxNodes <= 0 {
		o.MaxNodes = 50
	}
	if !o.IncludeTypes && !o.IncludeImports {
		o.IncludeTypes = true
	}
	return o
}

func (o TraversalOptions) relations() []string {
	rels := []string{"calls"}
	if o.IncludeTypes {
		rels = append(rels, "instantiates")
	}
	if o.IncludeImports {
		rels = append(rels, "imports")
	}
	return rels
}

func (o TraversalOptions) stopSet() map[string]bool {
	if len(o.StopAtFQN) == 0 {
		return nil
	}
	m := make(map[string]bool, len(o.StopAtFQN))
	for _, f := range o.StopAtFQN {
		if f = strings.TrimSpace(f); f != "" {
			m[f] = true
		}
	}
	return m
}

// TraverseBFS walks the CKG from startFQN with cycle protection via visited.
// First visit is kept (minimum BFS depth) — diamond dependencies collapse to one node.
func (s *Store) TraverseBFS(ctx context.Context, startFQN string, dir TraversalDirection, opts TraversalOptions) (*Subgraph, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("ckg store is nil")
	}
	startFQN = strings.TrimSpace(startFQN)
	if startFQN == "" {
		return nil, fmt.Errorf("start FQN is empty")
	}
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("ckg store is closed")
	}
	return s.traverseBFSUnlocked(ctx, startFQN, dir, opts)
}

func (s *Store) traverseBFSUnlocked(ctx context.Context, startFQN string, dir TraversalDirection, opts TraversalOptions) (*Subgraph, error) {
	opts = opts.normalized()
	root, err := s.getNodeByFQN(ctx, startFQN)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("node %q not found", startFQN)
	}

	sg := &Subgraph{
		Root:  root,
		Nodes: map[string]*Node{root.FQN: root},
		Depth: map[string]int{root.FQN: 0},
	}
	stop := opts.stopSet()
	rels := opts.relations()
	visited := map[string]bool{root.FQN: true}
	type item struct {
		fqn   string
		depth int
	}
	queue := []item{{fqn: root.FQN, depth: 0}}
	truncated := false

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= opts.MaxDepth {
			continue
		}
		if stop[cur.fqn] && cur.depth > 0 {
			continue
		}

		var neigh []neighbor
		if dir == DirectionDownstream || dir == DirectionBoth {
			n, err := s.neighbors(ctx, cur.fqn, true, rels)
			if err != nil {
				return nil, err
			}
			neigh = append(neigh, n...)
		}
		if dir == DirectionUpstream || dir == DirectionBoth {
			n, err := s.neighbors(ctx, cur.fqn, false, rels)
			if err != nil {
				return nil, err
			}
			neigh = append(neigh, n...)
		}

		for _, nb := range neigh {
			if visited[nb.node.FQN] {
				// Diamond: keep first (min) depth; still record the edge once.
				sg.addEdge(nb.edge)
				continue
			}
			if len(sg.Nodes) >= opts.MaxNodes {
				truncated = true
				sg.addEdge(nb.edge)
				continue
			}
			visited[nb.node.FQN] = true
			cp := nb.node
			sg.Nodes[cp.FQN] = cp
			sg.Depth[cp.FQN] = cur.depth + 1
			sg.addEdge(nb.edge)
			// External / stdlib leaves are recorded but never expanded.
			if cp.Kind == "external" || nb.edge.IsExternal {
				continue
			}
			if !truncated && (stop == nil || !stop[cp.FQN]) {
				queue = append(queue, item{fqn: cp.FQN, depth: cur.depth + 1})
			}
		}
		if truncated {
			break
		}
	}
	return sg, nil
}

func (sg *Subgraph) addEdge(e Edge) {
	for _, existing := range sg.Edges {
		if existing.SourceFQN == e.SourceFQN && existing.TargetFQN == e.TargetFQN && existing.Relation == e.Relation {
			return
		}
	}
	cp := e
	sg.Edges = append(sg.Edges, &cp)
}

type neighbor struct {
	node *Node
	edge Edge
}

func (s *Store) neighbors(ctx context.Context, fqn string, downstream bool, rels []string) ([]neighbor, error) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(rels)), ",")
	args := make([]any, 0, 2+len(rels))
	var q string
	if downstream {
		args = append(args, fqn)
		for _, r := range rels {
			args = append(args, r)
		}
		q = fmt.Sprintf(`
			SELECT COALESCE(n.id, 0), COALESCE(n.file_id, 0), e.target_fqn,
			       COALESCE(n.short_name, e.target_fqn), COALESCE(n.kind, CASE WHEN e.is_external = 1 THEN 'external' ELSE 'symbol' END),
			       COALESCE(n.line_start, 0), COALESCE(n.line_end, 0), COALESCE(n.complexity, 0),
			       COALESCE(n.package, ''), COALESCE(f.path, ''),
			       src.fqn, e.target_fqn, e.relation, e.is_external
			FROM edges e
			JOIN nodes src ON src.id = e.source_id
			LEFT JOIN nodes n ON n.id = e.target_id OR n.fqn = e.target_fqn
			LEFT JOIN files f ON f.id = n.file_id
			WHERE src.fqn = ? AND e.relation IN (%s)`, placeholders)
	} else {
		args = append(args, fqn, fqn)
		for _, r := range rels {
			args = append(args, r)
		}
		q = fmt.Sprintf(`
			SELECT src.id, src.file_id, src.fqn, src.short_name, src.kind,
			       src.line_start, src.line_end, src.complexity, src.package, COALESCE(f.path, ''),
			       src.fqn, e.target_fqn, e.relation, e.is_external
			FROM edges e
			JOIN nodes src ON src.id = e.source_id
			LEFT JOIN nodes tgt ON tgt.id = e.target_id
			LEFT JOIN files f ON f.id = src.file_id
			WHERE (tgt.fqn = ? OR e.target_fqn = ?) AND e.relation IN (%s)`, placeholders)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("neighbors: %w", err)
	}
	defer rows.Close()
	var out []neighbor
	seen := map[string]bool{}
	for rows.Next() {
		var n Node
		var e Edge
		var ext int
		if err := rows.Scan(&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind,
			&n.LineStart, &n.LineEnd, &n.Complexity, &n.Package, &n.RelPath,
			&e.SourceFQN, &e.TargetFQN, &e.Relation, &ext); err != nil {
			return nil, fmt.Errorf("neighbors scan: %w", err)
		}
		e.IsExternal = ext != 0
		key := n.FQN + "|" + e.Relation + "|" + e.SourceFQN + "->" + e.TargetFQN
		if seen[key] {
			continue
		}
		seen[key] = true
		cp := n
		out = append(out, neighbor{node: &cp, edge: e})
	}
	return out, rows.Err()
}

func (s *Store) getNodeByFQN(ctx context.Context, fqn string) (*Node, error) {
	var n Node
	err := s.db.QueryRowContext(ctx, `
		SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity, n.package, COALESCE(f.path, '')
		FROM nodes n LEFT JOIN files f ON f.id = n.file_id
		WHERE n.fqn = ?`, fqn).Scan(
		&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind, &n.LineStart, &n.LineEnd, &n.Complexity, &n.Package, &n.RelPath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", fqn, err)
	}
	return &n, nil
}

// FindPath returns a callee-direction path from fromFQN to toFQN (BFS, cycle-safe).
func (s *Store) FindPath(ctx context.Context, fromFQN, toFQN string, maxDepth int) (*GraphPath, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("ckg store is nil")
	}
	fromFQN, toFQN = strings.TrimSpace(fromFQN), strings.TrimSpace(toFQN)
	if fromFQN == "" || toFQN == "" {
		return nil, fmt.Errorf("from/to FQN is empty")
	}
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("ckg store is closed")
	}

	if maxDepth <= 0 {
		maxDepth = 6
	}
	src, err := s.getNodeByFQN(ctx, fromFQN)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, fmt.Errorf("node %q not found", fromFQN)
	}
	if fromFQN == toFQN {
		return &GraphPath{Nodes: []*Node{src}}, nil
	}

	type frame struct {
		fqn    string
		parent string
		edge   *Edge
		depth  int
	}
	visited := map[string]bool{fromFQN: true}
	parent := map[string]frame{fromFQN: {fqn: fromFQN}}
	queue := []frame{{fqn: fromFQN, depth: 0}}
	found := false

	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}
		neigh, err := s.neighbors(ctx, cur.fqn, true, []string{"calls", "instantiates"})
		if err != nil {
			return nil, err
		}
		for _, nb := range neigh {
			if visited[nb.node.FQN] {
				continue
			}
			visited[nb.node.FQN] = true
			e := nb.edge
			parent[nb.node.FQN] = frame{fqn: nb.node.FQN, parent: cur.fqn, edge: &e, depth: cur.depth + 1}
			if nb.node.FQN == toFQN {
				found = true
				break
			}
			queue = append(queue, frame{fqn: nb.node.FQN, depth: cur.depth + 1})
		}
	}
	if !found {
		return nil, nil
	}

	var fqns []string
	for at := toFQN; at != ""; at = parent[at].parent {
		fqns = append(fqns, at)
		if at == fromFQN {
			break
		}
	}
	// reverse
	for i, j := 0, len(fqns)-1; i < j; i, j = i+1, j-1 {
		fqns[i], fqns[j] = fqns[j], fqns[i]
	}
	path := &GraphPath{}
	for _, f := range fqns {
		n, err := s.getNodeByFQN(ctx, f)
		if err != nil {
			return nil, err
		}
		if n == nil {
			n = &Node{FQN: f, ShortName: f, Kind: "external"}
		}
		path.Nodes = append(path.Nodes, n)
	}
	for i := 1; i < len(fqns); i++ {
		if e := parent[fqns[i]].edge; e != nil {
			path.Edges = append(path.Edges, e)
		}
	}
	return path, nil
}

// TraverseDFS is a depth-first variant of TraverseBFS. First-discovery depth is
// recorded (not necessarily shortest). Cycle protection is the same visited set.
func (s *Store) TraverseDFS(ctx context.Context, startFQN string, dir TraversalDirection, opts TraversalOptions) (*Subgraph, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("ckg store is nil")
	}
	startFQN = strings.TrimSpace(startFQN)
	if startFQN == "" {
		return nil, fmt.Errorf("start FQN is empty")
	}
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("ckg store is closed")
	}

	opts = opts.normalized()
	root, err := s.getNodeByFQN(ctx, startFQN)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("node %q not found", startFQN)
	}
	sg := &Subgraph{
		Root:  root,
		Nodes: map[string]*Node{root.FQN: root},
		Depth: map[string]int{root.FQN: 0},
	}
	stop := opts.stopSet()
	rels := opts.relations()
	type frame struct {
		fqn   string
		depth int
	}
	stack := []frame{{fqn: root.FQN, depth: 0}}
	visited := map[string]bool{root.FQN: true}

	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur.depth >= opts.MaxDepth {
			continue
		}
		if stop[cur.fqn] && cur.depth > 0 {
			continue
		}
		var neigh []neighbor
		if dir == DirectionDownstream || dir == DirectionBoth {
			n, err := s.neighbors(ctx, cur.fqn, true, rels)
			if err != nil {
				return nil, err
			}
			neigh = append(neigh, n...)
		}
		if dir == DirectionUpstream || dir == DirectionBoth {
			n, err := s.neighbors(ctx, cur.fqn, false, rels)
			if err != nil {
				return nil, err
			}
			neigh = append(neigh, n...)
		}
		// Push in reverse so the first neighbor is visited first.
		for i := len(neigh) - 1; i >= 0; i-- {
			nb := neigh[i]
			if visited[nb.node.FQN] {
				sg.addEdge(nb.edge)
				continue
			}
			if len(sg.Nodes) >= opts.MaxNodes {
				sg.addEdge(nb.edge)
				continue
			}
			visited[nb.node.FQN] = true
			cp := nb.node
			sg.Nodes[cp.FQN] = cp
			sg.Depth[cp.FQN] = cur.depth + 1
			sg.addEdge(nb.edge)
			if cp.Kind == "external" || nb.edge.IsExternal {
				continue
			}
			if stop == nil || !stop[cp.FQN] {
				stack = append(stack, frame{fqn: cp.FQN, depth: cur.depth + 1})
			}
		}
	}
	return sg, nil
}

func ParseTraversalDirection(s string) TraversalDirection {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "downstream", "callees", "out", "forward":
		return DirectionDownstream
	case "upstream", "callers", "in", "back":
		return DirectionUpstream
	default:
		return DirectionBoth
	}
}
