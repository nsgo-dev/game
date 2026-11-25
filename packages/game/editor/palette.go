package editor

import (
	"fmt"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	palettePrefixCommand     = "cmd:"
	palettePrefixPluginTool  = "plugin-tool:"
	palettePrefixPluginPanel = "plugin-panel:"
	palettePrefixFile        = "file:"
)

// CommandPaletteOpen reports whether the Command Palette dialog is currently open.
func (e *Editor) CommandPaletteOpen() bool {
	return e != nil && e.paletteOpen
}

// OpenCommandPalette opens the Command Palette dialog.
func (e *Editor) OpenCommandPalette() {
	e.SetCommandPaletteOpen(true)
}

// CloseCommandPalette closes the Command Palette dialog.
func (e *Editor) CloseCommandPalette() {
	e.SetCommandPaletteOpen(false)
}

// ToggleCommandPalette toggles the Command Palette dialog open or closed.
func (e *Editor) ToggleCommandPalette() {
	if e == nil {
		return
	}
	e.SetCommandPaletteOpen(!e.paletteOpen)
}

// SetCommandPaletteOpen sets whether the Command Palette dialog is open.
func (e *Editor) SetCommandPaletteOpen(open bool) {
	if e == nil || e.paletteOpen == open {
		return
	}
	e.paletteOpen = open
	e.notify()
}

// FocusPluginPanel focuses and highlights the docked panel id of the active plugin.
func (e *Editor) FocusPluginPanel(plugin, id string) error {
	p := e.activePlugin(plugin)
	if p == nil {
		return fmt.Errorf("editor: plugin %q is not on", plugin)
	}
	for i := range p.Panels {
		if p.Panels[i].ID == id {
			e.focusedPluginPanel = panelRef{plugin, id}
			e.SetPanelVisible(PluginPanelKey(plugin, id), true)
			e.Status = "Panel: " + p.Panels[i].Title
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: plugin %q has no panel %q", plugin, id)
}

// FocusedPluginPanel returns the plugin and ID of the last focused plugin panel,
// or "", "", false when none is active.
func (e *Editor) FocusedPluginPanel() (plugin, id string, ok bool) {
	if e == nil || e.focusedPluginPanel.plugin == "" {
		return "", "", false
	}
	p := e.activePlugin(e.focusedPluginPanel.plugin)
	if p == nil {
		return "", "", false
	}
	for i := range p.Panels {
		if p.Panels[i].ID == e.focusedPluginPanel.id {
			return e.focusedPluginPanel.plugin, e.focusedPluginPanel.id, true
		}
	}
	return "", "", false
}

func (e *Editor) dropStaleFocusedPluginPanel() {
	if e.focusedPluginPanel.plugin != "" {
		if _, _, ok := e.FocusedPluginPanel(); !ok {
			e.focusedPluginPanel = panelRef{}
		}
	}
}

// OpenProjectFile opens or selects relPath, a slash-separated path relative to the open project root: scene files open in the scene editor through OpenScene (updating ScenePath and SelectedFile, clearing OnSave and losing the open scene's unsaved edits; CommandPaletteView asks first), game.yaml opens Project Settings and selects the file, and other files are selected in the file browser.
func (e *Editor) OpenProjectFile(relPath string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return nil
	}
	if isScreenFile(relPath) {
		e.selectFile(relPath)
		return e.OpenHUDScreen(relPath)
	}
	if isProjectScene(e.Project, relPath) {
		if e.HasPendingAgentTurn() && relPath != e.ScenePath {
			return e.RequestOpenScene(relPath)
		}
		if err := e.OpenScene(relPath); err != nil {
			e.selectFile(relPath)
			e.notify()
			return err
		}
		e.ScenePath = relPath
		e.selectFile(relPath)
		e.Status = "Opened " + relPath
		e.notify()
		return nil
	}
	e.selectFile(relPath)
	if isMaterialFile(relPath) {
		if err := e.OpenMaterialProjectFile(relPath); err != nil {
			return err
		}
		e.Status = "Opened " + relPath
		e.notify()
		return nil
	}
	if isStatesFile(relPath) {
		if err := e.OpenStatesProjectFile(relPath); err != nil {
			return err
		}
		e.Status = "Opened " + relPath
		e.notify()
		return nil
	}
	if isCutsceneFile(relPath) {
		if err := e.OpenCutsceneProjectFile(relPath); err != nil {
			return err
		}
		e.Status = "Opened " + relPath
		e.notify()
		return nil
	}
	if relPath == project.FileName {
		e.SetProjectSettingsOpen(true)
		e.Status = "Opened " + relPath
		e.notify()
		return nil
	}
	e.Status = "Selected " + relPath
	e.notify()
	return nil
}

// PaletteItems returns all items shown in the Command Palette:
// registered editor commands (built-in, custom, and active plugins'), active plugin tools,
// active plugin panels, and the open project's scenes and files.
func (e *Editor) PaletteItems() []base.CommandItem {
	var files []ProjectFile
	if e.Project != nil && e.ProjectPath != "" {
		if listed, err := e.ProjectFiles(); err == nil {
			files = listed
		}
	}
	return e.buildPaletteItems(files)
}

func (e *Editor) buildPaletteItems(files []ProjectFile) []base.CommandItem {
	cmds := e.Commands()
	items := make([]base.CommandItem, 0, len(cmds)+len(files)+8)

	for _, cmd := range cmds {
		disabled := cmd.Enabled != nil && !cmd.Enabled()
		checked := cmd.Checked != nil && cmd.Checked()
		label := cmd.Label
		if cmd.ID == CmdPlayStop {
			if e.Playing() {
				label = e.T("toolbar.stop")
			} else {
				label = e.T("toolbar.play")
			}
		}
		group := cmd.Category
		if group == "" {
			group = e.T("palette.group.commands")
		}
		items = append(items, base.CommandItem{
			ID:          palettePrefixCommand + cmd.ID,
			Label:       label,
			Description: cmd.Description,
			Keywords:    cmd.Keywords,
			Group:       group,
			Shortcut:    cmd.Shortcut,
			Disabled:    disabled,
			Checked:     checked,
		})
	}

	for _, p := range e.activePlugins() {
		for _, t := range p.Tools {
			items = append(items, base.CommandItem{
				ID:          palettePrefixPluginTool + p.Name + "\x00" + t.ID,
				Label:       e.translatePluginText(p, t.LabelKey, t.Label),
				Description: e.T("palette.pluginToolDesc", l10n.Param("plugin", p.Name)),
				Keywords:    []string{p.Name, t.ID, "tool", "plugin"},
				Group:       e.T("palette.group.pluginTools"),
				Disabled:    e.Playing(),
				Checked:     e.Tool == ToolPlugin && e.pluginTool == (pluginToolRef{p.Name, t.ID}),
			})
		}
	}

	for _, p := range e.activePlugins() {
		for _, pn := range p.Panels {
			items = append(items, base.CommandItem{
				ID:          palettePrefixPluginPanel + p.Name + "\x00" + pn.ID,
				Label:       e.translatePluginText(p, pn.TitleKey, pn.Title),
				Description: e.T("palette.pluginPanelDesc", l10n.Param("plugin", p.Name), l10n.Param("dock", pn.Dock.String())),
				Keywords:    []string{p.Name, pn.ID, pn.Dock.String(), "panel", "plugin"},
				Group:       e.T("palette.group.pluginPanels"),
				Checked:     e.focusedPluginPanel == (panelRef{p.Name, pn.ID}),
			})
		}
	}

	for _, f := range files {
		if f.Dir {
			continue
		}
		group := e.T("palette.group.files")
		desc := e.T("palette.projectFile")
		keywords := []string{path.Base(f.Path), "file", "open"}
		if isProjectScene(e.Project, f.Path) {
			group = e.T("palette.group.scenes")
			desc = e.T("palette.openScene")
			keywords = append(keywords, "scene")
		}
		items = append(items, base.CommandItem{
			ID:          palettePrefixFile + f.Path,
			Label:       f.Path,
			Description: desc,
			Keywords:    keywords,
			Group:       group,
			Disabled:    e.Playing(),
		})
	}

	return items
}

// ExecutePaletteItem runs the item identified by a palette item ID returned by PaletteItems.
func (e *Editor) ExecutePaletteItem(id string) error {
	switch {
	case strings.HasPrefix(id, palettePrefixCommand):
		cmdID := strings.TrimPrefix(id, palettePrefixCommand)
		return e.ExecuteCommand(cmdID)
	case strings.HasPrefix(id, palettePrefixPluginTool):
		rest := strings.TrimPrefix(id, palettePrefixPluginTool)
		plugin, toolID, ok := strings.Cut(rest, "\x00")
		if !ok {
			return fmt.Errorf("editor: invalid plugin tool palette ID %q", id)
		}
		if e.Playing() {
			return ErrPlaying
		}
		e.ensureViewModeForPluginTool(plugin, toolID)
		err := e.SetPluginTool(plugin, toolID)
		if err != nil {
			e.report(err)
		}
		return err
	case strings.HasPrefix(id, palettePrefixPluginPanel):
		rest := strings.TrimPrefix(id, palettePrefixPluginPanel)
		plugin, panelID, ok := strings.Cut(rest, "\x00")
		if !ok {
			return fmt.Errorf("editor: invalid plugin panel palette ID %q", id)
		}
		err := e.FocusPluginPanel(plugin, panelID)
		if err != nil {
			e.report(err)
		}
		return err
	case strings.HasPrefix(id, palettePrefixFile):
		relPath := strings.TrimPrefix(id, palettePrefixFile)
		err := e.OpenProjectFile(relPath)
		if err != nil {
			e.report(err)
		}
		return err
	default:
		return fmt.Errorf("editor: unknown palette item %q", id)
	}
}

// filterPaletteItems ranks exact, prefix, and substring matches via base.FilterCommands
// and appends any additional case-insensitive fuzzy subsequence matches.
func filterPaletteItems(items []base.CommandItem, query string) []base.CommandItem {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return items
	}
	ranked := base.FilterCommands(items, query)
	seen := make(map[string]struct{}, len(ranked))
	for _, it := range ranked {
		seen[it.ID] = struct{}{}
	}
	words := strings.Fields(q)
	for _, it := range items {
		if _, ok := seen[it.ID]; ok {
			continue
		}
		if fuzzyMatchesItem(it, words) {
			ranked = append(ranked, it)
		}
	}
	return ranked
}

func fuzzyMatchesItem(it base.CommandItem, words []string) bool {
	candidates := make([]string, 0, 1+len(it.Keywords))
	candidates = append(candidates, strings.ToLower(it.Label))
	for _, kw := range it.Keywords {
		candidates = append(candidates, strings.ToLower(kw))
	}
	for _, w := range words {
		matched := false
		for _, cand := range candidates {
			if isSubsequence(w, cand) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func isSubsequence(needle, haystack string) bool {
	if needle == "" {
		return true
	}
	nr := []rune(needle)
	ni := 0
	for _, r := range haystack {
		if r == nr[ni] {
			ni++
			if ni == len(nr) {
				return true
			}
		}
	}
	return false
}

// CommandPaletteView renders the editor's Command Palette modal using ui.CommandDialog.
func CommandPaletteView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node {
	query := core.NewSignal(cx, "")
	// Refresh project files only when the project, its file revision, or palette open state changes.
	fileKey := core.NewMemo(cx, func() string {
		_ = rev.Get()
		if !ed.CommandPaletteOpen() || ed.Project == nil || ed.ProjectPath == "" {
			return ""
		}
		return fmt.Sprintf("%s\x00%d", ed.ProjectPath, ed.filesRev)
	})
	projectFiles := core.NewMemoFunc(cx, func() []ProjectFile {
		if fileKey.Get() == "" {
			return nil
		}
		listed, err := ed.ProjectFiles()
		if err != nil {
			return nil
		}
		return listed
	}, func(a, b []ProjectFile) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	})

	onDialogClose := bindDialogFocusRestore(cx, ed, func() bool {
		_ = rev.Get()
		return ed.CommandPaletteOpen()
	})

	return ui.CommandDialog{
		TestID: "editor-command-palette",
		Open: func() bool {
			_ = rev.Get()
			return ed.CommandPaletteOpen()
		},
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open {
				query.Set("")
			}
			ed.SetCommandPaletteOpen(open)
		},
		OnClose: onDialogClose,
		Command: ui.Command{
			TestID:      "editor-command-palette-list",
			Label:       ed.T("palette.label"),
			SearchLabel: ed.T("palette.searchLabel"),
			Placeholder: ed.T("palette.placeholder"),
			Empty:       ed.T("palette.empty"),
			Query: func() *string {
				q := query.Get()
				return &q
			},
			OnQueryChange: func(q string) {
				query.Set(q)
			},
			Filter: filterPaletteItems,
			Items: func() []base.CommandItem {
				_ = rev.Get()
				return ed.buildPaletteItems(projectFiles.Get())
			},
			OnSelect: func(id string) {
				query.Set("")
				ed.CloseCommandPalette()
				_ = ed.ExecutePaletteItem(id)
			},
		},
	}
}
