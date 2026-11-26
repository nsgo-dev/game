package gamecli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
)

func TestGenerateEditor(t *testing.T) {
	dir := generateProject(t, nil)
	code, stdout, stderr := runGenerate(t, dir, "editor")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	wantOut := []string{"components.go", "editor.go", "editor_test.go", "main.go"}
	for i := range wantOut {
		wantOut[i] = filepath.Join(dir, wantOut[i])
	}
	if got := strings.Fields(stdout); !slices.Equal(got, wantOut) {
		t.Errorf("stdout lists %q, want %q", got, wantOut)
	}
	if got, want := readFile(t, dir, "main.go"), "//go:build !gameeditor\n\n// Command rocks is a game.\npackage main\n\nfunc main() {}\n"; got != want {
		t.Errorf("main.go:\n%s\nwant:\n%s", got, want)
	}
	src := readFile(t, dir, "editor.go")
	for _, want := range []string{"//go:build gameeditor\n\npackage main\n", "var editorPlugins []editor.Plugin", "editorcmd.Main(editorOptions())", "editorcmd.Options{Components: Components, Plugins: editorPlugins}"} {
		if !strings.Contains(src, want) {
			t.Errorf("editor.go lacks %q:\n%s", want, src)
		}
	}
	if test := readFile(t, dir, "editor_test.go"); !strings.HasPrefix(test, "//go:build gameeditor\n") || !strings.Contains(test, "func TestEditorOpensTheProject(") {
		t.Errorf("editor_test.go:\n%s", test)
	}

	// The editor command game editor runs is the tagged main package.
	run, ok, err := projectEditor(dir)
	if err != nil || !ok || !slices.Equal(run, []string{"run", "-tags", "gameeditor", "."}) {
		t.Fatalf("projectEditor = %q, %t, %v", run, ok, err)
	}

	// A project has one editor command.
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 1 || !strings.Contains(stderr, "already names the gameeditor tag") {
		t.Errorf("a second editor: exit %d: %s", code, stderr)
	}
}

func TestGenerateEditorKeepsMainsBuildConstraint(t *testing.T) {
	dir := generateProject(t, map[string]string{
		"main.go": "//go:build linux || darwin\n\n// Command rocks is a game.\npackage main\n\nfunc main() {}\n",
		"util.go": "package main\n\nfunc helper() {}\n",
	})
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got, want := readFile(t, dir, "main.go"), "//go:build (linux || darwin) && !gameeditor\n\n// Command rocks is a game.\npackage main\n\nfunc main() {}\n"; got != want {
		t.Errorf("main.go:\n%s\nwant:\n%s", got, want)
	}
	if got := readFile(t, dir, "util.go"); got != "package main\n\nfunc helper() {}\n" {
		t.Errorf("rewrote util.go:\n%s", got)
	}
}

func TestGenerateEditorLeavesOutAMainFilesOwnHelpers(t *testing.T) {
	dir := generateProject(t, map[string]string{
		"main.go": "package main\n\nimport \"os\"\n\nconst title = \"Rocks\"\n\ntype flags struct{ verbose bool }\n\nfunc (f flags) on() bool { return f.verbose }\n\nfunc run(f flags) int {\n\tif f.on() {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc main() { _ = title; os.Exit(run(flags{})) }\n",
		// A field called run, declared and read after a dot, is not the main file's run.
		"game.go": "package main\n\ntype game struct{ run int }\n\nfunc (g game) steps() int { return g.run }\n",
	})
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := readFile(t, dir, "main.go"); !strings.HasPrefix(got, "//go:build !gameeditor\n") {
		t.Errorf("main.go:\n%s", got)
	}
}

func TestGenerateEditorLeavesOutTheMainOfEveryPlatform(t *testing.T) {
	dir := generateProject(t, map[string]string{
		"main.go":         "",
		"main_windows.go": "// Command rocks is a game.\npackage main\n\nfunc main() {}\n",
		"main_unix.go":    "//go:build !windows\n\npackage main\n\nfunc main() {}\n",
		"gen.go":          "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	})
	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for name, want := range map[string]string{
		"main_windows.go": "//go:build !gameeditor\n\n// Command rocks is a game.\npackage main\n\nfunc main() {}\n",
		"main_unix.go":    "//go:build !windows && !gameeditor\n\npackage main\n\nfunc main() {}\n",
		"gen.go":          "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	} {
		if got := readFile(t, dir, name); got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
}

func TestGenerateEditorPutsBackTheMainFilesItRewroteWhenOneFails(t *testing.T) {
	dir := generateProject(t, map[string]string{
		"main.go":         "",
		"main_windows.go": "package main\n\nfunc main() {}\n",
		"main_unix.go":    "//go:build !windows\n\npackage main\n\nfunc main() {}\n",
	})
	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, dir)
	unix, windows := readFile(t, dir, "main_unix.go"), readFile(t, dir, "main_windows.go")
	// The first rewrite succeeds and the second fails.
	n := 0
	replaceProjectFile = func(p string, data []byte) error {
		if n++; n > 1 {
			return errors.New("disk full")
		}
		return replaceFile(p, data)
	}
	t.Cleanup(func() { replaceProjectFile = replaceFile })
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 1 || !strings.Contains(stderr, "disk full") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := listFiles(t, dir); !slices.Equal(got, before) || readFile(t, dir, "main_unix.go") != unix || readFile(t, dir, "main_windows.go") != windows {
		t.Errorf("a failed generate changed the project: %q", got)
	}
}

func TestGenerateEditorRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		args  []string
		code  int
		want  string
	}{
		{"name", nil, []string{"editor", "tools"}, 2, "editor takes no name"},
		{"library package", map[string]string{"main.go": "package rocks\n"}, []string{"editor"}, 1, "is not a main package"},
		{"no main function", map[string]string{"main.go": "package main\n\nfunc init() {}\n"}, []string{"editor"}, 1, "declares no main function"},
		{"plus build", map[string]string{"main.go": "// +build !plan9\n\npackage main\n\nfunc main() {}\n"}, []string{"editor"}, 1, "+build"},
		{"tagged already", map[string]string{"main.go": "//go:build !gameeditor\n\npackage main\n\nfunc main() {}\n"}, []string{"editor"}, 1, "already names the gameeditor tag"},
		{"declared", map[string]string{"plugins.go": "package main\n\nvar editorPlugins = 1\n"}, []string{"editor"}, 1, "already declares editorPlugins"},
		{"flag", nil, []string{"-main", "editor"}, 2, "-autoload and -main are for a scene"},
		{"main declares a name another file uses", map[string]string{"main.go": "package main\n\nvar Root = \"root\"\n\nfunc main() {}\n", "game.go": "package main\n\nfunc root() string { return Root }\n"}, []string{"editor"}, 1, "declares main and also Root"},
		{"main declares a type another file uses", map[string]string{"main.go": "package main\n\ntype level struct{}\n\nfunc main() {}\n", "game.go": "package main\n\nvar _ level\n"}, []string{"editor"}, 1, "declares main and also level"},
		{"main declares an init function", map[string]string{"main.go": "package main\n\nfunc init() {}\n\nfunc main() {}\n"}, []string{"editor"}, 1, "declares main and also an init function"},
		{"main declares a method on another file's type", map[string]string{"main.go": "package main\n\nfunc (g *game) Name() string { return \"\" }\n\nfunc main() {}\n", "game.go": "package main\n\ntype game struct{}\n"}, []string{"editor"}, 1, "the method game.Name"},
		{"a test uses a name main declares", map[string]string{"main.go": "package main\n\nfunc helper() {}\n\nfunc main() { helper() }\n", "main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHelper(t *testing.T) { helper() }\n"}, []string{"editor"}, 1, "declares main and also helper"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := generateProject(t, tc.files)
			before := listFiles(t, dir)
			mainGo := readFile(t, dir, "main.go")
			code, _, stderr := runGenerate(t, dir, tc.args...)
			if code != tc.code || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d: %s; want exit %d with %q", code, stderr, tc.code, tc.want)
			}
			if got := listFiles(t, dir); !slices.Equal(got, before) || readFile(t, dir, "main.go") != mainGo {
				t.Errorf("a refused generate changed the project: %q", got)
			}
		})
	}
}

func TestGenerateEditorRemovesWhatItWroteWhenMainCannotBeReplaced(t *testing.T) {
	dir := generateProject(t, nil)
	before := listFiles(t, dir)
	replaceProjectFile = func(string, []byte) error { return errors.New("disk full") }
	t.Cleanup(func() { replaceProjectFile = replaceFile })
	if code, _, stderr := runGenerate(t, dir, "editor"); code != 1 || !strings.Contains(stderr, "disk full") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if got := listFiles(t, dir); !slices.Equal(got, before) {
		t.Errorf("files %q, want %q", got, before)
	}
}

// TestGeneratedEditorChecksTheProjectsComponents is the acceptance test of a game's own editor: in a project game new creates, pointed at this checkout of the game module, with a component, a main scene and the editor game generate writes, game editor runs the project's editor command, which builds and passes go vet, and that editor opens the project and refuses a field the generated component does not declare.
func TestGeneratedEditorChecksTheProjectsComponents(t *testing.T) {
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
	for _, args := range [][]string{{"component", "health"}, {"-main", "scene", "ward"}, {"editor"}} {
		if code, _, stderr := runGenerate(t, dir, args...); code != 0 {
			t.Fatalf("game generate %v: exit %d: %s", args, code, stderr)
		}
	}
	writeFiles(t, dir, map[string]string{
		"scenes/ward.scene.yaml": "version: 2\nname: Ward\nlayers:\n  - name: main\n    objects:\n      - id: ada\n        components:\n          health: {}\n",
		"scenes/bad.scene.yaml":  "version: 2\nname: Bad\nlayers:\n  - name: main\n    objects:\n      - id: bob\n        components:\n          health: {hp: 3}\n",
		"scenes/body.scene.yaml": "version: 2\nname: Body\nlayers:\n  - name: main\n    objects:\n      - id: crate\n        components:\n          health: {}\n          body2d: {kind: static}\n",
		// The test opens both scenes through the editor the project builds, as game editor runs it.
		"editor_fields_test.go": `//go:build gameeditor

package main

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor/editorcmd"
)

func TestEditorChecksComponentFields(t *testing.T) {
	ed, err := editorcmd.New(editorOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := editorcmd.Open(ed, []string{"scenes/bad.scene.yaml"}); err == nil || !strings.Contains(err.Error(), "\"hp\"") {
		t.Fatalf("opened a scene setting a field health does not declare: %v", err)
	}
	if err := editorcmd.Open(ed, []string{"scenes/body.scene.yaml"}); err != nil {
		t.Fatalf("a scene with a built-in physics body: %v", err)
	}
	if err := editorcmd.Open(ed, []string{"scenes/ward.scene.yaml"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ed.ComponentRegistry().Lookup("health"); !ok || ed.Project == nil || ed.ScenePath != "scenes/ward.scene.yaml" {
		t.Fatalf("opened %q in project %v with registry %v", ed.ScenePath, ed.Project != nil, ed.ComponentRegistry())
	}
}
`,
	})

	// game editor runs the project's own editor on the project, from its directory.
	a, _, stderr = newTestApp(t)
	var ran []string
	a.goRun = func(d string, args []string, _, _ io.Writer) error {
		if d != dir {
			t.Errorf("ran the editor in %s, want %s", d, dir)
		}
		ran = args
		return nil
	}
	a.launch = func(*editor.Editor) { t.Error("game editor showed its own editor") }
	if code := run([]string{"editor", dir}, a); code != 0 {
		t.Fatalf("game editor: exit %d: %s", code, stderr)
	}
	if want := []string{"run", "-tags", "gameeditor", ".", dir}; !slices.Equal(ran, want) {
		t.Fatalf("game editor ran go %q, want go %q", ran, want)
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
	// The command game editor runs builds, in place of the game, which still builds as it did.
	exe := filepath.Join(t.TempDir(), "editor")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	goCmd("build", "-tags", "gameeditor", "-o", exe, ".")
	goCmd("vet", "-tags", "gameeditor", ".")
	goCmd("vet", ".")
	tests := []string{"TestEditorOpensTheProject", "TestEditorChecksComponentFields", "TestHealthComponent"}
	out := goCmd("tool", "nsgo", "test", "-run", "^("+strings.Join(tests, "|")+")$", ".", "-tags", "gameeditor", "-count=1")
	if n, ok := passedTests([]byte(out)); !ok || n != len(tests) {
		t.Errorf("%d tests passed, want %d:\n%s", n, len(tests), out)
	}
}
