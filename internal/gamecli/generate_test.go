package gamecli

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

// generateGameYAML is a project file in the form project.Encode writes.
const generateGameYAML = "version: 1\nname: Rocks\nid: dev.nsgo.rocks\nappVersion: 0.1.0\nwindow:\n  width: 480\n  height: 640\n"

// generateProject returns a directory holding a game.yaml and a main package, as game new writes them, with files added.
func generateProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	all := map[string]string{
		project.FileName: generateGameYAML,
		"main.go":        "// Command rocks is a game.\npackage main\n\nfunc main() {}\n",
	}
	for name, body := range files {
		all[name] = body
	}
	writeFiles(t, dir, all)
	return dir
}

// runGenerate runs game generate with args in dir and returns its exit code, standard output and standard error.
func runGenerate(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	a, stdout, stderr := newTestApp(t)
	code := run(append([]string{"generate", "-in", dir}, args...), a)
	return code, stdout.String(), stderr.String()
}

// listFiles returns the slash-separated paths of the files under dir, sorted.
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	return names
}

// readFile returns the content of the file at the slash-separated path name under dir.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGenerateNames(t *testing.T) {
	cases := []struct {
		name string
		want generateData
	}{
		{"health", generateData{Go: "Health", Camel: "health", Snake: "health", Title: "Health"}},
		{"player_health", generateData{Go: "PlayerHealth", Camel: "playerHealth", Snake: "player_health", Title: "Player Health"}},
		{"player-health", generateData{Go: "PlayerHealth", Camel: "playerHealth", Snake: "player_health", Title: "Player Health"}},
		{"PlayerHealth", generateData{Go: "PlayerHealth", Camel: "playerHealth", Snake: "player_health", Title: "Player Health"}},
		{"playerHealth", generateData{Go: "PlayerHealth", Camel: "playerHealth", Snake: "player_health", Title: "Player Health"}},
		{"HTTPServer", generateData{Go: "HTTPServer", Camel: "httpServer", Snake: "http_server", Title: "HTTP Server"}},
		{"level1", generateData{Go: "Level1", Camel: "level1", Snake: "level1", Title: "Level1"}},
		{"level1Boss", generateData{Go: "Level1Boss", Camel: "level1Boss", Snake: "level1_boss", Title: "Level1 Boss"}},
		{"AI", generateData{Go: "AI", Camel: "ai", Snake: "ai", Title: "AI"}},
		{"a", generateData{Go: "A", Camel: "a", Snake: "a", Title: "A"}},
		{"UI_button", generateData{Go: "UIButton", Camel: "uiButton", Snake: "ui_button", Title: "UI Button"}},
	}
	for _, tc := range cases {
		got, err := generateNames(tc.name)
		if err != nil {
			t.Errorf("generateNames(%q): %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("generateNames(%q) = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	for _, bad := range []string{"", "1up", "_x", "x_", "x__y", "x y", "é", "x.y", "-x", "x/y"} {
		if _, err := generateNames(bad); err == nil {
			t.Errorf("generateNames(%q) succeeded", bad)
		}
	}
}

// snakePattern is the form of generateData.Snake: lower-case words of letters and digits joined by _.
var snakePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// FuzzGenerateNames checks that every name generateNames takes gives Go identifiers, a lower camel case key and a snake case file stem, and that it refuses everything generateNamePattern does not match.
func FuzzGenerateNames(f *testing.F) {
	for _, s := range []string{"player_health", "HTTPServer", "level1Boss", "a-b-c", "AI", "x__y", "1up"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		d, err := generateNames(name)
		if !generateNamePattern.MatchString(name) {
			if err == nil {
				t.Fatalf("generateNames(%q) succeeded", name)
			}
			return
		}
		if err != nil {
			t.Fatalf("generateNames(%q): %v", name, err)
		}
		for _, ident := range []string{d.Go, d.Camel, "Test" + d.Go + "Component", d.Camel + "SceneYAML"} {
			if !token.IsIdentifier(ident) {
				t.Fatalf("generateNames(%q) gives %q, not a Go identifier", name, ident)
			}
		}
		if !token.IsExported(d.Go) || token.IsExported(d.Camel) {
			t.Fatalf("generateNames(%q) = %+v: Go must be exported and Camel not", name, d)
		}
		if !snakePattern.MatchString(d.Snake) {
			t.Fatalf("generateNames(%q) gives snake %q", name, d.Snake)
		}
		if again, err := generateNames(d.Snake); err != nil || again.Snake != d.Snake {
			t.Fatalf("generateNames(%q) = %+v, %v; want the snake case unchanged", d.Snake, again, err)
		}
	})
}

func TestGenerateComponent(t *testing.T) {
	dir := generateProject(t, nil)
	code, stdout, stderr := runGenerate(t, dir, "component", "player_health")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	wantOut := []string{"components.go", "player_health_component.go", "player_health_component_test.go"}
	for i := range wantOut {
		wantOut[i] = filepath.Join(dir, wantOut[i])
	}
	if got := strings.Fields(stdout); !slices.Equal(got, wantOut) {
		t.Errorf("stdout lists %q, want %q", got, wantOut)
	}
	src := readFile(t, dir, "player_health_component.go")
	for _, want := range []string{"package main\n", "type PlayerHealth struct{}", `Name: "playerHealth",`, `Doc:  "Player Health",`, "Components.MustRegister(playerHealthComponent)"} {
		if !strings.Contains(src, want) {
			t.Errorf("player_health_component.go lacks %q:\n%s", want, src)
		}
	}
	if !strings.Contains(readFile(t, dir, "components.go"), "var Components = sceneformat.NewComponentRegistry()") {
		t.Error("components.go does not declare Components")
	}

	// A second component registers in the same Components.
	if code, stdout, stderr := runGenerate(t, dir, "component", "Velocity"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	} else if strings.Contains(stdout, "components.go") {
		t.Errorf("wrote components.go again:\n%s", stdout)
	}
	want := []string{"components.go", "game.yaml", "main.go", "player_health_component.go", "player_health_component_test.go", "velocity_component.go", "velocity_component_test.go"}
	if got := listFiles(t, dir); !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
}

func TestGenerateSystem(t *testing.T) {
	dir := generateProject(t, nil)
	if code, _, stderr := runGenerate(t, dir, "system", "Movement"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	src := readFile(t, dir, "movement_system.go")
	for _, want := range []string{"func MovementSystem(w *entities.World, dt time.Duration) {}", `Systems.AddSystem("movement", MovementSystem)`} {
		if !strings.Contains(src, want) {
			t.Errorf("movement_system.go lacks %q:\n%s", want, src)
		}
	}
	if !strings.Contains(readFile(t, dir, "systems.go"), "var Systems = entities.NewScheduler()") {
		t.Error("systems.go does not declare Systems")
	}
	if got, want := listFiles(t, dir), []string{"game.yaml", "main.go", "movement_system.go", "movement_system_test.go", "systems.go"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
}

func TestGenerateScene(t *testing.T) {
	dir := generateProject(t, nil)
	code, stdout, stderr := runGenerate(t, dir, "scene", "level_one")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, project.FileName) {
		t.Errorf("rewrote game.yaml without -autoload:\n%s", stdout)
	}
	if got := readFile(t, dir, project.FileName); got != generateGameYAML {
		t.Errorf("game.yaml changed:\n%s", got)
	}
	scene := readFile(t, dir, "scenes/level_one.scene.yaml")
	want := sceneformat.SchemaCommentV4 + "\nversion: 4\nname: Level One\nvirtualWidth: 480\nvirtualHeight: 640\nlayers:\n  - name: main\n"
	if scene != want {
		t.Errorf("scenes/level_one.scene.yaml =\n%s\nwant\n%s", scene, want)
	}
	checkSceneRoundTrips(t, scene)
	src := readFile(t, dir, "level_one_scene.go")
	for _, want := range []string{"//go:embed scenes/level_one.scene.yaml\nvar levelOneSceneYAML []byte", "func LoadLevelOneScene() (*sceneformat.SceneDoc, error)", `File: "scenes/level_one.scene.yaml", Components: Components`} {
		if !strings.Contains(src, want) {
			t.Errorf("level_one_scene.go lacks %q:\n%s", want, src)
		}
	}
	if got, want := listFiles(t, dir), []string{"components.go", "game.yaml", "level_one_scene.go", "level_one_scene_test.go", "main.go", "scenes/level_one.scene.yaml"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
	checkAssets(t, dir, 1)
}

func TestGenerateSceneTakesTheVirtualResolution(t *testing.T) {
	dir := generateProject(t, map[string]string{project.FileName: generateGameYAML + "display:\n  width: 320\n  height: 180\n"})
	if code, _, stderr := runGenerate(t, dir, "scene", "title"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if scene := readFile(t, dir, "scenes/title.scene.yaml"); !strings.Contains(scene, "virtualWidth: 320\nvirtualHeight: 180\n") {
		t.Errorf("scene does not take the display's size:\n%s", scene)
	}
}

func TestGenerateSceneWithoutProjectFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.go": "package main\n"})
	if code, _, stderr := runGenerate(t, dir, "scene", "title"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	want := sceneformat.SchemaCommentV4 + "\nversion: 4\nname: Title\nlayers:\n  - name: main\n"
	if scene := readFile(t, dir, "scenes/title.scene.yaml"); scene != want {
		t.Errorf("scene =\n%s\nwant\n%s", scene, want)
	}
	if code, _, stderr := runGenerate(t, dir, "-autoload", "scene", "menu"); code != 1 || !strings.Contains(stderr, project.FileName) {
		t.Errorf("-autoload without game.yaml: exit %d, stderr %q", code, stderr)
	}
}

func TestGenerateSceneAutoload(t *testing.T) {
	dir := generateProject(t, map[string]string{"nsgo.yaml": "name: Rocks\nid: dev.nsgo.rocks\nversion: 0.1.0\n"})
	code, stdout, stderr := runGenerate(t, dir, "-autoload", "scene", "level_one")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasSuffix(stdout, filepath.Join(dir, project.FileName)+"\n") {
		t.Errorf("stdout does not end with game.yaml:\n%s", stdout)
	}
	want := generateGameYAML + "autoload:\n  scenes:\n    - name: levelOne\n      path: scenes/level_one.scene.yaml\n"
	if got := readFile(t, dir, project.FileName); got != want {
		t.Errorf("game.yaml =\n%s\nwant\n%s", got, want)
	}

	if code, _, stderr := runGenerate(t, dir, "-main", "scene", "title"); code != 0 {
		t.Fatalf("-main: exit %d: %s", code, stderr)
	}
	f, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if f.Autoload.MainScene != "title" || len(f.Autoload.Scenes) != 2 || f.Autoload.Scenes[1] != (project.Entry{Name: "title", Path: "scenes/title.scene.yaml"}) {
		t.Errorf("autoload = %+v", f.Autoload)
	}
	data := readFile(t, dir, project.FileName)
	if canonical, err := project.Encode(f); err != nil || string(canonical) != data {
		t.Errorf("game.yaml is not canonical (%v):\n%s", err, data)
	}
	if info, err := os.Stat(filepath.Join(dir, project.FileName)); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("game.yaml mode: %v, %v", info.Mode(), err)
	}
	if got := listFiles(t, dir); slices.ContainsFunc(got, func(n string) bool { return strings.HasPrefix(n, ".") }) {
		t.Errorf("left a temporary file: %q", got)
	}
}

func TestGeneratePrefab(t *testing.T) {
	dir := generateProject(t, nil)
	if code, _, stderr := runGenerate(t, dir, "prefab", "Enemy"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	scene := readFile(t, dir, "prefabs/enemy.scene.yaml")
	want := sceneformat.SchemaCommentV4 + "\nversion: 4\nname: Enemy\nlayers:\n  - name: main\n    objects:\n      - id: enemy\n        prefab: enemy\n"
	if scene != want {
		t.Errorf("prefabs/enemy.scene.yaml =\n%s\nwant\n%s", scene, want)
	}
	checkSceneRoundTrips(t, scene)
	src := readFile(t, dir, "enemy_prefab.go")
	for _, want := range []string{"//go:embed prefabs/enemy.scene.yaml\nvar enemyPrefabYAML []byte", `var EnemyPrefab = entities.NewPrefab("enemy")`, "Prefabs[EnemyPrefab.Name()] = EnemyPrefab"} {
		if !strings.Contains(src, want) {
			t.Errorf("enemy_prefab.go lacks %q:\n%s", want, src)
		}
	}
	if got, want := listFiles(t, dir), []string{"components.go", "enemy_prefab.go", "enemy_prefab_test.go", "game.yaml", "main.go", "prefabs.go", "prefabs/enemy.scene.yaml"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}

	// A scene that instances the prefab passes the check too.
	writeFiles(t, dir, map[string]string{"scenes/arena.scene.yaml": sceneformat.SchemaCommentV2 + "\nversion: 2\nlayers:\n  - name: main\n    objects:\n      - id: enemy_1\n        x: 40\n        scene: prefabs/enemy.scene.yaml\n"})
	checkAssets(t, dir, 2)
}

// checkSceneRoundTrips checks that scene parses and encodes back to the same bytes.
func checkSceneRoundTrips(t *testing.T, scene string) {
	t.Helper()
	doc, err := sceneformat.Parse([]byte(scene))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := sceneformat.Encode(doc); err != nil || string(out) != scene {
		t.Errorf("scene does not round-trip (%v):\n%s", err, out)
	}
}

// checkAssets checks that game check finds scenes scene files in dir and no problem with them.
func checkAssets(t *testing.T, dir string, scenes int) {
	t.Helper()
	report, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK() || report.Scenes != scenes {
		t.Errorf("assetcheck: %d scenes, problems %v", report.Scenes, report.Problems)
	}
}

func TestGenerateTakesThePackageName(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"rocks.go":        "package rocks\n",
		"rocks_test.go":   "package rocks_test\n\nfunc Health() {}\n",
		"tool_ignored.go": "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	})
	if code, _, stderr := runGenerate(t, dir, "component", "health"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, name := range []string{"components.go", "health_component.go", "health_component_test.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), name, readFile(t, dir, name), parser.PackageClauseOnly)
		if err != nil || f.Name.Name != "rocks" {
			t.Errorf("%s: package %v, %v; want rocks", name, f.Name, err)
		}
	}
}

func TestGenerateRefusesDeclaredIdentifiers(t *testing.T) {
	cases := []struct {
		files map[string]string
		args  []string
		ident string
	}{
		{map[string]string{"health.go": "package main\n\ntype Health struct{ HP int }\n"}, []string{"component", "health"}, "Health"},
		{map[string]string{"health.go": "package main\n\nvar healthComponent = 1\n"}, []string{"component", "health"}, "healthComponent"},
		{map[string]string{"health_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHealthComponent(*testing.T) {}\n"}, []string{"component", "health"}, "TestHealthComponent"},
		{map[string]string{"move_windows.go": "package main\n\nfunc MoveSystem() {}\n"}, []string{"system", "move"}, "MoveSystem"},
		{map[string]string{"load.go": "package main\n\nconst (\n\tx = 1\n\tLoadTitleScene = 2\n)\n"}, []string{"scene", "title"}, "LoadTitleScene"},
		{map[string]string{"boss.go": "package main\n\nvar a, BossPrefab int\n"}, []string{"prefab", "boss"}, "BossPrefab"},
		{map[string]string{"boss.go": "package main\n\nvar bossPrefabYAML []byte\n"}, []string{"prefab", "boss"}, "bossPrefabYAML"},
	}
	for _, tc := range cases {
		dir := generateProject(t, tc.files)
		before := listFiles(t, dir)
		code, stdout, stderr := runGenerate(t, dir, tc.args...)
		if code != 1 || !strings.Contains(stderr, "already declares "+tc.ident+",") {
			t.Errorf("%v: exit %d, stderr %q; want it to name %s", tc.args, code, stderr, tc.ident)
		}
		if stdout != "" {
			t.Errorf("%v: stdout %q", tc.args, stdout)
		}
		if after := listFiles(t, dir); !slices.Equal(after, before) {
			t.Errorf("%v: wrote %q", tc.args, after)
		}
	}
}

func TestGenerateRefusesARegistryThatIsNotAVariable(t *testing.T) {
	cases := []struct {
		files map[string]string
		args  []string
		want  string
	}{
		{map[string]string{"systems.go": "package main\n\ntype Systems struct{}\n"}, []string{"system", "move"}, "declares Systems as a type"},
		{map[string]string{"components.go": "package main\n\nfunc Components() {}\n"}, []string{"scene", "title"}, "declares Components as a func"},
		{map[string]string{"prefabs.go": "package main\n\nconst Prefabs = 1\n"}, []string{"prefab", "enemy"}, "declares Prefabs as a const"},
	}
	for _, tc := range cases {
		dir := generateProject(t, tc.files)
		before := listFiles(t, dir)
		if code, _, stderr := runGenerate(t, dir, tc.args...); code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want 1 and %q", tc.args, code, stderr, tc.want)
		}
		if after := listFiles(t, dir); !slices.Equal(after, before) {
			t.Errorf("%v: wrote %q", tc.args, after)
		}
	}
}

func TestGenerateRefusesIdentifiersItsImportsShadow(t *testing.T) {
	cases := []struct {
		files map[string]string
		args  []string
		want  string
	}{
		{map[string]string{"x.go": "package main\n\nvar entities = 1\n"}, []string{"system", "move"}, "declares entities, which systems.go imports"},
		{map[string]string{"x.go": "package main\n\nfunc time() {}\n"}, []string{"system", "move"}, "declares time, which move_system.go imports"},
		{map[string]string{"x.go": "package main\n\ntype sceneformat struct{}\n"}, []string{"component", "health"}, "declares sceneformat, which components.go imports"},
		{map[string]string{"x_test.go": "package main\n\nvar slices = 1\n"}, []string{"system", "move"}, "declares slices, which move_system_test.go imports"},
	}
	for _, tc := range cases {
		dir := generateProject(t, tc.files)
		before := listFiles(t, dir)
		if code, _, stderr := runGenerate(t, dir, tc.args...); code != 1 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want 1 and %q", tc.args, code, stderr, tc.want)
		}
		if after := listFiles(t, dir); !slices.Equal(after, before) {
			t.Errorf("%v: wrote %q", tc.args, after)
		}
	}
}

func TestGenerateRemovesWhatItWroteWhenGameYAMLCannotBeReplaced(t *testing.T) {
	replaceProjectFile = func(string, []byte) error { return errors.New("disk full") }
	t.Cleanup(func() { replaceProjectFile = replaceFile })

	dir := generateProject(t, nil)
	code, stdout, stderr := runGenerate(t, dir, "-main", "scene", "title")
	if code != 1 || !strings.Contains(stderr, "disk full") || stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := listFiles(t, dir); !slices.Equal(got, []string{"game.yaml", "main.go"}) {
		t.Errorf("left %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "scenes")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("left the scenes directory: %v", err)
	}
	if got := readFile(t, dir, project.FileName); got != generateGameYAML {
		t.Errorf("game.yaml changed:\n%s", got)
	}

	// A scenes directory that was there before stays, with what it held.
	dir = generateProject(t, map[string]string{"scenes/menu.scene.yaml": "version: 2\n"})
	if code, _, stderr := runGenerate(t, dir, "-autoload", "scene", "title"); code != 1 || !strings.Contains(stderr, "disk full") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if got := listFiles(t, dir); !slices.Equal(got, []string{"game.yaml", "main.go", "scenes/menu.scene.yaml"}) {
		t.Errorf("left %q", got)
	}
}

func TestGenerateRefusesExistingFiles(t *testing.T) {
	dir := generateProject(t, map[string]string{"scenes/title.scene.yaml": "version: 2\n"})
	before := readFile(t, dir, project.FileName)
	code, _, stderr := runGenerate(t, dir, "-main", "scene", "title")
	if code != 1 || !strings.Contains(stderr, filepath.Join(dir, "scenes", "title.scene.yaml")+" exists") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if got, want := listFiles(t, dir), []string{"game.yaml", "main.go", "scenes/title.scene.yaml"}; !slices.Equal(got, want) {
		t.Errorf("files %q, want %q", got, want)
	}
	if readFile(t, dir, project.FileName) != before {
		t.Error("game.yaml changed")
	}
}

func TestGenerateAutoloadRefusals(t *testing.T) {
	// A game.yaml that is not canonical would lose its comments.
	dir := generateProject(t, map[string]string{project.FileName: "# Rocks\n" + generateGameYAML})
	if code, _, stderr := runGenerate(t, dir, "-autoload", "scene", "title"); code != 1 || !strings.Contains(stderr, "run game project fmt first") {
		t.Errorf("non-canonical game.yaml: exit %d, stderr %q", code, stderr)
	}
	if got := listFiles(t, dir); !slices.Equal(got, []string{"game.yaml", "main.go"}) {
		t.Errorf("wrote %q", got)
	}

	// The autoload name is taken by another scene.
	dir = generateProject(t, map[string]string{
		project.FileName:   generateGameYAML + "autoload:\n  scenes:\n    - name: title\n      path: menus/title.yaml\n",
		"menus/title.yaml": "version: 2\n",
	})
	if code, _, stderr := runGenerate(t, dir, "-autoload", "scene", "title"); code != 1 || !strings.Contains(stderr, "already autoloads a scene called title") {
		t.Errorf("taken autoload name: exit %d, stderr %q", code, stderr)
	}

	// An invalid game.yaml.
	dir = generateProject(t, map[string]string{project.FileName: "version: 1\nname: Rocks\ncolour: red\n"})
	if code, _, stderr := runGenerate(t, dir, "scene", "title"); code != 1 || !strings.Contains(stderr, project.FileName) {
		t.Errorf("invalid game.yaml: exit %d, stderr %q", code, stderr)
	}
}

func TestGenerateWithoutAGoPackage(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{project.FileName: generateGameYAML})
	if code, _, stderr := runGenerate(t, dir, "system", "move"); code != 1 || !strings.Contains(stderr, "holds no Go package") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestGenerateUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "expected a kind"},
		{[]string{"scene"}, "expected a kind"},
		{[]string{"scene", "a", "b"}, "expected a kind"},
		{[]string{"level", "one"}, `unknown kind "level"`},
		{[]string{"scene", "level one"}, `name "level one"`},
		{[]string{"scene", "1up"}, `name "1up"`},
		{[]string{"-autoload", "component", "health"}, "-autoload and -main are for a scene"},
		{[]string{"-main", "prefab", "enemy"}, "-autoload and -main are for a scene"},
		{[]string{"component", "components"}, "the registry game generate declares for the game's components"},
		{[]string{"component", "systems"}, "the registry game generate declares for the game's systems"},
		{[]string{"system", "Prefabs"}, "the registry game generate declares for the game's prefabs"},
	}
	for _, tc := range cases {
		dir := generateProject(t, nil)
		code, _, stderr := runGenerate(t, dir, tc.args...)
		if code != 2 || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "usage: game generate [flags] component|system|scene|prefab <name>") {
			t.Errorf("%q: exit %d, stderr %q; want 2 and %q with the usage", tc.args, code, stderr, tc.want)
		}
		if got := listFiles(t, dir); !slices.Equal(got, []string{"game.yaml", "main.go"}) {
			t.Errorf("%q wrote %q", tc.args, got)
		}
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "game.yaml")
	if err := os.WriteFile(name, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(name, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dir, "game.yaml"); got != "new" {
		t.Errorf("content %q", got)
	}
	if info, err := os.Stat(name); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v; want 0600 kept", info.Mode(), err)
	}
	if got := listFiles(t, dir); !slices.Equal(got, []string{"game.yaml"}) {
		t.Errorf("files %q", got)
	}
	if err := replaceFile(filepath.Join(dir, "missing.yaml"), nil); err == nil {
		t.Error("replaced a file that does not exist")
	}
}

// TestGeneratedCodeBuildsAndPassesCheck is the acceptance test of game generate: in a project game new creates, pointed at this checkout of the game module as TestNewProjectBuildsAndPassesItsTests points it, a component, a system, a scene made the main scene and a prefab build, pass go vet and their generated tests and the starter's, and their files pass game check.
func TestGeneratedCodeBuildsAndPassesCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a project")
	}
	gameDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", "-module", "example.com/mygame", dir}, a); code != 0 {
		t.Fatalf("game new: exit %d: %s", code, stderr)
	}
	for _, args := range [][]string{
		{"component", "velocity"},
		{"component", "PlayerHealth"},
		{"system", "movement"},
		{"system", "damage"},
		{"-main", "scene", "level_one"},
		{"-autoload", "scene", "title"},
		{"prefab", "enemy"},
	} {
		if code, _, stderr := runGenerate(t, dir, args...); code != 0 {
			t.Fatalf("game generate %v: exit %d: %s", args, code, stderr)
		}
	}
	// The scene sets the components and instances the prefab, as an author would next.
	scene := sceneformat.SchemaCommentV2 + "\nversion: 2\nname: Level One\nvirtualWidth: 480\nvirtualHeight: 640\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        components:\n          playerHealth: {}\n          velocity: {}\n      - id: enemy_1\n        x: 40\n        scene: prefabs/enemy.scene.yaml\n"
	writeFiles(t, dir, map[string]string{"scenes/level_one.scene.yaml": scene})

	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("game check: exit %d: %s", code, stderr)
	}
	if got, want := stdout.String(), "3 scenes, 0 maps, 0 tilesets, 0 atlases: no problems\n"; got != want {
		t.Errorf("game check: %q, want %q", got, want)
	}

	env := append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	goCmd := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	goCmd("mod", "edit", "-replace="+gameModule+"="+gameDir)
	goCmd("vet", ".")
	tests := []string{"TestVelocityComponent", "TestPlayerHealthComponent", "TestMovementSystem", "TestDamageSystem", "TestLoadLevelOneScene", "TestLoadTitleScene", "TestEnemyPrefab", "TestProjectFileIsValidAndCanonical", "TestAutoloadsLoad"}
	out := goCmd("tool", "nsgo", "test", "-run", "^("+strings.Join(tests, "|")+")$", ".", "-count=1")
	if n, ok := passedTests([]byte(out)); !ok || n != len(tests) {
		t.Errorf("%d tests passed, want %d:\n%s", n, len(tests), out)
	}
	goCmd("tool", "nsgo", "test", ".")
}
