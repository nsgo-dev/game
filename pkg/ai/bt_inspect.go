package ai

import (
	"strconv"
	"strings"
	"time"
)

// btTraceState records a built-in node's most recent Tick result without heap allocations.
type btTraceState struct {
	lastStatus Status
	ticked     bool
}

func (t *btTraceState) record(st Status) Status {
	t.lastStatus = st
	t.ticked = true
	return st
}

func (t *btTraceState) resetTrace() {
	t.lastStatus = 0
	t.ticked = false
}

func (t *btTraceState) status() (Status, bool) {
	return t.lastStatus, t.ticked
}

// btInspectable is implemented internally by built-in behavior tree nodes so BTInspector
// can walk the tree structure and inspect statuses without mutating execution state.
type btInspectable interface {
	btKind() string
	btLastStatus() (Status, bool)
	btChildCount() int
	btChildAt(i int) Node
	btChildActive(i int) bool
}

// Named attaches an optional human-readable name to child for inspection, debug overlays,
// and statedump formatting.
//
// Named is transparent to tree topology: inspecting a Named built-in node reports a single
// entry with child's Kind and children and the given Name. Wrapping a custom Node also
// records its last Tick status for inspection. If name is empty, Named returns child unchanged.
func Named(name string, child Node) Node {
	if name == "" {
		return child
	}
	n := &namedNode{name: name, child: child}
	n.clockState.adopt(child)
	return n
}

type namedNode struct {
	name       string
	child      Node
	clockState btClockState
	trace      btTraceState
}

func (n *namedNode) btClockRef() *btClockState { return &n.clockState }

func (n *namedNode) Tick(dt time.Duration) Status {
	n.clockState.advanceIfRoot(dt)
	if n.child == nil {
		return n.trace.record(StatusFailure)
	}
	return n.trace.record(n.child.Tick(dt))
}

func (n *namedNode) Reset() {
	n.trace.resetTrace()
	if n.child != nil {
		n.child.Reset()
	}
}

func (n *namedNode) hasBBObserver() bool { return nodeHasBBObserver(n.child) }

func (n *namedNode) bbWantsPreempt() bool { return nodeWantsPreempt(n.child) }

func (n *namedNode) bbSelfAborted() bool {
	if sa, ok := n.child.(btSelfAbortable); ok {
		return sa.bbSelfAborted()
	}
	return false
}

// BTNodeTrace is a read-only snapshot of one node in a behavior tree.
type BTNodeTrace struct {
	// Path is the deterministic structural path from the root ("root", "root/0", "root/0/1", ...).
	Path string
	// Depth is the node's nesting depth in the tree (0 for the root).
	Depth int
	// Name is the optional label assigned via Named or YAML's name field.
	Name string
	// Kind is the node's kind ("selector", "sequence", "action", "wait", ..., or "custom").
	Kind string
	// LastStatus is the Status returned by this node's most recent Tick since build or Reset.
	// Only meaningful when Ticked is true.
	LastStatus Status
	// Ticked reports whether this node has been ticked since construction or its most recent Reset.
	Ticked bool
	// Active reports whether this node lies on the tree's currently running path.
	Active bool
}

// StatusString returns "Idle" when t has not been ticked since construction or Reset,
// and t.LastStatus.String() otherwise.
func (t BTNodeTrace) StatusString() string {
	if !t.Ticked {
		return "Idle"
	}
	return t.LastStatus.String()
}

type btInspectedEntry struct {
	path      string
	depth     int
	name      string
	kind      string
	parentIdx int
	childIdx  int

	// inspect is the underlying built-in btInspectable (nil for bare custom nodes).
	inspect btInspectable
	// namedFallback is the outermost namedNode when inspect is nil (wrapping a custom node).
	namedFallback *namedNode
}

// BTInspector provides zero-allocation, read-only inspection of a behavior tree once warmed up.
//
// Construct one inspector per tree with NewBTInspector(root), then call WalkInto(buf) after
// ticking the tree to populate a reusable slice of BTNodeTrace entries in pre-order.
type BTInspector struct {
	root         Node
	entries      []btInspectedEntry
	rootFallback btTraceState
	scratch      []BTNodeTrace
}

// NewBTInspector builds a reusable read-only inspector for root, precomputing its static
// pre-order traversal and node paths so subsequent WalkInto calls perform zero heap allocations.
func NewBTInspector(root Node) *BTInspector {
	ins := &BTInspector{root: root}
	if root != nil {
		ins.flatten(root, "root", 0, -1, -1)
	}
	return ins
}

func (ins *BTInspector) flatten(node Node, path string, depth, parentIdx, childIdx int) {
	if node == nil {
		return
	}
	var name string
	var outerNamed *namedNode
	cur := node
	for {
		if bt, ok := cur.(*BehaviorTree); ok {
			if bt == nil || bt.root == nil {
				return
			}
			cur = bt.root
			continue
		}
		nn, ok := cur.(*namedNode)
		if !ok {
			break
		}
		if name == "" && nn.name != "" {
			name = nn.name
		}
		if outerNamed == nil {
			outerNamed = nn
		}
		cur = nn.child
		if cur == nil {
			break
		}
	}

	var insp btInspectable
	kind := "custom"
	if cur != nil {
		if b, ok := cur.(btInspectable); ok {
			insp = b
			kind = b.btKind()
		}
	}
	var fallback *namedNode
	if insp == nil {
		fallback = outerNamed
	}

	idx := len(ins.entries)
	ins.entries = append(ins.entries, btInspectedEntry{
		path:          path,
		depth:         depth,
		name:          name,
		kind:          kind,
		parentIdx:     parentIdx,
		childIdx:      childIdx,
		inspect:       insp,
		namedFallback: fallback,
	})

	if insp != nil {
		count := insp.btChildCount()
		for i := range count {
			ch := insp.btChildAt(i)
			if ch == nil {
				continue
			}
			childPath := path + "/" + strconv.Itoa(i)
			ins.flatten(ch, childPath, depth+1, idx, i)
		}
	}
}

// Root returns the behavior tree root inspected by ins.
func (ins *BTInspector) Root() Node {
	if ins == nil {
		return nil
	}
	return ins.root
}

// Len returns the total number of nodes in the inspected tree.
func (ins *BTInspector) Len() int {
	if ins == nil {
		return 0
	}
	return len(ins.entries)
}

// Tick ticks ins's root node by dt and returns its status.
//
// Callers may tick root directly instead of calling ins.Tick; calling ins.Tick additionally
// records the root status when root is a bare custom Node not wrapped in Named.
func (ins *BTInspector) Tick(dt time.Duration) Status {
	if ins == nil || ins.root == nil {
		return StatusFailure
	}
	return ins.rootFallback.record(ins.root.Tick(dt))
}

// Reset resets ins's root node and clears any root fallback status.
func (ins *BTInspector) Reset() {
	if ins == nil {
		return
	}
	ins.rootFallback.resetTrace()
	if ins.root != nil {
		ins.root.Reset()
	}
}

// WalkInto writes a pre-order snapshot of every node in the tree into dst[:0] and returns
// the populated slice. Once cap(dst) >= ins.Len(), WalkInto performs zero heap allocations.
func (ins *BTInspector) WalkInto(dst []BTNodeTrace) []BTNodeTrace {
	dst = dst[:0]
	if ins == nil || len(ins.entries) == 0 {
		return dst
	}
	if cap(dst) < len(ins.entries) {
		dst = make([]BTNodeTrace, len(ins.entries))
	} else {
		dst = dst[:len(ins.entries)]
	}
	for i := range ins.entries {
		e := &ins.entries[i]
		var st Status
		var ticked bool
		switch {
		case e.inspect != nil:
			st, ticked = e.inspect.btLastStatus()
		case e.namedFallback != nil:
			st, ticked = e.namedFallback.trace.status()
		case i == 0 && ins.rootFallback.ticked:
			st, ticked = ins.rootFallback.status()
		}

		active := false
		if e.parentIdx < 0 {
			active = ticked && st == StatusRunning
		} else {
			parentTrace := &dst[e.parentIdx]
			if parentTrace.Active {
				parentInsp := ins.entries[e.parentIdx].inspect
				if parentInsp != nil && parentInsp.btChildActive(e.childIdx) {
					if e.inspect == nil && e.namedFallback == nil {
						// Bare custom child currently running under a built-in composite/decorator.
						st = StatusRunning
						ticked = true
						active = true
					} else if ticked && st == StatusRunning {
						active = true
					}
				}
			}
		}

		dst[i] = BTNodeTrace{
			Path:       e.path,
			Depth:      e.depth,
			Name:       e.name,
			Kind:       e.kind,
			LastStatus: st,
			Ticked:     ticked,
			Active:     active,
		}
	}
	return dst
}

// Walk returns a newly allocated pre-order snapshot of every node in the tree.
// For zero-allocation per-tick inspection, reuse a buffer with WalkInto.
func (ins *BTInspector) Walk() []BTNodeTrace {
	if ins == nil || len(ins.entries) == 0 {
		return nil
	}
	return ins.WalkInto(make([]BTNodeTrace, 0, len(ins.entries)))
}

// RunningPath returns a newly allocated slice of the nodes on the tree's currently running
// path (Active == true) in pre-order. For zero-allocation per-tick inspection, call WalkInto
// followed by RunningPathInto.
func (ins *BTInspector) RunningPath() []BTNodeTrace {
	if ins == nil || len(ins.entries) == 0 {
		return nil
	}
	ins.scratch = ins.WalkInto(ins.scratch)
	return RunningPathInto(nil, ins.scratch)
}

// Format returns a deterministic, human-readable text snapshot of the tree's current state
// suitable for statedump Values and diffing AI decisions across runs.
func (ins *BTInspector) Format() string {
	if ins == nil || len(ins.entries) == 0 {
		return ""
	}
	ins.scratch = ins.WalkInto(ins.scratch)
	return FormatBTTrace(ins.scratch)
}

// String implements fmt.Stringer by returning ins.Format().
func (ins *BTInspector) String() string {
	return ins.Format()
}

// InspectBehaviorTree performs a one-shot read-only walk of root and returns each node's trace
// in pre-order. For repeated per-tick inspection without allocations, construct a BTInspector
// with NewBTInspector and call WalkInto.
func InspectBehaviorTree(root Node) []BTNodeTrace {
	return NewBTInspector(root).Walk()
}

// RunningPathInto appends every node in nodes that lies on the currently running path
// (Active == true) into dst[:0], performing zero allocations when cap(dst) is sufficient.
func RunningPathInto(dst []BTNodeTrace, nodes []BTNodeTrace) []BTNodeTrace {
	dst = dst[:0]
	for i := range nodes {
		if nodes[i].Active {
			dst = append(dst, nodes[i])
		}
	}
	return dst
}

// FormatBTTrace renders nodes in a deterministic canonical text format suitable for
// statedump Values and golden-file diffs across runs.
//
// Each line represents one node indented by 2 spaces per depth level, prefixed with "* " when
// Active (on the running path) or "  " otherwise:
//
//   - selector "root" = Running
//     sequence "attack" = Failure
//     condition "in_range" = Failure
//     action "swing" = Idle
//   - action "patrol" = Running
func FormatBTTrace(nodes []BTNodeTrace) string {
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	for i := range nodes {
		if i > 0 {
			b.WriteByte('\n')
		}
		n := &nodes[i]
		for range n.Depth {
			b.WriteString("  ")
		}
		if n.Active {
			b.WriteString("* ")
		} else {
			b.WriteString("  ")
		}
		kind := n.Kind
		if kind == "" {
			kind = "custom"
		}
		b.WriteString(kind)
		if n.Name != "" {
			b.WriteByte(' ')
			b.WriteString(strconv.Quote(n.Name))
		}
		b.WriteString(" = ")
		b.WriteString(n.StatusString())
	}
	return b.String()
}
