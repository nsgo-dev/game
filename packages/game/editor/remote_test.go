package editor_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/entities"
	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type remoteJumpComp struct {
	JumpSpeed  float64 `json:"jump_speed"`
	Gravity    float64 `json:"gravity"`
	CoyoteTime float64 `json:"coyote_time"`
}

type remoteMotionComp struct {
	Speed  float64 `json:"speed"`
	Moved  float64 `json:"moved"`
	Active bool    `json:"active"`
}

type fixtureRemoteGame struct {
	mu          sync.Mutex
	scene       *scene.Scene
	altScene    *scene.Scene
	world       *entities.World
	loop        *game.Loop
	console     *debug.Console
	bridge      *gamemcp.Bridge
	knight      entities.Entity
	sword       entities.Entity
	coin        entities.Entity
	spawnedDust entities.Entity
	updateCount int
	gravity     float64
	godMode     bool
	maxJumps    int
	levelTag    string
	healed      int
}

func newFixtureRemoteGame(t *testing.T) (*fixtureRemoteGame, *sceneformat.SceneDoc) {
	t.Helper()

	doc, err := sceneformat.Parse([]byte(`version: 2
name: Level 01
virtualWidth: 640
virtualHeight: 360
backgroundColor: "#141824"
layers:
  - name: world
    space: world
    order: 0
  - name: actors
    space: world
    order: 10
    objects:
      - id: knight
        name: Knight
        x: 100
        y: 200
        components:
          jump:
            jump_speed: 420
            gravity: 1400
            coyote_time: 0.08
          motion:
            speed: 10
            moved: 0
            active: true
        children:
          - id: sword
            name: Sword
            x: 12
            y: 0
      - id: coin_01
        name: coin_01
        x: 240
        y: 200
`))
	if err != nil {
		t.Fatalf("parse fixture scene: %v", err)
	}

	sc := scene.NewScene(nil)
	sc.AddLayer(&scene.Layer{Name: "world", Order: 0, Visible: true})
	sc.AddLayer(&scene.Layer{Name: "actors", Order: 10, Visible: true})
	alt := scene.NewScene(nil)
	alt.AddLayer(&scene.Layer{Name: "hud", Order: 100, Visible: true})

	world := entities.NewWorld()
	if err := entities.RegisterCodec[remoteJumpComp](world, "jump"); err != nil {
		t.Fatal(err)
	}
	if err := entities.RegisterCodec[remoteMotionComp](world, "motion"); err != nil {
		t.Fatal(err)
	}

	spawned, err := sceneformat.SpawnEntities(doc, world)
	if err != nil {
		t.Fatalf("SpawnEntities: %v", err)
	}
	knight := spawned["knight"]
	sword := spawned["sword"]
	coin := spawned["coin_01"]
	entities.Set(world, knight, remoteJumpComp{JumpSpeed: 420, Gravity: 1400, CoyoteTime: 0.08})
	entities.Set(world, knight, remoteMotionComp{Speed: 10, Moved: 0, Active: true})

	// Also spawn a runtime-only entity with no ObjectRef.
	dust := world.Spawn()
	entities.Set(world, dust, entities.Transform2D{X: 112, Y: 210, ScaleX: 1, ScaleY: 1})

	g := &fixtureRemoteGame{
		scene:       sc,
		altScene:    alt,
		world:       world,
		knight:      knight,
		sword:       sword,
		coin:        coin,
		spawnedDust: dust,
		gravity:     980,
		godMode:     false,
		maxJumps:    2,
		levelTag:    "level_01",
	}

	g.console = debug.NewConsole()
	g.console.RegisterFloat("physics.gravity", 0, 3000, &g.gravity)
	g.console.RegisterBool("player.god_mode", &g.godMode)
	g.console.RegisterInt("player.max_jumps", 1, 5, &g.maxJumps)
	g.console.RegisterString("level.tag", &g.levelTag)
	g.console.RegisterCommand("heal", "heal <amount>", func(args []string) (string, error) {
		if len(args) != 1 {
			return "", fmt.Errorf("usage: heal <amount>")
		}
		var n int
		if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil {
			return "", err
		}
		g.healed += n
		return fmt.Sprintf("healed +%d (total %d)", n, g.healed), nil
	})

	g.loop = &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			g.updateCount++
			if m, ok := entities.Get[remoteMotionComp](g.world, g.knight); ok && m.Active {
				m.Moved += m.Speed
				entities.Set(g.world, g.knight, m)
				if tr, ok := entities.Get[entities.Transform2D](g.world, g.knight); ok {
					tr.X += float32(m.Speed)
					entities.Set(g.world, g.knight, tr)
					g.world.Hierarchy2D().MarkDirty(g.knight)
				}
			}
			g.world.Hierarchy2D().Sync()
		},
	}

	g.bridge = gamemcp.New(gamemcp.Config{
		Scene: g.scene,
		NamedScenes: map[string]*scene.Scene{
			"level_01": g.scene,
			"overlay":  g.altScene,
		},
		World:   g.world,
		Loop:    g.loop,
		Console: g.console,
		EntityName: func(e entities.Entity) string {
			if e == g.spawnedDust {
				return "dust_fx"
			}
			return ""
		},
		EntityTags: func(e entities.Entity) []string {
			if e == g.knight {
				return []string{"airborne"}
			}
			return nil
		},
	})

	return g, doc
}

func (g *fixtureRemoteGame) startInMemoryServer(t *testing.T) sdkmcp.Transport {
	t.Helper()
	srv := editor.NewRemoteBridgeServer(g.bridge, func(fn func()) {
		g.mu.Lock()
		defer g.mu.Unlock()
		fn()
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Run(ctx, serverTransport)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return clientTransport
}

// #911 Acceptance 1 + 2 + Live Changes + Keep in scene / Undo + Tunables + Console:
// Runs a fixture game with the MCP bridge attached, lists its scenes, layers, and
// entities (including hierarchy and runtime-spawned entities) in the Remote tab,
// edits a component field in the Remote Inspector and verifies the game's next step
// uses the new value, tests Pause + Step N advancing by exactly N fixed updates,
// edits tunables and runs a console command, keeps live changes into the open scene
// as a single undo step and verifies one Undo restores the scene, and captures a
// macOS nsgotest screenshot.
func TestRemoteSceneTreeInspectorStepAndLiveChanges(t *testing.T) {
	fg, doc := newFixtureRemoteGame(t)
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.RemoteRefreshInterval = 5 * time.Second // rely on explicit refreshes in deterministic test
	t.Cleanup(func() { _ = ed.Close() })

	beforeScene, err := sceneformat.Encode(ed.Doc)
	if err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))

	// Connect over in-memory MCP client/server transport.
	if err := ed.ConnectRemote(context.Background(), fg.startInMemoryServer(t)); err != nil {
		t.Fatalf("ConnectRemote: %v", err)
	}
	app.Settle()

	if !ed.RemoteConnected() {
		t.Fatal("expected RemoteConnected() == true")
	}
	if ed.SceneTreeTab() != editor.SceneTreeRemote {
		t.Fatalf("SceneTreeTab = %v, want SceneTreeRemote", ed.SceneTreeTab())
	}
	if !app.Exists("id:remote-play-bar") {
		t.Fatal("expected remote-play-bar while connected")
	}
	if !app.Exists("id:remote-scene-tree-panel") {
		t.Fatal("expected remote-scene-tree-panel in Remote tab")
	}

	snap := ed.RemoteSnapshot()
	if len(snap.Scenes.Scenes) != 2 {
		t.Fatalf("scenes = %+v, want 2 scenes (level_01 + overlay)", snap.Scenes.Scenes)
	}
	if len(snap.Layers) != 2 {
		t.Fatalf("layers = %+v, want 2 layers (world, actors)", snap.Layers)
	}
	if len(snap.Entities) != 4 {
		t.Fatalf("entities = %d, want 4 (knight, sword, coin_01, dust_fx)", len(snap.Entities))
	}

	knightRef := gamemcp.FormatEntityRef(fg.knight)
	swordRef := gamemcp.FormatEntityRef(fg.sword)
	dustRef := gamemcp.FormatEntityRef(fg.spawnedDust)

	// Verify entities (including child sword and runtime-spawned dust_fx) appear in the Remote tree UI.
	if !app.Exists("id:remote-entity-label-" + knightRef) {
		t.Fatalf("knight row %s not visible in remote tree", knightRef)
	}
	if !app.Exists("id:remote-entity-label-" + swordRef) {
		t.Fatalf("child sword row %s not visible in remote tree", swordRef)
	}
	if !app.Exists("id:remote-entity-label-" + dustRef) {
		t.Fatalf("spawned dust row %s not visible in remote tree", dustRef)
	}

	// Select knight in the Remote tab UI.
	app.Press("id:remote-entity-label-" + knightRef)
	app.Settle()
	if ed.SelectedRemoteEntity() != knightRef {
		t.Fatalf("SelectedRemoteEntity = %q, want %q", ed.SelectedRemoteEntity(), knightRef)
	}
	if got := app.Find("id:remote-inspector-entity-name").Text; got != "knight" {
		t.Fatalf("remote inspector entity name = %q, want knight", got)
	}

	// Acceptance 2: Pause and Step N in the editor advance the game by exactly N fixed updates.
	app.Press("id:remote-pause-resume")
	app.Settle()
	if got := ed.RemoteSnapshot().Loop; got == nil || !got.Paused {
		t.Fatalf("expected paused loop after Pause press, got %+v", got)
	}

	// Change motion.speed from 10 to 25 via the Remote Inspector control (game_component_set).
	app.Clear("id:remote-comp-motion-speed")
	app.Type("id:remote-comp-motion-speed", "25\n")
	app.Settle()

	// Also change jump.jump_speed from 420 to 460 for the scene Keep test.
	app.Clear("id:remote-comp-jump-jump_speed")
	app.Type("id:remote-comp-jump-jump_speed", "460\n")
	app.Settle()

	// Verify live change was applied in the running game world immediately.
	fg.mu.Lock()
	gotMotion, _ := entities.Get[remoteMotionComp](fg.world, fg.knight)
	gotJump, _ := entities.Get[remoteJumpComp](fg.world, fg.knight)
	fg.mu.Unlock()
	if gotMotion.Speed != 25 {
		t.Fatalf("game motion.speed = %v, want 25", gotMotion.Speed)
	}
	if gotJump.JumpSpeed != 460 {
		t.Fatalf("game jump.jump_speed = %v, want 460", gotJump.JumpSpeed)
	}

	// Step by N = 5 fixed updates using the Step N UI controls.
	app.Clear("id:remote-step-count")
	app.Type("id:remote-step-count", "5\n")
	app.Settle()
	app.Press("id:remote-step-n")
	app.Settle()

	fg.mu.Lock()
	stepsAfter5 := fg.updateCount
	movedAfter5, _ := entities.Get[remoteMotionComp](fg.world, fg.knight)
	trAfter5, _ := entities.Get[entities.Transform2D](fg.world, fg.knight)
	fg.mu.Unlock()

	if stepsAfter5 != 5 {
		t.Fatalf("game updateCount after Step 5 = %d, want 5", stepsAfter5)
	}
	// Acceptance 1: each of the 5 steps used the live-edited speed=25 -> moved == 125, X == 100 + 125 == 225.
	if movedAfter5.Moved != 125 {
		t.Fatalf("knight motion.moved after 5 steps with speed=25 = %v, want 125", movedAfter5.Moved)
	}
	if trAfter5.X != 225 {
		t.Fatalf("knight transform2d.x after 5 steps = %v, want 225", trAfter5.X)
	}

	// Step 1 more via the `.` keyboard shortcut while paused -> exactly 6 updates total.
	app.Key(".")
	app.Settle()
	fg.mu.Lock()
	stepsAfterDot := fg.updateCount
	fg.mu.Unlock()
	if stepsAfterDot != 6 {
		t.Fatalf("game updateCount after '.' step = %d, want 6", stepsAfterDot)
	}
	if got := ed.RemoteSnapshot().Loop.Steps; got != 6 {
		t.Fatalf("remote snapshot loop steps = %d, want 6", got)
	}

	// Time scale control via UI button.
	app.Press("id:remote-timescale-0.5")
	app.Settle()
	fg.mu.Lock()
	gotScale := fg.loop.EffectiveTimeScale()
	fg.mu.Unlock()
	if gotScale != 0.5 {
		t.Fatalf("game loop TimeScale = %v, want 0.5", gotScale)
	}

	// Tunables: edit physics.gravity and player.god_mode live through UI controls.
	app.Clear("id:remote-tunable-physics.gravity")
	app.Type("id:remote-tunable-physics.gravity", "1600\n")
	app.Settle()
	app.Press("id:remote-tunable-player.god_mode")
	app.Settle()
	fg.mu.Lock()
	gotGrav, gotGod := fg.gravity, fg.godMode
	fg.mu.Unlock()
	if gotGrav != 1600 || !gotGod {
		t.Fatalf("tunables after UI edit: gravity=%v godMode=%v, want 1600, true", gotGrav, gotGod)
	}

	// Console command execution via UI input + Run button.
	app.Type("id:remote-console-input", "heal 25")
	app.Press("id:remote-console-run")
	app.Settle()
	fg.mu.Lock()
	gotHealed := fg.healed
	fg.mu.Unlock()
	if gotHealed != 25 {
		t.Fatalf("console heal result = %d, want 25", gotHealed)
	}
	if hist := ed.RemoteConsoleHistory(); len(hist) != 1 || !hist[0].OK || !strings.Contains(hist[0].Output, "healed +25") {
		t.Fatalf("console history = %+v", hist)
	}

	// Destroy coin_01 in the game and refresh -> appears as a Taken entity in the Remote tree.
	fg.mu.Lock()
	fg.world.Destroy(fg.coin)
	fg.mu.Unlock()
	if err := ed.RefreshRemote(); err != nil {
		t.Fatalf("RefreshRemote after destroy: %v", err)
	}
	app.Settle()
	if !app.Exists("id:remote-taken-row-coin_01") {
		t.Fatalf("expected taken row for coin_01, snapshot taken=%+v", ed.RemoteSnapshot().Taken)
	}

	// Capture real nsgotest screenshot of the live Remote Play Mode layout.
	app.Screenshot("remote/play-mode")

	// Verify Live Changes list contains both edits (motion.speed and jump.jump_speed).
	changes := ed.RemoteLiveChanges()
	if len(changes) != 2 {
		t.Fatalf("RemoteLiveChanges = %+v, want 2 changes", changes)
	}

	// Stopping / disconnecting MUST preserve the unkept Live Changes list.
	app.Press("id:remote-disconnect")
	app.Settle()
	if ed.RemoteConnected() {
		t.Fatal("expected disconnected after pressing Disconnect")
	}
	if len(ed.RemoteLiveChanges()) != 2 {
		t.Fatalf("RemoteLiveChanges after Disconnect = %d, want 2 preserved", len(ed.RemoteLiveChanges()))
	}

	// Keep in scene writes both live changes into ed.Doc as ONE undo step.
	histBefore := ed.History.Index()
	app.Press("id:remote-changes-keep")
	app.Settle()

	if len(ed.RemoteLiveChanges()) != 0 {
		t.Fatalf("expected RemoteLiveChanges empty after Keep in scene, got %+v", ed.RemoteLiveChanges())
	}
	if ed.History.Index() != histBefore+1 {
		t.Fatalf("history index = %d, want %d (single undo group)", ed.History.Index(), histBefore+1)
	}
	knightObj, _, _ := ed.FindObject("knight")
	if knightObj == nil {
		t.Fatal("knight missing from scene doc")
	}
	if m, ok := knightObj.Components["jump"].(map[string]any); !ok || fmt.Sprint(m["jump_speed"]) != "460" {
		t.Fatalf("scene knight jump component after Keep = %#v, want jump_speed=460", knightObj.Components["jump"])
	}
	if m, ok := knightObj.Components["motion"].(map[string]any); !ok || fmt.Sprint(m["speed"]) != "25" {
		t.Fatalf("scene knight motion component after Keep = %#v, want speed=25", knightObj.Components["motion"])
	}

	// One Undo restores the scene document byte-for-byte!
	app.Press("id:toolbar-undo")
	app.Settle()
	afterUndo, err := sceneformat.Encode(ed.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterUndo, beforeScene) {
		t.Fatalf("scene after single Undo differs from before:\ngot:\n%s\nwant:\n%s", afterUndo, beforeScene)
	}
}

// Verifies RunGameWith(RunGameOptions{Automation: true}) passes `-automation`,
// connects automatically over an io.Pipe MCP transport (mirroring stdio),
// supports per-row Revert and Discard of live changes, and cleanly disconnects on StopGame.
func TestRunGameWithAutomationConnectsOverPipeAndRevertsOrDiscardsLiveChanges(t *testing.T) {
	fg, _ := newFixtureRemoteGame(t)
	dir := writeRunnableFixtureProject(t)

	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	ed.RemoteRefreshInterval = 5 * time.Second
	t.Cleanup(func() { _ = ed.Close() })
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()

	var capturedSpec editor.RunGameSpec
	var fakeProc *fakeGameProcess
	ed.RunGameCommand = func(spec editor.RunGameSpec, stdout, stderr io.Writer) (editor.GameProcess, error) {
		capturedSpec = spec
		fakeProc = newFakeGameProcess()
		return fakeProc, nil
	}

	// Wire an io.Pipe MCP transport to exercise newline-delimited JSON framing just like stdio `go tool nsgo mcp`.
	ed.RemoteTransport = func(ctx context.Context, spec editor.RunGameSpec) (sdkmcp.Transport, error) {
		c2sR, c2sW := io.Pipe()
		s2cR, s2cW := io.Pipe()
		srv := editor.NewRemoteBridgeServer(fg.bridge, func(fn func()) {
			fg.mu.Lock()
			defer fg.mu.Unlock()
			fn()
		})
		go func() {
			_ = srv.Run(ctx, &sdkmcp.IOTransport{Reader: c2sR, Writer: s2cW})
			_ = s2cW.Close()
		}()
		return &sdkmcp.IOTransport{Reader: s2cR, Writer: c2sW}, nil
	}

	if err := ed.RunGameWith(editor.RunGameOptions{Automation: true}); err != nil {
		t.Fatalf("RunGameWith(Automation: true): %v", err)
	}
	if !strings.Contains(strings.Join(capturedSpec.Args, " "), "-automation") {
		t.Fatalf("RunGameWith args = %v, want -automation", capturedSpec.Args)
	}

	waitForCondition(t, 5*time.Second, "remote MCP auto-connect", func() bool {
		return ed.RemoteConnected()
	})

	// Resume / Pause / Step via Go API.
	if err := ed.RemotePause(); err != nil {
		t.Fatalf("RemotePause: %v", err)
	}
	stepRes, err := ed.RemoteStep(3)
	if err != nil {
		t.Fatalf("RemoteStep(3): %v", err)
	}
	if stepRes.StepsAdvanced != 3 || stepRes.TotalSteps != 3 {
		t.Fatalf("RemoteStep(3) = %+v, want 3 advanced / 3 total", stepRes)
	}
	if err := ed.RemoteResume(); err != nil {
		t.Fatalf("RemoteResume: %v", err)
	}
	if ed.RemoteSnapshot().Loop.Paused {
		t.Fatal("expected unpaused after RemoteResume")
	}

	// Live edit two fields, revert one, then discard the remaining one.
	knightRef := gamemcp.FormatEntityRef(fg.knight)
	if err := ed.SelectRemoteEntity(knightRef); err != nil {
		t.Fatalf("SelectRemoteEntity: %v", err)
	}
	if err := ed.SetRemoteComponentField(knightRef, "jump", "jump_speed", 500.0); err != nil {
		t.Fatalf("SetRemoteComponentField jump_speed: %v", err)
	}
	if err := ed.SetRemoteComponentField(knightRef, "jump", "gravity", 1800.0); err != nil {
		t.Fatalf("SetRemoteComponentField gravity: %v", err)
	}
	if got := len(ed.RemoteLiveChanges()); got != 2 {
		t.Fatalf("live changes = %d, want 2", got)
	}

	// Revert index 0 (jump_speed -> back to 420).
	if err := ed.RevertRemoteLiveChange(0); err != nil {
		t.Fatalf("RevertRemoteLiveChange: %v", err)
	}
	fg.mu.Lock()
	jAfterRevert, _ := entities.Get[remoteJumpComp](fg.world, fg.knight)
	fg.mu.Unlock()
	if jAfterRevert.JumpSpeed != 420 || jAfterRevert.Gravity != 1800 {
		t.Fatalf("after revert: %+v, want jump_speed=420 gravity=1800", jAfterRevert)
	}

	// Discard remaining live change (gravity -> back to 1400).
	if err := ed.DiscardRemoteLiveChanges(); err != nil {
		t.Fatalf("DiscardRemoteLiveChanges: %v", err)
	}
	fg.mu.Lock()
	jAfterDiscard, _ := entities.Get[remoteJumpComp](fg.world, fg.knight)
	fg.mu.Unlock()
	if jAfterDiscard.Gravity != 1400 {
		t.Fatalf("after discard: %+v, want gravity=1400", jAfterDiscard)
	}
	if len(ed.RemoteLiveChanges()) != 0 {
		t.Fatalf("expected 0 live changes after Discard, got %+v", ed.RemoteLiveChanges())
	}

	// Switching scenes in Remote tab lists overlay's hud layer.
	if err := ed.SelectRemoteScene("overlay"); err != nil {
		t.Fatalf("SelectRemoteScene(overlay): %v", err)
	}
	layers := ed.RemoteSnapshot().Layers
	if len(layers) != 1 || layers[0].Name != "hud" {
		t.Fatalf("overlay layers = %+v, want [hud]", layers)
	}

	// StopGame disconnects remote session cleanly.
	if err := ed.StopGame(); err != nil {
		t.Fatalf("StopGame: %v", err)
	}
	if ed.RemoteConnected() {
		t.Fatal("expected RemoteConnected() == false after StopGame()")
	}
	if !fakeProc.Stopped() {
		t.Fatal("expected fake game process stopped")
	}
}
