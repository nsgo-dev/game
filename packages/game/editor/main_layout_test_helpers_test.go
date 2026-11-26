package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func runEditorCommand(t *testing.T, app *nsgotest.App, ed *editor.Editor, cmdID string) {
	t.Helper()
	var err error
	app.Post(func() {
		err = ed.ExecuteCommand(cmdID)
	})
	app.Settle()
	if err != nil {
		t.Fatalf("ExecuteCommand(%q): %v", cmdID, err)
	}
}

func undoInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Post(func() { ed.Undo() })
	app.Settle()
}

func redoInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Post(func() { ed.Redo() })
	app.Settle()
}

func saveInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Press("id:toolbar-file")
	app.Press("id:toolbar-menu-save")
	app.Settle()
}

func openFileBrowserInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Post(func() { ed.SetFileBrowserOpen(true) })
	app.Settle()
}

func openAssetBrowserInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Post(func() { ed.SetAssetBrowserOpen(true) })
	app.Settle()
}

func openLayersPanelInUI(app *nsgotest.App, ed *editor.Editor) {
	app.Post(func() { ed.SetLayersPanelOpen(true) })
	app.Settle()
}
