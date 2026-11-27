package editor

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	palettePrefixCommand     = "cmd:"
	palettePrefixPluginTool  = "plugin-tool:"
	palettePrefixPluginPanel = "plugin-panel:"
	palettePrefixFile        = "file:"
	palettePrefixNode        = "node:"
	palettePrefixSetting     = "setting:"
	palettePrefixAsk         = "ask:"

	maxPaletteRecent = 6
)

// PaletteRecent returns a copy of the most recently executed Command Palette item IDs (newest first).
func (e *Editor) PaletteRecent() []string {
	if e == nil || len(e.paletteRecent) == 0 {
		return nil
	}
	return slices.Clone(e.paletteRecent)
}

func (e *Editor) recordPaletteRecent(id string) {
	if e == nil || id == "" || strings.HasPrefix(id, palettePrefixAsk) {
		return
	}
	out := make([]string, 0, min(len(e.paletteRecent)+1, maxPaletteRecent))
	out = append(out, id)
	for _, existing := range e.paletteRecent {
		if existing == id {
			continue
		}
		out = append(out, existing)
		if len(out) >= maxPaletteRecent {
			break
		}
	}
	e.paletteRecent = out
}

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

	// Scene nodes in the currently open scene (@ scope in Palette.dc.html).
	if doc := e.resolvedDoc(); doc != nil && e.WorkspaceState() == WorkspaceReady {
		nodeGroup := e.T("palette.group.nodes")
		if sceneName := e.SceneBreadcrumbName(); sceneName != "" {
			nodeGroup = e.T("palette.group.nodesScene", l10n.Param("scene", sceneName))
		}
		var walkNodes func(layerName string, objs []sceneformat.ObjectDoc)
		walkNodes = func(layerName string, objs []sceneformat.ObjectDoc) {
			for i := range objs {
				o := &objs[i]
				kind := e.SceneRailObjectKind(o.ID)
				label := o.Name
				if label == "" {
					label = o.ID
				}
				kws := []string{o.ID, kind, layerName, "node", "object"}
				if o.Name != "" && o.Name != o.ID {
					kws = append(kws, o.Name)
				}
				for cn := range o.Components {
					kws = append(kws, cn)
				}
				items = append(items, base.CommandItem{
					ID:          palettePrefixNode + o.ID,
					Label:       label,
					Description: fmt.Sprintf("%s · %s", kind, layerName),
					Keywords:    kws,
					Group:       nodeGroup,
					Disabled:    e.Playing(),
					Checked:     e.IsSelected(o.ID),
				})
				walkNodes(layerName, o.Children)
			}
		}
		for i := range doc.Layers {
			walkNodes(doc.Layers[i].Name, doc.Layers[i].Objects)
		}
	}

	// Settings pages (matching Palette.dc.html SETTINGS group).
	type paletteSettingDef struct {
		page     SettingsPage
		labelKey string
		section  string
		keywords []string
	}
	settingDefs := []paletteSettingDef{
		{SettingsPageGeneral, "settings.nav.general", "Editor", []string{"language", "appearance", "theme", "dark", "light", "autosave"}},
		{SettingsPageViewport, "settings.nav.viewport", "Editor", []string{"grid", "snap", "zoom", "camera", "2d", "3d"}},
		{SettingsPageShortcuts, "settings.nav.shortcuts", "Editor", []string{"keys", "keybindings", "hotkeys"}},
		{SettingsPageModels, "settings.nav.models", "Agent", []string{"ai", "llm", "provider", "api"}},
		{SettingsPageTools, "settings.nav.tools", "Agent", []string{"mcp", "server"}},
		{SettingsPagePermissions, "settings.nav.permissions", "Agent", []string{"allow", "ask", "deny", "rules"}},
		{SettingsPageAutomations, "settings.nav.automations", "Agent", []string{"schedule", "cron", "tasks"}},
		{SettingsPageUsage, "settings.nav.usage", "Agent", []string{"tokens", "budget", "cost"}},
		{SettingsPageBrief, "settings.nav.brief", "Project", []string{"brief.yaml", "goals"}},
		{SettingsPageRules, "settings.nav.rules", "Project", []string{"agents.md", "instructions"}},
		{SettingsPageProject, "settings.nav.project", "Project", []string{"game.yaml", "window", "autoload"}},
		{SettingsPageInput, "settings.nav.input", "Project", []string{"controls", "gamepad", "keys"}},
		{SettingsPageAudio, "settings.nav.audio", "Project", []string{"mixer", "bus", "volume"}},
		{SettingsPageExport, "settings.nav.export", "Project", []string{"build", "release", "presets"}},
	}
	settingsGroup := e.T("palette.group.settings")
	for _, sd := range settingDefs {
		pageLabel := e.T(sd.labelKey)
		if pageLabel == "" || pageLabel == sd.labelKey {
			pageLabel = string(sd.page)
		}
		kws := append([]string{"settings", "preferences", string(sd.page), sd.section}, sd.keywords...)
		items = append(items, base.CommandItem{
			ID:          palettePrefixSetting + string(sd.page),
			Label:       pageLabel,
			Description: fmt.Sprintf("%s › %s", sd.section, pageLabel),
			Keywords:    kws,
			Group:       settingsGroup,
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
		err := e.ExecuteCommand(cmdID)
		if err == nil {
			e.recordPaletteRecent(id)
		}
		return err
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
		} else {
			e.recordPaletteRecent(id)
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
		} else {
			e.recordPaletteRecent(id)
		}
		return err
	case strings.HasPrefix(id, palettePrefixFile):
		relPath := strings.TrimPrefix(id, palettePrefixFile)
		err := e.OpenProjectFile(relPath)
		if err != nil {
			e.report(err)
		} else {
			e.recordPaletteRecent(id)
		}
		return err
	case strings.HasPrefix(id, palettePrefixNode):
		objID := strings.TrimPrefix(id, palettePrefixNode)
		if e.Playing() {
			return ErrPlaying
		}
		if obj, _ := e.FindResolvedObject(objID); obj == nil {
			return fmt.Errorf("editor: scene node %q not found", objID)
		}
		e.SelectObject(objID)
		e.SetRightRailTab(RightTabInspector)
		e.FrameSelection()
		e.recordPaletteRecent(id)
		return nil
	case strings.HasPrefix(id, palettePrefixSetting):
		page := SettingsPage(strings.TrimPrefix(id, palettePrefixSetting))
		if e.Playing() {
			return ErrPlaying
		}
		e.OpenSettingsPage(page)
		e.recordPaletteRecent(id)
		return nil
	case strings.HasPrefix(id, palettePrefixAsk):
		prompt := strings.TrimSpace(strings.TrimPrefix(id, palettePrefixAsk))
		if prompt == "" {
			return nil
		}
		e.SetRightRailTab(RightTabAgent)
		e.SetComposerDraft(prompt)
		e.Status = e.T("palette.askAgentStaged")
		e.notify()
		return nil
	default:
		return fmt.Errorf("editor: unknown palette item %q", id)
	}
}

// filterPaletteItemsWithEditor applies prefix scopes (> commands, @ nodes, # files),
// ranks exact/prefix/substring/fuzzy matches, hides the ask-agent item for empty or
// scoped queries, and appends the ask-agent item at the end of non-scoped results.
func (e *Editor) filterPaletteItemsWithEditor(items []base.CommandItem, query string) []base.CommandItem {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		out := make([]base.CommandItem, 0, len(items))
		for _, it := range items {
			if strings.HasPrefix(it.ID, palettePrefixAsk) {
				continue
			}
			out = append(out, it)
		}
		return out
	}

	scopePrefix := byte(0)
	searchTerm := trimmed
	if trimmed[0] == '>' || trimmed[0] == '@' || trimmed[0] == '#' {
		scopePrefix = trimmed[0]
		searchTerm = strings.TrimSpace(trimmed[1:])
	}

	var askItems []base.CommandItem
	scoped := make([]base.CommandItem, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(it.ID, palettePrefixAsk) {
			if scopePrefix == 0 {
				askItems = append(askItems, it)
			}
			continue
		}
		if scopePrefix == 0 {
			scoped = append(scoped, it)
			continue
		}
		switch scopePrefix {
		case '>':
			if strings.HasPrefix(it.ID, palettePrefixCommand) ||
				strings.HasPrefix(it.ID, palettePrefixPluginTool) ||
				strings.HasPrefix(it.ID, palettePrefixPluginPanel) ||
				strings.HasPrefix(it.ID, palettePrefixSetting) {
				scoped = append(scoped, it)
			}
		case '@':
			if strings.HasPrefix(it.ID, palettePrefixNode) {
				scoped = append(scoped, it)
			}
		case '#':
			if strings.HasPrefix(it.ID, palettePrefixFile) {
				scoped = append(scoped, it)
			}
		}
	}

	ranked := filterPaletteItems(scoped, searchTerm)
	if len(askItems) > 0 {
		ranked = append(ranked, askItems...)
	}
	return ranked
}

// buildPaletteItemsForUI returns the stable item slice indexed by ui.Command without
// reading query.Get() so typing never invalidates base.Command's index. Recent items
// are promoted into the Recent group, and a single stable "Ask the agent" entry reads
// the live query inside its reactive Trailing node.
func (e *Editor) buildPaletteItemsForUI(cx *core.Scope, files []ProjectFile, query *core.Signal[string]) []base.CommandItem {
	items := e.buildPaletteItems(files)
	if e != nil && len(e.paletteRecent) > 0 {
		byID := make(map[string]base.CommandItem, len(items))
		for _, it := range items {
			byID[it.ID] = it
		}
		recentSet := make(map[string]struct{}, len(e.paletteRecent))
		out := make([]base.CommandItem, 0, len(items)+1)
		recentGroup := e.T("palette.group.recent")
		for _, rid := range e.paletteRecent {
			if it, ok := byID[rid]; ok {
				it.Group = recentGroup
				out = append(out, it)
				recentSet[rid] = struct{}{}
			}
		}
		for _, it := range items {
			if _, ok := recentSet[it.ID]; ok {
				continue
			}
			out = append(out, it)
		}
		items = out
	}

	if e != nil && e.AgentMode() != AgentModeNone {
		var trailing core.Node
		if query != nil {
			trailing = core.Text{
				ContentFunc: func() string {
					return strings.TrimSpace(query.Get())
				},
				MaxLines:      1,
				EllipsizeMode: core.EllipsizeTail,
				Style:         core.Style{FontSize: 12, MaxWidth: core.Pt(240), FlexShrink: 1},
				StyleFunc: func() core.Style {
					return core.Style{Color: e.SpecPalette(cx).Agent.Fg}
				},
			}
		}
		items = append(items, base.CommandItem{
			ID:          palettePrefixAsk,
			Label:       e.T("palette.askAgentTitle"),
			Description: e.T("palette.askAgentDesc"),
			Group:       e.T("palette.group.agent"),
			Trailing:    trailing,
			Disabled:    e.Playing(),
		})
	}
	return items
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
			Filter: func(items []base.CommandItem, q string) []base.CommandItem {
				return ed.filterPaletteItemsWithEditor(items, q)
			},
			Items: func() []base.CommandItem {
				_ = rev.Get()
				if !ed.CommandPaletteOpen() {
					return nil
				}
				return ed.buildPaletteItemsForUI(cx, projectFiles.Get(), query)
			},
			OnSelect: func(id string) {
				q := strings.TrimSpace(query.Get())
				query.Set("")
				ed.CloseCommandPalette()
				if id == palettePrefixAsk {
					_ = ed.ExecutePaletteItem(palettePrefixAsk + q)
					return
				}
				_ = ed.ExecutePaletteItem(id)
			},
		},
	}
}
