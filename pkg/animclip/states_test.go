package animclip_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/animclip"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

func knightTestClips(t *testing.T) animclip.ClipMap {
	t.Helper()
	mk := func(name string, frames []string, ms int, loop bool) *resource.Animation {
		fps := 1000.0 / float64(ms)
		c, err := animclip.FromFrames(name, frames, fps, loop)
		if err != nil {
			t.Fatalf("FromFrames(%s): %v", name, err)
		}
		return c
	}
	return animclip.ClipMap{
		"anim/knight_idle.animation.yaml":   mk("idle", []string{"idle_0", "idle_1", "idle_2", "idle_3"}, 150, true),
		"anim/knight_run.animation.yaml":    mk("run", []string{"run_0", "run_1", "run_2", "run_3", "run_4", "run_5"}, 100, true),
		"anim/knight_jump.animation.yaml":   mk("jump", []string{"jump_0", "jump_1"}, 120, false),
		"anim/knight_fall.animation.yaml":   mk("fall", []string{"fall_0", "fall_1"}, 120, true),
		"anim/knight_attack.animation.yaml": mk("attack", []string{"atk_0", "atk_1", "atk_2", "atk_3"}, 80, false),
		"anim/knight_hurt.animation.yaml":   mk("hurt", []string{"hurt_0", "hurt_1"}, 90, false),
	}
}

func knightTestAtlas() *sprite.Atlas {
	frames := map[string]sprite.Frame{}
	names := []string{
		"idle_0", "idle_1", "idle_2", "idle_3",
		"run_0", "run_1", "run_2", "run_3", "run_4", "run_5",
		"jump_0", "jump_1",
		"fall_0", "fall_1",
		"atk_0", "atk_1", "atk_2", "atk_3",
		"hurt_0", "hurt_1",
	}
	for i, name := range names {
		frames[name] = sprite.Frame{Name: name, X: float32(i * 16), Y: 0, W: 16, H: 16}
	}
	return &sprite.Atlas{
		Image:  core.ImageSource{URI: "asset:knight.png"},
		Frames: frames,
	}
}

func loadKnightStates(t *testing.T) *animclip.StateMachineDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "knight.states.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := animclip.ParseStateMachine(data)
	if err != nil {
		t.Fatalf("ParseStateMachine: %v", err)
	}
	return doc
}

func TestStateMachineKnightScriptedTransitionSequence(t *testing.T) {
	doc := loadKnightStates(t)
	clips := knightTestClips(t)

	// Run 8s default scripted input and verify idle -> run -> jump -> fall -> run -> ... and no stuck states.
	report, err := animclip.SimulateStateMachine(doc, clips, nil, nil, animclip.SimulationOptions{
		Duration: 8 * time.Second,
		Rate:     60,
	})
	if err != nil {
		t.Fatalf("SimulateStateMachine: %v", err)
	}
	if len(report.StuckStates) > 0 {
		t.Fatalf("unexpected stuck states: %v", report.StuckStates)
	}
	if got := report.Summary(); got != "Played 8 s of scripted input · no stuck states" {
		t.Fatalf("Summary() = %q, want %q", got, "Played 8 s of scripted input · no stuck states")
	}

	// Verify the exact acceptance subsequence: idle -> run -> jump -> fall -> run.
	wantPrefix := []string{"idle", "run", "jump", "fall", "run"}
	if len(report.Trace) < len(wantPrefix) || !slices.Equal(report.Trace[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("trace prefix = %v, want %v (full trace: %v)", report.Trace, wantPrefix, report.Trace)
	}
	// All 6 knight states should have been visited.
	wantVisited := []string{"attack", "fall", "hurt", "idle", "jump", "run"}
	if !slices.Equal(report.VisitedStates, wantVisited) {
		t.Fatalf("visited = %v, want %v", report.VisitedStates, wantVisited)
	}
}

func TestStateMachineNewAnimatorOnBuiltScene(t *testing.T) {
	doc := loadKnightStates(t)
	clips := knightTestClips(t)
	atlas := knightTestAtlas()

	sceneDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Dungeon",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{{
				ID:     "knight",
				Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
				Components: map[string]any{
					sceneformat.AnimatorComponentName: map[string]any{
						"states":    "anim/knight.states.yaml",
						"startClip": "idle",
					},
				},
			}},
		}},
	}
	built, err := sceneformat.Build(sceneDoc, sceneformat.AtlasMap{"knight": atlas})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm, err := animclip.NewAnimator(built, "knight", animclip.StatesMap{"anim/knight.states.yaml": doc}, clips, nil)
	if err != nil {
		t.Fatalf("NewAnimator: %v", err)
	}
	if sm.State() != "idle" {
		t.Fatalf("initial state = %q, want idle", sm.State())
	}
	if got := built.Object("knight").Sprite.Frame.Name; got != "idle_0" {
		t.Fatalf("initial frame = %q, want idle_0", got)
	}

	// Drive with input.Actions through InputDriver.
	actions := input.NewActions()
	actions.Bind("move_right", input.Key("d"))
	actions.Bind("jump", input.Key("space"))
	driver := animclip.NewInputDriver()
	dt := time.Second / 60

	var trace []string
	trace = append(trace, sm.State())
	sm.OnTransition = func(_, to string) {
		trace = append(trace, to)
	}

	// Hold right for 300ms -> run.
	actions.Press("move_right")
	for range 18 {
		actions.Step()
		driver.StepActions(sm, actions, dt)
		sm.Update(dt)
	}
	if sm.State() != "run" {
		t.Fatalf("state after holding right = %q, want run", sm.State())
	}
	if got := built.Object("knight").Sprite.Frame.Name; !strings.HasPrefix(got, "run_") {
		t.Fatalf("sprite frame while running = %q, want run_*", got)
	}

	// Tap jump while holding right -> jump -> fall -> run.
	actions.Trigger("jump")
	for range 60 {
		actions.Step()
		driver.StepActions(sm, actions, dt)
		sm.Update(dt)
	}
	want := []string{"idle", "run", "jump", "fall", "run"}
	if !slices.Equal(trace, want) {
		t.Fatalf("trace = %v, want %v", trace, want)
	}
}

func TestStateMachineValidationAndConditions(t *testing.T) {
	t.Run("missing target state reports field path", func(t *testing.T) {
		raw := `version: 1
entry: idle
parameters:
  - name: go
    type: trigger
states:
  - name: idle
    clip: anim/idle.animation.yaml
transitions:
  - from: idle
    to: phantom
    when: go
`
		_, err := animclip.ParseStateMachine([]byte(raw))
		var sme *animclip.StateMachineError
		if !errors.As(err, &sme) {
			t.Fatalf("want *StateMachineError, got %T (%v)", err, err)
		}
		if !slices.Equal(sme.Path, []string{"transitions", "0", "to"}) {
			t.Fatalf("Path = %v, want [transitions 0 to]", sme.Path)
		}
	})

	t.Run("condition expression variants and type checks", func(t *testing.T) {
		params := map[string]animclip.ParamType{
			"move.x":   animclip.ParamFloat,
			"on_floor": animclip.ParamBool,
			"jump":     animclip.ParamTrigger,
		}
		for _, expr := range []string{
			"|move.x| > 0.1",
			"abs(move.x) <= 0.1",
			"|move.x| ≤ 0.1",
			"jump && on_floor",
			"!(on_floor) || move.x < -0.5",
			"on_floor == true and not jump",
		} {
			c, err := animclip.CompileCondition(expr, params)
			if err != nil {
				t.Fatalf("CompileCondition(%q): %v", expr, err)
			}
			_ = c.Eval(func(string) float64 { return 0.5 }, func(string) bool { return true })
		}
		for _, bad := range []string{
			"unknown > 0",
			"on_floor > 0",
			"move.x && on_floor",
			"|on_floor| > 0",
			"move.x",
		} {
			if _, err := animclip.CompileCondition(bad, params); err == nil {
				t.Fatalf("CompileCondition(%q) succeeded, want error", bad)
			}
		}
	})

	t.Run("unused parameters detected", func(t *testing.T) {
		raw := `version: 1
entry: idle
parameters:
  - name: move.x
    type: float
  - name: unused_flag
    type: bool
states:
  - name: idle
    clip: anim/idle.animation.yaml
  - name: run
    clip: anim/run.animation.yaml
transitions:
  - from: idle
    to: run
    when: '|move.x| > 0.1'
`
		doc, err := animclip.ParseStateMachine([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		unused := doc.UnusedParameters()
		if !slices.Equal(unused, []int{1}) {
			t.Fatalf("UnusedParameters() = %v, want [1]", unused)
		}
	})

	t.Run("stuck non-looping state detected by simulation", func(t *testing.T) {
		raw := `version: 1
entry: idle
parameters:
  - name: attack
    type: trigger
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
  - name: attack
    clip: anim/knight_attack.animation.yaml
transitions:
  - from: idle
    to: attack
    when: attack
`
		doc, err := animclip.ParseStateMachine([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		report, err := animclip.SimulateStateMachine(doc, knightTestClips(t), nil, nil, animclip.SimulationOptions{
			Duration: 2 * time.Second,
			Script: []animclip.ScriptStep{
				{At: 100 * time.Millisecond, Attack: true},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(report.StuckStates, "attack") {
			t.Fatalf("StuckStates = %v, want attack", report.StuckStates)
		}
		if !strings.Contains(report.Summary(), "1 stuck state: attack") {
			t.Fatalf("Summary = %q", report.Summary())
		}
	})

	t.Run("whitespace-only when and non-ASCII parameter names refused consistently", func(t *testing.T) {
		wsWhen := `version: 1
entry: idle
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
  - name: run
    clip: anim/knight_run.animation.yaml
transitions:
  - from: idle
    to: run
    when: "   "
    onEnd: true
`
		if _, err := animclip.ParseStateMachine([]byte(wsWhen)); err == nil {
			t.Fatal("expected whitespace-only when to be refused")
		}

		nonASCII := `version: 1
entry: idle
parameters:
  - name: 速度
    type: float
states:
  - name: idle
    clip: anim/knight_idle.animation.yaml
`
		if _, err := animclip.ParseStateMachine([]byte(nonASCII)); err == nil {
			t.Fatal("expected non-ASCII parameter name to be refused")
		}
	})

	t.Run("huge speed multiplier clamps without duration overflow", func(t *testing.T) {
		raw := `version: 1
entry: attack
states:
  - name: attack
    clip: anim/knight_attack.animation.yaml
  - name: idle
    clip: anim/knight_idle.animation.yaml
transitions:
  - from: attack
    to: idle
    onEnd: true
`
		doc, err := animclip.ParseStateMachine([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		sm, err := animclip.NewStateMachine(doc, knightTestClips(t), nil, animclip.NoopBinder)
		if err != nil {
			t.Fatal(err)
		}
		sm.SetSpeed(1e18)
		sm.Update(16 * time.Millisecond)
		if got := sm.State(); got != "idle" {
			t.Fatalf("State after clamped huge speed step = %q, want idle", got)
		}
	})
}

func TestAnimatorSignalsEventsAndHandlers(t *testing.T) {
	doc := loadKnightStates(t)
	clips := knightTestClips(t)
	// Add two cue events to the attack clip (length 320ms: 4 frames x 80ms).
	atk := clips["anim/knight_attack.animation.yaml"]
	atk.Events = []resource.Event{
		{Time: 0.08, Name: "swing"},
		{Time: 0.16, Name: "hit"},
	}

	reg := sceneformat.AnimatorComponentRegistry()
	var stateEvents []sceneformat.AnimatorStateChangedEvent
	var clipEvents []sceneformat.AnimatorClipEvent
	reg.MustRegister(sceneformat.ComponentType{
		Name: "button",
		Events: []sceneformat.Event{
			{Name: "attackPressed"},
			{Name: "forceRun"},
			{Name: "forceIdle"},
		},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "sfx",
		Handlers: []sceneformat.Handler{
			{
				Name: "onStateChanged",
				Func: func(c sceneformat.Call) {
					ev, ok := c.Payload.(sceneformat.AnimatorStateChangedEvent)
					if !ok {
						t.Errorf("stateChanged payload = %T, want AnimatorStateChangedEvent", c.Payload)
						return
					}
					stateEvents = append(stateEvents, ev)
				},
			},
			{
				Name: "onClipEvent",
				Func: func(c sceneformat.Call) {
					ev, ok := c.Payload.(sceneformat.AnimatorClipEvent)
					if !ok {
						t.Errorf("clipEvent payload = %T, want AnimatorClipEvent", c.Payload)
						return
					}
					clipEvents = append(clipEvents, ev)
				},
			},
		},
	})

	sceneDoc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Arena",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{
				{ID: "btn", Components: map[string]any{"button": map[string]any{}}},
				{
					ID:     "knight",
					Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
					Components: map[string]any{
						sceneformat.AnimatorComponentName: map[string]any{
							"states":    "anim/knight.states.yaml",
							"startClip": "idle",
						},
					},
				},
				{ID: "audio", Components: map[string]any{"sfx": map[string]any{}}},
			},
		}},
		Connections: []sceneformat.ConnectionDoc{
			{From: "btn", Event: "attackPressed", To: "knight", Handler: "animator.trigger", Args: map[string]any{"name": "attack"}},
			{From: "btn", Event: "forceRun", To: "knight", Handler: "animator.setState", Args: map[string]any{"state": "run"}},
			{From: "btn", Event: "forceIdle", To: "knight", Handler: "animator.setState", Args: map[string]any{"state": "idle"}},
			{From: "knight", Event: "animator.stateChanged", To: "audio", Handler: "onStateChanged"},
			{From: "knight", Event: "animator.clipEvent", To: "audio", Handler: "onClipEvent"},
		},
	}

	built, err := sceneformat.Build(sceneDoc, sceneformat.AtlasMap{"knight": knightTestAtlas()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm, err := animclip.NewAnimator(built, "knight", animclip.StatesMap{"anim/knight.states.yaml": doc}, clips, reg)
	if err != nil {
		t.Fatalf("NewAnimator: %v", err)
	}
	// Connect succeeds with the default animator component registration.
	sig, err := sceneformat.Connect(built.Doc, reg, built)
	if err != nil {
		t.Fatalf("sceneformat.Connect: %v", err)
	}
	if err := animclip.Connect(sm, sig, "knight"); err != nil {
		t.Fatalf("animclip.Connect: %v", err)
	}

	// Go callbacks must continue to fire alongside scene Signals.
	var cbTransitions []string
	var cbEvents []string
	sm.OnTransition = func(from, to string) {
		cbTransitions = append(cbTransitions, from+"->"+to)
	}
	sm.OnEvent = func(state, event string) {
		cbEvents = append(cbEvents, state+":"+event)
	}

	// 1. Trigger handler pulses "attack", transitioning idle -> attack on Update, firing cue events once each, then onEnd -> idle.
	if err := sig.Emit("btn", "attackPressed", nil); err != nil {
		t.Fatalf("Emit attackPressed: %v", err)
	}
	dt := 20 * time.Millisecond
	for range 20 { // 400ms > 320ms attack length
		sm.Update(dt)
	}
	if sm.State() != "idle" {
		t.Fatalf("state after attack finished = %q, want idle", sm.State())
	}

	wantStateEvents := []sceneformat.AnimatorStateChangedEvent{
		{ObjectID: "knight", From: "idle", To: "attack"},
		{ObjectID: "knight", From: "attack", To: "idle"},
	}
	if !reflect.DeepEqual(stateEvents, wantStateEvents) {
		t.Fatalf("stateEvents = %+v, want %+v", stateEvents, wantStateEvents)
	}
	wantClipEvents := []sceneformat.AnimatorClipEvent{
		{ObjectID: "knight", State: "attack", Name: "swing"},
		{ObjectID: "knight", State: "attack", Name: "hit"},
	}
	if !reflect.DeepEqual(clipEvents, wantClipEvents) {
		t.Fatalf("clipEvents = %+v, want %+v", clipEvents, wantClipEvents)
	}
	if !slices.Equal(cbTransitions, []string{"idle->attack", "attack->idle"}) {
		t.Fatalf("OnTransition = %v, want [idle->attack attack->idle]", cbTransitions)
	}
	if !slices.Equal(cbEvents, []string{"attack:swing", "attack:hit"}) {
		t.Fatalf("OnEvent = %v, want [attack:swing attack:hit]", cbEvents)
	}

	// 2. setState handler switches directly to "run" and emits stateChanged once; repeating same state emits nothing.
	if err := sig.Emit("btn", "forceRun", nil); err != nil {
		t.Fatalf("Emit forceRun: %v", err)
	}
	if sm.State() != "run" {
		t.Fatalf("state after setState(run) = %q, want run", sm.State())
	}
	if got := built.Object("knight").Sprite.Frame.Name; got != "run_0" {
		t.Fatalf("sprite frame after setState(run) = %q, want run_0", got)
	}
	if err := sig.Emit("btn", "forceRun", nil); err != nil {
		t.Fatalf("second Emit forceRun: %v", err)
	}
	if len(stateEvents) != 3 {
		t.Fatalf("stateEvents count = %d (%+v), want 3 (no duplicate for same state)", len(stateEvents), stateEvents)
	}
	if want := (sceneformat.AnimatorStateChangedEvent{ObjectID: "knight", From: "idle", To: "run"}); stateEvents[2] != want {
		t.Fatalf("stateEvents[2] = %+v, want %+v", stateEvents[2], want)
	}

	// 3. setState back to idle works too.
	if err := sig.Emit("btn", "forceIdle", nil); err != nil {
		t.Fatalf("Emit forceIdle: %v", err)
	}
	if sm.State() != "idle" || len(stateEvents) != 4 {
		t.Fatalf("after forceIdle: state=%q events=%+v", sm.State(), stateEvents)
	}
}

func TestNewAnimatorBuiltSceneContextHandlersWithoutExplicitConnect(t *testing.T) {
	doc := loadKnightStates(t)
	clips := knightTestClips(t)
	reg := sceneformat.AnimatorComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "go"}, {Name: "hit"}},
	})

	sceneDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{
				{ID: "btn", Components: map[string]any{"button": map[string]any{}}},
				{
					ID:     "knight",
					Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
					Components: map[string]any{
						sceneformat.AnimatorComponentName: map[string]any{
							"states": "anim/knight.states.yaml",
						},
					},
				},
			},
		}},
		Connections: []sceneformat.ConnectionDoc{
			{From: "btn", Event: "go", To: "knight", Handler: "animator.setState", Args: map[string]any{"state": "run"}},
			{From: "btn", Event: "hit", To: "knight", Handler: "animator.trigger", Args: map[string]any{"name": "hurt"}},
		},
	}

	built, err := sceneformat.Build(sceneDoc, sceneformat.AtlasMap{"knight": knightTestAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	sm, err := animclip.NewAnimator(built, "knight", animclip.StatesMap{"anim/knight.states.yaml": doc}, clips, reg)
	if err != nil {
		t.Fatal(err)
	}
	// Pass built as Call.Context without calling animclip.Connect: handlers must still drive sm.
	sig, err := sceneformat.Connect(built.Doc, reg, built)
	if err != nil {
		t.Fatalf("sceneformat.Connect: %v", err)
	}
	if err := sig.Emit("btn", "go", nil); err != nil {
		t.Fatal(err)
	}
	if sm.State() != "run" {
		t.Fatalf("state after setState via BuiltScene context = %q, want run", sm.State())
	}
	// Keep move.x non-zero so run -> idle does not preempt Any state -> hurt.
	sm.SetFloat("move.x", 1)
	if err := sig.Emit("btn", "hit", nil); err != nil {
		t.Fatal(err)
	}
	sm.Update(time.Second / 60)
	if sm.State() != "hurt" {
		t.Fatalf("state after trigger via BuiltScene context = %q, want hurt", sm.State())
	}
}

func TestAnimatorSelfWiredClipEventToTriggerAndSpawnConnected(t *testing.T) {
	doc := loadKnightStates(t)
	clips := knightTestClips(t)
	clips["anim/knight_attack.animation.yaml"].Events = []resource.Event{
		{Time: 0.08, Name: "recoil"},
	}

	reg := sceneformat.AnimatorComponentRegistry()
	var observed []sceneformat.AnimatorStateChangedEvent
	reg.MustRegister(sceneformat.ComponentType{
		Name: "obs",
		Handlers: []sceneformat.Handler{{
			Name: "record",
			Func: func(c sceneformat.Call) {
				if ev, ok := c.Payload.(sceneformat.AnimatorStateChangedEvent); ok {
					observed = append(observed, ev)
				}
			},
		}},
	})

	sceneDoc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{{
			Name:  "actors",
			Space: sceneformat.SpaceWorld,
			Objects: []sceneformat.ObjectDoc{
				{
					ID:     "knight",
					Sprite: &sceneformat.SpriteRef{Atlas: "knight", Frame: "idle_0"},
					Components: map[string]any{
						sceneformat.AnimatorComponentName: map[string]any{
							"states": "anim/knight.states.yaml",
						},
					},
				},
				{ID: "obs", Components: map[string]any{"obs": map[string]any{}}},
			},
		}},
		Connections: []sceneformat.ConnectionDoc{
			// Clip cue during attack pulses hurt trigger on the same animator; must survive to next Update.
			{From: "knight", Event: "animator.clipEvent", To: "knight", Handler: "animator.trigger", Args: map[string]any{"name": "hurt"}},
			{From: "knight", Event: "animator.stateChanged", To: "obs", Handler: "record"},
		},
	}

	built, err := sceneformat.Build(sceneDoc, sceneformat.AtlasMap{"knight": knightTestAtlas()})
	if err != nil {
		t.Fatal(err)
	}
	sm, err := animclip.NewAnimator(built, "knight", animclip.StatesMap{"anim/knight.states.yaml": doc}, clips, reg)
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnConnected(built.Doc, w, reg, built)
	if err != nil {
		t.Fatalf("SpawnConnected: %v", err)
	}
	if err := animclip.Connect(sm, sig, "knight"); err != nil {
		t.Fatalf("animclip.Connect: %v", err)
	}

	sm.Trigger("attack")
	sm.Update(100 * time.Millisecond) // transitions idle->attack, crosses 80ms "recoil" cue -> pulses "hurt"
	if sm.State() != "attack" {
		t.Fatalf("state after cue step = %q, want attack", sm.State())
	}
	sm.Update(20 * time.Millisecond) // consumes pending "hurt" trigger -> attack->hurt
	if sm.State() != "hurt" {
		t.Fatalf("state after follow-up step = %q, want hurt", sm.State())
	}
	if len(observed) != 2 || observed[0].To != "attack" || observed[1].From != "attack" || observed[1].To != "hurt" {
		t.Fatalf("observed transitions = %+v", observed)
	}

	// Destroying the entity stops further signal delivery.
	w.Destroy(spawned["knight"])
	sm.SetState("idle")
	if len(observed) != 2 {
		t.Fatalf("destroyed entity emitted stateChanged: %+v", observed)
	}

	// animclip.Connect error checks.
	if err := animclip.Connect(nil, sig, "knight"); err == nil {
		t.Fatal("Connect(nil, sig, id) succeeded, want error")
	}
	if err := animclip.Connect(sm, nil, "knight"); err == nil {
		t.Fatal("Connect(sm, nil, id) succeeded, want error")
	}
	if err := animclip.Connect(sm, sig, ""); err == nil {
		t.Fatal("Connect(sm, sig, \"\") succeeded, want error")
	}
	if err := animclip.Connect(sm, sig, "missing"); err == nil {
		t.Fatal("Connect(sm, sig, missing) succeeded, want error")
	}
}
