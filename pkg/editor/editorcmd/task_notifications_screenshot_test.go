package editorcmd_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/nsgo-dev/nsgo/ui"
)

// TestTaskNotificationsAndPlayDotScreenshots captures real macOS native screenshots
// (via `go tool nsgo test -target macos -run TestTaskNotificationsAndPlayDotScreenshots ./editor/editorcmd`)
// showing:
//  1. Live EditorView in Play mode with the corner dot hovered ("1 task needs you · stop Play to open")
//     and upgraded to red on failure ("1 task failed · stop Play to open").
//  2. The Spec-token Notifications, Dock Badge (4), and During Play gallery matching
//     docs/design/editor/Tasks.dc.html.
func TestTaskNotificationsAndPlayDotScreenshots(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "level_01",
		VirtualWidth:    640,
		VirtualHeight:   360,
		BackgroundColor: "#181C24",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Objects: []sceneformat.ObjectDoc{
					{ID: "player", Name: "Player", X: 140, Y: 230},
					{ID: "slime_02", Name: "slime_02", X: 360, Y: 240},
					{ID: "door", Name: "Door", X: 540, Y: 224},
				},
			},
		},
	}
	ed, err := editor.New(doc, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.AppName = "NSGo Editor"
	ed.Project = &project.File{
		Version:    project.CurrentVersion,
		Name:       "dungeon-run",
		ID:         "dev.nsgo.dungeonrun",
		AppVersion: "0.1.0",
		Window:     project.Window{Width: 640, Height: 360},
	}
	ed.SetAppearance(editor.AppearanceDark)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 760), nsgotest.Environment(core.Environment{
		OS:        core.OSMacOS,
		Idiom:     core.IdiomDesktop,
		Dark:      true,
		Locale:    "en-US",
		Lifecycle: core.LifecycleActive,
	}))

	// 1. Live EditorView in Play mode with 1 waiting task -> amber corner dot + hover label pill.
	var playErr error
	app.Post(func() {
		playErr = ed.Play()
		if playErr != nil {
			return
		}
		_, _ = ed.SubmitTask(editor.TaskRequest{
			ID:               "t3",
			Title:            "Torches around the boss arena",
			PermissionPrompt: "Allow the agent to run the game?",
			PermissionDetail: "wants to play boss_room headless for 10 s.",
			InitialState:     editor.TaskNeedsYou,
			ScenePath:        "scenes/boss_room.scene.yaml",
		})
		_, _ = ed.SubmitTask(editor.TaskRequest{
			ID:           "t4",
			Title:        "Nightly: fix game check warnings",
			Source:       editor.TaskSourceAutomation,
			InitialState: editor.TaskReadyToReview,
			ScenePath:    "scenes/level_02.scene.yaml",
			Review:       &editor.TaskReview{Summary: "3 changes in level_02.scene.yaml."},
		})
	})
	app.Settle()
	if playErr != nil {
		t.Fatalf("Play: %v", playErr)
	}
	app.Hover("id:play-task-dot")
	app.Settle()
	app.Screenshot("task-notifications/play-dot-waiting-hover")

	// 2. Live EditorView in Play mode with a failed task -> most-urgent red corner dot + hover label pill.
	app.Post(func() {
		_, _ = ed.SubmitTask(editor.TaskRequest{
			ID:           "t7",
			Title:        "Bake normals for the cave tiles",
			Error:        "the asset tool exited with code 1 at step 2 of 3. Nothing was changed.",
			InitialState: editor.TaskFailed,
		})
	})
	app.Settle()
	app.Screenshot("task-notifications/play-dot-failed-hover")

	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()

	// 3. Spec-token gallery beside docs/design/editor/Tasks.dc.html:
	//    left column = 3 system notifications, right column = Dock badge (4) + During Play corner dot.
	nWait, _ := ed.BuildAgentTaskNotification(editor.AgentTask{
		ID:               "t3",
		Title:            "Torches around the boss arena",
		PermissionPrompt: "Allow the agent to run the game?",
		PermissionDetail: "wants to play boss_room headless for 10 s.",
		State:            editor.TaskNeedsYou,
	})
	nReview, _ := ed.BuildAgentTaskNotification(editor.AgentTask{
		ID:     "t4",
		Title:  "Nightly: fix game check warnings",
		Review: &editor.TaskReview{Summary: "3 changes in level_02.scene.yaml."},
		State:  editor.TaskReadyToReview,
	})
	nFail, _ := ed.BuildAgentTaskNotification(editor.AgentTask{
		ID:    "t7",
		Title: "Bake normals for the cave tiles",
		Error: "the asset tool exited with code 1 at step 2 of 3. Nothing was changed.",
		State: editor.TaskFailed,
	})

	gallery := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(editor.SpecTheme())
		p := editor.DarkSpecPalette()
		sectionLabel := func(text string) core.Node {
			return core.Text{
				Content: text,
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
					Color:         p.TextFaint,
				},
			}
		}
		return core.View{
			TestID: "task-notifications-spec-gallery",
			Style: core.Style{
				Width:      core.Pct(100),
				Height:     core.Pct(100),
				Background: p.Bg,
				Padding:    core.All(32),
				Gap:        24,
			},
			Children: []core.Node{
				core.View{
					Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
					Children: []core.Node{
						core.Text{Content: "Background tasks · notifications, Dock badge & Play dot", Style: core.Style{FontSize: 20, FontWeight: 600, Color: p.TextPrimary}},
						core.Text{Content: "Only three events reach you outside the editor.", Style: core.Style{FontSize: 12.5, Color: p.TextMuted}},
					},
				},
				core.View{
					Style: core.Style{
						Direction: core.Row,
						Gap:       32,
						FlexGrow:  1,
					},
					Children: []core.Node{
						// Column 1: System notifications (only these three).
						core.View{
							Style: core.Style{Width: core.Pt(420), Gap: 12},
							Children: []core.Node{
								sectionLabel("SYSTEM NOTIFICATIONS · ONLY THESE THREE"),
								editor.AgentTaskNotificationCard(p, nWait, "now"),
								editor.AgentTaskNotificationCard(p, nReview, "2 min ago"),
								editor.AgentTaskNotificationCard(p, nFail, "01:30"),
								core.Text{
									Content: "Waiting for permission, ready to review, failed. Nothing else notifies: finished steps, queued starts and agent replies stay in the panel. A click opens the task in its scene.",
									Style:   core.Style{FontSize: 12, Color: p.TextFaint},
								},
							},
						},
						// Column 2: Dock badge + During Play dot.
						core.View{
							Style: core.Style{Width: core.Pt(420), Gap: 16},
							Children: []core.Node{
								sectionLabel("DOCK BADGE"),
								core.View{
									Style: core.Style{
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          24,
										Height:       core.Pt(112),
										Padding:      core.Symmetric(0, 24),
										CornerRadius: 12,
										Background:   p.Panel,
										BorderWidth:  1,
										BorderColor:  p.Line,
									},
									Children: []core.Node{
										core.View{
											Style: core.Style{
												Position:       core.Relative,
												Width:          core.Pt(64),
												Height:         core.Pt(64),
												CornerRadius:   16,
												Background:     p.Accent.Fg,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
											},
											Children: []core.Node{
												core.Canvas{
													Style: core.Style{Width: core.Pt(30), Height: core.Pt(30)},
													Draw: func(d *core.Drawing, _ core.Size) {
														d.SetStrokeColor(p.Accent.OnFill).SetLineWidth(2.6).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
														d.BeginPath().MoveTo(6, 24).LineTo(15, 6).LineTo(24, 24).Stroke()
													},
												},
												core.View{
													Style: core.Style{
														Position:       core.Absolute,
														Right:          core.Pt(-8),
														Top:            core.Pt(-8),
														MinWidth:       core.Pt(24),
														Height:         core.Pt(24),
														Padding:        core.Symmetric(0, 6),
														CornerRadius:   12,
														Background:     p.Removed.Fg,
														BorderWidth:    2,
														BorderColor:    p.Panel,
														AlignItems:     core.AlignCenter,
														JustifyContent: core.JustifyCenter,
													},
													Children: []core.Node{
														core.Text{
															Content: "4",
															Style: core.Style{
																FontSize:   12.5,
																FontWeight: 600,
																Color:      p.Removed.OnFill,
															},
														},
													},
												},
											},
										},
										core.Text{
											Content: "1 waiting for permission, 2 ready to review, 1 failed. Running and queued tasks never add to the badge.",
											Style: core.Style{
												FlexGrow:  1,
												FlexBasis: core.Pt(0),
												FontSize:  12.5,
												Color:     p.TextMuted,
											},
										},
									},
								},
								sectionLabel("DURING PLAY"),
								core.View{
									Style: core.Style{
										Position:     core.Relative,
										Height:       core.Pt(214),
										CornerRadius: 12,
										Background:   p.Viewport,
										BorderWidth:  1,
										BorderColor:  p.LineRaised,
										Overflow:     core.Hidden,
									},
									Children: []core.Node{
										// Minimal game floor & actors matching Tasks.dc.html.
										core.View{
											Style: core.Style{
												Position:    core.Absolute,
												Left:        core.Pt(0),
												Right:       core.Pt(0),
												Bottom:      core.Pt(0),
												Height:      core.Pt(40),
												Background:  p.Raised,
												Borders:     core.Edges{Top: 3},
												BorderColor: p.LineRaised,
											},
										},
										core.View{
											Style: core.Style{
												Position:     core.Absolute,
												Left:         core.Pt(70),
												Bottom:       core.Pt(40),
												Width:        core.Pt(16),
												Height:       core.Pt(28),
												CornerRadius: 4,
												Background:   p.Accent.Fg,
											},
										},
										core.View{
											Style: core.Style{
												Position:     core.Absolute,
												Left:         core.Pt(180),
												Bottom:       core.Pt(40),
												Width:        core.Pt(20),
												Height:       core.Pt(13),
												CornerRadius: 6,
												Background:   p.Added.Fg,
											},
										},
										// Hover pill + corner dot (exact Spec tokens).
										core.View{
											Style: core.Style{
												Position:       core.Absolute,
												Right:          core.Pt(28),
												Top:            core.Pt(6),
												Height:         core.Pt(22),
												Padding:        core.Symmetric(0, 8),
												CornerRadius:   6,
												Background:     p.PillBg,
												BorderWidth:    1,
												BorderColor:    p.LineRaised,
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
											},
											Children: []core.Node{
												core.Text{
													Content: "1 task needs you · stop Play to open",
													Style:   core.Style{FontSize: 11, Color: p.TextSecondary},
												},
											},
										},
										core.View{
											Style: core.Style{
												Position:       core.Absolute,
												Right:          core.Pt(9),
												Top:            core.Pt(9),
												Width:          core.Pt(14),
												Height:         core.Pt(14),
												CornerRadius:   7,
												Background:     ui.Alpha(p.Warning.Fg, 0.25),
												AlignItems:     core.AlignCenter,
												JustifyContent: core.JustifyCenter,
											},
											Children: []core.Node{
												core.View{
													Style: core.Style{
														Width:        core.Pt(8),
														Height:       core.Pt(8),
														CornerRadius: 4,
														Background:   p.Warning.Fg,
													},
												},
											},
										},
									},
								},
								core.Text{
									Content: "Nothing pops over a running game. One dot in the corner, in the color of the most urgent state; the label shows only on hover. System notifications wait until Play stops.",
									Style:   core.Style{FontSize: 12, Color: p.TextFaint},
								},
							},
						},
					},
				},
			},
		}
	}, nsgotest.Size(960, 640))
	gallery.Settle()
	gallery.Screenshot("task-notifications/spec-gallery")
}
