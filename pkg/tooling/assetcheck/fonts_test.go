package assetcheck_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gameproject "github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"golang.org/x/image/font/gofont/goregular"
)

const fontCheckProjectHead = `version: 1
name: FontCheck
id: dev.example.fontcheck
appVersion: '1'
window: {width: 320, height: 240}
`

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildTestAssets(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	f := gameproject.MustParse(data)
	b := &assetbuild.Builder{Root: root}
	if _, err := b.Build(context.Background(), f.Assets); err != nil {
		t.Fatalf("assetbuild.Build: %v", err)
	}
}

func TestCheckFontsReportsMissingCharactersFromBakedFont(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	l10nPath := filepath.Join(root, "l10n/en.yaml")
	writeTestFile(t, l10nPath, []byte(`version: 1
locale: en
strings:
  menu.play: "Play"
  hud.score: "[b]Score:[/b] {score}"
plurals:
  coins:
    one: "{count} coin"
    other: "{count} coins"
`))
	writeTestFile(t, filepath.Join(root, "game.yaml"), []byte(fontCheckProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui.fnt
      size: 16
      charsFrom:
        - l10n/en.yaml
`))
	buildTestAssets(t, root)

	// Freshly baked font covers every character: zero problems.
	rep, err := assetcheck.Check(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		t.Fatalf("unexpected problems on covered font: %v", rep.Problems)
	}

	// Edit the string table without rebuilding the font, adding 'Z' in a string and 'Q' in a plural form.
	writeTestFile(t, l10nPath, []byte(`version: 1
locale: en
strings:
  menu.play: "Play"
  hud.boss: "Boss Z {name}"
plurals:
  coins:
    one: "{count} coin"
    other: "{count} coins Q"
`))
	rep, err = assetcheck.Check(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Problems) != 2 {
		t.Fatalf("got %d problems (%v), want 2", len(rep.Problems), rep.Problems)
	}
	if p := rep.Problems[0]; p.File != "l10n/en.yaml" || p.Line != 5 || !strings.Contains(p.Message, `"hud.boss"`) || !strings.Contains(p.Message, `'Z'`) {
		t.Errorf("problem[0] = %+v, want l10n/en.yaml:5 mentioning hud.boss and 'Z'", p)
	}
	if p := rep.Problems[1]; p.File != "l10n/en.yaml" || p.Line != 9 || !strings.Contains(p.Message, `"coins"`) || !strings.Contains(p.Message, `"other"`) || !strings.Contains(p.Message, `'Q'`) {
		t.Errorf("problem[1] = %+v, want l10n/en.yaml:9 mentioning coins/other and 'Q'", p)
	}
}

func TestCheckFontsBeforeBuildReportsSourceFontMissingGlyphs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	writeTestFile(t, filepath.Join(root, "l10n/zh-Hans.yaml"), []byte(`version: 1
locale: zh-Hans
strings:
  boss.name: "Boss 龙"
`))
	writeTestFile(t, filepath.Join(root, "game.yaml"), []byte(fontCheckProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui-zh.fnt
      size: 16
      charsFrom:
        - l10n/zh-Hans.yaml
`))
	// Do NOT run assetbuild: check must still catch that fonts/go.ttf lacks '龙'.
	rep, err := assetcheck.Check(os.DirFS(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Problems) != 1 {
		t.Fatalf("got %d problems (%v), want 1", len(rep.Problems), rep.Problems)
	}
	p := rep.Problems[0]
	if p.File != "l10n/zh-Hans.yaml" || !strings.Contains(p.Message, `"boss.name"`) || !strings.Contains(p.Message, `'龙'`) || !strings.Contains(p.Message, "U+9F99") {
		t.Fatalf("problem = %+v, want boss.name missing '龙' (U+9F99)", p)
	}
}

func TestCheckFontsExplicitLocaleAndSplitTables(t *testing.T) {
	t.Run("explicit locale without charsFrom", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
		writeTestFile(t, filepath.Join(root, "l10n/en.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  ok: \"AB\"\n  bad: \"ABC\"\n"))
		writeTestFile(t, filepath.Join(root, "game.yaml"), []byte(fontCheckProjectHead+"assets:\n  fonts:\n    - in: fonts/go.ttf\n      out: assets/fonts/ui.fnt\n      locale: en\n      chars: \"AB\"\n"))
		rep, err := assetcheck.Check(os.DirFS(root))
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0].Message, `"bad"`) || !strings.Contains(rep.Problems[0].Message, `'C'`) {
			t.Fatalf("problems = %v, want 1 problem naming bad and 'C'", rep.Problems)
		}
	})

	t.Run("two split font targets for same locale do not cross-complain", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
		writeTestFile(t, filepath.Join(root, "l10n/ui.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  a: \"AB\"\n"))
		writeTestFile(t, filepath.Join(root, "l10n/dialogue.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  b: \"XY\"\n"))
		writeTestFile(t, filepath.Join(root, "game.yaml"), []byte(fontCheckProjectHead+"assets:\n  fonts:\n    - in: fonts/go.ttf\n      out: assets/fonts/ui.fnt\n      charsFrom:\n        - l10n/ui.yaml\n    - in: fonts/go.ttf\n      out: assets/fonts/dialogue.fnt\n      charsFrom:\n        - l10n/dialogue.yaml\n"))
		buildTestAssets(t, root)
		rep, err := assetcheck.Check(os.DirFS(root))
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() {
			t.Fatalf("unexpected cross-table problems: %v", rep.Problems)
		}
	})
}
