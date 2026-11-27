package editor

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
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

// projectSettingsState holds the active sub-section in the Settings → Project page.
type projectSettingsState struct {
	section ProjectSettingsSection
}

// ProjectSettingsOpen reports whether the Settings view is open on the Project page.
func (e *Editor) ProjectSettingsOpen() bool {
	return e != nil && e.settingsOpen && e.settingsPage == SettingsPageProject
}

// SetProjectSettingsOpen opens the Settings view on the Project page, or closes Settings when open is false and the Project page is active.
func (e *Editor) SetProjectSettingsOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		e.OpenSettingsPage(SettingsPageProject)
		return
	}
	if e.ProjectSettingsOpen() {
		e.CloseSettings()
	}
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
	pal := ed.SpecPalette(cx)
	disabled := func() bool {
		_ = rev.Get()
		return ed.Playing() || ed.Project == nil
	}

	section := core.NewMemo(cx, func() ProjectSettingsSection {
		_ = rev.Get()
		return ed.ProjectSettingsSection()
	})

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

	header := core.View{
		Style: core.Style{
			Direction:   core.Row,
			Wrap:        core.Wrap,
			AlignItems:  core.AlignCenter,
			Gap:         8,
			Padding:     core.Edges{Bottom: 12},
			Borders:     core.Edges{Bottom: 1},
			BorderColor: pal.Line,
		},
		Children: []core.Node{
			core.View{
				TestID: "project-settings-sidebar",
				Style:  core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 4},
				Children: []core.Node{
					tabBtn(ProjectSectionGeneral, "Overview"),
					tabBtn(ProjectSectionWindow, "Window"),
					tabBtn(ProjectSectionDisplay, "Display"),
					tabBtn(ProjectSectionPhysics, "Physics"),
					tabBtn(ProjectSectionAutoload, "Autoload"),
					tabBtn(ProjectSectionPlugins, "Plugins"),
				},
			},
			core.View{Style: core.Style{FlexGrow: 1}},
			core.Show{
				When: func() bool {
					_ = rev.Get()
					return ed.ProjectDirty()
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{
							Height:         core.Pt(24),
							Padding:        core.Symmetric(0, 8),
							CornerRadius:   6,
							Background:     pal.Warning.Fill,
							BorderWidth:    1,
							BorderColor:    pal.Warning.Edge,
							AlignItems:     core.AlignCenter,
							JustifyContent: core.JustifyCenter,
						},
						Children: []core.Node{
							core.Text{
								TestID:  "project-settings-dirty",
								Content: "Unsaved changes",
								Style:   core.Style{FontSize: 11, FontWeight: 600, Color: pal.Warning.Fg},
							},
						},
					}
				},
			},
			settingsSpecButton(pal, "project-settings-close", "Close", "Close Project Settings", false, nil, func() {
				ed.SetProjectSettingsOpen(false)
			}),
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
					Padding:      core.Symmetric(8, 12),
					CornerRadius: 8,
					Background:   pal.Warning.Fill,
					Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 2},
					BorderColor:  pal.Warning.Edge,
				},
				Children: []core.Node{
					core.Text{
						Content: "Saving writes game.yaml in canonical YAML form, which does not preserve comments.",
						Style:   core.Style{FontSize: 11.5, Color: pal.Warning.Fg},
					},
				},
			}
		},
	}

	content := core.View{
		TestID: "project-settings-content",
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 4},
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
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Gap:       12,
			Overflow:  core.Hidden,
		},
		Children: []core.Node{
			header,
			canonicalNotice,
			content,
		},
	}
}

func psSectionHeading(pal SpecPalette, title string) core.Node {
	return core.Text{
		Content: title,
		Style: core.Style{
			FontSize:      11,
			FontWeight:    600,
			LetterSpacing: 0.66,
			Color:         pal.TextFaint,
			Padding:       core.Edges{Top: 8, Bottom: 4},
		},
	}
}

func psErrorText(pal SpecPalette, testID string, errMsg *core.Signal[string]) core.Node {
	return core.Show{
		When: func() bool { return errMsg.Get() != "" },
		Then: func(cx *core.Scope) core.Node {
			return core.Text{
				TestID:      testID,
				ContentFunc: errMsg.Get,
				Style:       core.Style{FontSize: 11.5, Color: pal.Problem().Fg},
			}
		},
	}
}

func psFieldLayout(pal SpecPalette, ed *Editor, caption, help string, control core.Node, policy settingsAgentPolicy, errNode core.Node) core.Node {
	return settingsRow(
		pal,
		ed,
		"",
		caption,
		help,
		core.View{
			Style:    core.Style{Width: core.Pt(260)},
			Children: []core.Node{control},
		},
		policy,
		errNode,
	)
}

func psTextField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, help, label, placeholder string, policy settingsAgentPolicy, disabled func() bool, read func() string, apply func(string) error) core.Node {
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

	pal := ed.SpecPalette(cx)
	return psFieldLayout(pal, ed, caption, help, input, policy, psErrorText(pal, testID+"-error", errMsg))
}

func psNumberField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, help, label string, step float64, policy settingsAgentPolicy, disabled func() bool, read func() float64, apply func(float64) error) core.Node {
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

	pal := ed.SpecPalette(cx)
	return psFieldLayout(pal, ed, caption, help, num, policy, psErrorText(pal, testID+"-error", errMsg))
}

func psSwitchField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, help, label string, policy settingsAgentPolicy, disabled func() bool, read func() bool, apply func(bool) error) core.Node {
	errMsg := core.NewSignal(cx, "")

	synced := core.NewMemo(cx, func() string {
		_ = rev.Get()
		return fmt.Sprintf("%s\x00%t", ed.ProjectPath, read())
	})
	cx.Effect(func() {
		_ = synced.Get()
		errMsg.Set("")
	})

	sw := SpecSwitch{
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

	pal := ed.SpecPalette(cx)
	return settingsRow(pal, ed, "", caption, help, sw, policy, psErrorText(pal, testID+"-error", errMsg))
}

func psSelectField(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], testID, caption, help, label string, options []ui.Option[string], optionsFunc func() []ui.Option[string], policy settingsAgentPolicy, disabled func() bool, read func() string, apply func(string) error) core.Node {
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

	pal := ed.SpecPalette(cx)
	return psFieldLayout(pal, ed, caption, help, sel, policy, psErrorText(pal, testID+"-error", errMsg))
}

func psGeneralSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	pal := ed.SpecPalette(cx)
	winErr := core.NewSignal(cx, "")
	mainErr := core.NewSignal(cx, "")
	winSync := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil {
			return ""
		}
		return fmt.Sprintf("%s\x00%g\x00%g", ed.ProjectPath, ed.Project.Window.Width, ed.Project.Window.Height)
	})
	cx.Effect(func() {
		_ = winSync.Get()
		winErr.Set("")
	})
	mainSync := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if ed.Project == nil || ed.Project.Autoload == nil {
			return ed.ProjectPath
		}
		return ed.ProjectPath + "\x00" + ed.Project.Autoload.MainScene
	})
	cx.Effect(func() {
		_ = mainSync.Get()
		mainErr.Set("")
	})

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

	windowControl := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
			Width:      core.Pt(260),
		},
		Children: []core.Node{
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					ui.NumberInput{
						TestID:   "project-settings-overview-window-width",
						Label:    "Project Window Width",
						Step:     1,
						Disabled: disabled,
						Invalid:  func() bool { return winErr.Get() != "" },
						Style:    core.Style{Width: core.Pt(118), MinWidth: core.Pt(0)},
						Value: func() float64 {
							_ = rev.Get()
							if ed.Project == nil {
								return 0
							}
							return float64(ed.Project.Window.Width)
						},
						OnChange: func(v float64) {
							if ed.Project == nil {
								return
							}
							if err := ed.SetProjectWindowSize(float32(v), ed.Project.Window.Height); err != nil {
								winErr.Set(err.Error())
								ed.report(err)
								return
							}
							winErr.Set("")
						},
					},
				},
			},
			core.Text{
				Content: "×",
				Style:   core.Style{FontSize: 12, Color: pal.TextFaint},
			},
			core.View{
				Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(0)},
				Children: []core.Node{
					ui.NumberInput{
						TestID:   "project-settings-overview-window-height",
						Label:    "Project Window Height",
						Step:     1,
						Disabled: disabled,
						Invalid:  func() bool { return winErr.Get() != "" },
						Style:    core.Style{Width: core.Pt(118), MinWidth: core.Pt(0)},
						Value: func() float64 {
							_ = rev.Get()
							if ed.Project == nil {
								return 0
							}
							return float64(ed.Project.Window.Height)
						},
						OnChange: func(v float64) {
							if ed.Project == nil {
								return
							}
							if err := ed.SetProjectWindowSize(ed.Project.Window.Width, float32(v)); err != nil {
								winErr.Set(err.Error())
								ed.report(err)
								return
							}
							winErr.Set("")
						},
					},
				},
			},
		},
	}

	mainSceneControl := core.View{
		Style: core.Style{Width: core.Pt(260)},
		Children: []core.Node{
			ui.Select[string]{
				TestID:      "project-settings-overview-main-scene",
				Label:       "Project Main Scene",
				OptionsFunc: mainSceneOptions,
				Disabled:    disabled,
				Invalid:     func() bool { return mainErr.Get() != "" },
				Value: func() string {
					_ = rev.Get()
					if ed.Project == nil || ed.Project.Autoload == nil {
						return ""
					}
					return ed.Project.Autoload.MainScene
				},
				OnChange: func(v string) {
					if err := ed.SetProjectMainScene(v); err != nil {
						mainErr.Set(err.Error())
						ed.report(err)
						return
					}
					mainErr.Set("")
				},
			},
		},
	}

	return core.View{
		TestID: "project-settings-section-general",
		Style:  core.Style{Gap: 0},
		Children: []core.Node{
			psTextField(cx, ed, rev,
				"project-settings-name", "Name", "Shown in title bars and stores", "Project Name", "My Game", policyAgentMayChange, disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.Name
				},
				ed.SetProjectName,
			),
			psTextField(cx, ed, rev,
				"project-settings-id", "ID", "Bundle and package ID", "Application ID", "dev.example.game", policyYouOnly, disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.ID
				},
				ed.SetProjectID,
			),
			settingsRow(
				pal,
				ed,
				"project-settings-overview-window",
				"Window",
				"Logical size, scaled by whole pixels",
				windowControl,
				policyAgentMayChange,
				psErrorText(pal, "project-settings-overview-window-error", winErr),
			),
			settingsRow(
				pal,
				ed,
				"project-settings-overview-main-scene-row",
				"Main scene",
				"Opened when the game starts",
				mainSceneControl,
				policyAgentMayChange,
				psErrorText(pal, "project-settings-overview-main-scene-error", mainErr),
			),
			psTextField(cx, ed, rev,
				"project-settings-app-version", "App version", "Release version synced to game.yaml and nsgo.yaml", "App Version", "0.1.0", policyAgentMayChange, disabled,
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
	pal := ed.SpecPalette(cx)
	appearanceOpts := []ui.Option[string]{
		{Value: "", Label: "System (default)"},
		{Value: "light", Label: "Light"},
		{Value: "dark", Label: "Dark"},
	}

	return core.View{
		TestID: "project-settings-section-window",
		Style:  core.Style{Gap: 0},
		Children: []core.Node{
			psSectionHeading(pal, "WINDOW"),
			psTextField(cx, ed, rev,
				"project-settings-window-title", "Title", "Window title bar text (defaults to project name)", "Window Title", "Defaults to project name", policyAgentMayChange, disabled,
				func() string {
					if ed.Project == nil {
						return ""
					}
					return ed.Project.Window.Title
				},
				ed.SetProjectWindowTitle,
			),
			psNumberField(cx, ed, rev,
				"project-settings-window-width", "Width", "Logical window width in points", "Window Width", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-height", "Height", "Logical window height in points", "Window Height", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-min-width", "Min width", "Minimum window width when resizable (0 = unset)", "Window Min Width", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-min-height", "Min height", "Minimum window height when resizable (0 = unset)", "Window Min Height", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-max-width", "Max width", "Maximum window width when resizable (0 = unset)", "Window Max Width", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-max-height", "Max height", "Maximum window height when resizable (0 = unset)", "Window Max Height", 1, policyAgentMayChange, disabled,
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
				"project-settings-window-resizable", "Resizable", "Allow players to resize the desktop window", "Window Resizable", policyAgentMayChange, disabled,
				func() bool {
					if ed.Project == nil {
						return true
					}
					return ed.Project.Window.Resizable == nil || *ed.Project.Window.Resizable
				},
				ed.SetProjectWindowResizable,
			),
			psSelectField(cx, ed, rev,
				"project-settings-window-appearance", "Appearance", "Game window color scheme synced to nsgo.yaml", "Window Appearance", appearanceOpts, nil, policyAgentMayChange, disabled,
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
	pal := ed.SpecPalette(cx)
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
		Style:  core.Style{Gap: 0},
		Children: []core.Node{
			psSectionHeading(pal, "DISPLAY"),
			psSwitchField(cx, ed, rev,
				"project-settings-display-enabled", "Virtual display", "Render into a fixed logical resolution scaled to the window", "Enable Virtual Display", policyAgentMayChange, disabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil
				},
				ed.SetProjectDisplayEnabled,
			),
			psNumberField(cx, ed, rev,
				"project-settings-display-width", "Virtual width", "Logical framebuffer width in pixels", "Display Virtual Width", 1, policyAgentMayChange, displayDisabled,
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
				"project-settings-display-height", "Virtual height", "Logical framebuffer height in pixels", "Display Virtual Height", 1, policyAgentMayChange, displayDisabled,
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
				"project-settings-display-mode", "Mode", "How the virtual display fits the host window", "Display Mode", modeOpts, nil, policyAgentMayChange, displayDisabled,
				func() string {
					if ed.Project == nil || ed.Project.Display == nil {
						return ""
					}
					return ed.Project.Display.Mode
				},
				ed.SetProjectDisplayMode,
			),
			psSwitchField(cx, ed, rev,
				"project-settings-display-integer-scale", "Integer scale", "Scale by whole integer multiples for crisp pixel art", "Display Integer Scale", policyAgentMayChange, displayDisabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil && ed.Project.Display.IntegerScale
				},
				ed.SetProjectDisplayIntegerScale,
			),
			psSwitchField(cx, ed, rev,
				"project-settings-display-pixel-snap", "Pixel snap", "Snap rendered positions to whole virtual pixels", "Display Pixel Snap", policyAgentMayChange, displayDisabled,
				func() bool {
					return ed.Project != nil && ed.Project.Display != nil && ed.Project.Display.PixelSnap
				},
				ed.SetProjectDisplayPixelSnap,
			),
			psTextField(cx, ed, rev,
				"project-settings-display-bar-color", "Bar color", "Letterbox/pillarbox fill color in #RRGGBB format", "Display Bar Color", "#000000 (default black)", policyAgentMayChange, displayDisabled,
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
	pal := ed.SpecPalette(cx)
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
		Style:  core.Style{Gap: 12},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
				Children: []core.Node{
					psSectionHeading(pal, "AUTOLOAD"),
					core.View{Style: core.Style{FlexGrow: 1}},
					settingsSpecButton(pal, "project-settings-refresh-files", "Refresh Files", "Refresh project files", false, nil, func() {
						refreshFiles.Set(refreshFiles.Get() + 1)
					}),
				},
			},
			psSelectField(cx, ed, rev,
				"project-settings-main-scene", "Main scene", "Opened when the game starts", "Main Scene", nil, mainSceneOptions, policyAgentMayChange, disabled,
				func() string {
					if ed.Project == nil || ed.Project.Autoload == nil {
						return ""
					}
					return ed.Project.Autoload.MainScene
				},
				ed.SetProjectMainScene,
			),
			psSectionHeading(pal, "SCENES"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = scenesKey.Get()
				if ed.Project == nil || ed.Project.Autoload == nil || len(ed.Project.Autoload.Scenes) == 0 {
					return core.Text{
						TestID:  "project-settings-scenes-empty",
						Content: "No autoload scenes configured.",
						Style:   core.Style{FontSize: 11.5, Color: pal.TextFaint},
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
				return core.View{Style: core.Style{Gap: 8}, Children: rows}
			}),
			core.View{
				Style: core.Style{Gap: 4},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
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
							settingsSpecButton(pal, "project-settings-add-scene", "Add Scene", "Add autoload scene", false, disabled, func() {
								if err := ed.AddProjectAutoloadScene(newSceneName.Get(), newScenePath.Get()); err != nil {
									addSceneErr.Set(err.Error())
									ed.report(err)
									return
								}
								addSceneErr.Set("")
								newSceneName.Set("")
								newScenePath.Set("")
							}),
						},
					},
					psErrorText(pal, "project-settings-add-scene-error", addSceneErr),
				},
			},
			psSectionHeading(pal, "RESOURCES"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = resourcesKey.Get()
				if ed.Project == nil || ed.Project.Autoload == nil || len(ed.Project.Autoload.Resources) == 0 {
					return core.Text{
						TestID:  "project-settings-resources-empty",
						Content: "No autoload resources configured.",
						Style:   core.Style{FontSize: 11.5, Color: pal.TextFaint},
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
				return core.View{Style: core.Style{Gap: 8}, Children: rows}
			}),
			core.View{
				Style: core.Style{Gap: 4},
				Children: []core.Node{
					ui.HStack{
						Gap: 8,
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
							settingsSpecButton(pal, "project-settings-add-resource", "Add Resource", "Add autoload resource", false, disabled, func() {
								if err := ed.AddProjectAutoloadResource(newResName.Get(), newResPath.Get()); err != nil {
									addResErr.Set(err.Error())
									ed.report(err)
									return
								}
								addResErr.Set("")
								newResName.Set("")
								newResPath.Set("")
							}),
						},
					},
					psErrorText(pal, "project-settings-add-resource-error", addResErr),
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
		Style:  core.Style{Gap: 4},
		Children: []core.Node{
			ui.HStack{
				Gap: 8,
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
					settingsSpecButton(ed.SpecPalette(cx), prefix+"-remove", "Remove", fmt.Sprintf("Remove Autoload %s %d", kind, idx), false, disabled, func() {
						if err := remove(idx); err != nil {
							rowErr.Set(err.Error())
							ed.report(err)
							return
						}
						rowErr.Set("")
					}),
				},
			},
			psErrorText(ed.SpecPalette(cx), prefix+"-error", rowErr),
		},
	}
}

func psPluginsSection(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	pal := ed.SpecPalette(cx)
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
		Style:  core.Style{Gap: 8},
		Children: []core.Node{
			psSectionHeading(pal, "REGISTERED PLUGINS"),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = registeredKey.Get()
				names := ed.RegisteredPlugins()
				if len(names) == 0 {
					return core.Text{
						TestID:  "project-settings-plugins-empty",
						Content: "No editor plugins are registered in this binary.",
						Style:   core.Style{FontSize: 11.5, Color: pal.TextFaint},
					}
				}
				rows := make([]core.Node, 0, len(names))
				for _, name := range names {
					rows = append(rows, psRegisteredPluginRow(cx, ed, rev, name, disabled))
				}
				return core.View{Style: core.Style{Gap: 0}, Children: rows}
			}),
			core.Dynamic(func(cx *core.Scope) core.Node {
				_ = missingKey.Get()
				missing := ed.MissingPlugins()
				if len(missing) == 0 {
					return nil
				}
				rows := make([]core.Node, 0, len(missing)+1)
				rows = append(rows, psSectionHeading(pal, "MISSING PLUGINS"))
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
	pal := ed.SpecPalette(cx)
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

	return settingsRow(
		pal,
		ed,
		"project-settings-plugin-"+name,
		name,
		summary,
		SpecSwitch{
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
		policyYouOnly,
		psErrorText(pal, "project-settings-plugin-switch-"+name+"-error", errMsg),
	)
}

func psMissingPluginRow(cx *core.Scope, ed *Editor, name string, disabled func() bool) core.Node {
	pal := ed.SpecPalette(cx)
	errMsg := core.NewSignal(cx, "")
	return core.View{
		TestID: "project-settings-missing-plugin-" + name,
		Style: core.Style{
			Padding:      core.Symmetric(8, 12),
			Gap:          4,
			CornerRadius: 8,
			Background:   pal.Warning.Fill,
			Borders:      core.Edges{Top: 1, Right: 1, Bottom: 1, Left: 2},
			BorderColor:  pal.Warning.Edge,
		},
		Children: []core.Node{
			ui.HStack{
				Gap: 12,
				Children: []core.Node{
					core.View{
						Style: core.Style{FlexGrow: 1, Gap: 2},
						Children: []core.Node{
							core.Text{
								Content: name,
								Style:   core.Style{FontSize: 12.5, FontWeight: 600, Color: pal.TextPrimary},
							},
							core.Text{
								Content: "Listed in game.yaml, not built into this editor binary",
								Style:   core.Style{FontSize: 11.5, Color: pal.Warning.Fg},
							},
						},
					},
					settingsSpecButton(pal, "project-settings-missing-plugin-remove-"+name, "Remove", "Remove missing plugin "+name, false, disabled, func() {
						if err := ed.SetPluginEnabled(name, false); err != nil {
							errMsg.Set(err.Error())
							ed.report(err)
							return
						}
						errMsg.Set("")
					}),
				},
			},
			psErrorText(pal, "project-settings-missing-plugin-"+name+"-error", errMsg),
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
