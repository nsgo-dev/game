package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/online/achievements"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tween"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/gameservices"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	// Board layout inside virtual coordinates (leaves top space for HUD and bottom space for status).
	BoardOriginX float32 = 30
	BoardOriginY float32 = 148
	CellSize     float32 = 60
	BoardPixelSz float32 = CellSize * BoardSize

	defaultSeed  uint64 = 20260303
	saveSlotName        = "stats"
	atlasFramePx        = 64
	gemDrawScale        = float32(CellSize-8) / float32(atlasFramePx)

	swapDuration   = 135 * time.Millisecond
	bounceHalfDur  = 105 * time.Millisecond
	popDuration    = 180 * time.Millisecond
	fallBaseDur    = 130 * time.Millisecond
	fallPerRowDur  = 32 * time.Millisecond
	popupDuration  = 650 * time.Millisecond
	swipeThreshold = CellSize * 0.42
)

type animPhase uint8

const (
	phaseIdle animPhase = iota
	phaseSwapping
	phaseBouncing
	phasePopping
	phaseFalling
)

type tileVis struct {
	kind  GemKind
	x, y  float32
	scale float32
	alpha float32
	rot   float32
	z     int
}

type scorePopup struct {
	text  string
	x, y  float32
	alpha float32
	color core.Color
}

type savedStats struct {
	BestScore int `json:"best_score"`
}

type slideGemGame struct {
	cx       *core.Scope
	seed     uint64
	board    *Board
	viewport screen.Viewport
	actions  *input.Actions
	atlas    *sprite.Atlas
	batch    sprite.Batch
	tweens   tween.Group
	bounceTL *tween.Timeline
	sparkles particles.Emitter
	store    *save.Store
	trophies *achievements.Tracker
	mixer    *sound.Mixer
	chimeSFX *sound.Effect
	bounceFX *sound.Effect
	tapSFX   *sound.Effect

	phase      animPhase
	turnActive bool
	bestScore  int
	pulseSec   float64
	bounces    int
	swaps      int
	hintCount  int
	// unlockNote is the latest achievement unlocked this turn, kept as the status line once the turn settles.
	unlockNote string

	viewBounds core.Size

	// Visual tile grid and transient effects.
	gridVis [BoardSize][BoardSize]*tileVis
	popping []*tileVis
	popups  []*scorePopup

	// Touch/pointer selection & swipe state.
	hasSelected bool
	selected    Cell
	dragging    bool
	dragCell    Cell
	dragOrigin  core.Point

	// Active hint highlight.
	hasHint  bool
	hintMove Move

	// Reactive signals for HUD & Canvas redraw.
	scoreSig    *core.Signal[int]
	bestSig     *core.Signal[int]
	movesSig    *core.Signal[int]
	comboSig    *core.Signal[int]
	gameOverSig *core.Signal[bool]
	statusSig   *core.Signal[string]
	frameTick   *core.Signal[uint64]
	signedIn    *core.Signal[bool]
}

// Root constructs the SlideGem game root node with the default deterministic seed.
func Root(cx *core.Scope) core.Node {
	desktop.SetTitle(cx, gameProject.Title())
	_, node := newSlideGem(cx, defaultSeed)
	return node
}

func newSlideGem(cx *core.Scope, seed uint64) (*slideGemGame, core.Node) {
	atlas := buildGemAtlas()
	store := save.NewStorageStore(cx.Storage(), "slidegem", save.WithVersion(1))
	var stats savedStats
	_ = store.Load(saveSlotName, &stats)

	g := &slideGemGame{
		cx:        cx,
		seed:      seed,
		board:     NewBoard(seed),
		viewport:  gameViewport,
		actions:   input.NewActions(),
		atlas:     atlas,
		batch:     sprite.Batch{Atlas: atlas},
		store:     store,
		trophies:  newAchievementTracker(cx),
		mixer:     sound.NewMixer(cx, "slidegem.sound"),
		bestScore: stats.BestScore,
		sparkles: particles.Emitter{
			Atlas: atlas,
			Frames: []sprite.Frame{
				atlas.Frames["sparkle_star"],
				atlas.Frames["sparkle_dot"],
			},
			Shape:            particles.Circle(14),
			MaxParticles:     180,
			LifetimeMin:      260 * time.Millisecond,
			LifetimeMax:      480 * time.Millisecond,
			SpeedMin:         55,
			SpeedMax:         155,
			AngleMin:         0,
			AngleMax:         float32(2 * math.Pi),
			GravityY:         160,
			RotationSpeedMin: -4.5,
			RotationSpeedMax: 4.5,
			SizeCurve:        particles.FloatCurve{{T: 0, Value: 0.55}, {T: 0.25, Value: 0.72}, {T: 1, Value: 0.05}},
			AlphaCurve:       particles.FloatCurve{{T: 0, Value: 1}, {T: 0.65, Value: 0.85}, {T: 1, Value: 0}},
			Seed:             seed ^ 0x51A9E123,
			Z:                20,
		},
		scoreSig:    core.NewSignal(cx, 0),
		bestSig:     core.NewSignal(cx, stats.BestScore),
		movesSig:    core.NewSignal(cx, InitialMoves),
		comboSig:    core.NewSignal(cx, 0),
		gameOverSig: core.NewSignal(cx, false),
		statusSig:   core.NewSignal(cx, "Tap or swipe adjacent gems"),
		frameTick:   core.NewSignal(cx, uint64(0)),
		signedIn:    core.NewSignal(cx, false),
	}

	g.trophies.OnUnlock(func(d achievements.Definition) {
		g.unlockNote = "Achievement unlocked: " + d.Title
		g.statusSig.Set(g.unlockNote)
	})
	// Sign in to Game Center or Play Games where the platform has one; progress made before or without it is kept and reported once signed in.
	achievements.SignIn(cx, g.trophies, func(_ gameservices.Player, err error) {
		g.signedIn.Set(err == nil)
	})

	g.loadAudio()
	g.bindCellActions()
	g.syncGridVisImmediate()

	// Fixed-step 60 Hz game loop stepping input, tweens, timelines, and particles.
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			g.update(dt)
		},
		Draw: func(_ float64) {
			g.frameTick.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)

	return g, g.buildTree()
}

func (g *slideGemGame) loadAudio() {
	gameProject.ConfigureMixer(g.mixer)
	audio.LoadSound(g.cx, audio.Source{Data: buildChimeWAV()}, func(s *audio.Sound, err error) {
		if err == nil && s != nil {
			g.chimeSFX = g.mixer.Effect(s, "sfx", 6)
		}
	})
	audio.LoadSound(g.cx, audio.Source{Data: buildBounceWAV()}, func(s *audio.Sound, err error) {
		if err == nil && s != nil {
			g.bounceFX = g.mixer.Effect(s, "sfx", 3)
		}
	})
	audio.LoadSound(g.cx, audio.Source{Data: buildSelectWAV()}, func(s *audio.Sound, err error) {
		if err == nil && s != nil {
			g.tapSFX = g.mixer.Effect(s, "sfx", 4)
		}
	})
}

func (g *slideGemGame) bindCellActions() {
	g.actions.MapPoint = func(p core.Point) (core.Point, bool) {
		return g.viewport.ViewToGame(g.viewSize(), p)
	}
	for r := range BoardSize {
		for c := range BoardSize {
			rect := cellBounds(Cell{Row: r, Col: c})
			g.actions.Bind(
				cellActionName(r, c),
				input.TouchRegion(rect),
				input.MouseRegion(rect, core.MouseButtonLeft),
			)
		}
	}
}

func (g *slideGemGame) viewSize() core.Size {
	if g.viewBounds.Width > 0 && g.viewBounds.Height > 0 {
		return g.viewBounds
	}
	return g.cx.Env().Window()
}

func (g *slideGemGame) buildTree() core.Node {
	boardSurface := core.View{
		TestID: "game-surface",
		Style: core.Style{
			Position: core.Absolute,
			Top:      core.Pt(0),
			Left:     core.Pt(0),
			Width:    core.Pct(100),
			Height:   core.Pct(100),
		},
		OnLayout: func(b core.Bounds) {
			if b.Size.Width > 0 && b.Size.Height > 0 {
				g.viewBounds = b.Size
			}
		},
		OnTouch:     g.onSwipeTouch,
		OnPressDown: g.onSwipePressDown,
		OnMouseMove: g.onSwipeMouseMove,
		OnPressUp:   g.onSwipePressUp,
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "SlideGem 6 by 6 puzzle board",
				Style: core.Style{
					Width:  core.Pct(100),
					Height: core.Pct(100),
				},
				Draw: g.draw,
			},
		},
	}
	g.actions.Attach(&boardSurface)

	hudOverlay := core.View{
		TestID:        "hud-overlay",
		PointerEvents: core.PointerEventsBoxNone,
		StyleFunc: func() core.Style {
			safe := screen.SafeRectForEnv(g.cx.Env())
			return core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(safe.Origin.X),
				Top:            core.Pt(safe.Origin.Y),
				Width:          core.Pt(safe.Size.Width),
				Height:         core.Pt(safe.Size.Height),
				Padding:        core.Edges{Top: 10, Right: 12, Bottom: 12, Left: 12},
				JustifyContent: core.JustifySpaceBetween,
			}
		},
		Children: []core.Node{
			// Top HUD bar: stats + Hint / New Game buttons.
			core.View{
				TestID:        "hud-top",
				PointerEvents: core.PointerEventsBoxNone,
				Style: core.Style{
					Direction:      core.Column,
					Gap:            8,
					AlignItems:     core.AlignStretch,
					JustifyContent: core.JustifyStart,
				},
				Children: []core.Node{
					core.View{
						PointerEvents: core.PointerEventsBoxNone,
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							AlignItems:     core.AlignCenter,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{
								Content: "SLIDEGEM",
								Style: core.Style{
									FontSize:   18,
									FontWeight: core.FontWeightBold,
									Color:      core.RGB(0xF8FAFC),
								},
							},
							core.View{
								PointerEvents: core.PointerEventsBoxNone,
								Style: core.Style{
									Direction:  core.Row,
									Gap:        8,
									AlignItems: core.AlignCenter,
								},
								Children: []core.Node{
									core.Show{
										When: g.signedIn.Get,
										Then: func(*core.Scope) core.Node {
											return ui.Button{
												Text:    "Trophies",
												Variant: ui.ButtonSecondary,
												Size:    ui.SizeSmall,
												TestID:  "btn-trophies",
												OnPress: func() { gameservices.ShowAchievements(g.cx) },
											}
										},
									},
									ui.Button{
										Text:    "Hint",
										Variant: ui.ButtonSecondary,
										Size:    ui.SizeSmall,
										TestID:  "btn-hint",
										OnPress: g.requestHint,
									},
									ui.Button{
										Text:    "New Game",
										Variant: ui.ButtonPrimary,
										Size:    ui.SizeSmall,
										TestID:  "btn-new-game",
										OnPress: g.newGame,
									},
								},
							},
						},
					},
					core.View{
						PointerEvents: core.PointerEventsBoxNone,
						Style: core.Style{
							Direction:      core.Row,
							JustifyContent: core.JustifySpaceBetween,
							Gap:            8,
						},
						Children: []core.Node{
							statCard("SCORE", "hud-score", func() string { return fmt.Sprintf("%d", g.scoreSig.Get()) }, core.RGB(0x38BDF8)),
							statCard("BEST", "hud-best", func() string { return fmt.Sprintf("%d", g.bestSig.Get()) }, core.RGB(0xFBBF24)),
							statCard("MOVES", "hud-moves", func() string { return fmt.Sprintf("%d", g.movesSig.Get()) }, core.RGB(0x34D399)),
						},
					},
				},
			},
			// Bottom status / Game Over banner.
			core.Show{
				When: g.gameOverSig.Get,
				Then: func(*core.Scope) core.Node {
					return core.View{
						TestID: "game-over-card",
						Style: core.Style{
							Background:     core.RGBA(0x111827, 0.94),
							CornerRadius:   16,
							BorderWidth:    1.5,
							BorderColor:    core.RGB(0x38BDF8),
							Padding:        core.All(16),
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
							Gap:            8,
						},
						Children: []core.Node{
							core.Text{
								Content: "Out of Moves!",
								Style: core.Style{
									FontSize:   20,
									FontWeight: core.FontWeightBold,
									Color:      core.RGB(0xF8FAFC),
								},
							},
							core.Text{
								TestID: "game-over-summary",
								ContentFunc: func() string {
									return fmt.Sprintf("Final Score: %d  •  Best: %d", g.scoreSig.Get(), g.bestSig.Get())
								},
								Style: core.Style{
									FontSize: 13,
									Color:    core.RGB(0xCBD5E1),
								},
							},
							ui.Button{
								Text:    "Play Again",
								Variant: ui.ButtonPrimary,
								TestID:  "btn-play-again",
								OnPress: g.newGame,
							},
						},
					}
				},
				Else: func(*core.Scope) core.Node {
					return core.View{
						PointerEvents: core.PointerEventsNone,
						Style: core.Style{
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								TestID:      "hud-status",
								ContentFunc: g.statusSig.Get,
								Style: core.Style{
									FontSize:   13,
									FontWeight: core.FontWeightMedium,
									Color:      core.RGB(0x94A3B8),
								},
							},
						},
					}
				},
			},
		},
	}

	return core.View{
		TestID: "slidegem-root",
		Style: core.Style{
			Width:      core.Pct(100),
			Height:     core.Pct(100),
			Background: core.RGB(0x0B1020),
		},
		Children: []core.Node{
			boardSurface,
			hudOverlay,
		},
	}
}

func statCard(label, testID string, value func() string, accent core.Color) core.Node {
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style: core.Style{
			FlexGrow:       1,
			Background:     core.RGBA(0x172036, 0.90),
			CornerRadius:   10,
			BorderWidth:    1,
			BorderColor:    core.RGBA(0x334155, 0.85),
			Padding:        core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 10},
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
			Gap:            2,
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontSize:   10,
					FontWeight: core.FontWeightBold,
					Color:      core.RGB(0x94A3B8),
				},
			},
			core.Text{
				TestID:      testID,
				ContentFunc: value,
				Style: core.Style{
					FontSize:   18,
					FontWeight: core.FontWeightBold,
					Color:      accent,
				},
			},
		},
	}
}

// update steps input, tweens, bounce timeline, particles, and idle pulse on each 60 Hz fixed tick.
func (g *slideGemGame) update(dt time.Duration) {
	g.pulseSec += dt.Seconds()
	g.actions.Step()

	if g.phase == phaseIdle && !g.board.GameOver {
		g.pollCellActions()
	}

	g.tweens.Update(dt)
	if g.bounceTL != nil {
		g.bounceTL.Update(dt)
	}
	g.sparkles.Update(dt)
}

func (g *slideGemGame) pollCellActions() {
	anyPressed := false
	for r := range BoardSize {
		for c := range BoardSize {
			name := cellActionName(r, c)
			if g.actions.Pressed(name) {
				anyPressed = true
			}
			if g.actions.JustPressed(name) {
				cell := Cell{Row: r, Col: c}
				g.handleCellInteract(cell)
				if g.phase != phaseIdle {
					return
				}
			}
		}
	}
	if !anyPressed {
		g.dragging = false
	}
}

func (g *slideGemGame) handleCellInteract(cell Cell) {
	if g.phase != phaseIdle || g.board.GameOver || !InBounds(cell) {
		return
	}
	g.hasHint = false

	if !g.hasSelected {
		g.hasSelected = true
		g.selected = cell
		g.playTap()
		return
	}
	if g.selected == cell {
		g.hasSelected = false
		return
	}
	if Adjacent(g.selected, cell) {
		a, b := g.selected, cell
		g.hasSelected = false
		g.dragging = false
		g.attemptSwap(a, b)
		return
	}
	g.selected = cell
	g.playTap()
}

// Short-flick swipe support on touch and mouse drag (complements cell-crossing via input.Actions).
func (g *slideGemGame) onSwipeTouch(ev core.TouchEvent) {
	if g.phase != phaseIdle || g.board.GameOver {
		return
	}
	switch ev.Phase {
	case core.TouchStart:
		if len(ev.Touches) > 0 {
			g.beginPointerSwipe(ev.Touches[0].Position)
		}
	case core.TouchMove:
		if len(ev.Touches) > 0 {
			g.updatePointerSwipe(ev.Touches[0].Position)
		}
	case core.TouchEnd, core.TouchCancel:
		g.dragging = false
	}
}

func (g *slideGemGame) onSwipePressDown(ev core.PressEvent) {
	if ev.Keyboard || g.phase != phaseIdle || g.board.GameOver {
		return
	}
	g.beginPointerSwipe(ev.Position)
}

func (g *slideGemGame) onSwipeMouseMove(ev core.MouseEvent) {
	if ev.PressedButton != core.MouseButtonLeft || g.phase != phaseIdle || g.board.GameOver {
		return
	}
	g.updatePointerSwipe(ev.Position)
}

func (g *slideGemGame) onSwipePressUp(core.PressEvent) {
	g.dragging = false
}

func (g *slideGemGame) beginPointerSwipe(viewPt core.Point) {
	pt, ok := g.viewport.ViewToGame(g.viewSize(), viewPt)
	if !ok {
		return
	}
	cell, ok := cellAtPoint(pt)
	if !ok {
		return
	}
	g.dragging = true
	g.dragCell = cell
	g.dragOrigin = pt
}

func (g *slideGemGame) updatePointerSwipe(viewPt core.Point) {
	if !g.dragging {
		return
	}
	pt, ok := g.viewport.ViewToGame(g.viewSize(), viewPt)
	if !ok {
		return
	}
	dx := float32(pt.X - g.dragOrigin.X)
	dy := float32(pt.Y - g.dragOrigin.Y)
	adx := float32(math.Abs(float64(dx)))
	ady := float32(math.Abs(float64(dy)))
	if max(adx, ady) < swipeThreshold {
		return
	}
	target := g.dragCell
	if adx >= ady {
		if dx > 0 {
			target.Col++
		} else {
			target.Col--
		}
	} else {
		if dy > 0 {
			target.Row++
		} else {
			target.Row--
		}
	}
	g.dragging = false
	g.hasSelected = false
	if InBounds(target) {
		g.attemptSwap(g.dragCell, target)
	}
}

func (g *slideGemGame) attemptSwap(a, b Cell) {
	if g.phase != phaseIdle || g.board.GameOver || !Adjacent(a, b) {
		return
	}
	va := g.gridVis[a.Row][a.Col]
	vb := g.gridVis[b.Row][b.Col]
	if va == nil || vb == nil {
		return
	}
	g.hasHint = false
	pa := cellCenter(a)
	pb := cellCenter(b)

	if !g.board.WouldMatch(a, b) {
		g.startBounceBack(va, vb, pa, pb)
		return
	}

	g.swaps++
	g.phase = phaseSwapping
	g.turnActive = true
	va.z = 5
	vb.z = 4

	twA := tween.Point(pa, pb, swapDuration, core.EaseOut, func(p core.Point) {
		va.x, va.y = float32(p.X), float32(p.Y)
	})
	twB := tween.Point(pb, pa, swapDuration, core.EaseOut, func(p core.Point) {
		vb.x, vb.y = float32(p.X), float32(p.Y)
	}).OnComplete(func() {
		va.z, vb.z = 1, 1
		g.board.Swap(a, b)
		g.gridVis[a.Row][a.Col], g.gridVis[b.Row][b.Col] = vb, va
		g.resolveMatches()
	})
	g.tweens.Add(twA, twB)
}

func (g *slideGemGame) startBounceBack(va, vb *tileVis, pa, pb core.Point) {
	g.bounces++
	g.phase = phaseBouncing
	g.statusSig.Set("No match — try another swap")
	if g.bounceFX != nil {
		g.bounceFX.Play(audio.Voice{Volume: 0.65})
	}

	midA := core.Point{
		X: pa.X + (pb.X-pa.X)*0.62,
		Y: pa.Y + (pb.Y-pa.Y)*0.62,
	}
	midB := core.Point{
		X: pb.X + (pa.X-pb.X)*0.62,
		Y: pb.Y + (pa.Y-pb.Y)*0.62,
	}

	va.z = 5
	vb.z = 4

	tl := tween.NewTimeline()
	tl.Add(0, tween.Point(pa, midA, bounceHalfDur, core.EaseOut, func(p core.Point) {
		va.x, va.y = float32(p.X), float32(p.Y)
	}))
	tl.Add(0, tween.Point(pb, midB, bounceHalfDur, core.EaseOut, func(p core.Point) {
		vb.x, vb.y = float32(p.X), float32(p.Y)
	}))
	tl.Add(bounceHalfDur, tween.Point(midA, pa, bounceHalfDur, core.EaseInOut, func(p core.Point) {
		va.x, va.y = float32(p.X), float32(p.Y)
	}))
	tl.Add(bounceHalfDur, tween.Point(midB, pb, bounceHalfDur, core.EaseInOut, func(p core.Point) {
		vb.x, vb.y = float32(p.X), float32(p.Y)
	}))
	tl.Call(bounceHalfDur*2, func() {
		va.x, va.y = float32(pa.X), float32(pa.Y)
		vb.x, vb.y = float32(pb.X), float32(pb.Y)
		va.z, vb.z = 1, 1
		g.bounceTL = nil
		g.phase = phaseIdle
	})
	tl.Play()
	g.bounceTL = tl
}

func (g *slideGemGame) resolveMatches() {
	matches := g.board.FindMatches()
	if matches.Empty() {
		if g.turnActive {
			g.turnActive = false
			g.board.FinishTurn()
			g.movesSig.Set(g.board.MovesLeft)
			g.comboSig.Set(0)
			idle := g.idleStatus()
			if g.board.GameOver {
				g.gameOverSig.Set(true)
				g.statusSig.Set("Out of moves!")
			} else if g.board.EnsurePlayable() {
				g.statusSig.Set("No moves left — board reshuffled!")
				g.syncGridVisDropIn()
				return
			} else {
				g.statusSig.Set(idle)
			}
		}
		g.phase = phaseIdle
		return
	}

	g.phase = phasePopping
	points := g.board.ClearMatches(matches)
	g.scoreSig.Set(g.board.Score)
	g.comboSig.Set(g.board.Combo)
	if g.board.Combo > 1 {
		g.statusSig.Set(fmt.Sprintf("%dx CASCADE! +%d", g.board.Combo, points))
	} else {
		g.statusSig.Set(fmt.Sprintf("Match! +%d", points))
	}
	g.recordBestScore(g.board.Score)
	g.recordClear(len(matches.Cells))

	if g.chimeSFX != nil {
		rate := min(float32(2.0), 1.0+float32(g.board.Combo-1)*0.15)
		g.chimeSFX.Play(audio.Voice{Volume: 0.75, Rate: rate})
	}

	var sumX, sumY float32
	g.popping = g.popping[:0]
	for _, cell := range matches.Cells {
		pt := cellCenter(cell)
		sumX += float32(pt.X)
		sumY += float32(pt.Y)

		g.sparkles.X = float32(pt.X)
		g.sparkles.Y = float32(pt.Y)
		g.sparkles.Burst(9)

		v := g.gridVis[cell.Row][cell.Col]
		if v == nil {
			continue
		}
		v.z = 8
		g.popping = append(g.popping, v)
		g.gridVis[cell.Row][cell.Col] = nil
	}

	// Floating score popup at match centroid.
	if n := float32(len(matches.Cells)); n > 0 {
		label := fmt.Sprintf("+%d", points)
		col := core.RGB(0xFDE047)
		if g.board.Combo > 1 {
			label = fmt.Sprintf("%dx +%d", g.board.Combo, points)
			col = core.RGB(0x38BDF8)
		}
		g.spawnScorePopup(label, sumX/n, sumY/n, col)
	}

	// Tween all matched tiles popping away; when finished, trigger gravity fall.
	remaining := len(g.popping)
	if remaining == 0 {
		g.startGravityFall()
		return
	}
	for _, tv := range g.popping {
		v := tv
		startScale := v.scale
		tw := tween.Float32(0, 1, popDuration, core.EaseOut, func(p float32) {
			// Quick elastic swell then shrink out.
			if p < 0.28 {
				v.scale = startScale * (1 + 0.25*(p/0.28))
			} else {
				u := (p - 0.28) / 0.72
				v.scale = startScale * 1.25 * (1 - u)
			}
			v.alpha = 1 - p
			v.rot = p * 0.45
		}).OnComplete(func() {
			remaining--
			if remaining == 0 {
				g.popping = g.popping[:0]
				g.startGravityFall()
			}
		})
		g.tweens.Add(tw)
	}
}

func (g *slideGemGame) startGravityFall() {
	falls := g.board.ApplyGravity()
	if len(falls) == 0 {
		g.resolveMatches()
		return
	}
	g.phase = phaseFalling

	// Build new visual grid referencing existing stationary and falling tiles.
	var nextVis [BoardSize][BoardSize]*tileVis
	for r := range BoardSize {
		for c := range BoardSize {
			nextVis[r][c] = g.gridVis[r][c]
		}
	}

	// Clear old source cells of existing tiles that moved so none are duplicated.
	movedSrc := make(map[Cell]*tileVis, len(falls))
	for _, f := range falls {
		if !f.Spawned {
			src := Cell{Row: f.FromRow, Col: f.Col}
			movedSrc[src] = g.gridVis[f.FromRow][f.Col]
			nextVis[f.FromRow][f.Col] = nil
		}
	}

	remaining := len(falls)
	for _, f := range falls {
		var v *tileVis
		if f.Spawned {
			startY := BoardOriginY + float32(f.FromRow)*CellSize + CellSize/2
			startX := BoardOriginX + float32(f.Col)*CellSize + CellSize/2
			v = &tileVis{
				kind:  f.Kind,
				x:     startX,
				y:     startY,
				scale: gemDrawScale,
				alpha: 1,
				z:     1,
			}
		} else {
			v = movedSrc[Cell{Row: f.FromRow, Col: f.Col}]
			if v == nil {
				pt := cellCenter(Cell{Row: f.FromRow, Col: f.Col})
				v = &tileVis{
					kind:  f.Kind,
					x:     float32(pt.X),
					y:     float32(pt.Y),
					scale: gemDrawScale,
					alpha: 1,
					z:     1,
				}
			}
			v.kind = f.Kind
		}

		nextVis[f.ToRow][f.Col] = v
		dist := f.ToRow - f.FromRow
		if dist < 1 {
			dist = 1
		}
		targetY := BoardOriginY + float32(f.ToRow)*CellSize + CellSize/2
		dur := fallBaseDur + time.Duration(dist)*fallPerRowDur

		tile := v
		tw := tween.Float32(tile.y, targetY, dur, core.EaseOut, func(y float32) {
			tile.y = y
		}).OnComplete(func() {
			tile.y = targetY
			remaining--
			if remaining == 0 {
				g.resolveMatches()
			}
		})
		g.tweens.Add(tw)
	}
	g.gridVis = nextVis
}

func (g *slideGemGame) spawnScorePopup(text string, x, y float32, color core.Color) {
	pop := &scorePopup{
		text:  text,
		x:     x,
		y:     y,
		alpha: 1,
		color: color,
	}
	g.popups = append(g.popups, pop)
	tw := tween.Float32(0, 1, popupDuration, core.EaseOut, func(u float32) {
		pop.y = y - 36*u
		if u > 0.55 {
			pop.alpha = 1 - (u-0.55)/0.45
		}
	}).OnComplete(func() {
		for i, p := range g.popups {
			if p == pop {
				g.popups = append(g.popups[:i], g.popups[i+1:]...)
				break
			}
		}
	})
	g.tweens.Add(tw)
}

func (g *slideGemGame) recordBestScore(score int) {
	if score <= g.bestScore {
		return
	}
	g.bestScore = score
	g.bestSig.Set(score)
	_ = g.store.Save(saveSlotName, savedStats{BestScore: score})
}

func (g *slideGemGame) requestHint() {
	if g.phase != phaseIdle || g.board.GameOver {
		return
	}
	if mv, ok := g.board.FindHint(); ok {
		g.hintMove = mv
		g.hasHint = true
		g.hintCount++
		g.statusSig.Set("Try swapping the glowing gems!")
		g.playTap()
	}
}

func (g *slideGemGame) newGame() {
	g.tweens.Clear()
	g.bounceTL = nil
	g.sparkles.Clear()
	g.popping = g.popping[:0]
	g.popups = g.popups[:0]
	g.hasSelected = false
	g.hasHint = false
	g.dragging = false
	g.turnActive = false

	g.seed++
	g.board = NewBoard(g.seed)
	g.scoreSig.Set(0)
	g.movesSig.Set(g.board.MovesLeft)
	g.comboSig.Set(0)
	g.gameOverSig.Set(false)
	g.unlockNote = ""
	g.statusSig.Set("Tap or swipe adjacent gems")
	g.syncGridVisDropIn()
	g.playTap()
}

func (g *slideGemGame) syncGridVisImmediate() {
	for r := range BoardSize {
		for c := range BoardSize {
			pt := cellCenter(Cell{Row: r, Col: c})
			g.gridVis[r][c] = &tileVis{
				kind:  g.board.Grid[r][c],
				x:     float32(pt.X),
				y:     float32(pt.Y),
				scale: gemDrawScale,
				alpha: 1,
				z:     1,
			}
		}
	}
}

func (g *slideGemGame) syncGridVisDropIn() {
	g.phase = phaseFalling
	remaining := BoardSize * BoardSize
	for r := range BoardSize {
		for c := range BoardSize {
			pt := cellCenter(Cell{Row: r, Col: c})
			startY := float32(pt.Y) - CellSize*2.2
			v := &tileVis{
				kind:  g.board.Grid[r][c],
				x:     float32(pt.X),
				y:     startY,
				scale: gemDrawScale,
				alpha: 1,
				z:     1,
			}
			g.gridVis[r][c] = v
			tile := v
			targetY := float32(pt.Y)
			delay := time.Duration(c*18+(BoardSize-1-r)*12) * time.Millisecond
			tw := tween.Float32(startY, targetY, 210*time.Millisecond, core.EaseOut, func(y float32) {
				tile.y = y
			}).Delay(delay).OnComplete(func() {
				tile.y = targetY
				remaining--
				if remaining == 0 {
					g.phase = phaseIdle
				}
			})
			g.tweens.Add(tw)
		}
	}
}

func (g *slideGemGame) playTap() {
	if g.tapSFX != nil {
		g.tapSFX.Play(audio.Voice{Volume: 0.45})
	}
}

func (g *slideGemGame) draw(d *core.Drawing, size core.Size) {
	// Subscribe to frame ticks so Canvas redraws smoothly on each Loop.Draw.
	_ = g.frameTick.Get()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	g.viewBounds = size

	g.viewport.Draw(d, size, func(d *core.Drawing, _ core.Size) {
		// Deep midnight backdrop.
		d.SetFillColor(core.RGB(0x0E162B))
		d.FillRect(0, 0, VirtualWidth, VirtualHeight)

		// Board frame card.
		framePad := float32(10)
		d.BeginPath().
			RoundRect(BoardOriginX-framePad, BoardOriginY-framePad, BoardPixelSz+framePad*2, BoardPixelSz+framePad*2, 18).
			SetFillColor(core.RGB(0x16223C)).
			Fill().
			SetStrokeColor(core.RGB(0x2B3E66)).
			SetLineWidth(2).
			Stroke()

		// Checkerboard cell tiles.
		for r := range BoardSize {
			for c := range BoardSize {
				x := BoardOriginX + float32(c)*CellSize
				y := BoardOriginY + float32(r)*CellSize
				cellCol := core.RGB(0x1A2847)
				if (r+c)%2 == 1 {
					cellCol = core.RGB(0x15203A)
				}
				d.BeginPath().
					RoundRect(x+3, y+3, CellSize-6, CellSize-6, 10).
					SetFillColor(cellCol).
					Fill()
			}
		}

		// Hint highlight rings.
		if g.hasHint && g.phase == phaseIdle {
			pulse := float32(0.55 + 0.45*math.Sin(g.pulseSec*6.5))
			for _, hc := range []Cell{g.hintMove.A, g.hintMove.B} {
				x := BoardOriginX + float32(hc.Col)*CellSize
				y := BoardOriginY + float32(hc.Row)*CellSize
				d.Save()
				d.BeginPath().
					RoundRect(x+3, y+3, CellSize-6, CellSize-6, 11).
					SetFillColor(core.RGBA(0x38BDF8, 0.16+0.16*pulse)).
					Fill().
					SetStrokeColor(core.RGBA(0x7DD3FC, 0.55+0.45*pulse)).
					SetLineWidth(3).
					Stroke()
				d.Restore()
			}
		}

		// Selected cell ring.
		if g.hasSelected && g.phase == phaseIdle {
			pulse := float32(0.65 + 0.35*math.Sin(g.pulseSec*8.0))
			x := BoardOriginX + float32(g.selected.Col)*CellSize
			y := BoardOriginY + float32(g.selected.Row)*CellSize
			d.Save()
			d.BeginPath().
				RoundRect(x+2, y+2, CellSize-4, CellSize-4, 12).
				SetFillColor(core.RGBA(0xFBBF24, 0.14)).
				Fill().
				SetStrokeColor(core.RGBA(0xFDE047, pulse)).
				SetLineWidth(3).
				Stroke()
			d.Restore()
		}

		// Clip gems to the board interior so newly spawned gems enter cleanly from the top edge.
		d.Save()
		d.BeginPath().
			RoundRect(BoardOriginX-2, BoardOriginY-2, BoardPixelSz+4, BoardPixelSz+4, 14).
			Clip().
			BeginPath()

		// Build sprite batch for all live + popping gems.
		sprites := make([]sprite.Sprite, 0, BoardSize*BoardSize+len(g.popping))
		g.batch.Sprites = g.batch.Sprites[:0]

		appendTile := func(v *tileVis, lift bool) {
			if v == nil || v.kind < 0 || int(v.kind) >= GemKinds || v.alpha <= 0 || v.scale <= 0 {
				return
			}
			frame := g.atlas.Frames[GemNames[v.kind]]
			sp := sprite.NewSprite(frame)
			sp.X = v.x
			sp.Y = v.y
			sc := v.scale
			if lift {
				sc *= 1.09 + 0.03*float32(math.Sin(g.pulseSec*8.0))
			}
			sp.ScaleX = sc
			sp.ScaleY = sc
			sp.Rotation = v.rot
			sp.Alpha = v.alpha
			sp.Z = v.z
			sprites = append(sprites, sp)
		}

		for r := range BoardSize {
			for c := range BoardSize {
				lift := g.hasSelected && g.phase == phaseIdle && g.selected.Row == r && g.selected.Col == c
				appendTile(g.gridVis[r][c], lift)
			}
		}
		for _, pv := range g.popping {
			appendTile(pv, false)
		}
		for i := range sprites {
			g.batch.Sprites = append(g.batch.Sprites, &sprites[i])
		}
		g.batch.Draw(d)
		d.Restore()

		// Draw sparkle bursts above the board clip so particles can burst freely.
		g.sparkles.Draw(d)

		// Floating score popups.
		for _, pop := range g.popups {
			if pop.alpha <= 0 {
				continue
			}
			d.Save()
			d.SetAlpha(pop.alpha)
			d.SetTextAlign(core.TextAlignCenter)
			d.SetTextBaseline(core.BaselineMiddle)
			d.SetFillColor(pop.color)
			d.FillText(pop.text, pop.x, pop.y, core.Font{
				Size:   18,
				Weight: core.FontWeightBold,
			})
			d.Restore()
		}
	})
}

func cellActionName(r, c int) string {
	return fmt.Sprintf("cell_%d_%d", r, c)
}

func cellBounds(c Cell) core.Bounds {
	return core.Bounds{
		Origin: core.Point{
			X: core.Points(BoardOriginX + float32(c.Col)*CellSize),
			Y: core.Points(BoardOriginY + float32(c.Row)*CellSize),
		},
		Size: core.Size{
			Width:  core.Points(CellSize),
			Height: core.Points(CellSize),
		},
	}
}

func cellCenter(c Cell) core.Point {
	return core.Point{
		X: core.Points(BoardOriginX + float32(c.Col)*CellSize + CellSize/2),
		Y: core.Points(BoardOriginY + float32(c.Row)*CellSize + CellSize/2),
	}
}

func cellAtPoint(p core.Point) (Cell, bool) {
	gx := float32(p.X) - BoardOriginX
	gy := float32(p.Y) - BoardOriginY
	if gx < 0 || gy < 0 || gx >= BoardPixelSz || gy >= BoardPixelSz {
		return Cell{}, false
	}
	c := Cell{
		Row: int(gy / CellSize),
		Col: int(gx / CellSize),
	}
	if !InBounds(c) {
		return Cell{}, false
	}
	return c, true
}

// buildGemAtlas procedurally renders 6 distinct faceted gems + 2 sparkle particle
// sprites into a single RGBA image atlas — zero external asset files required.
func buildGemAtlas() *sprite.Atlas {
	const totalFrames = GemKinds + 2
	img := image.NewRGBA(image.Rect(0, 0, atlasFramePx*totalFrames, atlasFramePx))

	type gemPalette struct {
		base [3]float64
		hi   [3]float64
		dark [3]float64
	}
	palettes := [GemKinds]gemPalette{
		// Ruby: rich crimson octagon
		{base: [3]float64{230, 50, 74}, hi: [3]float64{255, 165, 178}, dark: [3]float64{130, 18, 36}},
		// Sapphire: royal blue diamond
		{base: [3]float64{38, 108, 240}, hi: [3]float64{145, 200, 255}, dark: [3]float64{18, 48, 138}},
		// Emerald: lush green chamfered emerald cut
		{base: [3]float64{16, 185, 129}, hi: [3]float64{142, 245, 205}, dark: [3]float64{8, 92, 66}},
		// Amber: golden hexagonal sunstone
		{base: [3]float64{245, 158, 11}, hi: [3]float64{255, 232, 120}, dark: [3]float64{158, 74, 8}},
		// Amethyst: royal violet crystal shield
		{base: [3]float64{155, 65, 238}, hi: [3]float64{226, 185, 255}, dark: [3]float64{82, 24, 138}},
		// Pearl: luminous cyan orb-star
		{base: [3]float64{14, 195, 224}, hi: [3]float64{205, 250, 255}, dark: [3]float64{12, 92, 122}},
	}

	frames := make(map[string]sprite.Frame, totalFrames)
	for kind := range GemKinds {
		ox := kind * atlasFramePx
		drawProceduralGem(img, ox, kind, palettes[kind].base, palettes[kind].hi, palettes[kind].dark)
		frames[GemNames[kind]] = sprite.Frame{
			Name:   GemNames[kind],
			X:      float32(ox),
			Y:      0,
			W:      atlasFramePx,
			H:      atlasFramePx,
			Width:  atlasFramePx,
			Height: atlasFramePx,
			PivotX: atlasFramePx / 2,
			PivotY: atlasFramePx / 2,
		}
	}

	// Sparkle star frame (index 6).
	starOX := GemKinds * atlasFramePx
	drawSparkleStar(img, starOX)
	frames["sparkle_star"] = sprite.Frame{
		Name:   "sparkle_star",
		X:      float32(starOX),
		Y:      0,
		W:      atlasFramePx,
		H:      atlasFramePx,
		Width:  atlasFramePx,
		Height: atlasFramePx,
		PivotX: atlasFramePx / 2,
		PivotY: atlasFramePx / 2,
	}

	// Sparkle dot frame (index 7).
	dotOX := (GemKinds + 1) * atlasFramePx
	drawSparkleDot(img, dotOX)
	frames["sparkle_dot"] = sprite.Frame{
		Name:   "sparkle_dot",
		X:      float32(dotOX),
		Y:      0,
		W:      atlasFramePx,
		H:      atlasFramePx,
		Width:  atlasFramePx,
		Height: atlasFramePx,
		PivotX: atlasFramePx / 2,
		PivotY: atlasFramePx / 2,
	}

	return &sprite.Atlas{
		Image:  core.ImageSource{Pixels: img, Version: 1},
		Frames: frames,
	}
}

func drawProceduralGem(img *image.RGBA, ox, kind int, base, hi, dark [3]float64) {
	half := float64(atlasFramePx) / 2.0
	rad := half - 5.0
	for py := range atlasFramePx {
		for px := range atlasFramePx {
			nx := (float64(px) + 0.5 - half) / rad
			ny := (float64(py) + 0.5 - half) / rad
			dist := gemShapeSDF(kind, nx, ny)
			if dist > 1.04 {
				continue
			}

			// Smooth anti-aliased alpha edge.
			alpha := 1.0
			if dist > 0.94 {
				alpha = (1.04 - dist) / 0.10
			}
			alpha = clamp01(alpha)

			// Faceted lighting from top-left + crisp inner table + rim bevel.
			light := clamp01(0.56 - 0.36*nx - 0.42*ny)
			var r, g, b float64
			if dist > 0.78 {
				// Outer dark bevel rim with top-left catchlight.
				bevel := clamp01(0.25 + 0.55*light)
				r = mix(dark[0], base[0], bevel)
				g = mix(dark[1], base[1], bevel)
				b = mix(dark[2], base[2], bevel)
			} else if dist < 0.48 {
				// Bright polished inner table facet.
				table := clamp01(0.35 + 0.50*light)
				r = mix(base[0], hi[0], table)
				g = mix(base[1], hi[1], table)
				b = mix(base[2], hi[2], table)
			} else {
				// Angled crown facets with subtle quadrant variation.
				facet := light
				if (nx > 0) != (ny > 0) {
					facet = clamp01(facet * 0.86)
				}
				r = mix(dark[0], hi[0], 0.22+0.68*facet)
				g = mix(dark[1], hi[1], 0.22+0.68*facet)
				b = mix(dark[2], hi[2], 0.22+0.68*facet)
			}

			// Crisp specular glint dot near top-left.
			gx, gy := nx+0.28, ny+0.30
			glint := math.Exp(-(gx*gx+gy*gy)*26.0) * 0.85
			r = mix(r, 255, glint)
			g = mix(g, 255, glint)
			b = mix(b, 255, glint)

			setPremulRGBA(img, ox+px, py, r, g, b, alpha*255)
		}
	}
}

// gemShapeSDF returns a normalized radial distance (<= 1 inside the gem silhouette)
// giving each of the 6 gem kinds a distinct geometric silhouette.
func gemShapeSDF(kind int, x, y float64) float64 {
	ax, ay := math.Abs(x), math.Abs(y)
	switch GemKind(kind) {
	case GemRuby:
		// Octagonal cushion cut.
		return max(max(ax, ay), (ax+ay)*0.74) / 0.96
	case GemSapphire:
		// Crisp diamond cut.
		return (ax*1.04 + ay*0.96) / 1.02
	case GemEmerald:
		// Emerald chamfered rectangle.
		return max(max(ax*1.12, ay*0.94), (ax+ay)*0.72) / 0.96
	case GemAmber:
		// Regular hexagon.
		return max(ay*1.06, (ax*0.8660254+ay*0.5)*1.06) / 0.96
	case GemAmethyst:
		// Rounded teardrop / crystal shield.
		sy := y - 0.08
		w := 0.88 - 0.22*clamp01(-sy)
		return math.Hypot(x/w, sy*1.02) / 0.94
	default: // GemPearl
		// Brilliant circular orb.
		return math.Hypot(x, y) / 0.94
	}
}

func drawSparkleStar(img *image.RGBA, ox int) {
	half := float64(atlasFramePx) / 2.0
	rad := half - 6.0
	for py := range atlasFramePx {
		for px := range atlasFramePx {
			nx := (float64(px) + 0.5 - half) / rad
			ny := (float64(py) + 0.5 - half) / rad
			ax, ay := math.Abs(nx), math.Abs(ny)
			// 4-point star distance.
			d := math.Pow(ax, 0.58) + math.Pow(ay, 0.58)
			if d >= 1.0 {
				continue
			}
			a := clamp01(1.0 - d)
			coreGlow := clamp01(1.0 - math.Hypot(nx, ny)*1.4)
			r := mix(255, 255, coreGlow)
			g := mix(228, 255, coreGlow)
			b := mix(120, 255, coreGlow)
			setPremulRGBA(img, ox+px, py, r, g, b, a*255)
		}
	}
}

func drawSparkleDot(img *image.RGBA, ox int) {
	half := float64(atlasFramePx) / 2.0
	rad := half - 14.0
	for py := range atlasFramePx {
		for px := range atlasFramePx {
			nx := (float64(px) + 0.5 - half) / rad
			ny := (float64(py) + 0.5 - half) / rad
			d := math.Hypot(nx, ny)
			if d >= 1.0 {
				continue
			}
			a := math.Pow(1.0-d, 1.5)
			setPremulRGBA(img, ox+px, py, 190, 240, 255, a*255)
		}
	}
}

func setPremulRGBA(img *image.RGBA, x, y int, r, g, b, a float64) {
	alpha := clamp01(a / 255.0)
	off := img.PixOffset(x, y)
	img.Pix[off+0] = clampByte(r * alpha)
	img.Pix[off+1] = clampByte(g * alpha)
	img.Pix[off+2] = clampByte(b * alpha)
	img.Pix[off+3] = clampByte(a)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func mix(a, b, t float64) float64 {
	return a + (b-a)*t
}

// Procedural WAV builders (16-bit PCM mono, 22050 Hz) — zero external audio files.
func buildChimeWAV() []byte {
	return synthesizeWAV(22050, 0.16, func(t, dur float64) float64 {
		env := math.Min(1.0, t/0.008) * math.Exp(-t*18.0)
		s1 := math.Sin(2 * math.Pi * 523.25 * t)
		s2 := 0.45 * math.Sin(2*math.Pi*1046.5*t)
		s3 := 0.20 * math.Sin(2*math.Pi*1567.98*t)
		return 0.55 * env * (s1 + s2 + s3)
	})
}

func buildBounceWAV() []byte {
	return synthesizeWAV(22050, 0.13, func(t, dur float64) float64 {
		env := math.Min(1.0, t/0.006) * math.Exp(-t*22.0)
		freq := 195.0 - 65.0*(t/dur)
		return 0.55 * env * math.Sin(2*math.Pi*freq*t)
	})
}

func buildSelectWAV() []byte {
	return synthesizeWAV(22050, 0.045, func(t, _ float64) float64 {
		env := math.Min(1.0, t/0.003) * math.Exp(-t*55.0)
		return 0.40 * env * math.Sin(2*math.Pi*784.0*t)
	})
}

func synthesizeWAV(sampleRate int, durationSec float64, sample func(t, dur float64) float64) []byte {
	n := int(float64(sampleRate) * durationSec)
	dataSize := n * 2
	buf := make([]byte, 44+dataSize)

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))

	for i := range n {
		t := float64(i) / float64(sampleRate)
		v := sample(t, durationSec)
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(int16(v*32767)))
	}
	return buf
}
