package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestToastsMacOSScreenshots renders all five Spec toast kinds, the completed export state,
// viewport-anchored placement with the +N waiting badge, and window-anchored placement on
// full-window Settings on the real native macOS host beside docs/design/editor/Toasts.dc.html.
//
// Run: go tool nsgo test -target macos -run TestToastsMacOSScreenshots ./editor/editorcmd
func TestToastsMacOSScreenshots(t *testing.T) {
	t.Run("gallery", func(t *testing.T) {
		gallery := func(cx *core.Scope) core.Node {
			cx.SetTheme(editor.SpecTheme())
			return core.Dynamic(func(cx *core.Scope) core.Node {
				p := editor.SpecPaletteFor(cx.Dark())
				title := "Toast · Light"
				if p.Dark {
					title = "Toast · Dark"
				}
				return core.View{
					Style: core.Style{
						Width:      core.Pct(100),
						Height:     core.Pct(100),
						AlignItems: core.AlignStart,
						Padding:    core.All(32),
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
							Message:     "Exporting dungeon-run for macOS",
							ActionLabel: "Cancel",
							Progress:    0.62,
						}, 0, nil),
						editor.SpecToastCard(p, editor.Toast{
							Kind:        editor.ToastSuccess,
							Message:     "Exported build/macos/dungeon-run.app",
							ActionLabel: "Show in Finder",
						}, 0, nil),
					},
				}
			})
		}

		app := nsgotest.Start(t, gallery, nsgotest.Size(760, 480), nsgotest.Environment(core.Environment{
			OS:           core.OSMacOS,
			Idiom:        core.IdiomDesktop,
			Dark:         true,
			ReduceMotion: true,
			Locale:       "en-US",
			Lifecycle:    core.LifecycleActive,
		}))
		app.Screenshot("toasts-gallery-dark")

		app.SetEnvironment(core.Environment{
			OS:           core.OSMacOS,
			Idiom:        core.IdiomDesktop,
			Dark:         false,
			ReduceMotion: true,
			Locale:       "en-US",
			Lifecycle:    core.LifecycleActive,
		})
		app.Screenshot("toasts-gallery-light")
	})

	t.Run("placement", func(t *testing.T) {
		doc, err := sceneformat.Parse([]byte(macosThemePreviewScene))
		if err != nil {
			t.Fatal(err)
		}
		ed, err := editor.New(doc.Clone(), nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ed.SetLanguage(editor.LanguageEnglish)
		ed.SelectObject("knight")
		ed.EmitToastEvent(editor.ToastEvent{
			Kind:     editor.ToastEventReviewKept,
			Target:   "level_01",
			Message:  "Kept 6 changes to level_01",
			OnAction: func() {},
		})
		ed.EmitToastEvent(editor.ToastEvent{
			Kind:    editor.ToastEventCopied,
			Target:  "mcp-claude",
			Message: "Copied the MCP config for Claude Code",
		})
		ed.EmitToastEvent(editor.ToastEvent{
			Kind:     editor.ToastEventDiskChanged,
			Target:   "scenes/level_01.scene.yaml",
			Message:  "level_01.scene.yaml changed on disk and was reloaded",
			OnAction: func() {},
		})

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return editor.EditorView(cx, ed)
		}, nsgotest.Size(1440, 900), nsgotest.Environment(core.Environment{
			OS:           core.OSMacOS,
			Idiom:        core.IdiomDesktop,
			Dark:         true,
			ReduceMotion: true,
			Locale:       "en-US",
			Lifecycle:    core.LifecycleActive,
		}))
		app.Screenshot("toasts-viewport-placement-dark")

		app.SetEnvironment(core.Environment{
			OS:           core.OSMacOS,
			Idiom:        core.IdiomDesktop,
			Dark:         false,
			ReduceMotion: true,
			Locale:       "en-US",
			Lifecycle:    core.LifecycleActive,
		})
		app.Screenshot("toasts-viewport-placement-light")

		app.SetEnvironment(core.Environment{
			OS:           core.OSMacOS,
			Idiom:        core.IdiomDesktop,
			Dark:         true,
			ReduceMotion: true,
			Locale:       "en-US",
			Lifecycle:    core.LifecycleActive,
		})
		app.Post(func() {
			ed.OpenSettingsPage(editor.SettingsPageGeneral)
		})
		app.Settle()
		app.Screenshot("toasts-window-placement-settings")
	})
}
