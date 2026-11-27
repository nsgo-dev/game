package main

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/placeholder"
	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// The actions game.yaml binds to keys and controller buttons; the code binds each lane's column of the playfield to touches and the mouse too.
const (
	actionUp      = "up"
	actionDown    = "down"
	actionConfirm = "confirm"
	actionBack    = "back"
	actionPause   = "pause"
)

// laneActions are the actions that hit each lane, left to right.
var laneActions = [Lanes]string{"lane1", "lane2", "lane3", "lane4"}

const (
	// stepRate is the game loop's fixed rate in steps per second. Hits are judged by the time their input went down, not by the step that reads them, so the rate only bounds a controller's timing, which is read once a step.
	stepRate = 120
	// leadIn is how long a chart waits before its music starts, so the first notes fall in from the top.
	leadIn = 2 * time.Second
	// approach is how long a note takes to fall from the top of the playfield to the hit line.
	approach = 1400 * time.Millisecond
	// hitFlash is how long a lane glows after it is hit.
	hitFlash = 200 * time.Millisecond
	// hitVolume is how loud a hit's click plays, under the music.
	hitVolume = 0.35
	// resultGuard is how long the result ignores confirm and back in a step a lane is hit in, after it shows. On a controller those are South and East, which are lane buttons too, so a player still tapping the lanes as a chart fails does not retry or leave before reading the result; Enter and Escape, which are no lanes, act at once.
	resultGuard = 750 * time.Millisecond
)

// screen is which of the game's screens shows.
type screen uint8

const (
	screenMenu screen = iota
	screenPlay
	screenResult
	screenCalibrate
)

// menuItem is one line of the menu: a chart to play, or a calibration to run.
type menuItem struct {
	// song and chart index the chart in Songs, when calibrate is false.
	song, chart int
	// calibrate says the item runs a calibration of kind.
	calibrate bool
	kind      rhythm.CalibrationKind
}

// The pause menu's and the result's items, in the order up and down move through them.
const (
	pauseResume = iota
	pauseRestart
	pauseQuit
	pauseItems
)

const (
	resultRetry = iota
	resultMenu
	resultItems
)

// hudState is what the native views show about the chart being played.
type hudState struct {
	Score, Combo, Energy int
	Status               Status
	// Waiting is true before the music starts.
	Waiting bool
	// Last is the latest judgement, Early or late by Offset; Judged counts judgements, so the same grade twice still shows anew.
	Last   Grade
	Offset time.Duration
	Judged int
}

// laneHit is the last time a lane was hit and what it hit, for the glow drawn under it.
type laneHit struct {
	at    time.Time
	grade Grade
	// hit says a note was judged, not an empty tap.
	hit bool
}

// rhythmGame is the whole game: the song library, the input, the music and its clock, the chart being played and the player's records and calibration.
type rhythmGame struct {
	cx      *core.Scope
	lib     *Library
	actions *input.Actions
	mixer   *sound.Mixer
	// click is the calibration's metronome, and hitFX plays it on each hit; both are nil until loaded or where the platform plays no audio.
	click *audio.Sound
	hitFX *sound.Effect

	offsetStore *rhythm.OffsetStore
	device      string
	offsets     rhythm.Offsets
	recordStore *recordStore
	records     Records
	items       []menuItem
	// picked is the menu item picked last, which the menu highlights as it shows again.
	picked int

	// The chart being played: its song and track, its run and the music's clock and player.
	song, chart  int
	track        Track
	run          *Run
	clock        *rhythm.Clock
	music        *audio.Player
	musicStarted bool
	// silent says the music cannot play here, so the clock runs on the engine's time alone.
	silent bool

	// laneInputs are the inputs game.yaml binds to each lane, which the playfield's regions are added to.
	laneInputs [Lanes][]input.Input
	// stamps are when an input last went down on each lane since the last step, by the engine's clock; the zero time for none, or for a controller, which the step reads.
	stamps [Lanes]time.Time
	// keysHeld and codesHeld are the keys held on the window as last seen, so a key going down is found.
	keysHeld, codesHeld []string
	// field is the playfield's size, which its regions are laid out in.
	field core.Size
	hits  [Lanes]laneHit

	calScreen *rhythm.CalibrationScreen
	// resultAt is when the result showed, which resultGuard counts from.
	resultAt time.Time

	screen     *core.Signal[screen]
	cursor     *core.Signal[int]
	paused     *core.Signal[bool]
	hud        *core.Signal[hudState]
	result     *core.Signal[resultState]
	frame      *core.Signal[uint64]
	saved      *core.Signal[int]
	saveFail   *core.Signal[bool]
	clickReady *core.Signal[bool]
}

// resultState is what the result screen shows of the chart just played.
type resultState struct {
	Status   Status
	Score    int
	MaxCombo int
	Counts   [len(grades)]int
	NewBest  bool
}

// Root is the game's root NSGo component: the menu, a chart being played on a full-screen Canvas driven by a fixed-step game.Loop with native views above it, the result, or a calibration.
func Root(cx *core.Scope) core.Node {
	g := newRhythmGame(cx)
	return g.view()
}

func newRhythmGame(cx *core.Scope) *rhythmGame {
	desktop.SetTitle(cx, Project.Title())
	g := &rhythmGame{
		cx:          cx,
		lib:         Songs,
		actions:     input.NewActions(),
		mixer:       sound.NewMixer(cx, "pulselanes.volume"),
		offsetStore: rhythm.NewOffsetStore(cx, calibrationKey),
		device:      rhythm.DeviceKey(cx),
		recordStore: newRecordStore(cx.Storage()),
		screen:      core.NewSignal(cx, screenMenu),
		cursor:      core.NewSignal(cx, 0),
		paused:      core.NewSignal(cx, false),
		hud:         core.NewSignal(cx, hudState{}),
		result:      core.NewSignal(cx, resultState{}),
		frame:       core.NewSignal(cx, uint64(0)),
		saved:       core.NewSignal(cx, 0),
		saveFail:    core.NewSignal(cx, false),
		clickReady:  core.NewSignal(cx, false),
	}
	g.offsets, _ = g.offsetStore.Get(g.device)
	var err error
	g.records, err = g.recordStore.Load()
	g.saveFail.Set(err != nil)
	for si, s := range g.lib.Songs {
		for ci := range s.Charts {
			g.items = append(g.items, menuItem{song: si, chart: ci})
		}
	}
	g.items = append(g.items, menuItem{calibrate: true, kind: rhythm.CalibrateAudio}, menuItem{calibrate: true, kind: rhythm.CalibrateVideo})

	Project.BindInput(g.actions)
	for l, name := range laneActions {
		g.laneInputs[l] = slices.Clone(g.actions.Bindings(name))
	}
	g.actions.WatchKeys(cx)
	g.actions.WatchGamepads(cx)
	cx.Effect(func() {
		keys, codes := core.KeysDown(cx), core.KeyCodesDown(cx)
		cx.Untrack(func() { g.keysChanged(keys, codes) })
	})
	// A chart pauses as the window loses the focus or goes to the background, so no note falls while the player is away.
	cx.Effect(func() {
		if cx.Env().Lifecycle() != core.LifecycleActive {
			cx.Untrack(g.pause)
		}
	})

	if src, err := placeholder.Sound("click"); err == nil {
		audio.LoadSound(cx, src, func(s *audio.Sound, err error) {
			if err != nil {
				return
			}
			g.click = s
			g.hitFX = g.mixer.Effect(s, "sfx", 8)
			g.clickReady.Set(true)
		})
	}

	loop := &game.Loop{
		Rate:   stepRate,
		Update: g.update,
		Draw:   func(float64) { g.frame.Update(func(n uint64) uint64 { return n + 1 }) },
	}
	loop.Run(cx)
	return g
}

// keysChanged stamps each lane a key bound to it went down on, by the engine's clock as the key went down, so a hit is judged by when it was pressed rather than by the step that reads it.
func (g *rhythmGame) keysChanged(keys, codes []string) {
	now := audio.Now(g.cx)
	for l := range Lanes {
		for _, in := range g.laneInputs[l] {
			if in.Kind == input.InputKey && slices.Contains(keys, in.Key) && !slices.Contains(g.keysHeld, in.Key) ||
				in.Kind == input.InputKeyCode && slices.Contains(codes, in.Code) && !slices.Contains(g.codesHeld, in.Code) {
				g.stamp(l, now)
			}
		}
	}
	g.keysHeld, g.codesHeld = slices.Clone(keys), slices.Clone(codes)
}

// stamp records that an input went down on lane at at, keeping the first since the last step.
func (g *rhythmGame) stamp(lane int, at time.Time) {
	if lane >= 0 && lane < Lanes && g.stamps[lane].IsZero() {
		g.stamps[lane] = at
	}
}

// pressTime returns when lane's input went down: its stamp, or now for a controller, which has none.
func (g *rhythmGame) pressTime(lane int, now time.Time) time.Time {
	if t := g.stamps[lane]; !t.IsZero() {
		return t
	}
	return now
}

// update is one fixed step: read the input and act on it for the screen showing.
func (g *rhythmGame) update(time.Duration) {
	g.actions.Step()
	now := audio.Now(g.cx)
	switch g.screen.Get() {
	case screenMenu:
		g.menuInput()
	case screenPlay:
		g.playStep(now)
	case screenResult:
		g.resultInput(now)
	case screenCalibrate:
		g.calibrateInput(now)
	}
	g.stamps = [Lanes]time.Time{}
}

// moveCursor moves the highlight up or down a list of n items, as the step's input asks.
func (g *rhythmGame) moveCursor(n int) {
	cur := g.cursor.Get()
	switch {
	case g.actions.JustPressed(actionUp):
		cur--
	case g.actions.JustPressed(actionDown):
		cur++
	}
	g.cursor.Set(min(max(cur, 0), n-1))
}

// menuInput moves the menu's highlight and picks the highlighted item.
func (g *rhythmGame) menuInput() {
	g.moveCursor(len(g.items))
	if g.actions.JustPressed(actionConfirm) {
		g.pick(g.cursor.Get())
	}
}

// pick plays the chart or runs the calibration of the menu's item i.
func (g *rhythmGame) pick(i int) {
	if i < 0 || i >= len(g.items) {
		return
	}
	it := g.items[i]
	g.picked = i
	g.cursor.Set(i)
	if it.calibrate {
		g.startCalibration(it.kind)
		return
	}
	g.startChart(it.song, it.chart)
}

// canCalibrate reports whether a calibration of kind can run here: an audio one needs the click it plays.
func (g *rhythmGame) canCalibrate(kind rhythm.CalibrationKind) bool {
	return kind == rhythm.CalibrateVideo || g.clickReady.Get()
}

// startChart plays chart ci of song si from its start: the clock runs from leadIn before the music, which starts once the clock reaches it.
func (g *rhythmGame) startChart(si, ci int) {
	g.closeMusic()
	song := &g.lib.Songs[si]
	g.song, g.chart = si, ci
	g.track = g.lib.Tracks[si]
	g.run = NewRun(g.lib.SongFile, song, g.track, &song.Charts[ci])
	g.clock = rhythm.NewClock(g.track.Timing)
	g.clock.SetOffsets(g.offsets)
	g.clock.Start(audio.Now(g.cx), -leadIn)
	g.musicStarted, g.silent = false, false
	var p *audio.Player
	p = audio.NewPlayer(g.cx, g.track.Music, audio.PlayerOptions{
		ProgressInterval: 100 * time.Millisecond,
		OnProgress:       func(pos, length time.Duration) { g.musicProgress(p, pos, length) },
		OnError:          func(error) { g.musicFailed(p) },
	})
	p.SetLoop(song.Loop)
	p.Preload()
	g.music = p
	g.hits = [Lanes]laneHit{}
	g.paused.Set(false)
	g.actions.Reset()
	g.hud.Set(hudState{})
	g.syncHUD(audio.Now(g.cx))
	g.screen.Set(screenPlay)
}

// closeMusic stops and releases the music of the chart played last.
func (g *rhythmGame) closeMusic() {
	if g.music != nil {
		g.music.Close()
		g.music = nil
	}
}

// startMusic plays the music from where the clock is, keeping the clock in step with it once it plays.
func (g *rhythmGame) startMusic() {
	g.musicStarted = true
	if g.silent {
		return
	}
	p := g.music
	p.Play(func(err error) {
		if p != g.music {
			return
		}
		if err != nil {
			g.silent = true
			return
		}
		g.syncClock(p.Position(), p.Duration())
	})
}

// musicProgress keeps the clock in step with the music p reports playing at pos, of a track length long.
func (g *rhythmGame) musicProgress(p *audio.Player, pos, length time.Duration) {
	if p != g.music || g.paused.Get() || g.screen.Get() != screenPlay {
		return
	}
	g.syncClock(pos, length)
}

// musicFailed has the chart go on without music when p fails, such as where the platform plays no audio.
func (g *rhythmGame) musicFailed(p *audio.Player) {
	if p == g.music {
		g.silent = true
	}
}

// syncClock moves the clock to the music's position pos when they drift apart, counting the times a looping track has started over.
func (g *rhythmGame) syncClock(pos, length time.Duration) {
	now := audio.Now(g.cx)
	if g.lib.Songs[g.song].Loop {
		pos = unwrapLoop(pos, length, g.clock.Track(now))
	}
	g.clock.Sync(pos, now)
}

// unwrapLoop returns the song position a looping track of length reports as pos: pos plus as many lengths as bring it nearest to expected, the position the clock holds. rhythm.Clock.Sync takes the song's position, which keeps growing as the track starts over.
func unwrapLoop(pos, length, expected time.Duration) time.Duration {
	if length <= 0 {
		return pos
	}
	n := math.Round(float64(expected-pos) / float64(length))
	return pos + time.Duration(max(n, 0))*length
}

// playStep is one step of a chart: start the music on time, judge the lanes hit, miss the notes gone past and end the chart when it is over.
func (g *rhythmGame) playStep(now time.Time) {
	a := g.actions
	if g.paused.Get() {
		g.pauseInput()
		return
	}
	if a.JustPressed(actionPause) {
		g.pause()
		return
	}
	if !g.musicStarted && g.clock.Track(now) >= 0 {
		g.startMusic()
	}
	for l, name := range laneActions {
		if a.JustPressed(name) {
			g.hit(l, g.pressTime(l, now), now)
		}
	}
	if missed := g.run.Advance(g.clock.Position(now)); len(missed) > 0 {
		g.judged(missed[len(missed)-1])
	}
	if g.run.Status != Playing {
		g.finish()
		return
	}
	g.syncHUD(now)
}

// hit judges a hit on lane that went down at at, read at now.
func (g *rhythmGame) hit(lane int, at, now time.Time) {
	j, ok := g.run.Hit(lane, g.clock.Position(at))
	g.hits[lane] = laneHit{at: now, grade: j.Grade, hit: ok}
	if !ok {
		return
	}
	if g.hitFX != nil {
		g.hitFX.Play(audio.Voice{Volume: hitVolume})
	}
	g.judged(j)
}

// judged shows j as the latest judgement.
func (g *rhythmGame) judged(j Judged) {
	g.hud.Update(func(h hudState) hudState {
		h.Last, h.Offset, h.Judged = j.Grade, j.Offset, h.Judged+1
		return h
	})
}

// syncHUD updates what the native views show from the run.
func (g *rhythmGame) syncHUD(now time.Time) {
	r := g.run
	g.hud.Update(func(h hudState) hudState {
		h.Score, h.Combo, h.Energy, h.Status = r.Score(), r.Combo, r.Energy, r.Status
		h.Waiting = !g.musicStarted && g.clock.Track(now) < 0
		return h
	})
}

// finish ends the chart: the music stops, a clear's score is kept when it is the best, and the result shows.
func (g *rhythmGame) finish() {
	g.closeMusic()
	r := g.run
	res := resultState{Status: r.Status, Score: r.Score(), MaxCombo: r.MaxCombo, Counts: r.Counts}
	if r.Status == Cleared {
		song := &g.lib.Songs[g.song]
		if g.records.Record(chartKey(song, &song.Charts[g.chart]), res.Score, res.MaxCombo) {
			res.NewBest = true
			g.saveFail.Set(g.recordStore.Save(g.records) != nil)
			g.saved.Update(func(n int) int { return n + 1 })
		}
	}
	g.result.Set(res)
	g.resultAt = audio.Now(g.cx)
	g.cursor.Set(resultRetry)
	g.actions.Reset()
	g.screen.Set(screenResult)
}

// pause stops a chart being played, its clock and its music, and shows the pause menu.
func (g *rhythmGame) pause() {
	if g.screen.Get() != screenPlay || g.paused.Get() {
		return
	}
	now := audio.Now(g.cx)
	g.clock.Pause(now)
	if g.musicStarted && !g.silent {
		g.music.Pause()
	}
	g.paused.Set(true)
	g.cursor.Set(pauseResume)
	g.actions.Reset()
}

// resume plays the paused chart on from where it paused.
func (g *rhythmGame) resume() {
	if !g.paused.Get() {
		return
	}
	g.clock.Resume(audio.Now(g.cx))
	g.paused.Set(false)
	g.actions.Reset()
	if g.musicStarted {
		g.startMusic()
	}
}

// pauseInput acts on the pause menu: the pause action resumes, and confirm picks the highlighted item.
func (g *rhythmGame) pauseInput() {
	if g.actions.JustPressed(actionPause) {
		g.resume()
		return
	}
	g.moveCursor(pauseItems)
	if g.actions.JustPressed(actionConfirm) {
		g.pausePick(g.cursor.Get())
	}
}

// pausePick resumes, restarts or quits the paused chart.
func (g *rhythmGame) pausePick(i int) {
	switch i {
	case pauseResume:
		g.resume()
	case pauseRestart:
		g.startChart(g.song, g.chart)
	case pauseQuit:
		g.showMenu()
	}
}

// resultInput acts on the result at now: confirm retries or goes back as highlighted, and back goes to the menu, except in a step a lane is hit in before resultGuard has passed since the result showed.
func (g *rhythmGame) resultInput(now time.Time) {
	if now.Sub(g.resultAt) < resultGuard && slices.ContainsFunc(laneActions[:], g.actions.JustPressed) {
		return
	}
	if g.actions.JustPressed(actionBack) {
		g.showMenu()
		return
	}
	g.moveCursor(resultItems)
	if g.actions.JustPressed(actionConfirm) {
		g.resultPick(g.cursor.Get())
	}
}

// resultPick plays the chart again or goes back to the menu.
func (g *rhythmGame) resultPick(i int) {
	if i == resultRetry {
		g.startChart(g.song, g.chart)
		return
	}
	g.showMenu()
}

// showMenu ends whatever plays and shows the menu with the item played last highlighted.
func (g *rhythmGame) showMenu() {
	g.closeMusic()
	if g.calScreen != nil {
		g.calScreen.Stop()
		g.calScreen = nil
	}
	g.paused.Set(false)
	g.actions.Reset()
	g.cursor.Set(g.picked)
	g.screen.Set(screenMenu)
}

// startCalibration runs a calibration of kind on its screen; saving its result keeps the device's offsets and goes back to the menu.
func (g *rhythmGame) startCalibration(kind rhythm.CalibrationKind) {
	if !g.canCalibrate(kind) {
		return
	}
	s := rhythm.NewCalibrationScreen(rhythm.NewCalibration(g.cx, kind, g.click), g.offsetStore, g.device)
	s.OnSave = func(o rhythm.Offsets) {
		g.offsets = o
		g.showMenu()
	}
	g.calScreen = s
	g.actions.Reset()
	g.screen.Set(screenCalibrate)
}

// calibrateInput taps the calibration with the lanes, by when each went down, as a chart judges them; confirm keeps a steady result or starts an unsteady one over, and back leaves.
//
// On a controller South and East are lane buttons as well as confirm and back, so a step a lane is hit in is a tap and nothing else: back never acts in it, and confirm acts only once the calibration was done before the step. A controller leaves a calibration with its Back (Select) button.
func (g *rhythmGame) calibrateInput(now time.Time) {
	s := g.calScreen
	cal := s.Calibration()
	done := cal.Calibrator.Done()
	tapped := false
	for l, name := range laneActions {
		if g.actions.JustPressed(name) {
			tapped = true
			if !done {
				cal.Calibrator.Tap(g.pressTime(l, now))
			}
		}
	}
	if !tapped && g.actions.JustPressed(actionBack) {
		g.showMenu()
		return
	}
	if tapped && !done {
		return
	}
	if g.actions.JustPressed(actionConfirm) {
		if r, done := cal.Result(); done && r.Steady() {
			_ = s.Save()
		} else if done {
			s.Retry()
		}
	}
}

// layoutField lays the playfield out at size and binds each lane's column to touches and the left mouse button, beside the inputs game.yaml binds. It runs as the playfield's view is laid out.
//
// Binding an action drops the press it latched since the last step, so a lane stamped since then, by a key, a touch or a click that went down before the playfield changed size, is triggered again and its hit is not lost.
func (g *rhythmGame) layoutField(size core.Size) {
	if size == g.field {
		return
	}
	g.field = size
	l := layoutFor(size)
	for lane, name := range laneActions {
		b := l.laneBounds(lane)
		g.actions.Bind(name, append(slices.Clone(g.laneInputs[lane]), input.TouchRegion(b), input.MouseRegion(b, core.MouseButtonLeft))...)
		if !g.stamps[lane].IsZero() {
			g.actions.Trigger(name)
		}
	}
}

// touched stamps the lanes the fingers going down in ev went down on.
func (g *rhythmGame) touched(ev core.TouchEvent) {
	if ev.Phase != core.TouchStart {
		return
	}
	l, now := layoutFor(g.field), audio.Now(g.cx)
	for _, t := range ev.Touches {
		if t.Changed {
			g.stamp(l.laneAt(t.Position.X), now)
		}
	}
}

// pressedDown stamps the lane a mouse press went down on.
func (g *rhythmGame) pressedDown(ev core.PressEvent) {
	if ev.Keyboard {
		return
	}
	g.stamp(layoutFor(g.field).laneAt(ev.Position.X), audio.Now(g.cx))
}
