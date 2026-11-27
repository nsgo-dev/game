package playtest_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func TestAllBuiltInBotProfilesRegistered(t *testing.T) {
	got := playtest.BotProfiles()
	for _, want := range []string{
		playtest.ProfileCardAI,
		playtest.ProfilePlatformerRunner,
		playtest.ProfilePuzzleSolver,
		playtest.ProfileShooter,
		playtest.ProfileTopDownExplorer,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("BotProfiles() = %v, missing %q", got, want)
		}
	}
}

func TestTemplatePlaytestConfigsCanonicalAndRegistered(t *testing.T) {
	templates := []struct {
		name    string
		profile string
	}{
		{"platformer2d", playtest.ProfilePlatformerRunner},
		{"rpg2d", playtest.ProfileTopDownExplorer},
		{"shmup2d", playtest.ProfileShooter},
		{"puzzle2d", playtest.ProfilePuzzleSolver},
		{"cards2d", playtest.ProfileCardAI},
	}
	for _, tc := range templates {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "template", tc.name, "template", playtest.FileName)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			cfg, err := playtest.ParseConfig(raw)
			if err != nil {
				t.Fatalf("ParseConfig(%s): %v", path, err)
			}
			if cfg.Scope.Template != tc.name {
				t.Errorf("scope.template = %q, want %q", cfg.Scope.Template, tc.name)
			}
			if cfg.Bot.Profile != tc.profile {
				t.Errorf("bot.profile = %q, want %q", cfg.Bot.Profile, tc.profile)
			}
			encoded, err := playtest.EncodeConfig(cfg)
			if err != nil {
				t.Fatalf("EncodeConfig(%s): %v", path, err)
			}
			if !bytes.Equal(raw, encoded) {
				t.Fatalf("%s is not in canonical EncodeConfig form:\n--- got ---\n%s\n--- want ---\n%s", path, raw, encoded)
			}
			if _, err := playtest.NewBot(cfg.Bot); err != nil {
				t.Fatalf("NewBot(%q): %v", cfg.Bot.Profile, err)
			}
		})
	}
}

// =============================================================================
// 1. Top-Down Explorer fixture (modeled on rpg2d)
// =============================================================================

type fixtureExplorerWorld struct {
	rows   []string
	w, h   int
	player playtest.GridCell
	facing string
	seen   map[playtest.GridCell]bool
	total  int

	npcs      []playtest.ExplorerNPC
	talkCount map[string]int
	chests    []playtest.ExplorerChest
	openCount map[string]int

	inDialogue   bool
	dialogueStep int
	hasGoalKey   bool
	stepCount    int
	stats        *gamedebug.FrameStats
}

func newFixtureExplorerWorld() *fixtureExplorerWorld {
	// '#' = wall/tree, '.' = walkable grass/path.
	// Top-right corner (cols 10..12, rows 1..3) is walled off like an unreadable grove gate so map_seen < 100%.
	rows := []string{
		"##############",
		"#........#...#",
		"#..##....#...#",
		"#........#####",
		"#....##......#",
		"#............#",
		"#..##....##..#",
		"#............#",
		"##############",
	}
	w := &fixtureExplorerWorld{
		rows:      rows,
		w:         len(rows[0]),
		h:         len(rows),
		player:    playtest.GridCell{X: 1, Y: 1},
		facing:    "right",
		seen:      make(map[playtest.GridCell]bool),
		talkCount: make(map[string]int),
		openCount: make(map[string]int),
		npcs: []playtest.ExplorerNPC{
			{ID: "mira", Cell: playtest.GridCell{X: 3, Y: 1}},
			{ID: "bram", Cell: playtest.GridCell{X: 7, Y: 5}},
		},
		chests: []playtest.ExplorerChest{
			{ID: "chest_potion", Cell: playtest.GridCell{X: 1, Y: 7}, Solid: false},
			{ID: "chest_key", Cell: playtest.GridCell{X: 11, Y: 7}, Solid: true},
		},
		stats: &gamedebug.FrameStats{},
	}
	for _, row := range rows {
		for x := range len(row) {
			if row[x] != '#' {
				w.total++
			}
		}
	}
	w.markSeen()
	return w
}

func (w *fixtureExplorerWorld) walkable(x, y int) bool {
	if x < 0 || y < 0 || x >= w.w || y >= w.h {
		return false
	}
	return w.rows[y][x] != '#'
}

func (w *fixtureExplorerWorld) markSeen() {
	const r = 2
	for y := max(0, w.player.Y-r); y <= min(w.h-1, w.player.Y+r); y++ {
		for x := max(0, w.player.X-r); x <= min(w.w-1, w.player.X+r); x++ {
			dx, dy := x-w.player.X, y-w.player.Y
			if dx*dx+dy*dy <= r*r && w.walkable(x, y) {
				w.seen[playtest.GridCell{X: x, Y: y}] = true
			}
		}
	}
}

func (w *fixtureExplorerWorld) observe() any {
	return playtest.ExplorerObservation{
		MapID:        "overworld",
		Width:        w.w,
		Height:       w.h,
		Walkable:     w.walkable,
		Player:       w.player,
		Facing:       w.facing,
		InDialogue:   w.inDialogue,
		VisionRadius: 2,
		NPCs:         slices.Clone(w.npcs),
		Chests:       slices.Clone(w.chests),
	}
}

func (w *fixtureExplorerWorld) blockedByEntity(c playtest.GridCell) bool {
	for _, n := range w.npcs {
		if n.Cell == c {
			return true
		}
	}
	for _, ch := range w.chests {
		if ch.Solid && ch.Cell == c {
			return true
		}
	}
	return false
}

func (w *fixtureExplorerWorld) facedCell() playtest.GridCell {
	c := w.player
	switch w.facing {
	case "up":
		c.Y--
	case "down":
		c.Y++
	case "left":
		c.X--
	case "right":
		c.X++
	}
	return c
}

func (w *fixtureExplorerWorld) step(src replay.ActionSource, metrics *playtest.Collector, _ time.Duration) {
	src.Step()
	if w.stepCount == 0 {
		metrics.StartTimer("goal_time")
		metrics.SetCounter("tiles_total", float64(w.total))
		metrics.SetCounter("npcs_total", float64(len(w.npcs)))
		metrics.SetCounter("chests_total", float64(len(w.chests)))
	}
	w.stepCount++
	w.stats.Record(3200*time.Microsecond, 2400*time.Microsecond, 800*time.Microsecond)

	// Record time-per-tile heat map samples at tile center coordinates.
	metrics.Event("tile_visit", float64(w.player.X*16+8), float64(w.player.Y*16+8), "overworld")

	if w.inDialogue {
		if src.JustPressed("confirm") {
			w.dialogueStep++
			if w.dialogueStep >= 2 {
				w.inDialogue = false
				w.dialogueStep = 0
			}
		}
		return
	}

	if src.JustPressed("confirm") {
		front := w.facedCell()
		for i := range w.npcs {
			if w.npcs[i].Cell == front {
				w.talkCount[w.npcs[i].ID]++
				if !w.npcs[i].Talked {
					w.npcs[i].Talked = true
					metrics.Inc("npcs_talked")
				}
				w.inDialogue = true
				w.dialogueStep = 0
				return
			}
		}
		for i := range w.chests {
			if w.chests[i].Cell == front || (!w.chests[i].Solid && w.chests[i].Cell == w.player) {
				if !w.chests[i].Opened {
					w.chests[i].Opened = true
					w.openCount[w.chests[i].ID]++
					metrics.Inc("chests_opened")
					if w.chests[i].ID == "chest_key" {
						w.hasGoalKey = true
						metrics.StopTimer("goal_time")
					}
				}
				return
			}
		}
	}

	// Move every 4 steps (simulating tile walk cadence).
	if w.stepCount%4 == 0 {
		var dx, dy int
		var dir string
		switch {
		case src.Pressed("up") || src.Axis("moveY") < -0.5:
			dy, dir = -1, "up"
		case src.Pressed("down") || src.Axis("moveY") > 0.5:
			dy, dir = 1, "down"
		case src.Pressed("left") || src.Axis("moveX") < -0.5:
			dx, dir = -1, "left"
		case src.Pressed("right") || src.Axis("moveX") > 0.5:
			dx, dir = 1, "right"
		}
		if dir != "" {
			w.facing = dir
			next := playtest.GridCell{X: w.player.X + dx, Y: w.player.Y + dy}
			if w.walkable(next.X, next.Y) && !w.blockedByEntity(next) {
				w.player = next
				w.markSeen()
				// Non-solid chest pickup on walk-over.
				for i := range w.chests {
					if !w.chests[i].Solid && !w.chests[i].Opened && w.chests[i].Cell == w.player {
						w.chests[i].Opened = true
						w.openCount[w.chests[i].ID]++
						metrics.Inc("chests_opened")
					}
				}
			}
		}
	}

	metrics.SetCounter("tiles_seen", float64(len(w.seen)))
	allNPCs := true
	for _, n := range w.npcs {
		if !n.Talked {
			allNPCs = false
		}
	}
	allChests := true
	for _, c := range w.chests {
		if !c.Opened {
			allChests = false
		}
	}
	if w.hasGoalKey && allNPCs && allChests {
		metrics.Finish()
	}
}

func TestTopDownExplorerFixtureDeterminism(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "..", "template", "rpg2d", "template", playtest.FileName)
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if cfg.Runs.Speed != 8 {
		t.Fatalf("rpg2d playtest speed = %v, want 8", cfg.Runs.Speed)
	}
	cfg.Runs.Count = 6
	cfg.Runs.MaxDuration = 30 * time.Second

	var lastWorlds []*fixtureExplorerWorld
	makeSim := func(_ uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		w := newFixtureExplorerWorld()
		lastWorlds = append(lastWorlds, w)
		return playtest.Sim{
			Step:       func(dt time.Duration) { w.step(src, metrics, dt) },
			Observe:    w.observe,
			FrameStats: w.stats,
		}, nil
	}

	res1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	worlds1 := slices.Clone(lastWorlds)
	lastWorlds = nil

	res2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	y1, err := playtest.EncodeReport(res1.Report)
	if err != nil {
		t.Fatalf("EncodeReport 1: %v", err)
	}
	y2, err := playtest.EncodeReport(res2.Report)
	if err != nil {
		t.Fatalf("EncodeReport 2: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("topdown-explorer reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}
	if res1.Report.Speed != 8 {
		t.Fatalf("report.Speed = %v, want 8", res1.Report.Speed)
	}

	// Verify every run talked to each NPC exactly once, opened every chest, and reached the goal key.
	for i, w := range worlds1 {
		if !w.hasGoalKey {
			t.Errorf("run %d did not reach chest_key", i+1)
		}
		for _, npc := range w.npcs {
			if w.talkCount[npc.ID] != 1 {
				t.Errorf("run %d talked to NPC %q %d times, want 1", i+1, npc.ID, w.talkCount[npc.ID])
			}
		}
		for _, ch := range w.chests {
			if w.openCount[ch.ID] != 1 {
				t.Errorf("run %d opened chest %q %d times, want 1", i+1, ch.ID, w.openCount[ch.ID])
			}
		}
	}

	// Verify time-per-tile heat map and map_seen metric (< 100% because of walled-off grove).
	if len(res1.Report.HeatMaps) != 1 || res1.Report.HeatMaps[0].Event != "tile_visit" || len(res1.Report.HeatMaps[0].Points) == 0 {
		t.Fatalf("expected non-empty tile_visit heatMap, got %+v", res1.Report.HeatMaps)
	}
	var mapSeen, goalTime *playtest.MetricReport
	for i := range res1.Report.Metrics {
		m := &res1.Report.Metrics[i]
		switch m.Name {
		case "map_seen":
			mapSeen = m
		case "time_to_goal":
			goalTime = m
		}
	}
	if mapSeen == nil || mapSeen.Value <= 50 || mapSeen.Value >= 100 {
		t.Fatalf("map_seen = %+v, want partial exploration in (50, 100) due to walled grove", mapSeen)
	}
	if goalTime == nil || goalTime.Value <= 0 {
		t.Fatalf("time_to_goal = %+v, want positive duration", goalTime)
	}

	// Verify explorer gives up with OutcomeTimeout when maxDuration is too short.
	shortCfg := cfg.Clone()
	shortCfg.Runs.Count = 1
	shortCfg.Runs.MaxDuration = 100 * time.Millisecond
	shortRes, err := playtest.Run(shortCfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("short timeout Run: %v", err)
	}
	if shortRes.Report.Runs[0].Outcome != playtest.OutcomeTimeout {
		t.Fatalf("short run outcome = %q, want %q", shortRes.Report.Runs[0].Outcome, playtest.OutcomeTimeout)
	}
}

// =============================================================================
// 2. Shooter fixture (modeled on shmup2d)
// =============================================================================

type fixtureShooter struct {
	w, h        float64
	shipX       float64
	shipY       float64
	shipRadius  float64
	shipSpeed   float64
	lives       int
	invulnSteps int
	wave        int
	stepCount   int
	bullets     []playtest.ShooterBullet
	enemies     []playtest.ShooterTarget
	powerUps    []playtest.ShooterPowerUp
	rng         *random.Stream
	stats       *gamedebug.FrameStats
}

func newFixtureShooter(seed uint64) *fixtureShooter {
	return &fixtureShooter{
		w:          270,
		h:          480,
		shipX:      135,
		shipY:      400,
		shipRadius: 4,
		shipSpeed:  170,
		lives:      6,
		wave:       1,
		rng:        random.NewStreams(seed).Stream("shmup"),
		stats:      &gamedebug.FrameStats{},
	}
}

func (s *fixtureShooter) observe() any {
	return playtest.ShooterObservation{
		PlayfieldWidth:  s.w,
		PlayfieldHeight: s.h,
		ShipX:           s.shipX,
		ShipY:           s.shipY,
		ShipRadius:      s.shipRadius,
		ShipAlive:       s.lives > 0,
		Invulnerable:    s.invulnSteps > 0,
		Wave:            s.wave,
		Bullets:         s.bullets,
		Enemies:         s.enemies,
		PowerUps:        s.powerUps,
	}
}

func (s *fixtureShooter) step(src replay.ActionSource, metrics *playtest.Collector, dt time.Duration) {
	src.Step()
	if s.stepCount == 0 {
		target := 1.0
		for w := 1; w <= 4; w++ {
			playtest.RecordShooterWave(metrics, fmt.Sprintf("wave %d", w), &target)
		}
	}
	s.stepCount++
	secs := dt.Seconds()
	s.stats.Record(5200*time.Microsecond, 4000*time.Microsecond, 1200*time.Microsecond)

	// 4 waves of 90 steps (1.5s) each = 360 steps (6s total stage).
	s.wave = min(4, 1+(s.stepCount-1)/90)
	waveLabel := fmt.Sprintf("wave %d", s.wave)

	// Spawn wave enemies & power-ups deterministically.
	if s.stepCount%90 == 1 {
		s.enemies = []playtest.ShooterTarget{
			{X: 70 + s.rng.Float64()*130, Y: 90, Radius: 12},
		}
		if s.wave == 1 || s.wave == 3 {
			pu := playtest.ShooterPowerUp{
				ID:   fmt.Sprintf("pu_%d", s.wave),
				Kind: "power",
				X:    s.shipX + (s.rng.Float64()-0.5)*70,
				Y:    s.shipY - 55,
			}
			s.powerUps = append(s.powerUps, pu)
			playtest.RecordShooterPowerUp(metrics, false, pu.X, pu.Y, pu.Kind)
		}
	}

	// Wave 4 fires denser, faster aimed spreads; waves 1..3 fire lighter volleys.
	fireEvery := 24
	spreadCount := 3
	bulletSpeed := 175.0
	if s.wave == 4 {
		fireEvery = 10
		spreadCount = 7
		bulletSpeed = 240.0
	}
	if s.stepCount%fireEvery == 0 && len(s.enemies) > 0 {
		e := s.enemies[0]
		baseAngle := math.Atan2(s.shipY-e.Y, s.shipX-e.X)
		for k := 0; k < spreadCount; k++ {
			offset := (float64(k) - float64(spreadCount-1)/2.0) * 0.14
			ang := baseAngle + offset
			s.bullets = append(s.bullets, playtest.ShooterBullet{
				X:      e.X,
				Y:      e.Y,
				VX:     math.Cos(ang) * bulletSpeed,
				VY:     math.Sin(ang) * bulletSpeed,
				Radius: 4,
			})
		}
	}

	// Move ship using bot's moveX/moveY axes and focus modifier.
	speed := s.shipSpeed
	if src.Pressed("focus") {
		speed = 80
	}
	mx, my := src.Axis("moveX"), src.Axis("moveY")
	if l := math.Hypot(mx, my); l > 1 {
		mx /= l
		my /= l
	}
	s.shipX = min(max(s.shipX+mx*speed*secs, 10), s.w-10)
	s.shipY = min(max(s.shipY+my*speed*secs, s.h*0.45), s.h-10)
	if src.Pressed("fire") {
		metrics.Inc("shots_fired")
	}

	// Advance bullets and check ship hits.
	if s.invulnSteps > 0 {
		s.invulnSteps--
	}
	kept := s.bullets[:0]
	for _, b := range s.bullets {
		b.X += b.VX * secs
		b.Y += b.VY * secs
		if b.X < -10 || b.X > s.w+10 || b.Y < -10 || b.Y > s.h+10 {
			continue
		}
		if s.invulnSteps == 0 && math.Hypot(b.X-s.shipX, b.Y-s.shipY) <= b.Radius+s.shipRadius {
			playtest.RecordShooterHit(metrics, waveLabel, s.shipX, s.shipY)
			s.lives--
			s.invulnSteps = 30
			if s.lives <= 0 {
				metrics.Fail("out of lives on " + waveLabel)
				return
			}
			continue
		}
		kept = append(kept, b)
	}
	s.bullets = kept

	// Advance power-ups and check collection.
	keptPU := s.powerUps[:0]
	for _, pu := range s.powerUps {
		pu.Y += 45 * secs
		if math.Hypot(pu.X-s.shipX, pu.Y-s.shipY) <= 16 {
			playtest.RecordShooterPowerUp(metrics, true, pu.X, pu.Y, pu.Kind)
			continue
		}
		if pu.Y <= s.h+16 {
			keptPU = append(keptPU, pu)
		}
	}
	s.powerUps = keptPU

	if s.stepCount >= 360 {
		metrics.Finish()
	}
}

func TestShooterBotFixtureDeterminismAndReactionTime(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "..", "template", "shmup2d", "template", playtest.FileName)
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	cfg.Runs.Count = 8
	cfg.Runs.MaxDuration = 10 * time.Second

	makeSim := func(seed uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		s := newFixtureShooter(seed)
		return playtest.Sim{
			Step:       func(dt time.Duration) { s.step(src, metrics, dt) },
			Observe:    s.observe,
			FrameStats: s.stats,
		}, nil
	}

	res1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	res2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	y1, err := playtest.EncodeReport(res1.Report)
	if err != nil {
		t.Fatalf("EncodeReport 1: %v", err)
	}
	y2, err := playtest.EncodeReport(res2.Report)
	if err != nil {
		t.Fatalf("EncodeReport 2: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("shooter reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}

	// Verify hits_per_wave series exists with all 4 waves.
	if len(res1.Report.Series) != 1 || res1.Report.Series[0].Name != "hits_per_wave" || len(res1.Report.Series[0].Entries) != 4 {
		t.Fatalf("unexpected series = %+v, want 4-wave hits_per_wave", res1.Report.Series)
	}
	// Verify power-ups were collected and shots fired.
	var puMetric *playtest.MetricReport
	for i := range res1.Report.Metrics {
		if res1.Report.Metrics[i].Name == "powerups_taken" {
			puMetric = &res1.Report.Metrics[i]
		}
	}
	if puMetric == nil || puMetric.Value <= 0 {
		t.Fatalf("powerups_taken = %+v, want > 0%% collected", puMetric)
	}
	if res1.Report.Runs[0].Counters["shots_fired"] == 0 {
		t.Fatal("expected shooter bot to fire constantly")
	}

	// Verify fixed reaction time meaningfully governs dodging: a much slower reaction time takes more total hits.
	fastCfg := cfg.Clone()
	fastCfg.Bot.ReactionMin = 0
	fastCfg.Bot.ReactionMax = 0
	slowCfg := cfg.Clone()
	slowCfg.Bot.ReactionMin = 500 * time.Millisecond
	slowCfg.Bot.ReactionMax = 500 * time.Millisecond

	fastRes, err := playtest.Run(fastCfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("fast Run: %v", err)
	}
	slowRes, err := playtest.Run(slowCfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("slow Run: %v", err)
	}
	totalHits := func(r *playtest.Report) float64 {
		var sum float64
		for _, run := range r.Runs {
			sum += run.Counters["hits_taken"]
		}
		return sum
	}
	if totalHits(slowRes.Report) <= totalHits(fastRes.Report) {
		t.Fatalf("expected 500ms reaction hits (%v) > 0ms reaction hits (%v)", totalHits(slowRes.Report), totalHits(fastRes.Report))
	}
}

// =============================================================================
// 3. Puzzle Solver fixture (modeled on puzzle2d's Spark Slide)
// =============================================================================

type slidePuzzleGrid struct {
	w, h   int
	rows   []string
	lamps  []playtest.GridCell
	lampAt map[playtest.GridCell]int
}

func newSlidePuzzleGrid(rows []string) (*slidePuzzleGrid, slidePuzzleState) {
	g := &slidePuzzleGrid{
		w:      len(rows[0]),
		h:      len(rows),
		rows:   rows,
		lampAt: make(map[playtest.GridCell]int),
	}
	var start playtest.GridCell
	for y, row := range rows {
		for x := range len(row) {
			switch row[x] {
			case 'S':
				start = playtest.GridCell{X: x, Y: y}
			case 'o':
				g.lampAt[playtest.GridCell{X: x, Y: y}] = len(g.lamps)
				g.lamps = append(g.lamps, playtest.GridCell{X: x, Y: y})
			}
		}
	}
	return g, slidePuzzleState{grid: g, spark: start}
}

type slidePuzzleState struct {
	grid   *slidePuzzleGrid
	spark  playtest.GridCell
	litBit uint32
	dead   bool
}

func (s slidePuzzleState) Key() string {
	return fmt.Sprintf("%d,%d:%x:%t", s.spark.X, s.spark.Y, s.litBit, s.dead)
}

func (s slidePuzzleState) Solved() bool {
	if s.dead {
		return false
	}
	want := uint32((1 << len(s.grid.lamps)) - 1)
	return s.litBit == want
}

func (s slidePuzzleState) Dead() bool { return s.dead }

func (s slidePuzzleState) ProgressScore() float64 {
	lit := 0
	for i := range len(s.grid.lamps) {
		if s.litBit&(1<<i) != 0 {
			lit++
		}
	}
	return float64(lit)
}

func (s slidePuzzleState) Moves() []playtest.PuzzleMove {
	if s.Solved() || s.dead {
		return nil
	}
	return []playtest.PuzzleMove{"up", "right", "down", "left"}
}

func (s slidePuzzleState) Apply(mv playtest.PuzzleMove) (playtest.PuzzleState, bool) {
	var dx, dy int
	switch mv {
	case "up":
		dy = -1
	case "down":
		dy = 1
	case "left":
		dx = -1
	case "right":
		dx = 1
	default:
		return nil, false
	}
	next := s
	moved := false
	for {
		cx, cy := next.spark.X+dx, next.spark.Y+dy
		if cx < 0 || cy < 0 || cx >= s.grid.w || cy >= s.grid.h || s.grid.rows[cy][cx] == '#' {
			break
		}
		next.spark = playtest.GridCell{X: cx, Y: cy}
		moved = true
		switch s.grid.rows[cy][cx] {
		case 'o':
			if idx, ok := s.grid.lampAt[next.spark]; ok {
				next.litBit |= 1 << idx
			}
		case 'x':
			next.dead = true
			return next, true
		}
	}
	if !moved {
		return nil, false
	}
	return next, true
}

func fixturePuzzleLevels() []playtest.PuzzleLevel {
	// Level 1: 3-move tutorial (from puzzle2d levels.yaml "first-light").
	_, s1 := newSlidePuzzleGrid([]string{
		"#######",
		"#S...o#",
		"#.###.#",
		"#o....#",
		"#######",
	})
	// Level 2: 5-move level (from puzzle2d levels.yaml "crossroads").
	_, s2 := newSlidePuzzleGrid([]string{
		"#######",
		"##...o#",
		"#....S#",
		"#..x..#",
		"#..o..#",
		"##.x.o#",
		"#.#...#",
		"#######",
	})
	// Level 3: 3-move level placed after 5-move level -> intentional difficulty dip!
	_, s3 := newSlidePuzzleGrid([]string{
		"#######",
		"#S..x.#",
		"#.#...#",
		"#.#.#o#",
		"#o....#",
		"#######",
	})
	// Level 4 ("shortcut"): designer intended a 7-move route around the maze,
	// but sliding right -> down -> left -> up along the outer wall lights every lamp in just 3 moves!
	_, s4 := newSlidePuzzleGrid([]string{
		"#######",
		"#S...o#",
		"#.#.#.#",
		"#.....#",
		"#o...o#",
		"#######",
	})

	return []playtest.PuzzleLevel{
		{ID: "level_01", Name: "First Light", Par: 3, IntendedMoves: 3, Start: s1},
		{ID: "level_02", Name: "Crossroads", Par: 5, IntendedMoves: 5, Start: s2},
		{ID: "level_03", Name: "Mind the Void", Par: 3, IntendedMoves: 3, Start: s3},
		{ID: "level_04", Name: "Shortcut Room", Par: 7, IntendedMoves: 7, Start: s4},
	}
}

func TestPuzzleSolverFindsKnownShorterSolutionAndIsDeterministic(t *testing.T) {
	levels := fixturePuzzleLevels()

	// Acceptance check: BFS finds the exact known 3-move shorter solution in level_04 (intended 7).
	shortcut := playtest.SolvePuzzleBFS(levels[3].Start, 100_000)
	if !shortcut.Solved {
		t.Fatalf("SolvePuzzleBFS failed to solve level_04: %+v", shortcut)
	}
	if shortcut.Moves >= levels[3].IntendedMoves {
		t.Fatalf("level_04 shortest moves = %d, want strictly shorter than intended %d", shortcut.Moves, levels[3].IntendedMoves)
	}
	if shortcut.Moves != 3 {
		t.Fatalf("level_04 shortest moves = %d, want exact known shortcut of 3 moves (%v)", shortcut.Moves, shortcut.Solution)
	}
	// Replay BFS solution step-by-step to prove it genuinely solves level_04.
	cur := levels[3].Start
	for _, mv := range shortcut.Solution {
		next, ok := cur.Apply(mv)
		if !ok || next == nil || next.Dead() {
			t.Fatalf("BFS solution move %q produced invalid/dead state", mv)
		}
		cur = next
	}
	if !cur.Solved() {
		t.Fatal("replaying BFS solution did not reach Solved state")
	}

	// Verify stateLimit is respected when too tight.
	limited := playtest.SolvePuzzleBFS(levels[1].Start, 2)
	if !limited.HitStateLimit {
		t.Fatalf("expected HitStateLimit=true with maxStates=2, got %+v", limited)
	}

	// Run full puzzle pack through playtest.Run using puzzle2d's playtest.yaml twice for determinism.
	cfgPath := filepath.Join("..", "..", "..", "template", "puzzle2d", "template", playtest.FileName)
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	cfg.Runs.Count = 6
	cfg.Runs.MaxDuration = 30 * time.Second
	cfg.Bot.Params["mistakeRate"] = 0.45

	makeSim := playtest.NewPuzzlePackSim(levels, cfg.Bot)
	res1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	res2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	y1, err := playtest.EncodeReport(res1.Report)
	if err != nil {
		t.Fatalf("EncodeReport 1: %v", err)
	}
	y2, err := playtest.EncodeReport(res2.Report)
	if err != nil {
		t.Fatalf("EncodeReport 2: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("puzzle-solver reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}

	// Verify metrics: 100% solvable, 1 difficulty dip (level_03 after level_02), 1 shorter solution (level_04).
	byName := map[string]playtest.MetricReport{}
	for _, m := range res1.Report.Metrics {
		byName[m.Name] = m
	}
	if m := byName["solvable"]; m.Value != 100 || m.Status != playtest.StatusMet {
		t.Errorf("solvable = %+v, want 100%% met", m)
	}
	if m := byName["difficulty_order"]; m.Value != 1 || m.Status != playtest.StatusMissed {
		t.Errorf("difficulty_order = %+v, want 1 dip missed", m)
	}
	if m := byName["shorter_solutions"]; m.Value != 1 || m.Status != playtest.StatusMissed {
		t.Errorf("shorter_solutions = %+v, want 1 missed", m)
	}

	// Verify shortest_solution series flags both the difficulty dip and the shorter solution.
	if len(res1.Report.Series) != 1 || res1.Report.Series[0].Name != "shortest_solution" || len(res1.Report.Series[0].Entries) != 4 {
		t.Fatalf("unexpected series = %+v", res1.Report.Series)
	}
	entries := res1.Report.Series[0].Entries
	if entries[2].Label != "level_03" || entries[2].Flag != playtest.FlagProblem {
		t.Errorf("expected level_03 difficulty dip flagged problem, got %+v", entries[2])
	}
	if entries[3].Label != "level_04" || entries[3].Value != 3 || entries[3].Flag != playtest.FlagWarning {
		t.Errorf("expected level_04 shorter solution (3 moves) flagged warning, got %+v", entries[3])
	}

	// Verify human-like player actually performed undos across the seeded runs.
	var totalUndos float64
	for _, r := range res1.Report.Runs {
		totalUndos += r.Counters["human_undos"]
	}
	if totalUndos == 0 {
		t.Fatalf("expected human-like puzzle player to trigger undo after bad moves across 6 seeded runs")
	}

	// Verify direct EvaluatePuzzlePack matches.
	packRep, err := playtest.EvaluatePuzzlePack(levels, cfg.Bot, 42)
	if err != nil {
		t.Fatalf("EvaluatePuzzlePack: %v", err)
	}
	if packRep.SolvableCount != 4 || packRep.ShorterSolutions != 1 || packRep.DifficultyDips != 1 {
		t.Fatalf("EvaluatePuzzlePack = %+v, want solvable=4 shorter=1 dips=1", packRep)
	}
	if !packRep.Levels[3].ShorterThanIntended || packRep.Levels[3].Solve.Moves != 3 {
		t.Fatalf("level_04 report = %+v, want ShorterThanIntended=true Moves=3", packRep.Levels[3])
	}
}

// =============================================================================
// 4. Card AI fixture (modeled on cards2d / Ember vs Frost vs Thorn deck duels)
// =============================================================================

type fixtureCard struct {
	name   string
	cost   int
	damage int
	shield int
	draw   int
}

type fixtureCardPlayer struct {
	deckName string
	hp       int
	shield   int
	mana     int
	maxMana  int
	deck     []fixtureCard
	hand     []fixtureCard
}

type fixtureCardState struct {
	players [2]fixtureCardPlayer
	active  int
	turn    int
}

func makeFixtureDeck(name string, seed uint64) []fixtureCard {
	var cards []fixtureCard
	switch name {
	case "Ember":
		// Aggressive burn deck with cheap Kindle card draw -> strong vs Frost.
		for range 5 {
			cards = append(cards, fixtureCard{name: "Kindle", cost: 1, damage: 4, draw: 1})
		}
		for range 5 {
			cards = append(cards, fixtureCard{name: "Flare", cost: 2, damage: 6})
		}
		for range 4 {
			cards = append(cards, fixtureCard{name: "Cinder", cost: 1, damage: 3, shield: 1})
		}
	case "Frost":
		// Slower defensive control deck.
		for range 5 {
			cards = append(cards, fixtureCard{name: "Chill", cost: 1, damage: 2, shield: 2})
		}
		for range 5 {
			cards = append(cards, fixtureCard{name: "Glacier", cost: 2, damage: 3, shield: 4})
		}
		for range 4 {
			cards = append(cards, fixtureCard{name: "Shard", cost: 1, damage: 3})
		}
	default: // Thorn
		// Midrange deck.
		for range 5 {
			cards = append(cards, fixtureCard{name: "Briar", cost: 1, damage: 3, shield: 2})
		}
		for range 5 {
			cards = append(cards, fixtureCard{name: "Bramble", cost: 2, damage: 5, shield: 2})
		}
		for range 4 {
			cards = append(cards, fixtureCard{name: "Sprout", cost: 1, damage: 2, draw: 1})
		}
	}
	rng := random.New(seed)
	random.Shuffle(rng, cards)
	return cards
}

func newFixtureCardMatch(deckA, deckB string, firstPlayer int, seed uint64) (playtest.CardGameState, error) {
	st := &fixtureCardState{
		active: firstPlayer,
		turn:   1,
	}
	names := [2]string{deckA, deckB}
	for p := range 2 {
		deck := makeFixtureDeck(names[p], seed^uint64((p+1)*7919))
		hand := slices.Clone(deck[:3])
		deck = slices.Clone(deck[3:])
		st.players[p] = fixtureCardPlayer{
			deckName: names[p],
			hp:       20,
			mana:     2,
			maxMana:  2,
			deck:     deck,
			hand:     hand,
		}
	}
	return st, nil
}

func (s *fixtureCardState) clone() *fixtureCardState {
	cp := *s
	for p := range 2 {
		cp.players[p].deck = slices.Clone(s.players[p].deck)
		cp.players[p].hand = slices.Clone(s.players[p].hand)
	}
	return &cp
}

func (s *fixtureCardState) ActivePlayer() int { return s.active }
func (s *fixtureCardState) Turn() int         { return s.turn }

func (s *fixtureCardState) Winner() (int, bool) {
	switch {
	case s.players[0].hp <= 0 && s.players[1].hp <= 0:
		return -1, true
	case s.players[1].hp <= 0:
		return 0, true
	case s.players[0].hp <= 0:
		return 1, true
	default:
		return -1, false
	}
}

func (s *fixtureCardState) LegalActions() []playtest.CardAction {
	if _, done := s.Winner(); done {
		return nil
	}
	p := &s.players[s.active]
	acts := make([]playtest.CardAction, 0, len(p.hand)+1)
	for i, c := range p.hand {
		if c.cost <= p.mana {
			acts = append(acts, playtest.CardAction{
				ID:     fmt.Sprintf("play:%d:%s", i, c.name),
				Action: "play",
				Index:  i,
			})
		}
	}
	acts = append(acts, playtest.CardAction{ID: "end_turn", Action: "end_turn"})
	return acts
}

func (s *fixtureCardState) Apply(act playtest.CardAction) playtest.CardGameState {
	cp := s.clone()
	me := &cp.players[cp.active]
	opp := &cp.players[1-cp.active]

	if act.Action == "play" && act.Index >= 0 && act.Index < len(me.hand) {
		c := me.hand[act.Index]
		if c.cost <= me.mana {
			me.mana -= c.cost
			me.hand = slices.Delete(me.hand, act.Index, act.Index+1)
			me.shield += c.shield
			dmg := c.damage
			if opp.shield > 0 {
				blocked := min(opp.shield, dmg)
				opp.shield -= blocked
				dmg -= blocked
			}
			opp.hp -= dmg
			for range c.draw {
				if len(me.deck) > 0 && len(me.hand) < 6 {
					me.hand = append(me.hand, me.deck[0])
					me.deck = me.deck[1:]
				}
			}
			return cp
		}
	}

	// End turn: pass control to opponent, ramp mana up to 4, and draw a card.
	cp.active = 1 - cp.active
	if cp.active == 0 {
		cp.turn++
	}
	nextP := &cp.players[cp.active]
	nextP.maxMana = min(4, nextP.maxMana+1)
	nextP.mana = nextP.maxMana
	if len(nextP.deck) > 0 && len(nextP.hand) < 6 {
		nextP.hand = append(nextP.hand, nextP.deck[0])
		nextP.deck = nextP.deck[1:]
	} else if len(nextP.deck) == 0 {
		// Fatigue damage prevents infinite stalls.
		nextP.hp -= 2
	}
	return cp
}

func (s *fixtureCardState) Evaluate(player int) float64 {
	me := s.players[player]
	opp := s.players[1-player]
	return float64(me.hp-opp.hp)*2.0 + float64(me.shield-opp.shield)*0.8 + float64(len(me.hand)-len(opp.hand))*1.2
}

func TestCardAIFixtureTournamentDeterminismAndMatrix(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "..", "template", "cards2d", "template", playtest.FileName)
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	cfg.Runs.Count = 2
	cfg.Runs.MaxDuration = 3 * time.Minute
	cfg.Bot.Params["matchesPerPair"] = 12

	decks := []string{"Ember", "Frost", "Thorn"}
	makeSim := playtest.NewCardTournamentSim(decks, newFixtureCardMatch, cfg.Bot)

	res1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	res2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	y1, err := playtest.EncodeReport(res1.Report)
	if err != nil {
		t.Fatalf("EncodeReport 1: %v", err)
	}
	y2, err := playtest.EncodeReport(res2.Report)
	if err != nil {
		t.Fatalf("EncodeReport 2: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("card-ai reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}

	// Verify report round-trips through ParseReport -> EncodeReport with MatrixChart intact.
	parsed, err := playtest.ParseReport(y1)
	if err != nil {
		t.Fatalf("ParseReport: %v", err)
	}
	yAgain, err := playtest.EncodeReport(parsed)
	if err != nil {
		t.Fatalf("EncodeReport(parsed): %v", err)
	}
	if !bytes.Equal(y1, yAgain) {
		t.Fatal("card-ai report did not round-trip identically")
	}

	// Verify 3x3 win-rate matrix exists, has nil diagonal, and non-nil complementary off-diagonal entries.
	if len(res1.Report.Matrices) != 1 {
		t.Fatalf("expected 1 win-rate matrix, got %d", len(res1.Report.Matrices))
	}
	mat := res1.Report.Matrices[0]
	if mat.Name != "deck_win_rates" || !slices.Equal(mat.Labels, decks) {
		t.Fatalf("unexpected matrix = %+v", mat)
	}
	if mat.FlagAbove == nil || *mat.FlagAbove != 60 {
		t.Fatalf("matrix FlagAbove = %v, want 60", mat.FlagAbove)
	}
	for i := range decks {
		if mat.Cells[i][i] != nil {
			t.Errorf("diagonal cell [%d][%d] = %v, want nil", i, i, *mat.Cells[i][i])
		}
		for j := i + 1; j < len(decks); j++ {
			if mat.Cells[i][j] == nil || mat.Cells[j][i] == nil {
				t.Errorf("off-diagonal pair (%s, %s) missing cell value", decks[i], decks[j])
				continue
			}
			sum := *mat.Cells[i][j] + *mat.Cells[j][i]
			if sum > 100.01 || sum < 0 {
				t.Errorf("pair (%s, %s) win rates sum to %v", decks[i], decks[j], sum)
			}
		}
	}

	// Verify all four card metrics were recorded cleanly.
	byName := map[string]playtest.MetricReport{}
	for _, m := range res1.Report.Metrics {
		byName[m.Name] = m
	}
	for _, name := range []string{"first_player_wins", "match_length", "most_lopsided_pair", "ai_think_time"} {
		m, ok := byName[name]
		if !ok {
			t.Errorf("missing metric %q", name)
			continue
		}
		if m.Value <= 0 {
			t.Errorf("metric %q = %v, want > 0", name, m.Value)
		}
	}

	// Verify direct RunCardTournament alternates who goes first evenly and is deterministic.
	t1, err := playtest.RunCardTournament(decks, newFixtureCardMatch, cfg.Bot, 99)
	if err != nil {
		t.Fatalf("RunCardTournament 1: %v", err)
	}
	t2, err := playtest.RunCardTournament(decks, newFixtureCardMatch, cfg.Bot, 99)
	if err != nil {
		t.Fatalf("RunCardTournament 2: %v", err)
	}
	if t1.MatchesPlayed != 36 || t1.FirstPlayerWins != t2.FirstPlayerWins || t1.MostLopsidedRate != t2.MostLopsidedRate || t1.MedianThinkMs != t2.MedianThinkMs {
		t.Fatalf("RunCardTournament not deterministic: %+v vs %+v", t1, t2)
	}
	firstZero, firstOne := 0, 0
	for _, mr := range t1.Matches {
		if mr.FirstPlayer == 0 {
			firstZero++
		} else {
			firstOne++
		}
	}
	if firstZero != firstOne {
		t.Fatalf("first player not alternated evenly: p0=%d p1=%d", firstZero, firstOne)
	}
}

// trapCardState verifies that CardAI truly looks 1 turn ahead:
// Action 0 ("greedy_trap") gives +5 immediate score but lets the opponent deal lethal on their reply turn.
// Action 1 ("defend") gives +1 immediate score and survives the opponent's reply turn.
type trapCardState struct {
	active int
	turn   int
	hp0    int
	hp1    int
	shield int
}

func (t trapCardState) ActivePlayer() int { return t.active }
func (t trapCardState) Turn() int         { return t.turn }
func (t trapCardState) Winner() (int, bool) {
	if t.hp0 <= 0 {
		return 1, true
	}
	if t.hp1 <= 0 {
		return 0, true
	}
	return -1, false
}
func (t trapCardState) LegalActions() []playtest.CardAction {
	if t.active == 0 {
		return []playtest.CardAction{
			{ID: "greedy_trap", Action: "play", Index: 0},
			{ID: "defend", Action: "play", Index: 1},
		}
	}
	return []playtest.CardAction{
		{ID: "strike", Action: "play", Index: 0},
	}
}
func (t trapCardState) Apply(act playtest.CardAction) playtest.CardGameState {
	cp := t
	if t.active == 0 {
		if act.ID == "greedy_trap" {
			cp.hp1 -= 5
		} else {
			cp.shield += 5
		}
		cp.active = 1
		return cp
	}
	dmg := max(0, 6-cp.shield)
	cp.hp0 -= dmg
	cp.active = 0
	cp.turn++
	return cp
}
func (t trapCardState) Evaluate(player int) float64 {
	if player == 0 {
		return float64(t.hp0-t.hp1) + float64(t.shield)*0.2
	}
	return float64(t.hp1 - t.hp0)
}

func TestCardAIOneTurnLookaheadAvoidsLethalTrap(t *testing.T) {
	raw, err := playtest.NewCardAI(playtest.BotConfig{Profile: playtest.ProfileCardAI})
	if err != nil {
		t.Fatalf("NewCardAI: %v", err)
	}
	ai := raw.(*playtest.CardAI)
	state := trapCardState{active: 0, turn: 1, hp0: 5, hp1: 10}
	dec := ai.ChooseAction(state, random.NewStreams(1).Stream("test"))
	if dec.Action.ID != "defend" {
		t.Fatalf("1-turn lookahead chose %q, want \"defend\" to survive opponent's 6-damage reply", dec.Action.ID)
	}
	if dec.NodesEvaluated == 0 || dec.ThinkMs <= 0 {
		t.Fatalf("expected positive NodesEvaluated and ThinkMs, got %+v", dec)
	}
}

type nanEvalCardState struct {
	trapCardState
}

func (n nanEvalCardState) Apply(act playtest.CardAction) playtest.CardGameState {
	return nanEvalCardState{trapCardState: n.trapCardState.Apply(act).(trapCardState)}
}

func (n nanEvalCardState) Evaluate(int) float64 { return math.NaN() }

func TestBotProfilesEdgeCasesAndGuards(t *testing.T) {
	t.Run("nil_puzzle_level_does_not_panic", func(t *testing.T) {
		levels := fixturePuzzleLevels()
		pack := []playtest.PuzzleLevel{
			{ID: "level_nil", Name: "Broken", Par: 1, Start: nil},
			levels[0],
		}
		cfg := &playtest.Config{
			Version: playtest.CurrentVersion,
			Scope:   playtest.Scope{Scene: "levels"},
			Runs:    playtest.Runs{Count: 1, Rate: 60, MaxDuration: 10 * time.Second},
			Bot:     playtest.BotConfig{Profile: playtest.ProfilePuzzleSolver},
		}
		res, err := playtest.Run(cfg, playtest.NewPuzzlePackSim(pack, cfg.Bot), playtest.RunOptions{})
		if err != nil {
			t.Fatalf("Run with nil level returned unexpected error: %v", err)
		}
		if res.Report.Runs[0].Outcome != playtest.OutcomeFailed {
			t.Fatalf("outcome = %q, want %q", res.Report.Runs[0].Outcome, playtest.OutcomeFailed)
		}
		if got := res.Report.Runs[0].Counters["levels_solved"]; got != 1 {
			t.Fatalf("levels_solved = %v, want 1", got)
		}
	})

	t.Run("nil_card_state_and_invalid_decks_rejected", func(t *testing.T) {
		botCfg := playtest.BotConfig{
			Profile: playtest.ProfileCardAI,
			Params:  map[string]any{"matchesPerPair": 2},
		}
		nilFactory := func(_, _ string, _ int, _ uint64) (playtest.CardGameState, error) {
			return nil, nil
		}
		if _, err := playtest.RunCardTournament([]string{"Ember", "Frost"}, nilFactory, botCfg, 1); err == nil {
			t.Fatal("RunCardTournament accepted nil state from factory")
		}
		if _, err := playtest.RunCardTournament([]string{"Ember", "Ember"}, newFixtureCardMatch, botCfg, 1); err == nil {
			t.Fatal("RunCardTournament accepted duplicate deck names")
		}
		if _, err := playtest.RunCardTournament([]string{"", "Frost"}, newFixtureCardMatch, botCfg, 1); err == nil {
			t.Fatal("RunCardTournament accepted empty deck name")
		}

		cfg := &playtest.Config{
			Version: playtest.CurrentVersion,
			Scope:   playtest.Scope{Scene: "duels"},
			Runs:    playtest.Runs{Count: 1, Rate: 60, MaxDuration: 10 * time.Second},
			Bot:     botCfg,
		}
		if _, err := playtest.Run(cfg, playtest.NewCardTournamentSim([]string{"Ember", "Frost"}, nilFactory, botCfg), playtest.RunOptions{}); err == nil {
			t.Fatal("NewCardTournamentSim accepted nil state on initial match")
		}
		if _, err := playtest.Run(cfg, playtest.NewCardTournamentSim([]string{"Ember", "Ember"}, newFixtureCardMatch, botCfg), playtest.RunOptions{}); err == nil {
			t.Fatal("NewCardTournamentSim accepted duplicate deck names")
		}

		// Nil state on a later match fails the run cleanly instead of stalling to timeout.
		calls := 0
		secondNilFactory := func(a, b string, first int, seed uint64) (playtest.CardGameState, error) {
			calls++
			if calls == 2 {
				return nil, nil
			}
			return newFixtureCardMatch(a, b, first, seed)
		}
		res, err := playtest.Run(cfg, playtest.NewCardTournamentSim([]string{"Ember", "Frost"}, secondNilFactory, botCfg), playtest.RunOptions{})
		if err != nil {
			t.Fatalf("Run with second-match nil state returned error: %v", err)
		}
		if res.Report.Runs[0].Outcome != playtest.OutcomeFailed {
			t.Fatalf("second-match nil state outcome = %q, want %q", res.Report.Runs[0].Outcome, playtest.OutcomeFailed)
		}
	})

	t.Run("card_ai_nan_evaluate_score_is_finite", func(t *testing.T) {
		raw, err := playtest.NewCardAI(playtest.BotConfig{Profile: playtest.ProfileCardAI})
		if err != nil {
			t.Fatalf("NewCardAI: %v", err)
		}
		ai := raw.(*playtest.CardAI)
		st := nanEvalCardState{trapCardState: trapCardState{active: 0, turn: 1, hp0: 5, hp1: 10}}
		dec := ai.ChooseAction(st, random.NewStreams(1).Stream("nan"))
		if math.IsNaN(dec.Score) || math.IsInf(dec.Score, 0) {
			t.Fatalf("ChooseAction returned non-finite score %v", dec.Score)
		}
	})

	t.Run("topdown_explorer_counts_origin_exit_0_0", func(t *testing.T) {
		raw, err := playtest.NewTopDownExplorer(playtest.BotConfig{
			Profile: playtest.ProfileTopDownExplorer,
			Params:  map[string]any{"visionRadius": 5},
		})
		if err != nil {
			t.Fatalf("NewTopDownExplorer: %v", err)
		}
		bot := raw.(*playtest.TopDownExplorer)
		obs := playtest.ExplorerObservation{
			MapID:        "room",
			Width:        3,
			Height:       1,
			Player:       playtest.GridCell{X: 1, Y: 0},
			VisionRadius: 5,
			Exits: []playtest.GridCell{
				{X: 0, Y: 0},
				{X: 2, Y: 0},
			},
		}
		// Step 0: both exits have 0 uses and distance 1; explorer picks (0, 0) or (2, 0) and increments its count.
		// With rng == nil, explorerDirs order is up, right, down, left -> first picks right (2, 0), then picks left (0, 0),
		// then MUST switch back to right (2, 0) once (0, 0) use count increments!
		in0 := bot.Decide(0, time.Second/60, obs, nil)
		in1 := bot.Decide(1, time.Second/60, obs, nil)
		in2 := bot.Decide(2, time.Second/60, obs, nil)
		if !in0.Actions["right"] {
			t.Fatalf("step 0 expected right toward (2,0), got %+v", in0.Actions)
		}
		if !in1.Actions["left"] {
			t.Fatalf("step 1 expected left toward (0,0), got %+v", in1.Actions)
		}
		if !in2.Actions["right"] {
			t.Fatalf("step 2 expected right after (0,0) use count incremented, got %+v", in2.Actions)
		}
	})

	t.Run("shooter_nan_observation_produces_finite_axes", func(t *testing.T) {
		raw, err := playtest.NewShooterBot(playtest.BotConfig{
			Profile:     playtest.ProfileShooter,
			ReactionMin: 0,
			ReactionMax: 0,
		})
		if err != nil {
			t.Fatalf("NewShooterBot: %v", err)
		}
		in := raw.Decide(0, time.Second/60, playtest.ShooterObservation{
			PlayfieldWidth:  math.NaN(),
			PlayfieldHeight: math.Inf(1),
			ShipX:           math.NaN(),
			ShipY:           math.Inf(-1),
			ShipRadius:      math.NaN(),
			ShipAlive:       true,
			Bullets:         []playtest.ShooterBullet{{X: math.NaN(), Y: 10, VX: 0, VY: 100, Radius: 4}},
			PowerUps:        []playtest.ShooterPowerUp{{X: math.NaN(), Y: 10}},
		}, random.NewStreams(1).Stream("nan"))
		if math.IsNaN(in.Axes["moveX"]) || math.IsInf(in.Axes["moveX"], 0) ||
			math.IsNaN(in.Axes["moveY"]) || math.IsInf(in.Axes["moveY"], 0) {
			t.Fatalf("ShooterBot produced non-finite axes: %+v", in.Axes)
		}
	})

	t.Run("extreme_numeric_params_rejected", func(t *testing.T) {
		cases := []playtest.BotConfig{
			{Profile: playtest.ProfileShooter, Params: map[string]any{"lookaheadMs": 1e18}},
			{Profile: playtest.ProfileTopDownExplorer, Params: map[string]any{"visionRadius": 1e9}},
			{Profile: playtest.ProfilePuzzleSolver, Params: map[string]any{"stateLimit": 1e12}},
			{Profile: playtest.ProfilePuzzleSolver, Params: map[string]any{"undoAfterBadMoves": 1e9}},
			{Profile: playtest.ProfilePuzzleSolver, Params: map[string]any{"maxHumanMoves": 1e18}},
			{Profile: playtest.ProfileCardAI, Params: map[string]any{"matchesPerPair": 1e9}},
			{Profile: playtest.ProfileCardAI, Params: map[string]any{"maxTurns": 1e9}},
			{Profile: playtest.ProfileCardAI, Params: map[string]any{"maxActionsPerTurn": 1e9}},
		}
		for _, bc := range cases {
			if _, err := playtest.NewBot(bc); err == nil {
				t.Errorf("NewBot(%+v) succeeded, want error for extreme param", bc)
			}
		}
	})
}
