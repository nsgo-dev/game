package editor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #917: add a key in the Localization panel, translate it (and a plural key) into two locales,
// verify missing translation detection, save both locale YAML files, and load both files into a Catalog.
func TestLocalizationPanelAddTranslateSaveAndLoadCatalog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	ed := newTestEditor(t)

	if err := ed.NewLocalizations(dir, "en", "zh-Hans"); err != nil {
		t.Fatalf("NewLocalizations: %v", err)
	}
	if !ed.LocalizationOpen() {
		t.Fatal("LocalizationOpen = false after NewLocalizations")
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	if !app.Exists("id:l10n-panel") {
		t.Fatal("l10n-panel did not render")
	}

	// Add a plain string key through the panel UI.
	app.Type("id:l10n-add-key-input", "menu.play")
	app.Press("id:l10n-add-key")

	// Both locales should now report missing translations and highlight their cells.
	if got := ed.LocalizationMissingCount(); got != 2 {
		t.Fatalf("missing count = %d, want 2", got)
	}
	if !app.Exists("id:l10n-missing-en-menu.play") || !app.Exists("id:l10n-missing-zh-Hans-menu.play") {
		t.Fatal("missing translation markers not shown for untranslated key")
	}
	if got := app.Find("id:l10n-missing-count").Text; got != "2 missing" {
		t.Fatalf("l10n-missing-count = %q, want %q", got, "2 missing")
	}

	// Translate menu.play into English and Simplified Chinese through the panel inputs.
	app.Type("id:l10n-input-en-menu.play", "Play")
	app.Type("id:l10n-input-zh-Hans-menu.play", "开始游戏")

	if got := ed.LocalizationMissingCount(); got != 0 {
		t.Fatalf("missing count after translating menu.play = %d, want 0", got)
	}
	if app.Exists("id:l10n-missing-en-menu.play") || app.Exists("id:l10n-missing-zh-Hans-menu.play") {
		t.Fatal("missing markers remained after translating menu.play")
	}

	// Add a plural key through the panel UI.
	app.Type("id:l10n-add-key-input", "coins")
	app.Press("id:l10n-add-key-plural")
	app.Press("id:l10n-add-key")

	if !ed.LocalizationKeyPlural("coins") {
		t.Fatal("coins key was not marked plural")
	}
	if got := ed.LocalizationMissingCount(); got != 2 {
		t.Fatalf("missing count after adding plural coins = %d, want 2", got)
	}

	// Translate CLDR plural forms for both locales.
	app.Type("id:l10n-plural-en-coins-zero", "No coins")
	app.Type("id:l10n-plural-en-coins-one", "{count} coin")
	app.Type("id:l10n-plural-en-coins-other", "{count} coins")
	app.Type("id:l10n-plural-zh-Hans-coins-other", "{count} 枚金币")

	if got := ed.LocalizationMissingCount(); got != 0 {
		t.Fatalf("missing count after translating coins = %d, want 0", got)
	}

	// Save both locale files via the panel Save button.
	app.Press("id:l10n-save")

	enBytes, err := os.ReadFile(filepath.Join(dir, "en.yaml"))
	if err != nil {
		t.Fatalf("read en.yaml: %v", err)
	}
	zhBytes, err := os.ReadFile(filepath.Join(dir, "zh-Hans.yaml"))
	if err != nil {
		t.Fatalf("read zh-Hans.yaml: %v", err)
	}

	// Both files must round-trip byte for byte through l10n.ParseYAML / l10n.EncodeYAML.
	for name, raw := range map[string][]byte{"en.yaml": enBytes, "zh-Hans.yaml": zhBytes} {
		parsed, err := l10n.ParseYAML(raw)
		if err != nil {
			t.Fatalf("ParseYAML(%s): %v\n%s", name, err, raw)
		}
		again, err := l10n.EncodeYAML(parsed)
		if err != nil {
			t.Fatalf("EncodeYAML(%s): %v", name, err)
		}
		if !bytes.Equal(again, raw) {
			t.Fatalf("%s did not round-trip byte for byte:\ngot:\n%s\nwant:\n%s", name, again, raw)
		}
	}

	// Load both saved files into a runtime l10n.Catalog and verify lookups & plurals.
	cat := l10n.NewCatalog("en")
	if err := cat.LoadYAML(enBytes); err != nil {
		t.Fatalf("Catalog.LoadYAML(en): %v", err)
	}
	if err := cat.LoadYAML(zhBytes); err != nil {
		t.Fatalf("Catalog.LoadYAML(zh-Hans): %v", err)
	}

	if got := cat.T("en", "menu.play"); got != "Play" {
		t.Errorf("en menu.play = %q, want Play", got)
	}
	if got := cat.T("zh-Hans-CN", "menu.play"); got != "开始游戏" {
		t.Errorf("zh-Hans-CN menu.play = %q, want 开始游戏", got)
	}
	if got := cat.Plural("en", "coins", 0); got != "No coins" {
		t.Errorf("en coins(0) = %q, want No coins", got)
	}
	if got := cat.Plural("en", "coins", 1); got != "1 coin" {
		t.Errorf("en coins(1) = %q, want 1 coin", got)
	}
	if got := cat.Plural("en", "coins", 5); got != "5 coins" {
		t.Errorf("en coins(5) = %q, want 5 coins", got)
	}
	if got := cat.Plural("zh-Hans", "coins", 3); got != "3 枚金币" {
		t.Errorf("zh-Hans coins(3) = %q, want 3 枚金币", got)
	}
}

func TestLocalizationPanelLocalesKeysUndoRedoAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locales")
	ed := newTestEditor(t)

	if err := ed.NewLocalizations(dir, "en"); err != nil {
		t.Fatalf("NewLocalizations: %v", err)
	}
	if err := ed.AddLocalizationLocale("ja"); err != nil {
		t.Fatalf("AddLocalizationLocale(ja): %v", err)
	}
	if err := ed.AddLocalizationLocale("zh_hans"); err != nil {
		t.Fatalf("AddLocalizationLocale(zh_hans): %v", err)
	}
	if got, want := ed.LocalizationLocales(), []string{"en", "ja", "zh-Hans"}; !slices.Equal(got, want) {
		t.Fatalf("locales = %v, want %v", got, want)
	}

	if err := ed.AddLocalizationKey("hud.score", false); err != nil {
		t.Fatalf("AddLocalizationKey: %v", err)
	}
	if err := ed.SetLocalizationString("en", "hud.score", "Score: {score}"); err != nil {
		t.Fatalf("SetLocalizationString en: %v", err)
	}
	if err := ed.SetLocalizationString("ja", "hud.score", "スコア: {score}"); err != nil {
		t.Fatalf("SetLocalizationString ja: %v", err)
	}

	// Only zh-Hans is missing hud.score.
	missing := ed.LocalizationMissing()
	if len(missing) != 1 || missing[0].Locale != "zh-Hans" || missing[0].Key != "hud.score" {
		t.Fatalf("missing = %+v, want [{Locale:zh-Hans Key:hud.score}]", missing)
	}

	// Remove a locale, then Undo restores it with its translations.
	if err := ed.RemoveLocalizationLocale("ja"); err != nil {
		t.Fatalf("RemoveLocalizationLocale: %v", err)
	}
	if slices.Contains(ed.LocalizationLocales(), "ja") {
		t.Fatal("ja still present after RemoveLocalizationLocale")
	}
	if !ed.Undo() {
		t.Fatal("Undo RemoveLocalizationLocale returned false")
	}
	if v, ok := ed.LocalizationValue("ja", "hud.score"); !ok || v != "スコア: {score}" {
		t.Fatalf("after Undo, ja hud.score = %q, %v", v, ok)
	}

	// Remove a key, then Undo restores it across all locales, and Redo removes it again.
	if err := ed.RemoveLocalizationKey("hud.score"); err != nil {
		t.Fatalf("RemoveLocalizationKey: %v", err)
	}
	if len(ed.LocalizationKeys()) != 0 {
		t.Fatalf("keys after remove = %v", ed.LocalizationKeys())
	}
	if !ed.Undo() {
		t.Fatal("Undo RemoveLocalizationKey returned false")
	}
	if v, ok := ed.LocalizationValue("en", "hud.score"); !ok || v != "Score: {score}" {
		t.Fatalf("after Undo key remove, en hud.score = %q, %v", v, ok)
	}
	if !ed.Redo() {
		t.Fatal("Redo RemoveLocalizationKey returned false")
	}
	if len(ed.LocalizationKeys()) != 0 {
		t.Fatalf("keys after Redo remove = %v", ed.LocalizationKeys())
	}
	if !ed.Undo() {
		t.Fatal("second Undo returned false")
	}

	// Save, remove zh-Hans, save again -> zh-Hans.yaml is removed from disk.
	if err := ed.SaveLocalizations(); err != nil {
		t.Fatalf("SaveLocalizations: %v", err)
	}
	if ed.LocalizationDirty() {
		t.Fatal("LocalizationDirty = true right after SaveLocalizations")
	}
	if err := ed.RemoveLocalizationLocale("zh-Hans"); err != nil {
		t.Fatalf("RemoveLocalizationLocale(zh-Hans): %v", err)
	}
	if !ed.LocalizationDirty() {
		t.Fatal("LocalizationDirty = false after removing locale")
	}
	if err := ed.SaveLocalizations(); err != nil {
		t.Fatalf("SaveLocalizations 2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "zh-Hans.yaml")); !os.IsNotExist(err) {
		t.Fatalf("zh-Hans.yaml still exists after removing locale and saving: %v", err)
	}

	// Reopen in a fresh editor and verify exact state.
	reopened := newTestEditor(t)
	if err := reopened.OpenLocalizations(dir); err != nil {
		t.Fatalf("OpenLocalizations: %v", err)
	}
	if got, want := reopened.LocalizationLocales(), []string{"en", "ja"}; !slices.Equal(got, want) {
		t.Fatalf("reopened locales = %v, want %v", got, want)
	}
	if got, want := reopened.LocalizationKeys(), []string{"hud.score"}; !slices.Equal(got, want) {
		t.Fatalf("reopened keys = %v, want %v", got, want)
	}
	if v, ok := reopened.LocalizationValue("ja", "hud.score"); !ok || v != "スコア: {score}" {
		t.Fatalf("reopened ja hud.score = %q, %v", v, ok)
	}
	if reopened.LocalizationDirty() {
		t.Fatal("reopened workspace should not be dirty")
	}
}

func TestLocalizationPanelUIAddRemoveLocaleAndKey(t *testing.T) {
	ed := newTestEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 900))

	// Open panel via toolbar Strings toggle.
	app.Press("id:toolbar-l10n")
	if !app.Exists("id:l10n-panel") {
		t.Fatal("toolbar-l10n did not open l10n-panel")
	}

	// Add locale "fr" via UI.
	app.Type("id:l10n-add-locale-input", "fr")
	app.Press("id:l10n-add-locale")
	if !app.Exists("id:l10n-col-fr") {
		t.Fatal("l10n-col-fr not rendered after adding fr")
	}

	// Add key and remove it via UI.
	app.Type("id:l10n-add-key-input", "ui.ok")
	app.Press("id:l10n-add-key")
	if !app.Exists("id:l10n-row-ui.ok") {
		t.Fatal("l10n-row-ui.ok not rendered after adding key")
	}
	app.Press("id:l10n-remove-key-ui.ok")
	if app.Exists("id:l10n-row-ui.ok") {
		t.Fatal("l10n-row-ui.ok still present after Remove")
	}

	// Remove locale "fr" via UI.
	app.Press("id:l10n-remove-locale-fr")
	if app.Exists("id:l10n-col-fr") {
		t.Fatal("l10n-col-fr still present after Remove")
	}

	// Close via Close button.
	app.Press("id:l10n-close")
	if app.Exists("id:l10n-panel") {
		t.Fatal("l10n-panel still open after Close")
	}
}
