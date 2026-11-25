package ai

import (
	"math"
)

// GraphEdge is a directed outgoing edge from a graph node with a finite non-negative step cost.
type GraphEdge[N comparable] struct {
	To   N
	Cost float64
}

// Graph defines weighted directed adjacency over nodes of comparable type N.
//
// AppendNeighbors appends all outgoing edges from node onto dst and returns the extended slice.
// Reusing dst lets GraphSearch expand nodes without heap allocations on repeated searches.
// Edges with negative, NaN, or infinite Cost are ignored.
type Graph[N comparable] interface {
	AppendNeighbors(dst []GraphEdge[N], node N) []GraphEdge[N]
}

// HeuristicGraph is a Graph that supplies an admissible lower-bound cost estimate
// between two nodes for A* search.
type HeuristicGraph[N comparable] interface {
	Graph[N]
	Heuristic(from, goal N) float64
}

// ValidatedGraph is an optional interface a Graph may implement to reject invalid,
// out-of-bounds, blocked, or removed nodes before searching.
type ValidatedGraph[N comparable] interface {
	ValidNode(node N) bool
}

// NodeOrder is an optional interface a Graph may implement to define a strict total
// order on nodes for deterministic tie-breaking when f and h scores are equal.
type NodeOrder[N comparable] interface {
	NodeLess(a, b N) bool
}

// GraphFunc adapts callback functions to HeuristicGraph[N].
type GraphFunc[N comparable] struct {
	// Neighbors appends outgoing edges from node onto dst.
	Neighbors func(dst []GraphEdge[N], node N) []GraphEdge[N]
	// Estimate optionally returns an admissible heuristic from -> goal (nil uses 0 / Dijkstra).
	Estimate func(from, goal N) float64
	// Valid optionally checks whether a start or goal node is valid (nil accepts all nodes).
	Valid func(node N) bool
	// Less optionally defines a strict total order on nodes for tie-breaking.
	Less func(a, b N) bool
}

// AppendNeighbors delegates to Neighbors when non-nil.
func (f GraphFunc[N]) AppendNeighbors(dst []GraphEdge[N], node N) []GraphEdge[N] {
	if f.Neighbors == nil {
		return dst
	}
	return f.Neighbors(dst, node)
}

// Heuristic delegates to Estimate when non-nil, returning 0 otherwise.
func (f GraphFunc[N]) Heuristic(from, goal N) float64 {
	if f.Estimate == nil {
		return 0
	}
	return f.Estimate(from, goal)
}

// ValidNode delegates to Valid when non-nil, returning true otherwise.
func (f GraphFunc[N]) ValidNode(node N) bool {
	if f.Valid == nil {
		return true
	}
	return f.Valid(node)
}

// NodeLess delegates to Less when non-nil.
func (f GraphFunc[N]) NodeLess(a, b N) bool {
	if f.Less == nil {
		return false
	}
	return f.Less(a, b)
}

var (
	_ HeuristicGraph[int] = GraphFunc[int]{}
	_ ValidatedGraph[int] = GraphFunc[int]{}
	_ NodeOrder[int]      = GraphFunc[int]{}
)

// ReachableNode pairs a node reached by GraphSearch.Range with its minimum path cost from start.
type ReachableNode[N comparable] struct {
	Node N
	Cost float64
}

// GraphRange holds the shortest-path tree produced by GraphSearch.Range or GraphSearch.RangeInto:
// every node reachable from Start within Budget, its minimum cost, and the parent links needed
// to reconstruct the optimal path to any reached node.
type GraphRange[N comparable] struct {
	start   N
	budget  float64
	hasRoot bool
	nodes   []ReachableNode[N]
	parents []int32
	index   map[N]int32
}

// Start returns the origin node of the range query and whether the range is non-empty.
func (r *GraphRange[N]) Start() (N, bool) {
	if r == nil || !r.hasRoot {
		var zero N
		return zero, false
	}
	return r.start, true
}

// Budget returns the maximum path cost budget used to compute this range.
func (r *GraphRange[N]) Budget() float64 {
	if r == nil {
		return 0
	}
	return r.budget
}

// Len returns the number of reachable nodes in the range.
func (r *GraphRange[N]) Len() int {
	if r == nil {
		return 0
	}
	return len(r.nodes)
}

// Nodes returns all reachable nodes and their minimum costs in deterministic order
// (ascending Cost, then deterministic tie-breaking order).
// The returned slice aliases r's internal storage and is overwritten by a subsequent RangeInto call on r.
func (r *GraphRange[N]) Nodes() []ReachableNode[N] {
	if r == nil {
		return nil
	}
	return r.nodes
}

// Contains reports whether node is reachable within the query's budget.
func (r *GraphRange[N]) Contains(node N) bool {
	if r == nil || len(r.nodes) == 0 {
		return false
	}
	_, ok := r.index[node]
	return ok
}

// Cost returns the minimum path cost from Start to node, or (0, false) if node is not in the range.
func (r *GraphRange[N]) Cost(node N) (float64, bool) {
	if r == nil || len(r.nodes) == 0 {
		return 0, false
	}
	idx, ok := r.index[node]
	if !ok {
		return 0, false
	}
	return r.nodes[idx].Cost, true
}

// Parent returns the predecessor of node on its shortest path from Start.
// It returns (zero, false) for Start itself or for nodes outside the range.
func (r *GraphRange[N]) Parent(node N) (N, bool) {
	if r == nil || len(r.nodes) == 0 {
		var zero N
		return zero, false
	}
	idx, ok := r.index[node]
	if !ok {
		var zero N
		return zero, false
	}
	p := r.parents[idx]
	if p < 0 {
		var zero N
		return zero, false
	}
	return r.nodes[p].Node, true
}

// Path returns the shortest path from Start to node inclusive, or (nil, false) if node is not reachable.
func (r *GraphRange[N]) Path(node N) ([]N, bool) {
	return r.PathInto(nil, node)
}

// PathInto writes the shortest path from Start to node inclusive into dst[:0],
// allocating zero times when dst has enough capacity.
func (r *GraphRange[N]) PathInto(dst []N, node N) ([]N, bool) {
	if r == nil || len(r.nodes) == 0 {
		return nil, false
	}
	idx, ok := r.index[node]
	if !ok {
		return nil, false
	}
	length := 0
	for cur := idx; cur >= 0; cur = r.parents[cur] {
		length++
	}
	var out []N
	if cap(dst) < length {
		out = make([]N, length)
	} else {
		out = dst[:length]
	}
	pos := length - 1
	for cur := idx; cur >= 0; cur = r.parents[cur] {
		out[pos] = r.nodes[cur].Node
		pos--
	}
	return out, true
}

type graphSearchNode[N comparable] struct {
	node    N
	g       float64
	h       float64
	f       float64
	parent  int32
	heapPos int32
	outIdx  int32
	closed  bool
}

// GraphSearch performs deterministic A*, Dijkstra, and budgeted range searches over any Graph[N],
// reusing its internal search buffers across calls.
//
// Tie-breaking matches AStar: lowest f, then lowest h, then Less (or the graph's NodeOrder[N]),
// and finally deterministic node discovery order. A GraphSearch is not safe for concurrent use.
type GraphSearch[N comparable] struct {
	// MaxExpansions caps the number of node expansions per search. Zero or negative means unlimited.
	MaxExpansions int
	// Less optionally defines a strict total order on N for tie-breaking equal (f, h) nodes.
	// When nil, GraphSearch checks whether the graph implements NodeOrder[N] and falls back
	// to deterministic discovery order.
	Less func(a, b N) bool

	nodes       []graphSearchNode[N]
	nodeIndex   map[N]int32
	open        []int32
	neighborBuf []GraphEdge[N]
	reached     []int32
}

// NewGraphSearch returns a new reusable GraphSearch for nodes of type N.
func NewGraphSearch[N comparable]() *GraphSearch[N] {
	return &GraphSearch[N]{}
}

// Find runs A* from start to goal on g (using g.Heuristic when g implements HeuristicGraph[N],
// or Dijkstra otherwise), returning the lowest-cost path inclusive of endpoints and its total cost.
//
// Internal search buffers are reused across calls; only the returned path slice is allocated.
func (s *GraphSearch[N]) Find(g Graph[N], start, goal N) (path []N, cost float64, ok bool) {
	return s.FindInto(nil, g, start, goal)
}

// FindInto behaves like Find but writes the resulting path into dst[:0],
// avoiding allocations when dst has enough capacity.
func (s *GraphSearch[N]) FindInto(dst []N, g Graph[N], start, goal N) (path []N, cost float64, ok bool) {
	if g == nil {
		return nil, 0, false
	}
	if hg, ok := g.(HeuristicGraph[N]); ok {
		return s.find(dst, g, start, goal, hg, nil)
	}
	return s.find(dst, g, start, goal, nil, nil)
}

// FindWithHeuristic runs A* from start to goal on g using heuristic h (nil runs Dijkstra).
// Internal search buffers are reused across calls; only the returned path slice is allocated.
func (s *GraphSearch[N]) FindWithHeuristic(g Graph[N], start, goal N, h func(from, goal N) float64) (path []N, cost float64, ok bool) {
	return s.FindWithHeuristicInto(nil, g, start, goal, h)
}

// FindWithHeuristicInto behaves like FindWithHeuristic but writes the path into dst[:0],
// avoiding allocations when dst has enough capacity.
func (s *GraphSearch[N]) FindWithHeuristicInto(dst []N, g Graph[N], start, goal N, h func(from, goal N) float64) (path []N, cost float64, ok bool) {
	if g == nil {
		return nil, 0, false
	}
	return s.find(dst, g, start, goal, nil, h)
}

// FindDijkstra runs Dijkstra's algorithm from start to goal on g (ignoring any HeuristicGraph method),
// returning the lowest-cost path inclusive of endpoints and its total cost.
//
// Internal search buffers are reused across calls; only the returned path slice is allocated.
func (s *GraphSearch[N]) FindDijkstra(g Graph[N], start, goal N) (path []N, cost float64, ok bool) {
	return s.FindDijkstraInto(nil, g, start, goal)
}

// FindDijkstraInto behaves like FindDijkstra but writes the path into dst[:0],
// avoiding allocations when dst has enough capacity.
func (s *GraphSearch[N]) FindDijkstraInto(dst []N, g Graph[N], start, goal N) (path []N, cost float64, ok bool) {
	if g == nil {
		return nil, 0, false
	}
	return s.find(dst, g, start, goal, nil, nil)
}

// Range runs a budgeted Dijkstra search from start on g and returns a GraphRange containing
// every node reachable within budget, its minimum cost, and parent links for path reconstruction.
//
// Internal search buffers are reused across calls; only the returned GraphRange is allocated.
func (s *GraphSearch[N]) Range(g Graph[N], start N, budget float64) *GraphRange[N] {
	return s.RangeInto(nil, g, start, budget)
}

// RangeInto behaves like Range but reuses dst's storage when dst is non-nil,
// avoiding heap allocations after warm-up.
func (s *GraphSearch[N]) RangeInto(dst *GraphRange[N], g Graph[N], start N, budget float64) *GraphRange[N] {
	if dst != nil {
		dst.hasRoot = false
		dst.budget = budget
		clear(dst.nodes)
		dst.nodes = dst.nodes[:0]
		dst.parents = dst.parents[:0]
		if dst.index != nil {
			clear(dst.index)
		}
	}
	if s == nil || g == nil || budget < 0 || math.IsNaN(budget) {
		if dst != nil {
			return dst
		}
		return &GraphRange[N]{budget: budget}
	}
	if vg, ok := g.(ValidatedGraph[N]); ok && !vg.ValidNode(start) {
		if dst != nil {
			return dst
		}
		return &GraphRange[N]{budget: budget}
	}

	s.reset()
	order := s.resolveOrder(g)

	startID := s.addNode(start, 0, 0, -1)
	s.heapPush(startID, order)

	expansions := 0
	maxExp := s.MaxExpansions

	for len(s.open) > 0 {
		if maxExp > 0 && expansions >= maxExp {
			break
		}
		curID := s.heapPop(order)
		curG := s.nodes[curID].g
		if curG > budget {
			break
		}
		expansions++
		s.nodes[curID].closed = true
		s.nodes[curID].outIdx = int32(len(s.reached))
		s.reached = append(s.reached, curID)

		curNode := s.nodes[curID].node
		s.neighborBuf = g.AppendNeighbors(s.neighborBuf[:0], curNode)
		for i := range s.neighborBuf {
			edge := s.neighborBuf[i]
			if edge.Cost < 0 || math.IsNaN(edge.Cost) || math.IsInf(edge.Cost, 0) {
				continue
			}
			ng := curG + edge.Cost
			if ng > budget {
				continue
			}
			nid, exists := s.nodeIndex[edge.To]
			if !exists {
				nid = s.addNode(edge.To, ng, 0, curID)
				s.heapPush(nid, order)
				continue
			}
			if s.nodes[nid].closed {
				continue
			}
			if ng < s.nodes[nid].g {
				s.nodes[nid].g = ng
				s.nodes[nid].f = ng
				s.nodes[nid].parent = curID
				if pos := s.nodes[nid].heapPos; pos >= 0 {
					s.siftUp(int(pos), order)
				} else {
					s.heapPush(nid, order)
				}
			}
		}
	}

	n := len(s.reached)
	if dst == nil {
		dst = &GraphRange[N]{
			nodes:   make([]ReachableNode[N], 0, n),
			parents: make([]int32, 0, n),
			index:   make(map[N]int32, n),
		}
	} else {
		if cap(dst.nodes) < n {
			dst.nodes = make([]ReachableNode[N], 0, n)
		}
		if cap(dst.parents) < n {
			dst.parents = make([]int32, 0, n)
		}
		if dst.index == nil {
			dst.index = make(map[N]int32, n)
		}
	}

	dst.start = start
	dst.budget = budget
	dst.hasRoot = n > 0
	for i, id := range s.reached {
		nd := &s.nodes[id]
		dst.nodes = append(dst.nodes, ReachableNode[N]{
			Node: nd.node,
			Cost: nd.g,
		})
		parentOut := int32(-1)
		if nd.parent >= 0 {
			parentOut = s.nodes[nd.parent].outIdx
		}
		dst.parents = append(dst.parents, parentOut)
		dst.index[nd.node] = int32(i)
	}
	return dst
}

func (s *GraphSearch[N]) find(dst []N, g Graph[N], start, goal N, hg HeuristicGraph[N], hFn func(from, goal N) float64) ([]N, float64, bool) {
	if s == nil || g == nil {
		return nil, 0, false
	}
	if vg, ok := g.(ValidatedGraph[N]); ok {
		if !vg.ValidNode(start) || !vg.ValidNode(goal) {
			return nil, 0, false
		}
	}
	if start == goal {
		var out []N
		if cap(dst) < 1 {
			out = make([]N, 1)
		} else {
			out = dst[:1]
		}
		out[0] = start
		return out, 0, true
	}

	s.reset()
	order := s.resolveOrder(g)

	h0 := evalHeuristic(hg, hFn, start, goal)
	startID := s.addNode(start, 0, h0, -1)
	s.heapPush(startID, order)

	expansions := 0
	maxExp := s.MaxExpansions

	for len(s.open) > 0 {
		if maxExp > 0 && expansions >= maxExp {
			return nil, 0, false
		}
		curID := s.heapPop(order)
		expansions++

		curNode := s.nodes[curID].node
		if curNode == goal {
			return s.reconstructPath(dst, curID), s.nodes[curID].g, true
		}
		s.nodes[curID].closed = true
		curG := s.nodes[curID].g

		s.neighborBuf = g.AppendNeighbors(s.neighborBuf[:0], curNode)
		for i := range s.neighborBuf {
			edge := s.neighborBuf[i]
			if edge.Cost < 0 || math.IsNaN(edge.Cost) || math.IsInf(edge.Cost, 0) {
				continue
			}
			ng := curG + edge.Cost
			nid, exists := s.nodeIndex[edge.To]
			if !exists {
				h := evalHeuristic(hg, hFn, edge.To, goal)
				nid = s.addNode(edge.To, ng, h, curID)
				s.heapPush(nid, order)
				continue
			}
			if s.nodes[nid].closed {
				continue
			}
			if ng < s.nodes[nid].g {
				s.nodes[nid].g = ng
				s.nodes[nid].f = ng + s.nodes[nid].h
				s.nodes[nid].parent = curID
				if pos := s.nodes[nid].heapPos; pos >= 0 {
					s.siftUp(int(pos), order)
				} else {
					s.heapPush(nid, order)
				}
			}
		}
	}

	return nil, 0, false
}

func evalHeuristic[N comparable](hg HeuristicGraph[N], hFn func(from, goal N) float64, from, goal N) float64 {
	var h float64
	if hFn != nil {
		h = hFn(from, goal)
	} else if hg != nil {
		h = hg.Heuristic(from, goal)
	}
	if h < 0 || math.IsNaN(h) || math.IsInf(h, 0) {
		return 0
	}
	return h
}

type graphOrder[N comparable] struct {
	less  func(a, b N) bool
	order NodeOrder[N]
}

func (s *GraphSearch[N]) resolveOrder(g Graph[N]) graphOrder[N] {
	if s.Less != nil {
		return graphOrder[N]{less: s.Less}
	}
	if ord, ok := g.(NodeOrder[N]); ok {
		return graphOrder[N]{order: ord}
	}
	return graphOrder[N]{}
}

func (o graphOrder[N]) compare(a, b N) int {
	if o.less != nil {
		if o.less(a, b) {
			return -1
		}
		if o.less(b, a) {
			return 1
		}
		return 0
	}
	if o.order != nil {
		if o.order.NodeLess(a, b) {
			return -1
		}
		if o.order.NodeLess(b, a) {
			return 1
		}
	}
	return 0
}

func (s *GraphSearch[N]) reset() {
	clear(s.nodes)
	s.nodes = s.nodes[:0]
	if s.nodeIndex == nil {
		s.nodeIndex = make(map[N]int32)
	} else {
		clear(s.nodeIndex)
	}
	s.open = s.open[:0]
	clear(s.neighborBuf)
	s.neighborBuf = s.neighborBuf[:0]
	s.reached = s.reached[:0]
}

func (s *GraphSearch[N]) addNode(node N, g, h float64, parent int32) int32 {
	id := int32(len(s.nodes))
	s.nodes = append(s.nodes, graphSearchNode[N]{
		node:    node,
		g:       g,
		h:       h,
		f:       g + h,
		parent:  parent,
		heapPos: -1,
		outIdx:  -1,
	})
	s.nodeIndex[node] = id
	return id
}

func (s *GraphSearch[N]) reconstructPath(dst []N, endID int32) []N {
	length := 0
	for id := endID; id >= 0; id = s.nodes[id].parent {
		length++
	}
	var out []N
	if cap(dst) < length {
		out = make([]N, length)
	} else {
		out = dst[:length]
	}
	pos := length - 1
	for id := endID; id >= 0; id = s.nodes[id].parent {
		out[pos] = s.nodes[id].node
		pos--
	}
	return out
}

func (s *GraphSearch[N]) lessID(aID, bID int32, order graphOrder[N]) bool {
	a := &s.nodes[aID]
	b := &s.nodes[bID]
	if a.f != b.f {
		return a.f < b.f
	}
	if a.h != b.h {
		return a.h < b.h
	}
	if cmp := order.compare(a.node, b.node); cmp != 0 {
		return cmp < 0
	}
	return aID < bID
}

func (s *GraphSearch[N]) heapPush(id int32, order graphOrder[N]) {
	idx := len(s.open)
	s.open = append(s.open, id)
	s.nodes[id].heapPos = int32(idx)
	s.siftUp(idx, order)
}

func (s *GraphSearch[N]) heapPop(order graphOrder[N]) int32 {
	top := s.open[0]
	s.nodes[top].heapPos = -1
	last := len(s.open) - 1
	if last == 0 {
		s.open = s.open[:0]
		return top
	}
	s.open[0] = s.open[last]
	s.nodes[s.open[0]].heapPos = 0
	s.open = s.open[:last]
	s.siftDown(0, order)
	return top
}

func (s *GraphSearch[N]) siftUp(i int, order graphOrder[N]) {
	for i > 0 {
		p := (i - 1) / 2
		if !s.lessID(s.open[i], s.open[p], order) {
			break
		}
		s.open[i], s.open[p] = s.open[p], s.open[i]
		s.nodes[s.open[i]].heapPos = int32(i)
		s.nodes[s.open[p]].heapPos = int32(p)
		i = p
	}
}

func (s *GraphSearch[N]) siftDown(i int, order graphOrder[N]) {
	n := len(s.open)
	for {
		l := 2*i + 1
		if l >= n {
			break
		}
		best := l
		if r := l + 1; r < n && s.lessID(s.open[r], s.open[l], order) {
			best = r
		}
		if !s.lessID(s.open[best], s.open[i], order) {
			break
		}
		s.open[i], s.open[best] = s.open[best], s.open[i]
		s.nodes[s.open[i]].heapPos = int32(i)
		s.nodes[s.open[best]].heapPos = int32(best)
		i = best
	}
}
