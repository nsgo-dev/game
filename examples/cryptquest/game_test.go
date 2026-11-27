package main

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func startTestGame(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *Game) {
	t.Helper()
	var g *Game
	baseOpts := []nsgotest.Option{nsgotest.Size(800, 560)}
	baseOpts = append(baseOpts, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store := save.NewStorageStore(cx.Storage(), "cryptquest", save.WithVersion(SaveSchemaVersion))
		g = NewGame(store)
		return RenderGame(cx, g)
	}, baseOpts...)
	return app, g
}

func TestEnemyPathfindsAroundWallsWithAStar(t *testing.T) {
	app, g := startTestGame(t)

	// Place the player in Entry Hall at (6, 2), separated from Bone Warden
	// at (9, 2) by the solid crypt wall along column 7 (rows 1..3).
	// Distance is 3 tiles (within senseRadius), so Bone Warden must alert,
	// switch to EnemyChase, and compute a wall-avoiding A* path down through
	// the doorway at (7, 4) and back up to (6, 2).
	targetCell := ai.Cell{Col: 6, Row: 2}
	targetPos := g.World.CellCenter(targetCell)
	app.Post(func() {
		g.SetPlayerPos(float32(targetPos.X), float32(targetPos.Y))
	})

	warden := g.Enemies[0]
	if warden.ID != "bone-warden" {
		t.Fatalf("expected first enemy bone-warden, got %q", warden.ID)
	}
	initialCap := cap(warden.Path)
	initialSmoothCap := cap(warden.SmoothPath)

	// Advance a few frames so flow.Machine transitions Patrol -> Chase and plans A* path.
	app.Advance(100 * time.Millisecond)

	if warden.State.Current() != EnemyChase {
		t.Fatalf("enemy state = %q, want %q", warden.State.Current(), EnemyChase)
	}
	if !warden.Alerted {
		t.Fatal("expected chasing enemy to have Alerted=true")
	}
	if len(warden.Path) < 4 {
		t.Fatalf("expected multi-step A* path around wall, got len=%d (%v)", len(warden.Path), warden.Path)
	}
	if len(warden.SmoothPath) < 2 || len(warden.SmoothPath) >= len(warden.Path) {
		t.Fatalf("expected SmoothGridPathInto to drop intermediate cells: smooth=%d raw=%d (%v)", len(warden.SmoothPath), len(warden.Path), warden.SmoothPath)
	}
	for i := 0; i+1 < len(warden.SmoothPath); i++ {
		if !ai.GridLineOfSight(g.World.Nav, warden.SmoothPath[i], warden.SmoothPath[i+1], enemyPathOptions) {
			t.Fatalf("smoothed segment %d (%v -> %v) violated clearance radius %.2f", i, warden.SmoothPath[i], warden.SmoothPath[i+1], enemyPathRadius)
		}
	}

	passedDoorway := false
	for _, c := range warden.Path {
		if !g.World.Nav.Walkable(c.Col, c.Row) {
			t.Fatalf("A* path included solid cell %+v", c)
		}
		if c.Col == 7 && c.Row == 4 {
			passedDoorway = true
		}
	}
	if !passedDoorway {
		t.Fatalf("expected A* path to route around wall via doorway (7,4), got %v", warden.Path)
	}
	if warden.Path[len(warden.Path)-1] != targetCell {
		t.Fatalf("A* path goal = %+v, want %+v", warden.Path[len(warden.Path)-1], targetCell)
	}
	if warden.SmoothPath[len(warden.SmoothPath)-1] != targetPos {
		t.Fatalf("smoothed path goal = %+v, want %+v", warden.SmoothPath[len(warden.SmoothPath)-1], targetPos)
	}

	startPos := warden.Center()
	// Advance simulation so the skeleton actively navigates around the wall.
	app.Advance(1800 * time.Millisecond)
	movedPos := warden.Center()
	if distPoints(startPos, movedPos) < 24 {
		t.Fatalf("expected chasing enemy to move substantially along A* path: start=%+v end=%+v", startPos, movedPos)
	}
	if cap(warden.Path) != initialCap {
		t.Fatalf("expected A* FindInto to reuse path buffer capacity %d, got %d", initialCap, cap(warden.Path))
	}
	if cap(warden.SmoothPath) != initialSmoothCap {
		t.Fatalf("expected SmoothGridPathInto to reuse smoothed path buffer capacity %d, got %d", initialSmoothCap, cap(warden.SmoothPath))
	}
}

func TestElderNPCDialogueMultiPageTypewriterAndClose(t *testing.T) {
	app, g := startTestGame(t)

	// Move player directly adjacent to Elder Aldric at (3, 3).
	nearElder := g.World.CellCenter(ai.Cell{Col: 3, Row: 3})
	app.Post(func() {
		g.SetPlayerPos(float32(nearElder.X), float32(nearElder.Y))
	})
	app.Advance(50 * time.Millisecond)

	if app.Exists("id:dialogue-controls") {
		t.Fatal("dialogue controls should not be visible before interacting with Elder")
	}

	// Interact with Elder via native button.
	app.Press("id:btn-interact")

	if !g.Dialogue.Open || !g.TalkedToElder {
		t.Fatalf("expected dialogue open and TalkedToElder=true, got open=%v talked=%v", g.Dialogue.Open, g.TalkedToElder)
	}
	if got := app.Find("id:dialogue-page").Text; got != "Elder Aldric — Page 1 of 3" {
		t.Fatalf("page label = %q, want %q", got, "Elder Aldric — Page 1 of 3")
	}

	initialRevealed := g.Dialogue.RevealedChars
	// Advance frames so typewriter reveals characters and text.DrawPlatform draws them.
	app.Advance(800 * time.Millisecond)
	if g.Dialogue.RevealedChars <= initialRevealed {
		t.Fatalf("expected typewriter reveal to advance: before=%d after=%d", initialRevealed, g.Dialogue.RevealedChars)
	}

	// Verify canvas recorded DrawFillText operations for speaker, page counter, and body.
	ops := app.Find("id:game-canvas").Ops
	var drawnTexts []string
	for _, op := range ops {
		if op.Kind == core.DrawFillText {
			drawnTexts = append(drawnTexts, op.Text)
		}
	}
	joined := strings.Join(drawnTexts, " | ")
	if !strings.Contains(joined, "Elder Aldric") || !strings.Contains(joined, "Page 1/3") || !strings.Contains(joined, "Welcome") {
		t.Fatalf("expected canvas DrawFillText ops to contain dialogue box text, got: %s", joined)
	}

	// Advance through pages 2 and 3, then close cleanly after page 3.
	app.Press("id:dialogue-advance")
	if got := app.Find("id:dialogue-page").Text; got != "Elder Aldric — Page 2 of 3" {
		t.Fatalf("after 1st Next, page label = %q, want Page 2 of 3", got)
	}

	app.Press("id:dialogue-advance")
	if got := app.Find("id:dialogue-page").Text; got != "Elder Aldric — Page 3 of 3" {
		t.Fatalf("after 2nd Next, page label = %q, want Page 3 of 3", got)
	}

	app.Press("id:dialogue-advance")
	if g.Dialogue.Open || app.Exists("id:dialogue-controls") {
		t.Fatal("expected dialogue to close cleanly after advancing past the final page")
	}

	// Re-open and test immediate Close button.
	app.Press("id:btn-interact")
	if !app.Exists("id:dialogue-controls") {
		t.Fatal("expected dialogue to re-open on Interact")
	}
	app.Press("id:dialogue-close")
	if g.Dialogue.Open || app.Exists("id:dialogue-controls") {
		t.Fatal("expected Close button to dismiss dialogue cleanly")
	}
}

func TestSaveAndLoadSurvivesAppRestart(t *testing.T) {
	app1, g1 := startTestGame(t)

	if app1.Enabled("id:btn-load") {
		t.Fatal("Load button should be disabled when no save exists")
	}

	// Visit Bone Corridor, banish Bone Warden, then visit Relic Chamber and claim the Sun Relic.
	wardenPos := g1.Enemies[0].Center()
	app1.Post(func() {
		g1.SetPlayerPos(float32(wardenPos.X), float32(wardenPos.Y)+12)
	})
	app1.Press("id:btn-attack")
	if !g1.Enemies[0].Defeated || g1.Enemies[0].State.Current() != EnemyDefeated {
		t.Fatalf("expected Bone Warden to be defeated after Banish, state=%q defeated=%v", g1.Enemies[0].State.Current(), g1.Enemies[0].Defeated)
	}

	chestPos := g1.World.ChestPos
	app1.Post(func() {
		g1.SetPlayerPos(float32(chestPos.X), float32(chestPos.Y)+10)
	})
	app1.Press("id:btn-interact")

	if !g1.HasRelic || !g1.DoorUnlocked {
		t.Fatalf("expected relic collected and door unlocked, got hasRelic=%v doorUnlocked=%v", g1.HasRelic, g1.DoorUnlocked)
	}
	if !g1.World.Nav.Walkable(g1.World.DoorCell.Col, g1.World.DoorCell.Row) {
		t.Fatal("expected sanctum door cell to be walkable after collecting relic")
	}
	if got := app1.Find("id:hud-relic").Text; got != "Relic: Sun Relic" {
		t.Fatalf("hud-relic = %q, want %q", got, "Relic: Sun Relic")
	}

	savedPos := g1.PlayerCenter()
	savedRooms := make([]string, 0, len(g1.VisitedRooms))
	for room := range g1.VisitedRooms {
		savedRooms = append(savedRooms, room)
	}
	slices.Sort(savedRooms)

	// Save via native Save button.
	app1.Press("id:btn-save")
	if !app1.Enabled("id:btn-load") {
		t.Fatal("Load button should become enabled after Save")
	}

	// Snapshot core.Storage to simulate a full app restart.
	kept := make(map[string]string)
	st := app1.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}
	if len(kept) == 0 {
		t.Fatal("expected save.Store to write entries to core.Storage")
	}

	// Boot a fresh app instance backed by the persisted storage snapshot.
	app2, g2 := startTestGame(t, nsgotest.Storage(kept))
	if g2.HasRelic || g2.DoorUnlocked {
		t.Fatal("fresh app instance should begin without relic before loading save")
	}
	if !app2.Enabled("id:btn-load") {
		t.Fatal("Load button should be enabled on restart when save exists in storage")
	}

	// Restore progress via native Load button.
	app2.Press("id:btn-load")

	restoredPos := g2.PlayerCenter()
	if math.Abs(float64(restoredPos.X-savedPos.X)) > 0.01 || math.Abs(float64(restoredPos.Y-savedPos.Y)) > 0.01 {
		t.Fatalf("restored player pos = %+v, want %+v", restoredPos, savedPos)
	}
	if !g2.HasRelic || !g2.DoorUnlocked {
		t.Fatalf("restored quest flags hasRelic=%v doorUnlocked=%v, want true/true", g2.HasRelic, g2.DoorUnlocked)
	}
	if !g2.World.Nav.Walkable(g2.World.DoorCell.Col, g2.World.DoorCell.Row) {
		t.Fatal("expected restored unlocked door to be walkable in navgrid")
	}
	if !g2.Enemies[0].Defeated || g2.Enemies[0].State.Current() != EnemyDefeated {
		t.Fatalf("expected Bone Warden defeated state to persist across restart, got state=%q defeated=%v", g2.Enemies[0].State.Current(), g2.Enemies[0].Defeated)
	}

	restoredRooms := make([]string, 0, len(g2.VisitedRooms))
	for room := range g2.VisitedRooms {
		restoredRooms = append(restoredRooms, room)
	}
	slices.Sort(restoredRooms)
	if !slices.Equal(restoredRooms, savedRooms) {
		t.Fatalf("restored visited rooms = %v, want %v", restoredRooms, savedRooms)
	}
	if got := app2.Find("id:hud-relic").Text; got != "Relic: Sun Relic" {
		t.Fatalf("restored hud-relic = %q, want %q", got, "Relic: Sun Relic")
	}

	// Step into the unlocked Exit Sanctum portal to verify quest completion.
	exitPos := g2.World.ExitPos
	app2.Post(func() {
		g2.SetPlayerPos(float32(exitPos.X), float32(exitPos.Y))
	})
	app2.Advance(50 * time.Millisecond)
	if !g2.QuestCompleted {
		t.Fatal("expected entering unlocked Exit Sanctum portal to complete the quest")
	}
}

func TestFullPlayableLoopRendersExpectedCanvasDrawOps(t *testing.T) {
	app := nsgotest.Start(
		t,
		Root,
		nsgotest.Size(800, 560),
		nsgotest.Environment(core.Environment{OS: core.OSMacOS}),
	)

	// Drive keyboard movement via View.OnKeyDown and fixed-step 60 Hz frames.
	app.Focus("id:stage")
	app.Key("d")
	app.Advance(250 * time.Millisecond)

	canvas := app.Find("id:game-canvas")
	if len(canvas.Ops) == 0 {
		t.Fatal("expected game-canvas to record core.DrawOp operations")
	}

	counts := make(map[core.DrawKind]int)
	for _, op := range canvas.Ops {
		counts[op.Kind]++
	}

	if n := len(gametest.ImageDraws(canvas.Ops)); n < 20 {
		t.Fatalf("expected tilemap + sprite image draws (>=20), got %d", n)
	}
	if counts[core.DrawFillText] < 1 {
		t.Fatalf("expected DrawFillText ops from text.DrawPlatform, got %d", counts[core.DrawFillText])
	}
	if counts[core.DrawTranslate] == 0 || counts[core.DrawScale] == 0 {
		t.Fatalf("expected viewport/camera transforms (translate=%d scale=%d)", counts[core.DrawTranslate], counts[core.DrawScale])
	}
	if counts[core.DrawRoundRect] == 0 {
		t.Fatalf("expected DrawRoundRect ops for badges/pads, got %d", counts[core.DrawRoundRect])
	}

	// Verify native HUD elements are mounted and populated.
	if got := app.Find("id:hud-hp").Text; got != "HP: 5/5" {
		t.Fatalf("hud-hp = %q, want %q", got, "HP: 5/5")
	}
	if got := app.Find("id:hud-room").Text; !strings.Contains(got, "Entry Hall") {
		t.Fatalf("hud-room = %q, expected Entry Hall", got)
	}
}

func controlFrame(t *testing.T, g *Game, id string) core.Bounds {
	t.Helper()
	for _, c := range g.Touch.Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("no touch control %q", id)
	return core.Bounds{}
}

func centerOf(f core.Bounds) core.Point {
	return core.Point{X: f.Origin.X + f.Size.Width/2, Y: f.Origin.Y + f.Size.Height/2}
}

func overlaps(a, b core.Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width && b.Origin.X < a.Origin.X+a.Size.Width &&
		a.Origin.Y < b.Origin.Y+b.Size.Height && b.Origin.Y < a.Origin.Y+a.Size.Height
}

func TestTouchStickMovesCardinallyAndDiagonallyWithOneThumb(t *testing.T) {
	app, g := startTestGame(t, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone}))
	app.Advance(50 * time.Millisecond)

	// Start in an open area of Entry Hall (col 3, row 4) with room to move in every direction.
	open := g.World.CellCenter(ai.Cell{Col: 3, Row: 4})
	app.Post(func() {
		g.SetPlayerPos(float32(open.X), float32(open.Y))
	})
	app.Advance(50 * time.Millisecond)

	stickCenter := centerOf(controlFrame(t, g, "move"))
	startPos := g.PlayerCenter()

	// Touch down on the floating stick, then drag right.
	finger := app.Touch("id:stage", stickCenter)
	finger.Move(core.Point{X: stickCenter.X + 48, Y: stickCenter.Y})
	app.Advance(250 * time.Millisecond)
	afterRight := g.PlayerCenter()
	if afterRight.X <= startPos.X+10 {
		t.Fatalf("dragging stick right did not move hero right: start=%+v after=%+v", startPos, afterRight)
	}

	// Keep the same thumb down and drag diagonally up-right (screen Y decreases -> hero moves up and right).
	finger.Move(core.Point{X: stickCenter.X + 40, Y: stickCenter.Y - 40})
	app.Advance(250 * time.Millisecond)
	afterUpRight := g.PlayerCenter()
	if afterUpRight.X <= afterRight.X+6 || afterUpRight.Y >= afterRight.Y-6 {
		t.Fatalf("dragging stick up-right did not move hero diagonally up-right: before=%+v after=%+v", afterRight, afterUpRight)
	}

	// Drag diagonally down-left with the same thumb (screen Y increases -> hero moves down and left).
	finger.Move(core.Point{X: stickCenter.X - 40, Y: stickCenter.Y + 40})
	app.Advance(250 * time.Millisecond)
	afterDownLeft := g.PlayerCenter()
	if afterDownLeft.X >= afterUpRight.X-6 || afterDownLeft.Y <= afterUpRight.Y+6 {
		t.Fatalf("dragging stick down-left did not move hero diagonally down-left: before=%+v after=%+v", afterUpRight, afterDownLeft)
	}

	// Lifting the thumb stops the hero.
	finger.Lift()
	app.Advance(100 * time.Millisecond)
	stopped := g.PlayerCenter()
	app.Advance(200 * time.Millisecond)
	if end := g.PlayerCenter(); end != stopped {
		t.Fatalf("hero kept moving after stick release: %+v -> %+v", stopped, end)
	}
}

func TestTouchButtonsAttackInteractAndHideDuringDialogue(t *testing.T) {
	app, g := startTestGame(t, nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone}))
	app.Advance(50 * time.Millisecond)

	if !app.Exists("id:touch-move") || !app.Exists("id:touch-interact") || !app.Exists("id:touch-attack") {
		t.Fatal("expected touch controls to be visible on a phone")
	}

	// Move beside Bone Warden and banish it with the touch ATTACK button while also moving the stick with a second finger.
	wardenPos := g.Enemies[0].Center()
	app.Post(func() {
		g.SetPlayerPos(float32(wardenPos.X), float32(wardenPos.Y)+12)
	})
	app.Advance(50 * time.Millisecond)

	stageOrigin := app.Find("id:stage-container").Frame.Origin
	toPage := func(local core.Point) core.Point {
		return core.Point{X: stageOrigin.X + local.X, Y: stageOrigin.Y + local.Y}
	}
	stickBase := toPage(centerOf(controlFrame(t, g, "move")))
	attackPt := toPage(centerOf(controlFrame(t, g, "attack")))

	startX := g.PlayerCenter().X
	app.Report(nsgotest.Event{
		Kind:    nsgotest.EventTouch,
		Phase:   core.GestureBegan,
		Touches: []core.Touch{{ID: 1, PagePosition: stickBase, Changed: true}},
	})
	app.Report(nsgotest.Event{
		Kind:  nsgotest.EventTouch,
		Phase: core.GestureChanged,
		Touches: []core.Touch{
			{ID: 1, PagePosition: core.Point{X: stickBase.X + 40, Y: stickBase.Y}, Changed: true},
		},
	})
	app.Report(nsgotest.Event{
		Kind:  nsgotest.EventTouch,
		Phase: core.GestureBegan,
		Touches: []core.Touch{
			{ID: 1, PagePosition: core.Point{X: stickBase.X + 40, Y: stickBase.Y}},
			{ID: 2, PagePosition: attackPt, Changed: true},
		},
	})
	app.Advance(120 * time.Millisecond)
	app.Report(nsgotest.Event{
		Kind:  nsgotest.EventTouch,
		Phase: core.GestureEnded,
		Touches: []core.Touch{
			{ID: 1, PagePosition: core.Point{X: stickBase.X + 40, Y: stickBase.Y}},
			{ID: 2, PagePosition: attackPt, Changed: true},
		},
	})
	app.Report(nsgotest.Event{
		Kind:    nsgotest.EventTouch,
		Phase:   core.GestureEnded,
		Touches: []core.Touch{{ID: 1, PagePosition: core.Point{X: stickBase.X + 40, Y: stickBase.Y}, Changed: true}},
	})
	app.Advance(50 * time.Millisecond)

	if !g.Enemies[0].Defeated || g.Enemies[0].State.Current() != EnemyDefeated {
		t.Fatalf("touch ATTACK did not defeat Bone Warden: state=%q defeated=%v", g.Enemies[0].State.Current(), g.Enemies[0].Defeated)
	}
	if gotX := g.PlayerCenter().X; gotX <= startX+2 {
		t.Fatalf("simultaneous stick drag did not move hero right while attacking: startX=%.1f endX=%.1f", startX, gotX)
	}

	// Move beside Elder Aldric and tap the touch USE button to open dialogue; touch controls must hide while dialogue is open.
	nearElder := g.World.CellCenter(ai.Cell{Col: 3, Row: 3})
	app.Post(func() {
		g.SetPlayerPos(float32(nearElder.X), float32(nearElder.Y))
	})
	app.Advance(50 * time.Millisecond)

	useFinger := app.Touch("id:stage", centerOf(controlFrame(t, g, "interact")))
	app.Advance(50 * time.Millisecond)
	useFinger.Lift()
	app.Advance(50 * time.Millisecond)

	if !g.Dialogue.Open {
		t.Fatal("touch USE button did not open Elder Aldric dialogue")
	}
	if app.Exists("id:touch-move") || app.Exists("id:touch-interact") || app.Exists("id:touch-attack") {
		t.Fatal("touch controls remained visible while dialogue was open")
	}

	// Closing the dialogue brings the touch controls back at full opacity.
	app.Press("id:dialogue-close")
	app.Advance(50 * time.Millisecond)
	if !app.Exists("id:touch-move") || !app.Exists("id:touch-attack") {
		t.Fatal("touch controls did not return after closing dialogue")
	}
	if got := g.Touch.Alpha(); got != 1 {
		t.Fatalf("touch controls alpha after dialogue close = %v, want 1", got)
	}
}

func TestTouchControlsHiddenOnDesktopAndMouseClickDoesNotMoveHero(t *testing.T) {
	app, g := startTestGame(t, nsgotest.Idiom(core.IdiomDesktop))
	app.Advance(50 * time.Millisecond)

	if app.Exists("id:touch-move") || app.Exists("id:touch-interact") || app.Exists("id:touch-attack") {
		t.Fatal("touch controls should start hidden on desktop")
	}

	// Clicking the stage with a mouse (including where the old virtual D-pad sat and where the stick sits) must not move the hero or show touch controls.
	startPos := g.PlayerCenter()
	stageFrame := app.Find("id:stage").Frame
	oldRightPad := g.Viewport.GameToView(stageFrame.Size, core.Point{X: 55, Y: 168})
	stickPt := centerOf(controlFrame(t, g, "move"))
	for _, pt := range []core.Point{oldRightPad, stickPt, {X: stickPt.X + 40, Y: stickPt.Y}} {
		app.MouseDownAt("id:stage", pt, core.MouseButtonLeft)
		app.Advance(150 * time.Millisecond)
		app.MouseUpAt("id:stage", pt, core.MouseButtonLeft)
	}
	if pos := g.PlayerCenter(); pos != startPos {
		t.Fatalf("mouse clicks on stage moved the hero: start=%+v end=%+v", startPos, pos)
	}
	if app.Exists("id:touch-move") {
		t.Fatal("mouse clicks on stage showed touch controls on desktop")
	}

	// Touching the screen shows the controls, and pressing a key hides them again.
	finger := app.Touch("id:stage", centerOf(controlFrame(t, g, "attack")))
	app.Advance(50 * time.Millisecond)
	finger.Lift()
	if !app.Exists("id:touch-move") || !app.Exists("id:touch-attack") {
		t.Fatal("a touch did not show the touch controls on desktop")
	}

	app.Key("d")
	app.Advance(50 * time.Millisecond)
	if app.Exists("id:touch-move") || app.Exists("id:touch-attack") {
		t.Fatal("keyboard input did not hide the touch controls on desktop")
	}
}

func TestTouchControlsPhoneSafeAreaSizesAndHUDLayout(t *testing.T) {
	cases := []struct {
		name string
		w, h core.Points
		safe core.Edges
	}{
		{
			name: "landscape_iphone",
			w:    844,
			h:    390,
			safe: core.Edges{Left: 47, Right: 47, Bottom: 21},
		},
		{
			name: "portrait_iphone",
			w:    390,
			h:    844,
			safe: core.Edges{Top: 59, Bottom: 34},
		},
		{
			name: "narrow_portrait_phone",
			w:    360,
			h:    640,
			safe: core.Edges{Top: 44, Bottom: 24},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, g := startTestGame(
				t,
				nsgotest.Size(tc.w, tc.h),
				nsgotest.Environment(core.Environment{Idiom: core.IdiomPhone, SafeArea: tc.safe}),
			)
			app.Advance(50 * time.Millisecond)

			hudFrame := app.Find("id:hud-bar").Frame
			statusFrame := app.Find("id:status-bar").Frame
			stageFrame := app.Find("id:stage-container").Frame

			if stageFrame.Size.Width <= 0 || stageFrame.Size.Height <= 0 {
				t.Fatalf("invalid stage frame %+v", stageFrame)
			}
			if hudFrame.Size.Width > tc.w || statusFrame.Origin.Y+statusFrame.Size.Height > tc.h {
				t.Fatalf("chrome exceeds window %vx%v: hud=%+v status=%+v", tc.w, tc.h, hudFrame, statusFrame)
			}
			if overlaps(hudFrame, stageFrame) {
				t.Fatalf("HUD bar %+v overlaps stage %+v", hudFrame, stageFrame)
			}

			ids := []string{"move", "interact", "attack"}
			winFrames := make(map[string]core.Bounds, len(ids))
			for _, id := range ids {
				if !app.Exists("id:touch-" + id) {
					t.Fatalf("touch control %q not visible", id)
				}
				f := controlFrame(t, g, id)
				if f.Size.Width < 44 || f.Size.Height < 44 {
					t.Fatalf("control %q smaller than 44pt minimum: %+v", id, f.Size)
				}
				wf := core.Bounds{
					Origin: core.Point{X: stageFrame.Origin.X + f.Origin.X, Y: stageFrame.Origin.Y + f.Origin.Y},
					Size:   f.Size,
				}
				winFrames[id] = wf
				if wf.Origin.X < tc.safe.Left ||
					wf.Origin.Y < tc.safe.Top ||
					wf.Origin.X+wf.Size.Width > tc.w-tc.safe.Right ||
					wf.Origin.Y+wf.Size.Height > tc.h-tc.safe.Bottom {
					t.Fatalf("control %q frame %+v falls outside safe area %+v of %vx%v", id, wf, tc.safe, tc.w, tc.h)
				}
				if overlaps(wf, hudFrame) {
					t.Fatalf("control %q %+v overlaps HUD bar %+v", id, wf, hudFrame)
				}
				if overlaps(wf, statusFrame) {
					t.Fatalf("control %q %+v overlaps status bar %+v", id, wf, statusFrame)
				}
			}

			if winFrames["attack"].Size.Width <= winFrames["interact"].Size.Width {
				t.Fatalf("ATTACK button (%+v) should be larger than USE button (%+v)", winFrames["attack"].Size, winFrames["interact"].Size)
			}
			for i := 0; i < len(ids); i++ {
				for j := i + 1; j < len(ids); j++ {
					a, b := ids[i], ids[j]
					if overlaps(winFrames[a], winFrames[b]) {
						t.Fatalf("controls %q %+v and %q %+v overlap", a, winFrames[a], b, winFrames[b])
					}
				}
			}
		})
	}
}

func TestNavMeshOverlayFollowsTheDoor(t *testing.T) {
	_, g := startTestGame(t)
	w := g.World
	check := func(doorOpen bool) {
		t.Helper()
		if w.NavMesh == nil {
			t.Fatal("the crypt has no navigation mesh")
		}
		nav := w.NavMesh.NavMesh()
		for row := range MapRows {
			for col := range MapCols {
				want := w.Map.FlagsAt(col, row)&tilemap.TileSolid == 0
				if got := nav.Contains(cellCenter(ai.Cell{Col: col, Row: row})); got != want {
					t.Fatalf("door open %v: cell (%d, %d) on the mesh = %v, want %v", doorOpen, col, row, got, want)
				}
			}
		}
		_, ok := nav.FindPath(w.PlayerSpawn, w.ExitPos)
		if ok != doorOpen {
			t.Fatalf("door open %v: path to the exit found = %v", doorOpen, ok)
		}
	}
	check(false)
	w.UnlockDoor()
	check(true)
	w.LockDoor()
	check(false)

	if !g.HandleKey("n") || !g.ShowNavMesh {
		t.Fatal("N did not turn the navigation mesh overlay on")
	}
	var d core.Drawing
	g.Draw(&d, core.Size{Width: 640, Height: 400})
	fills := 0
	for _, op := range d.Ops {
		if op.Kind == core.DrawFill {
			fills++
		}
	}
	g.HandleKey("n")
	var plain core.Drawing
	g.Draw(&plain, core.Size{Width: 640, Height: 400})
	plainFills := 0
	for _, op := range plain.Ops {
		if op.Kind == core.DrawFill {
			plainFills++
		}
	}
	if fills != plainFills+1 {
		t.Fatalf("overlay drew %d fills, %d without it; want one more", fills, plainFills)
	}
}
