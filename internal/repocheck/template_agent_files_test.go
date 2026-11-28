package repocheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplatesCarryAgentGuides(t *testing.T) {
	matches, err := filepath.Glob("template/*/template")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no templates found")
	}
	for _, dir := range matches {
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Errorf("%s: %v", filepath.Join(dir, name), err)
				continue
			}
			if len(data) == 0 {
				t.Errorf("%s is empty", filepath.Join(dir, name))
			}
		}
	}
}
