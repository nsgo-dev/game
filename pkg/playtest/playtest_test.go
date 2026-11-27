package playtest_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/platformer"
	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

// fixturePlatformer is a deterministic side-scrolling platformer built on physics.Grid and platformer.Controller.
// It has a runway, a gap, a raised ledge, a patrolling slime whose initial patrol offset depends on the run seed,
// collectible coins (one intentionally placed off the runner's route), and an exit door.
type fixturePlatformer struct {
	grid     *physics.Grid
	body     *physics.Body
	charBody *physics.CharacterBody
	ctrl     *platformer.Controller
	stats    *gamedebug.FrameStats

	tileSize float32
	cols     int
	rows     int
	startX   float32
	startY   float32
	exitBox  physics.Box

	// Patrolling slime hazard.
	slimeBaseX  float32
	slimeRange  float32
	slimePhase  float64
	slimeSpeed  float64
	slimeY      float32
	slimeW      float32
	slimeH      float32
	elapsedSecs float64

	coins     []fixtureCoin
	lives     int
	deaths    []deathPos
	finished  bool
	failed    bool
	stepCount int
}

type fixtureCoin struct {
	id    string
	box   physics.Box
	taken bool
}

type deathPos struct {
	step int
	x, y float64
	tag  string
}

func newFixturePlatformer(seed uint64) (*fixturePlatformer, error) {
	const (
		tile = float32(16)
		cols = 34
		rows = 12
	)
	g := physics.NewGrid(cols, rows, tile, tile)
	// Solid floor along row 10, with a 2-tile pit at cols 11..12.
	for c := 0; c < cols; c++ {
		if c == 11 || c == 12 {
			continue
		}
		g.Set(c, 10, physics.TileSolid)
		g.Set(c, 11, physics.TileSolid)
	}
	// Raised step/ledge at cols 17..19 on row 9.
	for c := 17; c <= 19; c++ {
		g.Set(c, 9, physics.TileSolid)
	}
	// Left boundary wall.
	for r := 0; r < rows; r++ {
		g.Set(0, r, physics.TileSolid)
	}

	startX := float32(2) * tile
	startY := float32(10)*tile - 14
	body := physics.NewBoxBody(startX, startY, 10, 14)
	cb := physics.NewCharacterBody(body)
	ctrl, err := platformer.NewController(cb, platformer.DefaultConfig())
	if err != nil {
		return nil, err
	}

	rng := random.NewStreams(seed).Stream("level")
	// Deterministic seed-dependent slime patrol starting phase creates realistic clear/death variety across 20 seeds.
	slimePhase := rng.Float64() * 2 * math.Pi

	f := &fixturePlatformer{
		grid:       g,
		body:       body,
		charBody:   cb,
		ctrl:       ctrl,
		stats:      &gamedebug.FrameStats{},
		tileSize:   tile,
		cols:       cols,
		rows:       rows,
		startX:     startX,
		startY:     startY,
		exitBox:    physics.Box{X: float32(30) * tile, Y: float32(8) * tile, W: tile, H: 2 * tile},
		slimeBaseX: float32(23) * tile,
		slimeRange: 2.2 * tile,
		slimePhase: slimePhase,
		slimeSpeed: 3.4,
		slimeY:     float32(10)*tile - 12,
		slimeW:     14,
		slimeH:     12,
		lives:      2,
		coins: []fixtureCoin{
			{id: "coin_01", box: physics.Box{X: float32(6) * tile, Y: float32(9) * tile, W: 8, H: 8}},
			{id: "coin_02", box: physics.Box{X: float32(18) * tile, Y: float32(8) * tile, W: 8, H: 8}},
			// coin_03 sits high above the route so the greedy runner never reaches it (causes Coins Taken < 100%).
			{id: "coin_03", box: physics.Box{X: float32(18) * tile, Y: float32(2) * tile, W: 8, H: 8}},
		},
	}
	return f, nil
}

func (f *fixturePlatformer) slimeBox() physics.Box {
	x := f.slimeBaseX + float32(math.Sin(f.slimePhase+f.elapsedSecs*f.slimeSpeed))*f.slimeRange
	return physics.Box{X: x, Y: f.slimeY, W: f.slimeW, H: f.slimeH}
}

func (f *fixturePlatformer) observe() any {
	ex, ey := f.exitBox.Center()
	wall := 0
	if f.body.OnLeftWall {
		wall = -1
	} else if f.body.OnRightWall {
		wall = 1
	}
	return playtest.ObservePlatformerGrid(
		f.grid,
		f.body.Bounds(),
		f.body.VX,
		f.body.VY,
		f.charBody.Grounded,
		wall,
		ex, ey,
		[]physics.Box{f.slimeBox()},
		f.tileSize*1.2,
	)
}

func boxesOverlap(a, b physics.Box) bool {
	return a.Left() < b.Right() && a.Right() > b.Left() && a.Top() < b.Bottom() && a.Bottom() > b.Top()
}

func (f *fixturePlatformer) step(src replay.ActionSource, metrics *playtest.Collector, dt time.Duration) {
	src.Step()
	if f.finished || f.failed {
		return
	}
	if f.stepCount == 0 {
		metrics.StartTimer("clear_time")
		metrics.SetCounter("coins_total", float64(len(f.coins)))
	}
	f.stepCount++
	f.elapsedSecs += dt.Seconds()

	move := float32(src.Axis("move_x"))
	if move == 0 {
		if src.Pressed("right") {
			move = 1
		} else if src.Pressed("left") {
			move = -1
		}
	}
	in := platformer.Input{
		Move:        move,
		JumpPressed: src.JustPressed("jump"),
		JumpHeld:    src.Pressed("jump"),
	}
	f.ctrl.Step(in, f.grid, nil, nil, dt)
	// Deterministic simulated frame-time sample via gamedebug.FrameStats so perf readout is exercised without wall-clock flake.
	f.stats.Record(4100*time.Microsecond, 3200*time.Microsecond, 900*time.Microsecond)

	pb := f.body.Bounds()
	for i := range f.coins {
		if !f.coins[i].taken && boxesOverlap(pb, f.coins[i].box) {
			f.coins[i].taken = true
			cx, cy := f.coins[i].box.Center()
			metrics.Event("pickup", float64(cx), float64(cy), f.coins[i].id)
			metrics.Inc("coins_taken")
		}
	}

	if boxesOverlap(pb, f.exitBox) {
		f.finished = true
		metrics.StopTimer("clear_time")
		metrics.Finish()
		return
	}

	diedTag := ""
	if pb.Top() > float32(f.rows)*f.tileSize {
		diedTag = "pit"
	} else if boxesOverlap(pb, f.slimeBox()) {
		diedTag = "slime"
	}
	if diedTag != "" {
		px, py := pb.Center()
		f.deaths = append(f.deaths, deathPos{step: f.stepCount - 1, x: float64(px), y: float64(py), tag: diedTag})
		metrics.Event("death", float64(px), float64(py), diedTag)
		f.lives--
		if f.lives <= 0 {
			f.failed = true
			metrics.Fail("out of lives")
			return
		}
		f.body.Shape = physics.BoxShape(physics.Box{X: f.startX, Y: f.startY, W: 10, H: 14})
		f.body.VX, f.body.VY = 0, 0
		f.ctrl.Reset()
	}
}

func fixturePlatformerConfig() *playtest.Config {
	return &playtest.Config{
		Version: playtest.CurrentVersion,
		Scope: playtest.Scope{
			Project:  "dungeon-run",
			Scene:    "level_01",
			Template: "platformer2d",
		},
		Runs: playtest.Runs{
			Count:       20,
			SeedStart:   1,
			Rate:        60,
			Speed:       1,
			MaxDuration: 15 * time.Second,
		},
		Bot: playtest.BotConfig{
			Profile:     playtest.ProfilePlatformerRunner,
			Strategy:    "Greedy route to the door, jumps at ledges and gaps, 120-220 ms reaction jitter",
			ReactionMin: 120 * time.Millisecond,
			ReactionMax: 220 * time.Millisecond,
		},
		Metrics: []playtest.Metric{
			{
				Name:  "finished",
				Label: "Finished",
				Measure: playtest.Measure{
					Kind:      playtest.MeasureOutcome,
					Key:       playtest.OutcomeFinished,
					Aggregate: playtest.AggCount,
				},
				Goal: &playtest.Goal{
					Summary: "at least half clear the level",
					Source:  "brief.yaml",
					Min:     playtest.Float64(1),
				},
			},
			{
				Name:  "median_time",
				Label: "Median time",
				Unit:  "s",
				Measure: playtest.Measure{
					Kind:      playtest.MeasureTimer,
					Key:       "clear_time",
					Aggregate: playtest.AggMedian,
				},
				Goal: &playtest.Goal{
					Summary: "quick stage",
					Source:  "brief.yaml",
					Min:     playtest.Float64(1),
					Max:     playtest.Float64(15),
				},
			},
			{
				Name:  "frame_time",
				Label: "Frame time",
				Unit:  "ms",
				Measure: playtest.Measure{
					Kind:      playtest.MeasurePerf,
					Key:       "frame_ms",
					Aggregate: playtest.AggMedian,
				},
				Goal: &playtest.Goal{
					Summary: "16.7 ms at 60 Hz",
					Source:  "game.yaml",
					Max:     playtest.Float64(16.7),
				},
			},
			{
				Name:  "coins_taken",
				Label: "Coins taken",
				Unit:  "%",
				Measure: playtest.Measure{
					Kind:      playtest.MeasureRatio,
					Key:       "coins_taken",
					DenomKey:  "coins_total",
					Aggregate: playtest.AggMean,
				},
				Goal: &playtest.Goal{
					Summary: "every coin on a route",
					Source:  "playtest.yaml",
					Min:     playtest.Float64(100),
				},
			},
			{
				Name:  "deaths",
				Label: "Deaths",
				Measure: playtest.Measure{
					Kind:      playtest.MeasureEvent,
					Key:       "death",
					Aggregate: playtest.AggSum,
					HeatMap:   true,
				},
			},
		},
	}
}

func makeFixtureSim(seed uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
	f, err := newFixturePlatformer(seed)
	if err != nil {
		return playtest.Sim{}, err
	}
	return playtest.Sim{
		Step: func(dt time.Duration) {
			f.step(src, metrics, dt)
		},
		Observe:    f.observe,
		FrameStats: f.stats,
	}, nil
}

// Acceptance test 1 & 2:
// - Twenty seeded runs of a fixture platformer give the exact same report twice.
// - A run's recording replays to the exact same death positions without the bot.
// - A metric whose goal is missed is reported as missed with its value, goal and source.
func TestTwentySeededRunsDeterministicAndReplayableToSameDeathPositions(t *testing.T) {
	cfg := fixturePlatformerConfig()
	recDir1 := filepath.Join(t.TempDir(), "recordings")
	recDir2 := filepath.Join(t.TempDir(), "recordings")

	res1, err := playtest.Run(cfg, makeFixtureSim, playtest.RunOptions{RecordingsDir: recDir1})
	if err != nil {
		t.Fatalf("first Run failed: %v", err)
	}
	res2, err := playtest.Run(cfg, makeFixtureSim, playtest.RunOptions{RecordingsDir: recDir2})
	if err != nil {
		t.Fatalf("second Run failed: %v", err)
	}

	if res1.Report.SeedCount != 20 || len(res1.Report.Runs) != 20 || len(res1.Recordings) != 20 {
		t.Fatalf("SeedCount=%d runs=%d recordings=%d, want 20", res1.Report.SeedCount, len(res1.Report.Runs), len(res1.Recordings))
	}

	yaml1, err := playtest.EncodeReport(res1.Report)
	if err != nil {
		t.Fatalf("EncodeReport 1: %v", err)
	}
	yaml2, err := playtest.EncodeReport(res2.Report)
	if err != nil {
		t.Fatalf("EncodeReport 2: %v", err)
	}
	if !bytes.Equal(yaml1, yaml2) {
		t.Fatalf("20 seeded runs produced different YAML reports:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", yaml1, yaml2)
	}

	// Verify report round-trip through ParseReport -> EncodeReport.
	parsed, err := playtest.ParseReport(yaml1)
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	yamlAgain, err := playtest.EncodeReport(parsed)
	if err != nil {
		t.Fatalf("EncodeReport(parsed): %v", err)
	}
	if !bytes.Equal(yaml1, yamlAgain) {
		t.Fatalf("report did not round-trip identically")
	}

	// Verify at least one run cleared and at least one run had a death event, so both clear and death paths are exercised.
	var clearedCount, deathRunIdx int
	deathRunIdx = -1
	for i, r := range res1.Report.Runs {
		if r.Outcome == playtest.OutcomeFinished {
			clearedCount++
		}
		for _, ev := range r.Events {
			if ev.Kind == "death" && deathRunIdx < 0 {
				deathRunIdx = i
			}
		}
	}
	if clearedCount == 0 {
		t.Fatalf("expected at least one of 20 runs to finish the fixture platformer; report:\n%s", res1.Report.Summary())
	}
	if deathRunIdx < 0 {
		t.Fatalf("expected at least one of 20 runs to record a death event; report:\n%s", res1.Report.Summary())
	}

	// Verify death heat map points match total death events across all runs.
	if len(res1.Report.HeatMaps) != 1 || res1.Report.HeatMaps[0].Event != "death" || len(res1.Report.HeatMaps[0].Points) == 0 {
		t.Fatalf("unexpected heatMaps: %+v", res1.Report.HeatMaps)
	}

	// Replay the saved .replay.yaml recording of a run with deaths (NO bot!) and verify identical death positions.
	deathRun := res1.Report.Runs[deathRunIdx]
	recFile := filepath.Join(filepath.Dir(recDir1), filepath.FromSlash(deathRun.Recording))
	loadedRec, err := replay.LoadFile(recFile)
	if err != nil {
		t.Fatalf("replay.LoadFile(%s): %v", recFile, err)
	}
	if loadedRec.Seed != deathRun.Seed {
		t.Fatalf("recording seed = %d, want %d", loadedRec.Seed, deathRun.Seed)
	}

	player := replay.NewPlayer(loadedRec)
	replayedSim, err := newFixturePlatformer(loadedRec.Seed)
	if err != nil {
		t.Fatalf("newFixturePlatformer: %v", err)
	}
	replayMetrics := playtest.NewCollector(loadedRec.Rate)
	dt := loadedRec.StepDuration()
	for step := 0; step < loadedRec.TotalSteps; step++ {
		replayMetrics.SetStep(step)
		replayedSim.step(player, replayMetrics, dt)
	}

	var wantDeaths, gotDeaths []deathPos
	for _, ev := range deathRun.Events {
		if ev.Kind == "death" {
			wantDeaths = append(wantDeaths, deathPos{step: ev.Step, x: ev.X, y: ev.Y, tag: ev.Tag})
		}
	}
	for _, ev := range replayMetrics.Events() {
		if ev.Kind == "death" {
			gotDeaths = append(gotDeaths, deathPos{step: ev.Step, x: ev.X, y: ev.Y, tag: ev.Tag})
		}
	}
	if !slices.Equal(gotDeaths, wantDeaths) {
		t.Fatalf("replayed death positions = %+v, want %+v", gotDeaths, wantDeaths)
	}

	// Acceptance test 2: coins_taken has goal >= 100% from playtest.yaml, but coin_03 is unreachable, so it must be reported as missed with its value and goal.
	if res1.Report.Passed {
		t.Fatalf("expected Report.Passed == false when coins_taken misses 100%% goal")
	}
	missed := res1.Report.MissedMetrics()
	if len(missed) == 0 {
		t.Fatal("expected at least one missed metric")
	}
	var coinsMetric *playtest.MetricReport
	var frameMetric *playtest.MetricReport
	for i := range res1.Report.Metrics {
		m := &res1.Report.Metrics[i]
		switch m.Name {
		case "coins_taken":
			coinsMetric = m
		case "frame_time":
			frameMetric = m
		}
	}
	if coinsMetric == nil || coinsMetric.Status != playtest.StatusMissed {
		t.Fatalf("coins_taken metric = %+v, want status=missed", coinsMetric)
	}
	if coinsMetric.Goal == nil || coinsMetric.Goal.Min == nil || *coinsMetric.Goal.Min != 100 || coinsMetric.Goal.Source != "playtest.yaml" {
		t.Fatalf("coins_taken goal = %+v, want min=100 from playtest.yaml", coinsMetric.Goal)
	}
	if coinsMetric.Value >= 100 {
		t.Fatalf("coins_taken value = %v, want < 100", coinsMetric.Value)
	}
	if frameMetric == nil || frameMetric.Status != playtest.StatusMet || math.Abs(frameMetric.Value-4.1) > 1e-6 {
		t.Fatalf("frame_time metric = %+v, want met with value 4.1 from perf readout", frameMetric)
	}
	summary := res1.Report.Summary()
	if !strings.Contains(summary, "[MISSED] Coins taken") || !strings.Contains(summary, "playtest.yaml") || !strings.Contains(summary, "100%") {
		t.Fatalf("Summary missing missed metric details:\n%s", summary)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	base := func() string {
		data, err := os.ReadFile(filepath.Join("testdata", "platformer.playtest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	// Sanity check base parses.
	if _, err := playtest.ParseConfig([]byte(base())); err != nil {
		t.Fatalf("base fixture failed to parse: %v", err)
	}

	cases := map[string]struct {
		yaml string
		want string
	}{
		"empty": {
			yaml: "   \n",
			want: "empty",
		},
		"unknown field": {
			yaml: "version: 1\nextraKey: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
			want: "extraKey",
		},
		"missing version": {
			yaml: "scope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
			want: "version",
		},
		"future version": {
			yaml: "version: 9\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
			want: "version",
		},
		"trailing doc": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n---\nversion: 1\n",
			want: "trailing",
		},
		"missing scene": {
			yaml: "version: 1\nscope: {}\nruns: {count: 1}\nbot: {profile: platformer-runner}\n",
			want: "scope.scene",
		},
		"no runs": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {}\nbot: {profile: platformer-runner}\n",
			want: "runs.count",
		},
		"reactionMax < reactionMin": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner, reactionMin: 200ms, reactionMax: 100ms}\n",
			want: "reactionMax",
		},
		"duplicate metric": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: m, measure: {kind: event, key: death}}\n  - {name: m, measure: {kind: event, key: death}}\n",
			want: "duplicate metric",
		},
		"ratio without denomKey": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: r, measure: {kind: ratio, key: a}}\n",
			want: "denomKey",
		},
		"invalid perf key": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: p, measure: {kind: perf, key: bogus}}\n",
			want: "unknown perf measure key",
		},
		"heatMap on non-event": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: c, measure: {kind: counter, key: k, heatMap: true}}\n",
			want: "heatMap",
		},
		"goal without bounds": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: c, measure: {kind: counter, key: k}, goal: {source: brief.yaml}}\n",
			want: "min, max or equal",
		},
		"goal min > max": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: c, measure: {kind: counter, key: k}, goal: {source: brief.yaml, min: 10, max: 5}}\n",
			want: "goal.max",
		},
		"goal equal with min": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner}\nmetrics:\n  - {name: c, measure: {kind: counter, key: k}, goal: {source: brief.yaml, equal: 5, min: 1}}\n",
			want: "cannot combine equal",
		},
		"non-scalar bot param": {
			yaml: "version: 1\nscope: {scene: s}\nruns: {count: 1}\nbot: {profile: platformer-runner, params: {nested: {a: 1}}}\n",
			want: "unsupported parameter value",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := playtest.ParseConfig([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseConfig error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestCollectorAndPerfReadout(t *testing.T) {
	// Nil collector is completely safe (no-op).
	var nilCol *playtest.Collector
	nilCol.SetStep(5)
	nilCol.Event("death", 1, 2, "pit")
	nilCol.Count("coins", 1)
	nilCol.Inc("coins")
	nilCol.SetCounter("total", 3)
	nilCol.StartTimer("run")
	nilCol.StopTimer("run")
	nilCol.RecordTimer("t", time.Second)
	nilCol.Finish()
	nilCol.Fail("nope")
	nilCol.RecordPerf(nil, nil)
	if nilCol.Done() || nilCol.Outcome() != playtest.OutcomeRunning || nilCol.Perf() != nil {
		t.Fatal("nil collector mutated state")
	}

	c := playtest.NewCollector(60)
	c.SetStep(0)
	c.StartTimer("clear")
	c.SetCounter("coins_total", 4)
	c.SetStep(30)
	c.Event("pickup", 100, 50, "coin_1")
	c.Inc("coins_taken")
	c.SetStep(120)
	dur := c.StopTimer("clear")
	if math.Abs(dur.Seconds()-2.0) > 1e-3 {
		t.Fatalf("StopTimer = %v, want ~2s", dur)
	}
	if sec, ok := c.Timer("clear"); !ok || sec != 2 {
		t.Fatalf("Timer(clear) = %v, %v, want 2, true", sec, ok)
	}
	if c.Counter("coins_taken") != 1 || c.Counter("coins_total") != 4 {
		t.Fatalf("counters = (%v, %v)", c.Counter("coins_taken"), c.Counter("coins_total"))
	}

	var stats gamedebug.FrameStats
	stats.Record(3*time.Millisecond, 2*time.Millisecond, 1*time.Millisecond)
	stats.Record(5*time.Millisecond, 3*time.Millisecond, 2*time.Millisecond)
	stats.Record(7*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	c.RecordPerf(&stats, nil)
	p := c.Perf()
	if p == nil || p.SampleCount != 3 || math.Abs(p.FrameMs-5) > 1e-6 || math.Abs(p.P99FrameMs-7) > 1e-6 || math.Abs(p.MaxFrameMs-7) > 1e-6 {
		t.Fatalf("Perf = %+v", p)
	}
}

func TestGoalEvaluateAndRunFailsIfSimIgnoresInput(t *testing.T) {
	eq := &playtest.Goal{Source: "playtest.yaml", Equal: playtest.Float64(0)}
	if eq.Evaluate(0) != playtest.StatusMet || eq.Evaluate(1) != playtest.StatusMissed {
		t.Fatal("Equal goal evaluation wrong")
	}
	rng := &playtest.Goal{Source: "brief.yaml", Min: playtest.Float64(40), Max: playtest.Float64(60)}
	if rng.Evaluate(39.9) != playtest.StatusMissed || rng.Evaluate(40) != playtest.StatusMet || rng.Evaluate(60) != playtest.StatusMet || rng.Evaluate(60.1) != playtest.StatusMissed {
		t.Fatal("Range goal evaluation wrong")
	}
	for _, nonFinite := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if eq.Evaluate(nonFinite) != playtest.StatusMissed {
			t.Fatalf("Equal goal evaluated %v as %s, want missed", nonFinite, eq.Evaluate(nonFinite))
		}
		if rng.Evaluate(nonFinite) != playtest.StatusMissed {
			t.Fatalf("Range goal evaluated %v as %s, want missed", nonFinite, rng.Evaluate(nonFinite))
		}
	}
	huge := playtest.Runs{Rate: 1_000_000, MaxDuration: 100_000 * time.Hour}
	if got := huge.MaxSteps(); got != math.MaxInt32 {
		t.Fatalf("MaxSteps overflow clamp = %d, want %d", got, math.MaxInt32)
	}

	cfg := &playtest.Config{
		Version: playtest.CurrentVersion,
		Scope:   playtest.Scope{Scene: "s"},
		Runs:    playtest.Runs{Count: 1, Rate: 60, MaxDuration: time.Second},
		Bot:     playtest.BotConfig{Profile: playtest.ProfilePlatformerRunner},
	}
	_, err := playtest.Run(cfg, func(_ uint64, _ replay.ActionSource, _ *playtest.Collector) (playtest.Sim, error) {
		return playtest.Sim{
			Step: func(time.Duration) {
				// Deliberately does NOT call src.Step().
			},
		}, nil
	}, playtest.RunOptions{})
	if err == nil || !strings.Contains(err.Error(), "must call src.Step") {
		t.Fatalf("expected error when sim ignores ActionSource, got %v", err)
	}
}

func TestRunTestHarnessEnvIntegration(t *testing.T) {
	dir := t.TempDir()
	cfg := fixturePlatformerConfig()
	cfg.Runs.Count = 3
	// Remove the failing coin metric so RunTest passes by default.
	cfg.Metrics = cfg.Metrics[:3]
	cfgPath := filepath.Join(dir, "playtest.yaml")
	if err := playtest.SaveConfigFile(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "report.yaml")
	recDir := filepath.Join(dir, "recs")

	t.Setenv(playtest.EnvConfig, cfgPath)
	t.Setenv(playtest.EnvOut, outPath)
	t.Setenv(playtest.EnvRecordings, recDir)
	t.Setenv(playtest.EnvSeeds, "1,2")

	res := playtest.RunTest(t, "", makeFixtureSim)
	if res.Report.SeedCount != 2 {
		t.Fatalf("SeedCount = %d, want 2 (from NSGO_PLAYTEST_SEEDS)", res.Report.SeedCount)
	}
	loaded, err := playtest.LoadReportFile(outPath)
	if err != nil {
		t.Fatalf("LoadReportFile: %v", err)
	}
	if loaded.SeedCount != 2 {
		t.Fatalf("loaded SeedCount = %d, want 2", loaded.SeedCount)
	}
	matches, err := filepath.Glob(filepath.Join(recDir, "*.replay.yaml"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("recordings = %v (err=%v), want 2 files", matches, err)
	}
}
