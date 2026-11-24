package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/entities"
	"github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/scene"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// TestAgentSkillCLIScaffoldGenerateCheckAndMCP exercises the end-to-end workflow
// documented in skills/nsgo-game: scaffold with `game new`, generate a scene and
// component with `game generate`, index and validate with `game index` and
// `game check`, load the generated scene into a running game with mcp.Bridge,
// and verify simulation state through MCP calls.
func TestAgentSkillCLIScaffoldGenerateCheckAndMCP(t *testing.T) {
	stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "star-hop")

	// 1. Scaffold project with `game new`.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", "-name", "Star Hop", "-module", "example.com/star-hop", dir}, a); code != 0 {
		t.Fatalf("game new exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	// 2. Generate a main scene and a component using `game generate`.
	if code, out, errOut := runGenerate(t, dir, "-main", "scene", "arena"); code != 0 {
		t.Fatalf("game generate scene exited %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	if code, out, errOut := runGenerate(t, dir, "component", "health"); code != 0 {
		t.Fatalf("game generate component exited %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}

	// 3. Assign stable UIDs with `game index` and validate everything with `game check`.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"index", dir}, a); code != 0 {
		t.Fatalf("game index exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("game check exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	// 4. Parse the generated project and main scene YAML files.
	proj, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check: %v", err)
	}
	if proj.Autoload.MainScene != "arena" || len(proj.Autoload.Scenes) != 1 {
		t.Fatalf("unexpected autoload scenes in game.yaml: %+v", proj.Autoload)
	}
	scenePath := filepath.Join(dir, filepath.FromSlash(proj.Autoload.Scenes[0].Path))
	sceneBytes, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatalf("read generated scene %s: %v", scenePath, err)
	}
	doc, err := sceneformat.Parse(sceneBytes)
	if err != nil {
		t.Fatalf("sceneformat.Parse(%s): %v", scenePath, err)
	}

	world := entities.NewWorld()
	if _, err := sceneformat.SpawnEntities(doc, world); err != nil {
		t.Fatalf("sceneformat.SpawnEntities: %v", err)
	}
	player := world.Spawn()
	entities.Set(world, player, entities.Transform2D{X: 10, Y: 20})

	sc := scene.NewScene(&tilemap.Camera{Width: 480, Height: 640})
	sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))

	rng := random.NewStreams(7)
	var cx *core.Scope
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		mgr := scene.NewManager(s)
		mgr.Push(&scene.Controller{Scene: sc})
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				if tr, ok := entities.Get[entities.Transform2D](world, player); ok {
					tr.X += float32(60 * dt.Seconds())
				}
			},
			Draw: func(float64) {},
		}
		loop.Run(s)
		if _, err := mcp.Attach(s, mcp.Config{
			Scenes:    mgr,
			SceneName: func(*scene.Controller) string { return proj.Autoload.MainScene },
			WorldFunc: func() *entities.World { return world },
			EntityName: func(e entities.Entity) string {
				if e == player {
					return "player"
				}
				return ""
			},
			Loop:          loop,
			RandomStreams: rng,
		}); err != nil {
			t.Errorf("mcp.Attach: %v", err)
		}
		return core.View{}
	})

	call := func(tool string, args any) json.RawMessage {
		t.Helper()
		var payload json.RawMessage
		if args != nil {
			b, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			payload = b
		}
		var (
			res json.RawMessage
			err error
		)
		app.Post(func() {
			res, err = automation.Call(cx, t.Context(), tool, payload)
		})
		app.Settle()
		if err != nil {
			t.Fatalf("%s failed: %v", tool, err)
		}
		return res
	}

	var scenes mcp.ScenesListResult
	if err := json.Unmarshal(call(mcp.ToolScenesList, nil), &scenes); err != nil {
		t.Fatal(err)
	}
	if scenes.Current != "arena" {
		t.Fatalf("current scene = %q, want %q", scenes.Current, "arena")
	}

	call(mcp.ToolLoopPause, nil)
	app.Post(func() {
		tr, _ := entities.Get[entities.Transform2D](world, player)
		tr.X = 10
	})
	app.Settle()
	call(mcp.ToolLoopSeed, map[string]any{"seed": uint64(99)})

	var step mcp.LoopStepResult
	if err := json.Unmarshal(call(mcp.ToolLoopStep, map[string]any{"count": 60}), &step); err != nil {
		t.Fatal(err)
	}
	if step.StepsAdvanced != 60 || step.DeltaSeconds != 1.0 {
		t.Fatalf("game_loop_step = %+v, want 60 steps / 1.0s", step)
	}

	var list mcp.EntitiesListResult
	if err := json.Unmarshal(call(mcp.ToolEntitiesList, map[string]any{"name": "player"}), &list); err != nil {
		t.Fatal(err)
	}
	if list.Returned != 1 {
		t.Fatalf("game_entities_list(name=player) = %+v", list)
	}

	var comp mcp.ComponentGetResult
	if err := json.Unmarshal(call(mcp.ToolComponentGet, map[string]any{
		"ref":       list.Entities[0].Ref,
		"component": "transform2d",
	}), &comp); err != nil {
		t.Fatal(err)
	}
	var tr entities.Transform2D
	if err := json.Unmarshal(comp.Value, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.X < 69.9 || tr.X > 70.1 {
		t.Fatalf("player X after 60 steps = %v, want ~70", tr.X)
	}
}
