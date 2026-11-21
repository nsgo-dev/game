package physics

// Trigger is a non-solid sensor region that reports bodies entering, staying inside,
// and exiting its Shape across World.StepTriggers calls.
//
// Layer and Mask follow the same rules as Body: zero values default to LayerAll,
// and a body interacts with a trigger when both masks accept the other's layer.
// On the first step a matching body overlaps Shape, OnEnter is called once.
// On each subsequent step it continues overlapping, OnStay is called.
// On the first step it no longer overlaps (or has been removed from World.Bodies),
// OnExit is called once.
type Trigger struct {
	ID       int
	Tag      string
	Shape    Shape
	Layer    LayerMask
	Mask     LayerMask
	OnEnter  func(*Body)
	OnStay   func(*Body)
	OnExit   func(*Body)
	UserData any
}

type triggerEvent struct {
	fn   func(*Body)
	body *Body
}

// AddTrigger appends non-nil triggers to w.Triggers.
func (w *World) AddTrigger(triggers ...*Trigger) {
	for _, tr := range triggers {
		if tr != nil {
			w.Triggers = append(w.Triggers, tr)
		}
	}
}

// RemoveTrigger deletes the first occurrence of tr from w.Triggers, preserving the order
// of remaining triggers, and clears its tracked overlap state.
func (w *World) RemoveTrigger(tr *Trigger) bool {
	for i, existing := range w.Triggers {
		if existing == tr {
			w.Triggers = append(w.Triggers[:i], w.Triggers[i+1:]...)
			if w.triggerActive != nil {
				delete(w.triggerActive, tr)
			}
			return true
		}
	}
	return false
}

// StepTriggers evaluates every trigger in w.Triggers against w.Bodies in deterministic order
// and dispatches OnEnter, OnStay, and OnExit callbacks for changed or continuing overlaps.
// Removing a body from w.Bodies causes OnExit to fire on the next StepTriggers call.
func (w *World) StepTriggers() {
	if len(w.Triggers) == 0 {
		clear(w.triggerActive)
		return
	}
	if w.triggerActive == nil {
		w.triggerActive = make(map[*Trigger][]*Body, len(w.Triggers))
	}

	// Track which triggers remain registered so state for manually removed triggers is pruned.
	present := make(map[*Trigger]struct{}, len(w.Triggers))
	var events []triggerEvent

	for _, tr := range w.Triggers {
		if tr == nil {
			continue
		}
		present[tr] = struct{}{}

		prev := w.triggerActive[tr]
		prevSet := make(map[*Body]struct{}, len(prev))
		for _, b := range prev {
			prevSet[b] = struct{}{}
		}

		var curr []*Body
		currSet := make(map[*Body]struct{})
		if !tr.Shape.Empty() {
			for _, b := range w.Bodies {
				if b == nil || b.Shape.Empty() {
					continue
				}
				if _, dup := currSet[b]; dup {
					continue
				}
				if !layersInteract(tr.Layer, tr.Mask, b.Layer, b.Mask) {
					continue
				}
				if !TestShapes(tr.Shape, b.Shape).Hit {
					continue
				}
				currSet[b] = struct{}{}
				curr = append(curr, b)
				if _, was := prevSet[b]; was {
					if tr.OnStay != nil {
						events = append(events, triggerEvent{fn: tr.OnStay, body: b})
					}
				} else {
					if tr.OnEnter != nil {
						events = append(events, triggerEvent{fn: tr.OnEnter, body: b})
					}
				}
			}
		}

		for _, b := range prev {
			if _, still := currSet[b]; !still {
				if tr.OnExit != nil {
					events = append(events, triggerEvent{fn: tr.OnExit, body: b})
				}
			}
		}

		if len(curr) == 0 {
			delete(w.triggerActive, tr)
		} else {
			w.triggerActive[tr] = curr
		}
	}

	for tr := range w.triggerActive {
		if _, ok := present[tr]; !ok {
			delete(w.triggerActive, tr)
		}
	}

	for _, ev := range events {
		ev.fn(ev.body)
	}
}
