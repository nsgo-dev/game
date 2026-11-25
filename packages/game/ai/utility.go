package ai

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/random"
)

// CurveKind identifies the response curve function used by a Consideration.
type CurveKind string

const (
	// CurveLinear computes y = Slope*(x - ShiftX) + ShiftY, clamped to [0, 1].
	CurveLinear CurveKind = "linear"
	// CurvePolynomial computes y = Slope*(x - ShiftX)^Exponent + ShiftY, clamped to [0, 1].
	CurvePolynomial CurveKind = "polynomial"
	// CurveLogistic computes y = Slope/(1 + exp(-Exponent*(x - ShiftX))) + ShiftY, clamped to [0, 1].
	CurveLogistic CurveKind = "logistic"
	// CurveLogit computes y = Slope*ln(u/(1 - u)) + ShiftY where u = x - ShiftX + 0.5, clamped to [0, 1].
	CurveLogit CurveKind = "logit"
	// CurveStep returns ShiftY when x < ShiftX and ShiftY + Slope when x >= ShiftX, clamped to [0, 1].
	CurveStep CurveKind = "step"
	// CurveResource delegates evaluation to a CurveEvaluator, such as a *resource.Curve loaded from a resource curve file.
	CurveResource CurveKind = "resource"
)

// CurveEvaluator evaluates a curve at a normalized input t in [0, 1].
//
// *resource.Curve satisfies CurveEvaluator directly through its Eval(float32) float32 method, allowing stable package game/ai to use resource curves without importing experimental package game/resource.
type CurveEvaluator interface {
	Eval(t float32) float32
}

// CurveFunc adapts a Go function into a CurveEvaluator.
type CurveFunc func(t float64) float64

// Eval calls f(float64(t)) and returns the result as float32, or 0 if f is nil.
func (f CurveFunc) Eval(t float32) float32 {
	if f == nil {
		return 0
	}
	return float32(f(float64(t)))
}

// ResponseCurve maps a normalized input x in [0, 1] into a utility score in [0, 1].
//
// The zero ResponseCurve acts as the identity curve (y = x).
type ResponseCurve struct {
	// Kind selects the curve formula; empty acts as identity unless Evaluator is set.
	Kind CurveKind
	// Slope scales the curve output (or defines the step height high - low for CurveStep).
	Slope float64
	// Exponent sets the power for CurvePolynomial or the steepness for CurveLogistic.
	Exponent float64
	// ShiftX shifts the input (the midpoint for CurveLogistic/CurveLogit or threshold for CurveStep).
	ShiftX float64
	// ShiftY offsets the output (or defines the low output below ShiftX for CurveStep).
	ShiftY float64
	// Evaluator supplies custom or resource-backed evaluation when Kind is CurveResource (or empty).
	Evaluator CurveEvaluator
}

// LinearCurve returns a linear response curve y = slope*(x - shiftX) + shiftY clamped to [0, 1].
func LinearCurve(slope, shiftX, shiftY float64) ResponseCurve {
	return ResponseCurve{Kind: CurveLinear, Slope: slope, ShiftX: shiftX, ShiftY: shiftY}
}

// PolynomialCurve returns a polynomial response curve y = slope*(x - shiftX)^exponent + shiftY clamped to [0, 1].
func PolynomialCurve(slope, exponent, shiftX, shiftY float64) ResponseCurve {
	return ResponseCurve{Kind: CurvePolynomial, Slope: slope, Exponent: exponent, ShiftX: shiftX, ShiftY: shiftY}
}

// LogisticCurve returns an S-shaped logistic response curve y = slope/(1 + exp(-steepness*(x - midpoint))) + shiftY clamped to [0, 1].
func LogisticCurve(slope, steepness, midpoint, shiftY float64) ResponseCurve {
	return ResponseCurve{Kind: CurveLogistic, Slope: slope, Exponent: steepness, ShiftX: midpoint, ShiftY: shiftY}
}

// LogitCurve returns a logit response curve y = slope*ln(u/(1 - u)) + shiftY where u = x - midpoint + 0.5, clamped to [0, 1].
func LogitCurve(slope, midpoint, shiftY float64) ResponseCurve {
	return ResponseCurve{Kind: CurveLogit, Slope: slope, ShiftX: midpoint, ShiftY: shiftY}
}

// StepCurve returns a threshold step response curve returning low for x < threshold and high for x >= threshold, clamped to [0, 1].
func StepCurve(threshold, low, high float64) ResponseCurve {
	return ResponseCurve{Kind: CurveStep, Slope: high - low, ShiftX: threshold, ShiftY: low}
}

// ResourceCurve returns a response curve backed by eval (for example a *resource.Curve).
func ResourceCurve(eval CurveEvaluator) ResponseCurve {
	return ResponseCurve{Kind: CurveResource, Evaluator: eval}
}

// CustomCurve returns a response curve backed by fn.
func CustomCurve(fn func(float64) float64) ResponseCurve {
	return ResponseCurve{Kind: CurveResource, Evaluator: CurveFunc(fn)}
}

// Eval evaluates c at x, clamping both the input and output to [0, 1].
// A NaN input or output yields 0.
func (c ResponseCurve) Eval(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	x = clamp01(x)
	switch c.Kind {
	case "":
		if c.Evaluator != nil {
			return clamp01(float64(c.Evaluator.Eval(float32(x))))
		}
		return x
	case CurveLinear:
		return clamp01(c.Slope*(x-c.ShiftX) + c.ShiftY)
	case CurvePolynomial:
		d := x - c.ShiftX
		var p float64
		switch {
		case c.Exponent == 0:
			p = 1
		case d == 0:
			if c.Exponent < 0 {
				if c.Slope >= 0 {
					return clamp01(1 + c.ShiftY)
				}
				return 0
			}
			p = 0
		case d < 0 && math.Trunc(c.Exponent) != c.Exponent:
			p = 0
		default:
			p = math.Pow(d, c.Exponent)
		}
		return clamp01(c.Slope*p + c.ShiftY)
	case CurveLogistic:
		y := c.Slope/(1+math.Exp(-c.Exponent*(x-c.ShiftX))) + c.ShiftY
		return clamp01(y)
	case CurveLogit:
		if c.Slope == 0 {
			return clamp01(c.ShiftY)
		}
		u := x - c.ShiftX + 0.5
		if u <= 0 {
			if c.Slope > 0 {
				return 0
			}
			return 1
		}
		if u >= 1 {
			if c.Slope > 0 {
				return 1
			}
			return 0
		}
		return clamp01(c.Slope*math.Log(u/(1-u)) + c.ShiftY)
	case CurveStep:
		if x < c.ShiftX {
			return clamp01(c.ShiftY)
		}
		return clamp01(c.ShiftY + c.Slope)
	case CurveResource:
		if c.Evaluator == nil {
			return 0
		}
		return clamp01(float64(c.Evaluator.Eval(float32(x))))
	default:
		return 0
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// Consideration turns one input reading from context C into a score in [0, 1] through Curve.
//
// When Max > Min, the raw input is linearly mapped from [Min, Max] into [0, 1] and clamped before applying Curve. Otherwise the input is clamped to [0, 1] directly.
type Consideration[C any] struct {
	// Name is an optional human-readable label.
	Name string
	// Input extracts the numeric value from context C. A nil Input evaluates to 0.
	Input func(C) float64
	// Min and Max optionally define the input range normalized into [0, 1].
	Min float64
	Max float64
	// Curve maps the normalized [0, 1] input to a utility score in [0, 1].
	Curve ResponseCurve
}

// Eval reads Input(ctx), normalizes it to [0, 1], and evaluates Curve.
func (c Consideration[C]) Eval(ctx C) float64 {
	if c.Input == nil {
		return 0
	}
	v := c.Input(ctx)
	if math.IsNaN(v) {
		return 0
	}
	if c.Max > c.Min {
		v = (v - c.Min) / (c.Max - c.Min)
	}
	return c.Curve.Eval(clamp01(v))
}

// UtilityAction is one candidate action scored by a Reasoner.
//
// Its score is the product of its Considerations adjusted by Dave Mark's compensation factor:
//
//	modFactor = 1 - 1/N
//	score = score + (1 - score)*modFactor*score
//
// multiplied by Weight (defaulting to 1 when Weight == 0; negative Weight disables the action), plus any momentum bonus when the action is currently active and its base score is positive.
type UtilityAction[C any] struct {
	// Name identifies the action (used to match behavior tree subtrees and YAML definitions).
	Name string
	// Weight scales the compensated consideration score. Zero defaults to 1; negative disables the action.
	Weight float64
	// Momentum is an optional per-action bonus added on top of Reasoner.Momentum while this action is active and scores > 0.
	Momentum float64
	// Considerations are the scoring axes for this action. Any consideration returning 0 vetoes the action (score 0).
	Considerations []Consideration[C]
}

// Score computes the action's utility score for ctx.
// When momentumBonus > 0 and the weighted compensated score is strictly positive, momentumBonus is added to resist rapid switching.
func (a *UtilityAction[C]) Score(ctx C, momentumBonus float64) float64 {
	if a == nil || math.IsNaN(a.Weight) || math.IsInf(a.Weight, 0) || a.Weight < 0 {
		return 0
	}
	weight := a.Weight
	if weight == 0 {
		weight = 1
	}
	n := len(a.Considerations)
	score := 1.0
	for i := range n {
		s := a.Considerations[i].Eval(ctx)
		if s <= 0 {
			return 0
		}
		score *= s
	}
	if n > 1 {
		modFactor := 1.0 - 1.0/float64(n)
		makeUp := (1.0 - score) * modFactor
		score += makeUp * score
	}
	score *= weight
	if score <= 0 {
		return 0
	}
	if momentumBonus > 0 && !math.IsNaN(momentumBonus) && !math.IsInf(momentumBonus, 0) {
		score += momentumBonus
	}
	return score
}

type scoredAction struct {
	index int
	score float64
}

// Reasoner scores a fixed set of UtilityActions and chooses either the highest-scoring action or a weighted random choice among the top TopN positive-scoring actions from a random.Stream.
//
// Ties always break in fixed action order (lowest index wins). After warm-up, Choose, ChooseWithCurrent, ScoreAction, and ScoreAll make zero heap allocations.
type Reasoner[C any] struct {
	// TopN chooses weighted-randomly among the top TopN positive-scoring actions when TopN > 1 and a non-nil *random.Stream is provided.
	// When TopN <= 1 or the stream is nil, the single highest-scoring action is picked deterministically.
	TopN int
	// Momentum is an optional bonus added to the currently active action's positive score to prevent oscillation between nearly equal actions.
	Momentum float64

	actions []UtilityAction[C]
	current int
	topBuf  []scoredAction
}

// NewReasoner constructs a Reasoner with defensive copies of actions and their consideration slices.
func NewReasoner[C any](actions ...UtilityAction[C]) *Reasoner[C] {
	cloned := make([]UtilityAction[C], len(actions))
	for i := range actions {
		cloned[i] = actions[i]
		cloned[i].Considerations = slices.Clone(actions[i].Considerations)
	}
	return &Reasoner[C]{
		actions: cloned,
		current: -1,
	}
}

// ActionCount returns the number of actions configured on r.
func (r *Reasoner[C]) ActionCount() int {
	if r == nil {
		return 0
	}
	return len(r.actions)
}

// ActionName returns the Name of action at index, or "" if index is out of bounds.
func (r *Reasoner[C]) ActionName(index int) string {
	if r == nil || index < 0 || index >= len(r.actions) {
		return ""
	}
	return r.actions[index].Name
}

// ActionIndex returns the index of the first action named name, or -1 if none matches.
func (r *Reasoner[C]) ActionIndex(name string) int {
	if r == nil {
		return -1
	}
	for i := range r.actions {
		if r.actions[i].Name == name {
			return i
		}
	}
	return -1
}

// Current returns the index of the currently active action tracked by Choose, or -1 if none is active.
func (r *Reasoner[C]) Current() int {
	if r == nil {
		return -1
	}
	return r.current
}

// SetCurrent sets the currently active action index tracked by Choose. Out-of-range values clear the active action to -1.
func (r *Reasoner[C]) SetCurrent(index int) {
	if r == nil {
		return
	}
	if index < 0 || index >= len(r.actions) {
		r.current = -1
		return
	}
	r.current = index
}

// Reset clears the currently active action tracked by Choose.
func (r *Reasoner[C]) Reset() {
	if r == nil {
		return
	}
	r.current = -1
}

func (r *Reasoner[C]) scoreAt(index int, ctx C, isCurrent bool) float64 {
	a := &r.actions[index]
	var bonus float64
	if isCurrent {
		if r.Momentum > 0 {
			bonus += r.Momentum
		}
		if a.Momentum > 0 {
			bonus += a.Momentum
		}
	}
	return a.Score(ctx, bonus)
}

// ScoreAction scores the action at index for ctx, applying momentum when isCurrent is true.
// Out-of-range indices return 0.
func (r *Reasoner[C]) ScoreAction(index int, ctx C, isCurrent bool) float64 {
	if r == nil || index < 0 || index >= len(r.actions) {
		return 0
	}
	return r.scoreAt(index, ctx, isCurrent)
}

// ScoreAll evaluates every action for ctx (applying momentum to action current) and writes the scores into dst, reusing dst's backing array when its capacity is at least ActionCount().
func (r *Reasoner[C]) ScoreAll(ctx C, current int, dst []float64) []float64 {
	if r == nil || len(r.actions) == 0 {
		return dst[:0]
	}
	n := len(r.actions)
	if cap(dst) < n {
		dst = make([]float64, n)
	} else {
		dst = dst[:n]
	}
	for i := range n {
		dst[i] = r.scoreAt(i, ctx, i == current)
	}
	return dst
}

// Choose scores all actions using r.Current() for momentum, selects the winning action, updates r.Current() to that action, and returns its index and score.
// Returns (-1, 0) if no action has a positive score.
func (r *Reasoner[C]) Choose(ctx C, rng *random.Stream) (int, float64) {
	if r == nil || len(r.actions) == 0 {
		return -1, 0
	}
	idx, score := r.ChooseWithCurrent(ctx, r.current, rng)
	r.current = idx
	return idx, score
}

// ChooseWithCurrent scores all actions using current as the active action index for momentum, without modifying r.Current().
// This lets one shared Reasoner score many agents without per-agent copies or heap allocations.
// Returns (-1, 0) if no action has a positive score.
func (r *Reasoner[C]) ChooseWithCurrent(ctx C, current int, rng *random.Stream) (int, float64) {
	if r == nil || len(r.actions) == 0 {
		return -1, 0
	}
	if r.TopN <= 1 || rng == nil {
		bestIdx := -1
		bestScore := 0.0
		for i := range r.actions {
			s := r.scoreAt(i, ctx, i == current)
			if s > bestScore {
				bestScore = s
				bestIdx = i
			}
		}
		return bestIdx, bestScore
	}

	k := min(r.TopN, len(r.actions))
	if cap(r.topBuf) < k {
		r.topBuf = make([]scoredAction, 0, k)
	}
	top := r.topBuf[:0]

	for i := range r.actions {
		s := r.scoreAt(i, ctx, i == current)
		if s <= 0 {
			continue
		}
		if len(top) == k && s <= top[k-1].score {
			continue
		}
		pos := len(top)
		for pos > 0 && s > top[pos-1].score {
			pos--
		}
		if len(top) < k {
			top = append(top, scoredAction{})
		}
		copy(top[pos+1:], top[pos:])
		top[pos] = scoredAction{index: i, score: s}
	}

	if len(top) == 0 {
		return -1, 0
	}
	if len(top) == 1 {
		return top[0].index, top[0].score
	}

	var total float64
	for i := range top {
		total += top[i].score
	}
	if total <= 0 {
		return -1, 0
	}
	target := rng.Float64() * total
	var acc float64
	for i := range top {
		acc += top[i].score
		if target < acc {
			return top[i].index, top[i].score
		}
	}
	last := top[len(top)-1]
	return last.index, last.score
}

// UtilitySelector creates a behavior tree Node that uses reasoner to pick an action on each Tick and runs that action's bound subtree from subtrees (keyed by UtilityAction.Name).
//
// Each UtilitySelector tracks its own active action for momentum, so multiple agents can share one Reasoner safely. When a different action beats a currently running subtree, the interrupted subtree is Reset before the newly chosen subtree is ticked. If no action scores above 0 or the chosen action has no subtree, Tick returns StatusFailure.
func UtilitySelector[C any](reasoner *Reasoner[C], ctx func() C, rng *random.Stream, subtrees map[string]Node) Node {
	var resolved []Node
	if reasoner != nil {
		resolved = make([]Node, len(reasoner.actions))
		for i := range reasoner.actions {
			resolved[i] = subtrees[reasoner.actions[i].Name]
		}
	}
	return &utilitySelectorNode[C]{
		reasoner: reasoner,
		ctx:      ctx,
		rng:      rng,
		subtrees: resolved,
		active:   -1,
		running:  -1,
	}
}

type utilitySelectorNode[C any] struct {
	reasoner *Reasoner[C]
	ctx      func() C
	rng      *random.Stream
	subtrees []Node
	active   int
	running  int
}

func (u *utilitySelectorNode[C]) Tick(dt time.Duration) Status {
	if u.reasoner == nil || u.ctx == nil || len(u.subtrees) == 0 {
		return StatusFailure
	}
	idx, _ := u.reasoner.ChooseWithCurrent(u.ctx(), u.active, u.rng)
	if idx < 0 || idx >= len(u.subtrees) {
		if u.running >= 0 && u.subtrees[u.running] != nil {
			u.subtrees[u.running].Reset()
		}
		u.running = -1
		u.active = -1
		return StatusFailure
	}
	if u.running >= 0 && u.running != idx {
		if prev := u.subtrees[u.running]; prev != nil {
			prev.Reset()
		}
		u.running = -1
	}
	ch := u.subtrees[idx]
	if ch == nil {
		u.active = -1
		return StatusFailure
	}
	st := ch.Tick(dt)
	switch st {
	case StatusRunning:
		u.active = idx
		u.running = idx
	case StatusSuccess:
		u.active = idx
		u.running = -1
	default:
		u.active = -1
		u.running = -1
	}
	return st
}

func (u *utilitySelectorNode[C]) Reset() {
	u.active = -1
	u.running = -1
	for _, ch := range u.subtrees {
		if ch != nil {
			ch.Reset()
		}
	}
}

func (u *utilitySelectorNode[C]) btUtilityChildren() []Node {
	return u.subtrees
}

func (u *utilitySelectorNode[C]) btUtilityState() (active, running int) {
	return u.active, u.running
}

func (u *utilitySelectorNode[C]) btRestoreUtilityState(active, running int) {
	u.active = active
	u.running = running
}
