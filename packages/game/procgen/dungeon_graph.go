package procgen

import (
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/random"
)

// RoomRole identifies a node's structural role in a dungeon LayoutGraph.
type RoomRole string

const (
	// RoleStart is the entrance room where the player begins.
	RoleStart RoomRole = "start"
	// RoleGoal is the exit or boss room at the end of the critical path.
	RoleGoal RoomRole = "goal"
	// RoleCritical is an interior room along the critical path from Start to Goal.
	RoleCritical RoomRole = "critical"
	// RoleBranch is an optional side room off the critical path.
	RoleBranch RoomRole = "branch"
	// RoleKey is a room holding a key that unlocks a locked passage later in the dungeon.
	RoleKey RoomRole = "key"
)

// GraphNode is one room in a LayoutGraph.
type GraphNode struct {
	// ID is the node's 0-based index in LayoutGraph.Nodes.
	ID int
	// Role is start, goal, critical, branch or key.
	Role RoomRole
	// Depth is the shortest-path edge count from Start.
	Depth int
	// Tier is the lock-progression zone (0 is reachable without any keys, k requires keys 1..k).
	Tier int
	// Tag is the preferred room template tag for this node (such as "start", "goal" or "key").
	Tag string
	// KeyID is non-empty when this room holds the key for locked edges with the matching LockID.
	KeyID string
}

// GraphEdge is an undirected passage between two rooms in a LayoutGraph.
type GraphEdge struct {
	// From and To are node IDs in LayoutGraph.Nodes.
	From int
	To   int
	// LockID is non-empty when this passage is locked and requires the key with matching KeyID.
	LockID string
	// Loop is true when this edge adds a cycle beyond the spanning tree.
	Loop bool
}

// LockKey records a lock/key pair in progression order.
type LockKey struct {
	// ID matches GraphNode.KeyID and GraphEdge.LockID.
	ID string
	// KeyNode is the ID of the node holding this key.
	KeyNode int
	// EdgeIndex is the index into LayoutGraph.Edges of the locked passage.
	EdgeIndex int
}

// LayoutGraph is the abstract room-and-passage topology of a dungeon.
type LayoutGraph struct {
	// Start is the entrance node ID (always 0).
	Start int
	// Goal is the exit/boss node ID.
	Goal int
	// CriticalPath lists the node IDs along the main route from Start to Goal.
	CriticalPath []int
	// Nodes holds every room node indexed by GraphNode.ID.
	Nodes []GraphNode
	// Edges holds every undirected passage between rooms.
	Edges []GraphEdge
	// Locks lists every lock/key pair in progression order.
	Locks []LockKey
}

// GraphConfig configures GenerateGraph. Zero fields take the defaults given on each field.
type GraphConfig struct {
	// CriticalPath is the number of rooms on the main path from Start to Goal, inclusive.
	// The default is 5; when set it must be at least 2.
	CriticalPath int
	// Branches is the number of side rooms attached off the critical path and earlier branches.
	// Additional key rooms may be added if a lock tier has no available branch room. The default is 0.
	Branches int
	// BranchMaxDepth limits how far a branch chain may extend from the critical path.
	// The default is 2; when Branches > 0 it must be at least 1.
	BranchMaxDepth int
	// Loops is the desired number of extra cycle edges added within the same lock tier.
	// Edges are chosen between nodes of opposite spanning-tree parity so cycles have even length
	// and can be embedded on an orthogonal room grid. The default is 0.
	Loops int
	// Locks is the number of lock/key pairs placed along the critical path.
	// Each key is placed in a tier strictly before its lock so it is always reachable without passing the lock.
	// The default is 0; it must be at most CriticalPath - 1.
	Locks int
	// MaxDegree caps how many passages may connect to a single room.
	// The default is 4 (matching the four cardinal sides of a 2D room).
	MaxDegree int
}

func (c GraphConfig) withDefaults() GraphConfig {
	if c.CriticalPath == 0 {
		c.CriticalPath = 5
	}
	if c.BranchMaxDepth == 0 {
		c.BranchMaxDepth = 2
	}
	if c.MaxDegree == 0 {
		c.MaxDegree = 4
	}
	return c
}

func (c GraphConfig) validate() error {
	switch {
	case c.CriticalPath < 2:
		return fmt.Errorf("%w: Graph CriticalPath %d must be at least 2", ErrInvalidConfig, c.CriticalPath)
	case c.Branches < 0:
		return fmt.Errorf("%w: Graph Branches %d is negative", ErrInvalidConfig, c.Branches)
	case c.BranchMaxDepth < 1:
		return fmt.Errorf("%w: Graph BranchMaxDepth %d must be at least 1", ErrInvalidConfig, c.BranchMaxDepth)
	case c.Loops < 0:
		return fmt.Errorf("%w: Graph Loops %d is negative", ErrInvalidConfig, c.Loops)
	case c.Locks < 0:
		return fmt.Errorf("%w: Graph Locks %d is negative", ErrInvalidConfig, c.Locks)
	case c.Locks >= c.CriticalPath:
		return fmt.Errorf("%w: Graph Locks %d must be less than CriticalPath %d", ErrInvalidConfig, c.Locks, c.CriticalPath)
	case c.MaxDegree < 2:
		return fmt.Errorf("%w: Graph MaxDegree %d must be at least 2", ErrInvalidConfig, c.MaxDegree)
	case c.MaxDegree < 3 && (c.Branches > 0 || c.Locks > 0 || c.Loops > 0):
		return fmt.Errorf("%w: Graph MaxDegree %d must be at least 3 when Branches, Locks or Loops are set", ErrInvalidConfig, c.MaxDegree)
	}
	return nil
}

// GenerateGraph builds a deterministic dungeon LayoutGraph from s with a critical path from Start to Goal, optional side branches, lock/key progression pairs and same-tier cycles.
//
// Every lock's key is placed in a progression tier strictly preceding its lock, and loop edges only connect rooms within the same tier, so every key is reachable from Start without passing its lock and no loop bypasses a lock.
func GenerateGraph(s *random.Stream, cfg GraphConfig) (*LayoutGraph, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	g, treeDepth := buildGraphTree(s, cfg)
	addGraphLoops(s, g, treeDepth, cfg.Loops, cfg.MaxDegree)
	computeBFSDepths(g)
	return g, nil
}

// buildGraphTree constructs the spanning tree of critical path, locks, branches and keys, returning the graph and each node's tree depth.
func buildGraphTree(s *random.Stream, cfg GraphConfig) (*LayoutGraph, []int) {
	p := cfg.CriticalPath
	g := &LayoutGraph{
		Start:        0,
		Goal:         p - 1,
		CriticalPath: make([]int, p),
		Nodes:        make([]GraphNode, p),
		Edges:        make([]GraphEdge, 0, p-1+cfg.Branches+cfg.Locks+cfg.Loops),
	}
	degree := make([]int, p)
	branchDepth := make([]int, p) // 0 for critical path nodes
	treeDepth := make([]int, p)

	for i := range p {
		role := RoleCritical
		tag := ""
		switch i {
		case 0:
			role = RoleStart
			tag = "start"
		case p - 1:
			role = RoleGoal
			tag = "goal"
		}
		g.CriticalPath[i] = i
		g.Nodes[i] = GraphNode{ID: i, Role: role, Tag: tag}
		treeDepth[i] = i
		if i > 0 {
			g.Edges = append(g.Edges, GraphEdge{From: i - 1, To: i})
			degree[i-1]++
			degree[i]++
		}
	}

	// Pick cfg.Locks distinct critical-path edges in sorted order so lock tiers increase monotonically.
	lockEdges := make([]int, 0, cfg.Locks)
	if cfg.Locks > 0 {
		pool := make([]int, p-1)
		for i := range p - 1 {
			pool[i] = i
		}
		random.Shuffle(s, pool)
		lockEdges = append(lockEdges, pool[:cfg.Locks]...)
		slices.Sort(lockEdges)
		for k, edgeIdx := range lockEdges {
			g.Edges[edgeIdx].LockID = fmt.Sprintf("key-%d", k+1)
		}
	}

	// Assign lock tiers along the critical path.
	tier := 0
	for i := range p {
		g.Nodes[i].Tier = tier
		if i < p-1 && g.Edges[i].LockID != "" {
			tier++
		}
	}

	// Grow cfg.Branches side rooms off non-Goal nodes.
	for range cfg.Branches {
		var parents []int
		for i := range g.Nodes {
			if i == g.Goal {
				continue
			}
			if degree[i] >= cfg.MaxDegree || branchDepth[i] >= cfg.BranchMaxDepth {
				continue
			}
			parents = append(parents, i)
		}
		if len(parents) == 0 {
			break
		}
		parent := random.Pick(s, parents)
		id := len(g.Nodes)
		g.Nodes = append(g.Nodes, GraphNode{
			ID:   id,
			Role: RoleBranch,
			Tier: g.Nodes[parent].Tier,
		})
		g.Edges = append(g.Edges, GraphEdge{From: parent, To: id})
		degree = append(degree, 1)
		degree[parent]++
		branchDepth = append(branchDepth, branchDepth[parent]+1)
		treeDepth = append(treeDepth, treeDepth[parent]+1)
	}

	// Place one key for each lock in tier k (strictly before lock k, which gates tier k+1).
	for k, edgeIdx := range lockEdges {
		lockID := g.Edges[edgeIdx].LockID
		keyNode := pickOrCreateKeyNode(s, g, &degree, &branchDepth, &treeDepth, k, cfg.MaxDegree)
		g.Nodes[keyNode].Role = RoleKey
		g.Nodes[keyNode].Tag = "key"
		g.Nodes[keyNode].KeyID = lockID
		g.Locks = append(g.Locks, LockKey{
			ID:        lockID,
			KeyNode:   keyNode,
			EdgeIndex: edgeIdx,
		})
	}

	return g, treeDepth
}

// pickOrCreateKeyNode chooses an existing branch leaf (or branch room) in tier k, or attaches a new dedicated key room to a node in tier k when none is available.
func pickOrCreateKeyNode(s *random.Stream, g *LayoutGraph, degree, branchDepth, treeDepth *[]int, tier, maxDegree int) int {
	var leaves, branches []int
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Tier != tier || n.Role != RoleBranch || n.KeyID != "" {
			continue
		}
		if (*degree)[i] == 1 {
			leaves = append(leaves, i)
		} else {
			branches = append(branches, i)
		}
	}
	if len(leaves) > 0 {
		return random.Pick(s, leaves)
	}
	if len(branches) > 0 {
		return random.Pick(s, branches)
	}
	// Attach a new key room off an available node in tier (or an earlier tier if tier is saturated).
	for targetTier := tier; targetTier >= 0; targetTier-- {
		var parents []int
		for i := range g.Nodes {
			if i == g.Goal {
				continue
			}
			if g.Nodes[i].Tier == targetTier && (*degree)[i] < maxDegree {
				parents = append(parents, i)
			}
		}
		if len(parents) > 0 {
			parent := random.Pick(s, parents)
			id := len(g.Nodes)
			g.Nodes = append(g.Nodes, GraphNode{
				ID:   id,
				Role: RoleKey,
				Tier: targetTier,
				Tag:  "key",
			})
			g.Edges = append(g.Edges, GraphEdge{From: parent, To: id})
			*degree = append(*degree, 1)
			(*degree)[parent]++
			*branchDepth = append(*branchDepth, (*branchDepth)[parent]+1)
			*treeDepth = append(*treeDepth, (*treeDepth)[parent]+1)
			return id
		}
	}
	// Fallback: reuse a non-start, non-goal, non-key critical node in tier <= target.
	for targetTier := tier; targetTier >= 0; targetTier-- {
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if n.Tier == targetTier && i != g.Start && i != g.Goal && n.KeyID == "" {
				return i
			}
		}
	}
	return g.Start
}

// addGraphLoops adds up to wantLoops same-tier cycle edges between non-adjacent nodes of opposite spanning-tree parity so cycles have even length (4, 6, ...).
func addGraphLoops(s *random.Stream, g *LayoutGraph, treeDepth []int, wantLoops, maxDegree int) {
	if wantLoops <= 0 {
		return
	}
	n := len(g.Nodes)
	adj := make([][]int, n)
	deg := make([]int, n)
	hasEdge := make([]bool, n*n)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
		deg[e.From]++
		deg[e.To]++
		hasEdge[e.From*n+e.To] = true
		hasEdge[e.To*n+e.From] = true
	}

	// Compute all-pairs shortest distances in the spanning tree to favor compact 4-cycles (distance 3).
	dist := make([][]int, n)
	queue := make([]int, 0, n)
	for src := range n {
		d := make([]int, n)
		for i := range d {
			d[i] = -1
		}
		d[src] = 0
		queue = append(queue[:0], src)
		for head := 0; head < len(queue); head++ {
			u := queue[head]
			for _, v := range adj[u] {
				if d[v] < 0 {
					d[v] = d[u] + 1
					queue = append(queue, v)
				}
			}
		}
		dist[src] = d
	}

	type pair struct{ u, v, d int }
	var cands []pair
	for u := range n {
		if u == g.Goal {
			continue
		}
		for v := u + 1; v < n; v++ {
			if v == g.Goal || hasEdge[u*n+v] {
				continue
			}
			if g.Nodes[u].Tier != g.Nodes[v].Tier {
				continue
			}
			// Opposite parity guarantees even-length cycles (embeddable on an orthogonal 2D grid).
			if (treeDepth[u] & 1) == (treeDepth[v] & 1) {
				continue
			}
			cands = append(cands, pair{u: u, v: v, d: dist[u][v]})
		}
	}
	// Shuffle first, then stable-sort by tree distance ascending so 4-cycles (d=3) are tried first.
	random.Shuffle(s, cands)
	slices.SortStableFunc(cands, func(a, b pair) int { return a.d - b.d })

	added := 0
	for _, c := range cands {
		if added >= wantLoops {
			break
		}
		if deg[c.u] >= maxDegree || deg[c.v] >= maxDegree {
			continue
		}
		g.Edges = append(g.Edges, GraphEdge{From: c.u, To: c.v, Loop: true})
		deg[c.u]++
		deg[c.v]++
		added++
	}
}

func computeBFSDepths(g *LayoutGraph) {
	n := len(g.Nodes)
	adj := make([][]int, n)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	for i := range g.Nodes {
		g.Nodes[i].Depth = -1
	}
	g.Nodes[g.Start].Depth = 0
	queue := []int{g.Start}
	for head := 0; head < len(queue); head++ {
		u := queue[head]
		for _, v := range adj[u] {
			if g.Nodes[v].Depth < 0 {
				g.Nodes[v].Depth = g.Nodes[u].Depth + 1
				queue = append(queue, v)
			}
		}
	}
}

// Neighbors returns the IDs of all nodes directly connected to nodeID by an edge in g, in edge order.
func (g *LayoutGraph) Neighbors(nodeID int) []int {
	if g == nil {
		return nil
	}
	var out []int
	for _, e := range g.Edges {
		switch {
		case e.From == nodeID:
			out = append(out, e.To)
		case e.To == nodeID:
			out = append(out, e.From)
		}
	}
	return out
}

// VerifyLocks checks that g's lock/key structure is sound: every lock has a valid key node, progressive traversal from Start collects every key without ever crossing a lock whose key has not yet been collected, every lock's key is reachable without crossing that lock, and Goal is reachable once keys are collected.
func (g *LayoutGraph) VerifyLocks() error {
	if g == nil || len(g.Nodes) == 0 {
		return fmt.Errorf("%w: empty layout graph", ErrInvalidConfig)
	}
	if g.Start < 0 || g.Start >= len(g.Nodes) || g.Goal < 0 || g.Goal >= len(g.Nodes) {
		return fmt.Errorf("%w: graph Start/Goal out of range", ErrInvalidConfig)
	}
	n := len(g.Nodes)
	adj := make([][]int, n) // stores edge indices
	for idx, e := range g.Edges {
		if e.From < 0 || e.From >= n || e.To < 0 || e.To >= n {
			return fmt.Errorf("%w: edge %d endpoint out of range", ErrInvalidConfig, idx)
		}
		adj[e.From] = append(adj[e.From], idx)
		adj[e.To] = append(adj[e.To], idx)
	}
	for i, lk := range g.Locks {
		if lk.ID == "" {
			return fmt.Errorf("procgen: lock %d has empty ID", i)
		}
		if lk.KeyNode < 0 || lk.KeyNode >= n {
			return fmt.Errorf("procgen: lock %q key node %d out of range", lk.ID, lk.KeyNode)
		}
		if lk.EdgeIndex < 0 || lk.EdgeIndex >= len(g.Edges) {
			return fmt.Errorf("procgen: lock %q edge index %d out of range", lk.ID, lk.EdgeIndex)
		}
		if g.Nodes[lk.KeyNode].KeyID != lk.ID {
			return fmt.Errorf("procgen: lock %q key node %d has KeyID %q", lk.ID, lk.KeyNode, g.Nodes[lk.KeyNode].KeyID)
		}
		if g.Edges[lk.EdgeIndex].LockID != lk.ID {
			return fmt.Errorf("procgen: lock %q edge %d has LockID %q", lk.ID, lk.EdgeIndex, g.Edges[lk.EdgeIndex].LockID)
		}
	}

	// Progressive key collection simulation: never cross a locked edge until its key is already owned.
	owned := make(map[string]bool, len(g.Locks))
	visited := make([]bool, n)
	queue := make([]int, 0, n)
	for {
		for i := range visited {
			visited[i] = false
		}
		visited[g.Start] = true
		queue = append(queue[:0], g.Start)
		gained := false
		for head := 0; head < len(queue); head++ {
			u := queue[head]
			if k := g.Nodes[u].KeyID; k != "" && !owned[k] {
				owned[k] = true
				gained = true
			}
			for _, ei := range adj[u] {
				e := g.Edges[ei]
				if e.LockID != "" && !owned[e.LockID] {
					continue
				}
				v := e.To
				if v == u {
					v = e.From
				}
				if !visited[v] {
					visited[v] = true
					queue = append(queue, v)
				}
			}
		}
		if !gained {
			break
		}
	}
	for _, lk := range g.Locks {
		if !owned[lk.ID] {
			return fmt.Errorf("procgen: key %q (node %d) is not reachable before its lock", lk.ID, lk.KeyNode)
		}
	}
	if !visited[g.Goal] {
		return fmt.Errorf("procgen: goal node %d is not reachable from start %d", g.Goal, g.Start)
	}
	for i := range n {
		if !visited[i] {
			return fmt.Errorf("procgen: node %d is not reachable from start %d", i, g.Start)
		}
	}

	// Explicit single-lock reachability check: removing lock lk completely still leaves lk.KeyNode reachable from Start.
	for _, lk := range g.Locks {
		for i := range visited {
			visited[i] = false
		}
		visited[g.Start] = true
		queue = append(queue[:0], g.Start)
		for head := 0; head < len(queue); head++ {
			u := queue[head]
			for _, ei := range adj[u] {
				e := g.Edges[ei]
				if e.LockID == lk.ID {
					continue
				}
				v := e.To
				if v == u {
					v = e.From
				}
				if !visited[v] {
					visited[v] = true
					queue = append(queue, v)
				}
			}
		}
		if !visited[lk.KeyNode] {
			return fmt.Errorf("procgen: key %q (node %d) is unreachable without passing its lock", lk.ID, lk.KeyNode)
		}
	}
	return nil
}
