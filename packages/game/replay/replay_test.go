package replay_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var _ replay.ActionSource = (*input.Actions)(nil)

const (
	frameStep  = 16667 * time.Microsecond
	goldenSeed = uint64(0x5eed172)
)

func rect(x, y, w, h core.Points) core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: x, Y: y},
		Size:   core.Size{Width: w, Height: h},
	}
}

func pt(x, y core.Points) core.Point {
	return core.Point{X: x, Y: y}
}

// levelState is the deterministic observable state of miniLevel.
type levelState struct {
	Step       int
	X, Y       float64
	VX, VY     float64
	OnGround   bool
	HP         int
	Score      int
	Coins      int
	JumpCount  int
	DashCount  int
	HazardHits int
	Won        bool
}

type coin struct {
	x, y      float64
	collected bool
}

type hazard struct {
	x, minX, maxX float64
	vx            float64
}

// miniLevel is a deterministic 2D runner used to exercise live recording, playback and golden replay tests.
type miniLevel struct {
	src        replay.ActionSource
	step       int
	x, y       float64
	vx, vy     float64
	onGround   bool
	hp         int
	score      int
	coins      int
	jumpCount  int
	dashCount  int
	hazardHits int
	invuln     int
	won        bool
	items      []coin
	hazards    []hazard
}

func newMiniLevel(seed uint64, src replay.ActionSource) *miniLevel {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	lvl := &miniLevel{
		src:      src,
		onGround: true,
		hp:       3,
		items:    make([]coin, 5),
		hazards:  make([]hazard, 3),
	}
	for i := range lvl.items {
		baseX := 35.0 + float64(i)*38.0
		lvl.items[i] = coin{
			x: baseX + float64(rng.IntN(11)-5),
			y: float64(rng.IntN(3)) * 12.0,
		}
	}
	for i := range lvl.hazards {
		center := 95.0 + float64(i)*50.0
		dir := 1.0
		if rng.IntN(2) == 0 {
			dir = -1.0
		}
		lvl.hazards[i] = hazard{
			x:    center + float64(rng.IntN(15)-7),
			minX: center - 14.0,
			maxX: center + 14.0,
			vx:   dir * (24.0 + float64(rng.IntN(10))),
		}
	}
	return lvl
}

func newMiniLevelSim(seed uint64, src replay.ActionSource) (func(time.Duration), func() levelState) {
	lvl := newMiniLevel(seed, src)
	return lvl.Update, lvl.State
}

func (l *miniLevel) Update(dt time.Duration) {
	l.src.Step()
	l.step++
	if l.won {
		return
	}

	sec := dt.Seconds()
	move := l.src.Axis("move_x")
	speed := 90.0
	if l.src.Pressed("dash") {
		speed = 160.0
	}
	if l.src.JustPressed("dash") {
		l.dashCount++
		l.score += 10
	}
	l.vx = move * speed

	if l.src.JustPressed("jump") && l.onGround {
		l.vy = 185.0
		l.onGround = false
		l.jumpCount++
	}
	if l.src.JustReleased("jump") && l.vy > 60.0 {
		l.vy *= 0.5
	}

	l.vy -= 420.0 * sec
	l.x += l.vx * sec
	if l.x < 0 {
		l.x = 0
	}
	l.y += l.vy * sec
	if l.y <= 0 {
		l.y = 0
		l.vy = 0
		l.onGround = true
	}

	for i := range l.hazards {
		h := &l.hazards[i]
		h.x += h.vx * sec
		if h.x < h.minX {
			h.x = h.minX
			h.vx = math.Abs(h.vx)
		} else if h.x > h.maxX {
			h.x = h.maxX
			h.vx = -math.Abs(h.vx)
		}
	}

	for i := range l.items {
		c := &l.items[i]
		if c.collected {
			continue
		}
		if math.Abs(l.x-c.x) <= 14.0 && math.Abs(l.y-c.y) <= 22.0 {
			c.collected = true
			l.coins++
			l.score += 100
		}
	}

	if l.invuln > 0 {
		l.invuln--
	} else {
		for _, h := range l.hazards {
			if math.Abs(l.x-h.x) <= 8.0 && l.y <= 14.0 {
				l.hp--
				l.hazardHits++
				l.invuln = 30
				break
			}
		}
	}

	if l.x >= 220.0 {
		l.won = true
		l.score += 500
	}
}

func (l *miniLevel) State() levelState {
	return levelState{
		Step:       l.step,
		X:          l.x,
		Y:          l.y,
		VX:         l.vx,
		VY:         l.vy,
		OnGround:   l.onGround,
		HP:         l.hp,
		Score:      l.score,
		Coins:      l.coins,
		JumpCount:  l.jumpCount,
		DashCount:  l.dashCount,
		HazardHits: l.hazardHits,
		Won:        l.won,
	}
}

func bindMiniLevelActions(a *input.Actions) {
	a.Bind("left", input.TouchRegion(rect(0, 200, 100, 100)))
	a.Bind("right", input.TouchRegion(rect(100, 200, 100, 100)))
	a.Bind("jump", input.TouchRegion(rect(300, 200, 100, 100)), input.MouseButton(core.MouseButtonLeft))
	a.Bind("dash", input.MouseRegion(rect(200, 0, 200, 200), core.MouseButtonLeft), input.MouseButton(core.MouseButtonRight))
	a.BindAxis("move_x", "left", "right")
}

// recordLiveMiniLevel drives a multi-second live run on game.Loop + *input.Actions under nsgotest and returns the recording and every intermediate state.
func recordLiveMiniLevel(t *testing.T, seed uint64) (*replay.Recording, []levelState) {
	t.Helper()
	actions := input.NewActions()
	bindMiniLevelActions(actions)
	rec := replay.NewRecorder(actions, seed, 60, []string{"left", "right", "jump", "dash"}, []string{"move_x"})
	lvl := newMiniLevel(seed, rec)
	var history []levelState

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		actions.Attach(&v)
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				lvl.Update(dt)
				history = append(history, lvl.State())
			},
		}
		loop.Run(cx)
		return v
	}, nsgotest.Size(400, 300))

	advanceSteps := func(n int) {
		target := len(history) + n
		for len(history) < target {
			app.Advance(frameStep)
		}
	}

	// Brief idle start.
	advanceSteps(6)

	// Run right with touch, slide into left region (reversing move_x axis), then back right.
	finger := app.Touch("game", pt(150, 250))
	advanceSteps(32)
	finger.Move(pt(50, 250))
	advanceSteps(10)
	finger.Move(pt(150, 250))
	advanceSteps(30)
	finger.Lift()
	advanceSteps(4)

	// Mouse left jump + mouse-region dash while airborne, then right-click dash pulse.
	app.PressDown("game")
	app.HoverAt("game", pt(300, 100))
	advanceSteps(10)
	app.PressUp("game")
	advanceSteps(6)
	app.RightClick("game")
	advanceSteps(4)

	// Multi-touch sprint right with a mid-run jump and brief opposite-action cancellation (move_x == 0).
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase:   core.TouchStart,
			Touches: []core.Touch{{ID: 10, Position: pt(150, 250), Changed: true}},
		})
	})
	app.Settle()
	advanceSteps(20)

	// Press jump (ID 11) while holding right (ID 10).
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchStart,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 11, Position: pt(350, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(8)

	// Release jump early (variable jump cut) while still running right.
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchEnd,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 11, Position: pt(350, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(26)

	// Briefly press left (ID 12) while holding right (ID 10) => opposite-action axis cancels to 0.
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchStart,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 12, Position: pt(50, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(6)

	// Lift left (ID 12) and jump again (ID 13) to sprint across the goal at X >= 220.
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchEnd,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 12, Position: pt(50, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(14)

	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchStart,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 13, Position: pt(350, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(16)

	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase: core.TouchEnd,
			Touches: []core.Touch{
				{ID: 10, Position: pt(150, 250), Changed: false},
				{ID: 13, Position: pt(350, 250), Changed: true},
			},
		})
	})
	app.Settle()
	advanceSteps(55)

	// Lift final right touch.
	app.Post(func() {
		actions.OnTouch(core.TouchEvent{
			Phase:   core.TouchEnd,
			Touches: []core.Touch{{ID: 10, Position: pt(150, 250), Changed: true}},
		})
	})
	app.Settle()
	advanceSteps(4)

	return rec.Recording(), history
}

// syntheticSource is a controllable ActionSource for deterministic edge-case tests.
type syntheticSource struct {
	nextActions map[string]bool
	nextAxes    map[string]float64
	curr        map[string]bool
	prev        map[string]bool
	axes        map[string]float64
}

func newSyntheticSource() *syntheticSource {
	return &syntheticSource{
		nextActions: map[string]bool{},
		nextAxes:    map[string]float64{},
		curr:        map[string]bool{},
		prev:        map[string]bool{},
		axes:        map[string]float64{},
	}
}

func (s *syntheticSource) Step() {
	s.prev, s.curr = s.curr, s.prev
	clear(s.curr)
	for k, v := range s.nextActions {
		if v {
			s.curr[k] = true
		}
	}
	clear(s.axes)
	for k, v := range s.nextAxes {
		s.axes[k] = v
	}
}

func (s *syntheticSource) Pressed(name string) bool      { return s.curr[name] }
func (s *syntheticSource) JustPressed(name string) bool  { return s.curr[name] && !s.prev[name] }
func (s *syntheticSource) JustReleased(name string) bool { return !s.curr[name] && s.prev[name] }
func (s *syntheticSource) Axis(name string) float64 {
	if v, ok := s.axes[name]; ok {
		return v
	}
	if s.curr[name] {
		return 1
	}
	return 0
}

func TestRecorderDeltaEncodingAndCompactSize(t *testing.T) {
	src := newSyntheticSource()
	rec := replay.NewRecorder(src, 12345, 60, []string{"left", "right", "jump", "", "jump"}, []string{"move_x", "aim", "aim"})

	// Simulate 10 minutes at 60 Hz (36,000 steps) with sparse input changes.
	const totalSteps = 36000
	changes := 0
	for step := range totalSteps {
		switch step {
		case 0:
			src.nextActions["right"] = true
			src.nextAxes["move_x"] = 1
			changes++
		case 120:
			src.nextActions["jump"] = true
			changes++
		case 135:
			src.nextActions["jump"] = false
			changes++
		case 6000:
			src.nextAxes["aim"] = 0.375
			changes++
		case 12000:
			src.nextActions["right"] = false
			src.nextActions["left"] = true
			src.nextAxes["move_x"] = -1
			changes++
		case 24000:
			src.nextActions["left"] = false
			src.nextAxes["move_x"] = 0
			src.nextAxes["aim"] = -0.75
			changes++
		case 35999:
			src.nextAxes["aim"] = 0
			changes++
		}
		rec.Step()
	}

	got := rec.Recording()
	if got.Version != replay.FormatVersion {
		t.Fatalf("version = %d, want %d", got.Version, replay.FormatVersion)
	}
	if got.Seed != 12345 || got.Rate != 60 || got.TotalSteps != totalSteps {
		t.Fatalf("header = (seed %d, rate %d, steps %d), want (12345, 60, %d)", got.Seed, got.Rate, got.TotalSteps, totalSteps)
	}
	if len(got.Actions) != 3 || len(got.Axes) != 2 {
		t.Fatalf("deduped names = actions:%v axes:%v", got.Actions, got.Axes)
	}
	if len(got.Events) != changes {
		t.Fatalf("events = %d, want %d for %d total steps", len(got.Events), changes, totalSteps)
	}

	text, err := got.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(text) > 1024 {
		t.Fatalf("10-minute recording is %d bytes, expected < 1024 bytes", len(text))
	}
	// Seven header lines, then exactly one line per changed step.
	if lines := strings.Count(string(text), "\n"); lines != 7+changes {
		t.Fatalf("recording has %d lines, want %d:\n%s", lines, 7+changes, text)
	}
}

func TestPlayerEdgeTransitionsAndRewind(t *testing.T) {
	src := newSyntheticSource()
	rec := replay.NewRecorder(src, 99, 60, []string{"jump", "left", "right"}, []string{"move_x"})

	type snap struct {
		jp, pr, jr bool
		leftPr     bool
		rightPr    bool
		moveAxis   float64
		jumpAxis   float64
		unknownPr  bool
		unknownAx  float64
	}

	var want []snap
	// 12 steps covering: step-0 press, multi-step hold, release, idle, rapid 1-step taps, simultaneous press+release+axis, and held-at-end.
	for step := range 12 {
		switch step {
		case 0: // edge on step 0
			src.nextActions["jump"] = true
		case 4: // release after 4-step hold
			src.nextActions["jump"] = false
		case 6: // single-step tap 1
			src.nextActions["jump"] = true
		case 7:
			src.nextActions["jump"] = false
		case 8: // single-step tap 2
			src.nextActions["jump"] = true
		case 9:
			src.nextActions["jump"] = false
			src.nextActions["left"] = true
			src.nextAxes["move_x"] = -1
		case 10: // simultaneous release left + press right + axis change
			src.nextActions["left"] = false
			src.nextActions["right"] = true
			src.nextAxes["move_x"] = 0.5
		case 11: // kept held on final step
		}
		rec.Step()
		want = append(want, snap{
			jp:        rec.JustPressed("jump"),
			pr:        rec.Pressed("jump"),
			jr:        rec.JustReleased("jump"),
			leftPr:    rec.Pressed("left"),
			rightPr:   rec.Pressed("right"),
			moveAxis:  rec.Axis("move_x"),
			jumpAxis:  rec.Axis("jump"),
			unknownPr: rec.Pressed("missing"),
			unknownAx: rec.Axis("missing"),
		})
	}

	player := replay.NewPlayer(rec.Recording())
	if player.Seed() != 99 || player.Rate() != 60 || player.TotalSteps() != 12 || player.StepIndex() != 0 || player.Done() {
		t.Fatalf("initial player metadata mismatch: seed=%d rate=%d total=%d idx=%d done=%v",
			player.Seed(), player.Rate(), player.TotalSteps(), player.StepIndex(), player.Done())
	}

	playOnce := func(pass string) {
		for i, w := range want {
			if player.Done() {
				t.Fatalf("%s step %d: Done() early", pass, i)
			}
			player.Step()
			if player.StepIndex() != i+1 {
				t.Fatalf("%s step %d: StepIndex = %d, want %d", pass, i, player.StepIndex(), i+1)
			}
			got := snap{
				jp:        player.JustPressed("jump"),
				pr:        player.Pressed("jump"),
				jr:        player.JustReleased("jump"),
				leftPr:    player.Pressed("left"),
				rightPr:   player.Pressed("right"),
				moveAxis:  player.Axis("move_x"),
				jumpAxis:  player.Axis("jump"),
				unknownPr: player.Pressed("missing"),
				unknownAx: player.Axis("missing"),
			}
			if got != w {
				t.Fatalf("%s step %d:\n got: %+v\nwant: %+v", pass, i, got, w)
			}
		}
		if !player.Done() {
			t.Fatalf("%s: expected Done() after %d steps", pass, len(want))
		}
		// Stepping past end cleanly releases held actions once, then stays idle.
		player.Step()
		if player.Pressed("right") || !player.JustReleased("right") || player.Axis("move_x") != 0 {
			t.Fatalf("%s post-end step 1: pressed=%v justReleased=%v axis=%v",
				pass, player.Pressed("right"), player.JustReleased("right"), player.Axis("move_x"))
		}
		player.Step()
		if player.Pressed("right") || player.JustReleased("right") || player.Axis("move_x") != 0 {
			t.Fatalf("%s post-end step 2: expected idle", pass)
		}
	}

	playOnce("pass1")
	player.Reset()
	if player.StepIndex() != 0 || player.Done() {
		t.Fatalf("after Reset: idx=%d done=%v", player.StepIndex(), player.Done())
	}
	playOnce("pass2")

	// Nil recording and empty recording do not panic.
	nilPlayer := replay.NewPlayer(nil)
	if !nilPlayer.Done() || nilPlayer.TotalSteps() != 0 || nilPlayer.Rate() != 60 {
		t.Fatalf("nil player unexpected state")
	}
	nilPlayer.Step()
	if nilPlayer.Pressed("jump") || nilPlayer.Axis("move_x") != 0 {
		t.Fatalf("nil player should stay idle")
	}
}

func TestEncodeDecodeRoundTripAndErrors(t *testing.T) {
	orig := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       424242,
		Rate:       120,
		Actions:    []string{"left", "right", "jump"},
		Axes:       []string{"move_x"},
		TotalSteps: 20,
		Events: []replay.StepEvent{
			{Step: 0, Press: []int{1}, Axes: []replay.AxisChange{{Index: 0, Value: 0.75}}},
			{Step: 5, Press: []int{2}, Release: []int{1}, Axes: []replay.AxisChange{{Index: 0, Value: -0.25}}},
			{Step: 19, Release: []int{2}, Axes: []replay.AxisChange{{Index: 0, Value: 0}}},
		},
	}

	raw, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := replay.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	decRaw, err := dec.Encode()
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if string(decRaw) != string(raw) {
		t.Fatalf("text round trip mismatch:\n got: %s\nwant: %s", decRaw, raw)
	}
	if !reflect.DeepEqual(dec, orig) {
		t.Fatalf("Decode(Encode(r)) = %+v, want %+v", dec, orig)
	}

	want := `version: 2
seed: 424242
rate: 120
actions: [left, right, jump]
axes: [move_x]
total_steps: 20
steps:
  - {step: 0, press: [right], axes: {move_x: 0.75}}
  - {step: 5, press: [jump], release: [right], axes: {move_x: -0.25}}
  - {step: 19, release: [jump], axes: {move_x: 0}}
`
	if string(raw) != want {
		t.Fatalf("Encode text mismatch:\n got:\n%s\nwant:\n%s", raw, want)
	}

	// File save and load round trip.
	path := filepath.Join(t.TempDir(), "run.replay.yaml")
	if err := replay.SaveFile(path, orig); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	loaded, err := replay.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	loadedRaw, err := loaded.Encode()
	if err != nil {
		t.Fatalf("Encode loaded: %v", err)
	}
	if string(loadedRaw) != string(raw) {
		t.Fatalf("file round trip mismatch:\n got: %s\nwant: %s", loadedRaw, raw)
	}
	if _, err := replay.LoadFile(filepath.Join(t.TempDir(), "missing.replay.yaml")); err == nil {
		t.Fatal("LoadFile accepted a missing file")
	}

	// Reject malformed, unknown and unsupported-version inputs.
	const head = "version: 2\nseed: 1\nrate: 60\nactions: [a]\naxes: [x]\ntotal_steps: 5\n"
	bad := []struct {
		name string
		data string
	}{
		{"empty", "  \n"},
		{"corrupt_yaml", "version: [2\n"},
		{"gzip", "\x1f\x8b\x08\x00rest"},
		{"version_missing", "seed: 1\nrate: 60\ntotal_steps: 1\n"},
		{"seed_missing", "version: 2\nrate: 60\ntotal_steps: 1\n"},
		{"total_steps_missing", "version: 2\nseed: 1\nrate: 60\n"},
		{"total_steps_missing_with_steps", "version: 2\nseed: 1\nrate: 60\nactions: [a]\nsteps:\n  - {step: 0, press: [a]}\n"},
		{"version_one_json", `{"version":1,"seed":1,"rate":60,"total_steps":2,"events":[{"s":0,"p":[0]}]}`},
		{"future_version", "version: 99\nseed: 1\nrate: 60\ntotal_steps: 1\n"},
		{"zero_rate", "version: 2\nseed: 1\nrate: 0\ntotal_steps: 1\n"},
		{"negative_rate", "version: 2\nseed: 1\nrate: -60\ntotal_steps: 1\n"},
		{"negative_steps", "version: 2\nseed: 1\nrate: 60\ntotal_steps: -1\n"},
		{"negative_seed", "version: 2\nseed: -1\nrate: 60\ntotal_steps: 1\n"},
		{"unknown_header_key", head + "speed: 2\n"},
		{"unknown_step_key", head + "steps:\n  - {step: 0, hold: [a]}\n"},
		{"trailing_document", head + "---\nversion: 2\n"},
		{"empty_action_name", "version: 2\nseed: 1\nrate: 60\nactions: [\"\"]\ntotal_steps: 1\n"},
		{"dup_action_name", "version: 2\nseed: 1\nrate: 60\nactions: [a, a]\ntotal_steps: 1\n"},
		{"dup_axis_name", "version: 2\nseed: 1\nrate: 60\naxes: [x, x]\ntotal_steps: 1\n"},
		{"step_missing", head + "steps:\n  - {press: [a]}\n"},
		{"step_out_of_range", head + "steps:\n  - {step: 5, press: [a]}\n"},
		{"step_negative", head + "steps:\n  - {step: -1, press: [a]}\n"},
		{"step_not_a_number", head + "steps:\n  - {step: two, press: [a]}\n"},
		{"non_increasing_step", head + "steps:\n  - {step: 2, press: [a]}\n  - {step: 2, release: [a]}\n"},
		{"decreasing_step", head + "steps:\n  - {step: 3, press: [a]}\n  - {step: 1, release: [a]}\n"},
		{"unknown_press", head + "steps:\n  - {step: 0, press: [b]}\n"},
		{"unknown_release", head + "steps:\n  - {step: 0, release: [b]}\n"},
		{"press_and_release_same", head + "steps:\n  - {step: 0, press: [a], release: [a]}\n"},
		{"press_twice", head + "steps:\n  - {step: 0, press: [a, a]}\n"},
		{"unknown_axis", head + "steps:\n  - {step: 0, axes: {y: 1}}\n"},
		{"dup_axis", head + "steps:\n  - {step: 0, axes: {x: 1, x: 0}}\n"},
		{"axis_not_a_number", head + "steps:\n  - {step: 0, axes: {x: left}}\n"},
		{"axis_nan", head + "steps:\n  - {step: 0, axes: {x: .nan}}\n"},
		{"axis_inf", head + "steps:\n  - {step: 0, axes: {x: .inf}}\n"},
		{"axes_not_a_mapping", head + "steps:\n  - {step: 0, axes: [x]}\n"},
		{"steps_not_a_list", head + "steps: {step: 0}\n"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := replay.Decode([]byte(tc.data)); err == nil {
				t.Fatalf("Decode(%q) succeeded, want error", tc.data)
			}
		})
	}
}

func TestRecorderSnapshotIsolationAndReset(t *testing.T) {
	src := newSyntheticSource()
	rec := replay.NewRecorder(src, 1, 0, []string{"jump"}, nil)
	if rec.Rate() != 60 {
		t.Fatalf("default rate = %d, want 60", rec.Rate())
	}

	src.nextActions["jump"] = true
	rec.Step()
	snap1 := rec.Recording()

	src.nextActions["jump"] = false
	rec.Step()
	snap2 := rec.Recording()

	if snap1.TotalSteps != 1 || len(snap1.Events) != 1 {
		t.Fatalf("snap1 mutated: steps=%d events=%d", snap1.TotalSteps, len(snap1.Events))
	}
	if snap2.TotalSteps != 2 || len(snap2.Events) != 2 {
		t.Fatalf("snap2 unexpected: steps=%d events=%d", snap2.TotalSteps, len(snap2.Events))
	}

	rec.Reset(777)
	if rec.Seed() != 777 || rec.TotalSteps() != 0 {
		t.Fatalf("after Reset: seed=%d steps=%d", rec.Seed(), rec.TotalSteps())
	}
	empty := rec.Recording()
	if empty.Seed != 777 || empty.TotalSteps != 0 || len(empty.Events) != 0 {
		t.Fatalf("recording after Reset: %+v", empty)
	}
}

func TestLiveMiniLevelMatchesPlaybackBitForBit(t *testing.T) {
	recorded, liveHistory := recordLiveMiniLevel(t, goldenSeed)
	if len(liveHistory) < 180 {
		t.Fatalf("live run had %d steps, want at least 180", len(liveHistory))
	}
	liveEnd := liveHistory[len(liveHistory)-1]
	if !liveEnd.Won || liveEnd.Coins == 0 || liveEnd.JumpCount == 0 || liveEnd.DashCount == 0 {
		t.Fatalf("expected live run to clear level with coins, jumps and dashes: %+v", liveEnd)
	}
	if len(recorded.Events) >= recorded.TotalSteps/4 {
		t.Fatalf("recording not compact: %d events for %d steps", len(recorded.Events), recorded.TotalSteps)
	}

	// Round-trip through text in memory and through a file before playback.
	encoded, err := recorded.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := replay.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "live.replay.yaml")
	if err := replay.SaveFile(path, recorded); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	decodedFile, err := replay.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	// 1. Direct step-by-step playback: assert EVERY intermediate step AND final state match live run bit-for-bit.
	player := replay.NewPlayer(decoded)
	sim := newMiniLevel(player.Seed(), player)
	dt := decoded.StepDuration()
	for step, want := range liveHistory {
		sim.Update(dt)
		got := sim.State()
		if got != want {
			t.Fatalf("step %d mismatch:\n got: %+v\nwant: %+v", step, got, want)
		}
	}
	if !player.Done() {
		t.Fatalf("player not done after %d steps (at %d)", len(liveHistory), player.StepIndex())
	}

	// 2. Playback inside a fresh game.Loop under nsgotest matches bit-for-bit.
	loopPlayer := replay.NewPlayer(decodedFile)
	loopSim := newMiniLevel(loopPlayer.Seed(), loopPlayer)
	var stop func()
	loopSteps := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop := &game.Loop{
			Rate: decodedFile.Rate,
			Update: func(dt time.Duration) {
				loopSim.Update(dt)
				loopSteps++
				if loopSteps >= decodedFile.TotalSteps {
					stop()
				}
			},
		}
		stop = loop.Run(cx)
		return core.View{}
	})
	for loopSteps < decodedFile.TotalSteps {
		app.Advance(frameStep)
	}
	if got := loopSim.State(); got != liveEnd {
		t.Fatalf("game.Loop playback end state mismatch:\n got: %+v\nwant: %+v", got, liveEnd)
	}

	// 3. replay.Run and replay.AssertEndState reproduce the exact end state.
	runEnd := replay.Run(t, decoded, newMiniLevelSim)
	if runEnd != liveEnd {
		t.Fatalf("replay.Run end state mismatch:\n got: %+v\nwant: %+v", runEnd, liveEnd)
	}
	replay.AssertEndState(t, decoded, liveEnd, func(a, b levelState) bool { return a == b }, newMiniLevelSim)
	replay.AssertEndState(t, decoded, liveEnd, nil, newMiniLevelSim)
}

type failCaptureTB struct {
	testing.TB
	failed string
}

type failSentinel struct{}

func (f *failCaptureTB) Helper() {}
func (f *failCaptureTB) Fatalf(format string, args ...any) {
	f.failed = fmt.Sprintf(format, args...)
	panic(failSentinel{})
}
func (f *failCaptureTB) Fatal(args ...any) {
	f.failed = fmt.Sprint(args...)
	panic(failSentinel{})
}

func TestAssertEndStateReportsClearDiagnosticOnMismatch(t *testing.T) {
	rec := &replay.Recording{
		Version:    replay.FormatVersion,
		Seed:       goldenSeed,
		Rate:       60,
		Actions:    []string{"right"},
		Axes:       []string{"move_x"},
		TotalSteps: 10,
	}
	fc := &failCaptureTB{TB: t}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(failSentinel); !ok {
					panic(r)
				}
			}
		}()
		replay.AssertEndState(fc, rec, levelState{Won: true, Score: 999}, nil, newMiniLevelSim)
	}()
	if !strings.Contains(fc.failed, "replay end state mismatch") || !strings.Contains(fc.failed, "10 steps") {
		t.Fatalf("unexpected diagnostic: %q", fc.failed)
	}
}

func approxLevelStateEqual(a, b levelState) bool {
	return a.Step == b.Step &&
		math.Abs(a.X-b.X) < 1e-6 &&
		math.Abs(a.Y-b.Y) < 1e-6 &&
		math.Abs(a.VX-b.VX) < 1e-6 &&
		math.Abs(a.VY-b.VY) < 1e-6 &&
		a.OnGround == b.OnGround &&
		a.HP == b.HP &&
		a.Score == b.Score &&
		a.Coins == b.Coins &&
		a.JumpCount == b.JumpCount &&
		a.DashCount == b.DashCount &&
		a.HazardHits == b.HazardHits &&
		a.Won == b.Won
}

func TestGoldenLevelClearRecording(t *testing.T) {
	path := filepath.Join("testdata", "level_clear.replay.yaml")
	rec, err := replay.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	// Loading and saving the committed text must not change a byte.
	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := rec.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(again) != string(file) {
		t.Fatalf("golden round trip changed the file:\n got:\n%s\nwant:\n%s", again, file)
	}
	if rec.Version != replay.FormatVersion {
		t.Fatalf("golden version = %d, want %d", rec.Version, replay.FormatVersion)
	}
	if rec.Rate != 60 || rec.TotalSteps < 180 {
		t.Fatalf("unexpected golden header: rate=%d total_steps=%d", rec.Rate, rec.TotalSteps)
	}
	if len(rec.Events) >= rec.TotalSteps/4 {
		t.Fatalf("golden recording not compact: %d events for %d steps", len(rec.Events), rec.TotalSteps)
	}

	want := levelState{
		Step:       254,
		X:          220.4999911800004,
		Y:          33.1333332726666,
		VX:         90,
		VY:         -33.499994960000045,
		OnGround:   false,
		HP:         1,
		Score:      1020,
		Coins:      5,
		JumpCount:  3,
		DashCount:  2,
		HazardHits: 2,
		Won:        true,
	}
	replay.AssertEndState(t, rec, want, approxLevelStateEqual, newMiniLevelSim)
}
