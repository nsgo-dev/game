package repocheck

import (
	"os"
	"strings"
	"testing"
)

func TestTutorialChapterOneHasCompleteSource(t *testing.T) {
	en, err := os.ReadFile("docs/en/tutorial-01-empty-project.md")
	if err != nil {
		t.Fatal(err)
	}
	zh, err := os.ReadFile("docs/zh/tutorial-01-empty-project.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(en)
	for _, want := range []string{"## Complete source files", "`game.go`:", "`game_test.go`:", "const groundHeight = 24", "func fill()", "func hasFillColor", "func hasRect"} {
		if !strings.Contains(text, want) {
			t.Errorf("English chapter 1 is missing %q", want)
		}
	}
	if !strings.Contains(string(zh), "## 完整源文件") || !strings.Contains(string(zh), "examples/tutorial/ch01-empty-project") {
		t.Error("Chinese chapter 1 does not point to the complete source")
	}
	tutorial, _ := os.ReadFile("docs/en/tutorial.md")
	if !strings.Contains(string(tutorial), "excerpts unless a section says Complete source files") {
		t.Error("tutorial overview does not explain excerpted code")
	}
}
