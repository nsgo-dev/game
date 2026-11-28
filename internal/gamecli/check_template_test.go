package gamecli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/project"
)

// newTemplateProject writes template tmplName into a new directory as game new renders it, named name, with an origin naming the template appended to its game.yaml, and returns the directory.
func newTemplateProject(t *testing.T, tmplName, name string) string {
	t.Helper()
	tmpl, ok := originTemplate(&project.Origin{Kind: "template", Path: tmplName})
	if !ok {
		t.Fatalf("no template %q", tmplName)
	}
	dir := filepath.Join(t.TempDir(), "game")
	data, err := projectData(dir, name, "example.com/game")
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderProject(tmpl.files, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProject(dir, files); err != nil {
		t.Fatal(err)
	}
	gameYAML := filepath.Join(dir, project.FileName)
	b, err := os.ReadFile(gameYAML)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("origin: {kind: template, source: "+tmpl.module+", path: "+tmplName+"}\n")...)
	if err := os.WriteFile(gameYAML, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheckWarnsAboutTemplateResidueWithoutFailing(t *testing.T) {
	dir := newTemplateProject(t, "cards2d", "Suit Ladders")
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("exit %d, want 0 (stderr: %s)", code, stderr)
	}
	gameYAML := filepath.Join(dir, project.FileName)
	b, err := os.ReadFile(gameYAML)
	if err != nil {
		t.Fatal(err)
	}
	originLine, _ := loaderr.FindField(b, "origin")
	nameLine, _ := loaderr.FindField(b, "name")
	want := []string{
		gameYAML + ":" + strconv.Itoa(originLine) + `: warning: 9 rule and data files still equal template "cards2d": board.yaml, game.go, hud.go, project.go, render.go, rules.go, rules.yaml, save.go, table.go; change them, or add rules or data to origin.keep for a deliberate copy`,
		gameYAML + ":" + strconv.Itoa(nameLine) + `: warning: name "Suit Ladders" is still template "cards2d"'s; name the game, or add name to origin.keep`,
		filepath.Join(dir, "README.md") + `: warning: README.md is still template "cards2d"'s; rewrite it for this game, or add readme to origin.keep`,
	}
	if got := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n"); !slices.Equal(got, want) {
		t.Fatalf("stderr =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.HasSuffix(stdout.String(), ": no problems, 3 template residue warnings\n") {
		t.Fatalf("stdout = %q, want the summary to count 3 warnings", stdout)
	}

	// Editing every rule and data file, renaming the game and rewriting the README clears every warning.
	for _, name := range []string{"board.yaml", "rules.yaml"} {
		appendFile(t, filepath.Join(dir, name), "# edited\n")
	}
	for _, name := range []string{"game.go", "hud.go", "project.go", "render.go", "rules.go", "save.go", "table.go"} {
		appendFile(t, filepath.Join(dir, name), "// edited\n")
	}
	for _, name := range []string{project.FileName, project.NSGoFileName} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(b), "name: Suit Ladders", "name: Card Night", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Card Night\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"check", dir}, a); code != 0 {
		t.Fatalf("after edits: exit %d, want 0 (stderr: %s)", code, stderr)
	}
	if stderr.Len() != 0 || strings.Contains(stdout.String(), "warning") {
		t.Fatalf("after edits: stdout %q, stderr %q, want no warnings", stdout, stderr)
	}
}

func TestTemplateResidueKeepAndOtherOrigins(t *testing.T) {
	tmpl, ok := originTemplate(&project.Origin{Kind: "template", Path: "shmup2d"})
	if !ok {
		t.Fatal("no template shmup2d")
	}
	read := func(name string) []byte {
		b, err := fs.ReadFile(tmpl.files, name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	gameYAML := func(origin string) []byte {
		return []byte("version: 1\nname: Bullet Garden\nid: dev.example.bulletgarden\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\norigin: " + origin + "\n")
	}
	files := fstest.MapFS{
		"README.md":       {Data: read("README.md")},
		"CREDITS.md":      {Data: read("CREDITS.md")},
		"data/stage.yaml": {Data: read("data/stage.yaml")},
	}
	messages := func(fsys fstest.MapFS) []string {
		var out []string
		for _, p := range templateResidue(fsys) {
			out = append(out, p.File+": "+p.Message)
		}
		return out
	}

	files[project.FileName] = &fstest.MapFile{Data: gameYAML("{kind: template, source: " + tmpl.module + ", path: shmup2d}")}
	got := messages(files)
	if len(got) != 3 || !strings.Contains(got[0], "1 rule and data file still equal") || !strings.Contains(got[0], "data/stage.yaml") || !strings.HasPrefix(got[1], "README.md: ") || !strings.HasPrefix(got[2], "CREDITS.md: ") {
		t.Fatalf("warnings = %q, want data, README and CREDITS", got)
	}

	files[project.FileName] = &fstest.MapFile{Data: gameYAML("{kind: template, source: " + tmpl.module + ", keep: [data, readme, credits]}")}
	if got := messages(files); len(got) != 0 {
		t.Fatalf("with origin.keep and the template named by its module: warnings = %q, want none", got)
	}

	for _, origin := range []string{"{kind: directory, source: ../other}", "{kind: template, source: example.com/elsewhere, path: unknown}"} {
		files[project.FileName] = &fstest.MapFile{Data: gameYAML(origin)}
		if got := messages(files); len(got) != 0 {
			t.Fatalf("origin %s: warnings = %q, want none", origin, got)
		}
	}
}

func TestOriginRefusesUnknownKeepWithItsLine(t *testing.T) {
	_, err := project.Parse([]byte("version: 1\nname: Game\nid: dev.example.game\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\norigin:\n  kind: template\n  source: github.com/nsgo-dev/game/template\n  keep: [readme, wat]\n"))
	var le *loaderr.Error
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want a *loaderr.Error", err)
	}
	if le.Line != 9 || !strings.Contains(le.Problem, `"wat"`) {
		t.Fatalf("err = %+v, want line 9 naming wat", le)
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
