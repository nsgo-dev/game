package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestFocusRingScreenshots captures real macOS native screenshots (via
// `go tool nsgo test -target macos -run TestFocusRingScreenshots ./editor/editorcmd`)
// showing the Spec "Focus ring" controls (button, icon button, switch, tree row,
// field, composer card :focus-within, and F6 region) alongside the live EditorView
// under F6 region cycling.
func TestFocusRingScreenshots(t *testing.T) {
	var (
		bg           = core.RGB(0x101216)
		panel        = core.RGB(0x12141A)
		raised       = core.RGB(0x171A21)
		line         = core.RGB(0x1F232A)
		lineRaised   = core.RGB(0x262B35)
		textStrong   = core.RGB(0xFFFFFF)
		textPrimary  = core.RGB(0xD9DDE5)
		textSec      = core.RGB(0xB4BAC6)
		textMuted    = core.RGB(0x8B92A0)
		accent       = core.RGB(0x3B7BFF)
		changed      = core.RGB(0xE9A84A)
		selectedFill = core.RGB(0x1C2638)
	)
	ringOut := core.Style{OutlineWidth: 2, OutlineColor: accent, OutlineOffset: 2}
	ringIn := core.Style{OutlineWidth: 2, OutlineColor: accent, OutlineOffset: -2}

	card := func(caption string, body core.Node) core.Node {
		return core.View{
			Style: core.Style{
				FlexGrow:       1,
				FlexBasis:      core.Pt(0),
				Height:         core.Pt(148),
				CornerRadius:   12,
				Background:     panel,
				BorderWidth:    1,
				BorderColor:    line,
				Padding:        core.Symmetric(16, 20),
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifyCenter,
				Gap:            16,
			},
			Children: []core.Node{
				body,
				core.Text{
					Content: caption,
					Style:   core.Style{FontSize: 12.5, Color: textMuted},
				},
			},
		}
	}

	// 1. Spec "Focus ring" gallery (button, icon button, switch, tree row, field, composer card, region via F6).
	gallery := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(editor.FocusTheme())
		return core.View{
			TestID: "focus-ring-spec-gallery",
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: bg,
				Padding:    core.All(32),
				Gap:        20,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
					Children: []core.Node{
						core.Text{Content: "Focus ring", Style: core.Style{FontSize: 22, FontWeight: 600, Color: textStrong}},
						core.Text{Content: "2 px accent ring (#3B7BFF), +2 px offset on controls, -2 px inset on rows, fields & regions", Style: core.Style{FontSize: 13, Color: textMuted}},
					},
				},
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 16},
					Children: []core.Node{
						card("Button · offset 2", core.View{
							Style: core.Style{
								Direction:      core.Row,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
								Height:         core.Pt(32),
								Padding:        core.Symmetric(0, 14),
								Gap:            8,
								CornerRadius:   8,
								Background:     textStrong,
							}.Merge(ringOut),
							Children: []core.Node{
								core.Icon{SVG: lucide.Play(), Style: core.Style{Width: core.Pt(12), Height: core.Pt(12), Color: bg}},
								core.Text{Content: "Play", Style: core.Style{FontSize: 12.5, FontWeight: 600, Color: bg}},
							},
						}),
						card("Icon button · offset 2", core.View{
							Style: core.Style{
								Width:          core.Pt(30),
								Height:         core.Pt(30),
								CornerRadius:   8,
								Background:     raised,
								BorderWidth:    1,
								BorderColor:    lineRaised,
								AlignItems:     core.AlignCenter,
								JustifyContent: core.JustifyCenter,
							}.Merge(ringOut),
							Children: []core.Node{
								core.Icon{SVG: lucide.RotateCcw(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14), Color: textPrimary}},
							},
						}),
						card("Switch · offset 2", core.View{
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Width:        core.Pt(32),
										Height:       core.Pt(18),
										CornerRadius: 9,
										Background:   accent,
										Padding:      core.All(2),
									}.Merge(ringOut),
									Children: []core.Node{
										core.View{
											Style: core.Style{
												Width:        core.Pt(14),
												Height:       core.Pt(14),
												CornerRadius: 7,
												Background:   textStrong,
												Transform:    core.Transform{TranslateX: 14},
											},
										},
									},
								},
								core.Text{Content: "Snap to grid", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
							},
						}),
					},
				},
				core.View{
					Style: core.Style{Direction: core.Row, Gap: 16},
					Children: []core.Node{
						card("Tree row · inset, offset -2", core.View{
							Style: core.Style{Width: core.Pct(100), Gap: 2},
							Children: []core.Node{
								core.View{
									Style: core.Style{
										Height:       core.Pt(28),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          8,
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
									},
									Children: []core.Node{
										core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 2, Background: accent}},
										core.Text{Content: "Player", Style: core.Style{FontSize: 12.5, Color: textSec}},
									},
								},
								core.View{
									Style: core.Style{
										Height:       core.Pt(28),
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          8,
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
										Background:   selectedFill,
									}.Merge(ringIn),
									Children: []core.Node{
										core.View{Style: core.Style{Width: core.Pt(7), Height: core.Pt(7), CornerRadius: 2, Background: changed}},
										core.Text{Content: "Knight", Style: core.Style{FontSize: 12.5, Color: textStrong}},
									},
								},
							},
						}),
						card("Field · inset, offset -2", core.View{
							Style: core.Style{
								Width:          core.Pct(100),
								Height:         core.Pt(32),
								CornerRadius:   6,
								Background:     raised,
								BorderWidth:    1,
								BorderColor:    lineRaised,
								Padding:        core.Symmetric(0, 10),
								JustifyContent: core.JustifyCenter,
							}.Merge(ringIn),
							Children: []core.Node{
								core.Text{Content: "Knight", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
							},
						}),
						card("Composer · card, focus-within", core.View{
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(40),
								CornerRadius: 12,
								Background:   raised,
								BorderWidth:  1,
								BorderColor:  lineRaised,
								Direction:    core.Row,
								AlignItems:   core.AlignCenter,
								Padding:      core.Symmetric(0, 12),
							}.Merge(ringOut),
							Children: []core.Node{
								core.Text{Content: "Make the slimes slower", Style: core.Style{FontSize: 12.5, Color: textPrimary}},
								core.View{Style: core.Style{Width: core.Pt(1.5), Height: core.Pt(16), Background: textStrong, Margin: core.Edges{Left: 2}}},
							},
						}),
						card("Region via F6 · inset, offset -2", core.View{
							Style: core.Style{
								Width:        core.Pct(100),
								Height:       core.Pt(64),
								CornerRadius: 4,
								Background:   core.RGB(0x15181D),
								Padding:      core.All(10),
							}.Merge(ringIn),
							Children: []core.Node{
								core.Text{Content: "Viewport", Style: core.Style{FontSize: 11, Color: textMuted}},
							},
						}),
					},
				},
			},
		}
	}, nsgotest.Size(1280, 440))
	gallery.Settle()
	gallery.Screenshot("focus-ring-gallery")

	// 2. Live EditorView under F6 region cycling (Header and Viewport regions).
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#15181D",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "player", Name: "Player", X: 180, Y: 220},
					{ID: "knight", Name: "Knight", X: 340, Y: 220},
					{ID: "slime", Name: "Slime", X: 460, Y: 240},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	t.Cleanup(func() { _ = ed.Close() })
	_ = ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1440, 900))
	app.Settle()

	app.Post(func() { ed.CycleRegion(+1) }) // Header
	app.Settle()
	app.Screenshot("editor-f6-header")

	app.Post(func() {
		ed.CycleRegion(+1) // Scene tree
		ed.CycleRegion(+1) // Viewport
		ed.SelectNextObject()
	})
	app.Settle()
	app.Screenshot("editor-f6-viewport")
}
