package ai

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/random"
)

// TimeLimit ticks child and enforces a maximum duration d of game time for one run.
//
// If child is still returning StatusRunning once accumulated dt reaches d (or immediately when d <= 0), TimeLimit calls child.Reset(), clears its timer, and returns StatusFailure. Completing with StatusSuccess or StatusFailure, or calling Reset, clears the elapsed timer.
func TimeLimit(d time.Duration, child Node) Node {
	return &timeLimitNode{limit: d, child: child}
}

type timeLimitNode struct {
	limit   time.Duration
	elapsed time.Duration
	child   Node
	trace   btTraceState
}

func (t *timeLimitNode) Tick(dt time.Duration) Status {
	if t.child == nil {
		t.elapsed = 0
		return t.trace.record(StatusFailure)
	}
	st := t.child.Tick(dt)
	if st != StatusRunning {
		t.elapsed = 0
		return t.trace.record(st)
	}
	if dt > 0 {
		t.elapsed += dt
	}
	if t.limit <= 0 || t.elapsed >= t.limit {
		t.elapsed = 0
		t.child.Reset()
		return t.trace.record(StatusFailure)
	}
	return t.trace.record(StatusRunning)
}

func (t *timeLimitNode) Reset() {
	t.elapsed = 0
	t.trace.resetTrace()
	if t.child != nil {
		t.child.Reset()
	}
}

func (t *timeLimitNode) btKind() string               { return string(BTNodeTimeLimit) }
func (t *timeLimitNode) btLastStatus() (Status, bool) { return t.trace.status() }
func (t *timeLimitNode) btChildCount() int {
	if t.child == nil {
		return 0
	}
	return 1
}
func (t *timeLimitNode) btChildAt(int) Node { return t.child }
func (t *timeLimitNode) btChildActive(i int) bool {
	return i == 0 && t.trace.ticked && t.trace.lastStatus == StatusRunning
}

// Retry reruns a failing child up to n additional times (up to 1+n total attempts).
//
// Like Repeater, Retry ticks child at most once per Tick, returning StatusRunning between failed attempts and StatusSuccess as soon as child succeeds. When n <= 0, no retries are performed and a child failure returns StatusFailure immediately.
func Retry(n int, child Node) Node {
	return &retryNode{retries: n, child: child}
}

type retryNode struct {
	retries  int
	failures int
	child    Node
	trace    btTraceState
}

func (r *retryNode) Tick(dt time.Duration) Status {
	if r.child == nil {
		r.failures = 0
		return r.trace.record(StatusFailure)
	}
	switch r.child.Tick(dt) {
	case StatusRunning:
		return r.trace.record(StatusRunning)
	case StatusSuccess:
		r.failures = 0
		return r.trace.record(StatusSuccess)
	default:
		if r.retries > 0 && r.failures < r.retries {
			r.failures++
			return r.trace.record(StatusRunning)
		}
		r.failures = 0
		return r.trace.record(StatusFailure)
	}
}

func (r *retryNode) Reset() {
	r.failures = 0
	r.trace.resetTrace()
	if r.child != nil {
		r.child.Reset()
	}
}

func (r *retryNode) btKind() string               { return string(BTNodeRetry) }
func (r *retryNode) btLastStatus() (Status, bool) { return r.trace.status() }
func (r *retryNode) btChildCount() int {
	if r.child == nil {
		return 0
	}
	return 1
}
func (r *retryNode) btChildAt(int) Node { return r.child }
func (r *retryNode) btChildActive(i int) bool {
	return i == 0 && r.trace.ticked && r.trace.lastStatus == StatusRunning
}

// RandomSelector tries children in a shuffled order drawn from stream at the start of each run, returning on the first child that succeeds or runs.
//
// When a child returns StatusRunning, RandomSelector keeps the chosen order and resumes that child on subsequent ticks; if that child later fails, evaluation continues with the remaining untried children in the shuffled order. A nil stream falls back to declaration order.
func RandomSelector(stream *random.Stream, children ...Node) Node {
	cloned := slices.Clone(children)
	return &randomSelectorNode{
		children: cloned,
		stream:   stream,
		order:    make([]int, len(cloned)),
	}
}

type randomSelectorNode struct {
	children []Node
	stream   *random.Stream
	order    []int
	cur      int
	active   bool
	trace    btTraceState
}

func (s *randomSelectorNode) Tick(dt time.Duration) Status {
	if len(s.children) == 0 {
		return s.trace.record(StatusFailure)
	}
	if !s.active {
		s.active = true
		s.cur = 0
		for i := range s.order {
			s.order[i] = i
		}
		if s.stream != nil {
			random.Shuffle(s.stream, s.order)
		}
	}
	for s.cur < len(s.order) {
		ch := s.children[s.order[s.cur]]
		if ch == nil {
			s.cur++
			continue
		}
		switch ch.Tick(dt) {
		case StatusRunning:
			return s.trace.record(StatusRunning)
		case StatusSuccess:
			s.active = false
			s.cur = 0
			return s.trace.record(StatusSuccess)
		default:
			s.cur++
		}
	}
	s.active = false
	s.cur = 0
	return s.trace.record(StatusFailure)
}

func (s *randomSelectorNode) Reset() {
	s.active = false
	s.cur = 0
	s.trace.resetTrace()
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (s *randomSelectorNode) btKind() string               { return string(BTNodeRandomSelector) }
func (s *randomSelectorNode) btLastStatus() (Status, bool) { return s.trace.status() }
func (s *randomSelectorNode) btChildCount() int            { return len(s.children) }
func (s *randomSelectorNode) btChildAt(i int) Node         { return s.children[i] }
func (s *randomSelectorNode) btChildActive(i int) bool {
	return s.trace.ticked && s.trace.lastStatus == StatusRunning && s.active && s.cur >= 0 && s.cur < len(s.order) && i == s.order[s.cur]
}

// RandomSequence runs children in a shuffled order drawn from stream at the start of each run, succeeding when every child succeeds and failing on the first failure.
//
// When a child returns StatusRunning, RandomSequence keeps the chosen order and resumes at that child on subsequent ticks until completion or Reset. A nil stream falls back to declaration order.
func RandomSequence(stream *random.Stream, children ...Node) Node {
	cloned := slices.Clone(children)
	return &randomSequenceNode{
		children: cloned,
		stream:   stream,
		order:    make([]int, len(cloned)),
	}
}

type randomSequenceNode struct {
	children []Node
	stream   *random.Stream
	order    []int
	cur      int
	active   bool
	trace    btTraceState
}

func (s *randomSequenceNode) Tick(dt time.Duration) Status {
	if len(s.children) == 0 {
		return s.trace.record(StatusSuccess)
	}
	if !s.active {
		s.active = true
		s.cur = 0
		for i := range s.order {
			s.order[i] = i
		}
		if s.stream != nil {
			random.Shuffle(s.stream, s.order)
		}
	}
	for s.cur < len(s.order) {
		ch := s.children[s.order[s.cur]]
		if ch == nil {
			s.active = false
			s.cur = 0
			return s.trace.record(StatusFailure)
		}
		switch ch.Tick(dt) {
		case StatusRunning:
			return s.trace.record(StatusRunning)
		case StatusFailure:
			s.active = false
			s.cur = 0
			return s.trace.record(StatusFailure)
		case StatusSuccess:
			s.cur++
		}
	}
	s.active = false
	s.cur = 0
	return s.trace.record(StatusSuccess)
}

func (s *randomSequenceNode) Reset() {
	s.active = false
	s.cur = 0
	s.trace.resetTrace()
	for _, ch := range s.children {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (s *randomSequenceNode) btKind() string               { return string(BTNodeRandomSequence) }
func (s *randomSequenceNode) btLastStatus() (Status, bool) { return s.trace.status() }
func (s *randomSequenceNode) btChildCount() int            { return len(s.children) }
func (s *randomSequenceNode) btChildAt(i int) Node         { return s.children[i] }
func (s *randomSequenceNode) btChildActive(i int) bool {
	return s.trace.ticked && s.trace.lastStatus == StatusRunning && s.active && s.cur >= 0 && s.cur < len(s.order) && i == s.order[s.cur]
}

// WeightedNode pairs a behavior tree child Node with a relative selection weight for WeightedSelector.
//
// Options with Weight <= 0, NaN, Inf, or a nil Node are never selected.
type WeightedNode struct {
	Node   Node
	Weight float64
}

// WeightedSelector draws children without replacement from stream with probability proportional to each WeightedNode.Weight until one returns StatusSuccess or StatusRunning.
//
// When a drawn child returns StatusRunning, WeightedSelector resumes that child on subsequent ticks; if it later returns StatusFailure, drawing continues among the remaining untried eligible options. Options with non-positive, NaN, or infinite weight (or nil Node) are skipped. When stream is nil, eligible options are tried in descending Weight order (ties broken in declaration order).
func WeightedSelector(stream *random.Stream, options ...WeightedNode) Node {
	cloned := slices.Clone(options)
	base := make([]float64, len(cloned))
	for i, opt := range cloned {
		if opt.Node != nil && opt.Weight > 0 && !math.IsNaN(opt.Weight) && !math.IsInf(opt.Weight, 0) {
			base[i] = opt.Weight
		}
	}
	return &weightedSelectorNode{
		options:     cloned,
		baseWeights: base,
		remWeights:  make([]float64, len(cloned)),
		stream:      stream,
		running:     -1,
	}
}

type weightedSelectorNode struct {
	options     []WeightedNode
	baseWeights []float64
	remWeights  []float64
	stream      *random.Stream
	running     int
	active      bool
	trace       btTraceState
}

func (w *weightedSelectorNode) pickNext() int {
	var maxW float64
	count := 0
	firstMax := -1
	lastPos := -1
	for i, wt := range w.remWeights {
		if wt <= 0 {
			continue
		}
		count++
		lastPos = i
		if wt > maxW {
			maxW = wt
			firstMax = i
		}
	}
	if count == 0 {
		return -1
	}
	if count == 1 || w.stream == nil {
		return firstMax
	}
	var total float64
	for _, wt := range w.remWeights {
		if wt > 0 {
			total += wt / maxW
		}
	}
	target := w.stream.Float64() * total
	var acc float64
	for i, wt := range w.remWeights {
		if wt <= 0 {
			continue
		}
		acc += wt / maxW
		if target < acc {
			return i
		}
	}
	return lastPos
}

func (w *weightedSelectorNode) Tick(dt time.Duration) Status {
	if len(w.options) == 0 {
		return w.trace.record(StatusFailure)
	}
	if !w.active {
		w.active = true
		w.running = -1
		copy(w.remWeights, w.baseWeights)
	}
	if w.running >= 0 {
		st := w.options[w.running].Node.Tick(dt)
		switch st {
		case StatusRunning:
			return w.trace.record(StatusRunning)
		case StatusSuccess:
			w.active = false
			w.running = -1
			return w.trace.record(StatusSuccess)
		default:
			w.running = -1
		}
	}
	for {
		idx := w.pickNext()
		if idx < 0 {
			w.active = false
			w.running = -1
			return w.trace.record(StatusFailure)
		}
		w.remWeights[idx] = 0
		st := w.options[idx].Node.Tick(dt)
		switch st {
		case StatusRunning:
			w.running = idx
			return w.trace.record(StatusRunning)
		case StatusSuccess:
			w.active = false
			w.running = -1
			return w.trace.record(StatusSuccess)
		}
	}
}

func (w *weightedSelectorNode) Reset() {
	w.active = false
	w.running = -1
	w.trace.resetTrace()
	for _, opt := range w.options {
		if opt.Node != nil {
			opt.Node.Reset()
		}
	}
}

func (w *weightedSelectorNode) btKind() string               { return string(BTNodeWeightedSelector) }
func (w *weightedSelectorNode) btLastStatus() (Status, bool) { return w.trace.status() }
func (w *weightedSelectorNode) btChildCount() int            { return len(w.options) }
func (w *weightedSelectorNode) btChildAt(i int) Node         { return w.options[i].Node }
func (w *weightedSelectorNode) btChildActive(i int) bool {
	return w.trace.ticked && w.trace.lastStatus == StatusRunning && i == w.running
}

// Service invokes fn immediately on entry into child and then every interval of game time while child remains StatusRunning, before ticking child.
//
// On entry, fn receives the tick's dt; on subsequent periodic ticks, fn receives the elapsed game time since fn last ran (preserving phase across non-divisor steps and firing at most once per Tick). When interval <= 0, fn runs on every tick while active. Finishing (StatusSuccess or StatusFailure) or calling Reset stops the service so the next activation runs fn on entry again.
func Service(interval time.Duration, fn func(dt time.Duration), child Node) Node {
	return &serviceNode{
		interval: interval,
		fn:       fn,
		child:    child,
	}
}

type serviceNode struct {
	interval  time.Duration
	fn        func(dt time.Duration)
	child     Node
	active    bool
	phase     time.Duration
	sinceLast time.Duration
	trace     btTraceState
}

func (s *serviceNode) Tick(dt time.Duration) Status {
	if s.child == nil {
		s.active = false
		s.phase = 0
		s.sinceLast = 0
		return s.trace.record(StatusFailure)
	}
	if !s.active {
		s.active = true
		s.phase = 0
		s.sinceLast = 0
		if s.fn != nil {
			s.fn(dt)
		}
	} else if s.interval <= 0 {
		if s.fn != nil {
			s.fn(dt)
		}
	} else if dt > 0 {
		s.phase += dt
		s.sinceLast += dt
		if s.phase >= s.interval {
			elapsed := s.sinceLast
			s.phase %= s.interval
			s.sinceLast = 0
			if s.fn != nil {
				s.fn(elapsed)
			}
		}
	}
	st := s.child.Tick(dt)
	if st != StatusRunning {
		s.active = false
		s.phase = 0
		s.sinceLast = 0
	}
	return s.trace.record(st)
}

func (s *serviceNode) Reset() {
	s.active = false
	s.phase = 0
	s.sinceLast = 0
	s.trace.resetTrace()
	if s.child != nil {
		s.child.Reset()
	}
}

func (s *serviceNode) btKind() string               { return string(BTNodeService) }
func (s *serviceNode) btLastStatus() (Status, bool) { return s.trace.status() }
func (s *serviceNode) btChildCount() int {
	if s.child == nil {
		return 0
	}
	return 1
}
func (s *serviceNode) btChildAt(int) Node { return s.child }
func (s *serviceNode) btChildActive(i int) bool {
	return i == 0 && s.active && s.trace.ticked && s.trace.lastStatus == StatusRunning
}
