package editor

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// ProjectSettingsSection identifies a category tab in the Project Settings panel.
type ProjectSettingsSection string

const (
	ProjectSectionGeneral  ProjectSettingsSection = "general"
	ProjectSectionWindow   ProjectSettingsSection = "window"
	ProjectSectionDisplay  ProjectSettingsSection = "display"
	ProjectSectionPhysics  ProjectSettingsSection = "physics"
	ProjectSectionAutoload ProjectSettingsSection = "autoload"
	ProjectSectionPlugins  ProjectSettingsSection = "plugins"
)

// projectSettingsState holds the Project Settings panel's open state and active section tab.
type projectSettingsState struct {
	open    bool
	section ProjectSettingsSection
}

// ProjectSettingsOpen reports whether the Project Settings panel is shown.
func (e *Editor) ProjectSettingsOpen() bool {
	return e != nil && e.projectSettings.open
}

// SetProjectSettingsOpen opens or closes the Project Settings panel.
func (e *Editor) SetProjectSettingsOpen(open bool) {
	if e == nil || e.projectSettings.open == open {
		return
	}
	e.projectSettings.open = open
	if open && e.projectSettings.section == "" {
		e.projectSettings.section = ProjectSectionGeneral
	}
	e.notify()
}

// ProjectSettingsSection returns the active section in the Project Settings panel, defaulting to ProjectSectionGeneral.
func (e *Editor) ProjectSettingsSection() ProjectSettingsSection {
	if e == nil || e.projectSettings.section == "" {
		return ProjectSectionGeneral
	}
	return e.projectSettings.section
}

// SetProjectSettingsSection switches the active section in the Project Settings panel.
func (e *Editor) SetProjectSettingsSection(section ProjectSettingsSection) {
	if e == nil {
		return
	}
	switch section {
	case ProjectSectionGeneral, ProjectSectionWindow, ProjectSectionDisplay, ProjectSectionPhysics, ProjectSectionAutoload, ProjectSectionPlugins:
	default:
		return
	}
	if e.ProjectSettingsSection() == section {
		return
	}
	e.projectSettings.section = section
	e.notify()
}

// ProjectCanonicalSavePending reports whether the open project has not yet been saved in canonical YAML form by SaveProject during this session.
func (e *Editor) ProjectCanonicalSavePending() bool {
	return e != nil && e.Project != nil && !e.projectSavedOnce
}

// SetProjectName updates game.yaml's name through EditProject (validated and undoable).
func (e *Editor) SetProjectName(name string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Name == name {
		return nil
	}
	return e.EditProject("Set Project Name", func(f *project.File) {
		f.Name = name
	})
}

// SetProjectID updates game.yaml's id through EditProject (validated and undoable).
func (e *Editor) SetProjectID(id string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.ID == id {
		return nil
	}
	return e.EditProject("Set Project ID", func(f *project.File) {
		f.ID = id
	})
}

// SetProjectAppVersion updates game.yaml's appVersion through EditProject (validated and undoable).
func (e *Editor) SetProjectAppVersion(version string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.AppVersion == version {
		return nil
	}
	return e.EditProject("Set App Version", func(f *project.File) {
		f.AppVersion = version
	})
}

// SetProjectWindowTitle updates game.yaml's window.title through EditProject (validated and undoable).
func (e *Editor) SetProjectWindowTitle(title string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Window.Title == title {
		return nil
	}
	return e.EditProject("Set Window Title", func(f *project.File) {
		f.Window.Title = title
	})
}

// SetProjectWindowSize updates game.yaml's window.width and window.height through EditProject (validated and undoable).
func (e *Editor) SetProjectWindowSize(width, height float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Window.Width == width && e.Project.Window.Height == height {
		return nil
	}
	return e.EditProject("Set Window Size", func(f *project.File) {
		f.Window.Width = width
		f.Window.Height = height
	})
}

// SetProjectWindowLimits updates game.yaml's window min/max width and height limits through EditProject (validated and undoable).
func (e *Editor) SetProjectWindowLimits(minWidth, minHeight, maxWidth, maxHeight float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	w := e.Project.Window
	if w.MinWidth == minWidth && w.MinHeight == minHeight && w.MaxWidth == maxWidth && w.MaxHeight == maxHeight {
		return nil
	}
	return e.EditProject("Set Window Limits", func(f *project.File) {
		f.Window.MinWidth = minWidth
		f.Window.MinHeight = minHeight
		f.Window.MaxWidth = maxWidth
		f.Window.MaxHeight = maxHeight
	})
}

// SetProjectWindowResizable updates game.yaml's window.resizable through EditProject (validated and undoable).
func (e *Editor) SetProjectWindowResizable(resizable bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	cur := e.Project.Window.Resizable == nil || *e.Project.Window.Resizable
	if cur == resizable {
		return nil
	}
	return e.EditProject("Set Window Resizable", func(f *project.File) {
		if resizable {
			f.Window.Resizable = nil
		} else {
			f.Window.Resizable = project.Bool(false)
		}
	})
}

// SetProjectWindowAppearance updates game.yaml's window.appearance through EditProject (validated and undoable).
func (e *Editor) SetProjectWindowAppearance(appearance string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Window.Appearance == appearance {
		return nil
	}
	return e.EditProject("Set Window Appearance", func(f *project.File) {
		f.Window.Appearance = appearance
	})
}

// SetProjectDisplayEnabled enables or disables game.yaml's virtual display section through EditProject.
// Enabling initializes display.width and display.height from the project's window size.
func (e *Editor) SetProjectDisplayEnabled(enabled bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if (e.Project.Display != nil) == enabled {
		return nil
	}
	if !enabled {
		return e.EditProject("Disable Virtual Display", func(f *project.File) {
			f.Display = nil
		})
	}
	return e.EditProject("Enable Virtual Display", func(f *project.File) {
		w, h := f.Window.Width, f.Window.Height
		if w <= 0 || h <= 0 {
			w, h = 640, 360
		}
		f.Display = &project.Display{Width: w, Height: h}
	})
}

// SetProjectDisplaySize updates game.yaml's display.width and display.height through EditProject (creating display if nil).
func (e *Editor) SetProjectDisplaySize(width, height float32) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Display != nil && e.Project.Display.Width == width && e.Project.Display.Height == height {
		return nil
	}
	return e.EditProject("Set Display Size", func(f *project.File) {
		if f.Display == nil {
			f.Display = &project.Display{}
		}
		f.Display.Width = width
		f.Display.Height = height
	})
}

// SetProjectDisplayMode updates game.yaml's display.mode through EditProject (validated and undoable).
func (e *Editor) SetProjectDisplayMode(mode string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Display == nil {
		return errors.New("editor: virtual display is disabled")
	}
	if e.Project.Display.Mode == mode {
		return nil
	}
	return e.EditProject("Set Display Mode", func(f *project.File) {
		f.Display.Mode = mode
	})
}

// SetProjectDisplayIntegerScale updates game.yaml's display.integerScale through EditProject (validated and undoable).
func (e *Editor) SetProjectDisplayIntegerScale(on bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Display == nil {
		return errors.New("editor: virtual display is disabled")
	}
	if e.Project.Display.IntegerScale == on {
		return nil
	}
	return e.EditProject("Set Display Integer Scale", func(f *project.File) {
		f.Display.IntegerScale = on
	})
}

// SetProjectDisplayPixelSnap updates game.yaml's display.pixelSnap through EditProject (validated and undoable).
func (e *Editor) SetProjectDisplayPixelSnap(on bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Display == nil {
		return errors.New("editor: virtual display is disabled")
	}
	if e.Project.Display.PixelSnap == on {
		return nil
	}
	return e.EditProject("Set Display Pixel Snap", func(f *project.File) {
		f.Display.PixelSnap = on
	})
}

// SetProjectDisplayBarColor updates game.yaml's display.barColor through EditProject (validated and undoable).
func (e *Editor) SetProjectDisplayBarColor(barColor string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Display == nil {
		return errors.New("editor: virtual display is disabled")
	}
	if e.Project.Display.BarColor == barColor {
		return nil
	}
	return e.EditProject("Set Display Bar Color", func(f *project.File) {
		f.Display.BarColor = barColor
	})
}

// SetProjectMainScene updates game.yaml's autoload.mainScene through EditProject (validated and undoable).
func (e *Editor) SetProjectMainScene(name string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	cur := ""
	if e.Project.Autoload != nil {
		cur = e.Project.Autoload.MainScene
	}
	if cur == name {
		return nil
	}
	return e.EditProject("Set Main Scene", func(f *project.File) {
		if f.Autoload == nil {
			if name == "" {
				return
			}
			f.Autoload = &project.Autoload{}
		}
		f.Autoload.MainScene = name
		cleanEmptyAutoload(f)
	})
}

// AddProjectAutoloadScene appends a scene entry to game.yaml's autoload.scenes through EditProject.
func (e *Editor) AddProjectAutoloadScene(name, relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	return e.EditProject("Add Autoload Scene "+name, func(f *project.File) {
		if f.Autoload == nil {
			f.Autoload = &project.Autoload{}
		}
		f.Autoload.Scenes = append(f.Autoload.Scenes, project.Entry{Name: name, Path: relPath})
	})
}

// UpdateProjectAutoloadScene updates the autoload scene entry at index through EditProject.
// When the renamed scene was autoload.mainScene, mainScene follows the new name.
func (e *Editor) UpdateProjectAutoloadScene(index int, name, relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Autoload == nil || index < 0 || index >= len(e.Project.Autoload.Scenes) {
		return fmt.Errorf("editor: autoload scene index %d out of range", index)
	}
	cur := e.Project.Autoload.Scenes[index]
	if cur.Name == name && cur.Path == relPath {
		return nil
	}
	return e.EditProject("Update Autoload Scene "+cur.Name, func(f *project.File) {
		oldName := f.Autoload.Scenes[index].Name
		f.Autoload.Scenes[index] = project.Entry{Name: name, Path: relPath}
		if f.Autoload.MainScene == oldName {
			f.Autoload.MainScene = name
		}
	})
}

// RemoveProjectAutoloadScene removes the autoload scene entry at index through EditProject.
// When the removed scene was autoload.mainScene, mainScene is cleared.
func (e *Editor) RemoveProjectAutoloadScene(index int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Autoload == nil || index < 0 || index >= len(e.Project.Autoload.Scenes) {
		return fmt.Errorf("editor: autoload scene index %d out of range", index)
	}
	removed := e.Project.Autoload.Scenes[index].Name
	return e.EditProject("Remove Autoload Scene "+removed, func(f *project.File) {
		f.Autoload.Scenes = slices.Delete(f.Autoload.Scenes, index, index+1)
		if len(f.Autoload.Scenes) == 0 {
			f.Autoload.Scenes = nil
		}
		if f.Autoload.MainScene == removed {
			f.Autoload.MainScene = ""
		}
		cleanEmptyAutoload(f)
	})
}

// AddProjectAutoloadResource appends a resource entry to game.yaml's autoload.resources through EditProject.
func (e *Editor) AddProjectAutoloadResource(name, relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	return e.EditProject("Add Autoload Resource "+name, func(f *project.File) {
		if f.Autoload == nil {
			f.Autoload = &project.Autoload{}
		}
		f.Autoload.Resources = append(f.Autoload.Resources, project.Entry{Name: name, Path: relPath})
	})
}

// UpdateProjectAutoloadResource updates the autoload resource entry at index through EditProject.
func (e *Editor) UpdateProjectAutoloadResource(index int, name, relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Autoload == nil || index < 0 || index >= len(e.Project.Autoload.Resources) {
		return fmt.Errorf("editor: autoload resource index %d out of range", index)
	}
	cur := e.Project.Autoload.Resources[index]
	if cur.Name == name && cur.Path == relPath {
		return nil
	}
	return e.EditProject("Update Autoload Resource "+cur.Name, func(f *project.File) {
		f.Autoload.Resources[index] = project.Entry{Name: name, Path: relPath}
	})
}

// RemoveProjectAutoloadResource removes the autoload resource entry at index through EditProject.
func (e *Editor) RemoveProjectAutoloadResource(index int) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if e.Project.Autoload == nil || index < 0 || index >= len(e.Project.Autoload.Resources) {
		return fmt.Errorf("editor: autoload resource index %d out of range", index)
	}
	removed := e.Project.Autoload.Resources[index].Name
	return e.EditProject("Remove Autoload Resource "+removed, func(f *project.File) {
		f.Autoload.Resources = slices.Delete(f.Autoload.Resources, index, index+1)
		if len(f.Autoload.Resources) == 0 {
			f.Autoload.Resources = nil
		}
		cleanEmptyAutoload(f)
	})
}

func cleanEmptyAutoload(f *project.File) {
	if f.Autoload != nil && f.Autoload.MainScene == "" && len(f.Autoload.Scenes) == 0 && len(f.Autoload.Resources) == 0 {
		f.Autoload = nil
	}
}

// ProjectSceneFiles lists slash-separated relative paths of .yaml/.yml files inside the open project (excluding game.yaml and nsgo.yaml) suitable for autoload.scenes.
func (e *Editor) ProjectSceneFiles() ([]string, error) {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if f.Dir || f.Path == project.FileName || f.Path == project.NSGoFileName {
			continue
		}
		if strings.HasSuffix(f.Path, ".yaml") || strings.HasSuffix(f.Path, ".yml") {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

// ProjectResourceFiles lists slash-separated relative paths of files inside the open project (excluding game.yaml and nsgo.yaml) suitable for autoload.resources.
func (e *Editor) ProjectResourceFiles() ([]string, error) {
	files, err := e.ProjectFiles()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if f.Dir || f.Path == project.FileName || f.Path == project.NSGoFileName {
			continue
		}
		out = append(out, f.Path)
	}
	return out, nil
}

// ProjectSettingsPanelView renders the Project Settings panel below the viewport while a project is open and ProjectSettingsOpen reports true.
// Every edit runs through Editor.EditProject (or SetPluginEnabled), validates with project.Validate with inline field errors, participates in Undo/Redo, and writes game.yaml + synced nsgo.yaml on Save.
func ProjectSettingsPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	return core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.Project != nil && ed.ProjectSettingsOpen()
		},
		Then: func(cx *core.Scope) core.Node {
			return projectSettingsPanel(cx, ed, rev)
		},
	}
}

func projectSettingsPanel(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	colors := ui.ColorsOf(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil
	}

	section := core.NewMemo(cx, func() ProjectSettingsSection {
		_ = rev.Get()
		return ed.ProjectSettingsSection()
	})

	header := ui.HStack{
		Gap: 8,
		Children: []core.Node{
			core.Text{
				TestID:  "project-settings-title",
				Content: "Project Settings",
				Style:   core.Style{FontSize: 13, FontWeight: 700, Color: colors.Text()},
			},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.ProjectDirty()
				},
				Then: func(cx *core.Scope) core.Node {
					return core.Text{
						TestID:  "project-settings-dirty",
						Content: "Unsaved changes",
						Style:   core.Style{FontSize: 11, Color: colors.Warn()},
					}
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			ui.Button{
				TestID:  "project-settings-close",
				Text:    "Close",
				Size:    ui.SizeSmall,
				Variant: ui.ButtonGhost,
				OnPress: func() { ed.SetProjectSettingsOpen(false) },
			},
		},
	}

	// Display notice before the first save that saving writes canonical YAML without comments.
	canonicalNotice := core.Show{
		When: func() bool {
			_ = rev.Get()
			return ed.ProjectCanonicalSavePending()
		},
		Then: func(cx *core.Scope) core.Node {
			return core.View{
				TestID: "project-settings-canonical-notice",
				Style: core.Style{
					Padding:      core.Symmetric(5, 8),
					CornerRadius: 4,
					Background:   colors.Raised(),
					Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 2},
					BorderColor:  colors.Warn(),
				},
				Children: []core.Node{
					core.Text{
						Content: "Saving writes game.yaml in canonical YAML form, which does not preserve comments.",
						Style:   core.Style{FontSize: 11, Color: colors.Text()},
					},
				},
			}
		},
	}

	tabBtn := func(sec ProjectSettingsSection, label string) core.Node {
		return ui.ToggleButton{
			TestID: "project-settings-tab-" + string(sec),
			Text:   label,
			On: func() bool {
				return section.Get() == sec
			},
			OnChange: func(on bool) {
				if on {
					ed.SetProjectSettingsSection(sec)
				}
			},
		}
	}

	sidebar := core.View{
		TestID: "project-settings-sidebar",
		Style: core.Style{
			Width:       core.Pt(136),
			Gap:         4,
			Padding:     core.Edges{Right: 10},
			Borders:     core.Edges{Right: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			tabBtn(ProjectSectionGeneral, "General"),
			tabBtn(ProjectSectionWindow, "Window"),
			tabBtn(ProjectSectionDisplay, "Display"),
			tabBtn(ProjectSectionPhysics, "Physics"),
			tabBtn(ProjectSectionAutoload, "Autoload"),
			tabBtn(ProjectSectionPlugins, "Plugins"),
		},
	}

	content := core.View{
		TestID: "project-settings-content",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.Edges{Left: 12},
		},
		Children: []core.Node{
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 8},
				Children: []core.Node{
					core.Dynamic(func(cx *core.Scope) core.Node {
						switch section.Get() {
						case ProjectSectionWindow:
							return psWindowSection(cx, ed, rev, disabled)
						case ProjectSectionDisplay:
							return psDisplaySection(cx, ed, rev, disabled)
						case ProjectSectionPhysics:
							return psPhysicsSection(cx, ed, rev, disabled)
						case ProjectSectionAutoload:
							return psAutoloadSection(cx, ed, rev, disabled)
						case ProjectSectionPlugins:
							return psPluginsSection(cx, ed, rev, disabled)
						default:
							return psGeneralSection(cx, ed, rev, disabled)
						}
					}),
				},
			},
		},
	}

	return core.View{
		TestID: "project-settings-panel",
		Style: core.Style{
			Height:      core.Pt(300),
			Padding:     core.Symmetric(8, 12),
			Gap:         8,
			Background:  colors.Inset(),
			Borders:     core.Edges{Top: 1},
			BorderColor: colors.Line(),
		},
		Children: []core.Node{
			header,
			canonicalNotice,
			core.View{
				Style: core.Style{
					Direction: core.Row,
					FlexGrow:  1,
					FlexBasis: core.Pt(0),
					Overflow:  core.Hidden,
				},
				Children: []core.Node{sidebar, content},
			},
		},
	}
}

func psSectionHeading(colors ui.Colors, title string) core.Node {
	return core.Text{
		Content: title,
		Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Muted()},
	}
}

func psErrorText(colors ui.Colors, testID string, errMsg *core.Signal[string]) core.Node {
	return core.Show{
		When: func() bool { return errMsg.Get() != "" },
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID:      testID,
				ContentFunc: errMsg.Get,
				Style:       core.Style{FontSize: 11, Color: colors.Danger()},
			}
		},
	}
}

func psFieldLayout(colors ui.Colors, caption string, control core.Node, errNode core.Node) core.Node {
	children := []core.Node{
		ui.HStack{
			Gap: 8,
			Children: []core.Node{
				core.Text{
					Content: caption,
					Style:   core.Style{FontSize: 12, Color: colors.Text(), Width: core.Pt(130)},
				},
				core.View{
					Style:    core.Style{FlexGrow: 1, MaxWidth: core.Pt(320)},
					Children: []core.Node{control},
				},
			},
		},
	}
	if errNode != nil {
		children = append(children, core.View{
			Style:    core.Style{Padding: core.Edges{Left: 138}},
			Children: []core.Node{errNode},
		})
	}
	return core.View{Style: core.Style{Gap: 2}, Children: children}
}

func psTextField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, label, placeholder string, disabled func() bool, read func() string, apply func(string) error) core.Node {
	colors := ui.ColorsOf(cx)
	draft := core.NewSignal(cx, read())
	dirty := core.NewSignal(cx, false)
	errMsg := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ProjectPath + "\x00" + read()
	})
	cx.Effect(func() {
		_ = synced.Get()
		draft.Set(read())
		dirty.Set(false)
		errMsg.Set("")
	})

	commit := func(text string) {
		dirty.Set(false)
		if err := apply(text); err != nil {
			errMsg.Set(err.Error())
			ed.report(err)
			return
		}
		errMsg.Set("")
		draft.Set(read())
	}

	input := ui.Input{
		TestID:      testID,
		Label:       label,
		Placeholder: placeholder,
		Disabled:    disabled,
		Invalid:     func() bool { return errMsg.Get() != "" },
		Value: func() string {
			_ = rev.Get()
			if dirty.Get() || errMsg.Get() != "" {
				return draft.Get()
			}
			return read()
		},
		OnFocus: func() {
			if !dirty.Get() && errMsg.Get() == "" {
				draft.Set(read())
			}
		},
		OnChange: func(text string) {
			draft.Set(text)
			dirty.Set(true)
			errMsg.Set("")
		},
		OnSubmit: func(text string) {
			draft.Set(text)
			commit(text)
		},
		OnBlur: func() {
			if dirty.Get() {
				commit(draft.Get())
			}
		},
	}

	return psFieldLayout(colors, caption, input, psErrorText(colors, testID+"-error", errMsg))
}

func psNumberField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, label string, step float64, disabled func() bool, read func() float64, apply func(float64) error) core.Node {
	colors := ui.ColorsOf(cx)
	errMsg := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%g", ed.ProjectPath, read())
	})
	cx.Effect(func() {
		_ = synced.Get()
		errMsg.Set("")
	})

	num := ui.NumberInput{
		TestID:   testID,
		Label:    label,
		Step:     step,
		Disabled: disabled,
		Invalid:  func() bool { return errMsg.Get() != "" },
		Value: func() float64 {
			_ = rev.Get()
			return read()
		},
		OnChange: func(v float64) {
			if err := apply(v); err != nil {
				errMsg.Set(err.Error())
				ed.report(err)
				return
			}
			errMsg.Set("")
		},
	}

	return psFieldLayout(colors, caption, num, psErrorText(colors, testID+"-error", errMsg))
}

func psSwitchField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, label string, disabled func() bool, read func() bool, apply func(bool) error) core.Node {
	colors := ui.ColorsOf(cx)
	errMsg := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%t", ed.ProjectPath, read())
	})
	cx.Effect(func() {
		_ = synced.Get()
		errMsg.Set("")
	})

	sw := ui.Switch{
		TestID:   testID,
		Label:    label,
		Disabled: disabled,
		Invalid:  func() bool { return errMsg.Get() != "" },
		Checked: func() bool {
			_ = rev.Get()
			return read()
		},
		OnChange: func(on bool) {
			if err := apply(on); err != nil {
				errMsg.Set(err.Error())
				ed.report(err)
				return
			}
			errMsg.Set("")
		},
	}

	return psFieldLayout(colors, caption, sw, psErrorText(colors, testID+"-error", errMsg))
}

func psSelectField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, label string, options []ui.Option[string], optionsFunc func() []ui.Option[string], disabled func() bool, read func() string, apply func(string) error) core.Node {
	colors := ui.ColorsOf(cx)
	errMsg := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ProjectPath + "\x00" + read()
	})
	cx.Effect(func() {
		_ = synced.Get()
		errMsg.Set("")
	})

	sel := ui.Select[string]{
		TestID:      testID,
		Label:       label,
		Options:     options,
		OptionsFunc: optionsFunc,
		Disabled:    disabled,
		Invalid:     func() bool { return errMsg.Get() != "" },
		Value: func() string {
			_ = rev.Get()
			return read()
		},
		OnChange: func(v string) {
			if err := apply(v); err != nil {
				errMsg.Set(err.Error())
				ed.report(err)
				return
			}
			errMsg.Set("")
		},
	}

	return psFieldLayout(colors, caption, sel, psErrorText(colors, testID+"-error", errMsg))
}

func psGeneralSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "project-settings-section-general",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			psSectionHeading(colors, "GENERAL"),
			psTextField(cx, ed, rev,
				"project-settings-name", "Name", "Project Name", "My Game", disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.Name
				},
				ed.SetProjectName,
			),
			psTextField(cx, ed, rev,
				"project-settings-id", "ID", "Application ID", "dev.example.game", disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.ID
				},
				ed.SetProjectID,
			),
			psTextField(cx, ed, rev,
				"project-settings-app-version", "App Version", "App Version", "0.1.0", disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.AppVersion
				},
				ed.SetProjectAppVersion,
			),
		},
	}
}

func psWindowSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	appearanceOpts := []ui.Option[string]{
		{Value: "", Label: "System (default)"},
		{Value: "light", Label: "Light"},
		{Value: "dark", Label: "Dark"},
	}

	return core.View{
		TestID: "project-settings-section-window",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			psSectionHeading(colors, "WINDOW"),
			psTextField(cx, ed, rev,
				"project-settings-window-title", "Title", "Window Title", "Defaults to project name", disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.Window.Title
				},
				ed.SetProjectWindowTitle,
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-width", "Width", "Window Width", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.Width)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					return ed.SetProjectWindowSize(float32(v), ed.Project.Window.Height)
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-height", "Height", "Window Height", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.Height)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					return ed.SetProjectWindowSize(ed.Project.Window.Width, float32(v))
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-min-width", "Min Width", "Window Min Width", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.MinWidth)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					w := ed.Project.Window
					return ed.SetProjectWindowLimits(float32(v), w.MinHeight, w.MaxWidth, w.MaxHeight)
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-min-height", "Min Height", "Window Min Height", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.MinHeight)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					w := ed.Project.Window
					return ed.SetProjectWindowLimits(w.MinWidth, float32(v), w.MaxWidth, w.MaxHeight)
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-max-width", "Max Width", "Window Max Width", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.MaxWidth)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					w := ed.Project.Window
					return ed.SetProjectWindowLimits(w.MinWidth, w.MinHeight, float32(v), w.MaxHeight)
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-max-height", "Max Height", "Window Max Height", 1, disabled,
				func() float64 {
					if ed.Project == nil {
						return 0
					}
					return float64(ed.Project.Window.MaxHeight)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					w := ed.Project.Window
					return ed.SetProjectWindowLimits(w.MinWidth, w.MinHeight, w.MaxWidth, float32(v))
				},
			),
			psSwitchField(cx, ed, rev,
				"project-settings-window-resizable", "Resizable", "Window Resizable", disabled,
				func() bool {
					if ed.Project == nil {
						return true
					}
					return ed.Project.Window.Resizable == nil || *ed.Project.Window.Resizable
				},
				ed.SetProjectWindowResizable,
			),
			psSelectField(cx, ed, rev,
				"project-settings-window-appearance", "Appearance", "Window Appearance", appearanceOpts, nil, disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.Window.Appearance
				},
				ed.SetProjectWindowAppearance,
			),
		},
	}
}

func psDisplaySection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	displayDisabled := func() bool {
		_ = rev.Get()
		return disabled() || ed.Project == nil || ed.Project.Display == nil
	}
	modeOpts := []ui.Option[string]{
		{Value: "", Label: "Default (fit)"},
		{Value: "fit", Label: "fit"},
		{Value: "fill", Label: "fill"},
		{Value: "stretch", Label: "stretch"},
		{Value: "expand", Label: "expand"},
	}

	return core.View{
		TestID: "project-settings-section-display",
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			psSectionHeading(colors, "DISPLAY"),
			psSwitchField(cx, ed, rev,
				"project-settings-display-enabled", "Virtual Display", "Enable Virtual Display", disabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil
				},
				ed.SetProjectDisplayEnabled,
			),
			psNumberField(cx, ed, rev,
				"project-settings-display-width", "Virtual Width", "Display Virtual Width", 1, displayDisabled,
				func() float64 {
					if ed.Project == nil || ed.Project.Display == nil {
						return 0
					}
					return float64(ed.Project.Display.Width)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					h := float32(0)
					if ed.Project.Display != nil {
						h = ed.Project.Display.Height
					}
					return ed.SetProjectDisplaySize(float32(v), h)
				},
			),
			psNumberField(cx, ed, rev,
				"project-settings-display-height", "Virtual Height", "Display Virtual Height", 1, displayDisabled,
				func() float64 {
					if ed.Project == nil || ed.Project.Display == nil {
						return 0
					}
					return float64(ed.Project.Display.Height)
				},
				func(v float64) error {
					if ed.Project == nil {
						return ErrNoProject
					}
					w := float32(0)
					if ed.Project.Display != nil {
						w = ed.Project.Display.Width
					}
					return ed.SetProjectDisplaySize(w, float32(v))
				},
			),
			psSelectField(cx, ed, rev,
				"project-settings-display-mode", "Mode", "Display Mode", modeOpts, nil, displayDisabled,
				func() string {
					if ed.Project == nil || ed.Project.Display == nil {
						return ""
					}
					return ed.Project.Display.Mode
				},
				ed.SetProjectDisplayMode,
			),
			psSwitchField(cx, ed, rev,
				"project-settings-display-integer-scale", "Integer Scale", "Display Integer Scale", displayDisabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil && ed.Project.Display.IntegerScale
				},
				ed.SetProjectDisplayIntegerScale,
			),
			psSwitchField(cx, ed, rev,
				"project-settings-display-pixel-snap", "Pixel Snap", "Display Pixel Snap", displayDisabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil && ed.Project.Display.PixelSnap
				},
				ed.SetProjectDisplayPixelSnap,
			),
			psTextField(cx, ed, rev,
				"project-settings-display-bar-color", "Bar Color", "Display Bar Color", "#000000 (default black)", displayDisabled,
				func() string {
					if ed.Project == nil || ed.Project.Display == nil {
						return ""
					}
					return ed.Project.Display.BarColor
				},
				ed.SetProjectDisplayBarColor,
			),
		},
	}
}

func psAutoloadSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	refreshFiles := core.NewSignal(cx, 0)

	filesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%d\x00%d", ed.ProjectPath, ed.filesRev, refreshFiles.Get())
	})
	sceneFiles := core.NewMemoFunc(cx, func() []string {
		_ = filesKey.Get()
		got, err := ed.ProjectSceneFiles()
		if err != nil {
			return nil
		}
		return got
	}, slices.Equal)
	resourceFiles := core.NewMemoFunc(cx, func() []string {
		_ = filesKey.Get()
		got, err := ed.ProjectResourceFiles()
		if err != nil {
			return nil
		}
		return got
	}, slices.Equal)

	// Scene list structure key changes only when project path or scene count changes so row inputs stay mounted while editing.
	scenesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		n := 0
		if ed.Project != nil && ed.Project.Autoload != nil {
			n = len(ed.Project.Autoload.Scenes)
		}
		return fmt.Sprintf("%s\x00%d", ed.ProjectPath, n)
	})
	resourcesKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		n := 0
		if ed.Project != nil && ed.Project.Autoload != nil {
			n = len(ed.Project.Autoload.Resources)
		}
		return fmt.Sprintf("%s\x00%d", ed.ProjectPath, n)
	})

	newSceneName := core.NewSignal(cx, "")
	newScenePath := core.NewSignal(cx, "")
	addSceneErr := core.NewSignal(cx, "")

	newResName := core.NewSignal(cx, "")
	newResPath := core.NewSignal(cx, "")
	addResErr := core.NewSignal(cx, "")

	mainSceneOptions := func() []ui.Option[string] {
		_ = rev.Get()
		opts := []ui.Option[string]{{Value: "", Label: "(None)"}}
		if ed.Project != nil && ed.Project.Autoload != nil {
			for _, s := range ed.Project.Autoload.Scenes {
				opts = append(opts, ui.Option[string]{Value: s.Name, Label: s.Name})
			}
		}
		return opts
	}

	pathOptionsWithCurrent := func(files []string, cur string) []ui.Option[string] {
		opts := make([]ui.Option[string], 0, len(files)+1)
		if cur != "" && !slices.Contains(files, cur) {
			opts = append(opts, ui.Option[string]{Value: cur, Label: cur})
		}
		for _, f := range files {
			opts = append(opts, ui.Option[string]{Value: f, Label: f})
		}
		return opts
	}

	return core.View{
		TestID: "project-settings-section-autoload",
		Style:  core.Style{Gap: 10},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					psSectionHeading(colors, "AUTOLOAD"),
					core.View{Style: core.Style{FlexGrow: 1}},
					ui.Button{
						TestID:  "project-settings-refresh-files",
						Text:    "Refresh Files",
						Size:    ui.SizeSmall,
						Variant: ui.ButtonSecondary,
						OnPress: func() { refreshFiles.Set(refreshFiles.Get() + 1) },
					},
				},
			},
			psSelectField(cx, ed, rev,
				"project-settings-main-scene", "Main Scene", "Main Scene", nil, mainSceneOptions, disabled,
				func() string {
					if ed.Project == nil || ed.Project.Autoload == nil {
						return ""
					}
					return ed.Project.Autoload.MainScene
				},
				ed.SetProjectMainScene,
			),
			psSectionHeading(colors, "SCENES"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = scenesKey.Get()
				if ed.Project == nil || ed.Project.Autoload == nil || len(ed.Project.Autoload.Scenes) == 0 {
					return core.Text{
						TestID:  "project-settings-scenes-empty",
						Content: "No autoload scenes configured.",
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					}
				}
				n := len(ed.Project.Autoload.Scenes)
				rows := make([]core.Node, 0, n)
				for i := range n {
					rows = append(rows, psAutoloadEntryRow(cx, ed, rev, "scene", i, disabled, sceneFiles, pathOptionsWithCurrent,
						func(idx int) (project.Entry, bool) {
							if ed.Project == nil || ed.Project.Autoload == nil || idx >= len(ed.Project.Autoload.Scenes) {
								return project.Entry{}, false
							}
							return ed.Project.Autoload.Scenes[idx], true
						},
						ed.UpdateProjectAutoloadScene,
						ed.RemoveProjectAutoloadScene,
					))
				}
				return core.View{Style: core.Style{Gap: 4}, Children: rows}
			}),
			core.View{
				Style: core.Style{Gap: 2},
				Children: []core.Node{
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.View{
								Style: core.Style{Width: core.Pt(140)},
								Children: []core.Node{
									ui.Input{
										TestID:      "project-settings-add-scene-name",
										Label:       "New Autoload Scene Name",
										Placeholder: "Scene name",
										Disabled:    disabled,
										Invalid:     func() bool { return addSceneErr.Get() != "" },
										Value:       newSceneName.Get,
										OnChange: func(v string) {
											newSceneName.Set(v)
											addSceneErr.Set("")
										},
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, MaxWidth: core.Pt(260)},
								Children: []core.Node{
									ui.Select[string]{
										TestID:      "project-settings-add-scene-path",
										Label:       "New Autoload Scene Path",
										Placeholder: "Pick scene file",
										Searchable:  true,
										Disabled:    disabled,
										Invalid:     func() bool { return addSceneErr.Get() != "" },
										OptionsFunc: func() []ui.Option[string] {
											return pathOptionsWithCurrent(sceneFiles.Get(), newScenePath.Get())
										},
										Value: newScenePath.Get,
										OnChange: func(v string) {
											newScenePath.Set(v)
											addSceneErr.Set("")
										},
									},
								},
							},
							ui.Button{
								TestID:   "project-settings-add-scene",
								Text:     "Add Scene",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonSecondary,
								Disabled: disabled,
								OnPress: func() {
									if err := ed.AddProjectAutoloadScene(newSceneName.Get(), newScenePath.Get()); err != nil {
										addSceneErr.Set(err.Error())
										ed.report(err)
										return
									}
									addSceneErr.Set("")
									newSceneName.Set("")
									newScenePath.Set("")
								},
							},
						},
					},
					psErrorText(colors, "project-settings-add-scene-error", addSceneErr),
				},
			},
			psSectionHeading(colors, "RESOURCES"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = resourcesKey.Get()
				if ed.Project == nil || ed.Project.Autoload == nil || len(ed.Project.Autoload.Resources) == 0 {
					return core.Text{
						TestID:  "project-settings-resources-empty",
						Content: "No autoload resources configured.",
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					}
				}
				n := len(ed.Project.Autoload.Resources)
				rows := make([]core.Node, 0, n)
				for i := range n {
					rows = append(rows, psAutoloadEntryRow(cx, ed, rev, "resource", i, disabled, resourceFiles, pathOptionsWithCurrent,
						func(idx int) (project.Entry, bool) {
							if ed.Project == nil || ed.Project.Autoload == nil || idx >= len(ed.Project.Autoload.Resources) {
								return project.Entry{}, false
							}
							return ed.Project.Autoload.Resources[idx], true
						},
						ed.UpdateProjectAutoloadResource,
						ed.RemoveProjectAutoloadResource,
					))
				}
				return core.View{Style: core.Style{Gap: 4}, Children: rows}
			}),
			core.View{
				Style: core.Style{Gap: 2},
				Children: []core.Node{
					ui.HStack{
						Gap: 6,
						Children: []core.Node{
							core.View{
								Style: core.Style{Width: core.Pt(140)},
								Children: []core.Node{
									ui.Input{
										TestID:      "project-settings-add-resource-name",
										Label:       "New Autoload Resource Name",
										Placeholder: "Resource name",
										Disabled:    disabled,
										Invalid:     func() bool { return addResErr.Get() != "" },
										Value:       newResName.Get,
										OnChange: func(v string) {
											newResName.Set(v)
											addResErr.Set("")
										},
									},
								},
							},
							core.View{
								Style: core.Style{FlexGrow: 1, MaxWidth: core.Pt(260)},
								Children: []core.Node{
									ui.Select[string]{
										TestID:      "project-settings-add-resource-path",
										Label:       "New Autoload Resource Path",
										Placeholder: "Pick project file",
										Searchable:  true,
										Disabled:    disabled,
										Invalid:     func() bool { return addResErr.Get() != "" },
										OptionsFunc: func() []ui.Option[string] {
											return pathOptionsWithCurrent(resourceFiles.Get(), newResPath.Get())
										},
										Value: newResPath.Get,
										OnChange: func(v string) {
											newResPath.Set(v)
											addResErr.Set("")
										},
									},
								},
							},
							ui.Button{
								TestID:   "project-settings-add-resource",
								Text:     "Add Resource",
								Size:     ui.SizeSmall,
								Variant:  ui.ButtonSecondary,
								Disabled: disabled,
								OnPress: func() {
									if err := ed.AddProjectAutoloadResource(newResName.Get(), newResPath.Get()); err != nil {
										addResErr.Set(err.Error())
										ed.report(err)
										return
									}
									addResErr.Set("")
									newResName.Set("")
									newResPath.Set("")
								},
							},
						},
					},
					psErrorText(colors, "project-settings-add-resource-error", addResErr),
				},
			},
		},
	}
}

func psAutoloadEntryRow(
	cx *core.Scope,
	ed *Editor,
	rev *core.Signal[uint64],
	kind string,
	idx int,
	disabled func() bool,
	files *core.Memo[[]string],
	pathOptions func([]string, string) []ui.Option[string],
	read func(int) (project.Entry, bool),
	update func(int, string, string) error,
	remove func(int) error,
) core.Node {
	colors := ui.ColorsOf(cx)
	prefix := fmt.Sprintf("project-settings-%s-%d", kind, idx)

	readName := func() string {
		if e, ok := read(idx); ok {
			return e.Name
		}
		return ""
	}
	readPath := func() string {
		if e, ok := read(idx); ok {
			return e.Path
		}
		return ""
	}

	nameDraft := core.NewSignal(cx, readName())
	nameDirty := core.NewSignal(cx, false)
	rowErr := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ProjectPath + "\x00" + readName() + "\x00" + readPath()
	})
	cx.Effect(func() {
		_ = synced.Get()
		nameDraft.Set(readName())
		nameDirty.Set(false)
		rowErr.Set("")
	})

	commitName := func(text string) {
		nameDirty.Set(false)
		if err := update(idx, text, readPath()); err != nil {
			rowErr.Set(err.Error())
			ed.report(err)
			return
		}
		rowErr.Set("")
		nameDraft.Set(readName())
	}

	return core.View{
		TestID: prefix,
		Style:  core.Style{Gap: 2},
		Children: []core.Node{
			ui.HStack{
				Gap: 6,
				Children: []core.Node{
					core.View{
						Style: core.Style{Width: core.Pt(140)},
						Children: []core.Node{
							ui.Input{
								TestID:   prefix + "-name",
								Label:    fmt.Sprintf("Autoload %s %d Name", kind, idx),
								Disabled: disabled,
								Invalid:  func() bool { return rowErr.Get() != "" },
								Value: func() string {
									_ = rev.Get()
									if nameDirty.Get() || rowErr.Get() != "" {
										return nameDraft.Get()
									}
									return readName()
								},
								OnFocus: func() {
									if !nameDirty.Get() && rowErr.Get() == "" {
										nameDraft.Set(readName())
									}
								},
								OnChange: func(v string) {
									nameDraft.Set(v)
									nameDirty.Set(true)
									rowErr.Set("")
								},
								OnSubmit: func(v string) {
									nameDraft.Set(v)
									commitName(v)
								},
								OnBlur: func() {
									if nameDirty.Get() {
										commitName(nameDraft.Get())
									}
								},
							},
						},
					},
					core.View{
						Style: core.Style{FlexGrow: 1, MaxWidth: core.Pt(260)},
						Children: []core.Node{
							ui.Select[string]{
								TestID:     prefix + "-path",
								Label:      fmt.Sprintf("Autoload %s %d Path", kind, idx),
								Searchable: true,
								Disabled:   disabled,
								Invalid:    func() bool { return rowErr.Get() != "" },
								OptionsFunc: func() []ui.Option[string] {
									_ = rev.Get()
									return pathOptions(files.Get(), readPath())
								},
								Value: func() string {
									_ = rev.Get()
									return readPath()
								},
								OnChange: func(p string) {
									if err := update(idx, readName(), p); err != nil {
										rowErr.Set(err.Error())
										ed.report(err)
										return
									}
									rowErr.Set("")
								},
							},
						},
					},
					ui.Button{
						TestID:   prefix + "-remove",
						Text:     "Remove",
						Label:    fmt.Sprintf("Remove Autoload %s %d", kind, idx),
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: disabled,
						OnPress: func() {
							if err := remove(idx); err != nil {
								rowErr.Set(err.Error())
								ed.report(err)
								return
							}
							rowErr.Set("")
						},
					},
				},
			},
			psErrorText(colors, prefix+"-error", rowErr),
		},
	}
}

func psPluginsSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	registeredKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ProjectPath + "\x00" + strings.Join(ed.RegisteredPlugins(), "\x00")
	})
	missingKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return ed.ProjectPath + "\x00" + strings.Join(ed.MissingPlugins(), "\x00")
	})

	return core.View{
		TestID: "project-settings-section-plugins",
		Style:  core.Style{Gap: 10},
		Children: []core.Node{
			psSectionHeading(colors, "REGISTERED PLUGINS"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = registeredKey.Get()
				names := ed.RegisteredPlugins()
				if len(names) == 0 {
					return core.Text{
						TestID:  "project-settings-plugins-empty",
						Content: "No editor plugins are registered in this binary.",
						Style:   core.Style{FontSize: 11, Color: colors.Muted()},
					}
				}
				rows := make([]core.Node, 0, len(names))
				for _, name := range names {
					rows = append(rows, psRegisteredPluginRow(cx, ed, rev, name, disabled))
				}
				return core.View{Style: core.Style{Gap: 4}, Children: rows}
			}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = missingKey.Get()
				missing := ed.MissingPlugins()
				if len(missing) == 0 {
					return nil
				}
				rows := make([]core.Node, 0, len(missing)+1)
				rows = append(rows, psSectionHeading(colors, "MISSING PLUGINS"))
				for _, name := range missing {
					rows = append(rows, psMissingPluginRow(cx, ed, name, disabled))
				}
				return core.View{
					TestID:   "project-settings-missing-plugins",
					Style:    core.Style{Gap: 4},
					Children: rows,
				}
			}),
		},
	}
}

func psRegisteredPluginRow(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], name string, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	errMsg := core.NewSignal(cx, "")
	summary := pluginSummary(ed.plugin(name))

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%t", ed.ProjectPath, ed.PluginEnabled(name))
	})
	cx.Effect(func() {
		_ = synced.Get()
		errMsg.Set("")
	})

	return core.View{
		TestID: "project-settings-plugin-" + name,
		Style: core.Style{
			Padding:      core.Symmetric(5, 8),
			Gap:          2,
			CornerRadius: 4,
			Background:   colors.Surface(),
			Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
			BorderColor:  colors.Line(),
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, Gap: 1},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text()},
							},
							core.Text{
								Content: summary,
								Style:   core.Style{FontSize: 11, Color: colors.Muted()},
							},
						},
					},
					ui.Switch{
						TestID:   "project-settings-plugin-switch-" + name,
						Label:    "Enable plugin " + name,
						Disabled: disabled,
						Invalid:  func() bool { return errMsg.Get() != "" },
						Checked: func() bool {
							_ = rev.Get()
							return ed.PluginEnabled(name)
						},
						OnChange: func(on bool) {
							if err := ed.SetPluginEnabled(name, on); err != nil {
								errMsg.Set(err.Error())
								ed.report(err)
								return
							}
							errMsg.Set("")
						},
					},
				},
			},
			psErrorText(colors, "project-settings-plugin-switch-"+name+"-error", errMsg),
		},
	}
}

func psMissingPluginRow(cx *core.Scope, ed *Editor, name string, disabled func() bool) core.Node {
	colors := ui.ColorsOf(cx)
	errMsg := core.NewSignal(cx, "")
	return core.View{
		TestID: "project-settings-missing-plugin-" + name,
		Style: core.Style{
			Padding:      core.Symmetric(5, 8),
			Gap:          2,
			CornerRadius: 4,
			Background:   colors.Surface(),
			Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 2},
			BorderColor:  colors.Warn(),
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, Gap: 1},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style:   core.Style{FontSize: 12, FontWeight: 700, Color: colors.Text()},
							},
							core.Text{
								Content: "Listed in game.yaml, not built into this editor binary",
								Style:   core.Style{FontSize: 11, Color: colors.Warn()},
							},
						},
					},
					ui.Button{
						TestID:   "project-settings-missing-plugin-remove-" + name,
						Text:     "Remove",
						Label:    "Remove missing plugin " + name,
						Size:     ui.SizeSmall,
						Variant:  ui.ButtonGhost,
						Disabled: disabled,
						OnPress: func() {
							if err := ed.SetPluginEnabled(name, false); err != nil {
								errMsg.Set(err.Error())
								ed.report(err)
								return
							}
							errMsg.Set("")
						},
					},
				},
			},
			psErrorText(colors, "project-settings-missing-plugin-"+name+"-error", errMsg),
		},
	}
}

func pluginSummary(p *Plugin) string {
	if p == nil {
		return ""
	}
	var parts []string
	if n := len(p.Panels); n > 0 {
		parts = append(parts, pluralCount(n, "panel", "panels"))
	}
	if n := len(p.Inspectors); n > 0 {
		parts = append(parts, pluralCount(n, "inspector", "inspectors"))
	}
	if n := len(p.Tools); n > 0 {
		parts = append(parts, pluralCount(n, "tool", "tools"))
	}
	if len(parts) == 0 {
		return "No UI extensions"
	}
	return strings.Join(parts, ", ")
}

func pluralCount(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}
