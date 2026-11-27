package gamecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// TestAgentSkillDocumentsEveryCommand requires the skill's CLI reference to name every game subcommand, so a new command fails here until skills/nsgo-game documents it.
func TestAgentSkillDocumentsEveryCommand(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "skills", "nsgo-game", "references", "cli-and-formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"help"}
	for _, c := range commands {
		names = append(names, c.name)
	}
	for _, name := range names {
		if !strings.Contains(string(doc), "`game "+name+"`") {
			t.Errorf("skills/nsgo-game/references/cli-and-formats.md does not document `game %s`", name)
		}
	}
}

// skillHealth stands in for the Health component game generate writes into the project, which the test cannot import.
type skillHealth struct{}

// TestAgentSkillCLIScaffoldGenerateCheckAndMCP exercises the end-to-end workflow documented in skills/nsgo-game: scaffold with `game new`, generate a scene and component with `game generate`, place an object using the component in the generated scene, index and validate with `game index` and `game check`, load the scene into a running game with mcp.Bridge, and verify the scene's object through MCP calls.
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

	// 3. Place a player using the generated health component in the generated scene, as an agent edits the YAML.
	var componentFile string
	for _, name := range listFiles(t, dir) {
		if strings.HasSuffix(name, "/health_component.go") || name == "health_component.go" {
			componentFile = name
		}
	}
	if componentFile == "" {
		t.Fatalf("game generate component health wrote no health_component.go: %v", listFiles(t, dir))
	}
	componentSrc, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(componentFile)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(componentSrc), `Name: "health"`) {
		t.Fatalf("%s does not register a component named health:\n%s", componentFile, componentSrc)
	}
	generatedScene := filepath.Join(dir, "scenes", "arena.scene.yaml")
	raw, err := os.ReadFile(generatedScene)
	if err != nil {
		t.Fatal(err)
	}
	edit, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("sceneformat.Parse(generated arena scene): %v", err)
	}
	edit.Layers[0].Objects = append(edit.Layers[0].Objects, sceneformat.ObjectDoc{
		ID: "player", Name: "Player", X: 10, Y: 20,
		Components: map[string]any{"health": map[string]any{}},
	})
	raw, err = sceneformat.Encode(edit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generatedScene, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Assign stable UIDs with `game index` and validate everything with `game check`.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"index", dir}, a); code != 0 {
		t.Fatalf("game index exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("game check exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	// 5. Parse the generated project and main scene YAML files, checking components against the generated registry.
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
	registry := sceneformat.NewComponentRegistry()
	registry.MustRegister(sceneformat.ComponentType{Name: "health", Doc: "Health"})
	doc, err := sceneformat.ParseWith(sceneBytes, sceneformat.ParseOptions{File: proj.Autoload.Scenes[0].Path, Components: registry})
	if err != nil {
		t.Fatalf("sceneformat.ParseWith(%s): %v", scenePath, err)
	}

	world := entities.NewWorld()
	if err := entities.RegisterCodec[skillHealth](world, "health"); err != nil {
		t.Fatal(err)
	}
	spawned, err := sceneformat.SpawnEntities(doc, world)
	if err != nil {
		t.Fatalf("sceneformat.SpawnEntities: %v", err)
	}
	names := map[entities.Entity]string{}
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			e := spawned[o.ID]
			names[e] = o.Name
			if _, ok := o.Components["health"]; ok {
				entities.Set(world, e, skillHealth{})
			}
			return true
		})
	}
	player, ok := spawned["player"]
	if !ok {
		t.Fatalf("player was not spawned from %s", scenePath)
	}

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
			Scenes:        mgr,
			SceneName:     func(*scene.Controller) string { return proj.Autoload.MainScene },
			WorldFunc:     func() *entities.World { return world },
			EntityName:    func(e entities.Entity) string { return names[e] },
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
	if err := json.Unmarshal(call(mcp.ToolEntitiesList, map[string]any{"name": "Player", "component": "health"}), &list); err != nil {
		t.Fatal(err)
	}
	if list.Returned != 1 {
		t.Fatalf("game_entities_list(name=Player, component=health) = %+v", list)
	}

	var inspect mcp.EntityInspectResult
	if err := json.Unmarshal(call(mcp.ToolEntityInspect, map[string]any{"ref": list.Entities[0].Ref}), &inspect); err != nil {
		t.Fatal(err)
	}
	components := make([]string, 0, len(inspect.Components))
	for name := range inspect.Components {
		components = append(components, name)
	}
	for _, want := range []string{"health", "object_ref", "transform2d"} {
		if !slices.Contains(components, want) {
			t.Fatalf("game_entity_inspect(player) components = %v, want %s", components, want)
		}
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
