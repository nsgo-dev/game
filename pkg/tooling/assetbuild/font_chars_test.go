package assetbuild_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/assetbuild"
	"golang.org/x/image/font/gofont/goregular"
)

const fontCharsProjectHead = `version: 1
name: Sample
id: dev.example.sample
appVersion: '1'
window: {width: 320, height: 240}
`

func TestFontCharsFromL10nPluralsPlaceholdersMarkupAndCharsFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	writeFile(t, filepath.Join(root, "l10n/en.yaml"), []byte(`version: 1
locale: en
strings:
  hud.score: "[color=#FFD700][b]Score:[/b][/color] {score}"
  hud.icon: "[icon=coin] [pause=0.2]Gold"
  hud.escaped: "{{OK}}"
plurals:
  coins:
    zero: "No coin"
    one: "{count} coin"
    other: "{count} coins!"
`))
	// UTF-8 BOM + newlines + extra runes.
	writeFile(t, filepath.Join(root, "fonts/extra.txt"), append([]byte{0xEF, 0xBB, 0xBF}, []byte("XYZ\n?!\r\n")...))
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui.fnt
      size: 16
      chars: "012"
      charsFrom:
        - l10n/en.yaml
      charsFile: fonts/extra.txt
`))

	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"font assets/fonts/ui.fnt": assetbuild.Built})
	if len(r.Targets[0].Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", r.Targets[0].Warnings)
	}

	f := loadFont(t, root, "assets/fonts/ui.fnt")
	// Merged chars from chars ("012"), l10n strings & plurals (placeholders & rich tags stripped), and charsFile ("XYZ?!").
	for _, wantRune := range []rune{'0', '1', '2', 'S', 'c', 'o', 'r', 'e', ':', ' ', 'G', 'l', 'd', '{', 'O', 'K', '}', 'N', 'i', 'n', 's', '!', 'X', 'Y', 'Z', '?'} {
		if _, ok := f.Glyphs[wantRune]; !ok {
			t.Errorf("baked font missing expected glyph %q", wantRune)
		}
	}
	// Letters that appeared ONLY inside placeholders ({score}, {count}) or rich tags ([color=#FFD700], [b], [icon=coin], [pause=0.2]) must not be baked.
	for _, absentRune := range []rune{'#', '=', 'F', '7', '[', ']', '/', 'b', 'p', 'a', 'u', 't', '.'} {
		if _, ok := f.Glyphs[absentRune]; ok {
			t.Errorf("baked font unexpectedly contains markup/placeholder glyph %q", absentRune)
		}
	}
}

func TestFontCharsFromRebuildsOnlyWhenCharacterSetChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	l10nPath := filepath.Join(root, "l10n/en.yaml")
	writeFile(t, l10nPath, []byte(`version: 1
locale: en
strings:
  greeting: "Hello {name}"
`))
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui.fnt
      size: 16
      charsFrom:
        - l10n/en.yaml
`))

	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"font assets/fonts/ui.fnt": assetbuild.Built})
	firstFNT, err := os.ReadFile(filepath.Join(root, "assets/fonts/ui.fnt"))
	if err != nil {
		t.Fatal(err)
	}

	// Edit the string table using ONLY existing characters, renaming the placeholder and adding a comment: must NOT rebuild.
	writeFile(t, l10nPath, []byte(`# updated copy
version: 1
locale: en
strings:
  greeting: "ollHe {player_title}"
  extra: "He lo"
`))
	r = build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"font assets/fonts/ui.fnt": assetbuild.UpToDate})

	// Add a brand-new character 'W': MUST rebuild and include 'W'.
	writeFile(t, l10nPath, []byte(`version: 1
locale: en
strings:
  greeting: "Hello World {player_title}"
`))
	r = build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"font assets/fonts/ui.fnt": assetbuild.Built})
	f := loadFont(t, root, "assets/fonts/ui.fnt")
	if _, ok := f.Glyphs['W']; !ok {
		t.Fatal("rebuilt font missing newly added glyph 'W'")
	}

	// Deterministic output: a fresh build in another directory with reordered strings produces byte-identical outputs.
	root2 := t.TempDir()
	writeFile(t, filepath.Join(root2, "fonts/go.ttf"), goregular.TTF)
	writeFile(t, filepath.Join(root2, "l10n/en.yaml"), []byte(`version: 1
locale: en
strings:
  a: "He"
  b: "llo "
`))
	writeFile(t, filepath.Join(root2, "game.yaml"), []byte(fontCharsProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui.fnt
      size: 16
      charsFrom:
        - l10n/en.yaml
`))
	build(t, root2, assetbuild.Builder{})
	secondFNT, err := os.ReadFile(filepath.Join(root2, "assets/fonts/ui.fnt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstFNT, secondFNT) {
		t.Fatal("baked .fnt differs across projects with the same effective character set")
	}
}

func TestFontCharsFromDataTableAndScene(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	writeFile(t, filepath.Join(root, "data/items.table.yaml"), []byte(`version: 1
rows:
  - id: sword
    name: "Blade"
    tags: ["Sharp", "Steel"]
    damage: 42
    rare: true
`))
	writeFile(t, filepath.Join(root, "scenes/town.scene.yaml"), []byte(`version: 1
name: Town
layers:
  - name: main
    objects:
      - id: sign
        properties:
          title: "Inn"
        components:
          signpost:
            text: "[b]Rest[/b]"
          body2d:
            kind: dynamic
`))
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui.fnt
      size: 16
      charsFrom:
        - data/items.table.yaml
        - scenes/town.scene.yaml
`))

	build(t, root, assetbuild.Builder{})
	f := loadFont(t, root, "assets/fonts/ui.fnt")
	for _, wantRune := range []rune{'B', 'l', 'a', 'd', 'e', 'S', 'h', 'r', 'p', 't', 'I', 'n', 'R', 's'} {
		if _, ok := f.Glyphs[wantRune]; !ok {
			t.Errorf("baked font missing expected glyph %q", wantRune)
		}
	}
	// Numbers/booleans from data table and built-in component enum "dynamic" must not be collected.
	for _, absentRune := range []rune{'4', '2', 'u', 'y', 'm', 'c', '['} {
		if _, ok := f.Glyphs[absentRune]; ok {
			t.Errorf("baked font unexpectedly contains non-text glyph %q", absentRune)
		}
	}
}

func TestFontWarnsWhenSourceFontLacksCharacters(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
	writeFile(t, filepath.Join(root, "l10n/zh-Hans.yaml"), []byte(`version: 1
locale: zh-Hans
strings:
  hud.score: "Score 得分：{score}"
`))
	writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+`assets:
  fonts:
    - in: fonts/go.ttf
      out: assets/fonts/ui-zh.fnt
      size: 16
      charsFrom:
        - l10n/zh-Hans.yaml
`))

	r := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r, map[string]assetbuild.Status{"font assets/fonts/ui-zh.fnt": assetbuild.Built})
	if len(r.Targets[0].Warnings) != 1 {
		t.Fatalf("warnings = %v, want 1 warning", r.Targets[0].Warnings)
	}
	w := r.Targets[0].Warnings[0]
	for _, sub := range []string{"fonts/go.ttf", "得", "分", "U+5F97", "U+5206"} {
		if !strings.Contains(w, sub) {
			t.Errorf("warning %q missing %q", w, sub)
		}
	}

	// Second build is UpToDate and preserves the warning from the cache.
	r2 := build(t, root, assetbuild.Builder{})
	wantStatuses(t, r2, map[string]assetbuild.Status{"font assets/fonts/ui-zh.fnt": assetbuild.UpToDate})
	if len(r2.Targets[0].Warnings) != 1 || r2.Targets[0].Warnings[0] != w {
		t.Fatalf("cached warnings = %v, want [%q]", r2.Targets[0].Warnings, w)
	}
}

func TestFontCharsSidecarAndRefusals(t *testing.T) {
	t.Run("sidecar charsFrom overrides target", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
		writeFile(t, filepath.Join(root, "l10n/en.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  a: \"XYZ\"\n"))
		writeFile(t, filepath.Join(root, "fonts/go.ttf.import.yaml"), []byte("version: 1\nfont:\n  charsFrom:\n    - l10n/en.yaml\n"))
		writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+"assets:\n  fonts:\n    - in: fonts/go.ttf\n      out: assets/fonts/ui.fnt\n      size: 16\n      chars: \"AB\"\n"))
		build(t, root, assetbuild.Builder{})
		f := loadFont(t, root, "assets/fonts/ui.fnt")
		for _, wantRune := range []rune{'A', 'B', 'X', 'Y', 'Z'} {
			if _, ok := f.Glyphs[wantRune]; !ok {
				t.Errorf("missing merged glyph %q", wantRune)
			}
		}
	})

	t.Run("empty collected set refused", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
		writeFile(t, filepath.Join(root, "l10n/en.yaml"), []byte("version: 1\nlocale: en\nstrings:\n  only: \"{score}\"\n"))
		writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+"assets:\n  fonts:\n    - in: fonts/go.ttf\n      out: assets/fonts/ui.fnt\n      charsFrom:\n        - l10n/en.yaml\n"))
		if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "no characters collected") {
			t.Fatalf("Build error = %v, want no characters collected", err)
		}
	})

	t.Run("invalid UTF-8 charsFile refused", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "fonts/go.ttf"), goregular.TTF)
		writeFile(t, filepath.Join(root, "fonts/bad.txt"), []byte{0xff, 0xfe})
		writeFile(t, filepath.Join(root, "game.yaml"), []byte(fontCharsProjectHead+"assets:\n  fonts:\n    - in: fonts/go.ttf\n      out: assets/fonts/ui.fnt\n      charsFile: fonts/bad.txt\n"))
		if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
			t.Fatalf("Build error = %v, want valid UTF-8 error", err)
		}
	})
}
