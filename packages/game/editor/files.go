package editor

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// FilesView renders the file browser: the files and directories of the open project (ProjectFiles), an Open button that opens the selected scene in the editor (OpenScene), and a path field with a Move button that renames or moves the selected file through MoveFile, so every reference to it follows and Undo moves it back. Pressing a row selects it and puts its path in the field, and pressing a scene file (*.scene.yaml, *.scene.yml, or one listed under autoload.scenes, as OpenProblem names scenes) twice opens it as Open does; while the scene or project has unsaved edits, opening asks first in EditorView, which shows the prompt, and is refused elsewhere. Refresh lists the files again after something outside the editor changed them. It renders nothing while no project is open.
func FilesView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	type targetDraft struct {
		rev  uint64
		text string
	}
	draft := core.NewSignal(cx, targetDraft{})
	targetText := func() string {
		_ = rev.Get()
		d := draft.Get()
		if d.rev == ed.selectedFileRev && ed.selectedFileRev != 0 {
			return d.text
		}
		return ed.SelectedFile
	}
	refresh := core.NewSignal(cx, 0)
	// The listing reads the disk, so it is made again only when the project or its files change, not on every edit.
	listing := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%d\x00%d", ed.ProjectPath, ed.filesRev, refresh.Get())
	})
	type listed struct {
		files []ProjectFile
		err   error
	}
	files := core.NewMemoFunc(cx, func() listed {
		_ = listing.Get()
		got, err := ed.ProjectFiles()
		return listed{got, err}
	}, func(a, b listed) bool { return slices.Equal(a.files, b.files) && a.err == b.err })
	// current is the selected path while the project still holds it: after an undo moves it away, nothing is selected and the field is empty.
	current := core.NewMemo(cx, func() string {
		_ = rev.Get()
		s := ed.SelectedFile
		if s == "" || !slices.ContainsFunc(files.Get().files, func(f ProjectFile) bool { return f.Path == s }) {
			return ""
		}
		return s
	})
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil && ed.ProjectPath != ""
	})

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		l := files.Get()
		if err := l.err; err != nil {
			return core.Text{
				TestID:  "files-error",
				Content: err.Error(),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		rows := make([]core.Node, 0, len(l.files))
		for _, f := range l.files {
			p := f.Path
			name := path.Base(p)
			if f.Dir {
				name += "/"
			}
			rows = append(rows, core.View{
				TestID: "files-row-" + p,
				StyleFunc: func() core.Style {
					bg := colors.Inset()
					if current.Get() == p {
						bg = colors.Raised()
					}
					return core.Style{
						Padding:      core.Symmetric(3, 6),
						Margin:       core.Edges{Left: core.Points(6 * strings.Count(p, "/"))},
						CornerRadius: 4,
						Background:   bg,
					}
				},
				OnPress: func(ev core.PressEvent) {
					ed.SelectFile(p)
					if ev.Clicks == 2 && !f.Dir && !ed.Playing() {
						switch {
						case isStatesFile(p):
							ed.report(ed.OpenStatesProjectFile(p))
						case isTerrainFile(p):
							ed.report(ed.OpenTerrainProjectFile(p))
						case isParticleFilePath(p):
							ed.report(ed.OpenParticleEmitterProjectFile(p))
						case isProjectScene(ed.Project, p) && p != ed.ScenePath:
							ed.openSceneAsked(p)
						}
					}
				},
				Children: []core.Node{core.Text{
					Content: name,
					Style:   core.Style{FontSize: 12, Color: colors.Text()},
				}},
			})
		}
		return core.View{Style: core.Style{Gap: 1}, Children: rows}
	})

	return core.Show{
		When: open.Get,
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "file-browser",
				Style: core.Style{
					FlexGrow:    1,
					FlexBasis:   core.Pt(0),
					Background:  colors.Inset(),
					Borders:     core.Edges{Top: 1},
					BorderColor: colors.Line(),
					Padding:     core.All(6),
					Gap:         4,
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.Text{
								TestID:  "files-title",
								Content: ed.T("files.title"),
								Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
							},
							ui.Button{
								TestID:  "files-open",
								Text:    ed.T("files.open"),
								Label:   ed.T("files.openLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									p := current.Get()
									if ed.Playing() || p == "" || slices.ContainsFunc(files.Get().files, func(f ProjectFile) bool { return f.Path == p && f.Dir }) {
										return true
									}
									if isStatesFile(p) || isParticleFilePath(p) {
										return false
									}
									return p == ed.ScenePath || !isProjectScene(ed.Project, p)
								},
								OnPress: func() {
									if p := current.Get(); p != "" {
										if isStatesFile(p) {
											ed.report(ed.OpenStatesProjectFile(p))
										} else if isParticleFilePath(p) {
											ed.report(ed.OpenParticleEmitterProjectFile(p))
										} else {
											ed.openSceneAsked(p)
										}
									}
								},
							},
							ui.Button{
								TestID:  "files-refresh",
								Text:    ed.T("files.refresh"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								OnPress: func() {
									refresh.Set(refresh.Get() + 1)
									ed.report(ed.RefreshSearch())
								},
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1, MinHeight: core.Pt(48)},
						Children: []core.Node{list},
					},
					ui.Input{
						TestID:   "files-path",
						Label:    ed.T("files.newPathLabel"),
						Disabled: func() bool { return current.Get() == "" },
						Value: func() string {
							if current.Get() == "" {
								return ""
							}
							return targetText()
						},
						OnChange: func(v string) {
							draft.Set(targetDraft{rev: ed.selectedFileRev, text: v})
						},
					},
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							ui.Button{
								TestID:  "files-move",
								Text:    ed.T("files.move"),
								Label:   ed.T("files.moveLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									from, to := current.Get(), strings.TrimSpace(targetText())
									return ed.Playing() || from == "" || to == "" || to == from
								},
								OnPress: func() {
									from, to := current.Get(), strings.TrimSpace(targetText())
									if from == "" {
										return
									}
									if err := ed.MoveFile(from, to); err != nil {
										ed.report(err)
										return
									}
									ed.SelectFile(to)
								},
							},
							ui.Button{
								TestID:  "files-open-table",
								Text:    ed.T("files.openTable"),
								Label:   ed.T("files.openTableLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									if ed.Playing() {
										return true
									}
									from := current.Get()
									if from == "" {
										return true
									}
									ext := strings.ToLower(path.Ext(from))
									return ext != ".yaml" && ext != ".yml"
								},
								OnPress: func() {
									from := current.Get()
									if from == "" {
										return
									}
									if err := ed.OpenDataTableProjectFile(from); err != nil {
										ed.report(err)
									}
								},
							},
							ui.Button{
								TestID:  "files-open-tileset",
								Text:    "Edit Tileset",
								Label:   "Open the selected tileset file in the Tileset Editor",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									if ed.Playing() {
										return true
									}
									return !isTilesetFile(current.Get())
								},
								OnPress: func() {
									from := current.Get()
									if from == "" {
										return
									}
									if err := ed.OpenTilesetProjectFile(from); err != nil {
										ed.report(err)
									}
								},
							},
							ui.Button{
								TestID:  "files-open-terrain",
								Text:    "Terrain",
								Label:   "Open the selected *.terrain.yaml file in the 3D terrain editor",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									if ed.Playing() {
										return true
									}
									from := current.Get()
									return from == "" || !isTerrainFile(from)
								},
								OnPress: func() {
									from := current.Get()
									if from == "" {
										return
									}
									if err := ed.OpenTerrainProjectFile(from); err != nil {
										ed.report(err)
									}
								},
							},
						},
					},
					core.Show{
						When: func() bool {
							_ = rev.Get()
							from := current.Get()
							return from != "" && isParticleFilePath(from)
						},
						Then: func(cx *core.Scope) core.Node {
							return ui.Button{
								TestID:  "files-open-emitter",
								Text:    "Open Emitter",
								Label:   "Open the selected YAML file in the Particle Emitter editor",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									_ = rev.Get()
									return ed.Playing()
								},
								OnPress: func() {
									if from := current.Get(); from != "" {
										if err := ed.OpenParticleEmitterProjectFile(from); err != nil {
											ed.report(err)
										}
									}
								},
							}
						},
					},
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							ui.ToggleButton{
								TestID: "files-search",
								Text:   ed.T("files.search"),
								Label:  ed.T("files.searchLabel"),
								On: func() bool {
									_ = rev.Get()
									return ed.SearchOpen()
								},
								OnChange: func(on bool) {
									ed.SetSearchOpen(on)
								},
							},
							ui.Button{
								TestID:  "files-find-refs",
								Text:    ed.T("files.findRefs"),
								Label:   ed.T("files.findRefsLabel"),
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								Disabled: func() bool {
									return current.Get() == ""
								},
								OnPress: func() {
									from := current.Get()
									if from == "" {
										return
									}
									if _, err := ed.ShowReferences(from); err != nil {
										ed.report(err)
									}
								},
							},
						},
					},
				},
			}
		},
	}
}
