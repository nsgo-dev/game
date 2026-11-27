package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const macosThemePreviewScene = `version: 3
name: level_01
virtualWidth: 640
virtualHeight: 360
layers:
  - name: actors
    space: world
    order: 0
    objects:
      - id: floor
        x: 0
        y: 300
      - id: knight
        name: Knight
        x: 240
        y: 230
        z: 10
      - id: slime_02
        name: Slime 02
        x: 340
        y: 268
      - id: door
        name: Door
        x: 520
        y: 248
`

// TestSpecLightAndDarkThemeMacOSScreenshots renders the scene editor, the Spec overlay legend,
// and the Spec toast variants on the real native macOS host in both Dark and Light themes
// for side-by-side visual review against design/editor/Spec.dc.html and Toasts.dc.html.
func TestSpecLightAndDarkThemeMacOSScreenshots(t *testing.T) {
	doc, err := sceneformat.Parse([]byte(macosThemePreviewScene))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("editor", func(t *testing.T) {
		ed, err := editor.New(doc.Clone(), nil)
		if err != nil {
			t.Fatal(err)
		}
		ed.SelectObject("knight")
		ed.SetOverlayLegendOpen(true)
		ed.ShowToast(editor.Toast{
			ID:             "kept",
			Kind:           editor.ToastSuccess,
			Message:        "Kept 6 changes to level_01",
			ActionLabel:    "Undo",
			ActionShortcut: "⌘Z",
		})
		ed.ShowToast(editor.Toast{
			ID:      "copied",
			Kind:    editor.ToastInfo,
			Message: "Copied the MCP config for Claude Code",
		})

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		}))

		app.Screenshot("spec-editor-dark")

		// Live system appearance change while Appearance is system repaints without restart.
		app.SetEnvironment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   false,
			Locale: "en-US",
		})
		app.Screenshot("spec-editor-light")
	})

	t.Run("overlay-legend", func(t *testing.T) {
		ed, err := editor.New(doc.Clone(), nil)
		if err != nil {
			t.Fatal(err)
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cx.SetTheme(editor.SpecTheme())
			rev := core.NewSignal(cx, ed.Revision())
			return editor.SpecOverlayLegendView(cx, ed, rev)
		}, nsgotest.Size(1120, 560), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		}))

		app.Screenshot("spec-overlay-legend-dark")

		app.SetEnvironment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   false,
			Locale: "en-US",
		})
		app.Screenshot("spec-overlay-legend-light")
	})

	t.Run("toasts", func(t *testing.T) {
		toastsGallery := func(cx *core.Scope) core.Node {
			cx.SetTheme(editor.SpecTheme())
			return core.Dynamic(func(cx *core.Scope) core.Node {
				p := editor.SpecPaletteFor(cx.Dark())
				title := "Toasts · Light"
				if p.Dark {
					title = "Toasts · Dark"
				}
				return core.View{
					Style: core.Style{
						Width:      core.Pct(100),
						Height:     core.Pct(100),
						AlignItems: core.AlignStart,
						Padding:    core.All(28),
						Gap:        16,
						Background: p.Bg,
					},
					Children: []core.Node{
						core.Text{
							Content: title,
							Style: core.Style{
								FontSize:   20,
								FontWeight: 600,
								Color:      p.TextPrimary,
							},
						},
						editor.SpecToastCard(p, editor.Toast{
							Kind:           editor.ToastSuccess,
							Message:        "Kept 6 changes to level_01",
							ActionLabel:    "Undo",
							ActionShortcut: "⌘Z",
						}, 2, nil),
						editor.SpecToastCard(p, editor.Toast{
							Kind:    editor.ToastInfo,
							Message: "Copied the MCP config for Claude Code",
						}, 0, nil),
						editor.SpecToastCard(p, editor.Toast{
							Kind:        editor.ToastWarning,
							Message:     "level_01.scene.yaml changed on disk and was reloaded",
							ActionLabel: "Show",
						}, 0, nil),
						editor.SpecToastCard(p, editor.Toast{
							Kind:        editor.ToastError,
							Message:     "Couldn't save level_01.scene.yaml: the disk is full",
							ActionLabel: "Retry",
						}, 0, nil),
						editor.SpecToastCard(p, editor.Toast{
							Kind:        editor.ToastProgress,
							Message:     "Exporting dungeon-run for macOS · 62%",
							ActionLabel: "Cancel",
							Progress:    0.62,
						}, 0, nil),
					},
				}
			})
		}

		app := nsgotest.Start(t, toastsGallery, nsgotest.Size(760, 400), nsgotest.Environment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   true,
			Locale: "en-US",
		}))

		app.Screenshot("spec-toasts-dark")

		app.SetEnvironment(core.Environment{
			OS:     core.OSMacOS,
			Idiom:  core.IdiomDesktop,
			Dark:   false,
			Locale: "en-US",
		})
		app.Screenshot("spec-toasts-light")
	})
}
