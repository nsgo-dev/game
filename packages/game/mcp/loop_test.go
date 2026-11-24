package mcp_test

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestLoopStep60AdvancesExactlyOneSecond verifies the core acceptance criterion:
// pausing the loop and stepping 60 updates at 60 Hz executes Update exactly 60
// times and advances the game by exactly 1.0 second of game time.
func TestLoopStep60AdvancesExactlyOneSecond(t *testing.T) {
	var (
		cx      *core.Scope
		updates int
		draws   int
		gotDTs  []time.Duration
		posX    float64
	)

	world := entities.NewWorld()
	if err := entities.RegisterCodec[posComp](world, "position"); err != nil {
		t.Fatal(err)
	}
	player := world.Spawn()
	entities.Set(world, player, posComp{X: 0, Y: 10})

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			updates++
			gotDTs = append(gotDTs, dt)
			// Move 60 units per second -> 1 unit per 60 Hz step.
			posX += 60.0 / 60.0
			if p, ok := entities.Get[posComp](world, player); ok {
				p.X = float32(posX)
			}
		},
		Draw: func(float64) {
			draws++
		},
	}

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		world.Attach(cx)
		loop.Run(cx)
		if _, err := mcp.Attach(cx, mcp.Config{
			Loop:  loop,
			World: world,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Pause immediately before any display frames advance.
	st := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopPause, nil)
	if !st.Paused || st.StepHz != 60 || st.Steps != 0 || st.GameTimeSeconds != 0 || st.TimeScale != 1 || st.SeedConfigured {
		t.Fatalf("status after pause = %+v", st)
	}
	if math.Abs(st.StepSeconds-1.0/60.0) > 1e-12 {
		t.Fatalf("step_seconds = %v, want 1/60", st.StepSeconds)
	}

	// Advancing display time while paused must run zero Update steps.
	app.Advance(time.Second)
	if updates != 0 || loop.Steps() != 0 {
		t.Fatalf("updates while paused = %d (loop.Steps=%d), want 0", updates, loop.Steps())
	}

	// Step 60 updates synchronously -> must advance EXACTLY 1.0 second of game time!
	drawsBefore := draws
	stepRes := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 60})
	if stepRes.StepsAdvanced != 60 || stepRes.TotalSteps != 60 {
		t.Fatalf("step result counts = %+v, want steps_advanced=60 total_steps=60", stepRes)
	}
	if stepRes.GameTimeSeconds != 1.0 || stepRes.DeltaSeconds != 1.0 {
		t.Fatalf("step result times = %+v, want exact game_time_seconds=1.0 delta_seconds=1.0", stepRes)
	}
	if !stepRes.Paused {
		t.Fatalf("step result paused = false, want true")
	}
	if updates != 60 {
		t.Fatalf("Update count = %d, want 60", updates)
	}
	if draws != drawsBefore+1 {
		t.Fatalf("Draw count after step = %d, want %d", draws, drawsBefore+1)
	}
	if loop.Elapsed() != time.Second || loop.ElapsedSeconds() != 1.0 {
		t.Fatalf("loop elapsed = %v (%v s), want exact 1s", loop.Elapsed(), loop.ElapsedSeconds())
	}
	wantDT := time.Second / 60
	for i, dt := range gotDTs {
		if dt != wantDT {
			t.Fatalf("update[%d] dt = %v, want %v", i, dt, wantDT)
		}
	}

	// Inspect entity immediately after stepping (without advancing display clock) to confirm synchronous effect.
	comp := callTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       mcp.FormatEntityRef(player),
		"component": "position",
	})
	var gotPos posComp
	if err := json.Unmarshal(comp.Value, &gotPos); err != nil {
		t.Fatal(err)
	}
	if gotPos.X != 60 || gotPos.Y != 10 {
		t.Fatalf("player position after 60 steps = %+v, want (60, 10)", gotPos)
	}

	// Step default (omitted count -> 1) and verify incremental status.
	oneRes := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, nil)
	if oneRes.StepsAdvanced != 1 || oneRes.TotalSteps != 61 || math.Abs(oneRes.DeltaSeconds-1.0/60.0) > 1e-12 {
		t.Fatalf("default single step = %+v", oneRes)
	}

	// Step another 59 updates -> total 120 updates = exact 2.0 seconds.
	remRes := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 59})
	if remRes.StepsAdvanced != 59 || remRes.TotalSteps != 120 || remRes.GameTimeSeconds != 2.0 {
		t.Fatalf("second 1s step = %+v, want total_steps=120 game_time_seconds=2.0", remRes)
	}

	// Confirm game_loop_status matches.
	statusAfter := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopStatus, nil)
	if statusAfter.Steps != 120 || statusAfter.GameTimeSeconds != 2.0 || !statusAfter.Paused {
		t.Fatalf("status after 120 steps = %+v", statusAfter)
	}
}

func TestLoopPauseResumeAndTimeScale(t *testing.T) {
	var (
		cx          *core.Scope
		updates     int
		pauseEvents []bool
	)

	loop := &game.Loop{
		Rate:    60,
		Update:  func(time.Duration) { updates++ },
		OnPause: func(p bool) { pauseEvents = append(pauseEvents, p) },
	}

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		loop.Run(cx)
		if _, err := mcp.Attach(cx, mcp.Config{Loop: loop}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Initially running at 1x speed.
	st := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopStatus, nil)
	if st.Paused || st.TimeScale != 1 || st.StepHz != 60 {
		t.Fatalf("initial status = %+v", st)
	}

	// Stepping while running (not paused) also works synchronously.
	stepRunning := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 10})
	if stepRunning.StepsAdvanced != 10 || stepRunning.TotalSteps != 10 || stepRunning.Paused {
		t.Fatalf("step while running = %+v", stepRunning)
	}

	// Pause stops display-driven updates and fires OnPause(true).
	stPaused := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopPause, nil)
	if !stPaused.Paused {
		t.Fatalf("expected paused=true: %+v", stPaused)
	}
	app.Advance(time.Second)
	if updates != 10 {
		t.Fatalf("updates while paused = %d, want 10", updates)
	}

	// Resume restores updates without catch-up backlog and fires OnPause(false).
	stResumed := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopResume, nil)
	if stResumed.Paused {
		t.Fatalf("expected paused=false: %+v", stResumed)
	}
	app.Advance(time.Second)
	if updates != 70 {
		t.Fatalf("updates after 1s resumed = %d, want 70", updates)
	}
	if !slices.Equal(pauseEvents, []bool{true, false}) {
		t.Fatalf("OnPause events = %v, want [true false]", pauseEvents)
	}

	// Set time_scale = 0.5 -> 30 updates per real second.
	stHalf := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopTimeScale, map[string]any{"time_scale": 0.5})
	if stHalf.TimeScale != 0.5 || loop.TimeScale != 0.5 {
		t.Fatalf("half speed status = %+v, loop.TimeScale = %v", stHalf, loop.TimeScale)
	}
	app.Advance(time.Second)
	if updates != 100 {
		t.Fatalf("updates after 1s at 0.5x = %d, want 100", updates)
	}

	// Set time_scale = 2.0 -> 120 updates per real second.
	stDouble := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopTimeScale, map[string]any{"time_scale": 2.0})
	if stDouble.TimeScale != 2.0 || loop.TimeScale != 2.0 {
		t.Fatalf("double speed status = %+v, loop.TimeScale = %v", stDouble, loop.TimeScale)
	}
	app.Advance(time.Second)
	if updates != 220 {
		t.Fatalf("updates after 1s at 2.0x = %d, want 220", updates)
	}

	// Set time_scale = 0 -> resets Loop.TimeScale to 0 (effective 1x speed).
	stReset := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopTimeScale, map[string]any{"time_scale": 0})
	if loop.TimeScale != 0 || stReset.TimeScale != 1.0 {
		t.Fatalf("reset speed status = %+v, loop.TimeScale = %v", stReset, loop.TimeScale)
	}
	app.Advance(time.Second)
	if updates != 280 {
		t.Fatalf("updates after 1s at reset 1x = %d, want 280", updates)
	}
}

func TestLoopSeedDeterminism(t *testing.T) {
	rng := random.New(1)
	streams := random.NewStreams(1)
	lootStream := streams.Stream("loot")
	var callbackSeed uint64
	var callbackCalls int

	var (
		cx      *core.Scope
		samples []uint64
		loots   []int
	)
	loop := &game.Loop{
		Rate:   60,
		Paused: true,
		Update: func(time.Duration) {
			samples = append(samples, rng.Uint64())
			loots = append(loots, lootStream.Intn(1000))
		},
	}

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		loop.Run(cx)
		if _, err := mcp.Attach(cx, mcp.Config{
			Loop:          loop,
			Random:        rng,
			RandomStreams: streams,
			SetSeed: func(s uint64) {
				callbackSeed = s
				callbackCalls++
			},
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	st := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopStatus, nil)
	if !st.SeedConfigured {
		t.Fatalf("expected seed_configured=true: %+v", st)
	}

	// Run 1 with seed 424242.
	seedRes := callTool[mcp.LoopSeedResult](t, app, cx, mcp.ToolLoopSeed, map[string]any{"seed": uint64(424242)})
	if seedRes.Seed != 424242 || callbackSeed != 424242 || callbackCalls != 1 {
		t.Fatalf("seed result = %+v, callback=(%d, %d)", seedRes, callbackSeed, callbackCalls)
	}
	_ = callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 20})
	run1Samples := slices.Clone(samples)
	run1Loots := slices.Clone(loots)

	// Run 2 with the exact same seed 424242 must produce the identical sequence.
	samples = nil
	loots = nil
	_ = callTool[mcp.LoopSeedResult](t, app, cx, mcp.ToolLoopSeed, map[string]any{"seed": uint64(424242)})
	_ = callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 20})
	if !slices.Equal(run1Samples, samples) {
		t.Fatalf("RNG sequence diverged across identical seeds:\nrun1=%v\nrun2=%v", run1Samples, samples)
	}
	if !slices.Equal(run1Loots, loots) {
		t.Fatalf("RandomStreams sequence diverged across identical seeds:\nrun1=%v\nrun2=%v", run1Loots, loots)
	}

	// Different seed (including 0) produces a different sequence and succeeds cleanly.
	samples = nil
	loots = nil
	zeroRes := callTool[mcp.LoopSeedResult](t, app, cx, mcp.ToolLoopSeed, map[string]any{"seed": 0})
	if zeroRes.Seed != 0 || rng.Seed() != 0 || streams.Seed() != 0 {
		t.Fatalf("seed 0 result = %+v, rng=%d streams=%d", zeroRes, rng.Seed(), streams.Seed())
	}
	_ = callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 20})
	if slices.Equal(run1Samples, samples) {
		t.Fatal("expected seed 0 to produce a different sequence than seed 424242")
	}
}

func TestLoopDynamicResolverAndStopMidStep(t *testing.T) {
	var stop1 func()
	updates1 := 0
	l1 := &game.Loop{
		Rate:   120,
		Paused: true,
		Update: func(time.Duration) {
			updates1++
			if updates1 == 5 {
				stop1()
			}
		},
	}
	updates2 := 0
	l2 := &game.Loop{
		Rate:   120,
		Paused: true,
		Update: func(time.Duration) { updates2++ },
	}
	cur := l1

	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		stop1 = l1.Run(cx)
		l2.Run(cx)
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{
			LoopFunc: func() *game.Loop { return cur },
		})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Update stops l1 on step 5 out of 20 -> reports exact steps_advanced=5.
	res := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 20})
	if res.StepsAdvanced != 5 || res.TotalSteps != 5 || updates1 != 5 {
		t.Fatalf("stop mid-step = %+v (updates1=%d), want 5", res, updates1)
	}

	// Swap active loop via LoopFunc to l2 (120 Hz): 120 steps = exact 1.0 s.
	cur = l2
	res2 := callTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 120})
	if res2.StepsAdvanced != 120 || res2.TotalSteps != 120 || res2.GameTimeSeconds != 1.0 || res2.DeltaSeconds != 1.0 || updates2 != 120 {
		t.Fatalf("l2 120 Hz step = %+v (updates2=%d), want 120 steps / 1.0s", res2, updates2)
	}

	// SetLoop works when LoopFunc returns nil.
	cur = nil
	l3 := &game.Loop{Rate: 60, Paused: true}
	bridge.SetLoop(l3)
	st3 := callTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopStatus, nil)
	if st3.StepHz != 60 || st3.Steps != 0 {
		t.Fatalf("SetLoop status = %+v", st3)
	}
}

func TestLoopControlErrorsAndValidation(t *testing.T) {
	var (
		cx     *core.Scope
		bridge *mcp.Bridge
	)
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		var err error
		bridge, err = mcp.Attach(cx, mcp.Config{})
		if err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Calling loop tools when Loop == nil must return clean errors without panicking.
	for _, tc := range []struct {
		tool string
		args any
	}{
		{mcp.ToolLoopStatus, nil},
		{mcp.ToolLoopPause, nil},
		{mcp.ToolLoopResume, nil},
		{mcp.ToolLoopStep, map[string]any{"count": 1}},
		{mcp.ToolLoopTimeScale, map[string]any{"time_scale": 1.0}},
	} {
		if _, err := callToolRaw(t, app, cx, tc.tool, tc.args); err == nil || !strings.Contains(err.Error(), "no game loop") {
			t.Fatalf("%s with nil loop = %v, want no game loop error", tc.tool, err)
		}
	}

	// Calling game_loop_seed when no random target is configured returns a clear error.
	if _, err := callToolRaw(t, app, cx, mcp.ToolLoopSeed, map[string]any{"seed": 1}); err == nil || !strings.Contains(err.Error(), "no random seed target") {
		t.Fatalf("seed without target = %v, want actionable error", err)
	}

	// Attach a loop and verify argument validation and unknown-field rejection.
	l := &game.Loop{Rate: 60, Paused: true}
	bridge.SetLoop(l)

	// Unknown JSON fields rejected across all loop tools.
	for _, tool := range []string{
		mcp.ToolLoopStatus,
		mcp.ToolLoopPause,
		mcp.ToolLoopResume,
		mcp.ToolLoopStep,
		mcp.ToolLoopTimeScale,
		mcp.ToolLoopSeed,
	} {
		if _, err := callToolRaw(t, app, cx, tool, `{"unknown":1}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s unknown field = %v, want unknown field error", tool, err)
		}
	}

	// Invalid step counts rejected.
	for _, badCount := range []int{0, -1, mcp.MaxLoopSteps + 1} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolLoopStep, map[string]any{"count": badCount}); err == nil || !strings.Contains(err.Error(), "count must be between") {
			t.Fatalf("step count %d = %v, want range error", badCount, err)
		}
	}

	// Invalid time_scale values rejected (missing, negative, overflow/non-finite JSON, non-number).
	if _, err := callToolRaw(t, app, cx, mcp.ToolLoopTimeScale, `{}`); err == nil || !strings.Contains(err.Error(), "time_scale is required") {
		t.Fatalf("missing time_scale = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolLoopTimeScale, map[string]any{"time_scale": -0.5}); err == nil || !strings.Contains(err.Error(), "finite and >= 0") {
		t.Fatalf("negative time_scale = %v", err)
	}
	for _, badRaw := range []string{`{"time_scale":1e400}`, `{"time_scale":"NaN"}`, `{"time_scale":"Inf"}`} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolLoopTimeScale, badRaw); err == nil {
			t.Fatalf("time_scale %s succeeded, want error", badRaw)
		}
	}

	// Invalid seed values rejected (missing, negative, float, string).
	if _, err := callToolRaw(t, app, cx, mcp.ToolLoopSeed, `{}`); err == nil || !strings.Contains(err.Error(), "seed is required") {
		t.Fatalf("missing seed = %v", err)
	}
	for _, badRaw := range []string{`{"seed":-1}`, `{"seed":1.5}`, `{"seed":"42"}`} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolLoopSeed, badRaw); err == nil {
			t.Fatalf("seed %s succeeded, want error", badRaw)
		}
	}
}
