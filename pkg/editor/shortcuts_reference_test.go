package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	shortcutDocsBeginMarker = "  <!-- BEGIN GENERATED EDITOR SHORTCUTS (EDITOR_UPDATE_SHORTCUT_DOCS=1 go tool nsgo test ./pkg/editor -run TestShortcutDocsMatchRegistry) -->"
	shortcutDocsEndMarker   = "  <!-- END GENERATED EDITOR SHORTCUTS -->"
)

type shortcutDocSpec struct {
	lang       string
	editorLang Language
	colon      string
	sep        string
	openParen  string
	closeParen string
	period     string
}

var shortcutDocSpecs = []shortcutDocSpec{
	{
		lang:       "en",
		editorLang: LanguageEnglish,
		colon:      ": ",
		sep:        ", ",
		openParen:  " (",
		closeParen: ")",
		period:     ".",
	},
	{
		lang:       "zh",
		editorLang: LanguageChinese,
		colon:      "：",
		sep:        "、",
		openParen:  "（",
		closeParen: "）",
		period:     "。",
	},
}

func renderShortcutDocBlock(t *testing.T, spec shortcutDocSpec) string {
	t.Helper()
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetLanguage(spec.editorLang)

	cmds := ed.Commands()
	byScope := make(map[string][]EditorCommand, len(scopeDisplayOrder))
	var extraScopes []string
	for _, cmd := range cmds {
		if len(shortcutsForCommand(cmd)) == 0 {
			continue
		}
		sc := normalizeCommandScope(cmd.Scope)
		if _, seen := byScope[sc]; !seen {
			known := false
			for _, s := range scopeDisplayOrder {
				if s == sc {
					known = true
					break
				}
			}
			if !known {
				extraScopes = append(extraScopes, sc)
			}
		}
		byScope[sc] = append(byScope[sc], cmd)
	}

	scopes := append(append([]string(nil), scopeDisplayOrder...), extraScopes...)
	var b strings.Builder
	b.WriteString(shortcutDocsBeginMarker)
	b.WriteByte('\n')
	for _, scope := range scopes {
		list := byScope[scope]
		if len(list) == 0 {
			continue
		}
		b.WriteString("  - **")
		b.WriteString(ed.CommandScopeLabel(scope))
		b.WriteString("**")
		b.WriteString(spec.colon)
		for i, cmd := range list {
			if i > 0 {
				b.WriteString(spec.sep)
			}
			scs := shortcutsForCommand(cmd)
			for j, sc := range scs {
				if j > 0 {
					b.WriteString(" / ")
				}
				b.WriteByte('`')
				b.WriteString(sc)
				b.WriteByte('`')
			}
			b.WriteString(spec.openParen)
			b.WriteString(cmd.Label)
			b.WriteString(spec.closeParen)
		}
		b.WriteString(spec.period)
		b.WriteByte('\n')
	}
	b.WriteString(shortcutDocsEndMarker)
	return b.String()
}

func replaceGeneratedBlock(doc, want string) (updated string, current string, ok bool) {
	start := strings.Index(doc, shortcutDocsBeginMarker)
	if start < 0 {
		return "", "", false
	}
	end := strings.Index(doc[start:], shortcutDocsEndMarker)
	if end < 0 {
		return "", "", false
	}
	end += start + len(shortcutDocsEndMarker)
	return doc[:start] + want + doc[end:], doc[start:end], true
}

// TestShortcutDocsMatchRegistry keeps the shortcut lists and top-level menu bar list
// in docs/en/editor.md and docs/zh/editor.md synchronized with Editor.Commands and Editor.ApplicationMenu.
func TestShortcutDocsMatchRegistry(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	ed.SetLanguage(LanguageEnglish)
	topMenus := ed.ApplicationMenu()

	for _, spec := range shortcutDocSpecs {
		t.Run(spec.lang, func(t *testing.T) {
			path := filepath.Join("..", "..", "docs", spec.lang, "editor.md")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			want := renderShortcutDocBlock(t, spec)
			updated, current, ok := replaceGeneratedBlock(string(raw), want)
			if !ok {
				t.Fatalf("%s is missing %q ... %q markers", path, shortcutDocsBeginMarker, shortcutDocsEndMarker)
			}
			if os.Getenv("EDITOR_UPDATE_SHORTCUT_DOCS") == "1" {
				if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				current = want
			}
			if current != want {
				t.Errorf("%s shortcut reference is out of date with Editor.Commands; run:\n  EDITOR_UPDATE_SHORTCUT_DOCS=1 go tool nsgo test ./pkg/editor -run TestShortcutDocsMatchRegistry", path)
			}

			// Every built-in command with a shortcut must appear in the generated block.
			_ = ed.SetLanguage(spec.editorLang)
			for _, cmd := range ed.Commands() {
				for _, sc := range shortcutsForCommand(cmd) {
					token := "`" + sc + "`"
					if !strings.Contains(current, token) {
						t.Errorf("%s missing shortcut %s for command %q", path, token, cmd.ID)
					}
				}
				if len(shortcutsForCommand(cmd)) > 0 && !strings.Contains(current, cmd.Label) {
					t.Errorf("%s missing label %q for command %q", path, cmd.Label, cmd.ID)
				}
			}

			// The Desktop menu bar bullet must list every top-level menu returned by ApplicationMenu().
			docText := updated
			if os.Getenv("EDITOR_UPDATE_SHORTCUT_DOCS") != "1" {
				docText = string(raw)
			}
			menuLine := ""
			for _, line := range strings.Split(docText, "\n") {
				if strings.Contains(line, "Editor.ApplicationMenu") {
					menuLine = line
					break
				}
			}
			if menuLine == "" {
				t.Fatalf("%s missing Editor.ApplicationMenu line", path)
			}
			for _, m := range topMenus {
				wantMenu := "`" + m.Label + "`"
				if !strings.Contains(menuLine, wantMenu) {
					t.Errorf("%s Desktop menu bar line is missing top-level menu %s:\n  %s", path, wantMenu, menuLine)
				}
			}
		})
	}
}
