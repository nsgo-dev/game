package editor

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"
)

// errUnsaved is reported when something would lose unsaved edits and no EditorView is there to ask the author first.
var errUnsaved = errors.New("editor: save or revert the unsaved changes first")

// pathMode is what the scene path dialog asks a path for.
type pathMode int

const (
	pathNone pathMode = iota
	pathNewScene
	pathSaveAs
)

// unsavedPrompt is a question the unsaved-changes prompt is asking: proceed runs once the edits are saved or discarded, and cancel when the author keeps them. When description or save are set, they override the default prompt message and SaveAll handler (for closing one dirty tab without touching other tabs or the shared project).
type unsavedPrompt struct {
	description     string
	save            func() bool
	proceed, cancel func()
}

// whenSaved runs proceed at once while nothing is unsaved, and else asks the author, through the prompt EditorView shows, to save, discard or cancel: proceed runs after a save that succeeds and after Discard, cancel after Cancel and after a save that fails. Without an EditorView it reports errUnsaved and runs cancel. cancel may be nil.
func (e *Editor) whenSaved(proceed, cancel func()) {
	if cancel == nil {
		cancel = func() {}
	}
	switch {
	case !e.HasUnsavedChanges():
		proceed()
	case e.askUnsaved != nil:
		e.askUnsaved(proceed, cancel)
	default:
		e.report(errUnsaved)
		cancel()
	}
}

// saveFromUI saves as the toolbar's Save button does, putting an error in the status bar
// and showing a persistent Spec error toast with Retry on explicit save failure, and reports whether it saved.
func (e *Editor) saveFromUI() bool {
	if err := e.Save(); err != nil {
		e.Status = "Save error: " + err.Error()
		e.notifyExplicitSaveFailure(e.SceneLabel(), err, e.Save)
		e.notify()
		return false
	}
	e.clearExplicitSaveFailedToast()
	return true
}

// saveTabByIDFromUI saves the scene in tabID, putting an error in the status bar
// and showing a persistent Spec error toast with Retry on explicit save failure, and reports whether it saved.
func (e *Editor) saveTabByIDFromUI(tabID uint64) bool {
	idx := e.findTabByID(tabID)
	if idx < 0 {
		return true
	}
	if err := e.SaveTab(idx); err != nil {
		e.Status = "Save error: " + err.Error()
		label := e.SceneLabel()
		if idx < len(e.tabs) && e.tabs[idx] != nil {
			if p := tabFullPath(e.tabs[idx]); p != "" {
				label = p
			}
		}
		e.notifyExplicitSaveFailure(label, err, func() error {
			i := e.findTabByID(tabID)
			if i < 0 {
				return nil
			}
			return e.SaveTab(i)
		})
		e.notify()
		return false
	}
	e.clearExplicitSaveFailedToast()
	return true
}

// saveAllFromUI saves every dirty editor (project, all dirty scene tabs, GridMap, localizations, clip, data table),
// putting an error in the status bar and showing a persistent Spec error toast with Retry on explicit save failure,
// and reports whether it saved.
func (e *Editor) saveAllFromUI() bool {
	if err := e.SaveAll(); err != nil {
		e.Status = "Save error: " + err.Error()
		e.notifyExplicitSaveFailure(e.SceneLabel(), err, e.SaveAll)
		e.notify()
		return false
	}
	e.clearExplicitSaveFailedToast()
	return true
}

// unsavedState is what has unsaved edits: the active scene (SceneDirty), any open scene tab (AnySceneDirty), the project (ProjectDirty), and auxiliary editors (GridMap, terrain, room map, clip, data table, localizations, skeletal rig), as EditorView works it out once per revision for the toolbar's marker, its Revert item and the window title.
type unsavedState struct{ scene, anyScene, project, other bool }

// either reports whether anything in the session has unsaved edits (HasUnsavedChanges).
func (s unsavedState) either() bool { return s.anyScene || s.project || s.other }

// unsavedMemo works out unsavedState once per revision of ed. The checks encode documents, so while Play runs (or on pure rig-preview playback ticks), which make a revision every frame, it keeps what it already computed rather than re-encoding every document 60 times a second.
func unsavedMemo(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) *core.Memo[unsavedState] {
	var last unsavedState
	computed, lastPlaying := false, false
	return core.NewMemo(cx, func() unsavedState {
		r := rev.Get()
		playing := ed.Playing()
		rigStepOnly := ed.RigPlaying() && r == ed.rigStepRev
		if !computed || (!playing && !rigStepOnly) || (playing && !lastPlaying) {
			sceneDirty := ed.SceneDirty()
			anySceneDirty := sceneDirty || ed.AnySceneDirty()
			last = unsavedState{
				scene:    sceneDirty,
				anyScene: anySceneDirty,
				project:  ed.ProjectDirty(),
				other:    ed.GridMapDirty() || ed.TerrainDirty() || ed.RoomsDirty() || ed.TilesetDirty() || ed.LocalizationDirty() || ed.ClipDirty() || ed.CutsceneDirty() || ed.DataTableDirty() || ed.RigDirty() || ed.MaterialDirty() || ed.hasDirtyKeptStagedFiles(),
			}
			computed = true
		}
		lastPlaying = playing
		return last
	})
}

// bindSceneFileHooks wires the editor's unsaved prompt callback, window title, window close guard, and quit request guard at the root EditorView scope.
func bindSceneFileHooks(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dirty *core.Memo[unsavedState], prompt *core.Signal[*unsavedPrompt]) {
	setPrompt := func(next *unsavedPrompt) {
		// A question still open is answered Cancel before the new one replaces it.
		if ed.UnsavedPromptOpen() {
			ed.CancelPendingSwitch()
		}
		if p := prompt.Get(); p != nil {
			p.cancel()
		}
		prompt.Set(next)
	}
	ed.askUnsaved = func(proceed, cancel func()) {
		setPrompt(&unsavedPrompt{proceed: proceed, cancel: cancel})
	}
	ed.askCloseTab = func(tabID uint64, label string) {
		setPrompt(&unsavedPrompt{
			description: "The scene " + label + " has unsaved changes.",
			save: func() bool {
				return ed.saveTabByIDFromUI(tabID)
			},
			proceed: func() {
				if idx := ed.findTabByID(tabID); idx >= 0 {
					ed.report(ed.CloseTab(idx))
				}
			},
			cancel: func() {},
		})
	}
	cx.Effect(func() {
		ed.setUnsavedPromptOpen(prompt.Get() != nil)
	})
	cx.Cleanup(func() {
		ed.askUnsaved = nil
		ed.askCloseTab = nil
		ed.setUnsavedPromptOpen(false)
	})

	// The title is a memo, so the window is retitled only when the title changes, not on every revision, which Play makes on every frame.
	title := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.windowTitle(dirty.Get().either())
	})
	cx.Effect(func() { desktop.SetTitle(cx, title.Get()) })
	// On macOS the window's close button closes the window, which ends the app as its last window goes, without asking OnQuitRequest, which Windows and Linux ask then too (core.Window.requestClose in NSGo v0.3.0). Asking here covers every platform; once the edits are saved or discarded the window closes without asking again.
	if w := desktop.CurrentWindow(cx); w != nil {
		w.OnClosing(cx, func(c *desktop.WindowClosing) {
			// The view is kept whether or not the edits are, so the scene opens where it was left next time.
			ed.saveViewStateQuietly()
			if ed.HasPendingAgentTurn() {
				c.Cancel()
				ed.askPendingTurn("Close window", func() {
					if !ed.HasUnsavedChanges() {
						cx.Post(w.Close)
						return
					}
					ed.whenSaved(func() { cx.Post(w.Close) }, nil)
				}, nil)
				return
			}
			if !ed.HasUnsavedChanges() {
				return
			}
			c.Cancel()
			// The window closes after the prompt's handler returns, not from inside the window's own event.
			ed.whenSaved(func() { cx.Post(w.Close) }, nil)
		})
	}
	desktop.OnQuitRequest(cx, func(q *desktop.QuitRequest) {
		ed.saveViewStateQuietly()
		if q.Forced() {
			return
		}
		if ed.HasPendingAgentTurn() {
			answer := q.Defer()
			ed.askPendingTurn("Quit editor", func() {
				if !ed.HasUnsavedChanges() {
					answer(true)
					return
				}
				ed.whenSaved(func() { answer(true) }, func() { answer(false) })
			}, func() { answer(false) })
			return
		}
		if !ed.HasUnsavedChanges() {
			return
		}
		answer := q.Defer()
		ed.whenSaved(func() { answer(true) }, func() { answer(false) })
	})
}

// sceneFileDialogs returns the localized unsaved-changes prompt and the path dialog of New Scene and Save As.
func sceneFileDialogs(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], prompt *core.Signal[*unsavedPrompt], mode *core.Signal[pathMode], target *core.Signal[string]) []core.Node {
	onUnsavedClose := bindDialogFocusRestore(cx, ed, func() bool {
		_ = rev.Get()
		return prompt.Get() != nil || ed.UnsavedPromptOpen()
	})
	onPathClose := bindDialogFocusRestore(cx, ed, func() bool {
		return mode.Get() != pathNone
	})
	// answer takes the open question off the prompt and hands it to fn.
	answer := func(fn func(p *unsavedPrompt)) func() {
		return func() {
			p := prompt.Get()
			if p == nil {
				return
			}
			prompt.Set(nil)
			fn(p)
		}
	}
	confirm := func() {
		m, p := mode.Get(), strings.TrimSpace(target.Get())
		if p == "" {
			return
		}
		mode.Set(pathNone)
		switch m {
		case pathNewScene:
			if _, err := ed.NewScene(p); err != nil {
				ed.report(err)
			}
		case pathSaveAs:
			if _, err := ed.SaveSceneAs(p); err != nil {
				ed.report(err)
			}
		}
	}
	return []core.Node{
		ui.AlertDialog{
			TestID: "unsaved-dialog",
			Open: func() bool {
				_ = rev.Get()
				return prompt.Get() != nil || ed.UnsavedPromptOpen()
			},
			OnOpenChange: func(open bool, _ base.Reason) {
				if open {
					return
				}
				if ed.UnsavedPromptOpen() {
					ed.CancelPendingSwitch()
				}
				if p := prompt.Get(); p != nil {
					prompt.Set(nil)
					p.cancel()
				}
			},
			OnClose: onUnsavedClose,
			Title:   ed.T("dialog.unsavedTitle"),
			DescriptionFunc: func() string {
				_ = rev.Get()
				if pending, ok := ed.PendingProjectAction(); ok && pending.Summary != "" {
					return fmt.Sprintf("You have unsaved edits. Save them before proceeding (%s)?", pending.Summary)
				}
				if p := prompt.Get(); p != nil && p.description != "" {
					return p.description
				}
				return ed.T("dialog.unsavedDesc", l10n.Param("scene", ed.SceneLabel()))
			},
			Actions: []ui.AlertAction{
				{
					TestID:  "unsaved-cancel",
					Text:    ed.T("dialog.cancel"),
					Variant: ui.ButtonSecondary,
					Role:    core.AlertCancel,
					OnPress: func() {
						if ed.UnsavedPromptOpen() {
							ed.CancelPendingSwitch()
							return
						}
						answer(func(p *unsavedPrompt) { p.cancel() })()
					},
				},
				{
					TestID:  "unsaved-discard",
					Text:    ed.T("dialog.discard"),
					Variant: ui.ButtonSecondary,
					Role:    core.AlertDestructive,
					OnPress: func() {
						if ed.UnsavedPromptOpen() {
							ed.report(ed.ConfirmDiscardAndSwitch())
							return
						}
						answer(func(p *unsavedPrompt) { p.proceed() })()
					},
				},
				{
					TestID:  "unsaved-save",
					Text:    ed.T("dialog.save"),
					Variant: ui.ButtonPrimary,
					Default: true,
					OnPress: func() {
						if ed.UnsavedPromptOpen() {
							if ed.Playing() {
								if err := ed.Stop(); err != nil {
									ed.report(err)
									ed.CancelPendingSwitch()
									return
								}
							}
							ed.report(ed.ConfirmSaveAndSwitch())
							return
						}
						answer(func(p *unsavedPrompt) {
							// Save refuses while playing, and what has unsaved edits is the scene as it was before Play, which Stop puts back, so a quit asked during Play stops it to save.
							if err := ed.Stop(); err != nil {
								ed.report(err)
								p.cancel()
								return
							}
							saveFn := p.save
							if saveFn == nil {
								saveFn = ed.saveAllFromUI
							}
							if saveFn() {
								p.proceed()
							} else {
								p.cancel()
							}
						})()
					},
				},
			},
		},
		ui.Dialog{
			TestID: "scene-path-dialog",
			Open:   func() bool { return mode.Get() != pathNone },
			OnOpenChange: func(open bool, _ base.Reason) {
				if !open {
					mode.Set(pathNone)
				}
			},
			OnClose:   onPathClose,
			OnConfirm: confirm,
			TitleFunc: func() string {
				if mode.Get() == pathSaveAs {
					return ed.T("dialog.saveAs")
				}
				return ed.T("dialog.newScene")
			},
			Description: ed.T("dialog.scenePathDesc"),
			Children: []core.Node{specInputField(cx, ed, ui.Input{
				TestID:    "scene-path-input",
				Label:     ed.T("dialog.scenePathLabel"),
				AutoFocus: true,
				Value:     target.Get,
				OnChange:  target.Set,
				OnSubmit:  func(string) { confirm() },
			})},
			Footer: []core.Node{
				ui.Button{
					TestID:  "scene-path-cancel",
					Text:    ed.T("dialog.cancel"),
					Variant: ui.ButtonSecondary,
					OnPress: func() { mode.Set(pathNone) },
				},
				ui.Button{
					TestID: "scene-path-confirm",
					TextFunc: func() string {
						if mode.Get() == pathSaveAs {
							return ed.T("dialog.save")
						}
						return ed.T("dialog.create")
					},
					Variant:  ui.ButtonPrimary,
					Disabled: func() bool { return strings.TrimSpace(target.Get()) == "" },
					OnPress:  confirm,
				},
			},
		},
		saveConflictDialog(cx, ed, rev),
		pendingTurnDialog(cx, ed, rev),
	}
}

// sceneFileButtons are the toolbar's scene file controls: the open scene's path, its unsaved marker, and the File menu with New Scene, Save As and Revert.
func sceneFileButtons(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dirty *core.Memo[unsavedState], mode *core.Signal[pathMode], target *core.Signal[string]) []core.Node {
	colors := ui.ColorsOf(cx)
	fileBtnRef := &core.Ref{}
	if ed.focus.ctrl != nil {
		ed.focus.ctrl.registerCandidate(cx, RegionHeader, fileBtnRef, nil)
	}
	noProject := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil || ed.ProjectPath == ""
	}
	return []core.Node{
		core.View{
			Style: core.Style{MaxWidth: core.Pt(220)},
			Children: []core.Node{
				core.Text{
					TestID: "toolbar-scene-path",
					ContentFunc: func() string {
						_ = rev.Get()
						return ed.SceneLabel()
					},
					MaxLines:      1,
					EllipsizeMode: core.EllipsizeHead,
					Style:         core.Style{FontSize: 12, Color: colors.Muted(), MaxWidth: core.Pt(220)},
				},
			},
		},
		// Reserve a fixed slot for the unsaved dot so marking the scene dirty, clean, or save-failed
		// never widens the toolbar and wraps it onto an extra row mid-interaction.
		core.View{
			Style: core.Style{Width: core.Pt(10), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
			Children: []core.Node{
				core.Show{
					When: func() bool {
						_ = rev.Get()
						return dirty.Get().either() || ed.SaveFailed()
					},
					Then: func(cx *core.Scope) core.Node {
						return core.View{
							TestID: "toolbar-unsaved",
							LabelFunc: func() string {
								_ = rev.Get()
								if ed.SaveFailed() {
									return ed.T("toolbar.unsavedFailedLabel")
								}
								return ed.T("toolbar.unsavedLabel")
							},
							StyleFunc: func() core.Style {
								_ = rev.Get()
								p := ed.SpecPalette(cx)
								bg := p.Changed.Fg
								if ed.SaveFailed() {
									bg = p.Problem().Fg
								}
								return core.Style{
									Width:        core.Pt(6),
									Height:       core.Pt(6),
									CornerRadius: 3,
									Background:   bg,
								}
							},
						}
					},
				},
			},
		},
		ui.Menu{
			TestID:  "toolbar-file",
			Label:   ed.T("toolbar.fileMenuLabel"),
			Trigger: ui.Button{Ref: fileBtnRef, Text: ed.T("toolbar.file"), Size: ui.SizeSmall, Variant: ui.ButtonSecondary},
			Children: []core.Node{
				ui.MenuItem{
					TestID:   "toolbar-new",
					Text:     ed.T("toolbar.newScene"),
					Disabled: noProject,
					OnSelect: func() {
						dir := "scenes"
						if ed.ScenePath != "" {
							dir = path.Dir(ed.ScenePath)
						}
						target.Set(path.Join(dir, "new.scene.yaml"))
						mode.Set(pathNewScene)
					},
				},
				ui.MenuItem{
					TestID:   "toolbar-save-as",
					Text:     ed.T("toolbar.saveAs"),
					Disabled: noProject,
					OnSelect: func() {
						p := ed.ScenePath
						if p == "" {
							p = "scenes/untitled.scene.yaml"
						}
						target.Set(p)
						mode.Set(pathSaveAs)
					},
				},
				ui.MenuItem{
					TestID: "toolbar-revert",
					Text:   ed.T("toolbar.revert"),
					Disabled: func() bool {
						_ = rev.Get()
						return ed.Playing() || ed.FilePath == "" || !dirty.Get().scene
					},
					OnSelect: func() { ed.report(ed.RevertScene()) },
				},
				ui.MenuSeparator{},
				ui.MenuItem{
					TestID: "toolbar-open-instanced-scene",
					Text:   ed.T("menu.openInstancedScene"),
					Disabled: func() bool {
						return noProject() || !ed.selectedObjectInstancesScene()
					},
					OnSelect: func() { ed.report(ed.OpenInstancedScene(ed.SelectedID)) },
				},
				ui.MenuItem{
					TestID: "toolbar-open-base-scene",
					Text:   ed.T("menu.openBaseScene"),
					Disabled: func() bool {
						return noProject() || ed.Doc == nil || ed.Doc.Inherits == ""
					},
					OnSelect: func() { ed.report(ed.OpenBaseScene()) },
				},
				ui.MenuSeparator{},
				ui.MenuItem{
					TestID: "toolbar-close-tab",
					Text:   ed.T("toolbar.closeTab"),
					Disabled: func() bool {
						_ = rev.Get()
						return ed.Playing()
					},
					OnSelect: func() { ed.report(ed.RequestCloseTab(ed.ActiveTab())) },
				},
			},
		},
	}
}
