package input

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// GestureKind identifies a touch gesture recognized by GestureRecognizer.
type GestureKind uint8

const (
	GestureNone GestureKind = iota
	GestureTap
	GestureDoubleTap
	GestureLongPress
	GestureSwipeLeft
	GestureSwipeRight
	GestureSwipeUp
	GestureSwipeDown
	GesturePinchIn
	GesturePinchOut
)

// String returns a human-readable name for g.
func (g GestureKind) String() string {
	switch g {
	case GestureNone:
		return "none"
	case GestureTap:
		return "tap"
	case GestureDoubleTap:
		return "double_tap"
	case GestureLongPress:
		return "long_press"
	case GestureSwipeLeft:
		return "swipe_left"
	case GestureSwipeRight:
		return "swipe_right"
	case GestureSwipeUp:
		return "swipe_up"
	case GestureSwipeDown:
		return "swipe_down"
	case GesturePinchIn:
		return "pinch_in"
	case GesturePinchOut:
		return "pinch_out"
	default:
		return "unknown"
	}
}

// GestureConfig tunes the thresholds used by GestureRecognizer.
//
// Any non-positive field falls back to DefaultGestureConfig.
type GestureConfig struct {
	// TapMaxDuration is the maximum press duration for GestureTap.
	TapMaxDuration time.Duration
	// TapMaxDistance is the maximum finger drift in points allowed for GestureTap and GestureLongPress.
	TapMaxDistance float32
	// DoubleTapMaxInterval is the maximum elapsed time between two taps to recognize GestureDoubleTap.
	DoubleTapMaxInterval time.Duration
	// DoubleTapMaxDistance is the maximum distance in points between two taps to recognize GestureDoubleTap.
	DoubleTapMaxDistance float32
	// LongPressDuration is how long a stationary finger must be held to fire GestureLongPress.
	LongPressDuration time.Duration
	// SwipeMinDistance is the minimum drag distance in points for a directional swipe.
	SwipeMinDistance float32
	// SwipeMinVelocity is the minimum drag speed in points per second for a directional swipe.
	SwipeMinVelocity float32
	// PinchMinDelta is the minimum change in two-finger spacing in points to fire GesturePinchIn or GesturePinchOut.
	PinchMinDelta float32
}

// DefaultGestureConfig returns balanced mobile/tablet thresholds for GestureRecognizer.
func DefaultGestureConfig() GestureConfig {
	return GestureConfig{
		TapMaxDuration:       250 * time.Millisecond,
		TapMaxDistance:       18,
		DoubleTapMaxInterval: 300 * time.Millisecond,
		DoubleTapMaxDistance: 36,
		LongPressDuration:    500 * time.Millisecond,
		SwipeMinDistance:     40,
		SwipeMinVelocity:     150,
		PinchMinDelta:        10,
	}
}

func (c GestureConfig) resolved() GestureConfig {
	def := DefaultGestureConfig()
	if c.TapMaxDuration <= 0 {
		c.TapMaxDuration = def.TapMaxDuration
	}
	if c.TapMaxDistance <= 0 {
		c.TapMaxDistance = def.TapMaxDistance
	}
	if c.DoubleTapMaxInterval <= 0 {
		c.DoubleTapMaxInterval = def.DoubleTapMaxInterval
	}
	if c.DoubleTapMaxDistance <= 0 {
		c.DoubleTapMaxDistance = def.DoubleTapMaxDistance
	}
	if c.LongPressDuration <= 0 {
		c.LongPressDuration = def.LongPressDuration
	}
	if c.SwipeMinDistance <= 0 {
		c.SwipeMinDistance = def.SwipeMinDistance
	}
	if c.SwipeMinVelocity <= 0 {
		c.SwipeMinVelocity = def.SwipeMinVelocity
	}
	if c.PinchMinDelta <= 0 {
		c.PinchMinDelta = def.PinchMinDelta
	}
	return c
}

// GestureEvent describes a single gesture recognized on a simulation step.
type GestureEvent struct {
	Kind     GestureKind
	Position core.Point
	// Scale is the current pinch ratio (currentDistance / initialDistance) for GesturePinchIn and GesturePinchOut, and 0 for non-pinch gestures.
	Scale float64
}

type gestureTouch struct {
	id          int
	startPos    core.Point
	pos         core.Point
	startTime   time.Duration
	maxDist     float32
	longPressed bool
	usedInPinch bool
}

// GestureRecognizer deterministically recognizes taps, double-taps, long-presses, four-direction swipes, and two-finger pinches from core.TouchEvent streams stepped by game.Loop's Update(dt).
//
// Call OnTouch from the view (or Attach to wire it) and call Update(dt) once per game.Loop.Update step before Actions.Step so gesture-bound actions trigger on that exact step.
//
// The zero value is ready to use.
type GestureRecognizer struct {
	// Config overrides default gesture thresholds. Zero fields use DefaultGestureConfig.
	Config GestureConfig
	// MapPoint, when set, transforms touch points from view coordinates before gesture recognition. Returning false ignores the point.
	MapPoint func(core.Point) (core.Point, bool)
	// OnGesture, when set, runs during Update for each gesture recognized on that step.
	OnGesture func(GestureEvent)

	actions  *Actions
	bindings map[GestureKind][]string

	now     time.Duration
	touches map[int]*gestureTouch

	hasLastTap  bool
	lastTapTime time.Duration
	lastTapPos  core.Point

	pinchActive    bool
	pinchStartDist float32
	pinchRefDist   float32
	pinchScale     float64
	stepPinchScale float64

	pending []GestureEvent
	current []GestureEvent
}

// NewGestureRecognizer returns a GestureRecognizer optionally wired to trigger actions on a.
func NewGestureRecognizer(a *Actions) *GestureRecognizer {
	return &GestureRecognizer{actions: a}
}

// BindActions wires g to trigger bound actions on a (or disconnects when a is nil).
func (g *GestureRecognizer) BindActions(a *Actions) {
	g.actions = a
}

// BindGesture binds action to gesture kind. When kind is recognized, g triggers action on its bound Actions. Binding GestureNone removes action's gesture bindings.
func (g *GestureRecognizer) BindGesture(action string, kind GestureKind) {
	if action == "" {
		return
	}
	if kind == GestureNone {
		g.UnbindGesture(action)
		return
	}
	if g.bindings == nil {
		g.bindings = make(map[GestureKind][]string)
	}
	if !slices.Contains(g.bindings[kind], action) {
		g.bindings[kind] = append(g.bindings[kind], action)
	}
}

// UnbindGesture removes all gesture bindings for action.
func (g *GestureRecognizer) UnbindGesture(action string) {
	for kind, list := range g.bindings {
		if i := slices.Index(list, action); i >= 0 {
			list = slices.Delete(list, i, i+1)
			if len(list) == 0 {
				delete(g.bindings, kind)
			} else {
				g.bindings[kind] = list
			}
		}
	}
}

// Triggered reports whether kind fired on the most recent Update step.
func (g *GestureRecognizer) Triggered(kind GestureKind) bool {
	for _, ev := range g.current {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// Events returns a copy of all gestures recognized on the most recent Update step.
func (g *GestureRecognizer) Events() []GestureEvent {
	if len(g.current) == 0 {
		return nil
	}
	return slices.Clone(g.current)
}

// PinchScale returns the current two-finger pinch scale ratio (currentDistance / initialDistance), or 1 when no pinch is active.
func (g *GestureRecognizer) PinchScale() float64 {
	if g.pinchActive && g.pinchScale > 0 {
		return g.pinchScale
	}
	if g.stepPinchScale > 0 {
		return g.stepPinchScale
	}
	return 1
}

// Reset clears active touches, pinch state, pending tap history, and the current step's events while keeping Config, MapPoint, OnGesture, and gesture bindings.
func (g *GestureRecognizer) Reset() {
	clear(g.touches)
	g.hasLastTap = false
	g.pinchActive = false
	g.pinchStartDist = 0
	g.pinchRefDist = 0
	g.pinchScale = 0
	g.stepPinchScale = 0
	g.pending = g.pending[:0]
	g.current = g.current[:0]
}

// Attach wires g.OnTouch onto v, chaining any existing v.OnTouch callback.
func (g *GestureRecognizer) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { g.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = g.OnTouch
	}
}

// Update advances the deterministic gesture clock by dt, evaluates held long-presses, promotes pending touch gestures into the current step, and triggers any bound Actions.
func (g *GestureRecognizer) Update(dt time.Duration) {
	if dt > 0 {
		g.now += dt
	}
	cfg := g.Config.resolved()

	if g.hasLastTap && g.now-g.lastTapTime > cfg.DoubleTapMaxInterval {
		g.hasLastTap = false
	}

	g.current = append(g.current[:0], g.pending...)
	g.pending = g.pending[:0]

	g.stepPinchScale = 0
	for _, ev := range g.current {
		if ev.Kind == GesturePinchIn || ev.Kind == GesturePinchOut {
			g.stepPinchScale = ev.Scale
		}
	}

	if len(g.touches) == 1 {
		for _, t := range g.touches {
			if !t.usedInPinch && !t.longPressed && t.maxDist <= cfg.TapMaxDistance && g.now-t.startTime >= cfg.LongPressDuration {
				t.longPressed = true
				g.hasLastTap = false
				ev := GestureEvent{Kind: GestureLongPress, Position: t.pos}
				g.current = append(g.current, ev)
				g.triggerBound(GestureLongPress)
			}
		}
	}

	if g.OnGesture != nil {
		for _, ev := range g.current {
			g.OnGesture(ev)
		}
	}
}

// OnTouch records touch activity for gesture recognition.
func (g *GestureRecognizer) OnTouch(ev core.TouchEvent) {
	cfg := g.Config.resolved()

	switch ev.Phase {
	case core.TouchStart:
		if g.touches == nil && len(ev.Touches) > 0 {
			g.touches = make(map[int]*gestureTouch, len(ev.Touches))
		}
		for _, t := range ev.Touches {
			pos, ok := g.mapPoint(t.Position)
			if !ok {
				continue
			}
			if existing, exists := g.touches[t.ID]; exists {
				existing.pos = pos
				if d := pointDist(existing.startPos, pos); d > existing.maxDist {
					existing.maxDist = d
				}
				continue
			}
			g.touches[t.ID] = &gestureTouch{
				id:        t.ID,
				startPos:  pos,
				pos:       pos,
				startTime: g.now,
			}
		}
		g.syncPinchStart()

	case core.TouchMove:
		for _, t := range ev.Touches {
			existing, ok := g.touches[t.ID]
			if !ok {
				continue
			}
			pos, mapped := g.mapPoint(t.Position)
			if !mapped {
				continue
			}
			existing.pos = pos
			if d := pointDist(existing.startPos, pos); d > existing.maxDist {
				existing.maxDist = d
			}
		}
		g.checkPinchMove(cfg)

	case core.TouchEnd:
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			existing, ok := g.touches[t.ID]
			if !ok {
				continue
			}
			if pos, mapped := g.mapPoint(t.Position); mapped {
				existing.pos = pos
				if d := pointDist(existing.startPos, pos); d > existing.maxDist {
					existing.maxDist = d
				}
			}
			if !t.Changed && hasChanged {
				continue
			}
			delete(g.touches, t.ID)
			if !existing.usedInPinch && !existing.longPressed {
				g.classifyRelease(existing, cfg)
			}
		}
		if len(g.touches) < 2 {
			g.pinchActive = false
		}

	case core.TouchCancel:
		if len(ev.Touches) == 0 {
			clear(g.touches)
			g.pinchActive = false
			return
		}
		hasChanged := touchesHaveChanged(ev.Touches)
		for _, t := range ev.Touches {
			if t.Changed || !hasChanged {
				delete(g.touches, t.ID)
			}
		}
		if len(g.touches) < 2 {
			g.pinchActive = false
		}
	}
}

func (g *GestureRecognizer) syncPinchStart() {
	if len(g.touches) >= 2 {
		for _, t := range g.touches {
			t.usedInPinch = true
		}
		g.hasLastTap = false
	}
	if len(g.touches) == 2 {
		if !g.pinchActive {
			a, b := g.twoTouches()
			dist := pointDist(a.pos, b.pos)
			g.pinchActive = true
			g.pinchStartDist = dist
			g.pinchRefDist = dist
			g.pinchScale = 1
		}
	} else {
		g.pinchActive = false
	}
}

func (g *GestureRecognizer) checkPinchMove(cfg GestureConfig) {
	if !g.pinchActive || len(g.touches) != 2 {
		return
	}
	a, b := g.twoTouches()
	dist := pointDist(a.pos, b.pos)
	if g.pinchStartDist > 0 {
		g.pinchScale = float64(dist) / float64(g.pinchStartDist)
	} else {
		g.pinchScale = 1
	}

	delta := dist - g.pinchRefDist
	if math.Abs(float64(delta)) < float64(cfg.PinchMinDelta) {
		return
	}
	g.pinchRefDist = dist
	mid := core.Point{
		X: (a.pos.X + b.pos.X) / 2,
		Y: (a.pos.Y + b.pos.Y) / 2,
	}
	kind := GesturePinchOut
	if delta < 0 {
		kind = GesturePinchIn
	}
	g.emitPending(GestureEvent{Kind: kind, Position: mid, Scale: g.pinchScale})
}

func (g *GestureRecognizer) classifyRelease(t *gestureTouch, cfg GestureConfig) {
	dur := g.now - t.startTime
	if dur <= cfg.TapMaxDuration && t.maxDist <= cfg.TapMaxDistance {
		g.emitPending(GestureEvent{Kind: GestureTap, Position: t.pos})
		if g.hasLastTap && g.now-g.lastTapTime <= cfg.DoubleTapMaxInterval && pointDist(g.lastTapPos, t.pos) <= cfg.DoubleTapMaxDistance {
			g.hasLastTap = false
			g.emitPending(GestureEvent{Kind: GestureDoubleTap, Position: t.pos})
		} else {
			g.hasLastTap = true
			g.lastTapTime = g.now
			g.lastTapPos = t.pos
		}
		return
	}

	dx := float32(t.pos.X - t.startPos.X)
	dy := float32(t.pos.Y - t.startPos.Y)
	dist := float32(math.Hypot(float64(dx), float64(dy)))
	if dist < cfg.SwipeMinDistance {
		g.hasLastTap = false
		return
	}

	if dur > 0 {
		vel := dist / float32(dur.Seconds())
		if vel < cfg.SwipeMinVelocity {
			g.hasLastTap = false
			return
		}
	}

	var kind GestureKind
	if math.Abs(float64(dx)) >= math.Abs(float64(dy)) {
		if dx > 0 {
			kind = GestureSwipeRight
		} else {
			kind = GestureSwipeLeft
		}
	} else {
		if dy > 0 {
			kind = GestureSwipeDown
		} else {
			kind = GestureSwipeUp
		}
	}
	g.hasLastTap = false
	g.emitPending(GestureEvent{Kind: kind, Position: t.pos})
}

func (g *GestureRecognizer) emitPending(ev GestureEvent) {
	g.pending = append(g.pending, ev)
	g.triggerBound(ev.Kind)
}

func (g *GestureRecognizer) triggerBound(kind GestureKind) {
	if g.actions == nil {
		return
	}
	for _, action := range g.bindings[kind] {
		g.actions.Trigger(action)
	}
}

func (g *GestureRecognizer) twoTouches() (*gestureTouch, *gestureTouch) {
	var a, b *gestureTouch
	for _, t := range g.touches {
		if a == nil {
			a = t
		} else {
			b = t
			break
		}
	}
	return a, b
}

func (g *GestureRecognizer) mapPoint(p core.Point) (core.Point, bool) {
	if g.MapPoint == nil {
		return p, true
	}
	return g.MapPoint(p)
}
