package gamecli

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/project"
)

func TestTemplateResidueWarningsAndKeep(t *testing.T) {
	dir := t.TempDir()
	read := func(path string) []byte {
		data, err := fs.ReadFile(projectsFS, "_templates/projects/starter/"+path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), read("README.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &project.File{Name: "Starter Game", Origin: &project.Origin{Kind: "template", Name: "starter"}}
	warnings := templateResidueProblems(dir, f)
	if len(warnings) < 2 {
		t.Fatalf("warnings = %+v, want name/readme or data warnings", warnings)
	}
	for _, p := range warnings {
		if !p.Warning {
			t.Fatalf("problem is not warning: %+v", p)
		}
	}
	f.Origin.Keep = []string{"name", "readme", "data", "rules"}
	if got := templateResidueProblems(dir, f); len(got) != 0 {
		t.Fatalf("kept warnings = %+v", got)
	}
}

func TestOriginRejectsUnknownKeep(t *testing.T) {
	_, err := project.Parse([]byte("version: 1\nname: Game\nid: dev.example.game\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\norigin: {kind: template, name: starter, keep: [wat] }\n"))
	if err == nil {
		t.Fatal("unknown origin.keep accepted")
	}
}
