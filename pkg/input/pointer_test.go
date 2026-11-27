package input_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type lookStep struct {
	dx, dy     float64
	yaw, pitch float64
	axis       float64
	requested  bool
}

type lookHarness struct {
	app     *nsgotest.App
	actions *input.Actions
	steps   []lookStep
	keys    []string
	locks   []bool
}

// startLook mounts a game view that locks the pointer when pressed and records each step's look values.
func startLook(t *testing.T, env core.Environment) *lookHarness {
	t.Helper()
	h := &lookHarness{actions: input.NewActions()}
	h.actions.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.5})
	h.actions.BindPointerAxis("pitch", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.25, Invert: true})
	h.actions.BindPointerAxis("steer", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.1})
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var ref core.Ref
		v := core.View{
			Ref:    &ref,
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			OnPress: func(core.PressEvent) {
				h.locks = append(h.locks, h.actions.LockPointer(cx, &ref))
			},
			OnKeyDown: func(ev core.KeyEvent) bool {
				h.keys = append(h.keys, ev.Key)
				return false
			},
		}
		h.actions.Attach(&v)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				h.actions.Step()
				dx, dy := h.actions.PointerDelta()
				h.steps = append(h.steps, lookStep{
					dx: dx, dy: dy,
					yaw:       h.actions.Delta("yaw"),
					pitch:     h.actions.Delta("pitch"),
					axis:      h.actions.Axis("steer"),
					requested: h.actions.PointerLockRequested(),
				})
			},
		}
		loop.Run(cx)
		return v
	}, nsgotest.Size(400, 300), nsgotest.Environment(env))
	return h
}

func (h *lookHarness) step() lookStep {
	before := len(h.steps)
	for len(h.steps) == before {
		h.app.Advance(frameStep)
	}
	return h.steps[len(h.steps)-1]
}

func desktopEnv(canLock bool) core.Environment {
	return core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, CanLockPointer: canLock}
}

func TestPointerLockFeedsLookAxesOncePerStep(t *testing.T) {
	h := startLook(t, desktopEnv(true))
	h.step()

	// Motion before the lock never reaches the game.
	h.app.PointerDelta(30, 30)
	if got := h.step(); got != (lookStep{}) {
		t.Fatalf("before the lock: got %+v, want nothing", got)
	}

	h.app.Press("id:game")
	if len(h.locks) != 1 || !h.locks[0] {
		t.Fatalf("LockPointer results = %v, want [true]", h.locks)
	}
	h.app.PointerDelta(4, -2)
	h.app.PointerDelta(6, 10)
	got := h.step()
	want := lookStep{dx: 10, dy: 8, yaw: 5, pitch: -2, axis: 1, requested: true}
	if got != want {
		t.Fatalf("locked step: got %+v, want %+v", got, want)
	}

	// The next step without motion reads nothing, and a small motion stays inside the clamp.
	if got := h.step(); got != (lookStep{requested: true}) {
		t.Fatalf("still step: got %+v", got)
	}
	h.app.PointerDelta(-3, 0)
	got = h.step()
	if got.yaw != -1.5 || math.Abs(got.axis-(-0.3)) > 1e-12 {
		t.Fatalf("small motion: got %+v", got)
	}
}

func TestUnlockPointerRestoresThePointer(t *testing.T) {
	h := startLook(t, desktopEnv(true))
	h.app.Press("id:game")
	h.app.PointerDelta(2, 2)
	h.app.Post(h.actions.UnlockPointer)
	h.app.Settle()
	got := h.step()
	if got.dx != 2 || got.requested {
		t.Fatalf("motion before the release: got %+v, want dx 2 and no request", got)
	}

	// The platform no longer holds a lock: motion does not arrive and Escape reaches the focused view.
	h.app.PointerDelta(9, 9)
	if got := h.step(); got != (lookStep{}) {
		t.Fatalf("after the release: got %+v, want nothing", got)
	}
	h.app.Focus("id:game")
	h.app.Key("Escape")
	if len(h.keys) != 1 || h.keys[0] != "escape" {
		t.Fatalf("keys after the release = %q, want [escape]", h.keys)
	}

	// Locking again resumes the look, and while the game holds the lock the platform takes Escape to release it, so the view never hears the key.
	h.app.Press("id:game")
	h.app.PointerDelta(1, 0)
	if got := h.step(); got.dx != 1 || !got.requested {
		t.Fatalf("relocked: got %+v", got)
	}
	h.app.Focus("id:game")
	h.app.Key("Escape")
	if len(h.keys) != 1 {
		t.Fatalf("a held lock let Escape through: %q", h.keys)
	}
	// NSGo does not tell the app that Escape released the lock, so the request stays while no motion arrives.
	h.app.PointerDelta(5, 5)
	if got := h.step(); got != (lookStep{requested: true}) {
		t.Fatalf("motion after Escape: got %+v, want no motion and the stale request", got)
	}

	// The next press locks again without UnlockPointer first.
	h.app.Press("id:game")
	h.app.PointerDelta(2, 0)
	if got := h.step(); got.dx != 2 || !got.requested {
		t.Fatalf("relocked after Escape: got %+v", got)
	}
}

func TestResetKeepsThePointerLock(t *testing.T) {
	h := startLook(t, desktopEnv(true))
	h.app.Press("id:game")
	h.app.PointerDelta(7, 7)
	h.app.Post(h.actions.Reset)
	h.app.Settle()
	if got := h.step(); got != (lookStep{requested: true}) {
		t.Fatalf("after Reset: got %+v, want no motion and the lock kept", got)
	}

	// Play goes on after Reset, as across a scene change, without another press.
	h.app.PointerDelta(3, 3)
	if got := h.step(); got.dx != 3 || got.dy != 3 || !got.requested {
		t.Fatalf("motion after Reset: got %+v", got)
	}
}

func TestLockPointerReportsAnUnsupportedSurface(t *testing.T) {
	h := startLook(t, desktopEnv(false))
	h.app.Press("id:game")
	if len(h.locks) != 1 || h.locks[0] {
		t.Fatalf("LockPointer results = %v, want [false]", h.locks)
	}
	h.app.PointerDelta(5, 5)
	if got := h.step(); got != (lookStep{}) {
		t.Fatalf("unsupported lock: got %+v", got)
	}

	a := input.NewActions()
	if a.LockPointer(nil, nil) || a.PointerLockRequested() {
		t.Fatal("LockPointer without a scope or ref locked")
	}
	a.UnlockPointer()
}

func TestPointerAxisBindingsAndNonFiniteMotion(t *testing.T) {
	a := input.NewActions()
	a.BindPointerAxis("look_x", input.PointerX, input.PointerAxisOptions{Sensitivity: math.NaN(), Invert: true})
	ax, opts, ok := a.PointerAxisBinding("look_x")
	if !ok || ax != input.PointerX || opts.Sensitivity != 0 || !opts.Invert {
		t.Fatalf("binding = %v %+v %v", ax, opts, ok)
	}
	a.OnPointerDelta(math.Inf(1), 1)
	a.OnPointerDelta(math.NaN(), 0)
	a.OnPointerDelta(2, math.Inf(-1))
	a.OnPointerDelta(3, 1)
	a.OnPointerDelta(math.MaxFloat64, 0)
	a.OnPointerDelta(math.MaxFloat64, 0)
	a.Step()
	if dx, dy := a.PointerDelta(); dx != math.MaxFloat64 || dy != 1 {
		t.Fatalf("PointerDelta = %v, %v", dx, dy)
	}
	a.Step()
	a.OnPointerDelta(3, 1)
	a.Step()
	if got := a.Delta("look_x"); got != -3 {
		t.Fatalf("Delta with default sensitivity = %v, want -3", got)
	}
	if got := a.Axis("look_x"); got != -1 {
		t.Fatalf("Axis = %v, want -1", got)
	}
	// Scaled motion that overflows saturates instead of reading as no motion.
	a.BindPointerAxis("fast", input.PointerX, input.PointerAxisOptions{Sensitivity: 2})
	a.BindPointerAxis("fast_inv", input.PointerX, input.PointerAxisOptions{Sensitivity: 2, Invert: true})
	a.OnPointerDelta(math.MaxFloat64, 0)
	a.Step()
	if got := a.Delta("fast"); got != math.MaxFloat64 {
		t.Fatalf("overflowing Delta = %v, want MaxFloat64", got)
	}
	if got := a.Delta("fast_inv"); got != -math.MaxFloat64 {
		t.Fatalf("overflowing inverted Delta = %v, want -MaxFloat64", got)
	}
	if got := a.Axis("fast"); got != 1 {
		t.Fatalf("overflowing Axis = %v, want 1", got)
	}
	a.OnPointerDelta(3, 1)
	a.Step()

	if got := a.Delta("missing"); got != 0 {
		t.Fatalf("unbound Delta = %v", got)
	}

	a.BindPointerAxis("look_x", input.PointerAxisNone, input.PointerAxisOptions{})
	if _, _, ok := a.PointerAxisBinding("look_x"); ok {
		t.Fatal("PointerAxisNone left the binding")
	}
	if got := a.Axis("look_x"); got != 0 {
		t.Fatalf("unbound Axis = %v", got)
	}
	for ax, want := range map[input.PointerAxis]string{input.PointerAxisNone: "none", input.PointerX: "pointer_x", input.PointerY: "pointer_y", 9: "PointerAxis(9)"} {
		if ax.String() != want {
			t.Fatalf("%d.String() = %q, want %q", ax, ax.String(), want)
		}
	}
}

func TestAttachChainsPointerDelta(t *testing.T) {
	a := input.NewActions()
	var heard float64
	v := core.View{OnPointerDelta: func(dx, _ float64) { heard += dx }}
	a.Attach(&v)
	v.OnPointerDelta(2, 0)
	a.Step()
	if dx, _ := a.PointerDelta(); dx != 2 || heard != 2 {
		t.Fatalf("chained motion: actions %v, view %v", dx, heard)
	}
}

func TestPointerAxesPersist(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()
	a := input.NewActions()
	a.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.002})
	a.BindPointerAxis("pitch", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.0015, Invert: true})
	if err := a.Save(store, "controls"); err != nil {
		t.Fatal(err)
	}
	raw, _ := store.GetItem("controls")
	const want = `{"version":1,"pointer_axes":{"pitch":{"axis":"pointer_y","sensitivity":0.0015,"invert":true},"yaw":{"axis":"pointer_x","sensitivity":0.002}}}`
	if raw != want {
		t.Fatalf("saved %s, want %s", raw, want)
	}

	b := input.NewActions()
	b.BindPointerAxis("roll", input.PointerX, input.PointerAxisOptions{})
	if ok, err := b.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if ax, _, ok := b.PointerAxisBinding("roll"); !ok || ax != input.PointerX {
		t.Fatalf("Load dropped a pointer axis absent from the save: %v, %v", ax, ok)
	}
	for _, name := range []string{"yaw", "pitch"} {
		wantAx, wantOpts, _ := a.PointerAxisBinding(name)
		ax, opts, ok := b.PointerAxisBinding(name)
		if !ok || ax != wantAx || opts != wantOpts {
			t.Fatalf("%s after Load = %v %+v %v, want %v %+v", name, ax, opts, ok, wantAx, wantOpts)
		}
	}
	if ok, err := b.LoadReplace(store, "controls"); !ok || err != nil {
		t.Fatalf("LoadReplace = %v, %v", ok, err)
	}
	if _, _, ok := b.PointerAxisBinding("roll"); ok {
		t.Fatal("LoadReplace kept a pointer axis the save did not have")
	}
	if err := b.Save(store, "again"); err != nil {
		t.Fatal(err)
	}
	if again, _ := store.GetItem("again"); again != raw {
		t.Fatalf("round trip changed the save: %s", again)
	}

	if err := store.SetItem("bad", `{"version":1,"pointer_axes":{"yaw":{"axis":"wheel"}}}`); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.Load(store, "bad"); ok || err == nil {
		t.Fatalf("Load of an unknown pointer axis = %v, %v", ok, err)
	}
	if _, _, ok := b.PointerAxisBinding("yaw"); !ok {
		t.Fatal("a failed Load dropped the bindings")
	}
}
