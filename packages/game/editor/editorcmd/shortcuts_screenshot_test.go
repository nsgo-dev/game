package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestShortcutsMacOSScreenshots renders real macOS screenshots of Settings › Shortcuts,
// the in-place same-scope conflict banner with Swap, and the ⌘/ Keyboard Shortcuts sheet (#1061).
func TestShortcutsMacOSScreenshots(t *testing.T) {
	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	_ = ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   true,
		Locale: "en-US",
	}))

	// 1. Settings › Shortcuts page showing searchable commands and scope pills.
	app.Post(func() { ed.OpenShortcutsSettings() })
	app.Settle()
	app.Screenshot("shortcuts-settings")

	// 2. Same-scope conflict flagged in place with Swap (Frame Selection -> V, conflicting with Tool: Select in Viewport).
	app.Type("id:shortcuts-search", "Frame")
	app.Press("id:shortcut-bind-" + editor.CmdFrameSelection)
	app.Key("v")
	app.Screenshot("shortcuts-conflict")

	// 3. Swap, close Settings, and open the ⌘/ Keyboard Shortcuts reference sheet showing the updated bindings.
	app.Press("id:shortcut-conflict-swap")
	app.Press("id:editor-settings-close")
	app.Key("meta+/")
	app.Screenshot("shortcuts-sheet")
}
