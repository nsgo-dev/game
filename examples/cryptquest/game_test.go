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

func TestTouchDPadMovesPlayerThroughInputActions(t *testing.T) {
	app, g := startTestGame(t)
	app.Advance(50 * time.Millisecond)

	startPos := g.PlayerCenter()
	stageFrame := app.Find("id:stage").Frame
	// Map the center of the virtual right D-pad button into stage view coordinates.
	padCenterVirtual := core.Point{
		X: dpadRightRect.Origin.X + dpadRightRect.Size.Width/2,
		Y: dpadRightRect.Origin.Y + dpadRightRect.Size.Height/2,
	}
	viewPt := g.Viewport.GameToView(stageFrame.Size, padCenterVirtual)

	finger := app.Touch("id:stage", viewPt)
	app.Advance(300 * time.Millisecond)
	finger.Lift()

	endPos := g.PlayerCenter()
	if endPos.X <= startPos.X+10 {
		t.Fatalf("expected holding touch D-pad right to move player right: start=%+v end=%+v", startPos, endPos)
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
