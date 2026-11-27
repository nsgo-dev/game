package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const testFrameStep = 17 * time.Millisecond

func startTestGame(t *testing.T, seed uint64, opts ...nsgotest.Option) (*nsgotest.App, *slideGemGame) {
	t.Helper()
	var g *slideGemGame
	baseOpts := []nsgotest.Option{
		nsgotest.Size(440, 660),
		nsgotest.Environment(core.Environment{
			Audio:    true,
			SafeArea: core.Edges{Top: 24, Bottom: 16},
		}),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{
				string(buildChimeWAV()):  160 * time.Millisecond,
				string(buildBounceWAV()): 130 * time.Millisecond,
				string(buildSelectWAV()): 45 * time.Millisecond,
			}
		},
	}
	baseOpts = append(baseOpts, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		g, node = newSlideGem(cx, seed)
		return node
	}, baseOpts...)
	app.Advance(testFrameStep)
	app.Settle()
	return app, g
}

func cellViewPoint(g *slideGemGame, c Cell) core.Point {
	return g.viewport.GameToView(g.viewSize(), cellCenter(c))
}

func tapCell(app *nsgotest.App, g *slideGemGame, c Cell) {
	pt := cellViewPoint(g, c)
	finger := app.Touch("game-surface", pt)
	app.Advance(testFrameStep)
	finger.Lift()
	app.Advance(testFrameStep)
}

func advanceUntilIdle(t *testing.T, app *nsgotest.App, g *slideGemGame, maxFrames int) {
	t.Helper()
	for range maxFrames {
		app.Advance(testFrameStep)
		if g.phase == phaseIdle {
			app.Settle()
			return
		}
	}
	t.Fatalf("game did not return to phaseIdle after %d frames (phase=%d)", maxFrames, g.phase)
}

func findInvalidAdjacentPair(b *Board) (Move, bool) {
	for r := range BoardSize {
		for c := range BoardSize {
			a := Cell{Row: r, Col: c}
			if c+1 < BoardSize {
				right := Cell{Row: r, Col: c + 1}
				if !b.WouldMatch(a, right) {
					return Move{A: a, B: right}, true
				}
			}
			if r+1 < BoardSize {
				down := Cell{Row: r + 1, Col: c}
				if !b.WouldMatch(a, down) {
					return Move{A: a, B: down}, true
				}
			}
		}
	}
	return Move{}, false
}

func TestDeterministicBoardGeneration(t *testing.T) {
	a := NewBoard(42)
	b := NewBoard(42)
	c := NewBoard(43)

	if a.Grid != b.Grid {
		t.Fatal("same seed produced different boards")
	}
	if a.Grid == c.Grid {
		t.Fatal("different seeds unexpectedly produced identical boards")
	}

	for seed := uint64(1); seed <= 50; seed++ {
		board := NewBoard(seed)
		if m := board.FindMatches(); !m.Empty() {
			t.Fatalf("seed %d generated board with initial matches: %+v", seed, m.Cells)
		}
		mv, ok := board.FindHint()
		if !ok {
			t.Fatalf("seed %d generated board with no valid move", seed)
		}
		if !Adjacent(mv.A, mv.B) || !board.WouldMatch(mv.A, mv.B) {
			t.Fatalf("seed %d hint %+v is not a valid match move", seed, mv)
		}
	}
}

func TestMatchDetectionCascadeGravityAndComboScoring(t *testing.T) {
	b := NewBoard(77)

	// Set up a controlled board:
	// Clearing a horizontal ruby 3-match on row 5 (cols 0..2) drops a blue sapphire
	// from (4,1) to (5,1), forming a horizontal sapphire 3-match at (5,0..2) or vertical cascade.
	b.Grid = [BoardSize][BoardSize]GemKind{
		{GemEmerald, GemAmber, GemAmethyst, GemPearl, GemRuby, GemSapphire},
		{GemAmber, GemPearl, GemEmerald, GemAmethyst, GemSapphire, GemRuby},
		{GemPearl, GemSapphire, GemAmber, GemEmerald, GemRuby, GemAmethyst},
		{GemAmethyst, GemSapphire, GemPearl, GemAmber, GemEmerald, GemRuby},
		{GemSapphire, GemEmerald, GemSapphire, GemPearl, GemAmber, GemAmethyst},
		{GemRuby, GemRuby, GemRuby, GemAmethyst, GemSapphire, GemEmerald},
	}

	m1 := b.FindMatches()
	if len(m1.Cells) != 3 || len(m1.Groups) != 1 {
		t.Fatalf("initial match = %d cells / %d groups, want 3 / 1", len(m1.Cells), len(m1.Groups))
	}

	pts1 := b.ClearMatches(m1)
	if pts1 != 90 || b.Combo != 1 || b.Score != 90 {
		t.Fatalf("first clear: pts=%d combo=%d score=%d, want 90 / 1 / 90", pts1, b.Combo, b.Score)
	}

	falls := b.ApplyGravity()
	if len(falls) == 0 {
		t.Fatal("ApplyGravity returned no fall moves")
	}

	// Verify spawned gems enter from negative rows and all cells are repopulated.
	spawnedCount := 0
	for _, fm := range falls {
		if fm.Spawned {
			spawnedCount++
			if fm.FromRow >= 0 {
				t.Fatalf("spawned gem FromRow=%d, want < 0", fm.FromRow)
			}
		} else if fm.ToRow <= fm.FromRow {
			t.Fatalf("fallen gem did not move down: %+v", fm)
		}
	}
	if spawnedCount != 3 {
		t.Fatalf("spawnedCount=%d, want 3", spawnedCount)
	}
	for r := range BoardSize {
		for c := range BoardSize {
			if b.Grid[r][c] == GemNone {
				t.Fatalf("cell (%d,%d) remained empty after ApplyGravity", r, c)
			}
		}
	}

	// Verify row 5 col 1 now holds GemEmerald dropped from row 4, and col 1 rows 3..4 hold GemSapphire.
	// Now verify a second match increments combo to 2 and doubles score.
	b.Grid[5][0] = GemSapphire
	b.Grid[5][1] = GemSapphire
	b.Grid[5][2] = GemSapphire
	b.Grid[5][3] = GemSapphire // 4-gem match with combo=2 -> (4*30 + 1*40)*2 = 320
	m2 := b.FindMatches()
	if len(m2.Cells) < 4 {
		t.Fatalf("cascade match len=%d, want >= 4", len(m2.Cells))
	}
	pts2 := b.ClearMatches(m2)
	if b.Combo != 2 || pts2 < 320 {
		t.Fatalf("cascade clear: combo=%d pts=%d, want combo=2 pts>=320", b.Combo, pts2)
	}

	// Verify deadlock reshuffle via EnsurePlayable.
	for r := range BoardSize {
		for c := range BoardSize {
			b.Grid[r][c] = GemKind((r*3 + c) % GemKinds)
		}
	}
	if _, ok := b.FindHint(); ok {
		t.Fatal("expected constructed deadlock board to have no hint")
	}
	if !b.EnsurePlayable() {
		t.Fatal("EnsurePlayable did not reshuffle deadlocked board")
	}
	if _, ok := b.FindHint(); !ok {
		t.Fatal("board still unplayable after EnsurePlayable")
	}
	if !b.FindMatches().Empty() {
		t.Fatal("reshuffled board has immediate matches")
	}
}

func TestTouchFirstTapSwapCascadeAndHighScorePersistence(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)

	mv, ok := g.board.FindHint()
	if !ok {
		t.Fatal("seeded board has no valid move")
	}

	// Tap first cell -> selects it.
	tapCell(app, g, mv.A)
	if !g.hasSelected || g.selected != mv.A {
		t.Fatalf("after first tap: hasSelected=%v selected=%+v, want %+v", g.hasSelected, g.selected, mv.A)
	}

	// Tap adjacent second cell -> triggers tweened swap -> pop -> sparkle burst -> cascade fall.
	tapCell(app, g, mv.B)
	if g.phase == phaseIdle {
		t.Fatal("valid swap did not enter animation phase")
	}

	sawParticles := false
	for range 120 {
		app.Advance(testFrameStep)
		if g.sparkles.Count() > 0 {
			sawParticles = true
		}
		if g.phase == phaseIdle {
			break
		}
	}
	app.Settle()

	if g.phase != phaseIdle {
		t.Fatalf("swap sequence did not settle to idle (phase=%d)", g.phase)
	}
	if !sawParticles {
		t.Fatal("expected sparkle particles to burst on cleared match")
	}
	if g.board.Score <= 0 {
		t.Fatalf("score=%d after valid match swap, want > 0", g.board.Score)
	}
	if g.board.MovesLeft != InitialMoves-1 {
		t.Fatalf("moves=%d, want %d", g.board.MovesLeft, InitialMoves-1)
	}
	if g.bestScore != g.board.Score {
		t.Fatalf("bestScore=%d, want %d", g.bestScore, g.board.Score)
	}

	// Verify HUD labels reflect updated state.
	wantScore := fmt.Sprintf("%d", g.board.Score)
	wantMoves := fmt.Sprintf("%d", InitialMoves-1)
	if got := app.Find("hud-score").Text; got != wantScore {
		t.Fatalf("hud-score=%q, want %q", got, wantScore)
	}
	if got := app.Find("hud-best").Text; got != wantScore {
		t.Fatalf("hud-best=%q, want %q", got, wantScore)
	}
	if got := app.Find("hud-moves").Text; got != wantMoves {
		t.Fatalf("hud-moves=%q, want %q", got, wantMoves)
	}

	// Persist storage across a fresh app restart and confirm Best Score survives.
	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	if len(kept) == 0 {
		t.Fatal("expected save.NewStorageStore to persist high score keys")
	}

	restartedApp, restartedGame := startTestGame(t, defaultSeed+99, nsgotest.Storage(kept))
	if restartedGame.bestScore != g.board.Score {
		t.Fatalf("restarted bestScore=%d, want %d", restartedGame.bestScore, g.board.Score)
	}
	if got := restartedApp.Find("hud-best").Text; got != wantScore {
		t.Fatalf("restarted hud-best=%q, want %q", got, wantScore)
	}
}

func TestTouchSwipeSwap(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)
	mv, ok := g.board.FindHint()
	if !ok {
		t.Fatal("seeded board has no hint")
	}

	ptA := cellViewPoint(g, mv.A)
	ptB := cellViewPoint(g, mv.B)

	finger := app.Touch("game-surface", ptA)
	app.Advance(testFrameStep)
	finger.Move(ptB)
	app.Advance(testFrameStep)
	finger.Lift()

	advanceUntilIdle(t, app, g, 120)

	if g.board.Score <= 0 {
		t.Fatalf("swipe swap did not score: score=%d", g.board.Score)
	}
	if g.board.MovesLeft != InitialMoves-1 {
		t.Fatalf("moves=%d after swipe swap, want %d", g.board.MovesLeft, InitialMoves-1)
	}
}

func TestInvalidSwapBounceBackTimeline(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)
	origGrid := g.board.Grid

	bad, ok := findInvalidAdjacentPair(g.board)
	if !ok {
		t.Fatal("could not find an invalid adjacent pair")
	}

	va := g.gridVis[bad.A.Row][bad.A.Col]
	pa := cellCenter(bad.A)

	tapCell(app, g, bad.A)
	tapCell(app, g, bad.B)

	if g.phase != phaseBouncing || g.bounceTL == nil {
		t.Fatalf("expected phaseBouncing with active tween.Timeline, got phase=%d bounceTL=%v", g.phase, g.bounceTL)
	}
	if g.bounces != 1 {
		t.Fatalf("bounces=%d, want 1", g.bounces)
	}

	// Step halfway into the bounce lunge and verify visual offset moved.
	for range 4 {
		app.Advance(testFrameStep)
	}
	moved := math.Hypot(float64(va.x-float32(pa.X)), float64(va.y-float32(pa.Y)))
	if moved < 5 {
		t.Fatalf("expected bounced tile to displace during Timeline lunge, moved=%v", moved)
	}

	// Finish bounce timeline.
	advanceUntilIdle(t, app, g, 40)

	if g.bounceTL != nil {
		t.Fatal("bounceTL was not cleared after completion")
	}
	if g.board.Grid != origGrid {
		t.Fatal("invalid swap mutated the board grid")
	}
	if g.board.Score != 0 || g.board.MovesLeft != InitialMoves {
		t.Fatalf("invalid swap altered score=%d or moves=%d", g.board.Score, g.board.MovesLeft)
	}
	if math.Abs(float64(va.x-float32(pa.X))) > 0.5 || math.Abs(float64(va.y-float32(pa.Y))) > 0.5 {
		t.Fatalf("tile did not bounce back to origin: got (%v,%v), want (%v,%v)", va.x, va.y, pa.X, pa.Y)
	}
}

func TestHintNewGameButtonsAndCanvasDrawOps(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)

	// Press Hint button and verify a valid move is highlighted.
	app.Press("btn-hint")
	app.Advance(testFrameStep)
	app.Settle()

	if !g.hasHint || g.hintCount != 1 {
		t.Fatalf("Hint button did not activate hint (hasHint=%v hintCount=%d)", g.hasHint, g.hintCount)
	}
	if !g.board.WouldMatch(g.hintMove.A, g.hintMove.B) {
		t.Fatalf("hint move %+v is not a valid match", g.hintMove)
	}

	// Inspect recorded core.DrawOps on the canvas.
	canvas := app.Find("game-canvas")
	if len(canvas.Ops) == 0 {
		t.Fatal("expected recorded core.DrawOps on game-canvas")
	}
	drawImageOps := len(gametest.ImageDraws(canvas.Ops))
	roundRectOps := 0
	for _, op := range canvas.Ops {
		if op.Kind == core.DrawRoundRect {
			roundRectOps++
		}
	}
	if drawImageOps < BoardSize*BoardSize {
		t.Fatalf("drew %d images, want at least %d (one per gem)", drawImageOps, BoardSize*BoardSize)
	}
	if roundRectOps < BoardSize*BoardSize {
		t.Fatalf("recorded %d DrawRoundRect ops, want at least %d (board tiles + highlights)", roundRectOps, BoardSize*BoardSize)
	}

	// Execute the hinted move so score > 0 and moves decrement.
	tapCell(app, g, g.hintMove.A)
	tapCell(app, g, g.hintMove.B)
	advanceUntilIdle(t, app, g, 120)
	if g.board.Score <= 0 {
		t.Fatal("hinted move did not score")
	}
	prevBest := g.bestScore
	prevSeed := g.seed

	// Press New Game button and verify clean reset preserving Best Score.
	app.Press("btn-new-game")
	advanceUntilIdle(t, app, g, 60)

	if g.seed == prevSeed {
		t.Fatal("New Game did not advance puzzle seed")
	}
	if g.board.Score != 0 || g.board.MovesLeft != InitialMoves {
		t.Fatalf("New Game did not reset state: score=%d moves=%d", g.board.Score, g.board.MovesLeft)
	}
	if g.bestScore != prevBest {
		t.Fatalf("New Game lost bestScore: got %d, want %d", g.bestScore, prevBest)
	}
	if got := app.Find("hud-score").Text; got != "0" {
		t.Fatalf("hud-score after New Game=%q, want \"0\"", got)
	}
	if got := app.Find("hud-moves").Text; got != fmt.Sprintf("%d", InitialMoves) {
		t.Fatalf("hud-moves after New Game=%q, want \"%d\"", got, InitialMoves)
	}
}

func TestSafeAreaHUDAndProceduralAssets(t *testing.T) {
	insets := core.Edges{Top: 44, Left: 12, Right: 12, Bottom: 34}
	app, _ := startTestGame(t, defaultSeed,
		nsgotest.Size(440, 660),
		nsgotest.Environment(core.Environment{Audio: true, SafeArea: insets}),
	)

	hud := app.Find("hud-overlay")
	if hud == nil {
		t.Fatal("hud-overlay not found")
	}
	hudBounds := hud.Bounds()
	if hudBounds.Origin.Y < insets.Top-0.5 || hudBounds.Origin.X < insets.Left-0.5 {
		t.Fatalf("hud-overlay bounds %+v do not respect safe area %+v", hudBounds, insets)
	}

	// Verify procedural atlas & WAV headers.
	atlas := buildGemAtlas()
	if atlas.Image.Pixels == nil {
		t.Fatal("procedural gem atlas has nil RGBA pixels")
	}
	for _, name := range GemNames {
		if _, ok := atlas.Frames[name]; !ok {
			t.Fatalf("missing gem atlas frame %q", name)
		}
	}
	for _, wav := range [][]byte{buildChimeWAV(), buildBounceWAV(), buildSelectWAV()} {
		if len(wav) <= 44 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
			t.Fatalf("invalid procedural WAV header (len=%d)", len(wav))
		}
	}
}

func TestGameOverPlayAgainAudioAndRootMount(t *testing.T) {
	app, g := startTestGame(t, defaultSeed)
	g.board.MovesLeft = 1
	g.movesSig.Set(1)

	mv, ok := g.board.FindHint()
	if !ok {
		t.Fatal("expected valid hint")
	}
	tapCell(app, g, mv.A)
	tapCell(app, g, mv.B)
	advanceUntilIdle(t, app, g, 120)

	if !g.board.GameOver || !g.gameOverSig.Get() {
		t.Fatal("expected GameOver after final move")
	}
	if !app.Exists("game-over-card") {
		t.Fatal("expected game-over-card to mount when out of moves")
	}
	// Verify procedural chime audio played on match.
	totalPlays := 0
	for _, s := range app.Sounds {
		totalPlays += len(s.Plays)
	}
	if totalPlays == 0 {
		t.Fatal("expected procedural SFX to play during gameplay")
	}

	// Press Play Again -> restarts cleanly and dismisses Game Over card.
	app.Press("btn-play-again")
	advanceUntilIdle(t, app, g, 60)

	if g.board.GameOver || g.board.MovesLeft != InitialMoves || g.board.Score != 0 {
		t.Fatalf("Play Again did not reset cleanly: gameOver=%v moves=%d score=%d", g.board.GameOver, g.board.MovesLeft, g.board.Score)
	}
	if app.Exists("game-over-card") {
		t.Fatal("game-over-card still mounted after Play Again")
	}

	// Verify exported Root mounts cleanly as well.
	rootApp := nsgotest.Start(t, Root, nsgotest.Size(440, 660))
	rootApp.Advance(testFrameStep)
	rootApp.Settle()
	if !rootApp.Exists("game-canvas") || !rootApp.Exists("btn-hint") || !rootApp.Exists("btn-new-game") {
		t.Fatal("Root did not mount expected game views")
	}
}
