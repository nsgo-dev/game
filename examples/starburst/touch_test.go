package main

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const testFrame = time.Second / StepRate

// multiTouch reports multi-finger touch events directly to the app surface, matching platform delivery when two thumbs are down simultaneously.
type multiTouch struct {
	app     *nsgotest.App
	fingers map[int]core.Point
}

func (m *multiTouch) report(phase core.GesturePhase, id int, at core.Point) {
	touches := []core.Touch{{ID: id, PagePosition: at, Changed: true}}
	for other, p := range m.fingers {
		if other != id {
			touches = append(touches, core.Touch{ID: other, PagePosition: p})
		}
	}
	m.app.Report(nsgotest.Event{Kind: nsgotest.EventTouch, Phase: phase, Touches: touches})
}

func (m *multiTouch) down(id int, at core.Point) {
	if m.fingers == nil {
		m.fingers = map[int]core.Point{}
	}
	m.report(core.GestureBegan, id, at)
	m.fingers[id] = at
}

func (m *multiTouch) moveTo(id int, at core.Point) {
	m.fingers[id] = at
	m.report(core.GestureChanged, id, at)
}

func (m *multiTouch) up(id int) {
	at, ok := m.fingers[id]
	if !ok {
		return
	}
	m.report(core.GestureEnded, id, at)
	delete(m.fingers, id)
}

func touchControlFrame(t testing.TB, gc *GameController, id string) core.Bounds {
	t.Helper()
	for _, c := range gc.Touch.Controls() {
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

func boundsOverlap(a, b core.Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width && b.Origin.X < a.Origin.X+a.Size.Width &&
		a.Origin.Y < b.Origin.Y+b.Size.Height && b.Origin.Y < a.Origin.Y+a.Size.Height
}

func angleDiff(a, b float32) float32 {
	d := float32(math.Remainder(float64(a-b), 2*math.Pi))
	if d < 0 {
		return -d
	}
	return d
}

func TestTouchTwinStickMoveAimFireAndNova(t *testing.T) {
	phoneEnv := core.Environment{
		Idiom:    core.IdiomPhone,
		Pointer:  core.PointerCoarse,
		SafeArea: core.Edges{Left: 47, Right: 47, Bottom: 21},
	}
	app, gc := startStarburst(t, nsgotest.Size(844, 390), nsgotest.Environment(phoneEnv))

	// On a coarse-pointer phone, all three controls start visible as canvas nodes.
	for _, id := range []string{"touch-move", "touch-aim", "touch-nova"} {
		if !app.Exists("id:" + id) {
			t.Fatalf("expected %s to start visible on a phone", id)
		}
	}

	// Clear enemies so firing is driven solely by the aim stick rather than auto-fire on enemies.
	gc.Arena.enemies = gc.Arena.enemies[:0]
	gc.Arena.waveRemaining = 0
	gc.Arena.bullets = gc.Arena.bullets[:0]
	gc.Arena.Player.FireCooldown = 0
	gc.Arena.Player.NovaCooldown = 0

	moveOrigin := centerOf(touchControlFrame(t, gc, "move"))
	aimOrigin := centerOf(touchControlFrame(t, gc, "aim"))
	novaCenter := centerOf(touchControlFrame(t, gc, "nova"))
	mt := &multiTouch{app: app}

	// 1. Inside the aim stick dead zone, AimStickX/Y stay 0 and Fire stays false.
	mt.down(2, aimOrigin)
	mt.moveTo(2, core.Point{X: aimOrigin.X + 3, Y: aimOrigin.Y - 2})
	app.Advance(2 * testFrame)
	if gc.Arena.Control.AimStickX != 0 || gc.Arena.Control.AimStickY != 0 {
		t.Fatalf("inside dead zone got AimStick=(%v, %v), want (0, 0)", gc.Arena.Control.AimStickX, gc.Arena.Control.AimStickY)
	}
	if gc.Arena.Control.Fire {
		t.Fatal("expected Fire=false while aim stick is inside its dead zone")
	}

	// 2. Two thumbs at once: move stick dragged diagonally up-right, aim stick dragged straight up.
	startX, startY := gc.Arena.Player.X, gc.Arena.Player.Y
	mt.down(1, moveOrigin)
	mt.moveTo(1, core.Point{X: moveOrigin.X + 45, Y: moveOrigin.Y - 45})
	mt.moveTo(2, core.Point{X: aimOrigin.X, Y: aimOrigin.Y - 50})
	app.Advance(150 * time.Millisecond)

	if gc.Arena.Player.X <= startX+5 || gc.Arena.Player.Y >= startY-5 {
		t.Fatalf("expected diagonal up-right move stick drag to move ship (+X, -Y): start=(%.1f, %.1f) now=(%.1f, %.1f)",
			startX, startY, gc.Arena.Player.X, gc.Arena.Player.Y)
	}
	if gc.Arena.Control.AimStickY >= -0.8 || math.Abs(float64(gc.Arena.Control.AimStickX)) > 0.05 {
		t.Fatalf("aim stick dragged up got AimStick=(%v, %v), want ~(0, -1)", gc.Arena.Control.AimStickX, gc.Arena.Control.AimStickY)
	}
	if !gc.Arena.Control.Fire {
		t.Fatal("expected Fire=true while aim stick is pushed past its dead zone")
	}
	wantUp := float32(-math.Pi / 2)
	if diff := angleDiff(gc.Arena.Player.AimAngle, wantUp); diff > 0.05 {
		t.Fatalf("AimAngle while aiming up = %v, want %v (diff %v)", gc.Arena.Player.AimAngle, wantUp, diff)
	}
	if gc.Arena.ActiveBullets() == 0 {
		t.Fatal("expected aim stick past dead zone to fire plasma bullets even with no enemies")
	}

	// 3. Drag aim stick down-left and verify AimStickX < 0, AimStickY > 0 (screen-down Y).
	mt.moveTo(2, core.Point{X: aimOrigin.X - 45, Y: aimOrigin.Y + 45})
	app.Advance(2 * testFrame)
	if gc.Arena.Control.AimStickX >= -0.5 || gc.Arena.Control.AimStickY <= 0.5 {
		t.Fatalf("aim stick dragged down-left got AimStick=(%v, %v), want (< -0.5, > 0.5)",
			gc.Arena.Control.AimStickX, gc.Arena.Control.AimStickY)
	}
	wantDownLeft := float32(3 * math.Pi / 4)
	if diff := angleDiff(gc.Arena.Player.AimAngle, wantDownLeft); diff > 0.1 {
		t.Fatalf("AimAngle while aiming down-left = %v, want %v (diff %v)", gc.Arena.Player.AimAngle, wantDownLeft, diff)
	}

	// 4. Release aim stick: AimStick returns to (0, 0), Fire returns to false, and auto-aim targets the nearest enemy.
	mt.up(2)
	mt.up(1)
	app.Advance(2 * testFrame)
	if gc.Arena.Control.AimStickX != 0 || gc.Arena.Control.AimStickY != 0 || gc.Arena.Control.Fire {
		t.Fatalf("after releasing aim stick got AimStick=(%v, %v) Fire=%v, want (0, 0) false",
			gc.Arena.Control.AimStickX, gc.Arena.Control.AimStickY, gc.Arena.Control.Fire)
	}
	if gc.Arena.Control.HasWorldAim {
		t.Fatal("touch controls must never set HasWorldAim")
	}

	// Place an enemy directly to the right of the player and verify auto-aim snaps to it.
	gc.Arena.spawnEnemyAt(EnemySwarm, gc.Arena.Player.X+60, gc.Arena.Player.Y)
	app.Advance(2 * testFrame)
	if diff := angleDiff(gc.Arena.Player.AimAngle, 0); diff > 0.1 {
		t.Fatalf("after releasing aim stick, auto-aim AimAngle = %v, want ~0 toward enemy on +X", gc.Arena.Player.AimAngle)
	}

	// 5. Tapping the round NOVA button triggers Nova and destroys the nearby enemy.
	scoreBefore := gc.Arena.Score
	mt.down(3, novaCenter)
	app.Advance(2 * testFrame)
	mt.up(3)
	if gc.Arena.Player.NovaCooldown <= 0 {
		t.Fatal("expected pressing touch NOVA button to trigger Nova cooldown")
	}
	if gc.Arena.Score <= scoreBefore {
		t.Fatalf("expected touch NOVA button to destroy nearby enemy: score before=%d after=%d", scoreBefore, gc.Arena.Score)
	}
}

func TestTouchNeverSetsStaleWorldAim(t *testing.T) {
	app, gc := startStarburst(t, nsgotest.Idiom(core.IdiomDesktop))

	// Desktop mouse hover enables world aim toward the cursor.
	app.HoverAt("arena-view", core.Point{X: 840, Y: 270})
	app.Advance(2 * testFrame)
	if !gc.Arena.Control.HasWorldAim {
		t.Fatal("expected mouse hover to enable HasWorldAim")
	}

	// Any touch clears HasWorldAim and never leaves a stale world aim point after lift.
	moveCenter := centerOf(touchControlFrame(t, gc, "move"))
	f := app.Touch("arena-view", moveCenter)
	app.Advance(2 * testFrame)
	if gc.Arena.Control.HasWorldAim {
		t.Fatal("expected touch down to clear HasWorldAim")
	}
	f.Lift()
	app.Advance(2 * testFrame)
	if gc.Arena.Control.HasWorldAim {
		t.Fatal("expected HasWorldAim to remain false after touch lift")
	}

	// Touching open canvas space (outside sticks) also does not set HasWorldAim, so auto-aim targets the nearest enemy.
	gc.Arena.enemies = gc.Arena.enemies[:0]
	gc.Arena.waveRemaining = 0
	gc.Arena.spawnEnemyAt(EnemyDrone, gc.Arena.Player.X, gc.Arena.Player.Y-80) // directly above ship (-pi/2)
	f2 := app.Touch("arena-view", core.Point{X: 480, Y: 480})                  // bottom center, opposite the enemy
	app.Advance(2 * testFrame)
	f2.Lift()
	app.Advance(2 * testFrame)
	if gc.Arena.Control.HasWorldAim {
		t.Fatal("touch on open canvas set HasWorldAim")
	}
	wantUp := float32(-math.Pi / 2)
	if diff := angleDiff(gc.Arena.Player.AimAngle, wantUp); diff > 0.1 {
		t.Fatalf("auto-aim after touch aimed at %v, want %v (nearest enemy above, not stale touch below)", gc.Arena.Player.AimAngle, wantUp)
	}

	// Mouse down on desktop re-enables mouse world aim without needing a prior hover.
	app.MouseDown("arena-view", core.MouseButtonLeft)
	app.Advance(2 * testFrame)
	if !gc.Arena.Control.HasWorldAim {
		t.Fatal("expected mouse down to enable HasWorldAim on desktop")
	}
	app.MouseUp("arena-view", core.MouseButtonLeft)

	// Left-click press fires on desktop and mouse hover aims at the cursor.
	app.HoverAt("arena-view", core.Point{X: 840, Y: 270})
	app.PressDown("arena-view")
	app.Advance(2 * testFrame)
	if !gc.Arena.Control.HasWorldAim {
		t.Fatal("expected mouse hover to keep HasWorldAim enabled on desktop")
	}
	if !gc.Arena.Control.Fire {
		t.Fatal("expected left mouse press to fire on desktop")
	}
	if diff := angleDiff(gc.Arena.Player.AimAngle, 0); diff > 0.1 {
		t.Fatalf("desktop mouse aim to right of ship got AimAngle = %v, want ~0", gc.Arena.Player.AimAngle)
	}
	app.PressUp("arena-view")
}

func TestTouchControlsHideOnDesktopUntilTouched(t *testing.T) {
	app, gc := startStarburst(t, nsgotest.Idiom(core.IdiomDesktop))

	if app.Exists("id:touch-move") || app.Exists("id:touch-aim") || app.Exists("id:touch-nova") {
		t.Fatal("expected touch controls to start hidden on desktop")
	}
	if !app.Exists("id:hud-controls") {
		t.Fatal("expected desktop control legend to be visible before touch")
	}

	// First touch reveals the controls and hides the desktop legend.
	f := app.Touch("arena-view", centerOf(touchControlFrame(t, gc, "move")))
	app.Advance(2 * testFrame)
	f.Lift()
	if !app.Exists("id:touch-move") || !app.Exists("id:touch-aim") || !app.Exists("id:touch-nova") {
		t.Fatal("expected touch to reveal touch controls")
	}
	if app.Exists("id:hud-controls") {
		t.Fatal("expected desktop control legend to hide while touch controls are visible")
	}

	// Keyboard input hides the touch controls again.
	app.Key("d")
	app.Advance(2 * testFrame)
	if app.Exists("id:touch-move") || app.Exists("id:touch-aim") || app.Exists("id:touch-nova") {
		t.Fatal("expected keyboard input to hide touch controls")
	}
	if !app.Exists("id:hud-controls") {
		t.Fatal("expected desktop control legend to return after keyboard input")
	}
}

func TestTouchControlsStayInsideSafeAreaAndLeaveShipUncovered(t *testing.T) {
	safe := core.Edges{Left: 47, Right: 47, Bottom: 21}
	_, gc := startStarburst(t, nsgotest.Size(844, 390), nsgotest.Environment(core.Environment{
		Idiom:    core.IdiomPhone,
		Pointer:  core.PointerCoarse,
		SafeArea: safe,
	}))

	move := touchControlFrame(t, gc, "move")
	aim := touchControlFrame(t, gc, "aim")
	nova := touchControlFrame(t, gc, "nova")

	for name, f := range map[string]core.Bounds{"move": move, "aim": aim, "nova": nova} {
		if f.Origin.X < safe.Left || f.Origin.X+f.Size.Width > 844-safe.Right || f.Origin.Y < safe.Top || f.Origin.Y+f.Size.Height > 390-safe.Bottom {
			t.Fatalf("%s frame %+v extends outside safe area %+v", name, f, safe)
		}
		if f.Size.Width < 44 || f.Size.Height < 44 {
			t.Fatalf("%s frame %+v is smaller than 44pt minimum touch target", name, f)
		}
	}

	if boundsOverlap(move, aim) {
		t.Fatalf("move %+v and aim %+v overlap", move, aim)
	}
	if boundsOverlap(nova, aim) {
		t.Fatalf("nova %+v and aim %+v overlap", nova, aim)
	}
	if nova.Origin.Y+nova.Size.Height > aim.Origin.Y {
		t.Fatalf("expected nova %+v to sit above aim %+v", nova, aim)
	}

	// Player ship starts in the center of the arena and must not be covered by any touch control.
	size := gc.viewSize
	shipGame := gc.Arena.Camera.WorldToView(core.Point{X: core.Points(gc.Arena.Player.X), Y: core.Points(gc.Arena.Player.Y)})
	shipView := gc.Arena.Viewport.GameToView(size, shipGame)
	shipBounds := core.Bounds{
		Origin: core.Point{X: shipView.X - 16, Y: shipView.Y - 16},
		Size:   core.Size{Width: 32, Height: 32},
	}
	for name, f := range map[string]core.Bounds{"move": move, "aim": aim, "nova": nova} {
		if boundsOverlap(f, shipBounds) {
			t.Fatalf("%s frame %+v covers the ship at spawn %+v", name, f, shipBounds)
		}
	}
}
