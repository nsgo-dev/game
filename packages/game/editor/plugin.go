package editor

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/math3d"
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
	// Tools are added to the 2D and/or 3D viewport's tool bar according to which hooks they set.
	Tools []PluginTool
	// Gizmos3D register 3D viewport handles and drag editors per component type while the plugin is on.
	Gizmos3D []ComponentGizmo3D
	// Overlays are custom 2D viewport overlays added to the Overlays popover and editormcp's viewport_image tool while the plugin is on.
	Overlays []ViewportOverlay
	// Commands are added to the editor's command registry, desktop menu bar, command palette, and keyboard shortcuts while the plugin is on.
	Commands []PluginCommand
	// Catalog optionally supplies localized strings for this plugin (for example English and Simplified Chinese) resolved in the editor's active language via Editor.PluginT, Editor.PluginPlural, and panel/tool/command TitleKey/LabelKey fields.
	Catalog *l10n.Catalog
	// LocaleYAML optionally supplies versioned l10n YAML string table documents (such as embedded locales/*.yaml files) merged into the plugin's localization catalog at RegisterPlugin time.
	LocaleYAML [][]byte

	catalog *l10n.Catalog
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
	// ID identifies the panel within its plugin and has no control characters. The panel's container has the test ID "plugin-panel-<plugin>-<id>"; as the parts are joined with '-', a plugin whose name or IDs contain '-' can share a test ID with another, so name plugins that are used together so that their test IDs differ.
	ID string
	// Title is shown above the panel.
	Title string
	// TitleKey is an optional localization key in the plugin's catalog used in place of Title when present.
	TitleKey string
	// Dock is where the panel sits.
	Dock Dock
	// View builds the panel's content once when the panel appears; the panel stays mounted, keeping its scope and state, while its plugin is on, whatever other panels come and go in its dock. It reads rev, which changes on every editor state change, inside reactive functions to follow the editor's state, as the editor's own panels do.
	View func(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) core.Node
}

// Inspector draws the inspector's section for one component type in place of its generic fields.
type Inspector struct {
	// Component is the component type name the inspector draws, as it is keyed under an object's components, with no control characters; its container has the test ID "inspector-plugin-<plugin>-<component>", which can collide as Panel.ID's can.
	Component string
	// View builds the section's controls for the object objectID once when the inspector shows it, below the component's title. It reads rev inside reactive functions to follow the editor's state, and edits the component through Editor.SetComponentField; Editor.ComponentField reads a field with its registered default. As the generic fields do, its controls should be disabled while Editor.Playing reports true, and an edit the editor refuses should be shown with Editor.Report rather than dropped.
	View func(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], objectID string) core.Node
}

// PluginTool is a 2D and/or 3D viewport tool a plugin adds. While it is the active tool (Editor.SetPluginTool),
// in 2D a press calls Press, drags call OnDragBegin/OnDragMove/OnDragEnd/OnDragCancel, pointer motion calls OnHover,
// and Draw draws over the scene in edit mode; in 3D a press calls Press3D with the camera's pick ray, drags call
// OnDragBegin3D/OnDragMove3D/OnDragEnd3D/OnDragCancel3D, pointer motion calls OnHover3D, and Draw3D draws over the
// 3D view with the camera's Projection3D.
type PluginTool struct {
	// ID identifies the tool within its plugin and has no control characters; its tool bar button has the test ID "tool-<plugin>-<id>", which can collide as Panel.ID's can.
	ID string
	// Label is the tool's button text and its name in the status bar.
	Label string
	// LabelKey is an optional localization key in the plugin's catalog used in place of Label when present.
	LabelKey string
	// Press, when set, handles a press at canvas coordinates (x, y), which go through the editor's 2D view (View2D) rather than the scene camera: Editor.CanvasToWorld converts them to world coordinates for a world-space layer, and Editor.CanvasToScreen to the scene's screen coordinates for a screen-space layer. It is not called while playing. At least one of Press, OnDragBegin, Press3D, or OnDragBegin3D must be set.
	Press func(ed *Editor, x, y float32)
	// Draw, when set, draws over the scene in canvas coordinates while the tool is active and the editor is not playing. Canvas coordinates follow the 2D view: Editor.WorldToCanvas places a world point and Editor.ScreenToCanvas a screen-space layer's point.
	Draw func(ed *Editor, d *core.Drawing)
	// OnDragBegin, when set, is called when a primary drag begins at canvas coordinates (x, y) while the tool is active in 2D edit mode. Returning true claims the drag so subsequent motion calls OnDragMove and release calls OnDragEnd (or OnDragCancel if cancelled).
	OnDragBegin func(ed *Editor, x, y float32) bool
	// OnDragMove, when set, is called as a claimed drag moves to canvas coordinates (x, y).
	OnDragMove func(ed *Editor, x, y float32)
	// OnDragEnd, when set, is called when a claimed drag finishes at canvas coordinates (x, y).
	OnDragEnd func(ed *Editor, x, y float32)
	// OnDragCancel, when set, is called when a claimed drag is cancelled by Escape, a cancelled gesture, a second finger, Play, Undo/Redo, Save, or changing tools, view mode or scene.
	OnDragCancel func(ed *Editor)
	// OnHover, when set, is called as the pointer moves across the 2D viewport at canvas coordinates (x, y) while the tool is active in edit mode, and with active=false when the pointer leaves the viewport or the tool becomes inactive.
	OnHover func(ed *Editor, x, y float32, active bool)
	// Press3D, when set, handles a press in the 3D viewport with the world-space pick ray from the editor's 3D camera. Not called while playing.
	Press3D func(ed *Editor, ray math3d.Ray)
	// Draw3D, when set, draws over the 3D viewport while the tool is active in 3D edit mode using proj's camera projection and helpers.
	Draw3D func(ed *Editor, d *core.Drawing, proj Projection3D)
	// OnDragBegin3D, when set, is called when a primary drag begins in the 3D viewport with the pick ray. Returning true claims the drag so subsequent motion calls OnDragMove3D and release calls OnDragEnd3D (or OnDragCancel3D if cancelled), instead of orbiting the camera.
	OnDragBegin3D func(ed *Editor, ray math3d.Ray) bool
	// OnDragMove3D, when set, is called as a claimed 3D drag moves with the updated pick ray.
	OnDragMove3D func(ed *Editor, ray math3d.Ray)
	// OnDragEnd3D, when set, is called when a claimed 3D drag finishes with the final pick ray.
	OnDragEnd3D func(ed *Editor, ray math3d.Ray)
	// OnDragCancel3D, when set, is called when a claimed 3D drag is cancelled by Escape, a cancelled gesture, a second finger, Play, Undo/Redo, Save, or changing tools, view mode or scene.
	OnDragCancel3D func(ed *Editor)
	// OnHover3D, when set, is called as the pointer moves across the 3D viewport with the pick ray while the tool is active in 3D edit mode, and with active=false when the pointer leaves the viewport or the tool becomes inactive.
	OnHover3D func(ed *Editor, ray math3d.Ray, active bool)
}

func (t PluginTool) supports2D() bool {
	return t.Press != nil || t.OnDragBegin != nil || t.Draw != nil || t.OnHover != nil
}

func (t PluginTool) supports3D() bool {
	return t.Press3D != nil || t.OnDragBegin3D != nil || t.Draw3D != nil || t.OnHover3D != nil
}

// PluginCommand is a command a plugin adds to the editor's command registry, command palette,
// desktop menu bar, and optional keyboard shortcut.
type PluginCommand struct {
	// ID identifies the command within its plugin (registered globally as "plugin.<plugin>.<id>") and has no spaces or control characters.
	ID string
	// Label is the display name shown in the command palette and menu bar.
	Label string
	// LabelKey is an optional localization key in the plugin's catalog used in place of Label when present.
	LabelKey string
	// Description is optional explanatory text shown in the command palette.
	Description string
	// DescriptionKey is an optional localization key in the plugin's catalog used in place of Description when present.
	DescriptionKey string
	// Category groups the command in the command palette (defaults to the plugin's name when empty).
	Category string
	// CategoryKey is an optional localization key in the plugin's catalog used in place of Category when present.
	CategoryKey string
	// Keywords are extra search terms matched by the command palette.
	Keywords []string
	// Scope names the keyboard scope for shortcut conflict checks (for example ScopeGlobal, ScopeViewport, ScopeTile, ScopeViewport3D). Empty defaults to ScopeGlobal.
	Scope string
	// Shortcut is an optional NSGo key shortcut (such as "mod+shift+h" or "alt+h"). Clashing with any built-in, custom, or other registered plugin shortcut is refused at RegisterPlugin time.
	Shortcut string
	// Enabled, when set, reports whether the command can run right now; nil means always enabled.
	Enabled func(ed *Editor) bool
	// Run executes the command.
	Run func(ed *Editor) error
}

// RegisterPlugin makes p available to the editor. It is on while the open project names it under editor.plugins in game.yaml, or after SetPluginEnabled turns it on. RegisterPlugin refuses a name that is empty, has spaces or control characters or is already registered, a panel, inspector, tool or command without its ID, title, label, component or function, a panel or tool ID or component name with control characters, a command ID with spaces or control characters, a panel with an unknown dock, two panels, inspectors, tools or commands of p with the same ID or component, and any command whose shortcut is invalid or clashes with an already-registered shortcut. The editor keeps a copy of p's lists.
func (e *Editor) RegisterPlugin(p Plugin) error {
	if err := validatePlugin(&p); err != nil {
		return err
	}
	if e.plugin(p.Name) != nil {
		return fmt.Errorf("editor: plugin %q is already registered", p.Name)
	}
	ownersAll := e.registeredShortcutOwners()
	localAll := make(map[string]string)
	localByScope := make(map[string]map[string]string)
	clonedCmds := make([]PluginCommand, len(p.Commands))
	for i, c := range p.Commands {
		fullID := pluginCommandID(p.Name, c.ID)
		if e.hasRegisteredCommandID(fullID) {
			return fmt.Errorf("editor: plugin %s: command %q is already registered", p.Name, fullID)
		}
		norm, err := NormalizeShortcut(c.Shortcut)
		if err != nil {
			return fmt.Errorf("editor: plugin %s: command %q: %w", p.Name, c.ID, err)
		}
		explicitScope := strings.TrimSpace(c.Scope) != ""
		scope := normalizeCommandScope(c.Scope)
		owners := ownersAll
		if explicitScope {
			owners = e.registeredShortcutOwnersInScope(scope)
		}
		if localByScope[scope] == nil {
			localByScope[scope] = make(map[string]string)
		}
		for _, ck := range shortcutCollisionKeys(norm) {
			if existing, clash := owners[ck]; clash {
				return fmt.Errorf("editor: plugin %s: command %q shortcut %q clashes with command %q", p.Name, c.ID, norm, existing)
			}
			if explicitScope {
				if prev, clash := localByScope[scope][ck]; clash {
					return fmt.Errorf("editor: plugin %s: command %q shortcut %q clashes with command %q", p.Name, c.ID, norm, prev)
				}
			} else if prev, clash := localAll[ck]; clash {
				return fmt.Errorf("editor: plugin %s: command %q shortcut %q clashes with command %q", p.Name, c.ID, norm, prev)
			}
			localAll[ck] = fullID
			localByScope[scope][ck] = fullID
		}
		c.Scope = scope
		c.Shortcut = norm
		c.Keywords = slices.Clone(c.Keywords)
		clonedCmds[i] = c
	}
	p.Panels = slices.Clone(p.Panels)
	p.Inspectors = slices.Clone(p.Inspectors)
	p.Tools = slices.Clone(p.Tools)
	p.Gizmos3D = slices.Clone(p.Gizmos3D)
	e.initBuiltinOverlays()
	for _, o := range p.Overlays {
		if e.hasRegisteredOverlayID(o.ID) {
			return fmt.Errorf("editor: plugin %s: overlay %q is already registered", p.Name, o.ID)
		}
	}
	p.Overlays = slices.Clone(p.Overlays)
	p.Commands = clonedCmds
	cat, err := buildPluginCatalog(&p)
	if err != nil {
		return err
	}
	p.catalog = cat
	e.plugins = append(e.plugins, &p)
	if e.Project != nil && e.Project.Editor != nil && slices.Contains(e.Project.Editor.Plugins, p.Name) {
		e.Status = "Plugin " + p.Name + " is on"
	}
	e.notify()
	return nil
}

func buildPluginCatalog(p *Plugin) (*l10n.Catalog, error) {
	if p.Catalog == nil && len(p.LocaleYAML) == 0 {
		return nil, nil
	}
	fallback := "en"
	if p.Catalog != nil && p.Catalog.FallbackLocale() != "" {
		fallback = p.Catalog.FallbackLocale()
	}
	cat := l10n.NewCatalog(fallback)
	if p.Catalog != nil {
		for _, loc := range p.Catalog.Locales() {
			if tbl, ok := p.Catalog.StringTable(loc); ok {
				if err := cat.ApplyStringTable(tbl); err != nil {
					return nil, fmt.Errorf("editor: plugin %s: catalog locale %q: %w", p.Name, loc, err)
				}
			}
		}
	}
	for i, data := range p.LocaleYAML {
		if err := cat.LoadYAML(data); err != nil {
			return nil, fmt.Errorf("editor: plugin %s: locale YAML %d: %w", p.Name, i, err)
		}
	}
	return cat, nil
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
		case hasControl(pn.ID):
			return fmt.Errorf("%s: panel ID %q has control characters", where, pn.ID)
		case pn.Title == "" && pn.TitleKey == "":
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
		case hasControl(in.Component):
			return fmt.Errorf("%s: inspector component %q has control characters", where, in.Component)
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
		case hasControl(t.ID):
			return fmt.Errorf("%s: tool ID %q has control characters", where, t.ID)
		case t.Label == "" && t.LabelKey == "":
			return fmt.Errorf("%s: tool %q has no label", where, t.ID)
		case t.Press == nil && t.OnDragBegin == nil && t.Press3D == nil && t.OnDragBegin3D == nil:
			return fmt.Errorf("%s: tool %q has no Press, OnDragBegin, Press3D or OnDragBegin3D", where, t.ID)
		}
		if _, dup := tools[t.ID]; dup {
			return fmt.Errorf("%s: two tools have ID %q", where, t.ID)
		}
		tools[t.ID] = struct{}{}
	}
	gizmos3D := make(map[string]struct{}, len(p.Gizmos3D))
	for i, g := range p.Gizmos3D {
		switch {
		case g.Component == "":
			return fmt.Errorf("%s: 3D gizmo %d names no component", where, i)
		case hasControl(g.Component):
			return fmt.Errorf("%s: 3D gizmo component %q has control characters", where, g.Component)
		case g.Handles == nil:
			return fmt.Errorf("%s: 3D gizmo for %q has no Handles", where, g.Component)
		}
		if _, dup := gizmos3D[g.Component]; dup {
			return fmt.Errorf("%s: two 3D gizmos handle component %q", where, g.Component)
		}
		gizmos3D[g.Component] = struct{}{}
	}
	overlays := make(map[string]struct{}, len(p.Overlays))
	for i := range p.Overlays {
		o := &p.Overlays[i]
		if err := validateViewportOverlay(o, where); err != nil {
			return err
		}
		if _, dup := overlays[o.ID]; dup {
			return fmt.Errorf("%s: two overlays have ID %q", where, o.ID)
		}
		overlays[o.ID] = struct{}{}
	}
	cmds := make(map[string]struct{}, len(p.Commands))
	for i, c := range p.Commands {
		switch {
		case c.ID == "":
			return fmt.Errorf("%s: command %d has no ID", where, i)
		case strings.IndexFunc(c.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
			return fmt.Errorf("%s: command ID %q must have no spaces or control characters", where, c.ID)
		case strings.TrimSpace(c.Label) == "" && strings.TrimSpace(c.LabelKey) == "":
			return fmt.Errorf("%s: command %q has no label", where, c.ID)
		case c.Run == nil:
			return fmt.Errorf("%s: command %q has no Run", where, c.ID)
		}
		if _, dup := cmds[c.ID]; dup {
			return fmt.Errorf("%s: two commands have ID %q", where, c.ID)
		}
		cmds[c.ID] = struct{}{}
	}
	return nil
}

// hasControl reports whether s has a control character, which the memo keys of the plugin views use as their separator.
func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
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

// SetPluginEnabled turns the registered plugin name on or off for the open project by adding it to or removing it from editor.plugins in game.yaml, as one undoable step that SaveProject writes. A plugin turned on goes last; turning off the last one removes the editor section when nothing else is left in it, including a section the author wrote, and Undo puts it back. It returns ErrNoProject without a project, ErrPlaying while playing, and an error for a name that is not registered when turning it on; turning off a plugin the project names but this editor lacks is allowed.
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
			// An editor section left with nothing in it goes too, so game.yaml has no empty editor: {} after the last plugin is turned off.
			if reflect.DeepEqual(*f.Editor, project.Editor{}) {
				f.Editor = nil
			}
		}
	})
}

// SetPluginTool makes the tool id of the plugin that is on the active viewport tool; Tool becomes ToolPlugin. Choosing another tool with SetTool, or turning the plugin off, ends it.
func (e *Editor) SetPluginTool(plugin, id string) error {
	if e == nil {
		return fmt.Errorf("editor: nil editor")
	}
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
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.ClearPluginHover2D()
	e.CancelGizmoDrag3D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.ClearPluginHover3D()
	e.CancelCameraDrag()
	e.endTileStrokeForChange()
	e.endGridMapStrokeForChange()
	e.gridMap.tool = GridMapToolNone
	e.Tool = ToolPlugin
	e.pluginTool = pluginToolRef{plugin, id}
	e.Status = "Tool: " + p.Tools[i].Label
	e.notify()
	return nil
}

// BeginPluginDrag3D starts a drag on the active 3D plugin tool at viewport pixel (sx, sy), returning true if the tool claimed the drag via OnDragBegin3D.
func (e *Editor) BeginPluginDrag3D(sx, sy, w, h float32) bool {
	if e == nil || e.playing || e.ViewMode != ViewMode3D || e.Tool != ToolPlugin || e.pluginDrag3D || w <= 0 || h <= 0 || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return false
	}
	t := e.activePluginTool()
	if t == nil || t.OnDragBegin3D == nil {
		return false
	}
	ray := e.PickRay3D(sx, sy, w, h)
	if !t.OnDragBegin3D(e, ray) {
		return false
	}
	e.pluginDrag3D = true
	e.pluginDrag3DMoved = false
	e.pluginDrag3DRef = e.pluginTool
	e.pluginDrag3DLast = math2d.Vec2{X: sx, Y: sy}
	e.pluginDrag3DW = w
	e.pluginDrag3DH = h
	e.drag3D = drag3DState{
		kind:    drag3DPlugin,
		startSX: sx,
		startSY: sy,
		lastSX:  sx,
		lastSY:  sy,
		vpW:     w,
		vpH:     h,
	}
	e.notify()
	return true
}

// UpdatePluginDrag3D delivers drag movement at viewport pixel (sx, sy) to the active plugin tool's OnDragMove3D callback as a camera pick ray.
func (e *Editor) UpdatePluginDrag3D(sx, sy float32) {
	if e == nil || !e.pluginDrag3D || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	if e.playing || e.ViewMode != ViewMode3D || e.Tool != ToolPlugin || e.pluginTool != e.pluginDrag3DRef {
		e.CancelPluginDrag3D()
		return
	}
	pos := math2d.Vec2{X: sx, Y: sy}
	if e.pluginDrag3DMoved && pos == e.pluginDrag3DLast {
		return
	}
	e.pluginDrag3DMoved = true
	e.pluginDrag3DLast = pos
	if e.drag3D.kind == drag3DPlugin {
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
	}
	if t := e.activePluginTool(); t != nil && t.OnDragMove3D != nil {
		t.OnDragMove3D(e, e.PickRay3D(sx, sy, e.pluginDrag3DW, e.pluginDrag3DH))
	}
}

// EndPluginDrag3D finishes an active 3D plugin tool drag at viewport pixel (sx, sy), calling OnDragEnd3D with the final pick ray.
func (e *Editor) EndPluginDrag3D(sx, sy float32) bool {
	if e == nil || !e.pluginDrag3D {
		return false
	}
	ref := e.pluginDrag3DRef
	w, h := e.pluginDrag3DW, e.pluginDrag3DH
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		sx, sy = e.pluginDrag3DLast.X, e.pluginDrag3DLast.Y
	}
	e.pluginDrag3D = false
	e.pluginDrag3DMoved = false
	e.pluginDrag3DRef = pluginToolRef{}
	if e.drag3D.kind == drag3DPlugin {
		e.drag3D = drag3DState{}
	}
	if t := e.pluginToolByRef(ref); t != nil && t.OnDragEnd3D != nil && isFiniteFloat32(sx) && isFiniteFloat32(sy) {
		t.OnDragEnd3D(e, e.PickRay3D(sx, sy, w, h))
	}
	e.notify()
	return true
}

// CancelPluginDrag3D aborts an active 3D plugin tool drag, calling OnDragCancel3D when set.
func (e *Editor) CancelPluginDrag3D() bool {
	if e == nil || !e.pluginDrag3D {
		return false
	}
	ref := e.pluginDrag3DRef
	e.pluginDrag3D = false
	e.pluginDrag3DMoved = false
	e.pluginDrag3DRef = pluginToolRef{}
	if e.drag3D.kind == drag3DPlugin {
		e.drag3D = drag3DState{}
	}
	if t := e.pluginToolByRef(ref); t != nil && t.OnDragCancel3D != nil {
		t.OnDragCancel3D(e)
	}
	e.notify()
	return true
}

// PluginDragging3D reports whether a plugin tool currently owns a 3D viewport drag.
func (e *Editor) PluginDragging3D() bool {
	return e != nil && e.pluginDrag3D
}

// SetPluginHover3D notifies the active plugin tool's OnHover3D callback of pointer motion at viewport pixel (sx, sy) in 3D edit mode.
func (e *Editor) SetPluginHover3D(sx, sy, w, h float32) {
	if e == nil || e.playing || e.ViewMode != ViewMode3D || e.Tool != ToolPlugin || w <= 0 || h <= 0 || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	t := e.activePluginTool()
	if t == nil || t.OnHover3D == nil {
		return
	}
	ray := e.PickRay3D(sx, sy, w, h)
	e.pluginHover3DActive = true
	e.pluginHover3DRef = e.pluginTool
	e.pluginHover3DRay = ray
	t.OnHover3D(e, ray, true)
}

// ClearPluginHover3D notifies the hovered 3D plugin tool's OnHover3D callback with active=false when the pointer leaves the viewport or the tool changes.
func (e *Editor) ClearPluginHover3D() {
	if e == nil || !e.pluginHover3DActive {
		return
	}
	ref := e.pluginHover3DRef
	ray := e.pluginHover3DRay
	e.pluginHover3DActive = false
	e.pluginHover3DRef = pluginToolRef{}
	if t := e.pluginToolByRef(ref); t != nil && t.OnHover3D != nil {
		t.OnHover3D(e, ray, false)
	}
}

// BeginPluginDrag2D starts a drag on the active plugin tool at canvas coordinates (x, y), returning true if the tool claimed the drag via OnDragBegin.
func (e *Editor) BeginPluginDrag2D(x, y float32) bool {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || e.Tool != ToolPlugin || e.pluginDrag || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return false
	}
	t := e.activePluginTool()
	if t == nil || t.OnDragBegin == nil {
		return false
	}
	if !t.OnDragBegin(e, x, y) {
		return false
	}
	e.pluginDrag = true
	e.pluginDragMoved = false
	e.pluginDragRef = e.pluginTool
	e.pluginDragLast = math2d.Vec2{X: x, Y: y}
	e.notify()
	return true
}

// UpdatePluginDrag2D delivers drag movement at canvas coordinates (x, y) to the active plugin tool's OnDragMove callback.
func (e *Editor) UpdatePluginDrag2D(x, y float32) {
	if e == nil || !e.pluginDrag || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	if e.playing || e.ViewMode != ViewMode2D || e.Tool != ToolPlugin || e.pluginTool != e.pluginDragRef {
		e.CancelPluginDrag2D()
		return
	}
	pos := math2d.Vec2{X: x, Y: y}
	if e.pluginDragMoved && pos == e.pluginDragLast {
		return
	}
	e.pluginDragMoved = true
	e.pluginDragLast = pos
	if t := e.activePluginTool(); t != nil && t.OnDragMove != nil {
		t.OnDragMove(e, x, y)
	}
}

// EndPluginDrag2D finishes an active plugin tool drag at canvas coordinates (x, y), calling OnDragEnd.
func (e *Editor) EndPluginDrag2D(x, y float32) bool {
	if e == nil || !e.pluginDrag {
		return false
	}
	ref := e.pluginDragRef
	e.pluginDrag = false
	e.pluginDragMoved = false
	e.pluginDragRef = pluginToolRef{}
	if t := e.pluginToolByRef(ref); t != nil && t.OnDragEnd != nil {
		t.OnDragEnd(e, x, y)
	}
	e.notify()
	return true
}

// CancelPluginDrag2D aborts an active plugin tool drag, calling OnDragCancel when set.
func (e *Editor) CancelPluginDrag2D() bool {
	if e == nil || !e.pluginDrag {
		return false
	}
	ref := e.pluginDragRef
	e.pluginDrag = false
	e.pluginDragMoved = false
	e.pluginDragRef = pluginToolRef{}
	if t := e.pluginToolByRef(ref); t != nil && t.OnDragCancel != nil {
		t.OnDragCancel(e)
	}
	e.notify()
	return true
}

// PluginDragging2D reports whether a plugin tool currently owns a 2D viewport drag.
func (e *Editor) PluginDragging2D() bool {
	return e != nil && e.pluginDrag
}

// SetPluginHover2D notifies the active plugin tool's OnHover callback of pointer motion at canvas coordinates (x, y) in 2D edit mode.
func (e *Editor) SetPluginHover2D(x, y float32) {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || e.Tool != ToolPlugin || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	t := e.activePluginTool()
	if t == nil || t.OnHover == nil {
		return
	}
	e.pluginHoverActive = true
	e.pluginHoverRef = e.pluginTool
	e.pluginHoverPos = math2d.Vec2{X: x, Y: y}
	t.OnHover(e, x, y, true)
}

// ClearPluginHover2D notifies the hovered plugin tool's OnHover callback with active=false when the pointer leaves the viewport or the tool changes.
func (e *Editor) ClearPluginHover2D() {
	if e == nil || !e.pluginHoverActive {
		return
	}
	ref := e.pluginHoverRef
	pos := e.pluginHoverPos
	e.pluginHoverActive = false
	e.pluginHoverRef = pluginToolRef{}
	if t := e.pluginToolByRef(ref); t != nil && t.OnHover != nil {
		t.OnHover(e, pos.X, pos.Y, false)
	}
}

func (e *Editor) pluginToolByRef(ref pluginToolRef) *PluginTool {
	p := e.plugin(ref.plugin)
	if p == nil {
		return nil
	}
	for i := range p.Tools {
		if p.Tools[i].ID == ref.id {
			return &p.Tools[i]
		}
	}
	return nil
}

// PluginTool returns the plugin and ID of the active plugin tool, and false when the active tool is not a plugin's.
func (e *Editor) PluginTool() (plugin, id string, ok bool) {
	if e.activePluginTool() == nil {
		return "", "", false
	}
	return e.pluginTool.plugin, e.pluginTool.id, true
}

// ComponentField returns the value of field in the component on objectID: the value the scene sets, or else the field's registered default when a component registry is set (SetComponentRegistry). It returns false when the object does not exist, when the object has no such component, or when the field is neither set nor registered.
func (e *Editor) ComponentField(objectID, component, field string) (any, bool) {
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return nil, false
	}
	c, ok := obj.Components[component]
	if !ok {
		return nil, false
	}
	if m, ok := c.(map[string]any); ok {
		if v, ok := m[field]; ok {
			return v, true
		}
	}
	if t, ok := e.lookupComponentType(component); ok {
		if f, ok := t.Field(field); ok {
			return f.Default, true
		}
	}
	return nil, false
}

// Report shows err in the status bar as the editor's own panels show an edit it refuses, so a plugin's control does not drop the error silently; a nil err does nothing.
func (e *Editor) Report(err error) {
	e.report(err)
}

// pluginToolRef names a plugin's tool.
type pluginToolRef struct{ plugin, id string }

// panelRef names a plugin's panel; it keys the panels of a dock.
type panelRef struct{ plugin, id string }

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
		e.CancelPluginDrag2D()
		e.ClearPluginHover2D()
		e.CancelPluginDrag3D()
		e.ClearPluginHover3D()
		e.Tool = ToolSelect
		e.pluginTool = pluginToolRef{}
	}
	if e.compGizmoDrag.active && e.componentGizmo3D(e.compGizmoDrag.component) != e.compGizmoDrag.gizmo {
		e.CancelComponentGizmoDrag3D()
	}
}

func (e *Editor) activePluginToolSupports3D() bool {
	t := e.activePluginTool()
	return t != nil && t.supports3D()
}

// ensureViewModeForPluginTool switches the viewport to 2D or 3D when the plugin tool supports only one mode,
// keeping the current mode when the tool supports both.
func (e *Editor) ensureViewModeForPluginTool(plugin, id string) {
	p := e.activePlugin(plugin)
	if p == nil {
		return
	}
	for i := range p.Tools {
		if p.Tools[i].ID == id {
			s2 := p.Tools[i].supports2D()
			s3 := p.Tools[i].supports3D()
			if s2 && !s3 {
				e.SetViewMode(ViewMode2D)
			} else if s3 && !s2 {
				e.SetViewMode(ViewMode3D)
			}
			return
		}
	}
}

// pluginComponentGizmo3D returns the ComponentGizmo3D of the first active plugin in editor.plugins order that handles component.
func (e *Editor) pluginComponentGizmo3D(component string) *ComponentGizmo3D {
	for _, p := range e.activePlugins() {
		for i := range p.Gizmos3D {
			if p.Gizmos3D[i].Component == component {
				return &p.Gizmos3D[i]
			}
		}
	}
	return nil
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
		return e.translatePluginText(e.activePlugin(e.pluginTool.plugin), t.LabelKey, t.Label)
	}
	return e.localizedToolName(e.Tool)
}

// dockedPanel is a plugin's panel with the plugin's name.
type dockedPanel struct {
	plugin string
	owner  *Plugin
	panel  *Panel
}

// dockedPanels returns the panels of the plugins that are on at dock, in the project's order and each plugin's order.
func (e *Editor) dockedPanels(dock Dock) []dockedPanel {
	var out []dockedPanel
	for _, p := range e.activePlugins() {
		for i := range p.Panels {
			if p.Panels[i].Dock == dock {
				out = append(out, dockedPanel{p.Name, p, &p.Panels[i]})
			}
		}
	}
	return out
}

// pluginDockView lays out the panels docked at dock: a column for each side dock and a row below the viewport for the bottom dock, or nothing when no plugin that is on docks a panel there. Each panel is keyed by its plugin and ID, so a panel mounts when it appears and stays mounted while other panels come and go.
func pluginDockView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dock Dock) core.Node {
	shown := core.NewMemo(cx, func() bool {
		_ = rev.Get()
		return len(ed.dockedPanels(dock)) > 0
	})
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if !shown.Get() {
			return nil
		}
		colors := ui.ColorsOf(cx)
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
		panels := core.Each(
			func() []dockedPanel {
				_ = rev.Get()
				return ed.dockedPanels(dock)
			},
			func(dp dockedPanel) panelRef { return panelRef{dp.plugin, dp.panel.ID} },
			func(cx *core.Scope, item func() dockedPanel, _ func() int) core.Node {
				return pluginPanelView(cx, ed, rev, item())
			},
		)
		return core.View{
			TestID:   "plugin-dock-" + dock.String(),
			Style:    style,
			Children: []core.Node{panels},
		}
	})
}

// pluginPanelView is one docked panel: its title above its content, which scrolls.
func pluginPanelView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], dp dockedPanel) core.Node {
	colors := ui.ColorsOf(cx)
	ref := panelRef{dp.plugin, dp.panel.ID}
	return core.View{
		TestID: "plugin-panel-" + dp.plugin + "-" + dp.panel.ID,
		Style: core.Style{
			FlexGrow:  1,
			FlexBasis: core.Pt(0),
			Padding:   core.All(10),
			Gap:       8,
		},
		StyleFunc: func() core.Style {
			_ = rev.Get()
			if ed.focusedPluginPanel == ref {
				return core.Style{
					BorderWidth: 1,
					BorderColor: colors.Accent(),
				}
			}
			return core.Style{}
		},
		Children: []core.Node{
			core.Text{
				ContentFunc: func() string {
					_ = rev.Get()
					return ed.translatePluginText(dp.owner, dp.panel.TitleKey, dp.panel.Title)
				},
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

// pluginToolsView is the tool bar group of the active plugins' tools that support mode (ViewMode2D or ViewMode3D), or nothing when they have none.
func pluginToolsView(cx *core.Scope, ed *Editor, rev *core.Signal[uint64], disabled func() bool, mode ViewMode) core.Node {
	matches := func(t PluginTool) bool {
		if mode == ViewMode3D {
			return t.supports3D()
		}
		return t.supports2D()
	}
	key := core.NewMemo(cx, func() string {
		_ = rev.Get()
		var b strings.Builder
		for _, p := range ed.activePlugins() {
			for _, t := range p.Tools {
				if !matches(t) {
					continue
				}
				// Names and IDs have no control characters, so the NUL separators keep two different sets of tools from giving the same key.
				b.WriteString(p.Name)
				b.WriteByte(0)
				b.WriteString(t.ID)
				b.WriteByte(0)
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
				if !matches(t) {
					continue
				}
				ref := pluginToolRef{p.Name, t.ID}
				buttons = append(buttons, ui.ToggleButton{
					TestID:   "tool-" + ref.plugin + "-" + ref.id,
					Text:     ed.translatePluginText(p, t.LabelKey, t.Label),
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
		return ui.ToolbarGroup{Label: ed.T("toolbar.group.pluginTools"), Children: buttons}
	})
}
