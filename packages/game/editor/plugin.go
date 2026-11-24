package editor

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Plugin extends the scene editor with panels, component inspectors and viewport tools, as Godot's EditorPlugin does. Go has no portable way to load code at run time, so a plugin is Go code built into the editor binary: the binary registers it with Editor.RegisterPlugin, and a project turns it on by naming it under editor.plugins in its game.yaml (project.Editor). A registered plugin the open project does not name stays off.
//
// A plugin changes the scene only through the editor's methods, such as SetComponentField and MoveObject, or by executing its own Command on Editor.History, so every edit it makes can be undone.
type Plugin struct {
	// Name identifies the plugin in game.yaml; it is unique among the editor's plugins and has no spaces or control characters.
	Name string
	// Panels are docked around the viewport while the plugin is on.
	Panels []Panel
	// Inspectors replace the inspector's generic fields of the components they name.
	Inspectors []Inspector
	// Tools are added to the 2D viewport's tool bar.
	Tools []PluginTool
}

// Dock is where a plugin's Panel sits in the editor window.
type Dock int

const (
	// DockLeft places the panel in a column between the asset browser and the viewport.
	DockLeft Dock = iota
	// DockRight places the panel in a column after the inspector.
	DockRight
	// DockBottom places the panel in a row below the viewport, above the animation panel.
	DockBottom
)

// String returns the dock's name: "left", "right" or "bottom".
func (d Dock) String() string {
	switch d {
	case DockLeft:
		return "left"
	case DockRight:
		return "right"
	case DockBottom:
		return "bottom"
	default:
		return fmt.Sprintf("Dock(%d)", int(d))
	}
}

// Panel is a view a plugin docks in the editor window.
type Panel struct {
	// ID identifies the panel within its plugin; the panel's container has the test ID "plugin-panel-<plugin>-<id>".
	ID string
	// Title is shown above the panel.
	Title string
	// Dock is where the panel sits.
	Dock Dock
	// View builds the panel's content once when the panel appears. It reads rev, which changes on every editor state change, inside reactive functions to follow the editor's state, as the editor's own panels do.
	View func(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node
}

// Inspector draws the inspector's section for one component type in place of its generic fields.
type Inspector struct {
	// Component is the component type name the inspector draws, as it is keyed under an object's components; its container has the test ID "inspector-plugin-<plugin>-<component>".
	Component string
	// View builds the section's controls for the object objectID once when the inspector shows it, below the component's title. It reads rev inside reactive functions to follow the editor's state, and edits the component through Editor.SetComponentField; Editor.ComponentField reads a field with its registered default.
	View func(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], objectID string) core.Node
}

// PluginTool is a 2D viewport tool a plugin adds. While it is the active tool (Editor.SetPluginTool), a press on the 2D viewport calls Press, and Draw draws over the scene in edit mode.
type PluginTool struct {
	// ID identifies the tool within its plugin; its tool bar button has the test ID "tool-<plugin>-<id>".
	ID string
	// Label is the tool's button text and its name in the status bar.
	Label string
	// Press handles a press at canvas coordinates (x, y); Editor.CanvasToWorld converts them to world coordinates. It is not called while playing.
	Press func(ed *Editor, x, y float32)
	// Draw, when set, draws over the scene in canvas coordinates while the tool is active and the editor is not playing.
	Draw func(ed *Editor, d *core.Drawing)
}

// RegisterPlugin makes p available to the editor. It is on while the open project names it under editor.plugins in game.yaml, or after SetPluginEnabled turns it on. RegisterPlugin refuses a name that is empty, has spaces or control characters or is already registered, a panel, inspector or tool without its ID, title, label, component or function, a panel with an unknown dock, and two panels, inspectors or tools of p with the same ID or component. The editor keeps a copy of p's lists.
func (e *Editor) RegisterPlugin(p Plugin) error {
	if err := validatePlugin(&p); err != nil {
		return err
	}
	if e.plugin(p.Name) != nil {
		return fmt.Errorf("editor: plugin %q is already registered", p.Name)
	}
	p.Panels = slices.Clone(p.Panels)
	p.Inspectors = slices.Clone(p.Inspectors)
	p.Tools = slices.Clone(p.Tools)
	e.plugins = append(e.plugins, &p)
	if e.Project != nil && e.Project.Editor != nil && slices.Contains(e.Project.Editor.Plugins, p.Name) {
		e.Status = "Plugin " + p.Name + " is on"
	}
	e.notify()
	return nil
}

func validatePlugin(p *Plugin) error {
	if err := validatePluginName(p.Name); err != nil {
		return err
	}
	where := "editor: plugin " + p.Name
	panels := make(map[string]struct{}, len(p.Panels))
	for i, pn := range p.Panels {
		switch {
		case pn.ID == "":
			return fmt.Errorf("%s: panel %d has no ID", where, i)
		case pn.Title == "":
			return fmt.Errorf("%s: panel %q has no title", where, pn.ID)
		case pn.View == nil:
			return fmt.Errorf("%s: panel %q has no View", where, pn.ID)
		case pn.Dock < DockLeft || pn.Dock > DockBottom:
			return fmt.Errorf("%s: panel %q has unknown dock %v", where, pn.ID, pn.Dock)
		}
		if _, dup := panels[pn.ID]; dup {
			return fmt.Errorf("%s: two panels have ID %q", where, pn.ID)
		}
		panels[pn.ID] = struct{}{}
	}
	inspectors := make(map[string]struct{}, len(p.Inspectors))
	for i, in := range p.Inspectors {
		switch {
		case in.Component == "":
			return fmt.Errorf("%s: inspector %d names no component", where, i)
		case in.View == nil:
			return fmt.Errorf("%s: inspector for %q has no View", where, in.Component)
		}
		if _, dup := inspectors[in.Component]; dup {
			return fmt.Errorf("%s: two inspectors draw component %q", where, in.Component)
		}
		inspectors[in.Component] = struct{}{}
	}
	tools := make(map[string]struct{}, len(p.Tools))
	for i, t := range p.Tools {
		switch {
		case t.ID == "":
			return fmt.Errorf("%s: tool %d has no ID", where, i)
		case t.Label == "":
			return fmt.Errorf("%s: tool %q has no label", where, t.ID)
		case t.Press == nil:
			return fmt.Errorf("%s: tool %q has no Press", where, t.ID)
		}
		if _, dup := tools[t.ID]; dup {
			return fmt.Errorf("%s: two tools have ID %q", where, t.ID)
		}
		tools[t.ID] = struct{}{}
	}
	return nil
}

// validatePluginName applies the rule project.Validate applies to the names under editor.plugins, so every registered plugin can be named there.
func validatePluginName(name string) error {
	if name == "" {
		return errors.New("editor: plugin has no name")
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("editor: plugin name %q must have no spaces or control characters", name)
	}
	return nil
}

// RegisteredPlugins returns the names of the registered plugins in the order they were registered.
func (e *Editor) RegisteredPlugins() []string {
	names := make([]string, len(e.plugins))
	for i, p := range e.plugins {
		names[i] = p.Name
	}
	return names
}

// ActivePlugins returns the names of the plugins that are on: those the open project names under editor.plugins that are registered, in the project's order. Without a project, no plugin is on.
func (e *Editor) ActivePlugins() []string {
	var names []string
	for _, p := range e.activePlugins() {
		names = append(names, p.Name)
	}
	return names
}

// MissingPlugins returns the names the open project lists under editor.plugins that no registered plugin has, in the project's order: plugins this editor binary was built without.
func (e *Editor) MissingPlugins() []string {
	var names []string
	for _, name := range e.projectPlugins() {
		if e.plugin(name) == nil {
			names = append(names, name)
		}
	}
	return names
}

// PluginEnabled reports whether the plugin name is on.
func (e *Editor) PluginEnabled(name string) bool {
	return e.plugin(name) != nil && slices.Contains(e.projectPlugins(), name)
}

// SetPluginEnabled turns the registered plugin name on or off for the open project by adding it to or removing it from editor.plugins in game.yaml, as one undoable step that SaveProject writes. A plugin turned on goes last. It returns ErrNoProject without a project, ErrPlaying while playing, and an error for a name that is not registered when turning it on; turning off a plugin the project names but this editor lacks is allowed.
func (e *Editor) SetPluginEnabled(name string, on bool) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	if on && e.plugin(name) == nil {
		return fmt.Errorf("editor: plugin %q is not registered (registered: %s)", name, strings.Join(e.RegisteredPlugins(), ", "))
	}
	listed := slices.Contains(e.projectPlugins(), name)
	if listed == on {
		return nil
	}
	label := "Turn off plugin " + name
	if on {
		label = "Turn on plugin " + name
	}
	return e.EditProject(label, func(f *project.File) {
		if on {
			if f.Editor == nil {
				f.Editor = &project.Editor{}
			}
			f.Editor.Plugins = append(f.Editor.Plugins, name)
			return
		}
		f.Editor.Plugins = slices.DeleteFunc(f.Editor.Plugins, func(n string) bool { return n == name })
		if len(f.Editor.Plugins) == 0 {
			f.Editor.Plugins = nil
			// An editor section left with nothing in it goes too, so turning a plugin on and off again writes game.yaml back as it was.
			if reflect.DeepEqual(*f.Editor, project.Editor{}) {
				f.Editor = nil
			}
		}
	})
}

// SetPluginTool makes the tool id of the plugin that is on the active viewport tool; Tool becomes ToolPlugin. Choosing another tool with SetTool, or turning the plugin off, ends it.
func (e *Editor) SetPluginTool(plugin, id string) error {
	p := e.activePlugin(plugin)
	if p == nil {
		return fmt.Errorf("editor: plugin %q is not on", plugin)
	}
	i := slices.IndexFunc(p.Tools, func(t PluginTool) bool { return t.ID == id })
	if i < 0 {
		return fmt.Errorf("editor: plugin %q has no tool %q", plugin, id)
	}
	if e.Tool == ToolPlugin && e.pluginTool == (pluginToolRef{plugin, id}) {
		return nil
	}
	e.Tool = ToolPlugin
	e.pluginTool = pluginToolRef{plugin, id}
	e.Status = "Tool: " + p.Tools[i].Label
	e.notify()
	return nil
}

// PluginTool returns the plugin and ID of the active plugin tool, and false when the active tool is not a plugin's.
func (e *Editor) PluginTool() (plugin, id string, ok bool) {
	if e.activePluginTool() == nil {
		return "", "", false
	}
	return e.pluginTool.plugin, e.pluginTool.id, true
}

// ComponentField returns the value of field in the component on objectID: the value the scene sets, or else the field's registered default when a component registry is set (SetComponentRegistry). It returns false when the object does not exist, or when the field is neither set nor registered.
func (e *Editor) ComponentField(objectID, component, field string) (any, bool) {
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return nil, false
	}
	if m, ok := obj.Components[component].(map[string]any); ok {
		if v, ok := m[field]; ok {
			return v, true
		}
	}
	if t, ok := e.components.Lookup(component); ok {
		if f, ok := t.Field(field); ok {
			return f.Default, true
		}
	}
	return nil, false
}

// pluginToolRef names a plugin's tool.
type pluginToolRef struct{ plugin, id string }

func (e *Editor) plugin(name string) *Plugin {
	for _, p := range e.plugins {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func (e *Editor) projectPlugins() []string {
	if e.Project == nil || e.Project.Editor == nil {
		return nil
	}
	return e.Project.Editor.Plugins
}

// activePlugins returns the plugins that are on, in the project's order.
func (e *Editor) activePlugins() []*Plugin {
	var out []*Plugin
	for _, name := range e.projectPlugins() {
		if p := e.plugin(name); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func (e *Editor) activePlugin(name string) *Plugin {
	if !slices.Contains(e.projectPlugins(), name) {
		return nil
	}
	return e.plugin(name)
}

// activePluginTool returns the active plugin tool, or nil when Tool is not ToolPlugin or its plugin is off.
func (e *Editor) activePluginTool() *PluginTool {
	if e.Tool != ToolPlugin {
		return nil
	}
	p := e.activePlugin(e.pluginTool.plugin)
	if p == nil {
		return nil
	}
	for i := range p.Tools {
		if p.Tools[i].ID == e.pluginTool.id {
			return &p.Tools[i]
		}
	}
	return nil
}

// dropStalePluginTool returns to ToolSelect when the active plugin tool's plugin is no longer on.
func (e *Editor) dropStalePluginTool() {
	if e.Tool == ToolPlugin && e.activePluginTool() == nil {
		e.Tool = ToolSelect
		e.pluginTool = pluginToolRef{}
	}
}

// pluginInspector returns the inspector of the first plugin that is on, in the project's order, that draws component, and that plugin's name.
func (e *Editor) pluginInspector(component string) (*Inspector, string) {
	for _, p := range e.activePlugins() {
		for i := range p.Inspectors {
			if p.Inspectors[i].Component == component {
				return &p.Inspectors[i], p.Name
			}
		}
	}
	return nil, ""
}

// toolLabel names the active tool for the status bar.
func (e *Editor) toolLabel() string {
	if t := e.activePluginTool(); t != nil {
		return t.Label
	}
	return e.Tool.String()
}

// dockedPanel is a plugin's panel with the plugin's name.
type dockedPanel struct {
	plugin string
	panel  *Panel
}

// dockedPanels returns the panels of the plugins that are on at dock, in the project's order and each plugin's order.
func (e *Editor) dockedPanels(dock Dock) []dockedPanel {
	var out []dockedPanel
	for _, p := range e.activePlugins() {
		for i := range p.Panels {
			if p.Panels[i].Dock == dock {
				out = append(out, dockedPanel{p.Name, &p.Panels[i]})
			}
		}
	}
	return out
}

// pluginDockView lays out the panels docked at dock: a column for each side dock and a row below the viewport for the bottom dock, or nothing when no plugin that is on docks a panel there. The panels mount again only when the set of panels changes.
func pluginDockView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dock Dock) core.Node {
	key := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		for _, dp := range ed.dockedPanels(dock) {
			b.WriteString(dp.plugin)
			b.WriteByte('/')
			b.WriteString(dp.panel.ID)
			b.WriteByte('|')
		}
		return b.String()
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if key.Get() == "" {
			return nil
		}
		colors := ui.ColorsOf(cx)
		panels := ed.dockedPanels(dock)
		children := make([]core.Node, 0, len(panels))
		for _, dp := range panels {
			children = append(children, pluginPanelView(cx, ed, rev, dp))
		}
		style := core.Style{
			Background:  colors.Inset(),
			BorderColor: colors.Line(),
		}
		switch dock {
		case DockBottom:
			style.Direction = core.Row
			style.Height = core.Pt(180)
			style.Borders = core.Edges{Top: 1}
		case DockLeft:
			style.Width = core.Pt(224)
			style.Borders = core.Edges{Right: 1}
		default:
			style.Width = core.Pt(224)
			style.Borders = core.Edges{Left: 1}
		}
		return core.View{
			TestID:   "plugin-dock-" + dock.String(),
			Style:    style,
			Children: children,
		}
	})
}

// pluginPanelView is one docked panel: its title above its content, which scrolls.
func pluginPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dp dockedPanel) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{
		TestID: "plugin-panel-" + dp.plugin + "-" + dp.panel.ID,
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(10),
			Gap:       8,
		},
		Children: []core.Node{
			core.Text{
				Content: dp.panel.Title,
				Style: core.Style{
					FontSize:   13,
					FontWeight: 700,
					Color:      colors.Text(),
				},
			},
			ui.ScrollView{
				Style:        core.Style{FlexGrow: 1},
				ContentStyle: core.Style{Gap: 6},
				Children:     []core.Node{dp.panel.View(cx, ed, rev)},
			},
		},
	}
}

// pluginToolsView is the tool bar group of the 2D tools of the plugins that are on, or nothing when they have none.
func pluginToolsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool) core.Node {
	key := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		for _, p := range ed.activePlugins() {
			for _, t := range p.Tools {
				b.WriteString(p.Name)
				b.WriteByte('/')
				b.WriteString(t.ID)
				b.WriteByte('|')
			}
		}
		return b.String()
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if key.Get() == "" {
			return nil
		}
		var buttons []core.Node
		for _, p := range ed.activePlugins() {
			for _, t := range p.Tools {
				ref := pluginToolRef{p.Name, t.ID}
				buttons = append(buttons, ui.ToggleButton{
					TestID:   "tool-" + ref.plugin + "-" + ref.id,
					Text:     t.Label,
					Disabled: disabled,
					On: func() bool {
						_ = rev.Get()
						return ed.Tool == ToolPlugin && ed.pluginTool == ref
					},
					OnChange: func(on bool) {
						if on {
							ed.report(ed.SetPluginTool(ref.plugin, ref.id))
						}
					},
				})
			}
		}
		return ui.ToolbarGroup{Label: "Plugin Tools", Children: buttons}
	})
}
