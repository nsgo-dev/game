package editor_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// uiWidgetTypes lists NSGo UI composite literal types whose user-visible string fields
// must go through the editor localization catalog (ed.T / ed.Plural).
var uiWidgetTypes = map[string]bool{
	"core.Text":                   true,
	"core.View":                   true,
	"core.Input":                  true,
	"core.Notification":           true,
	"core.PickFilesOptions":       true,
	"core.PickDirectoriesOptions": true,
	"core.SaveOptions":            true,
	"ui.Button":                   true,
	"ui.ToggleButton":             true,
	"ui.IconButton":               true,
	"ui.Input":                    true,
	"ui.NumberInput":              true,
	"ui.Checkbox":                 true,
	"ui.Switch":                   true,
	"ui.Slider":                   true,
	"ui.Badge":                    true,
	"ui.Tooltip":                  true,
	"ui.Menu":                     true,
	"ui.MenuItem":                 true,
	"ui.Submenu":                  true,
	"ui.ContextMenu":              true,
	"ui.Dialog":                   true,
	"ui.AlertDialog":              true,
	"ui.AlertAction":              true,
	"ui.CommandDialog":            true,
	"ui.Command":                  true,
	"ui.DockTabLabel":             true,
	"base.Button":                 true,
	"base.Toggle":                 true,
	"base.Switch":                 true,
	"base.Radio":                  true,
	"base.NumberInput":            true,
	"base.CommandItem":            true,
}

var uiStringFields = map[string]bool{
	"Content":         true,
	"ContentFunc":     true,
	"Text":            true,
	"TextFunc":        true,
	"Label":           true,
	"LabelFunc":       true,
	"Placeholder":     true,
	"PlaceholderFunc": true,
	"Title":           true,
	"TitleFunc":       true,
	"Subtitle":        true,
	"SubtitleFunc":    true,
	"Tooltip":         true,
	"TooltipFunc":     true,
	"Message":         true,
	"Description":     true,
	"EmptyText":       true,
	"ButtonLabel":     true,
	"Body":            true,
	"Category":        true,
}

var englishWordRe = regexp.MustCompile(`[A-Za-z]{2,}`)
var fmtVerbRe = regexp.MustCompile(`%[-+# 0*]*(\d+|\*)?(\.(\d+|\*))?[vTtbcdoOqxXUeEfFgGsp%]`)
var filePathLikeRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+\.(yaml|yml|json|go|md|png|jpg|jpeg|webp|svg|glb|gltf|wav|ogg|mp3|ttf|otf|txt|csv|tsx|ts|js|html|css|scene\.yaml|prefab\.yaml|anim\.yaml|material\.yaml)$`)
var hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{3,8}$`)
var snakeCaseIdentRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)+$`)

// uiLiteralAllowedWords holds proper nouns, technical acronyms, units, keyboard key names,
// and format identifiers that are intentionally kept in Latin script in both English and Chinese.
var uiLiteralAllowedWords = map[string]bool{
	// Product / tool / platform / vendor proper nouns
	"nsgo":       true,
	"git":        true,
	"github":     true,
	"mcp":        true,
	"macos":      true,
	"windows":    true,
	"linux":      true,
	"ios":        true,
	"android":    true,
	"tvos":       true,
	"watchos":    true,
	"harmonyos":  true,
	"web":        true,
	"steam":      true,
	"steampipe":  true,
	"itch":       true,
	"apple":      true,
	"google":     true,
	"play":       true,
	"testflight": true,
	"appimage":   true,
	"chrome":     true,
	"perfetto":   true,
	"markdown":   true,
	"developer":  true,
	"gc":         true,
	"shell":      true,
	"claude":     true,
	"codex":      true,
	"anthropic":  true,
	"openai":     true,
	"gemini":     true,
	"ollama":     true,
	"blender":    true,
	"maya":       true,
	"tiled":      true,
	"ldtk":       true,
	"aseprite":   true,
	"godot":      true,
	"unity":      true,
	"go":         true,
	// Technical acronyms, formats, units, math/axis tokens
	"yaml":  true,
	"yml":   true,
	"json":  true,
	"png":   true,
	"jpg":   true,
	"jpeg":  true,
	"webp":  true,
	"svg":   true,
	"gltf":  true,
	"glb":   true,
	"wav":   true,
	"ogg":   true,
	"mp3":   true,
	"wasm":  true,
	"apk":   true,
	"aab":   true,
	"ipa":   true,
	"dmg":   true,
	"exe":   true,
	"zip":   true,
	"csv":   true,
	"html":  true,
	"http":  true,
	"https": true,
	"url":   true,
	"uri":   true,
	"lan":   true,
	"qr":    true,
	"ip":    true,
	"tcp":   true,
	"udp":   true,
	"ws":    true,
	"api":   true,
	"cli":   true,
	"sdk":   true,
	"ide":   true,
	"fps":   true,
	"gpu":   true,
	"cpu":   true,
	"ram":   true,
	"vram":  true,
	"hdr":   true,
	"sdr":   true,
	"pbr":   true,
	"ibl":   true,
	"ssao":  true,
	"fxaa":  true,
	"msaa":  true,
	"taa":   true,
	"fov":   true,
	"lod":   true,
	"aabb":  true,
	"obb":   true,
	"bvh":   true,
	"ik":    true,
	"fk":    true,
	"uv":    true,
	"rgb":   true,
	"rgba":  true,
	"hsv":   true,
	"hsl":   true,
	"hex":   true,
	"xyz":   true,
	"ui":    true,
	"hud":   true,
	"ai":    true,
	"id":    true,
	"ids":   true,
	"dsp":   true,
	"eq":    true,
	"lufs":  true,
	"db":    true,
	"hz":    true,
	"khz":   true,
	"px":    true,
	"pt":    true,
	"ms":    true,
	"kb":    true,
	"mb":    true,
	"gb":    true,
	"deg":   true,
	"rad":   true,
	"min":   true,
	"max":   true,
	"ok":    true,
	"vs":    true,
	"sha":   true,
	"utf":   true,
	"ascii": true,
	"bcp":   true,
	"iso":   true,
	"en":    true,
	"zh":    true,
	"ja":    true,
	"ko":    true,
	"es":    true,
	"fr":    true,
	"de":    true,
	// Keyboard / shortcut labels
	"esc":   true,
	"tab":   true,
	"cmd":   true,
	"ctrl":  true,
	"alt":   true,
	"opt":   true,
	"shift": true,
	"enter": true,
	"del":   true,
	"mod":   true,
	"wasd":  true,
	"lmb":   true,
	"rmb":   true,
	"mmb":   true,
	// Common code/config filenames
	"agents": true,
	"readme": true,
}

// uiLiteralAllowedExact holds exact literal strings that are proper nouns, format templates,
// technical identifiers, or sample placeholders that should not be translated.
var uiLiteralAllowedExact = map[string]bool{
	"English":                true,
	"简体中文":                   true,
	"简体中文 (Chinese)":         true,
	"game.yaml":              true,
	"nsgo.yaml":              true,
	"brief.yaml":             true,
	"permissions.yaml":       true,
	"automations.yaml":       true,
	"AGENTS.md":              true,
	"README.md":              true,
	"CHANGELOG.md":           true,
	"en":                     true,
	"zh":                     true,
	"en-US":                  true,
	"zh-CN":                  true,
	"2D":                     true,
	"3D":                     true,
	"1x":                     true,
	"2x":                     true,
	"0.5x":                   true,
	"16:9":                   true,
	"4:3":                    true,
	"1:1":                    true,
	"X":                      true,
	"Y":                      true,
	"Z":                      true,
	"W":                      true,
	"H":                      true,
	"R":                      true,
	"G":                      true,
	"B":                      true,
	"A":                      true,
	"XY":                     true,
	"XZ":                     true,
	"YZ":                     true,
	"XYZ":                    true,
	"RGB":                    true,
	"RGBA":                   true,
	"HEX":                    true,
	"UV":                     true,
	"dB":                     true,
	"Hz":                     true,
	"kHz":                    true,
	"ms":                     true,
	"px":                     true,
	"pt":                     true,
	"FPS":                    true,
	"LOD":                    true,
	"FOV":                    true,
	"MCP":                    true,
	"Git":                    true,
	"NSGo":                   true,
	"YAML":                   true,
	"JSON":                   true,
	"glTF":                   true,
	"GLB":                    true,
	"PNG":                    true,
	"SVG":                    true,
	"HUD":                    true,
	"AI":                     true,
	"ID":                     true,
	"URL":                    true,
	"API":                    true,
	"macOS":                  true,
	"Windows":                true,
	"Linux":                  true,
	"iOS":                    true,
	"Android":                true,
	"Web":                    true,
	"tvOS":                   true,
	"watchOS":                true,
	"HarmonyOS":              true,
	"Claude":                 true,
	"Codex":                  true,
	"Anthropic":              true,
	"OpenAI":                 true,
	"Ollama":                 true,
	"Blender":                true,
	"Maya":                   true,
	"F1":                     true,
	"F2":                     true,
	"F5":                     true,
	"F6":                     true,
	"Esc":                    true,
	"Tab":                    true,
	"Enter":                  true,
	"Shift+Enter":            true,
	"Shift+Tab":              true,
	"WASD":                   true,
	"⌘K":                     true,
	"⌘/":                     true,
	"⌘.":                     true,
	"⌘S":                     true,
	"⌘Z":                     true,
	"⌘⇧Z":                    true,
	"Ctrl+K":                 true,
	"Ctrl+.":                 true,
	"Ctrl+/":                 true,
	"MOD":                    true,
	"NEW":                    true,
	"DEL":                    true,
	"dev.example.mygame":     true,
	"https://":               true,
	"http://localhost:8080":  true,
	"scenes/main.scene.yaml": true,
}

func isAllowedUILiteral(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	if uiLiteralAllowedExact[trimmed] {
		return true
	}
	if filePathLikeRe.MatchString(trimmed) || hexColorRe.MatchString(trimmed) || snakeCaseIdentRe.MatchString(trimmed) {
		return true
	}
	// Strip printf verbs like %s, %d, %.1f, %+v before scanning for English words.
	stripped := fmtVerbRe.ReplaceAllString(trimmed, " ")
	words := englishWordRe.FindAllString(stripped, -1)
	if len(words) == 0 {
		return true
	}
	for _, w := range words {
		if !uiLiteralAllowedWords[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

func i18nExprTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func collectDisallowedStrings(fset *token.FileSet, expr ast.Expr, ctx string, out *[]string) {
	if expr == nil {
		return
	}
	ast.Inspect(expr, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			// Do not descend into ed.T(...), ed.Plural(...), ed.PluginT(...), ed.PluginPlural(...),
			// l10n.Arg(...), or TestID/icon/palette lookups.
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "T", "Plural", "PluginT", "PluginPlural", "Arg", "Icon":
					return false
				}
			}
			// Check fmt.Sprintf format string only (not arbitrary key arguments like map lookups).
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if val, err := strconv.Unquote(lit.Value); err == nil && !isAllowedUILiteral(val) {
						pos := fset.Position(lit.Pos())
						*out = append(*out, fmt.Sprintf("%s:%d: %s fmt.Sprintf(%q)", pos.Filename, pos.Line, ctx, val))
					}
				}
				for _, arg := range x.Args[1:] {
					collectDisallowedStrings(fset, arg, ctx, out)
				}
				return false
			}
			// Skip function calls other than fmt.Sprintf/string helpers to avoid flagging IDs/keys passed to getters.
			return false
		case *ast.IndexExpr:
			// Skip map index keys like m["foo"].
			return false
		case *ast.BinaryExpr:
			// Only inspect string concatenation (+), skip equality comparisons (x == "foo").
			if x.Op == token.ADD {
				return true
			}
			return false
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if val, err := strconv.Unquote(x.Value); err == nil && !isAllowedUILiteral(val) {
					pos := fset.Position(x.Pos())
					*out = append(*out, fmt.Sprintf("%s:%d: %s literal %q", pos.Filename, pos.Line, ctx, val))
				}
			}
			return false
		}
		return true
	})
}

func inspectFuncStringReturns(fset *token.FileSet, fn *ast.FuncLit, ctx string, out *[]string) {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return
	}
	id, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || id.Name != "string" {
		return
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			collectDisallowedStrings(fset, res, ctx, out)
		}
		return false
	})
}

func scanFileForUnlocalizedUILiterals(fset *token.FileSet, f *ast.File) []string {
	var violations []string
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typeName := i18nExprTypeName(cl.Type)
		if !uiWidgetTypes[typeName] {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || !uiStringFields[key.Name] {
				continue
			}
			ctx := typeName + "." + key.Name
			if fn, ok := kv.Value.(*ast.FuncLit); ok {
				inspectFuncStringReturns(fset, fn, ctx, &violations)
			} else {
				collectDisallowedStrings(fset, kv.Value, ctx, &violations)
			}
		}
		return true
	})
	return violations
}

// #1503: Lint-style AST guard that fails whenever a non-test file in pkg/editor places an
// unlocalized English string literal into a UI widget field (Text, Content, Label, Placeholder,
// Title, Subtitle, Tooltip, Message, Description, etc., or their *Func callbacks) instead of
// rendering through ed.T / ed.Plural.
func TestEditorUILiteralsLocalized(t *testing.T) {
	fset := token.NewFileSet()
	goFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(goFiles)

	var violations []string
	byFile := map[string]int{}

	for _, file := range goFiles {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		found := scanFileForUnlocalizedUILiterals(fset, f)
		if len(found) > 0 {
			byFile[file] = len(found)
			violations = append(violations, found...)
		}
	}

	if len(violations) > 0 {
		type fileCount struct {
			file  string
			count int
		}
		var summary []fileCount
		for file, c := range byFile {
			summary = append(summary, fileCount{file, c})
		}
		sort.Slice(summary, func(i, j int) bool {
			if summary[i].count != summary[j].count {
				return summary[i].count > summary[j].count
			}
			return summary[i].file < summary[j].file
		})
		var sb strings.Builder
		fmt.Fprintf(&sb, "found %d unlocalized UI string literal(s) across %d file(s):\n", len(violations), len(summary))
		for _, fc := range summary {
			fmt.Fprintf(&sb, "  %4d  %s\n", fc.count, fc.file)
		}
		sb.WriteString("\nFirst 60 violations:\n")
		limit := min(len(violations), 60)
		for _, v := range violations[:limit] {
			sb.WriteString("  " + v + "\n")
		}
		t.Fatal(sb.String())
	}
}

// #1503: Verify the AST lint guard catches every form of unlocalized UI literal while
// permitting ed.T / ed.Plural calls, proper nouns, hex colors, and MCP tool identifiers.
func TestEditorUILiteralsLocalizedCatchesBypass(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantCount int
	}{
		{
			name: "localized and allowlisted pass cleanly",
			src: `package editor
func f(ed *Editor) core.Node {
	return core.View{
		Label: ed.T("ok"),
		Children: []core.Node{
			core.Text{Content: "NSGo"},
			core.Text{Content: "viewport_overlays"},
			ui.Input{Placeholder: "#ffffff"},
			ui.Button{TextFunc: func() string { return ed.T("btn") }},
		},
	}
}`,
			wantCount: 0,
		},
		{
			name: "direct Text, Label, Placeholder, Title literals fail",
			src: `package editor
func f() core.Node {
	return core.View{
		Label: "Settings panel",
		Children: []core.Node{
			ui.Button{Text: "Save changes"},
			ui.Input{Placeholder: "Enter a name"},
			ui.Dialog{Title: "Delete item"},
		},
	}
}`,
			wantCount: 4,
		},
		{
			name: "func() string returning English literal or Sprintf fails",
			src: `package editor
func f(n int) core.Node {
	return core.Text{
		ContentFunc: func() string {
			if n == 0 {
				return "Nothing found"
			}
			return fmt.Sprintf("%d items remaining", n)
		},
	}
}`,
			wantCount: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "sample_ui.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := scanFileForUnlocalizedUILiterals(fset, f)
			if len(got) != tc.wantCount {
				t.Fatalf("scanFileForUnlocalizedUILiterals() returned %d violations (%v), want %d", len(got), got, tc.wantCount)
			}
		})
	}
}
