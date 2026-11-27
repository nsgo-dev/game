package refs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/tooling/refs"
)

func TestScanAndMoveScreenSpriteAndTextKeys(t *testing.T) {
	hudYAML := `# HUD screen
version: 1
name: hud
designWidth: 640
designHeight: 360
nodes:
  - id: status
    type: vbox
    anchor: top_left
    children:
      - id: hearts
        type: sprite_row
        sprite: art/heart.png
        spriteCount: 3
      - id: hint
        type: label
        textKey: hud.hint.lever
`
	enYAML := `version: 1
locale: en
strings:
  hud.hint.lever: Jump at levers to pull
`
	zhYAML := `version: 1
locale: zh
strings:
  hud.hint.lever: 靠近拉杆时按下跳跃键即可拉动
`
	fsys := fstest.MapFS{
		"game.yaml":               {Data: []byte("version: 1\nname: Test\nid: dev.nsgo.test\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\n")},
		"screens/hud.screen.yaml": {Data: []byte(hudYAML)},
		"art/heart.png":           {Data: []byte("png")},
		"locales/en.yaml":         {Data: []byte(enYAML)},
		"locales/zh.yaml":         {Data: []byte(zhYAML)},
	}

	proj, err := refs.Scan(fsys)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(proj.Problems) != 0 {
		t.Fatalf("Problems: %v", proj.Problems)
	}

	// Verify sprite ref was found.
	foundSprite := false
	for _, r := range proj.Refs {
		if r.File == "screens/hud.screen.yaml" && r.Field == refs.FieldScreenSprite && r.Value == "art/heart.png" && r.Target == "art/heart.png" {
			foundSprite = true
		}
	}
	if !foundSprite {
		t.Fatalf("expected sprite ref in screens/hud.screen.yaml, got %+v", proj.Refs)
	}

	// Verify textKey usages across screen + both locale tables.
	usages := proj.TextKeyUsages("hud.hint.lever")
	if len(usages) != 3 {
		t.Fatalf("TextKeyUsages = %+v, want 3 entries (en, zh, screen)", usages)
	}

	// Move sprite on disk and verify in-place rewrite keeps comments intact.
	dir := t.TempDir()
	for name, f := range fsys {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	movePlan, err := refs.PlanMove(os.DirFS(dir), "art/heart.png", "art/ui/heart.png")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if err := movePlan.Apply(dir); err != nil {
		t.Fatalf("Apply move: %v", err)
	}
	updatedHUD, err := os.ReadFile(filepath.Join(dir, "screens", "hud.screen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(updatedHUD), "# HUD screen\n") || !strings.Contains(string(updatedHUD), "sprite: art/ui/heart.png\n") {
		t.Fatalf("unexpected rewritten hud.screen.yaml:\n%s", updatedHUD)
	}

	// Rename text key across screen + locale tables.
	renamePlan, err := refs.PlanRenameTextKey(os.DirFS(dir), "hud.hint.lever", "hud.tip.lever")
	if err != nil {
		t.Fatalf("PlanRenameTextKey: %v", err)
	}
	if len(renamePlan.UpdatedTextKeys) != 3 {
		t.Fatalf("UpdatedTextKeys = %d, want 3", len(renamePlan.UpdatedTextKeys))
	}
	if err := renamePlan.Apply(dir); err != nil {
		t.Fatalf("Apply rename: %v", err)
	}
	for _, rel := range []string{"screens/hud.screen.yaml", "locales/en.yaml", "locales/zh.yaml"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "hud.tip.lever") || strings.Contains(string(got), "hud.hint.lever") {
			t.Fatalf("%s was not updated:\n%s", rel, got)
		}
	}
}
