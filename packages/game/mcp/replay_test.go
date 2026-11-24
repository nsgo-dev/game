package mcp_test

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type replayStatsComp struct {
	Score    int    `json:"score"`
	Jumps    int    `json:"jumps"`
	Dashes   int    `json:"dashes"`
	RNGSum   uint64 `json:"rng_sum"`
	Grounded bool   `json:"grounded"`
}

type deterministicSim struct {
	world  *entities.World
	player entities.Entity
	rng    *rand.Rand
	seed   uint64
	step   int
	score  int
	jumps  int
	dashes int
	rngSum uint64
}

type deterministicSnapshot struct {
	Seed     uint64
	Step     int
	PosXBits uint32
	PosYBits uint32
	VelXBits uint32
	VelYBits uint32
	Score    int
	Jumps    int
	Dashes   int
	RNGSum   uint64
	Grounded bool
}

func newDeterministicSim(seed uint64) *deterministicSim {
	s := &deterministicSim{}
	s.Reset(seed)
	return s
}

func (s *deterministicSim) Reset(seed uint64) {
	if s.world != nil && !s.world.Disposed() {
		s.world.Dispose()
	}
	w := entities.NewWorld()
	_ = entities.RegisterCodec[posComp](w, "position")
	_ = entities.RegisterCodec[velComp](w, "velocity")
	_ = entities.RegisterCodec[replayStatsComp](w, "stats")

	p := w.Spawn()
	entities.Set(w, p, posComp{X: 16, Y: 0})
	entities.Set(w, p, velComp{VX: 0, VY: 0})
	entities.Set(w, p, replayStatsComp{Grounded: true})

	s.world = w
	s.player = p
	s.seed = seed
	s.rng = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	s.step = 0
	s.score = 0
	s.jumps = 0
	s.dashes = 0
	s.rngSum = 0
}

func (s *deterministicSim) Step(src replay.ActionSource, dt time.Duration) {
	src.Step()
	s.step++

	sec := float32(dt.Seconds())
	pos, _ := entities.Get[posComp](s.world, s.player)
	vel, _ := entities.Get[velComp](s.world, s.player)
	st, _ := entities.Get[replayStatsComp](s.world, s.player)

	move := float32(src.Axis("move_x"))
	vel.VX = move * 120
	if src.Pressed("dash") {
		vel.VX *= 2
		if src.JustPressed("dash") {
			s.dashes++
			s.rngSum += s.rng.Uint64() & 0xffff
		}
	}
	if src.JustPressed("jump") && st.Grounded {
		vel.VY = 240
		st.Grounded = false
		s.jumps++
		s.score += int(s.rng.IntN(50)) + 10
	}
	if src.JustReleased("jump") && vel.VY > 100 {
		vel.VY = 100
	}

	if !st.Grounded {
		vel.VY -= 600 * sec
	}
	pos.X += vel.VX * sec
	pos.Y += vel.VY * sec
	if pos.Y <= 0 {
		pos.Y = 0
		vel.VY = 0
		st.Grounded = true
	}

	if move != 0 {
		s.score += 1 + int(s.rng.IntN(3))
	}
	s.rngSum ^= s.rng.Uint64() & 0x7fffffff

	st.Score = s.score
	st.Jumps = s.jumps
	st.Dashes = s.dashes
	st.RNGSum = s.rngSum
}

func (s *deterministicSim) Snapshot() deterministicSnapshot {
	pos, _ := entities.Get[posComp](s.world, s.player)
	vel, _ := entities.Get[velComp](s.world, s.player)
	st, _ := entities.Get[replayStatsComp](s.world, s.player)
	return deterministicSnapshot{
		Seed:     s.seed,
		Step:     s.step,
		PosXBits: math.Float32bits(pos.X),
		PosYBits: math.Float32bits(pos.Y),
		VelXBits: math.Float32bits(vel.VX),
		VelYBits: math.Float32bits(vel.VY),
		Score:    st.Score,
		Jumps:    st.Jumps,
		Dashes:   st.Dashes,
		RNGSum:   st.RNGSum,
		Grounded: st.Grounded,
	}
}

func TestReplayRecordAndPlaybackDeterminism(t *testing.T) {
	actions := input.NewActions()
	actions.Bind("left", input.Key("left"))
	actions.Bind("right", input.Key("right"))
	actions.Bind("jump", input.Key("space"))
	actions.Bind("dash", input.Key("shift"))
	actions.BindAxis("move_x", "left", "right")

	sim := newDeterministicSim(0)
	defer func() {
		if sim.world != nil && !sim.world.Disposed() {
			sim.world.Dispose()
		}
	}()

	var (
		cx            *core.Scope
		ctrl          *mcp.ReplayController
		resetSeeds    []uint64
		recordStarts  []string
		recordStops   []string
		replayStarts  int
		replayStops   int
		fixedStepRate = 60
		fixedDT       = time.Second / time.Duration(fixedStepRate)
	)

	ctrl = mcp.NewReplayController(mcp.ReplayConfig{
		Source:   actions,
		Actions:  []string{"left", "right", "jump", "dash"},
		Axes:     []string{"move_x"},
		StepRate: fixedStepRate,
		SeedFunc: func() uint64 { return 999 },
		ResetFunc: func(seed uint64) error {
			resetSeeds = append(resetSeeds, seed)
			actions.Reset()
			sim.Reset(seed)
			return nil
		},
		StepFunc: func(dt time.Duration) {
			sim.Step(ctrl, dt)
		},
		OnRecordStart: func(name string, _ uint64) {
			recordStarts = append(recordStarts, name)
		},
		OnRecordStop: func(name string, _ *replay.Recording) {
			recordStops = append(recordStops, name)
		},
		OnReplayStart: func(*replay.Recording) {
			replayStarts++
		},
		OnReplayStop: func() {
			replayStops++
		},
	})

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{
			WorldFunc: func() *entities.World { return sim.world },
			Replay:    ctrl,
		}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Initial idle status.
	status0 := callTool[mcp.ReplayStatusResult](t, app, cx, mcp.ToolReplayStatus, nil)
	if status0.Recording || status0.Replaying || status0.FramesRecorded != 0 || len(status0.SavedRecordings) != 0 {
		t.Fatalf("initial status = %+v", status0)
	}

	// Start recording via MCP with deterministic reset and seed.
	const seed uint64 = 123456789
	startRes := callTool[mcp.ReplayRecordStartResult](t, app, cx, mcp.ToolReplayRecordStart, map[string]any{
		"name":      "run_alpha",
		"seed":      seed,
		"step_rate": fixedStepRate,
		"reset":     true,
		"metadata":  map[string]string{"level": "cavern_1", "agent": "tester"},
	})
	if !startRes.Recording || startRes.Name != "run_alpha" || startRes.Seed != seed || startRes.StepRate != fixedStepRate {
		t.Fatalf("startRes = %+v", startRes)
	}
	if startRes.Metadata["level"] != "cavern_1" || len(recordStarts) != 1 || recordStarts[0] != "run_alpha" {
		t.Fatalf("record start metadata/hook mismatch: %+v starts=%v", startRes, recordStarts)
	}

	// Drive a varied multi-step input sequence (including step-0 press, holds, taps, release, axis changes).
	app.Post(func() {
		// Steps 0..14: hold right; tap jump on step 0, release on step 5.
		actions.OnKeyDown(core.KeyEvent{Key: "right"})
		actions.OnKeyDown(core.KeyEvent{Key: "space"})
		for range 5 {
			sim.Step(ctrl, fixedDT)
		}
		actions.OnKeyUp(core.KeyEvent{Key: "space"})
		for range 10 {
			sim.Step(ctrl, fixedDT)
		}
	})
	app.Settle()

	// Check mid-recording status via MCP.
	statusMid := callTool[mcp.ReplayStatusResult](t, app, cx, mcp.ToolReplayStatus, nil)
	if !statusMid.Recording || statusMid.Replaying || statusMid.FramesRecorded != 15 || statusMid.RecordingName != "run_alpha" || statusMid.RecordingSeed != seed {
		t.Fatalf("mid-recording status = %+v", statusMid)
	}

	// Continue recording: dash right, then switch to left + jump, then idle coast.
	app.Post(func() {
		actions.OnKeyDown(core.KeyEvent{Key: "shift"})
		for range 8 {
			sim.Step(ctrl, fixedDT)
		}
		actions.OnKeyUp(core.KeyEvent{Key: "right"})
		actions.OnKeyUp(core.KeyEvent{Key: "shift"})
		actions.OnKeyDown(core.KeyEvent{Key: "left"})
		actions.Trigger("jump") // single-step latched tap
		for range 12 {
			sim.Step(ctrl, fixedDT)
		}
		actions.OnKeyUp(core.KeyEvent{Key: "left"})
		for range 10 {
			sim.Step(ctrl, fixedDT)
		}
	})
	app.Settle()

	// Capture expected final state and MCP entity inspection output before stopping.
	wantSnap := sim.Snapshot()
	wantEntity := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{
		"ref": mcp.FormatEntityRef(sim.player),
	})
	if wantSnap.Step != 45 || wantSnap.Jumps == 0 || wantSnap.Dashes == 0 || wantSnap.Score == 0 {
		t.Fatalf("expected non-trivial simulation state, got %+v", wantSnap)
	}

	// Stop recording via MCP.
	stopRes := callTool[mcp.ReplayRecordStopResult](t, app, cx, mcp.ToolReplayRecordStop, nil)
	if stopRes.Name != "run_alpha" || stopRes.Frames != 45 || stopRes.Seed != seed || stopRes.StepRate != fixedStepRate {
		t.Fatalf("stopRes = %+v", stopRes)
	}
	if stopRes.Events == 0 || !strings.Contains(stopRes.YAML, "version: 2") || !strings.Contains(stopRes.YAML, "total_steps: 45") {
		t.Fatalf("unexpected YAML in stopRes: %s", stopRes.YAML)
	}
	if len(recordStops) != 1 || recordStops[0] != "run_alpha" {
		t.Fatalf("recordStops = %v", recordStops)
	}

	// List recordings via MCP.
	listRes := callTool[mcp.ReplayListResult](t, app, cx, mcp.ToolReplayList, nil)
	if listRes.Count != 1 || len(listRes.Recordings) != 1 || listRes.Recordings[0].Name != "run_alpha" || listRes.Recordings[0].Frames != 45 || listRes.Recordings[0].Metadata["agent"] != "tester" {
		t.Fatalf("listRes = %+v", listRes)
	}

	// Scramble the game state with a different seed and random steps so it diverges completely.
	app.Post(func() {
		sim.Reset(7777)
		actions.OnKeyDown(core.KeyEvent{Key: "left"})
		for range 20 {
			sim.Step(ctrl, fixedDT)
		}
		actions.Reset()
	})
	app.Settle()
	if reflect.DeepEqual(sim.Snapshot(), wantSnap) {
		t.Fatal("scrambled state unexpectedly matched wantSnap")
	}

	// 1) Replay by stored name with run_to_end: true.
	playNameRes := callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{
		"name":       "run_alpha",
		"run_to_end": true,
	})
	if !playNameRes.Completed || playNameRes.Replaying || playNameRes.FramesPlayed != 45 || playNameRes.ReplayTotalFrames != 45 || playNameRes.Seed != seed {
		t.Fatalf("playNameRes = %+v", playNameRes)
	}
	gotSnapName := sim.Snapshot()
	if gotSnapName != wantSnap {
		t.Fatalf("replay by name state mismatch:\n got: %+v\nwant: %+v", gotSnapName, wantSnap)
	}
	gotEntityName := callTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{
		"ref": mcp.FormatEntityRef(sim.player),
	})
	if !reflect.DeepEqual(gotEntityName, wantEntity) {
		t.Fatalf("replay by name entity inspect mismatch:\n got: %+v\nwant: %+v", gotEntityName, wantEntity)
	}

	// 2) Scramble again and replay from inline YAML text.
	app.Post(func() {
		sim.Reset(1)
		for range 5 {
			sim.Step(ctrl, fixedDT)
		}
	})
	app.Settle()

	playYAMLRes := callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{
		"yaml":       stopRes.YAML,
		"run_to_end": true,
	})
	if !playYAMLRes.Completed || playYAMLRes.Replaying || playYAMLRes.FramesPlayed != 45 || playYAMLRes.Seed != seed {
		t.Fatalf("playYAMLRes = %+v", playYAMLRes)
	}
	if got := sim.Snapshot(); got != wantSnap {
		t.Fatalf("replay from inline YAML mismatch:\n got: %+v\nwant: %+v", got, wantSnap)
	}

	// 3) Scramble again and replay frame-by-frame (run_to_end: false).
	app.Post(func() {
		sim.Reset(2)
	})
	app.Settle()

	playStepRes := callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{
		"name": "run_alpha",
	})
	if !playStepRes.Replaying || playStepRes.Completed || playStepRes.FramesPlayed != 0 || playStepRes.ReplayTotalFrames != 45 {
		t.Fatalf("frame-by-frame play start = %+v", playStepRes)
	}

	// Advance 20 frames manually.
	app.Post(func() {
		for range 20 {
			sim.Step(ctrl, fixedDT)
		}
	})
	app.Settle()

	midPlay := callTool[mcp.ReplayStatusResult](t, app, cx, mcp.ToolReplayStatus, nil)
	if !midPlay.Replaying || midPlay.ReplayDone || midPlay.ReplayFrame != 20 || midPlay.ReplayTotalFrames != 45 || midPlay.ReplayName != "run_alpha" {
		t.Fatalf("mid-replay status = %+v", midPlay)
	}

	// Advance remaining 25 frames to reach exact end state.
	app.Post(func() {
		for range 25 {
			sim.Step(ctrl, fixedDT)
		}
	})
	app.Settle()

	endPlay := callTool[mcp.ReplayStatusResult](t, app, cx, mcp.ToolReplayStatus, nil)
	if endPlay.Replaying || !endPlay.ReplayDone || endPlay.ReplayFrame != 45 || endPlay.ReplayTotalFrames != 45 {
		t.Fatalf("end-replay status = %+v", endPlay)
	}
	if got := sim.Snapshot(); got != wantSnap {
		t.Fatalf("frame-by-frame replay mismatch:\n got: %+v\nwant: %+v", got, wantSnap)
	}

	// Stepping one more tick transitions cleanly back to live input and fires OnReplayStop.
	app.Post(func() {
		actions.OnKeyDown(core.KeyEvent{Key: "right"})
		sim.Step(ctrl, fixedDT)
		if !ctrl.Pressed("right") {
			t.Error("expected live input to resume after replay finished")
		}
		actions.Reset()
	})
	app.Settle()

	afterLive := callTool[mcp.ReplayStatusResult](t, app, cx, mcp.ToolReplayStatus, nil)
	if afterLive.Replaying || afterLive.ReplayDone || afterLive.ReplayFrame != 0 || afterLive.ReplayTotalFrames != 0 {
		t.Fatalf("status after returning to live input = %+v", afterLive)
	}
	if replayStarts != 3 || replayStops != 3 {
		t.Fatalf("replay hooks = (starts=%d, stops=%d), want (3, 3)", replayStarts, replayStops)
	}
	if len(resetSeeds) != 4 { // 1 record(reset:true) + 3 replays
		t.Fatalf("resetSeeds = %v, want 4 resets", resetSeeds)
	}
	for i, gotSeed := range resetSeeds {
		if gotSeed != seed {
			t.Fatalf("resetSeeds[%d] = %d, want %d", i, gotSeed, seed)
		}
	}
}

func TestReplayStepChunkLoopFallbackAndEarlyStop(t *testing.T) {
	actions := input.NewActions()
	actions.Bind("jump", input.Key("space"))

	var (
		cx         *core.Scope
		ctrl       *mcp.ReplayController
		stepsRun   int
		jumpPress  int
		stopCalled int
	)

	loop := &game.Loop{
		Rate: 30,
		Update: func(time.Duration) {
			ctrl.Step()
			stepsRun++
			if ctrl.JustPressed("jump") {
				jumpPress++
			}
		},
	}

	ctrl = mcp.NewReplayController(mcp.ReplayConfig{
		Source:  actions,
		Actions: []string{"jump"},
		Loop:    loop,
		ResetFunc: func(uint64) error {
			stepsRun = 0
			jumpPress = 0
			return nil
		},
		OnReplayStop: func() {
			stopCalled++
		},
	})

	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{Replay: ctrl}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Start with omitted name and step_rate (should auto-name "recording_1" and inherit Loop.Rate=30).
	recStart := callTool[mcp.ReplayRecordStartResult](t, app, cx, mcp.ToolReplayRecordStart, nil)
	if recStart.Name != "recording_1" || recStart.StepRate != 30 {
		t.Fatalf("recStart = %+v", recStart)
	}
	app.Post(func() {
		for i := range 20 {
			if i == 2 || i == 10 {
				actions.Trigger("jump")
			}
			loop.Update(time.Second / 30)
		}
	})
	app.Settle()

	recStop := callTool[mcp.ReplayRecordStopResult](t, app, cx, mcp.ToolReplayRecordStop, map[string]any{
		"name": "jump_demo",
	})
	if recStop.Name != "jump_demo" || recStop.Frames != 20 || recStop.StepRate != 30 {
		t.Fatalf("recStop = %+v", recStop)
	}

	// Advance only 5 steps via Loop.Update fallback, then stop early.
	playRes := callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{
		"name":  "jump_demo",
		"steps": 5,
	})
	if !playRes.Replaying || playRes.Completed || playRes.FramesPlayed != 5 || playRes.ReplayTotalFrames != 20 {
		t.Fatalf("partial playRes = %+v", playRes)
	}
	if stepsRun != 5 || jumpPress != 1 {
		t.Fatalf("after 5 steps: stepsRun=%d jumpPress=%d, want 5, 1", stepsRun, jumpPress)
	}

	stopRes := callTool[mcp.ReplayStopResult](t, app, cx, mcp.ToolReplayStop, nil)
	if !stopRes.Stopped || stopRes.Name != "jump_demo" || stopRes.FramesPlayed != 5 || stopRes.ReplayTotalFrames != 20 {
		t.Fatalf("stopRes = %+v", stopRes)
	}
	if stopCalled != 1 || ctrl.Replaying() {
		t.Fatalf("expected OnReplayStop once and idle state, stopCalled=%d replaying=%v", stopCalled, ctrl.Replaying())
	}
}

func TestReplaySaveDirAndPathSecurity(t *testing.T) {
	saveDir := t.TempDir()
	actions := input.NewActions()
	actions.Bind("fire", input.Key("f"))

	ctrl := mcp.NewReplayController(mcp.ReplayConfig{
		Source:  actions,
		Actions: []string{"fire"},
		SaveDir: saveDir,
	})

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if _, err := mcp.Attach(cx, mcp.Config{Replay: ctrl}); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	_ = callTool[mcp.ReplayRecordStartResult](t, app, cx, mcp.ToolReplayRecordStart, map[string]any{
		"name": "boss_fight",
		"seed": 42,
	})
	app.Post(func() {
		actions.Trigger("fire")
		ctrl.Step()
		ctrl.Step()
	})
	app.Settle()

	// Unsafe paths must fail WITHOUT stopping the active recording.
	for _, badPath := range []string{"../escape.replay.yaml", "/tmp/abs.replay.yaml", ".", ".."} {
		if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStop, map[string]any{"save_path": badPath}); err == nil {
			t.Fatalf("expected error for unsafe save_path %q", badPath)
		}
		if !ctrl.Recording() {
			t.Fatalf("unsafe save_path %q unexpectedly aborted active recording", badPath)
		}
	}

	// Safe nested relative path succeeds and matches replay.LoadFile byte-for-byte.
	stopRes := callTool[mcp.ReplayRecordStopResult](t, app, cx, mcp.ToolReplayRecordStop, map[string]any{
		"save_path": "runs/boss_fight.replay.yaml",
	})
	if stopRes.SavedPath != "runs/boss_fight.replay.yaml" {
		t.Fatalf("SavedPath = %q, want runs/boss_fight.replay.yaml", stopRes.SavedPath)
	}
	diskPath := filepath.Join(saveDir, "runs", "boss_fight.replay.yaml")
	diskBytes, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read saved replay: %v", err)
	}
	if string(diskBytes) != stopRes.YAML {
		t.Fatalf("disk YAML differs from response YAML:\ndisk:\n%s\nresp:\n%s", diskBytes, stopRes.YAML)
	}
	loaded, err := replay.LoadFile(diskPath)
	if err != nil {
		t.Fatalf("replay.LoadFile: %v", err)
	}
	if loaded.Seed != 42 || loaded.TotalSteps != 2 || len(loaded.Events) != 2 {
		t.Fatalf("loaded recording = %+v", loaded)
	}
}

func TestReplayControllerHelpersPrebuiltRecorderAndZeroFrame(t *testing.T) {
	// Verify all registered tool schemas are valid JSON.
	bridge := mcp.New(mcp.Config{})
	for _, tool := range bridge.Tools() {
		if !json.Valid(tool.InputSchema) {
			t.Fatalf("tool %q has invalid JSON InputSchema: %s", tool.Name, tool.InputSchema)
		}
	}

	// Pre-built Recorder fallback + Go helper methods.
	actions := input.NewActions()
	actions.Bind("a", input.Key("a"))
	rec := replay.NewRecorder(actions, 7, 60, []string{"a"}, nil)
	ctrl := mcp.NewReplayController(mcp.ReplayConfig{
		Recorder: rec,
	})
	bridge.SetReplay(ctrl)
	if bridge.Replay() != ctrl {
		t.Fatal("Bridge.Replay() mismatch")
	}

	fixture := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       55,
		Rate:       60,
		Actions:    []string{"a"},
		TotalSteps: 0,
	}
	if err := ctrl.SaveRecording("recording_1", fixture); err != nil {
		t.Fatal(err)
	}
	if got, ok := ctrl.GetRecording("recording_1"); !ok || got.Seed != 55 {
		t.Fatalf("GetRecording = (%+v, %v)", got, ok)
	}

	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		if err := bridge.Attach(cx); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})

	// Auto-name skips existing "recording_1" and allocates "recording_2".
	startRes := callTool[mcp.ReplayRecordStartResult](t, app, cx, mcp.ToolReplayRecordStart, map[string]any{
		"seed": 88,
	})
	if startRes.Name != "recording_2" || startRes.Seed != 88 {
		t.Fatalf("startRes = %+v", startRes)
	}
	// Stop immediately with 0 frames.
	stopRes := callTool[mcp.ReplayRecordStopResult](t, app, cx, mcp.ToolReplayRecordStop, nil)
	if stopRes.Frames != 0 || stopRes.Name != "recording_2" {
		t.Fatalf("zero-frame stopRes = %+v", stopRes)
	}
	// Replay 0-frame recording completes immediately.
	playRes := callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{
		"name": "recording_2",
	})
	if !playRes.Completed || playRes.Replaying || playRes.FramesPlayed != 0 {
		t.Fatalf("zero-frame playRes = %+v", playRes)
	}

	if !ctrl.DeleteRecording("recording_1") || ctrl.DeleteRecording("recording_1") {
		t.Fatal("DeleteRecording return value mismatch")
	}
	if names := ctrl.RecordingNames(); len(names) != 1 || names[0] != "recording_2" {
		t.Fatalf("RecordingNames = %v", names)
	}
}

func TestReplayErrorHandling(t *testing.T) {
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

	// 1. Unconfigured replay controller: all 6 tools return a clear error without panicking.
	for _, toolName := range []string{
		mcp.ToolReplayStatus,
		mcp.ToolReplayRecordStart,
		mcp.ToolReplayRecordStop,
		mcp.ToolReplayPlay,
		mcp.ToolReplayStop,
		mcp.ToolReplayList,
	} {
		if _, err := callToolRaw(t, app, cx, toolName, nil); err == nil || !strings.Contains(err.Error(), "no replay controller configured") {
			t.Fatalf("%s with nil Replay = %v, want unconfigured error", toolName, err)
		}
	}

	// 2. Unknown JSON fields rejected on every replay tool.
	for _, toolName := range []string{
		mcp.ToolReplayStatus,
		mcp.ToolReplayRecordStart,
		mcp.ToolReplayRecordStop,
		mcp.ToolReplayPlay,
		mcp.ToolReplayStop,
		mcp.ToolReplayList,
	} {
		if _, err := callToolRaw(t, app, cx, toolName, `{"bogus_field":1}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("%s with unknown field = %v, want unknown field error", toolName, err)
		}
	}

	// 3. Configure controller with no Source/Recorder -> record_start fails cleanly.
	bridge.SetReplay(mcp.NewReplayController(mcp.ReplayConfig{}))
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStart, nil); err == nil || !strings.Contains(err.Error(), "no input source") {
		t.Fatalf("record_start without source = %v", err)
	}

	// 4. Configure controller with live Source and failing/optional ResetFunc.
	actions := input.NewActions()
	resetErr := errors.New("boom reset")
	failReset := false
	ctrl := mcp.NewReplayController(mcp.ReplayConfig{
		Source:  actions,
		Actions: []string{"jump"},
		ResetFunc: func(uint64) error {
			if failReset {
				return resetErr
			}
			return nil
		},
	})
	bridge.SetReplay(ctrl)

	// Stop when not recording / not replaying.
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStop, nil); err == nil || !strings.Contains(err.Error(), "no recording in progress") {
		t.Fatalf("record_stop idle = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayStop, nil); err == nil || !strings.Contains(err.Error(), "no replay in progress") {
		t.Fatalf("replay_stop idle = %v", err)
	}

	// Negative step_rate rejected.
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStart, map[string]any{"step_rate": -5}); err == nil {
		t.Fatal("expected error for negative step_rate")
	}

	// Start recording, then verify conflicting start/play and save_path without SaveDir fail cleanly.
	_ = callTool[mcp.ReplayRecordStartResult](t, app, cx, mcp.ToolReplayRecordStart, map[string]any{"name": "rec1"})
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStart, nil); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("double record_start = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1"}); err == nil || !strings.Contains(err.Error(), "recording is already in progress") {
		t.Fatalf("play while recording = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStop, map[string]any{"save_path": "a.yaml"}); err == nil || !strings.Contains(err.Error(), "SaveDir") {
		t.Fatalf("save_path without SaveDir = %v", err)
	}
	app.Post(func() {
		ctrl.Step()
		ctrl.Step()
	})
	app.Settle()
	_ = callTool[mcp.ReplayRecordStopResult](t, app, cx, mcp.ToolReplayRecordStop, nil)

	// Invalid play arguments.
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, `{}`); err == nil || !strings.Contains(err.Error(), "either name or yaml") {
		t.Fatalf("play empty = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1", "yaml": "version: 2"}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("play both name and yaml = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("play missing name = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"yaml": "version: 1\nseed: 0\n"}); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("play v1 yaml = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"yaml": "not: [valid: yaml"}); err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1", "run_to_end": true, "steps": 2}); err == nil {
		t.Fatal("expected error when both run_to_end and steps are set")
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1", "steps": -1}); err == nil {
		t.Fatal("expected error for negative steps")
	}
	// run_to_end without StepFunc or Loop.Update fails cleanly before starting replay.
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1", "run_to_end": true}); err == nil || !strings.Contains(err.Error(), "StepFunc") {
		t.Fatalf("run_to_end without StepFunc = %v", err)
	}
	if ctrl.Replaying() {
		t.Fatal("failed play call should not leave controller replaying")
	}

	// Failing ResetFunc rolls back playback state cleanly.
	failReset = true
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1"}); err == nil || !strings.Contains(err.Error(), "boom reset") {
		t.Fatalf("play with failing reset = %v", err)
	}
	if ctrl.Replaying() {
		t.Fatal("failed ResetFunc should roll back replay state")
	}
	failReset = false

	// Start active playback and verify conflicting record_start / play fail cleanly.
	_ = callTool[mcp.ReplayPlayResult](t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1"})
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayRecordStart, nil); err == nil || !strings.Contains(err.Error(), "replay is already in progress") {
		t.Fatalf("record_start while replaying = %v", err)
	}
	if _, err := callToolRaw(t, app, cx, mcp.ToolReplayPlay, map[string]any{"name": "rec1"}); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("double play = %v", err)
	}
	_ = callTool[mcp.ReplayStopResult](t, app, cx, mcp.ToolReplayStop, nil)
}
