package editor

import (
	"fmt"
	"path"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// FilesView renders the file browser: the files and directories of the open project (ProjectFiles), and a path field with a Move button that renames or moves the selected one through MoveFile, so every reference to it follows and Undo moves it back. Pressing a row selects it and puts its path in the field; Refresh lists the files again after something outside the editor changed them. It renders nothing while no project is open.
func FilesView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	selected := core.NewSignal(cx, "")
	target := core.NewSignal(cx, "")
	refresh := core.NewSignal(cx, 0)
	// The listing reads the disk, so it is made again only when the project or its files change, not on every edit.
	listing := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%d\x00%d", ed.ProjectPath, ed.filesRev, refresh.Get())
	})
	open := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return ed.Project != nil && ed.ProjectPath != ""
	})

	list := core.Dynamic(func(cx *core.Scope) core.Node {
		_ = listing.Get()
		files, err := ed.ProjectFiles()
		if err != nil {
			return core.Text{
				TestID:  "files-error",
				Content: err.Error(),
				Style:   core.Style{FontSize: 11, Color: colors.Muted()},
			}
		}
		rows := make([]core.Node, 0, len(files))
		for _, f := range files {
			p := f.Path
			name := path.Base(p)
			if f.Dir {
				name += "/"
			}
			rows = append(rows, core.View{
				TestID: "files-row-" + p,
				StyleFunc: func() core.Style {
					bg := colors.Inset()
					if selected.Get() == p {
						bg = colors.Raised()
					}
					return core.Style{
						Padding:      core.Symmetric(3, 6),
						Margin:       core.Edges{Left: core.Points(6 * strings.Count(p, "/"))},
						CornerRadius: 4,
						Background:   bg,
					}
				},
				OnPress: func(core.PressEvent) {
					selected.Set(p)
					target.Set(p)
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
					Padding:     core.All(10),
					Gap:         8,
				},
				Children: []core.Node{
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.Text{
								Content: "Files",
								Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text(), FlexGrow: 1},
							},
							ui.Button{
								TestID:  "files-refresh",
								Text:    "Refresh",
								Size:    ui.SizeSmall,
								Variant: ui.ButtonSecondary,
								OnPress: func() { refresh.Set(refresh.Get() + 1) },
							},
						},
					},
					ui.ScrollView{
						Style:    core.Style{FlexGrow: 1},
						Children: []core.Node{list},
					},
					ui.Input{
						TestID:   "files-path",
						Label:    "New path of the selected file",
						Disabled: func() bool { return selected.Get() == "" },
						Value:    target.Get,
						OnChange: target.Set,
					},
					ui.Button{
						TestID:  "files-move",
						Text:    "Move",
						Label:   "Rename or move the selected file and update every reference to it",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						Disabled: func() bool {
							_ = rev.Get()
							from, to := selected.Get(), strings.TrimSpace(target.Get())
							return ed.Playing() || from == "" || to == "" || to == from
						},
						OnPress: func() {
							from, to := selected.Get(), strings.TrimSpace(target.Get())
							if err := ed.MoveFile(from, to); err != nil {
								ed.report(err)
								return
							}
							selected.Set(to)
							target.Set(to)
						},
					},
				},
			}
		},
	}
}
