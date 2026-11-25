package editor_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// scriptedActionSource is a deterministic replay.ActionSource for Play-mode rolling replay tests.
type scriptedActionSource struct {
	step     int
	pressed  map[string]bool
	prev     map[string]bool
	axes     map[string]float64
	schedule func(step int, pressed map[string]bool, axes map[string]float64)
}

func newScriptedActionSource(fn func(step int, pressed map[string]bool, axes map[string]float64)) *scriptedActionSource {
	return &scriptedActionSource{
		pressed:  map[string]bool{},
		prev:     map[string]bool{},
		axes:     map[string]float64{},
		schedule: fn,
	}
}

func (s *scriptedActionSource) Step() {
	clear(s.prev)
	for k, v := range s.pressed {
		s.prev[k] = v
	}
	if s.schedule != nil {
		s.schedule(s.step, s.pressed, s.axes)
	}
	s.step++
}

func (s *scriptedActionSource) Pressed(name string) bool { return s.pressed[name] }
func (s *scriptedActionSource) JustPressed(name string) bool {
	return s.pressed[name] && !s.prev[name]
}
func (s *scriptedActionSource) JustReleased(name string) bool {
	return !s.pressed[name] && s.prev[name]
}
func (s *scriptedActionSource) Axis(name string) float64 { return s.axes[name] }

func writePlayMarksProject(t *testing.T) (string, *editor.Editor) {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		project.FileName: `version: 1
name: dungeon-run
id: dev.example.dungeonrun
appVersion: 1.0.0
window:
  width: 640
  height: 360
autoload:
  mainScene: level_01
  scenes:
    - name: level_01
      path: scenes/level_01.scene.yaml
`,
		project.NSGoFileName: `name: dungeon-run
id: dev.example.dungeonrun
version: 1.0.0
`,
		"scenes/level_01.scene.yaml": `version: 2
name: level_01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: actors
    objects:
      - id: knight
        name: Knight
        x: 100
        y: 200
      - id: slime_02
        name: slime_02
        x: 320
        y: 200
`,
		"game/lever/system.go": "package lever\n// fixture file for panic link navigation\n",
	}, nil)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := ed.OpenProjectFile("scenes/level_01.scene.yaml"); err != nil {
		t.Fatalf("OpenProjectFile: %v", err)
	}
	return dir, ed
}

// #1042: Rolling 60-second input buffer holds up to 3,600 frames at 60 Hz, rebases older events cleanly
// while preserving held actions and axes entering the window, and encodes/decodes as valid replay v2 YAML.
func TestPlayRollingReplay60SecondCapAndRebase(t *testing.T) {
	_, ed := writePlayMarksProject(t)
	ed.PlayRate = 60
	ed.PlaySeed = 42
	ed.PlayInputActions = []string{"right", "jump"}
	ed.PlayInputAxes = []string{"move_x"}
	ed.PlayInputSource = newScriptedActionSource(func(step int, pressed map[string]bool, axes map[string]float64) {
		// Hold "right" and move_x=1 starting at step 10 (before the 60s window slides past it).
		if step == 10 {
			pressed["right"] = true
			axes["move_x"] = 1
		}
		// Press "jump" at step 3700 (inside the final 60s window of 3800 steps) and release at 3710.
		if step == 3700 {
			pressed["jump"] = true
		}
		if step == 3710 {
			pressed["jump"] = false
		}
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760))
	t.Cleanup(func() { _ = ed.Close() })

	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.SetPlayPaused(true)
		ed.StepPlay(3800)
	})
	app.Settle()

	if got := ed.PlaySteps(); got != 3800 {
		t.Fatalf("PlaySteps = %d, want 3800", got)
	}
	if got := ed.PlayRollingFrameCount(); got != 3600 {
		t.Fatalf("PlayRollingFrameCount = %d, want 3600 (60 s cap)", got)
	}
	if got := ed.PlayRollingSummary(); got != "Last 60 s kept · 3 600 frames" {
		t.Fatalf("PlayRollingSummary = %q, want %q", got, "Last 60 s kept · 3 600 frames")
	}

	rec := ed.PlayRollingRecording()
	data, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode rolling recording: %v", err)
	}
	decoded, err := replay.Decode(data)
	if err != nil {
		t.Fatalf("Decode rolling recording: %v", err)
	}
	if decoded.Version != replay.FormatVersion || decoded.Seed != 42 || decoded.Rate != 60 || decoded.TotalSteps != 3600 {
		t.Fatalf("decoded header = %+v", decoded)
	}

	// Verify playback reproduces held "right" and move_x=1 at rebased step 0, and "jump" at rebased step 3500 (3700 - 200).
	player := replay.NewPlayer(decoded)
	var jumpStart, jumpEnd int
	jumpStart, jumpEnd = -1, -1
	for s := 0; s < decoded.TotalSteps; s++ {
		player.Step()
		if s == 0 {
			if !player.Pressed("right") || player.Axis("move_x") != 1 {
				t.Fatalf("at rebased step 0: right=%v move_x=%v, want true, 1", player.Pressed("right"), player.Axis("move_x"))
			}
		}
		if player.JustPressed("jump") {
			jumpStart = s
		}
		if player.JustReleased("jump") {
			jumpEnd = s
		}
	}
	if jumpStart != 3500 || jumpEnd != 3510 {
		t.Fatalf("rebased jump window = [%d, %d), want [3500, 3510)", jumpStart, jumpEnd)
	}
}

// #1042 Acceptance Criterion 1:
// Pressing ⌥F (alt+f) twice during Play and stopping sends ONE thread containing 2 marks,
// each with a valid replay v2 YAML file on disk that replays cleanly to its marked frame.
func TestPlayMarksAltFTwiceAndStopSendsOneThreadWithValidReplays(t *testing.T) {
	dir, ed := writePlayMarksProject(t)
	ed.PlayRate = 60
	ed.PlaySeed = 77
	ed.PlayInputActions = []string{"right", "jump"}
	ed.PlayInputSource = newScriptedActionSource(func(step int, pressed map[string]bool, _ map[string]float64) {
		switch step {
		case 5:
			pressed["right"] = true
		case 20:
			pressed["jump"] = true
		case 25:
			pressed["jump"] = false
		case 40:
			pressed["right"] = false
		}
	})

	var threads []editor.PlayMarksThread
	ed.OnPlayMarksThread = func(th editor.PlayMarksThread) {
		threads = append(threads, th)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	t.Cleanup(func() { _ = ed.Close() })

	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.SetPlayPaused(true)
		ed.StepPlay(15)
	})
	app.Settle()

	// Press alt+f for Mark 1 at frame 15 (must NOT stop Play).
	app.Key("alt+f")
	if !ed.Playing() {
		t.Fatal("alt+f must not stop Play mode")
	}

	// Step to frame 50 and press alt+f for Mark 2.
	app.Post(func() {
		ed.StepPlay(35)
	})
	app.Settle()
	app.Key("alt+f")
	if !ed.Playing() {
		t.Fatal("second alt+f must not stop Play mode")
	}

	// Verify MARKS · 2 is shown in Agent tab and nothing has been sent yet.
	if len(threads) != 0 {
		t.Fatalf("marks thread sent prematurely before Stop: %d", len(threads))
	}
	marks := ed.PlayMarks()
	if len(marks) != 2 {
		t.Fatalf("PlayMarks len = %d, want 2", len(marks))
	}
	if marks[0].Frame != 15 || marks[1].Frame != 50 {
		t.Fatalf("mark frames = [%d, %d], want [15, 50]", marks[0].Frame, marks[1].Frame)
	}

	// Open Agent tab and verify MARKS · 2 UI rows render.
	app.Press("id:play-tab-agent")
	app.Find("id:play-marks-header")
	app.Find("id:play-mark-row-0")
	app.Find("id:play-mark-row-1")

	// Stop Play -> sends ONE thread with both marks.
	app.Press("id:play-stop-btn")
	if ed.Playing() {
		t.Fatal("expected Play to stop after pressing Stop")
	}
	if len(threads) != 1 {
		t.Fatalf("OnPlayMarksThread call count = %d, want 1", len(threads))
	}
	th := threads[0]
	if len(th.Marks) != 2 {
		t.Fatalf("thread marks len = %d, want 2", len(th.Marks))
	}
	last := ed.LastPlayMarksThread()
	if last == nil || len(last.Marks) != 2 {
		t.Fatalf("LastPlayMarksThread = %+v, want 2 marks", last)
	}

	// Verify each mark's replay file exists under replays/, decodes as version 2 YAML, and replays cleanly to its marked frame.
	wantFrames := []int{15, 50}
	for i, m := range th.Marks {
		if !strings.HasPrefix(m.ReplayPath, "replays/") || !strings.HasSuffix(m.ReplayPath, ".replay.yaml") {
			t.Fatalf("mark %d ReplayPath = %q, want replays/*.replay.yaml", i, m.ReplayPath)
		}
		diskPath := filepath.Join(dir, filepath.FromSlash(m.ReplayPath))
		if m.AbsPath != diskPath {
			t.Fatalf("mark %d AbsPath = %q, want %q", i, m.AbsPath, diskPath)
		}
		rec, err := replay.LoadFile(diskPath)
		if err != nil {
			t.Fatalf("mark %d replay.LoadFile(%q): %v", i, diskPath, err)
		}
		if rec.Version != replay.FormatVersion {
			t.Fatalf("mark %d replay version = %d, want %d", i, rec.Version, replay.FormatVersion)
		}
		if rec.TotalSteps != wantFrames[i] {
			t.Fatalf("mark %d replay TotalSteps = %d, want %d", i, rec.TotalSteps, wantFrames[i])
		}
		p := replay.NewPlayer(rec)
		rightSteps := 0
		jumpSteps := 0
		for !p.Done() {
			p.Step()
			if p.Pressed("right") {
				rightSteps++
			}
			if p.Pressed("jump") {
				jumpSteps++
			}
		}
		if p.StepIndex() != wantFrames[i] {
			t.Fatalf("mark %d replayed StepIndex = %d, want %d", i, p.StepIndex(), wantFrames[i])
		}
		if i == 0 && (rightSteps != 10 || jumpSteps != 0) {
			t.Fatalf("mark 0 replay inputs: right=%d jump=%d, want 10, 0", rightSteps, jumpSteps)
		}
		if i == 1 && (rightSteps != 35 || jumpSteps != 5) {
			t.Fatalf("mark 1 replay inputs: right=%d jump=%d, want 35, 5", rightSteps, jumpSteps)
		}
	}
}

// #1042: "Send marks now" dispatches unsent marks immediately without stopping Play,
// and stopping afterward does not re-send already-sent marks.
func TestPlaySendMarksNowDispatchesEarlyWithoutStopping(t *testing.T) {
	_, ed := writePlayMarksProject(t)
	sentThreads := 0
	ed.OnPlayMarksThread = func(th editor.PlayMarksThread) {
		sentThreads++
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760))
	t.Cleanup(func() { _ = ed.Close() })

	app.Post(func() {
		_ = ed.Play()
		ed.SetPlayPaused(true)
		ed.StepPlay(12)
		_, _ = ed.AddPlayMark()
		ed.SetPlayInspectorTab(editor.PlayTabAgent)
	})
	app.Settle()

	app.Press("id:play-send-marks-btn")
	if sentThreads != 1 {
		t.Fatalf("Send marks now call count = %d, want 1", sentThreads)
	}
	if !ed.Playing() {
		t.Fatal("Send marks now must keep Play active")
	}
	if len(ed.UnsentPlayMarks()) != 0 {
		t.Fatalf("UnsentPlayMarks = %d, want 0", len(ed.UnsentPlayMarks()))
	}

	app.Post(func() { _ = ed.Stop() })
	app.Settle()
	if sentThreads != 1 {
		t.Fatalf("Stop re-sent already sent marks: count = %d, want 1", sentThreads)
	}
}

// #1042: "Something looks wrong ⌘L" pauses Play, opens "Ask about frame N" with captured frame PNG,
// submits with replay + live state attached, and agent stepping displays the viewport pill & step log
// while keeping the game paused until the person resumes.
func TestPlayAskFrozenFrameAndAgentStepping(t *testing.T) {
	_, ed := writePlayMarksProject(t)
	ed.SelectObject("knight")
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, dt time.Duration) {
		if k := scene.Object("knight"); k != nil {
			k.Doc.X += 2
		}
	}

	var gotReq *editor.AskFrozenFrameRequest
	ed.OnAskFrozenFrame = func(req editor.AskFrozenFrameRequest) {
		cp := req
		gotReq = &cp
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	t.Cleanup(func() { _ = ed.Close() })

	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.StepPlay(20)
	})
	app.Settle()

	// Trigger "Something looks wrong ⌘L" via shortcut.
	app.Key("mod+l")
	if !ed.PlayPaused() {
		t.Fatal("⌘L must pause Play mode")
	}
	if !ed.AskFrozenFrameOpen() {
		t.Fatal("⌘L must open Ask about frame dialog")
	}
	app.Find("id:play-ask-dialog")
	app.Find("id:play-ask-review-note")

	// Submit question.
	app.Post(func() {
		ed.SetAskFrozenFrameDraft("The knight snags on the plank's left edge.")
	})
	app.Settle()
	app.Press("id:play-ask-submit")

	if ed.AskFrozenFrameOpen() {
		t.Fatal("Ask dialog should close on submit")
	}
	if !ed.PlayPaused() {
		t.Fatal("game must remain paused after submitting question")
	}
	if gotReq == nil {
		t.Fatal("OnAskFrozenFrame was not invoked")
	}
	if gotReq.Prompt != "The knight snags on the plank's left edge." {
		t.Fatalf("prompt = %q", gotReq.Prompt)
	}
	if len(gotReq.FramePNG) < 8 || !bytes.Equal(gotReq.FramePNG[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("expected valid PNG header on FramePNG (len=%d)", len(gotReq.FramePNG))
	}
	if !strings.HasPrefix(gotReq.ReplayPath, "replays/") {
		t.Fatalf("ReplayPath = %q, want replays/...", gotReq.ReplayPath)
	}
	if len(gotReq.LiveEntities) < 2 {
		t.Fatalf("LiveEntities = %d, want >= 2", len(gotReq.LiveEntities))
	}

	// Simulate the agent stepping frames 20..32 via AgentStepPlayReplay.
	app.Post(func() {
		cur, err := ed.AgentStepPlayReplay(20, 32, "knight reaches x 164")
		if err != nil {
			t.Errorf("AgentStepPlayReplay: %v", err)
		}
		if cur != 32 {
			t.Errorf("current frame after step = %d, want 32", cur)
		}
	})
	app.Settle()

	if !ed.PlayPaused() {
		t.Fatal("game must stay paused after agent steps the replay")
	}
	if pill := ed.PlayAgentPill(); pill != "agent stepping frames 20–32" {
		t.Fatalf("PlayAgentPill = %q, want %q", pill, "agent stepping frames 20–32")
	}
	app.Find("id:play-agent-pill")

	foundStepLog := false
	for _, st := range ed.PlayAgentSteps() {
		if strings.Contains(st.Text, "Stepped frames 20–32 with game_loop_step") {
			foundStepLog = true
			break
		}
	}
	if !foundStepLog {
		t.Fatalf("expected step log mentioning game_loop_step, got %+v", ed.PlayAgentSteps())
	}

	// Resuming clears the agent stepping pill.
	app.Post(func() {
		ed.ResumePlay()
	})
	app.Settle()
	if ed.PlayPaused() {
		t.Fatal("ResumePlay should unpause")
	}
	if ed.PlayAgentPill() != "" {
		t.Fatalf("PlayAgentPill after resume = %q, want empty", ed.PlayAgentPill())
	}
}

// #1042: Output panel parses Go panic stack traces, links game frames, folds runtime frames with Show/Hide toggle,
// and dispatches "Ask the agent to fix".
func TestOutputPanicCardParsingFoldingAndLinks(t *testing.T) {
	dir, ed := writePlayMarksProject(t)

	traceLines := []string{
		"14:02:17 frame 2940",
		"panic: runtime error: index out of range [3] with length 3",
		"goroutine 1 [running]:",
		"example.com/dungeon/game/lever.(*System).toggle(0xc00012a000, 0x3)",
		fmt.Sprintf("\t%s/game/lever/system.go:83 +0x4c", dir),
		"example.com/dungeon/game/lever.(*System).Update(0xc00012a000, 0x1)",
		fmt.Sprintf("\t%s/game/lever/system.go:57 +0x24", dir),
		"github.com/nsgo-dev/game/packages/game.(*Loop).StepNow(0xc000090000, 0x1)",
		"\t/go/pkg/packages/game/loop.go:175 +0x90",
		"github.com/nsgo-dev/nsgo/core.(*Scope).frame(0xc000080000)",
		"\t/go/pkg/nsgo/core/scope.go:221 +0x38",
		"runtime.gopanic(0x1, 0x2)",
		"\t/usr/local/go/src/runtime/panic.go:770 +0x132",
		"runtime.goPanicIndex(0x3, 0x3)",
		"\t/usr/local/go/src/runtime/panic.go:115 +0x74",
	}

	rep, ok := editor.ParsePanicTrace(traceLines, dir)
	if !ok {
		t.Fatal("ParsePanicTrace returned ok=false")
	}
	if rep.Frame != 2940 {
		t.Fatalf("parsed Frame = %d, want 2940", rep.Frame)
	}
	if rep.Message != "panic: runtime error: index out of range [3] with length 3" {
		t.Fatalf("Message = %q", rep.Message)
	}
	if got := len(rep.GameFrames()); got != 2 {
		t.Fatalf("GameFrames = %d, want 2 (%+v)", got, rep.GameFrames())
	}
	if got := len(rep.RuntimeFrames()); got != 4 {
		t.Fatalf("RuntimeFrames = %d, want 4 (%+v)", got, rep.RuntimeFrames())
	}
	if rep.GameFrames()[0].Function != "lever.(*System).toggle" || rep.GameFrames()[0].File != "game/lever/system.go" || rep.GameFrames()[0].Line != 83 {
		t.Fatalf("game frame 0 = %+v", rep.GameFrames()[0])
	}

	var askedPanic *editor.PanicReport
	ed.OnAskPanicFix = func(p editor.PanicReport) {
		cp := p
		askedPanic = &cp
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760))
	t.Cleanup(func() { _ = ed.Close() })

	app.Post(func() {
		rep.RunNumber = 2
		rep.Time = time.Date(2026, 11, 25, 14, 2, 17, 0, time.UTC)
		ed.SetOutputPanic(rep)
	})
	app.Settle()

	app.Find("id:output-panic-card")
	app.Find("id:output-panic-frame-0")
	app.Find("id:output-panic-frame-1")
	app.Find("id:output-panic-runtime-row")

	// Clicking a game stack link selects the file in the file browser.
	app.Press("id:output-panic-link-0")
	if ed.SelectedFile != "game/lever/system.go" {
		t.Fatalf("SelectedFile after clicking panic link = %q, want game/lever/system.go", ed.SelectedFile)
	}

	// Runtime frames are folded initially; clicking Show expands them.
	if ed.OutputPanicRuntimeExpanded() {
		t.Fatal("runtime frames should be folded initially")
	}
	app.Press("id:output-panic-runtime-toggle")
	if !ed.OutputPanicRuntimeExpanded() {
		t.Fatal("runtime frames should expand after pressing Show")
	}
	app.Find("id:output-panic-runtime-frame-0")
	app.Find("id:output-panic-runtime-frame-3")

	// Pressing "Ask the agent to fix" dispatches OnAskPanicFix.
	app.Press("id:output-panic-ask-btn")
	if askedPanic == nil || askedPanic.Frame != 2940 {
		t.Fatalf("OnAskPanicFix got %+v, want frame 2940", askedPanic)
	}
}

// #1042 Acceptance Criterion 2:
// ReplayUpToPanic on a fixture game/recording that panics at a known frame K
// stops and pauses at frame K - 1 without triggering the panic.
func TestReplayUpToPanicStopsOneFrameBeforePanic(t *testing.T) {
	_, ed := writePlayMarksProject(t)

	const panicAtFrame = 45
	ed.PlayRate = 60
	ed.PlaySeed = 1234
	ed.PlayInputActions = []string{"right", "toggle_lever"}

	// Live scripted input presses "right" for steps 10..44 and presses "toggle_lever" at step 44 (frame 45),
	// which triggers the fixture game's panic on frame 45.
	ed.PlayInputSource = newScriptedActionSource(func(step int, pressed map[string]bool, _ map[string]float64) {
		pressed["right"] = step >= 10 && step < 44
		pressed["toggle_lever"] = step == 44
	})

	var simStep int
	var knightX float32
	var panicCount int
	ed.OnPlayReset = func(seed uint64) error {
		simStep = 0
		knightX = 100
		return nil
	}
	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, _ time.Duration) {
		simStep++
		in := ed.PlayActiveInput()
		if in != nil && in.Pressed("right") {
			knightX += 3
			if k := scene.Object("knight"); k != nil {
				k.Doc.X = knightX
			}
		}
		if simStep == panicAtFrame || (in != nil && in.Pressed("toggle_lever")) {
			panicCount++
			panic("runtime error: index out of range [3] with length 3")
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760))
	t.Cleanup(func() { _ = ed.Close() })

	// Run Play until the panic fires on frame 45.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.SetPlayPaused(true)
		ed.StepPlay(50)
	})
	app.Settle()

	if panicCount != 1 {
		t.Fatalf("expected fixture game to panic once during initial run, got %d", panicCount)
	}
	if ed.Playing() {
		t.Fatal("Play mode should have stopped cleanly after recovering the panic")
	}
	rep := ed.OutputPanic()
	if rep == nil {
		t.Fatal("expected OutputPanic to be populated after Play panic")
	}
	if rep.Frame != panicAtFrame {
		t.Fatalf("OutputPanic().Frame = %d, want %d", rep.Frame, panicAtFrame)
	}
	if rep.ReplayPath == "" {
		t.Fatal("expected OutputPanic().ReplayPath to be saved under replays/")
	}

	// Click "Replay up to the panic" in the Output panel panic card!
	app.Press("id:output-panic-replay-btn")

	// Must NOT trigger the panic again, must remain Playing & Paused at frame K - 1 = 44!
	if panicCount != 1 {
		t.Fatalf("ReplayUpToPanic triggered the panic again! panicCount = %d, want 1", panicCount)
	}
	if !ed.Playing() {
		t.Fatal("expected Play mode to be active after ReplayUpToPanic")
	}
	if !ed.PlayPaused() {
		t.Fatal("expected Play mode to be paused after ReplayUpToPanic")
	}
	if got := ed.PlaySteps(); got != panicAtFrame-1 {
		t.Fatalf("PlaySteps after ReplayUpToPanic = %d, want %d (K - 1)", got, panicAtFrame-1)
	}
	// Verify deterministic replayed position: "right" was pressed for steps 10..43 (34 steps * +3 = +102 -> X = 202).
	if k := ed.PlayScene().Object("knight"); k == nil || k.Doc.X != 202 {
		t.Fatalf("replayed knight X = %v, want 202 at frame %d", k.Doc.X, panicAtFrame-1)
	}
}

// #1042 Acceptance Criterion 3:
// Real nsgotest screenshot verification on macOS using Spec tokens (optional PNG dump via PLAYMODE_SCREENSHOT_DIR).
func TestPlayModeSpecScreenshots(t *testing.T) {
	dir, ed := writePlayMarksProject(t)
	ed.SelectObject("knight")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true}))
	t.Cleanup(func() { _ = ed.Close() })

	// 1) Play mode with marks, rolling replay footer, and 60s frame-time strip.
	app.Post(func() {
		_ = ed.Play()
		ed.SetPlayPaused(true)
		ed.StepPlay(1407)
		_, _ = ed.AddPlayMark()
		ed.SetPlayInspectorTab(editor.PlayTabAgent)
	})
	app.Settle()
	app.Find("id:play-mode-bar")
	app.Find("id:play-replay-footer")
	app.Find("id:play-marks-header")
	app.Screenshot("playmode-marks")

	// 2) Frozen frame with "Ask about frame N" dialog open.
	app.Post(func() {
		_ = ed.OpenAskFrozenFrame()
		ed.SetAskFrozenFrameDraft("The knight snags on the plank's left edge when he lands short.")
	})
	app.Settle()
	app.Find("id:play-ask-dialog")
	app.Find("id:play-ask-review-note")
	app.Screenshot("playmode-ask-frozen")

	// 3) Agent stepping pill + step log + Output panel panic card.
	app.Post(func() {
		_, _ = ed.SubmitAskFrozenFrame("The knight snags on the plank's left edge when he lands short.")
		_, _ = ed.AgentStepPlayReplay(1407, 1419, "the knight stops at x 251 with velocity 0")
		rep, _ := editor.ParsePanicTrace([]string{
			"14:02:17 frame 2940",
			"panic: runtime error: index out of range [3] with length 3",
			"goroutine 1 [running]:",
			"example.com/dungeon/game/lever.(*System).toggle(0xc00012a000, 0x3)",
			fmt.Sprintf("\t%s/game/lever/system.go:83 +0x4c", dir),
			"example.com/dungeon/game/lever.(*System).Update(0xc00012a000, 0x1)",
			fmt.Sprintf("\t%s/game/lever/system.go:57 +0x24", dir),
			"github.com/nsgo-dev/game/packages/game.(*Loop).StepNow(0xc000090000, 0x1)",
			"\t/go/pkg/packages/game/loop.go:175 +0x90",
			"github.com/nsgo-dev/nsgo/core.(*Scope).frame(0xc000080000)",
			"\t/go/pkg/nsgo/core/scope.go:221 +0x38",
			"runtime.gopanic(0x1, 0x2)",
			"\t/usr/local/go/src/runtime/panic.go:770 +0x132",
			"runtime.goPanicIndex(0x3, 0x3)",
			"\t/usr/local/go/src/runtime/panic.go:115 +0x74",
		}, dir)
		rep.RunNumber = 2
		rep.Time = time.Date(2026, 11, 25, 14, 2, 17, 0, time.UTC)
		rep.ReplayPath = ed.PlayLastReplayPath()
		ed.SetOutputPanic(rep)
	})
	app.Settle()
	app.Find("id:play-agent-pill")
	app.Find("id:output-panic-card")
	app.Screenshot("playmode-agent-and-panic")
}
