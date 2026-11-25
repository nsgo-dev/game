package animclip_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/animclip"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
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
}
