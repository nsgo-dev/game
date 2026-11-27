package input_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLoadMergesWithDefaultsAddedAfterSave(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	// v1.0: player customises existing bindings and saves.
	v1 := input.NewActions()
	v1.Bind("jump", input.Key("w"))
	v1.BindAxis("move_x", "right", "left")
	v1.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{DeadZone: 0.1, Invert: true})
	v1.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.004})
	v1.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{DeadZone: 0.2})
	if err := v1.Save(store, "controls"); err != nil {
		t.Fatalf("v1.Save failed: %v", err)
	}

	// v1.1: game binds v1.0 defaults plus new actions and axes, then loads the v1.0 save.
	v2 := input.NewActions()
	v2.Bind("jump", input.Key("space"))
	v2.Bind("dodge", input.Key("shift"))
	v2.BindAxis("move_x", "left", "right")
	v2.BindAxis("move_y", "down", "up")
	v2.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{})
	v2.BindMotionAxis("pitch_tilt", input.TiltY, input.MotionAxisOptions{Sensitivity: 1.5})
	v2.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.002})
	v2.BindPointerAxis("pitch", input.PointerY, input.PointerAxisOptions{Sensitivity: 0.002, Invert: true})
	v2.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{})
	v2.BindGamepadAxis("aim_y", gamepad.AxisRightY, input.GamepadAxisOptions{Invert: true})

	loaded, err := v2.Load(store, "controls")
	if !loaded || err != nil {
		t.Fatalf("v2.Load = (%v, %v), want (true, nil)", loaded, err)
	}

	// Saved names replace the defaults.
	if got := v2.Bindings("jump"); !slices.Equal(got, []input.Input{input.Key("w")}) {
		t.Fatalf("jump = %+v, want [key(w)]", got)
	}
	if neg, pos, ok := v2.AxisBinding("move_x"); !ok || neg != "right" || pos != "left" {
		t.Fatalf("move_x = (%q, %q, %v), want (right, left, true)", neg, pos, ok)
	}
	if ax, opts, ok := v2.MotionAxisBinding("steer"); !ok || ax != input.TiltX || opts.DeadZone != 0.1 || !opts.Invert {
		t.Fatalf("steer = (%v, %+v, %v), want saved TiltX", ax, opts, ok)
	}
	if ax, opts, ok := v2.PointerAxisBinding("yaw"); !ok || ax != input.PointerX || opts.Sensitivity != 0.004 {
		t.Fatalf("yaw = (%v, %+v, %v), want saved PointerX", ax, opts, ok)
	}
	if ax, opts, ok := v2.GamepadAxisBinding("look_x"); !ok || ax != gamepad.AxisRightX || opts.DeadZone != 0.2 {
		t.Fatalf("look_x = (%v, %+v, %v), want saved AxisRightX", ax, opts, ok)
	}

	// Names added in v1.1 and absent from the v1.0 save keep their defaults.
	if got := v2.Bindings("dodge"); !slices.Equal(got, []input.Input{input.Key("shift")}) {
		t.Fatalf("dodge = %+v, want [key(shift)]", got)
	}
	if neg, pos, ok := v2.AxisBinding("move_y"); !ok || neg != "down" || pos != "up" {
		t.Fatalf("move_y = (%q, %q, %v), want (down, up, true)", neg, pos, ok)
	}
	if ax, opts, ok := v2.MotionAxisBinding("pitch_tilt"); !ok || ax != input.TiltY || opts.Sensitivity != 1.5 {
		t.Fatalf("pitch_tilt = (%v, %+v, %v), want default TiltY", ax, opts, ok)
	}
	if ax, opts, ok := v2.PointerAxisBinding("pitch"); !ok || ax != input.PointerY || opts.Sensitivity != 0.002 || !opts.Invert {
		t.Fatalf("pitch = (%v, %+v, %v), want default PointerY", ax, opts, ok)
	}
	if ax, opts, ok := v2.GamepadAxisBinding("aim_y"); !ok || ax != gamepad.AxisRightY || !opts.Invert {
		t.Fatalf("aim_y = (%v, %+v, %v), want default AxisRightY", ax, opts, ok)
	}

	// Live input confirms dodge still fires on its default key and jump fires only on the rebound key.
	v2.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	v2.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	v2.Step()
	if !v2.Pressed("dodge") {
		t.Fatal("new default dodge did not activate on shift after loading v1.0 save")
	}
	if v2.Pressed("jump") {
		t.Fatal("old default space activated rebound jump")
	}
	v2.OnKeyDown(core.KeyEvent{Key: "w", Code: "KeyW"})
	v2.Step()
	if !v2.Pressed("jump") {
		t.Fatal("rebound key w did not activate jump")
	}
}

func TestLoadRespectsExplicitUnbindingsAndRoundTrips(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	bindDefaults := func(a *input.Actions) {
		a.Bind("jump", input.Key("space"))
		a.Bind("drop", input.Key("q"))
		a.Bind("alt", input.Key("e"))
		a.BindAxis("move_x", "left", "right")
		a.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{})
		a.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.002})
		a.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{})
	}

	// Player explicitly unbinds one entry of every binding kind and saves.
	first := input.NewActions()
	bindDefaults(first)
	first.Unbind("drop")
	first.Bind("alt") // zero inputs also unbinds
	first.UnbindAxis("move_x")
	first.UnbindMotionAxis("steer")
	first.UnbindPointerAxis("yaw")
	first.UnbindGamepadAxis("look_x")
	if err := first.Save(store, "controls"); err != nil {
		t.Fatalf("first.Save failed: %v", err)
	}

	saved1, _ := store.GetItem("controls")
	for _, wantSub := range []string{
		`"drop":[{"kind":"none"}]`,
		`"alt":[{"kind":"none"}]`,
		`"move_x":{}`,
		`"steer":{"axis":"none"}`,
		`"yaw":{"axis":"none"}`,
		`"look_x":{"axis":"none"}`,
	} {
		if !strings.Contains(saved1, wantSub) {
			t.Fatalf("saved JSON missing %s:\n%s", wantSub, saved1)
		}
	}

	// Restart (v1.1 adds "dodge"): defaults are bound first, then saved bindings are merged.
	second := input.NewActions()
	bindDefaults(second)
	second.Bind("dodge", input.Key("shift"))
	if ok, err := second.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("second.Load = (%v, %v), want (true, nil)", ok, err)
	}

	if got := second.Bindings("drop"); got != nil {
		t.Fatalf("drop = %+v, want nil (explicitly unbound)", got)
	}
	if got := second.Bindings("alt"); got != nil {
		t.Fatalf("alt = %+v, want nil (explicitly unbound via Bind())", got)
	}
	if _, _, ok := second.AxisBinding("move_x"); ok {
		t.Fatal("move_x still bound after loading explicit unbind")
	}
	if _, _, ok := second.MotionAxisBinding("steer"); ok {
		t.Fatal("steer still bound after loading explicit unbind")
	}
	if _, _, ok := second.PointerAxisBinding("yaw"); ok {
		t.Fatal("yaw still bound after loading explicit unbind")
	}
	if _, _, ok := second.GamepadAxisBinding("look_x"); ok {
		t.Fatal("look_x still bound after loading explicit unbind")
	}
	if got := second.Bindings("dodge"); !slices.Equal(got, []input.Input{input.Key("shift")}) {
		t.Fatalf("dodge = %+v, want [key(shift)]", got)
	}

	// Saving again preserves the explicit unbindings and is round-trip stable.
	if err := second.Save(store, "controls"); err != nil {
		t.Fatalf("second.Save failed: %v", err)
	}
	saved2, _ := store.GetItem("controls")

	third := input.NewActions()
	bindDefaults(third)
	third.Bind("dodge", input.Key("shift"))
	if ok, err := third.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("third.Load = (%v, %v), want (true, nil)", ok, err)
	}
	if third.Bindings("drop") != nil || third.Bindings("alt") != nil {
		t.Fatal("explicit action unbindings were lost on second restart")
	}
	if _, _, ok := third.AxisBinding("move_x"); ok {
		t.Fatal("explicit axis unbinding was lost on second restart")
	}
	if _, _, ok := third.MotionAxisBinding("steer"); ok {
		t.Fatal("explicit motion axis unbinding was lost on second restart")
	}
	if _, _, ok := third.PointerAxisBinding("yaw"); ok {
		t.Fatal("explicit pointer axis unbinding was lost on second restart")
	}
	if _, _, ok := third.GamepadAxisBinding("look_x"); ok {
		t.Fatal("explicit gamepad axis unbinding was lost on second restart")
	}
	if err := third.Save(store, "controls"); err != nil {
		t.Fatalf("third.Save failed: %v", err)
	}
	if saved3, _ := store.GetItem("controls"); saved3 != saved2 {
		t.Fatalf("Save -> Load -> Save changed JSON:\n%s\n%s", saved2, saved3)
	}

	// Re-binding a previously unbound action clears the tombstone.
	third.Bind("drop", input.Key("g"))
	if err := third.Save(store, "controls"); err != nil {
		t.Fatalf("rebind Save failed: %v", err)
	}
	fourth := input.NewActions()
	bindDefaults(fourth)
	if ok, err := fourth.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("fourth.Load = (%v, %v)", ok, err)
	}
	if got := fourth.Bindings("drop"); !slices.Equal(got, []input.Input{input.Key("g")}) {
		t.Fatalf("rebound drop = %+v, want [key(g)]", got)
	}
}

func TestLoadAcceptsEmptyListAndNoneMarkersAsUnbind(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	const raw = `{"version":1,"actions":{"drop":[],"alt":[{"kind":"none"}]},"axes":{"move_x":{}},"motion_axes":{"steer":{"axis":""}},"pointer_axes":{"yaw":{"axis":"none"}},"gamepad_axes":{"look_x":{"axis":""}}}`
	if err := store.SetItem("controls", raw); err != nil {
		t.Fatal(err)
	}

	a := input.NewActions()
	a.Bind("jump", input.Key("space"))
	a.Bind("drop", input.Key("q"))
	a.Bind("alt", input.Key("e"))
	a.BindAxis("move_x", "left", "right")
	a.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{})
	a.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{})
	a.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{})

	if ok, err := a.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v), want (true, nil)", ok, err)
	}
	if !slices.Equal(a.Bindings("jump"), []input.Input{input.Key("space")}) {
		t.Fatalf("jump = %+v, want default space", a.Bindings("jump"))
	}
	if a.Bindings("drop") != nil || a.Bindings("alt") != nil {
		t.Fatalf("unbound actions still bound: drop=%+v alt=%+v", a.Bindings("drop"), a.Bindings("alt"))
	}
	if _, _, ok := a.AxisBinding("move_x"); ok {
		t.Fatal("move_x still bound")
	}
	if _, _, ok := a.MotionAxisBinding("steer"); ok {
		t.Fatal("steer still bound")
	}
	if _, _, ok := a.PointerAxisBinding("yaw"); ok {
		t.Fatal("yaw still bound")
	}
	if _, _, ok := a.GamepadAxisBinding("look_x"); ok {
		t.Fatal("look_x still bound")
	}
}

func TestLoadUnknownAndRemovedActions(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	// Save written by an older build that had "old_skill" and "old_unbound", later removed from defaults.
	const oldSave = `{"version":1,"actions":{"jump":[{"kind":"key","key":"w"}],"old_skill":[{"kind":"key","key":"r"}],"old_unbound":[{"kind":"none"}]},"axes":{"old_axis":{"negative":"a","positive":"d"}}}`
	if err := store.SetItem("controls", oldSave); err != nil {
		t.Fatal(err)
	}

	a := input.NewActions()
	a.Bind("jump", input.Key("space"))
	a.Bind("dodge", input.Key("shift"))

	if ok, err := a.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v), want (true, nil)", ok, err)
	}
	if got := a.Bindings("jump"); !slices.Equal(got, []input.Input{input.Key("w")}) {
		t.Fatalf("jump = %+v, want [key(w)]", got)
	}
	if got := a.Bindings("dodge"); !slices.Equal(got, []input.Input{input.Key("shift")}) {
		t.Fatalf("dodge = %+v, want [key(shift)]", got)
	}
	if got := a.Bindings("old_skill"); !slices.Equal(got, []input.Input{input.Key("r")}) {
		t.Fatalf("old_skill = %+v, want [key(r)]", got)
	}
	if got := a.Bindings("old_unbound"); got != nil {
		t.Fatalf("old_unbound = %+v, want nil", got)
	}
	if neg, pos, ok := a.AxisBinding("old_axis"); !ok || neg != "a" || pos != "d" {
		t.Fatalf("old_axis = (%q, %q, %v), want (a, d, true)", neg, pos, ok)
	}
}

func TestLoadReplaceReplacesAllBindings(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	snap := input.NewActions()
	snap.Bind("jump", input.Key("w"))
	snap.Unbind("drop")
	if err := snap.Save(store, "controls"); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := store.GetItem("controls")

	a := input.NewActions()
	a.Bind("jump", input.Key("space"))
	a.Bind("dodge", input.Key("shift"))
	a.BindAxis("move_x", "left", "right")
	a.BindMotionAxis("steer", input.TiltX, input.MotionAxisOptions{})
	a.BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{})
	a.BindGamepadAxis("look_x", gamepad.AxisRightX, input.GamepadAxisOptions{})

	// Absent key leaves bindings untouched.
	if ok, err := a.LoadReplace(store, "missing"); ok || err != nil {
		t.Fatalf("LoadReplace(missing) = (%v, %v), want (false, nil)", ok, err)
	}
	if !slices.Equal(a.Bindings("dodge"), []input.Input{input.Key("shift")}) {
		t.Fatal("LoadReplace on absent key dropped bindings")
	}

	// Invalid JSON leaves bindings untouched.
	if err := store.SetItem("bad", `{"version":1,"actions":{"jump":[{"kind":"bogus"}]}}`); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.LoadReplace(store, "bad"); ok || err == nil {
		t.Fatalf("LoadReplace(bad) = (%v, %v), want (false, error)", ok, err)
	}
	if !slices.Equal(a.Bindings("dodge"), []input.Input{input.Key("shift")}) {
		t.Fatal("failed LoadReplace mutated bindings")
	}

	// Valid snapshot replaces everything, dropping defaults absent from the snapshot.
	if ok, err := a.LoadReplace(store, "controls"); !ok || err != nil {
		t.Fatalf("LoadReplace = (%v, %v), want (true, nil)", ok, err)
	}
	if got := a.Bindings("jump"); !slices.Equal(got, []input.Input{input.Key("w")}) {
		t.Fatalf("jump = %+v, want [key(w)]", got)
	}
	if a.Bindings("dodge") != nil || a.Bindings("drop") != nil {
		t.Fatalf("unexpected bindings after LoadReplace: dodge=%+v drop=%+v", a.Bindings("dodge"), a.Bindings("drop"))
	}
	if _, _, ok := a.AxisBinding("move_x"); ok {
		t.Fatal("LoadReplace kept move_x")
	}
	if _, _, ok := a.MotionAxisBinding("steer"); ok {
		t.Fatal("LoadReplace kept steer")
	}
	if _, _, ok := a.PointerAxisBinding("yaw"); ok {
		t.Fatal("LoadReplace kept yaw")
	}
	if _, _, ok := a.GamepadAxisBinding("look_x"); ok {
		t.Fatal("LoadReplace kept look_x")
	}

	// Re-saving after LoadReplace produces the exact same snapshot JSON.
	if err := a.Save(store, "again"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetItem("again"); got != wantJSON {
		t.Fatalf("Save after LoadReplace:\ngot:  %s\nwant: %s", got, wantJSON)
	}
}
