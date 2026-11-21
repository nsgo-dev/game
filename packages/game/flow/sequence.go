package flow

import "time"

// Step is one unit of work in a sequence, advanced deterministically by Update without goroutines.
//
// Update advances the step by up to dt and returns any unused time along with whether the step has finished, so a Seq can pass leftover time to the next step on the same tick. Cancel stops an unfinished step cleanly when its Handle or an enclosing Race cancels it.
type Step interface {
	Update(dt time.Duration) (leftover time.Duration, done bool)
	Cancel()
}

// Runner drives one or more active Steps on each game step. The zero value is ready to use.
type Runner struct {
	handles []*Handle
	depth   int
}

// Handle represents a running Step started on a Runner.
type Handle struct {
	step Step
	done bool
}

// Start schedules step to run on subsequent Update calls and returns a Handle to observe or cancel it. A nil step returns an already-done Handle.
func (r *Runner) Start(step Step) *Handle {
	if step == nil {
		return &Handle{done: true}
	}
	h := &Handle{step: step}
	r.handles = append(r.handles, h)
	return h
}

// Clear cancels every active Handle on the Runner.
func (r *Runner) Clear() {
	for _, h := range r.handles {
		h.Cancel()
	}
	if r.depth == 0 {
		clear(r.handles)
		r.handles = r.handles[:0]
	}
}

// Update advances all active steps by dt in start order. Steps started from inside a callback begin on the next Update step.
func (r *Runner) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	r.depth++
	defer func() {
		r.depth--
		if r.depth == 0 {
			r.compact()
		}
	}()

	n := len(r.handles)
	for i := 0; i < n; i++ {
		h := r.handles[i]
		if h.done {
			continue
		}
		_, done := h.step.Update(dt)
		if done {
			h.done = true
			h.step = nil
		}
	}
}

func (r *Runner) compact() {
	w := 0
	for _, h := range r.handles {
		if !h.done {
			r.handles[w] = h
			w++
		}
	}
	clear(r.handles[w:])
	r.handles = r.handles[:w]
}

// Cancel stops the step so it will not run further. Cancelling a nil or finished Handle is a no-op.
func (h *Handle) Cancel() {
	if h == nil || h.done {
		return
	}
	h.done = true
	st := h.step
	h.step = nil
	if st != nil {
		st.Cancel()
	}
}

// Done reports whether the step has finished or been cancelled.
func (h *Handle) Done() bool {
	return h == nil || h.done
}

// Wait returns a Step that completes after d of game time has elapsed.
func Wait(d time.Duration) Step {
	return &waitStep{remaining: max(0, d)}
}

type waitStep struct {
	remaining time.Duration
	done      bool
}

func (s *waitStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	if dt < 0 {
		dt = 0
	}
	if s.remaining <= dt {
		leftover := dt - s.remaining
		s.remaining = 0
		s.done = true
		return leftover, true
	}
	s.remaining -= dt
	return 0, false
}

func (s *waitStep) Cancel() {
	s.done = true
	s.remaining = 0
}

// Do returns a Step that calls fn once and completes immediately, passing all remaining dt to the next step.
func Do(fn func()) Step {
	return &doStep{fn: fn}
}

type doStep struct {
	fn   func()
	done bool
}

func (s *doStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	s.done = true
	fn := s.fn
	s.fn = nil
	if fn != nil {
		fn()
	}
	return dt, true
}

func (s *doStep) Cancel() {
	s.done = true
	s.fn = nil
}

// WaitUntil returns a Step that completes on the first Update where cond returns true (or immediately if cond is nil).
func WaitUntil(cond func() bool) Step {
	return &waitUntilStep{cond: cond}
}

type waitUntilStep struct {
	cond   func() bool
	waited bool
	done   bool
}

func (s *waitUntilStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	if s.cond == nil || s.cond() {
		s.done = true
		s.cond = nil
		if s.waited {
			return 0, true
		}
		return dt, true
	}
	s.waited = true
	return 0, false
}

func (s *waitUntilStep) Cancel() {
	s.done = true
	s.cond = nil
}

// While returns a Step that runs update(dt) on each step while cond returns true, completing as soon as cond returns false.
func While(cond func() bool, update func(dt time.Duration)) Step {
	return &whileStep{cond: cond, update: update}
}

type whileStep struct {
	cond   func() bool
	update func(dt time.Duration)
	ran    bool
	done   bool
}

func (s *whileStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	if s.cond == nil || !s.cond() {
		s.done = true
		s.cond = nil
		s.update = nil
		if s.ran {
			return 0, true
		}
		return dt, true
	}
	if dt > 0 {
		s.ran = true
		if s.update != nil {
			s.update(dt)
		}
		if s.done {
			return 0, true
		}
		if s.cond == nil || !s.cond() {
			s.done = true
			s.cond = nil
			s.update = nil
			return 0, true
		}
	}
	return 0, false
}

func (s *whileStep) Cancel() {
	s.done = true
	s.cond = nil
	s.update = nil
}

// Seq returns a Step that runs steps in order, carrying any leftover dt from a completed step into the next one on the same tick. Nil entries are skipped.
func Seq(steps ...Step) Step {
	return &seqStep{steps: append([]Step(nil), steps...)}
}

type seqStep struct {
	steps []Step
	index int
	done  bool
}

func (s *seqStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	for s.index < len(s.steps) {
		cur := s.steps[s.index]
		if cur == nil {
			s.index++
			continue
		}
		rem, done := cur.Update(dt)
		if s.done {
			return 0, true
		}
		if !done {
			return 0, false
		}
		s.steps[s.index] = nil
		s.index++
		dt = rem
	}
	s.done = true
	return dt, true
}

func (s *seqStep) Cancel() {
	if s.done {
		return
	}
	s.done = true
	for i := s.index; i < len(s.steps); i++ {
		if s.steps[i] != nil {
			s.steps[i].Cancel()
			s.steps[i] = nil
		}
	}
	s.index = len(s.steps)
}

// Parallel returns a Step that updates every branch on each step until all branches have completed.
func Parallel(steps ...Step) Step {
	return &parallelStep{steps: append([]Step(nil), steps...)}
}

type parallelStep struct {
	steps []Step
	done  bool
}

func (s *parallelStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	remaining := 0
	minLeftover := dt
	finishedAny := false
	for i := range s.steps {
		st := s.steps[i]
		if st == nil {
			continue
		}
		rem, done := st.Update(dt)
		if s.done {
			return 0, true
		}
		if done {
			s.steps[i] = nil
			if !finishedAny || rem < minLeftover {
				minLeftover = rem
				finishedAny = true
			}
		} else {
			remaining++
		}
	}
	if remaining == 0 {
		s.done = true
		return minLeftover, true
	}
	return 0, false
}

func (s *parallelStep) Cancel() {
	if s.done {
		return
	}
	s.done = true
	for i, st := range s.steps {
		if st != nil {
			st.Cancel()
			s.steps[i] = nil
		}
	}
}

// Race returns a Step that updates branches in order until the first branch completes, then cancels the remaining branches.
func Race(steps ...Step) Step {
	var nonNil []Step
	for _, st := range steps {
		if st != nil {
			nonNil = append(nonNil, st)
		}
	}
	return &raceStep{steps: nonNil, done: len(nonNil) == 0}
}

type raceStep struct {
	steps []Step
	done  bool
}

func (s *raceStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	for i, st := range s.steps {
		rem, done := st.Update(dt)
		if s.done {
			return 0, true
		}
		if done {
			s.done = true
			for j, other := range s.steps {
				if j != i && other != nil {
					other.Cancel()
				}
				s.steps[j] = nil
			}
			return rem, true
		}
	}
	return 0, false
}

func (s *raceStep) Cancel() {
	if s.done {
		return
	}
	s.done = true
	for i, st := range s.steps {
		if st != nil {
			st.Cancel()
			s.steps[i] = nil
		}
	}
}

// Repeat returns a Step that runs the Step returned by makeStep n times in sequence. A non-positive n or nil makeStep completes immediately.
func Repeat(n int, makeStep func() Step) Step {
	if n <= 0 || makeStep == nil {
		return &repeatStep{done: true}
	}
	return &repeatStep{remaining: n, makeStep: makeStep}
}

// Loop returns a Step that repeats the Step returned by makeStep until cancelled. A zero-duration body runs once per Update step rather than spinning indefinitely.
func Loop(makeStep func() Step) Step {
	if makeStep == nil {
		return &repeatStep{done: true}
	}
	return &repeatStep{infinite: true, makeStep: makeStep}
}

type repeatStep struct {
	remaining int
	infinite  bool
	makeStep  func() Step
	cur       Step
	done      bool
}

func (s *repeatStep) Update(dt time.Duration) (time.Duration, bool) {
	if s.done {
		return dt, true
	}
	for {
		if s.cur == nil {
			if !s.infinite && s.remaining <= 0 {
				s.done = true
				s.makeStep = nil
				return dt, true
			}
			s.cur = s.makeStep()
			if !s.infinite {
				s.remaining--
			}
			if s.cur == nil {
				if s.infinite {
					return 0, false
				}
				continue
			}
		}
		rem, done := s.cur.Update(dt)
		if s.done {
			return 0, true
		}
		if !done {
			return 0, false
		}
		s.cur = nil
		if !s.infinite && s.remaining <= 0 {
			s.done = true
			s.makeStep = nil
			return rem, true
		}
		if s.infinite && rem == dt {
			return 0, false
		}
		dt = rem
	}
}

func (s *repeatStep) Cancel() {
	if s.done {
		return
	}
	s.done = true
	s.makeStep = nil
	if s.cur != nil {
		s.cur.Cancel()
		s.cur = nil
	}
}
