package editor

import (
	"errors"
	"path"
	"strings"

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

// unsavedPrompt is a question the unsaved-changes prompt is asking: proceed runs once the edits are saved or discarded, and cancel when the author keeps them.
type unsavedPrompt struct {
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

// openSceneAsked opens the project's scene at rel, asking first while edits are unsaved; an author who chose Discard loses the project's unsaved edits too.
func (e *Editor) openSceneAsked(rel string) {
	e.whenSaved(func() {
		if err := e.OpenScene(rel); err != nil {
			e.report(err)
			return
		}
		e.discardProject()
		e.notify()
	}, nil)
}

// newSceneAsked makes a new scene at rel, asking first while edits are unsaved, as openSceneAsked does.
func (e *Editor) newSceneAsked(rel string) {
	e.whenSaved(func() {
		if _, err := e.NewScene(rel); err != nil {
			e.report(err)
			return
		}
		e.discardProject()
		e.notify()
	}, nil)
}

// openFromSearchAsked runs open, a Search or Find References result's navigation to file, asking first while edits are unsaved when it would open a scene other than the open one (object marks a result inside a scene), as the file browser and Problems panel do; Discard drops the project's unsaved edits too.
func (e *Editor) openFromSearchAsked(file string, object bool, open func() error) {
	if e.Project == nil || file == "" || e.isOpenScene(file) || (!object && !e.isKnownSceneFile(file)) {
		e.report(open())
		return
	}
	e.whenSaved(func() {
		if err := open(); err != nil {
			e.report(err)
			return
		}
		e.discardProject()
		e.notify()
	}, nil)
}

// saveFromUI saves as the toolbar's Save button does, putting an error in the status bar, and reports whether it saved.
func (e *Editor) saveFromUI() bool {
	if err := e.Save(); err != nil {
		e.Status = "Save error: " + err.Error()
		e.notify()
		return false
	}
	return true
}

// unsavedState is what has unsaved edits, the scene (SceneDirty) and the project (ProjectDirty), as EditorView works it out once per revision for the toolbar's marker, its Revert item and the window title.
type unsavedState struct{ scene, project bool }

// either reports whether the scene or the project has unsaved edits (HasUnsavedChanges).
func (s unsavedState) either() bool { return s.scene || s.project }

// unsavedMemo works out unsavedState once per revision of ed. Both checks encode a whole file, so while Play runs, which makes a revision every frame, it keeps what it worked out at the first revision of Play: what is compared then is the scene as it was before Play, and the project cannot be edited.
func unsavedMemo(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) *core.Memo[unsavedState] {
	var last unsavedState
	computed, lastPlaying := false, false
	return core.NewMemo(cx, func() unsavedState {
		_ = rev.Get()
		playing := ed.Playing()
		if !computed || !playing || !lastPlaying {
			last = unsavedState{scene: ed.SceneDirty(), project: ed.ProjectDirty()}
			computed = true
		}
		lastPlaying = playing
		return last
	})
}

// sceneFileView binds EditorView's scene file handling (Mod+S is the Save command's shortcut, CmdSave): a user's request to quit or to close the window asks first while edits are unsaved, the window title follows the open scene and its unsaved marker, and it returns the unsaved-changes prompt and the path dialog of New Scene and Save As.
func sceneFileView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dirty *core.Memo[unsavedState], mode *core.Signal[pathMode], target *core.Signal[string]) []core.Node {
	prompt := core.NewSignal[*unsavedPrompt](cx, nil)
	ed.askUnsaved = func(proceed, cancel func()) {
		// A question still open is answered Cancel before the new one replaces it.
		if p := prompt.Get(); p != nil {
			p.cancel()
		}
		prompt.Set(&unsavedPrompt{proceed: proceed, cancel: cancel})
	}
	cx.Cleanup(func() { ed.askUnsaved = nil })

	// The title is a memo, so the window is retitled only when the title changes, not on every revision, which Play makes on every frame.
	title := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.windowTitle(dirty.Get().either())
	})
	cx.Effect(func() { desktop.SetTitle(cx, title.Get()) })
	// On macOS the window's close button closes the window, which ends the app as its last window goes, without asking OnQuitRequest, which Windows and Linux ask then too (core.Window.requestClose in NSGo v0.3.0). Asking here covers every platform; once the edits are saved or discarded the window closes without asking again.
	if w := desktop.CurrentWindow(cx); w != nil {
		w.OnClosing(cx, func(c *desktop.WindowClosing) {
			if !ed.HasUnsavedChanges() {
				return
			}
			c.Cancel()
			// The window closes after the prompt's handler returns, not from inside the window's own event.
			ed.whenSaved(func() { cx.Post(w.Close) }, nil)
		})
	}
	desktop.OnQuitRequest(cx, func(q *desktop.QuitRequest) {
		if q.Forced() || !ed.HasUnsavedChanges() {
			return
		}
		answer := q.Defer()
		ed.whenSaved(func() { answer(true) }, func() { answer(false) })
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
			ed.newSceneAsked(p)
		case pathSaveAs:
			if _, err := ed.SaveSceneAs(p); err != nil {
				ed.report(err)
			}
		}
	}
	return []core.Node{
		ui.AlertDialog{
			TestID: "unsaved-dialog",
			Open:   func() bool { return prompt.Get() != nil },
			Title:  "Save changes?",
			DescriptionFunc: func() string {
				_ = rev.Get()
				return "The scene " + ed.SceneLabel() + " or the project has unsaved changes."
			},
			Actions: []ui.AlertAction{
				{TestID: "unsaved-cancel", Text: "Cancel", Variant: ui.ButtonSecondary, Role: core.AlertCancel, OnPress: answer(func(p *unsavedPrompt) { p.cancel() })},
				{TestID: "unsaved-discard", Text: "Discard", Variant: ui.ButtonSecondary, Role: core.AlertDestructive, OnPress: answer(func(p *unsavedPrompt) { p.proceed() })},
				{TestID: "unsaved-save", Text: "Save", Variant: ui.ButtonPrimary, Default: true, OnPress: answer(func(p *unsavedPrompt) {
					// Save refuses while playing, and what has unsaved edits is the scene as it was before Play, which Stop puts back, so a quit asked during Play stops it to save.
					if err := ed.Stop(); err != nil {
						ed.report(err)
						p.cancel()
						return
					}
					if ed.saveFromUI() {
						p.proceed()
					} else {
						p.cancel()
					}
				})},
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
			OnConfirm: confirm,
			TitleFunc: func() string {
				if mode.Get() == pathSaveAs {
					return "Save Scene As"
				}
				return "New Scene"
			},
			Description: "A path from the project's root. Without a .yaml extension, .scene.yaml is added.",
			Children: []core.Node{ui.Input{
				TestID:    "scene-path-input",
				Label:     "Scene path",
				AutoFocus: true,
				Value:     target.Get,
				OnChange:  target.Set,
				OnSubmit:  func(string) { confirm() },
			}},
			Footer: []core.Node{
				ui.Button{
					TestID:  "scene-path-cancel",
					Text:    "Cancel",
					Variant: ui.ButtonSecondary,
					OnPress: func() { mode.Set(pathNone) },
				},
				ui.Button{
					TestID: "scene-path-confirm",
					TextFunc: func() string {
						if mode.Get() == pathSaveAs {
							return "Save"
						}
						return "Create"
					},
					Variant:  ui.ButtonPrimary,
					Disabled: func() bool { return strings.TrimSpace(target.Get()) == "" },
					OnPress:  confirm,
				},
			},
		},
	}
}

// sceneFileButtons are the toolbar's scene file controls: the open scene's path, its unsaved marker, and the File menu with New Scene, Save As and Revert.
func sceneFileButtons(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dirty *core.Memo[unsavedState], mode *core.Signal[pathMode], target *core.Signal[string]) []core.Node {
	colors := ui.ColorsOf(cx)
	noProject := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil || ed.ProjectPath == ""
	}
	return []core.Node{
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
		core.Show{
			When: func() bool { return dirty.Get().either() },
			Then: func(cx *core.Scope) core.Node {
				return core.Text{
					TestID:  "toolbar-unsaved",
					Content: "*",
					Label:   "Unsaved changes",
					Style:   core.Style{FontSize: 14, FontWeight: 700, Color: colors.Accent()},
				}
			},
		},
		ui.Menu{
			TestID:  "toolbar-file",
			Label:   "Scene file",
			Trigger: ui.Button{Text: "File", Size: ui.SizeSmall, Variant: ui.ButtonSecondary},
			Children: []core.Node{
				ui.MenuItem{
					TestID:   "toolbar-new",
					Text:     "New Scene…",
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
					Text:     "Save As…",
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
					Text:   "Revert",
					Disabled: func() bool {
						_ = rev.Get()
						return ed.Playing() || ed.FilePath == "" || !dirty.Get().scene
					},
					OnSelect: func() { ed.report(ed.RevertScene()) },
				},
			},
		},
	}
}
