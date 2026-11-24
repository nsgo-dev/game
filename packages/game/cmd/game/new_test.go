package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/project"
	"go.yaml.in/yaml/v3"
)

// stubGo replaces runGo for the test with one that records each call's directory and arguments and returns err.
func stubGo(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	saved := runGo
	runGo = func(dir string, _, _ io.Writer, args ...string) error {
		calls = append(calls, append([]string{dir}, args...))
		return err
	}
	t.Cleanup(func() { runGo = saved })
	return &calls
}

// starterNames are the files game new writes from the starter, in the order it writes them.
func starterNames(t *testing.T) []string {
	t.Helper()
	var names []string
	err := fs.WalkDir(projectTemplates[0].files, ".", func(name string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, strings.TrimSuffix(name, ".tmpl"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// readYAML decodes the YAML file at path into a map.
func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

// checkProjectFile checks that the project in dir has a game.yaml naming the game name with id, in the form gameproject fmt writes, and that its nsgo.yaml agrees with it, as the starter's own TestProjectFileIsValidAndCanonical does.
func checkProjectFile(t *testing.T, dir, name, id string) {
	t.Helper()
	f, err := project.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if f.Name != name || f.ID != id {
		t.Errorf("%s: game.yaml names %q with id %q, want %q, %q", dir, f.Name, f.ID, name, id)
	}
	src, err := os.ReadFile(filepath.Join(dir, project.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if canonical, err := project.Encode(f); err != nil || !bytes.Equal(canonical, src) {
		t.Errorf("%s: game.yaml is not canonical (%v):\n%s", dir, err, canonical)
	}
}

func TestNewCreatesStarter(t *testing.T) {
	calls := stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "mygame")
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", "-version", "v1.2.3", dir}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	names := starterNames(t)
	for _, want := range []string{".gitignore", "go.mod", "go.sum", "game.yaml", "nsgo.yaml", "main.go", "game.go", "game_test.go", "README.md"} {
		if !slices.Contains(names, want) {
			t.Errorf("starter has no %s: %q", want, names)
		}
	}
	var listed []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Error(err)
		}
		listed = append(listed, path)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod.tmpl")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("go.mod.tmpl was copied: %v", err)
	}
	want := strings.Join(listed, "\n") + "\n\ncd " + shellWord(dir, runtime.GOOS) + " && go tool nsgo dev\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(mod), "module mygame\n") || strings.Contains(string(mod), "{{") {
		t.Errorf("go.mod:\n%s", mod)
	}
	cfg := readYAML(t, filepath.Join(dir, "nsgo.yaml"))
	if cfg["name"] != "mygame" || cfg["id"] != "dev.nsgo.mygame" || cfg["game"] != true {
		t.Errorf("nsgo.yaml = %v", cfg)
	}
	checkProjectFile(t, dir, "mygame", "dev.nsgo.mygame")

	// Every other file is the starter's, byte for byte.
	for _, name := range []string{"game.go", "go.sum", ".gitignore"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src, err := fs.ReadFile(projectTemplates[0].files, name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, src) {
			t.Errorf("%s differs from the starter's", name)
		}
	}

	wantCalls := [][]string{
		{dir, "get", "-tool", "github.com/nsgo-dev/game/packages/game/cmd/game@v1.2.3"},
		{dir, "mod", "tidy"},
	}
	if !slices.EqualFunc(*calls, wantCalls, slices.Equal) {
		t.Errorf("go calls = %q, want %q", *calls, wantCalls)
	}
}

func TestNewNameAndModule(t *testing.T) {
	cases := []struct {
		dir, name, module      string
		wantName, wantID, want string
	}{
		{"rocks", "Space Rocks", "example.com/rocks", "Space Rocks", "dev.nsgo.spacerocks", "example.com/rocks"},
		{"Space Rocks", "", "", "Space Rocks", "dev.nsgo.spacerocks", "space-rocks"},
		{"2048.clone", "", "", "clone", "dev.nsgo.clone", "clone"},
		{"my_game", "", "github.com/me/my_game.v2", "my_game", "dev.nsgo.mygame", "github.com/me/my_game.v2"},
		// A name YAML would read as a boolean is quoted so that it stays a string.
		{"yes", "True", "", "True", "dev.nsgo.true", "true"},
		// A standard-library name needs a module path of its own.
		{"time", "", "example.com/time", "time", "dev.nsgo.time", "example.com/time"},
	}
	for _, tc := range cases {
		stubGo(t, nil)
		dir := filepath.Join(t.TempDir(), tc.dir)
		args := []string{"new"}
		if tc.name != "" {
			args = append(args, "-name", tc.name)
		}
		if tc.module != "" {
			args = append(args, "-module", tc.module)
		}
		a, _, stderr := newTestApp(t)
		if code := run(append(args, dir), a); code != 0 {
			t.Fatalf("%q exit %d: %s", args, code, stderr)
		}
		cfg := readYAML(t, filepath.Join(dir, "nsgo.yaml"))
		if cfg["name"] != tc.wantName || cfg["id"] != tc.wantID {
			t.Errorf("%s: name %#v, id %#v, want %q, %q", tc.dir, cfg["name"], cfg["id"], tc.wantName, tc.wantID)
		}
		checkProjectFile(t, dir, tc.wantName, tc.wantID)
		mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(string(mod), "\n"); first != "module "+tc.want {
			t.Errorf("%s: go.mod starts %q, want module %s", tc.dir, first, tc.want)
		}
	}
}

func TestNewKeepsNsgoYAMLComments(t *testing.T) {
	stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", dir}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "nsgo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := fs.ReadFile(projectTemplates[0].files, "nsgo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	gotLines, srcLines := strings.Split(string(got), "\n"), strings.Split(string(src), "\n")
	if len(gotLines) != len(srcLines) {
		t.Fatalf("nsgo.yaml has %d lines, the starter's %d", len(gotLines), len(srcLines))
	}
	for i := range gotLines {
		if gotLines[i] != srcLines[i] && !strings.HasPrefix(srcLines[i], "name: ") && !strings.HasPrefix(srcLines[i], "id: ") {
			t.Errorf("line %d = %q, want the starter's %q", i+1, gotLines[i], srcLines[i])
		}
	}
}

func TestNewDefaultVersion(t *testing.T) {
	calls := stubGo(t, nil)
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", dir}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := gameTool + "@" + toolVersion(); (*calls)[0][3] != want {
		t.Errorf("go get %s, want %s", (*calls)[0][3], want)
	}
}

func TestNewRefusesExistingFiles(t *testing.T) {
	calls := stubGo(t, nil)
	dir := t.TempDir()
	mine := []byte("package main // mine\n")
	if err := os.WriteFile(filepath.Join(dir, "game.go"), mine, 0o644); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", "-name", "Mine", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := filepath.Join(dir, "game.go") + " exists"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("wrote files beside game.go: %v", entries)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "game.go")); !bytes.Equal(got, mine) {
		t.Errorf("game.go changed to %q", got)
	}
	if len(*calls) != 0 || stdout.Len() != 0 {
		t.Errorf("ran go %q, wrote %q", *calls, stdout)
	}
}

func TestNewIntoEmptyDirectory(t *testing.T) {
	stubGo(t, nil)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "My Game"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"new", filepath.Join(dir, "My Game")}, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if cfg := readYAML(t, filepath.Join(dir, "My Game", "nsgo.yaml")); cfg["name"] != "My Game" {
		t.Errorf("name = %v", cfg["name"])
	}
	// The closing hint quotes the directory, so it can be pasted into a shell.
	if want := "\ncd " + shellWord(filepath.Join(dir, "My Game"), runtime.GOOS) + " && go tool nsgo dev\n"; !strings.HasSuffix(stdout.String(), want) || !strings.ContainsAny(want, `'"`) {
		t.Errorf("stdout = %q, want it to end with %q", stdout, want)
	}
}

func TestNewReportsGoFailure(t *testing.T) {
	stubGo(t, errors.New("exit status 1"))
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := "game new: go get -tool " + gameTool + "@"; !strings.HasPrefix(stderr.String(), want) || !strings.Contains(stderr.String(), "in "+dir+": exit status 1") || !strings.Contains(stderr.String(), " && go mod tidy there") {
		t.Errorf("stderr = %q", stderr)
	}
	// The files stay, so the go command can be run there again.
	if _, err := os.Stat(filepath.Join(dir, "nsgo.yaml")); err != nil {
		t.Error(err)
	}
}

func TestNewReportsGoModTidyFailure(t *testing.T) {
	var calls [][]string
	saved := runGo
	runGo = func(_ string, _, _ io.Writer, args ...string) error {
		calls = append(calls, args)
		if args[0] == "mod" {
			return errors.New("exit status 1")
		}
		return nil
	}
	t.Cleanup(func() { runGo = saved })
	dir := filepath.Join(t.TempDir(), "mygame")
	a, _, stderr := newTestApp(t)
	if code := run([]string{"new", "-version", "v1.2.3", dir}, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	// go get has succeeded, so only go mod tidy is left to run.
	if want := "game new: go mod tidy in " + dir + ": exit status 1; the project's files are written, so run go mod tidy there once it can succeed\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if len(calls) != 2 {
		t.Errorf("go calls = %q", calls)
	}
}

func TestShellWord(t *testing.T) {
	cases := []struct {
		s, goos, want string
	}{
		{"mygame", "darwin", "mygame"},
		{"/home/me/games/my-game_2.0", "linux", "/home/me/games/my-game_2.0"},
		{"/Users/me/My Game", "darwin", "'/Users/me/My Game'"},
		{"it's", "linux", `'it'\''s'`},
		{"a$b", "linux", "'a$b'"},
		{"", "linux", "''"},
		{`C:\Users\me\mygame`, "windows", `C:\Users\me\mygame`},
		{`C:\Users\me\My Game`, "windows", `"C:\Users\me\My Game"`},
		{`C:\mygame`, "linux", `'C:\mygame'`},
	}
	for _, tc := range cases {
		if got := shellWord(tc.s, tc.goos); got != tc.want {
			t.Errorf("shellWord(%q, %q) = %s, want %s", tc.s, tc.goos, got, tc.want)
		}
	}
}

func TestNewUsageErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"new"}, "game new: a directory is required"},
		{[]string{"new", "a", "b"}, `game new: unexpected arguments ["b"]`},
		{[]string{"new", "-template", "rpg", "a"}, `game new: no template "rpg"; use starter`},
		{[]string{"new", "-name", "9lives", "a"}, `game new: -name "9lives" is not a game name`},
		{[]string{"new", "-name", "a/b", "a"}, `game new: -name "a/b" is not a game name`},
		{[]string{"new", filepath.Join(dir, "2048")}, `game new: cannot name a game after "2048"; give one with -name`},
		{[]string{"new", "-module", "example.com/my game", "a"}, `game new: -module "example.com/my game": element "my game" holds ' '`},
		{[]string{"new", "-module", "example.com/x\nreplace y => ../y", "a"}, `game new: -module`},
		{[]string{"new", "-module", "-x", "a"}, `game new: -module "-x": starts with -`},
		{[]string{"new", "-module", "example.com//x", "a"}, "has an empty path element"},
		{[]string{"new", "-module", "example.com/.x", "a"}, "starts or ends with a dot"},
		{[]string{"new", "-version", "v1 v2", "a"}, `game new: -version "v1 v2" is not a module version`},
		{[]string{"new", "-version", "-u", "a"}, `game new: -version "-u" is not a module version`},
		// A main module named after a standard-library package cannot build.
		{[]string{"new", filepath.Join(dir, "time")}, `game new: module path "time" is a standard-library package, so the project could not build; give another with -module, such as example.com/time`},
		{[]string{"new", "-name", "Image", "a"}, `game new: module path "image" is a standard-library package`},
		{[]string{"new", "-module", "encoding/json", "a"}, `game new: module path "encoding/json" is a standard-library package`},
	}
	for _, tc := range cases {
		calls := stubGo(t, nil)
		a, stdout, stderr := newTestApp(t)
		if code := run(tc.args, a); code != 2 {
			t.Errorf("%q exit %d, want 2", tc.args, code)
		}
		if !strings.Contains(stderr.String(), tc.want) || !strings.Contains(stderr.String(), "usage: game new [flags] <dir>") {
			t.Errorf("%q stderr = %q, want %q and the usage", tc.args, stderr, tc.want)
		}
		if stdout.Len() != 0 || len(*calls) != 0 {
			t.Errorf("%q wrote %q and ran go %q", tc.args, stdout, *calls)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("usage errors wrote %v", entries)
	}
}

func TestBuildVersion(t *testing.T) {
	game := func(version string, replace *debug.Module) *debug.Module {
		return &debug.Module{Path: gameModule, Version: version, Replace: replace}
	}
	other := debug.Module{Path: "example.com/mygame", Version: "(devel)"}
	cases := []struct {
		name string
		info debug.BuildInfo
		want string
	}{
		{"go run @version", debug.BuildInfo{Main: *game("v0.4.0", nil)}, "v0.4.0"},
		{"pseudo-version", debug.BuildInfo{Main: *game("v0.0.0-20261124000000-0123456789ab", nil)}, "v0.0.0-20261124000000-0123456789ab"},
		{"checkout", debug.BuildInfo{Main: *game("(devel)", nil)}, "latest"},
		{"modified checkout", debug.BuildInfo{Main: *game("v0.4.1-0.20261124000000-0123456789ab+dirty", nil)}, "latest"},
		{"no version", debug.BuildInfo{Main: *game("", nil)}, "latest"},
		{"go tool game", debug.BuildInfo{Main: other, Deps: []*debug.Module{{Path: "github.com/nsgo-dev/nsgo", Version: "v0.3.0"}, game("v0.4.0", nil)}}, "v0.4.0"},
		{"replaced", debug.BuildInfo{Main: other, Deps: []*debug.Module{game("v0.4.0", &debug.Module{Path: "../game"})}}, "latest"},
		{"absent", debug.BuildInfo{Main: other}, "latest"},
	}
	for _, tc := range cases {
		if got := buildVersion(&tc.info); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSetYAMLScalar(t *testing.T) {
	src := "# name: in a comment\nname: Old # keep\nnames: [a]\nid: x\nnested:\n  name: inner\n"
	got, err := setYAMLScalar([]byte(src), "name", "New Name")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# name: in a comment\nname: New Name\nnames: [a]\nid: x\nnested:\n  name: inner\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, err := setYAMLScalar([]byte("id: x"), "id", "null"); err != nil || string(got) != `id: "null"` {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := setYAMLScalar([]byte("name: a\nname: b\n"), "name", "c"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("duplicate key: %v", err)
	}
	if _, err := setYAMLScalar([]byte("title: a\n"), "name", "c"); err == nil || !strings.Contains(err.Error(), "no top-level name") {
		t.Errorf("missing key: %v", err)
	}
}

func TestRenderProject(t *testing.T) {
	data := newData{Name: "Rocks", ID: "dev.nsgo.rocks", Module: "example.com/rocks", Slug: "rocks"}
	src := fstest.MapFS{
		"go.mod.tmpl":          {Data: []byte("module {{.Module}}\n")},
		"nsgo.yaml":            {Data: []byte("name: {{.Slug}}\nid: placeholder\n")},
		"main.go":              {Data: []byte("// Command {{.Slug}}.\npackage main\n")},
		"levels/one.yaml.tmpl": {Data: []byte("title: {{.Name}}\n")},
		"levels/raw.txt":       {Data: []byte("{{.NotRendered}}\n")},
	}
	files, err := renderProject(src, data)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var order []string
	for _, f := range files {
		order = append(order, f.name)
		if !f.dir {
			got[f.name] = string(f.content)
		}
	}
	want := map[string]string{
		"go.mod":          "module example.com/rocks\n",
		"nsgo.yaml":       "name: Rocks\nid: dev.nsgo.rocks\n",
		"main.go":         "// Command rocks.\npackage main\n",
		"levels/one.yaml": "title: Rocks\n",
		"levels/raw.txt":  "{{.NotRendered}}\n",
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s = %q, want %q", name, got[name], content)
		}
	}
	if i := slices.Index(order, "levels"); i < 0 || i > slices.Index(order, "levels/one.yaml") {
		t.Errorf("levels does not come before its files: %q", order)
	}

	for _, bad := range []struct {
		name string
		fs   fstest.MapFS
		want string
	}{
		{"missing key", fstest.MapFS{"go.mod.tmpl": {Data: []byte("module {{.Nope}}\n")}, "nsgo.yaml": {Data: []byte("name: a\nid: b\n")}, "main.go": {}}, "Nope"},
		{"no go.mod", fstest.MapFS{"nsgo.yaml": {Data: []byte("name: a\nid: b\n")}, "main.go": {}}, "no go.mod"},
		{"no main.go", fstest.MapFS{"go.mod": {}, "nsgo.yaml": {Data: []byte("name: a\nid: b\n")}}, "no main.go"},
		{"no id", fstest.MapFS{"go.mod": {}, "nsgo.yaml": {Data: []byte("name: a\n")}, "main.go": {}}, "nsgo.yaml: no top-level id"},
		{"two go.mod", fstest.MapFS{"go.mod": {}, "go.mod.tmpl": {}, "nsgo.yaml": {Data: []byte("name: a\nid: b\n")}, "main.go": {}}, "go.mod comes from two template files"},
	} {
		if _, err := renderProject(bad.fs, data); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%s: %v, want %q", bad.name, err, bad.want)
		}
	}
}

func TestWriteProjectCreatesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	files := []projectFile{{name: "levels", dir: true}, {name: "levels/one.yaml", content: []byte("x: 1\n")}, {name: "main.go", content: []byte("package main\n")}}
	if err := writeProject(dir, files); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "levels", "one.yaml")); err != nil || string(got) != "x: 1\n" {
		t.Errorf("levels/one.yaml = %q, %v", got, err)
	}
	// A directory that is already there is used; a file that is there stops the whole write.
	if err := os.Remove(filepath.Join(dir, "levels", "one.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := writeProject(dir, files); err == nil || !strings.Contains(err.Error(), "main.go exists") {
		t.Errorf("rewrite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "levels", "one.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("wrote levels/one.yaml before finding main.go: %v", err)
	}
}

// TestStarterMatchesTemplateModule keeps the starter game new embeds identical to template/template, the starter nsgo init -template github.com/nsgo-dev/game/template writes, when the repository is checked out around this module. After changing template/template, copy it over: rm -r _templates/starter && cp -R ../../../../template/template _templates/starter (from this directory).
func TestStarterMatchesTemplateModule(t *testing.T) {
	const moduleDir = "../../../../template/template"
	if _, err := os.Stat(filepath.Join(moduleDir, "go.mod.tmpl")); err != nil {
		t.Skipf("no template module checked out beside this one: %v", err)
	}
	want := os.DirFS(moduleDir)
	got := projectTemplates[0].files
	collect := func(fsys fs.FS) map[string][]byte {
		files := map[string][]byte{}
		err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			files[name], err = fs.ReadFile(fsys, name)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	wantFiles, gotFiles := collect(want), collect(got)
	for name, content := range wantFiles {
		if g, ok := gotFiles[name]; !ok {
			t.Errorf("_templates/starter has no %s", name)
		} else if !bytes.Equal(g, content) {
			t.Errorf("_templates/starter/%s differs from template/template's", name)
		}
	}
	for name := range gotFiles {
		if _, ok := wantFiles[name]; !ok {
			t.Errorf("_templates/starter/%s is not in template/template", name)
		}
	}
}

// TestNewProjectBuildsAndPassesItsTests creates a project, points it at this checkout of the game module in place of the go get game new runs, which needs the network, then builds it, runs the game tool it adds and runs its tests with the nsgo tool it already has, all offline from the module cache.
func TestNewProjectBuildsAndPassesItsTests(t *testing.T) {
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
		t.Fatalf("exit %d: %s", code, stderr)
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
	goCmd("mod", "edit", "-replace="+gameModule+"="+gameDir, "-tool="+gameTool)
	exe := filepath.Join(t.TempDir(), "mygame")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	goCmd("build", "-o", exe, ".")
	if out := goCmd("tool", "game", "help"); !strings.Contains(out, "\n  new ") {
		t.Errorf("go tool game help:\n%s", out)
	}
	goCmd("tool", "nsgo", "test", ".")
}
