package editor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/tooling/library"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ErrProjectDirty is returned by InstallPack and RemovePack while the open project has edits that are not saved: installing writes game.yaml, so the edits must be saved or undone first.
var ErrProjectDirty = errors.New("editor: save the project before installing or removing a pack")

// InstallPack installs the fetched pack p into the open project, as library.PlanInstall describes: it writes the pack's files beside game.yaml, records the pack, its autoload entries and its plugins in the project, and saves the project (SaveProject). The registered plugins a plugin pack names turn on, and the status bar names those this editor was built without.
//
// Installing writes files the undo history cannot take back, so it empties the history, as OpenProject does; RemovePack undoes it. It refuses while playing (ErrPlaying), without a project (ErrNoProject) and while the project has unsaved edits (ErrProjectDirty), and on any error leaves the project and its files as they were.
func (e *Editor) InstallPack(p *library.Pack) error {
	dir, err := e.packProjectDir()
	if err != nil {
		return err
	}
	c, err := library.PlanInstall(os.DirFS(dir), e.Project, p, "")
	if err != nil {
		return err
	}
	verb := "Installed"
	if before := e.Project.FindPack(c.Pack.Name); before != nil {
		verb = "Updated"
	}
	return e.applyPackChange(dir, c, verb+" pack "+c.Pack.Name+" "+c.Pack.PackVersion+" into "+c.Pack.Dir)
}

// RemovePack removes the installed pack called name from the open project, as library.PlanRemove describes, and saves the project. It empties the undo history and refuses as InstallPack does.
func (e *Editor) RemovePack(name string) error {
	dir, err := e.packProjectDir()
	if err != nil {
		return err
	}
	c, err := library.PlanRemove(os.DirFS(dir), e.Project, name)
	if err != nil {
		return err
	}
	return e.applyPackChange(dir, c, "Removed pack "+name)
}

// packProjectDir returns the open project's directory, or why a pack cannot be installed into it now.
func (e *Editor) packProjectDir() (string, error) {
	switch {
	case e.playing:
		return "", ErrPlaying
	case e.Project == nil || e.ProjectPath == "":
		return "", ErrNoProject
	case e.ProjectDirty():
		return "", ErrProjectDirty
	}
	return filepath.Dir(e.ProjectPath), nil
}

// applyPackChange writes c's files in the project directory dir and saves c's project, restoring both when saving fails, and reports done in the status bar.
func (e *Editor) applyPackChange(dir string, c *library.Change, done string) error {
	if c.NoOp() {
		e.Status = "Pack " + c.Pack.Name + " " + c.Pack.PackVersion + " is already installed"
		e.notify()
		return nil
	}
	undo, err := c.ApplyFiles(dir)
	if err != nil {
		return err
	}
	prev := e.Project
	e.Project = c.Project
	if err := e.SaveProject(); err != nil {
		e.Project = prev
		return errors.Join(err, undo())
	}
	for i, t := range e.tabs {
		if i != e.activeTab && t.history != nil {
			t.history.commands = nil
			t.history.cursor = 0
			if t.history.group != nil {
				t.history.group.cmds = nil
			}
		}
	}
	e.History.Clear()
	e.filesRev++
	e.assetsRev++
	e.rebuildAllInactiveTabs()
	e.invalidateProjectIndex()
	e.dropStalePluginTool()
	e.Status = done
	if missing := e.MissingPlugins(); len(missing) > 0 {
		e.Status += "; plugins not built into this editor: " + strings.Join(missing, ", ")
	}
	e.notify()
	return nil
}

// packFetch is a fetch of a pack from a remote library running off the event loop.
type packFetch struct {
	// name is the pack's name.
	name string
	// project is the path of the project the pack is for.
	project string
	// cancel stops the fetch.
	cancel context.CancelFunc
}

// FetchingPack names the pack the library panel is fetching from a remote library, or is empty.
func (e *Editor) FetchingPack() string {
	if e.fetch == nil {
		return ""
	}
	return e.fetch.name
}

// stopFetch cancels the library panel's fetch under way, if any, so that its pack is not installed; opening another project and closing the panel do.
func (e *Editor) stopFetch() {
	if e.fetch != nil {
		e.fetch.cancel()
		e.fetch = nil
		e.notify()
	}
}

// installFromLibrary fetches the pack called name from Library and installs it. A library on disk is read at once; a remote one is fetched off the event loop and installed back on it through cx, while FetchingPack names the pack. A fetch that fails, or that is cancelled by stopFetch, installs nothing, and so does one that finishes after another project was opened.
func (e *Editor) installFromLibrary(cx *core.Scope, name string) {
	lib := e.Library
	if lib == nil || e.fetch != nil {
		return
	}
	if !lib.Remote() {
		p, err := lib.Fetch(context.Background(), name)
		if err == nil {
			err = e.InstallPack(p)
		}
		e.report(err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &packFetch{name: name, project: e.ProjectPath, cancel: cancel}
	e.fetch = f
	e.Status = "Fetching pack " + name + "…"
	e.notify()
	go func() {
		p, err := lib.Fetch(ctx, name)
		cx.Post(func() {
			cancel()
			if e.fetch != f {
				// stopFetch cancelled this fetch; the project it was for may no longer be open.
				return
			}
			e.fetch = nil
			if err == nil && e.ProjectPath != f.project {
				err = errors.New("editor: the project changed while pack " + name + " was fetched; it was not installed")
			}
			if err == nil {
				err = e.InstallPack(p)
			}
			if err != nil {
				e.report(err)
				return
			}
			e.notify()
		})
	}()
}

// LibraryView renders the library panel: every pack of Editor.Library with its kind, version and description, the version the open project has installed, and buttons to install, update and remove it. It renders nothing while Library is nil.
func LibraryView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	if ed.Library == nil {
		return nil
	}
	colors := ui.ColorsOf(cx)
	entries := ed.Library.Index.Search("", "")
	installed := func(name string) (version, digest string) {
		_ = rev.Get()
		if ed.Project == nil {
			return "", ""
		}
		if p := ed.Project.FindPack(name); p != nil {
			return p.PackVersion, p.SHA256
		}
		return "", ""
	}
	blocked := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil || ed.fetch != nil
	}
	// A fetch the panel started ends with it.
	cx.Cleanup(ed.stopFetch)

	rows := make([]core.Node, 0, len(entries))
	for _, entry := range entries {
		e := entry
		rows = append(rows, core.View{
			TestID: "library-pack-" + e.Name,
			Style: core.Style{
				Gap:          4,
				Padding:      core.All(6),
				CornerRadius: 6,
				BorderWidth:  1,
				BorderColor:  colors.Line(),
				Background:   colors.Surface(),
			},
			Children: []core.Node{
				core.Text{
					Content: e.Name,
					Style:   core.Style{FontSize: 12, FontWeight: 600, Color: colors.Text()},
				},
				core.Text{
					TestID: "library-version-" + e.Name,
					ContentFunc: func() string {
						line := e.Kind + " " + e.PackVersion
						if v, _ := installed(e.Name); v != "" {
							line += " · " + ed.T("library.installedMeta", l10n.Param("version", v))
						}
						return line
					},
					Style: core.Style{FontSize: 10, Color: colors.Muted()},
				},
				core.Text{
					Content: e.Description,
					Style:   core.Style{FontSize: 11, Color: colors.Muted()},
				},
				ui.HStack{
					Gap: 6,
					Children: []core.Node{
						ui.Button{
							TestID:  "library-install-" + e.Name,
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							TextFunc: func() string {
								switch v, digest := installed(e.Name); {
								case digest == e.SHA256:
									return ed.T("library.installed")
								case v != "":
									return ed.T("library.update")
								default:
									return ed.T("library.install")
								}
							},
							Busy: func() bool {
								_ = rev.Get()
								return ed.FetchingPack() == e.Name
							},
							Disabled: func() bool {
								_, digest := installed(e.Name)
								return blocked() || digest == e.SHA256
							},
							OnPress: func() { ed.installFromLibrary(cx, e.Name) },
						},
						ui.Button{
							TestID:  "library-remove-" + e.Name,
							Text:    ed.T("library.remove"),
							Size:    ui.SizeSmall,
							Variant: ui.ButtonSecondary,
							Disabled: func() bool {
								v, _ := installed(e.Name)
								return blocked() || v == ""
							},
							OnPress: func() { ed.report(ed.RemovePack(e.Name)) },
						},
					},
				},
			},
		})
	}
	if len(rows) == 0 {
		rows = append(rows, core.Text{
			Content: ed.T("library.empty"),
			Style:   core.Style{FontSize: 12, Color: colors.Muted()},
		})
	}

	return core.View{
		TestID: "library-panel",
		Style: dockedStyle(cx, core.Style{
			Width:       core.Pt(224),
			Background:  colors.Surface(),
			Borders:     core.Edges{Right: 1},
			BorderColor: colors.Line(),
			Padding:     core.All(10),
			Gap:         8,
		}),
		Children: []core.Node{
			core.Text{
				TestID:  "library-title",
				Content: ed.T("library.title"),
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1, FlexBasis: core.Pt(0), MinHeight: core.Pt(0)},
				ContentStyle: core.Style{Gap: 6},
				Children:     rows,
			},
		},
	}
}
