package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain gives the platform the main thread under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

const (
	// stepTime is one fixed step of the game loop.
	stepTime = time.Second / stepRate
	// settleTime is longer than any move's animation, so the result shows after it.
	settleTime = 700 * time.Millisecond
)

// startGame starts the game in a phone-sized window and returns it with its state.
func startGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *sparkGame) {
	t.Helper()
	var g *sparkGame
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		g = newSparkGame(cx)
		return g.view()
	}, append([]nsgotest.Option{nsgotest.Size(480, 720)}, opts...)...)
	app.Advance(stepTime)
	return app, g
}

// keyFor is the arrow key that slides the spark in d.
var keyFor = map[Dir]string{DirLeft: "left", DirRight: "right", DirUp: "up", DirDown: "down"}

// key presses a key and lets the move it makes finish.
func key(app *nsgotest.App, name string) {
	app.Key(name)
	app.Advance(settleTime)
}

// unlockAll records a one-star win of every level, as if played before.
func unlockAll(g *sparkGame) {
	for _, l := range g.levels {
		g.progress.Record(l.ID, 1)
	}
	g.saved.Update(func(n int) int { return n + 1 })
}

func expectText(t *testing.T, app *nsgotest.App, id, want string) {
	t.Helper()
	if got := app.Find(id).Text; got != want {
		t.Fatalf("%s = %q, want %q", id, got, want)
	}
}

// TestKeyboardWinsALevelAndSavesItsStars plays the first level to a three-star win with the arrow keys, and checks that the stars and the unlocked second level outlive the app.
func TestKeyboardWinsALevelAndSavesItsStars(t *testing.T) {
	app, g := startGame(t)
	expectText(t, app, "level-1-stars", "☆☆☆")
	expectText(t, app, "level-2-stars", "Locked")

	key(app, "enter")
	expectText(t, app, "level-name", "1. "+Levels.Levels[0].Name)
	best, path := solve(NewBoard(&Levels.Levels[0]))
	for i, d := range path {
		if app.Exists("result-title") {
			t.Fatalf("the result showed after %d of %d moves", i, best)
		}
		key(app, keyFor[d])
	}
	expectText(t, app, "result-title", "Level clear")
	expectText(t, app, "result-stars", "★★★")
	if got := g.progress.Stars[Levels.Levels[0].ID]; got != 3 {
		t.Fatalf("progress holds %d stars for level 1, want 3", got)
	}
	// Undo keeps a won level won.
	key(app, "z")
	expectText(t, app, "result-title", "Level clear")

	// Enter goes on to the next level.
	key(app, "enter")
	expectText(t, app, "level-name", "2. "+Levels.Levels[1].Name)

	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	again, _ := startGame(t, nsgotest.Storage(kept))
	expectText(t, again, "level-1-stars", "★★★")
	expectText(t, again, "level-2-stars", "☆☆☆")
}

// TestKeyboardRunsOutOfMovesThenUndoesAndRestarts loses the first level by sliding back and forth until its moves run out, undoes the last move and restarts it.
func TestKeyboardRunsOutOfMovesThenUndoesAndRestarts(t *testing.T) {
	app, g := startGame(t)
	key(app, "enter")
	l := &Levels.Levels[0]
	for i := range l.Moves {
		if i%2 == 0 {
			key(app, "right")
		} else {
			key(app, "left")
		}
	}
	expectText(t, app, "result-title", "Out of moves")
	expectText(t, app, "moves", "Moves "+strconv.Itoa(l.Moves)+"/"+strconv.Itoa(l.Moves))

	app.Press("role:button Undo")
	app.Advance(stepTime)
	if app.Exists("result-title") {
		t.Fatal("the result still shows after undo")
	}
	expectText(t, app, "moves", "Moves "+strconv.Itoa(l.Moves-1)+"/"+strconv.Itoa(l.Moves))

	// A wall blocks up from the start's row, which costs no move.
	key(app, "r")
	expectText(t, app, "moves", "Moves 0/"+strconv.Itoa(l.Moves))
	key(app, "up")
	expectText(t, app, "moves", "Moves 0/"+strconv.Itoa(l.Moves))

	// Redo makes an undone move again.
	key(app, "right")
	key(app, "z")
	expectText(t, app, "moves", "Moves 0/"+strconv.Itoa(l.Moves))
	key(app, "y")
	expectText(t, app, "moves", "Moves 1/"+strconv.Itoa(l.Moves))

	// Escape goes back to the level select.
	key(app, "escape")
	if g.screen.Get() != screenLevels || !app.Exists("level-1") {
		t.Fatal("Escape did not show the level select")
	}
}

// TestGamepadFallsIntoTheVoidAndUndoes plays the second level with a game controller: the D-pad slides the spark into a void, West undoes it, South retries from the result.
func TestGamepadFallsIntoTheVoidAndUndoes(t *testing.T) {
	app, g := startGame(t)
	unlockAll(g)
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(stepTime)
	button := func(b gamepad.Button) {
		app.SetGamepadButton(1, b, 1)
		app.Advance(2 * stepTime)
		app.SetGamepadButton(1, b, 0)
		app.Advance(settleTime)
	}

	button(gamepad.ButtonDPadRight)
	if g.cursor.Get() != 1 {
		t.Fatalf("D-pad right highlighted level %d, want 2", g.cursor.Get()+1)
	}
	button(gamepad.ButtonSouth)
	expectText(t, app, "level-name", "2. "+Levels.Levels[1].Name)

	button(gamepad.ButtonDPadRight)
	expectText(t, app, "result-title", "Lost in the void")
	if g.sparkScale != 0 {
		t.Fatalf("the fallen spark is still drawn at scale %v", g.sparkScale)
	}

	button(gamepad.ButtonWest)
	if app.Exists("result-title") || g.board().Status() != Playing {
		t.Fatal("West did not undo the fall")
	}

	button(gamepad.ButtonDPadRight)
	button(gamepad.ButtonSouth)
	expectText(t, app, "moves", "Moves 0/"+strconv.Itoa(Levels.Levels[1].Moves))
}

// TestGamepadStickMovesOncePerPush checks that holding the left stick over makes one move, and that letting it back to the middle lets it make another.
func TestGamepadStickMovesOncePerPush(t *testing.T) {
	app, g := startGame(t)
	key(app, "enter")
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.Advance(stepTime)

	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	app.Advance(settleTime)
	app.Advance(settleTime)
	if b := g.board(); b.Moves != 1 || b.Spark.X == 1 {
		t.Fatalf("holding the stick right made %d moves, spark at %v", b.Moves, b.Spark)
	}
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	app.Advance(stepTime)
	app.SetGamepadAxis(1, gamepad.AxisLeftX, -1)
	app.Advance(settleTime)
	if b := g.board(); b.Moves != 2 || b.Spark != (Cell{1, 1}) {
		t.Fatalf("pushing the stick left made %d moves, spark at %v", b.Moves, b.Spark)
	}
}

// TestTouchSwipesAndTapsSlideTheSpark plays the first level by touch: a swipe slides the spark the way it goes, and a tap slides it toward where the finger lands.
func TestTouchSwipesAndTapsSlideTheSpark(t *testing.T) {
	app, g := startGame(t)
	app.Press("level-1")
	app.Advance(stepTime)

	start := g.board().Spark
	at := layoutFor(g.viewSize, g.board()).center(start)
	finger := app.Touch("board", at)
	app.Advance(stepTime)
	finger.Move(core.Point{X: at.X + 120, Y: at.Y})
	app.Advance(stepTime)
	finger.Lift()
	app.Advance(settleTime)
	if b := g.board(); b.Moves != 1 || b.Spark.Y != start.Y || b.Spark.X <= start.X {
		t.Fatalf("a swipe right left the spark at %v after %d moves, from %v", b.Spark, b.Moves, start)
	}

	// The spark is in the top right corner: a tap below it slides it down.
	b := g.board()
	below := layoutFor(g.viewSize, b).center(Cell{b.Spark.X, b.Height - 1})
	finger = app.Touch("board", below)
	app.Advance(stepTime)
	finger.Lift()
	app.Advance(settleTime)
	if b.Moves != 2 || b.Spark.X != 5 || b.Spark.Y <= 1 {
		t.Fatalf("a tap below the spark left it at %v after %d moves", b.Spark, b.Moves)
	}
}

// TestMouseClickSlidesTheSpark clicks the board to the right of the spark.
func TestMouseClickSlidesTheSpark(t *testing.T) {
	app, g := startGame(t)
	app.Press("level-1")
	app.Advance(stepTime)
	b := g.board()
	app.PressAt("board", layoutFor(g.viewSize, b).center(Cell{b.Width - 1, b.Spark.Y}))
	app.Advance(settleTime)
	if b.Moves != 1 || b.Spark != (Cell{5, 1}) {
		t.Fatalf("a click right of the spark left it at %v after %d moves", b.Spark, b.Moves)
	}
}

// TestLockedLevelsDoNotStart checks that the level select's highlight moves with the keys but a locked level does not start.
func TestLockedLevelsDoNotStart(t *testing.T) {
	app, g := startGame(t)
	key(app, "down")
	if g.cursor.Get() != levelColumns {
		t.Fatalf("down highlighted level %d, want %d", g.cursor.Get()+1, levelColumns+1)
	}
	key(app, "enter")
	if g.screen.Get() != screenLevels {
		t.Fatal("a locked level started")
	}
	key(app, "up")
	key(app, "left")
	if g.cursor.Get() != 0 {
		t.Fatalf("the highlight is on level %d, want 1", g.cursor.Get()+1)
	}
}

// TestEveryLevelIsWonInPlay plays every level in order at par with the keyboard, each win unlocking the next, and checks that the last win's result leads back to the level select.
func TestEveryLevelIsWonInPlay(t *testing.T) {
	app, g := startGame(t)
	key(app, "enter")
	for i := range Levels.Levels {
		l := &Levels.Levels[i]
		expectText(t, app, "level-name", strconv.Itoa(i+1)+". "+l.Name)
		_, path := solve(NewBoard(l))
		for _, d := range path {
			key(app, keyFor[d])
		}
		expectText(t, app, "result-stars", "★★★")
		key(app, "enter")
	}
	if g.screen.Get() != screenLevels {
		t.Fatal("the last level's win did not lead back to the level select")
	}
	for i := range Levels.Levels {
		expectText(t, app, "level-"+strconv.Itoa(i+1)+"-stars", "★★★")
	}
}

// TestKeyboardAndAssistivePressesOnTheBoardMakeNoMove checks, on a desktop, that Tab never stops on the board, so Space and Enter cannot press it, and that a screen reader's activation of the board, which lands at its origin, slides nothing.
func TestKeyboardAndAssistivePressesOnTheBoardMakeNoMove(t *testing.T) {
	app, g := startGame(t, nsgotest.Environment(core.Environment{OS: core.OSMacOS}))
	app.Press("level-1")
	app.Advance(stepTime)
	for range 6 {
		app.Key("tab")
		if app.Find("board").Focused {
			t.Fatal("Tab stopped on the board")
		}
	}
	app.AccessibilityAction("board", core.ActionActivate)
	app.Advance(settleTime)
	if b := g.board(); b.Moves != 0 {
		t.Fatalf("activating the board made %d moves, leaving the spark at %v", b.Moves, b.Spark)
	}
}

// TestPressesBesideTheBoardMakeNoMove presses the counters above the board and the gap beside the buttons below it, which let presses through to the board's view.
func TestPressesBesideTheBoardMakeNoMove(t *testing.T) {
	app, g := startGame(t)
	app.Press("level-1")
	app.Advance(stepTime)
	w, h := g.viewSize.Width, g.viewSize.Height
	for _, at := range []core.Point{{X: w / 2, Y: 20}, {X: 8, Y: h - 30}, {X: w - 8, Y: h - 30}} {
		app.PressAt("board", at)
		app.Advance(settleTime)
		if b := g.board(); b.Moves != 0 {
			t.Fatalf("a press at %v made %d moves, leaving the spark at %v", at, b.Moves, b.Spark)
		}
	}
}

// TestSavedStarsOutOfRangeAreDropped starts the game on a save holding more stars than a win earns and fewer than none, as a hand-edited save or a later version may, and checks that the level select shows and a real win still saves.
func TestSavedStarsOutOfRangeAreDropped(t *testing.T) {
	app, g := startGame(t)
	if err := g.store.Save(Progress{Stars: map[string]int{Levels.Levels[0].ID: 5, Levels.Levels[1].ID: -1}}); err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	again, g2 := startGame(t, nsgotest.Storage(kept))
	expectText(t, again, "level-1-stars", "☆☆☆")
	expectText(t, again, "level-2-stars", "Locked")
	if !g2.progress.Record(Levels.Levels[0].ID, 3) {
		t.Fatal("a 3-star win did not beat the dropped saved stars")
	}
	if got := starText(5) + starText(-1); got != "★★★☆☆☆" {
		t.Fatalf("starText(5) + starText(-1) = %q", got)
	}
}
