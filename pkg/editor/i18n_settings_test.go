package editor_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

var placeholderPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func extractPlaceholders(s string) []string {
	matches := placeholderPattern.FindAllStringSubmatch(s, -1)
	var out []string
	for _, m := range matches {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

// #921: Every translation key must exist in both English (locales/en.yaml) and Simplified Chinese (locales/zh.yaml),
// both files must be valid canonical l10n YAML, placeholders must match, and every key referenced in Go code must exist.
func TestLocaleCatalogsComplete(t *testing.T) {
	enData, err := os.ReadFile(filepath.Join("locales", "en.yaml"))
	if err != nil {
		t.Fatalf("read locales/en.yaml: %v", err)
	}
	zhData, err := os.ReadFile(filepath.Join("locales", "zh.yaml"))
	if err != nil {
		t.Fatalf("read locales/zh.yaml: %v", err)
	}

	enTbl, err := l10n.ParseYAML(enData)
	if err != nil {
		t.Fatalf("parse locales/en.yaml: %v", err)
	}
	zhTbl, err := l10n.ParseYAML(zhData)
	if err != nil {
		t.Fatalf("parse locales/zh.yaml: %v", err)
	}
	if enTbl.Locale != "en" {
		t.Fatalf("locales/en.yaml locale = %q, want en", enTbl.Locale)
	}
	if zhTbl.Locale != "zh" {
		t.Fatalf("locales/zh.yaml locale = %q, want zh", zhTbl.Locale)
	}

	// Verify both YAML files round-trip canonically through l10n.EncodeYAML.
	for _, item := range []struct {
		path string
		tbl  *l10n.StringTable
		data []byte
	}{
		{filepath.Join("locales", "en.yaml"), enTbl, enData},
		{filepath.Join("locales", "zh.yaml"), zhTbl, zhData},
	} {
		canon, err := l10n.EncodeYAML(item.tbl)
		if err != nil {
			t.Fatalf("encode %s: %v", item.path, err)
		}
		if os.Getenv("FORMATTEST_UPDATE") == "1" {
			if err := os.WriteFile(item.path, canon, 0o644); err != nil {
				t.Fatalf("rewrite %s: %v", item.path, err)
			}
			continue
		}
		if !bytes.Equal(canon, item.data) {
			t.Errorf("%s is not in canonical l10n.EncodeYAML form (run with FORMATTEST_UPDATE=1 to rewrite)", item.path)
		}
	}

	// Verify exact key parity and placeholder parity across strings.
	for k, enVal := range enTbl.Strings {
		if strings.TrimSpace(enVal) == "" {
			t.Errorf("locales/en.yaml key %q has empty value", k)
		}
		zhVal, ok := zhTbl.Strings[k]
		if !ok {
			t.Errorf("key %q exists in locales/en.yaml but is missing in locales/zh.yaml", k)
			continue
		}
		if strings.TrimSpace(zhVal) == "" {
			t.Errorf("locales/zh.yaml key %q has empty value", k)
		}
		enPH := extractPlaceholders(enVal)
		zhPH := extractPlaceholders(zhVal)
		if !slices.Equal(enPH, zhPH) {
			t.Errorf("placeholder mismatch for key %q: en=%v, zh=%v", k, enPH, zhPH)
		}
	}
	for k := range zhTbl.Strings {
		if _, ok := enTbl.Strings[k]; !ok {
			t.Errorf("key %q exists in locales/zh.yaml but is missing in locales/en.yaml", k)
		}
	}

	// Verify exact key parity across plurals.
	for k, enForms := range enTbl.Plurals {
		zhForms, ok := zhTbl.Plurals[k]
		if !ok {
			t.Errorf("plural key %q exists in locales/en.yaml but is missing in locales/zh.yaml", k)
			continue
		}
		if strings.TrimSpace(enForms.Other) == "" {
			t.Errorf("locales/en.yaml plural %q has empty other form", k)
		}
		if strings.TrimSpace(zhForms.Other) == "" {
			t.Errorf("locales/zh.yaml plural %q has empty other form", k)
		}
		enPH := extractPlaceholders(enForms.Other)
		zhPH := extractPlaceholders(zhForms.Other)
		if !slices.Equal(enPH, zhPH) {
			t.Errorf("placeholder mismatch for plural %q: en=%v, zh=%v", k, enPH, zhPH)
		}
	}
	for k := range zhTbl.Plurals {
		if _, ok := enTbl.Plurals[k]; !ok {
			t.Errorf("plural key %q exists in locales/zh.yaml but is missing in locales/en.yaml", k)
		}
	}

	// Scan all non-test Go files in package editor for literal ed.T("...") / e.T("...") / Plural("...") calls
	// and verify every referenced key exists in both catalogs.
	fset := token.NewFileSet()
	goFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range goFiles {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "T" && sel.Sel.Name != "Plural") {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if sel.Sel.Name == "T" {
				if _, ok := enTbl.Strings[key]; !ok {
					t.Errorf("%s: T(%q) missing from locales/en.yaml", file, key)
				}
				if _, ok := zhTbl.Strings[key]; !ok {
					t.Errorf("%s: T(%q) missing from locales/zh.yaml", file, key)
				}
			} else {
				if _, ok := enTbl.Plurals[key]; !ok {
					t.Errorf("%s: Plural(%q) missing from locales/en.yaml", file, key)
				}
				if _, ok := zhTbl.Plurals[key]; !ok {
					t.Errorf("%s: Plural(%q) missing from locales/zh.yaml", file, key)
				}
			}
			return true
		})
	}
}

// #921: EditorSettings strict parsing, validation, canonical encoding, and per-user persistence.
func TestEditorSettingsParseEncodeAndPersist(t *testing.T) {
	def := editor.DefaultEditorSettings()
	if def.Version != editor.SettingsVersion || def.Language != editor.LanguageSystem || def.Appearance != editor.AppearanceSystem {
		t.Fatalf("DefaultEditorSettings = %+v", def)
	}
	encoded, err := editor.EncodeEditorSettings(&def)
	if err != nil {
		t.Fatalf("EncodeEditorSettings: %v", err)
	}
	if !strings.HasPrefix(string(encoded), editor.SettingsSchemaComment+"\n") {
		t.Fatalf("encoded settings missing schema comment:\n%s", encoded)
	}
	parsed, err := editor.ParseEditorSettings(encoded)
	if err != nil {
		t.Fatalf("ParseEditorSettings: %v", err)
	}
	if !reflect.DeepEqual(*parsed, def) {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", *parsed, def)
	}

	// Refuse invalid values, unknown fields, trailing documents, and anchors/aliases.
	badInputs := map[string]string{
		"empty":          "",
		"invalidLang":    "version: 1\nlanguage: fr\nappearance: system\n",
		"invalidAppear":  "version: 1\nlanguage: en\nappearance: neon\n",
		"unknownField":   "version: 1\nlanguage: en\nappearance: dark\nextra: true\n",
		"missingVersion": "language: en\nappearance: dark\n",
		"futureVersion":  "version: 2\nlanguage: en\nappearance: dark\n",
		"trailingDoc":    "version: 1\nlanguage: en\nappearance: dark\n---\nversion: 1\n",
		"yamlAnchor":     "version: 1\nlanguage: &l en\nappearance: system\n",
		"duplicateKey":   "version: 1\nlanguage: en\nlanguage: zh\nappearance: dark\n",
		"nonPlainVer":    "version: 0x1\nlanguage: en\nappearance: dark\n",
	}
	for name, raw := range badInputs {
		t.Run(name, func(t *testing.T) {
			if _, err := editor.ParseEditorSettings([]byte(raw)); err == nil {
				t.Fatalf("ParseEditorSettings(%s) succeeded, want error", name)
			}
		})
	}

	// SaveEditorSettings refuses empty path without touching disk.
	if err := editor.SaveEditorSettings("   ", &def); err == nil {
		t.Fatal("SaveEditorSettings(empty) succeeded, want error")
	}
	// SaveEditorSettings and LoadEditorSettings round-trip on disk in a nested non-existent directory.
	path := filepath.Join(t.TempDir(), "nested", "editor.yaml")
	want := editor.EditorSettings{
		Version:    editor.SettingsVersion,
		Language:   editor.LanguageChinese,
		Appearance: editor.AppearanceDark,
	}
	if err := editor.SaveEditorSettings(path, &want); err != nil {
		t.Fatalf("SaveEditorSettings: %v", err)
	}
	loaded, err := editor.LoadEditorSettings(path)
	if err != nil {
		t.Fatalf("LoadEditorSettings: %v", err)
	}
	if !reflect.DeepEqual(*loaded, want) {
		t.Fatalf("loaded = %+v, want %+v", *loaded, want)
	}

	// DefaultSettingsPath resolves to a non-empty path ending with editor.yaml.
	if defPath, err := editor.DefaultSettingsPath(); err != nil || filepath.Base(defPath) != "editor.yaml" {
		t.Fatalf("DefaultSettingsPath() = (%q, %v)", defPath, err)
	}
}

// #921 Acceptance 1: With language set to Chinese, toolbar, file browser, inspector, menu bar,
// and status bar show Chinese labels, and switching back to English requires no restart.
func TestEditorUIChineseAndLiveSwitchWithoutRestart(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.WaitForProblems()
	ed.SelectObject("hero")

	settingsFile := filepath.Join(t.TempDir(), "editor.yaml")
	if err := ed.LoadSettings(settingsFile); err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "en-US"}))

	// Initially English.
	openFileBrowserInUI(app, ed)
	openAssetBrowserInUI(app, ed)
	openLayersPanelInUI(app, ed)
	if got := allText(app.Find("id:editor-toolbar")); !strings.Contains(got, "Search") || !strings.Contains(got, "Play") {
		t.Fatalf("initial English toolbar missing Search/Play: %q", got)
	}
	if got := allText(app.Find("id:file-browser")); !strings.Contains(got, "Files") || !strings.Contains(got, "Refresh") {
		t.Fatalf("initial English file browser missing Files/Refresh: %q", got)
	}
	if got := allText(app.Find("id:editor-inspector")); !strings.Contains(got, "Inspector") || !strings.Contains(got, "TRANSFORM") {
		t.Fatalf("initial English inspector missing Inspector/TRANSFORM: %q", got)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Select") {
		t.Fatalf("initial English status bar = %q", got)
	}
	if len(app.Menu) != 5 || app.Menu[0].Label != "File" || app.Menu[1].Label != "Edit" || app.Menu[4].Label != "Help" {
		t.Fatalf("initial English menu = %v", app.Menu)
	}

	// Open Editor Settings via command and switch to Chinese live.
	runEditorCommand(t, app, ed, editor.CmdEditorSettings)
	if !ed.EditorSettingsOpen() {
		t.Fatal("CmdEditorSettings did not open Editor Settings dialog")
	}
	app.Press("id:editor-settings-lang-zh")
	if ed.Language() != editor.LanguageChinese || ed.ResolvedLanguage() != "zh" {
		t.Fatalf("after choosing Chinese: Language=%q Resolved=%q", ed.Language(), ed.ResolvedLanguage())
	}

	// Verify toolbar, file browser, asset browser, scene tree, layers, inspector, status bar, and menu bar are Chinese.
	if got := allText(app.Find("id:editor-toolbar")); !strings.Contains(got, "搜索") || !strings.Contains(got, "播放") {
		t.Fatalf("Chinese toolbar = %q", got)
	}
	if got := allText(app.Find("id:file-browser")); !strings.Contains(got, "文件") || !strings.Contains(got, "打开") || !strings.Contains(got, "刷新") || !strings.Contains(got, "移动") {
		t.Fatalf("Chinese file browser = %q", got)
	}
	if got := allText(app.Find("id:asset-browser")); !strings.Contains(got, "资源") {
		t.Fatalf("Chinese asset browser = %q", got)
	}
	if got := allText(app.Find("id:scene-rail-title")); !strings.Contains(got, "场景") {
		t.Fatalf("Chinese scene rail title = %q", got)
	}
	if got := allText(app.Find("id:layers-panel")); !strings.Contains(got, "图层") {
		t.Fatalf("Chinese layers panel = %q", got)
	}
	if got := allText(app.Find("id:editor-inspector")); !strings.Contains(got, "检查器") || !strings.Contains(got, "名称") || !strings.Contains(got, "变换") || !strings.Contains(got, "删除") {
		t.Fatalf("Chinese inspector = %q", got)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "选择") {
		t.Fatalf("Chinese status bar mode = %q", got)
	}
	if got := allText(app.Find("id:status-active-layer")); !strings.Contains(got, "图层") {
		t.Fatalf("Chinese status bar layer = %q", got)
	}
	if got := app.Find("id:status-problems").Text; !strings.Contains(got, "无问题") && !strings.Contains(got, "问题") {
		t.Fatalf("Chinese status bar problems = %q", got)
	}
	if len(app.Menu) != 5 || app.Menu[0].Label != "文件" || app.Menu[1].Label != "编辑" || app.Menu[2].Label != "视图" || app.Menu[3].Label != "项目" || app.Menu[4].Label != "帮助" {
		t.Fatalf("Chinese desktop menu bar = [%s, %s, %s, %s, %s]", app.Menu[0].Label, app.Menu[1].Label, app.Menu[2].Label, app.Menu[3].Label, app.Menu[4].Label)
	}

	// Switch back to English live without restart!
	app.Press("id:editor-settings-lang-en")
	if ed.Language() != editor.LanguageEnglish || ed.ResolvedLanguage() != "en" {
		t.Fatalf("after switching back to English: Language=%q Resolved=%q", ed.Language(), ed.ResolvedLanguage())
	}
	if got := allText(app.Find("id:editor-toolbar")); !strings.Contains(got, "Search") || !strings.Contains(got, "Play") {
		t.Fatalf("switched-back English toolbar = %q", got)
	}
	if got := allText(app.Find("id:file-browser")); !strings.Contains(got, "Files") || !strings.Contains(got, "Open") {
		t.Fatalf("switched-back English file browser = %q", got)
	}
	if got := allText(app.Find("id:editor-inspector")); !strings.Contains(got, "Inspector") || !strings.Contains(got, "TRANSFORM") {
		t.Fatalf("switched-back English inspector = %q", got)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Select") {
		t.Fatalf("switched-back English status bar = %q", got)
	}
	if app.Menu[0].Label != "File" {
		t.Fatalf("switched-back English menu[0] = %q, want File", app.Menu[0].Label)
	}

	app.Press("id:editor-settings-close")
	if ed.EditorSettingsOpen() {
		t.Fatal("editor-settings-close did not close dialog")
	}
}

// #921: System language mode follows the OS/environment locale (e.g. zh-CN -> Chinese).
func TestEditorLanguageFollowsSystemLocale(t *testing.T) {
	ed := newTestEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))

	if ed.Language() != editor.LanguageSystem || ed.ResolvedLanguage() != "zh" {
		t.Fatalf("system zh-CN: Language=%q Resolved=%q", ed.Language(), ed.ResolvedLanguage())
	}
	if got := allText(app.Find("id:editor-toolbar")); !strings.Contains(got, "搜索") || !strings.Contains(got, "播放") {
		t.Fatalf("system zh-CN toolbar = %q", got)
	}
	if got := app.Find("id:tool-select").Props.Label; got != "选择" {
		t.Fatalf("system zh-CN tool-select label = %q, want 选择", got)
	}
}

// #921: Plugins can supply their own en/zh YAML catalogs and follow the editor's active language live.
func TestPluginLocalizationFollowsEditorLanguageLive(t *testing.T) {
	enCatalogYAML := []byte(`version: 1
locale: en
strings:
  panel.title: Health Monitor
  panel.healAll: Heal All
  inspector.label: Current HP
  tool.spawn: Spawn Unit
  cmd.healAll: Heal All Units
  cmd.healAllDesc: Restore every unit to full HP
  cmd.category: Combat
`)
	zhCatalogYAML := []byte(`version: 1
locale: zh
strings:
  panel.title: 生命值监控
  panel.healAll: 全部治疗
  inspector.label: 当前生命值
  tool.spawn: 生成单位
  cmd.healAll: 治疗所有单位
  cmd.healAllDesc: 将所有单位恢复至满生命值
  cmd.category: 战斗
`)

	dir := writePluginProject(t, "health")
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentRegistry(healthComponents(t)); err != nil {
		t.Fatal(err)
	}

	// Verify RegisterPlugin rejects malformed plugin LocaleYAML.
	if err := ed.RegisterPlugin(editor.Plugin{
		Name:       "bad-l10n",
		LocaleYAML: [][]byte{[]byte("version: 99\nlocale: en\n")},
	}); err == nil {
		t.Fatal("RegisterPlugin accepted invalid LocaleYAML, want error")
	}

	if err := ed.RegisterPlugin(editor.Plugin{
		Name:       "health",
		LocaleYAML: [][]byte{enCatalogYAML, zhCatalogYAML},
		Panels: []editor.Panel{{
			ID:       "overview",
			Title:    "Health Monitor",
			TitleKey: "panel.title",
			Dock:     editor.DockRight,
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64]) core.Node {
				return ui.Button{
					TestID: "health-localized-btn",
					TextFunc: func() string {
						_ = rev.Get()
						return ed.PluginT("health", "panel.healAll")
					},
				}
			},
		}},
		Inspectors: []editor.Inspector{{
			Component: "health",
			View: func(cx *core.Scope, ed *editor.Editor, rev *core.Signal[uint64], id string) core.Node {
				return core.Text{
					TestID: "health-localized-inspector",
					ContentFunc: func() string {
						_ = rev.Get()
						return ed.PluginT("health", "inspector.label")
					},
				}
			},
		}},
		Tools: []editor.PluginTool{{
			ID:       "spawn",
			Label:    "Spawn Unit",
			LabelKey: "tool.spawn",
			Press:    func(*editor.Editor, float32, float32) {},
		}},
		Commands: []editor.PluginCommand{{
			ID:             "heal-all",
			Label:          "Heal All Units",
			LabelKey:       "cmd.healAll",
			Description:    "Restore every unit to full HP",
			DescriptionKey: "cmd.healAllDesc",
			Category:       "Combat",
			CategoryKey:    "cmd.category",
			Run:            func(*editor.Editor) error { return nil },
		}},
	}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	ed.SelectObject("hero")
	if err := ed.SetPluginTool("health", "spawn"); err != nil {
		t.Fatal(err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "en-US"}))

	// English check.
	if got := allText(app.Find("id:plugin-panel-health-overview")); !strings.Contains(got, "Health Monitor") || !strings.Contains(got, "Heal All") {
		t.Fatalf("English plugin panel = %q", got)
	}
	if got := app.Find("id:health-localized-inspector").Text; got != "Current HP" {
		t.Fatalf("English plugin inspector = %q, want Current HP", got)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "Spawn Unit") {
		t.Fatalf("English status bar with plugin tool = %q", got)
	}
	if _, ok := findMenuItem(app.Menu, "Project", "Plugins", "Heal All Units"); !ok {
		t.Fatal("English plugin command missing from menu")
	}

	// Switch to Chinese live -> every plugin UI element follows without restart!
	app.Post(func() {
		if err := ed.SetLanguage(editor.LanguageChinese); err != nil {
			t.Errorf("SetLanguage(zh): %v", err)
		}
	})
	app.Settle()

	if got := allText(app.Find("id:plugin-panel-health-overview")); !strings.Contains(got, "生命值监控") || !strings.Contains(got, "全部治疗") {
		t.Fatalf("Chinese plugin panel = %q", got)
	}
	if got := app.Find("id:health-localized-inspector").Text; got != "当前生命值" {
		t.Fatalf("Chinese plugin inspector = %q, want 当前生命值", got)
	}
	if got := app.Find("id:editor-status-mode").Text; !strings.Contains(got, "生成单位") {
		t.Fatalf("Chinese status bar with plugin tool = %q", got)
	}
	if _, ok := findMenuItem(app.Menu, "项目", "插件", "治疗所有单位"); !ok {
		t.Fatal("Chinese plugin command missing from menu")
	}
	if cmd, ok := ed.Command("plugin.health.heal-all"); !ok || cmd.Label != "治疗所有单位" || cmd.Category != "战斗" {
		t.Fatalf("Chinese plugin command = (%+v, %v)", cmd, ok)
	}

	// Switch back to English live!
	app.Post(func() {
		if err := ed.SetLanguage(editor.LanguageEnglish); err != nil {
			t.Errorf("SetLanguage(en): %v", err)
		}
	})
	app.Settle()

	if got := allText(app.Find("id:plugin-panel-health-overview")); !strings.Contains(got, "Health Monitor") {
		t.Fatalf("reverted English plugin panel = %q", got)
	}
}

// #921 Acceptance 2: Choosing dark keeps the editor dark while the system is light,
// and the choice survives saving/reloading settings across a restart.
func TestEditorAppearanceDarkOnLightSystemSurvivesRestart(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "user-config", "editor.yaml")

	ed := newTestEditor(t)
	if err := ed.LoadSettings(settingsPath); err != nil {
		t.Fatalf("LoadSettings (missing file): %v", err)
	}

	var scopeDark bool
	var scopeAppearance core.Appearance
	lightEnv := nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: false, Locale: "en-US"})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Effect(func() {
			scopeDark = cx.Env().Dark()
			scopeAppearance = cx.Env().Appearance()
		})
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1200, 760), lightEnv)

	// Starts following light system.
	if scopeDark || ed.Dark() {
		t.Fatalf("expected initial light appearance on light system, got scopeDark=%v ed.Dark=%v", scopeDark, ed.Dark())
	}

	// Open Editor Settings and choose Dark while system remains light.
	runEditorCommand(t, app, ed, editor.CmdEditorSettings)
	app.Press("id:editor-settings-appearance-dark")

	if ed.Appearance() != editor.AppearanceDark {
		t.Fatalf("Appearance() = %q, want dark", ed.Appearance())
	}
	if !scopeDark || !ed.Dark() || scopeAppearance != core.AppearanceDark {
		t.Fatalf("expected forced dark on light system: scopeDark=%v ed.Dark=%v scopeAppearance=%v", scopeDark, ed.Dark(), scopeAppearance)
	}

	// Verify persisted YAML on disk outside any project.
	diskData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read persisted settings: %v", err)
	}
	reloadedSettings, err := editor.ParseEditorSettings(diskData)
	if err != nil {
		t.Fatalf("ParseEditorSettings(disk): %v", err)
	}
	if reloadedSettings.Appearance != editor.AppearanceDark {
		t.Fatalf("persisted Appearance = %q, want dark", reloadedSettings.Appearance)
	}

	// Simulate a full restart: brand-new Editor + brand-new UI session on a light system.
	restarted := newTestEditor(t)
	if err := restarted.LoadSettings(settingsPath); err != nil {
		t.Fatalf("restarted.LoadSettings: %v", err)
	}
	var restartedDark bool
	var restartedAppearance core.Appearance
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.Effect(func() {
			restartedDark = cx.Env().Dark()
			restartedAppearance = cx.Env().Appearance()
		})
		return editor.EditorView(cx, restarted)
	}, nsgotest.Size(1200, 760), lightEnv)

	if restarted.Appearance() != editor.AppearanceDark {
		t.Fatalf("restarted Appearance() = %q, want dark", restarted.Appearance())
	}
	if !restartedDark || !restarted.Dark() || restartedAppearance != core.AppearanceDark {
		t.Fatalf("restarted editor not dark on light system: restartedDark=%v ed.Dark=%v appearance=%v", restartedDark, restarted.Dark(), restartedAppearance)
	}

	// Switching to Light works even on a dark system, and System follows live system updates.
	app2.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: true, Locale: "en-US"})
	app2.Post(func() {
		if err := restarted.SetAppearance(editor.AppearanceLight); err != nil {
			t.Errorf("SetAppearance(light): %v", err)
		}
	})
	app2.Settle()
	if restartedDark || restarted.Dark() || restartedAppearance != core.AppearanceLight {
		t.Fatalf("forced light on dark system failed: dark=%v ed.Dark=%v appearance=%v", restartedDark, restarted.Dark(), restartedAppearance)
	}

	app2.Post(func() {
		if err := restarted.SetAppearance(editor.AppearanceSystem); err != nil {
			t.Errorf("SetAppearance(system): %v", err)
		}
	})
	app2.Settle()
	if !restartedDark || !restarted.Dark() || restartedAppearance != core.AppearanceSystem {
		t.Fatalf("system appearance on dark system failed: dark=%v ed.Dark=%v appearance=%v", restartedDark, restarted.Dark(), restartedAppearance)
	}
}
