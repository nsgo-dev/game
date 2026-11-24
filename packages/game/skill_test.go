package game_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
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
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/automation"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"go.yaml.in/yaml/v3"
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var (
	skillNamePattern      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	markdownLinkPattern   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	skillPackageRowRegexp = regexp.MustCompile("^\\| `(game(?:/[a-z0-9_.-]+)*)` \\| (Stable|Experimental) \\|")
)

func parseSkillFrontmatter(t *testing.T, data []byte) (skillFrontmatter, string) {
	t.Helper()
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("SKILL.md must start with YAML frontmatter delimiter ---")
	}
	rest := text[len("---\n"):]
	fmText, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatalf("SKILL.md is missing closing YAML frontmatter delimiter ---")
	}
	var fm skillFrontmatter
	dec := yaml.NewDecoder(strings.NewReader(fmText))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		t.Fatalf("parse SKILL.md frontmatter: %v", err)
	}
	return fm, body
}

func stripFencedCodeBlocks(md string) string {
	var out strings.Builder
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// TestAgentSkillValid verifies that skills/nsgo-game conforms to the Agent Skills layout
// (installable via `bunx skills add`), has valid relative links, stays in sync with all
// packages under packages/game, documents all CLI subcommands, and lists every registered
// game/mcp tool.
func TestAgentSkillValid(t *testing.T) {
	skillDir := filepath.Join("..", "..", "skills", "nsgo-game")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	raw, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read %s: %v", skillPath, err)
	}
	fm, body := parseSkillFrontmatter(t, raw)

	if fm.Name != "nsgo-game" {
		t.Errorf("SKILL.md frontmatter name = %q, want %q", fm.Name, "nsgo-game")
	}
	if !skillNamePattern.MatchString(fm.Name) || len(fm.Name) > 64 {
		t.Errorf("SKILL.md frontmatter name %q is not a valid Agent Skill name", fm.Name)
	}
	if strings.TrimSpace(fm.Description) == "" {
		t.Errorf("SKILL.md frontmatter description must be non-empty")
	}
	if len(fm.Description) > 1024 {
		t.Errorf("SKILL.md frontmatter description length = %d, want <= 1024", len(fm.Description))
	}

	for _, ref := range []string{
		"references/packages.md",
		"references/cli-and-formats.md",
		"references/mcp-tools.md",
	} {
		if !strings.Contains(body, ref) {
			t.Errorf("SKILL.md must reference %s", ref)
		}
	}

	absSkillDir, err := filepath.Abs(skillDir)
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(skillDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return walkErr
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		cleaned := stripFencedCodeBlocks(string(data))
		for _, m := range markdownLinkPattern.FindAllStringSubmatch(cleaned, -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") ||
				strings.HasPrefix(target, "#") {
				continue
			}
			targetPath, _, _ := strings.Cut(target, "#")
			if targetPath == "" {
				continue
			}
			resolved, err := filepath.Abs(filepath.Join(filepath.Dir(p), filepath.FromSlash(targetPath)))
			if err != nil {
				t.Errorf("%s: resolve link %q: %v", p, target, err)
				continue
			}
			rel, err := filepath.Rel(absSkillDir, resolved)
			if err != nil || strings.HasPrefix(rel, "..") {
				t.Errorf("%s: relative link %q escapes %s", p, target, skillDir)
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s: broken relative link %q: %v", p, target, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify references/packages.md matches the real module packages.
	pkgsDoc, err := os.ReadFile(filepath.Join(skillDir, "references", "packages.md"))
	if err != nil {
		t.Fatal(err)
	}
	documentedPkgs := map[string]bool{}
	for i, line := range strings.Split(string(pkgsDoc), "\n") {
		m := skillPackageRowRegexp.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pkg := m[1]
		if documentedPkgs[pkg] {
			t.Errorf("skills/nsgo-game/references/packages.md:%d: duplicate package %q", i+1, pkg)
		}
		documentedPkgs[pkg] = true
	}
	actualPkgs := modulePackages(t)
	for pkg := range documentedPkgs {
		if _, ok := actualPkgs[pkg]; !ok {
			t.Errorf("skills/nsgo-game/references/packages.md lists unknown package %q", pkg)
		}
	}
	var missingPkgs []string
	for pkg := range actualPkgs {
		if !documentedPkgs[pkg] {
			missingPkgs = append(missingPkgs, pkg)
		}
	}
	slices.Sort(missingPkgs)
	for _, pkg := range missingPkgs {
		t.Errorf("skills/nsgo-game/references/packages.md is missing package %q", pkg)
	}

	// Verify references/cli-and-formats.md covers every `game` CLI subcommand.
	cliDocBytes, err := os.ReadFile(filepath.Join(skillDir, "references", "cli-and-formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	cliDoc := string(cliDocBytes)
	for _, cmd := range []string{
		"game new",
		"game dev",
		"game generate",
		"game editor",
		"game check",
		"game index",
		"game mv",
		"game library",
		"game assets",
		"game pack",
		"game audio",
		"game tex",
		"game manifest",
		"game credits",
		"game replay",
		"game bench",
		"game release",
		"game completion",
		"game help",
	} {
		if !strings.Contains(cliDoc, "`"+cmd+"`") {
			t.Errorf("skills/nsgo-game/references/cli-and-formats.md does not document `%s`", cmd)
		}
	}

	// Verify references/mcp-tools.md documents every currently registered MCP tool.
	mcpDocBytes, err := os.ReadFile(filepath.Join(skillDir, "references", "mcp-tools.md"))
	if err != nil {
		t.Fatal(err)
	}
	mcpDoc := string(mcpDocBytes)
	bridge := mcp.New(mcp.Config{})
	tools := bridge.Tools()
	if len(tools) == 0 {
		t.Fatal("mcp.New returned zero tools")
	}
	for _, tool := range tools {
		if !strings.Contains(mcpDoc, "`"+tool.Name+"`") {
			t.Errorf("skills/nsgo-game/references/mcp-tools.md is missing registered MCP tool %q", tool.Name)
		}
	}
}

type skillVelocity struct {
	VX float32 `json:"vx"`
	VY float32 `json:"vy"`
}

func callSkillMCPTool[T any](t *testing.T, app *nsgotest.App, cx *core.Scope, name string, args any) T {
	t.Helper()
	var payload json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshal args for %s: %v", name, err)
		}
		payload = b
	}
	var (
		raw json.RawMessage
		err error
	)
	app.Post(func() {
		raw, err = automation.Call(cx, t.Context(), name, payload)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s result %s: %v", name, raw, err)
	}
	return out
}

// TestAgentSkillWorkflowBuildAndVerifyThroughMCP simulates an agent following
// skills/nsgo-game: authoring plain-text YAML project & scene files, validating them,
// loading the scene into an ECS world with a fixed-step game.Loop, attaching mcp.Bridge,
// and verifying deterministic gameplay over MCP.
func TestAgentSkillWorkflowBuildAndVerifyThroughMCP(t *testing.T) {
	dir := t.TempDir()
	gameYAML := strings.Join([]string{
		"version: 1",
		"name: Skill Demo",
		"id: dev.nsgo.skilldemo",
		"appVersion: 0.1.0",
		"window:",
		"  width: 320",
		"  height: 180",
		"autoload:",
		"  mainScene: arena",
		"  scenes:",
		"    - name: arena",
		"      path: scenes/arena.scene.yaml",
		"",
	}, "\n")
	nsgoYAML := "name: Skill Demo\nid: dev.nsgo.skilldemo\nversion: 0.1.0\ngame: true\n"
	arenaYAML := strings.Join([]string{
		"version: 1",
		"name: arena",
		"virtualWidth: 320",
		"virtualHeight: 180",
		"layers:",
		"  - name: world",
		"    space: world",
		"    order: 0",
		"    objects:",
		"      - id: hero",
		"        x: 20",
		"        \"y\": 40",
		"      - id: target",
		"        x: 140",
		"        \"y\": 40",
		"  - name: hud",
		"    space: screen",
		"    order: 10",
		"",
	}, "\n")

	if err := os.MkdirAll(filepath.Join(dir, "scenes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		project.FileName:          gameYAML,
		"nsgo.yaml":               nsgoYAML,
		"scenes/arena.scene.yaml": arenaYAML,
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Step 1: Validate project file and scene references (as `game check` does).
	proj, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("project.Check: %v", err)
	}
	if proj.Autoload.MainScene != "arena" {
		t.Fatalf("MainScene = %q, want %q", proj.Autoload.MainScene, "arena")
	}
	report, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("assetcheck.Check: %v", err)
	}
	if len(report.Problems) > 0 {
		t.Fatalf("assetcheck reported problems: %v", report.Problems)
	}

	// Step 2: Load the YAML scene and spawn entities into an ECS world.
	sceneBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "arena.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := sceneformat.Parse(sceneBytes)
	if err != nil {
		t.Fatalf("sceneformat.Parse: %v", err)
	}
	world := entities.NewWorld()
	entities.RegisterCodec[skillVelocity](world, "velocity")

	spawned, err := sceneformat.SpawnEntities(doc, world)
	if err != nil {
		t.Fatalf("sceneformat.SpawnEntities: %v", err)
	}
	hero := spawned["hero"]
	if hero == entities.Zero {
		t.Fatal("hero entity was not spawned from arena.scene.yaml")
	}
	entities.Set(world, hero, skillVelocity{VX: 120, VY: 0})

	sc := scene.NewScene(&tilemap.Camera{X: 160, Y: 90, Width: 320, Height: 180})
	sc.AddLayer(scene.NewLayer("world", scene.SpaceWorld, 0))
	sc.AddLayer(scene.NewLayer("hud", scene.SpaceScreen, 10))

	rng := random.NewStreams(1)
	var (
		cx     *core.Scope
		loop   *game.Loop
		bridge *mcp.Bridge
	)

	// Step 3: Start the game with a fixed-step Loop and attach mcp.Bridge.
	app := nsgotest.Start(t, func(s *core.Scope) core.Node {
		cx = s
		mgr := scene.NewManager(s)
		mgr.Push(&scene.Controller{Scene: sc})
		frame := core.NewSignal(s, uint64(0))
		loop = &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				secs := float32(dt.Seconds())
				jitter := float32(rng.Stream("movement").Intn(3)) // deterministic given seed
				if tr, ok := entities.Get[entities.Transform2D](world, hero); ok {
					if vel, ok := entities.Get[skillVelocity](world, hero); ok {
						tr.X += vel.VX*secs + jitter*0.01
						tr.Y += vel.VY * secs
					}
				}
			},
			Draw: func(float64) {
				frame.Update(func(n uint64) uint64 { return n + 1 })
			},
		}
		loop.Run(s)

		var attachErr error
		bridge, attachErr = mcp.Attach(s, mcp.Config{
			Scenes:        mgr,
			SceneName:     func(c *scene.Controller) string { return "arena" },
			WorldFunc:     func() *entities.World { return world },
			Loop:          loop,
			RandomStreams: rng,
		})
		if attachErr != nil {
			t.Errorf("mcp.Attach: %v", attachErr)
		}

		return core.Canvas{
			TestID: "game-canvas",
			Style:  core.Style{FlexGrow: 1, FlexShrink: 1},
			Draw: func(d *core.Drawing, _ core.Size) {
				_ = frame.Get()
				sc.Draw(d)
			},
		}
	})
	if bridge == nil || !bridge.Attached() {
		t.Fatal("mcp.Bridge failed to attach")
	}

	// Step 4: Follow the skill's MCP verification loop: inspect -> pause -> seed -> step -> assert.
	scenesRes := callSkillMCPTool[mcp.ScenesListResult](t, app, cx, mcp.ToolScenesList, nil)
	if scenesRes.Current != "arena" || scenesRes.StackDepth != 1 {
		t.Fatalf("game_scenes_list = %+v, want current=arena depth=1", scenesRes)
	}

	layersRes := callSkillMCPTool[mcp.LayersListResult](t, app, cx, mcp.ToolLayersList, nil)
	if len(layersRes.Layers) != 2 || layersRes.Layers[0].Name != "world" || layersRes.Layers[1].Name != "hud" {
		t.Fatalf("game_layers_list = %+v, want [world, hud]", layersRes.Layers)
	}

	pauseRes := callSkillMCPTool[mcp.LoopStatusResult](t, app, cx, mcp.ToolLoopPause, nil)
	if !pauseRes.Paused || !pauseRes.SeedConfigured {
		t.Fatalf("game_loop_pause = %+v, want paused=true seed_configured=true", pauseRes)
	}

	// Reset hero position and apply a fixed seed for a reproducible 1-second simulation.
	app.Post(func() {
		tr, _ := entities.Get[entities.Transform2D](world, hero)
		tr.X = 20
		tr.Y = 40
	})
	app.Settle()

	seedRes := callSkillMCPTool[mcp.LoopSeedResult](t, app, cx, mcp.ToolLoopSeed, map[string]any{"seed": uint64(42)})
	if seedRes.Seed != 42 {
		t.Fatalf("game_loop_seed = %+v, want seed=42", seedRes)
	}

	entList := callSkillMCPTool[mcp.EntitiesListResult](t, app, cx, mcp.ToolEntitiesList, map[string]any{"name": "hero"})
	if entList.Returned != 1 || entList.Entities[0].Name != "hero" {
		t.Fatalf("game_entities_list(name=hero) = %+v", entList)
	}
	heroRef := entList.Entities[0].Ref

	stepRes := callSkillMCPTool[mcp.LoopStepResult](t, app, cx, mcp.ToolLoopStep, map[string]any{"count": 60})
	if stepRes.StepsAdvanced != 60 || math.Abs(stepRes.DeltaSeconds-1.0) > 1e-9 {
		t.Fatalf("game_loop_step(60) = %+v, want steps_advanced=60 delta_seconds=1.0", stepRes)
	}

	inspectRes := callSkillMCPTool[mcp.EntityInspectResult](t, app, cx, mcp.ToolEntityInspect, map[string]any{"ref": heroRef})
	if _, ok := inspectRes.Components["velocity"]; !ok {
		t.Fatalf("game_entity_inspect missing velocity component: %+v", inspectRes.Components)
	}

	trRes := callSkillMCPTool[mcp.ComponentGetResult](t, app, cx, mcp.ToolComponentGet, map[string]any{
		"ref":       heroRef,
		"component": "transform2d",
	})
	var tr entities.Transform2D
	if err := json.Unmarshal(trRes.Value, &tr); err != nil {
		t.Fatalf("unmarshal transform2d %s: %v", trRes.Value, err)
	}

	// Recompute exact expected X from seed 42 over 60 steps at 60 Hz.
	checkRNG := random.NewStreams(42)
	expectedX := float32(20)
	dtSecs := float32((time.Second / 60).Seconds())
	for range 60 {
		expectedX += 120*dtSecs + float32(checkRNG.Stream("movement").Intn(3))*0.01
	}
	if math.Abs(float64(tr.X-expectedX)) > 1e-4 {
		t.Fatalf("hero transform2d.X = %v, want %v (seed 42, 60 steps)", tr.X, expectedX)
	}

	// Verify that a fresh YAML project file round-trips canonically.
	canonical, err := project.Encode(proj)
	if err != nil {
		t.Fatalf("project.Encode: %v", err)
	}
	if !bytes.Equal(canonical, []byte(gameYAML)) {
		t.Fatalf("game.yaml did not round-trip canonically:\ngot:\n%s\nwant:\n%s", canonical, gameYAML)
	}
}
