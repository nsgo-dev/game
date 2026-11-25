package editor

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ObjectEvent is an event declared by one of an object's registered components.
type ObjectEvent struct {
	// Component is the component type name on the object.
	Component string
	// Event is the event declaration from the component registry.
	Event sceneformat.Event
	// Ref is the shortest unambiguous name to write in ConnectionDoc.Event: the bare event name when unique on the object, or "component.event" when several of its registered components declare the same name.
	Ref string
}

// ObjectHandler is a handler declared by one of an object's registered components.
type ObjectHandler struct {
	// Component is the component type name on the object.
	Component string
	// Handler is the handler declaration (including Params and Doc) from the component registry.
	Handler sceneformat.Handler
	// Ref is the shortest unambiguous name to write in ConnectionDoc.Handler: the bare handler name when unique on the object, or "component.handler" when several of its registered components declare the same name.
	Ref string
}

// ConnectionEntry pairs a scene connection with its index in Doc.Connections.
type ConnectionEntry struct {
	Index int
	Conn  sceneformat.ConnectionDoc
}

// connPickState holds an active target-object pick callback for the connections UI: the next object pressed in the 2D viewport, 3D viewport or scene tree is delivered to onPick without changing SelectedID.
type connPickState struct {
	sourceID string
	onPick   func(targetID string)
}

// ObjectEvents returns the events declared by objectID's registered components, ordered by component name ascending and then each component type's Events order. It returns nil when no component registry is set or objectID is not found.
func (e *Editor) ObjectEvents(objectID string) []ObjectEvent {
	if e == nil || e.components == nil || objectID == "" {
		return nil
	}
	obj := e.findObjectForConnection(objectID)
	if obj == nil || len(obj.Components) == 0 {
		return nil
	}
	compNames := slices.Sorted(maps.Keys(obj.Components))
	counts := make(map[string]int)
	var out []ObjectEvent
	for _, comp := range compNames {
		t, ok := e.components.Lookup(comp)
		if !ok {
			continue
		}
		for _, ev := range t.Events {
			counts[ev.Name]++
			out = append(out, ObjectEvent{Component: comp, Event: ev, Ref: ev.Name})
		}
	}
	for i := range out {
		if counts[out[i].Event.Name] > 1 {
			out[i].Ref = out[i].Component + "." + out[i].Event.Name
		}
	}
	return out
}

// ObjectHandlers returns the handlers declared by objectID's registered components, ordered by component name ascending and then each component type's Handlers order. It returns nil when no component registry is set or objectID is not found.
func (e *Editor) ObjectHandlers(objectID string) []ObjectHandler {
	if e == nil || e.components == nil || objectID == "" {
		return nil
	}
	obj := e.findObjectForConnection(objectID)
	if obj == nil || len(obj.Components) == 0 {
		return nil
	}
	compNames := slices.Sorted(maps.Keys(obj.Components))
	counts := make(map[string]int)
	var out []ObjectHandler
	for _, comp := range compNames {
		t, ok := e.components.Lookup(comp)
		if !ok {
			continue
		}
		for _, h := range t.Handlers {
			counts[h.Name]++
			out = append(out, ObjectHandler{Component: comp, Handler: h, Ref: h.Name})
		}
	}
	for i := range out {
		if counts[out[i].Handler.Name] > 1 {
			out[i].Ref = out[i].Component + "." + out[i].Handler.Name
		}
	}
	return out
}

// ObjectConnections returns the outgoing (From == objectID) and incoming (To == objectID) connections of objectID in document order. Each ConnectionEntry carries a deep copy of its connection so callers cannot mutate Doc directly.
func (e *Editor) ObjectConnections(objectID string) (outgoing, incoming []ConnectionEntry) {
	if e == nil || e.Doc == nil || objectID == "" {
		return nil, nil
	}
	for i, c := range e.Doc.Connections {
		if c.From == objectID {
			outgoing = append(outgoing, ConnectionEntry{Index: i, Conn: cloneConnectionDoc(c)})
		}
		if c.To == objectID {
			incoming = append(incoming, ConnectionEntry{Index: i, Conn: cloneConnectionDoc(c)})
		}
	}
	return outgoing, incoming
}

// SceneObjectIDs returns the IDs of every object in the open scene (including nested children and, when resolved, instanced or inherited objects) in document order.
func (e *Editor) SceneObjectIDs() []string {
	if e == nil || e.Doc == nil {
		return nil
	}
	doc := e.Doc
	if doc.RefersToScenes() && e.Built != nil && e.Built.Doc != nil {
		doc = e.Built.Doc
	}
	var ids []string
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			ids = append(ids, o.ID)
			return true
		})
	}
	return ids
}

// AddConnection appends conn to Doc.Connections as an undoable command after validating the resulting scene with sceneformat.Validate and, when a component registry is set, sceneformat.ValidateConnections. Invalid connections are refused before reaching History, with the reason reported in Status.
func (e *Editor) AddConnection(conn sceneformat.ConnectionDoc) (int, error) {
	if e == nil || e.Doc == nil {
		return -1, errors.New("editor: no open scene")
	}
	if e.playing {
		return -1, ErrPlaying
	}
	cleaned := cloneConnectionDoc(conn)
	candidate := make([]sceneformat.ConnectionDoc, len(e.Doc.Connections)+1)
	copy(candidate, e.Doc.Connections)
	candidate[len(e.Doc.Connections)] = cleaned
	if err := e.validateConnections(candidate); err != nil {
		e.report(err)
		return -1, err
	}
	cmd := &addConnectionCmd{ed: e, conn: cleaned}
	e.History.Execute(cmd)
	return cmd.index, nil
}

// UpdateConnection replaces Doc.Connections[index] with conn as an undoable command after validating the resulting scene with sceneformat.Validate and, when a component registry is set, sceneformat.ValidateConnections. Invalid connections are refused before reaching History, with the reason reported in Status.
func (e *Editor) UpdateConnection(index int, conn sceneformat.ConnectionDoc) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	if index < 0 || index >= len(e.Doc.Connections) {
		err := fmt.Errorf("editor: connection index %d out of range", index)
		e.report(err)
		return err
	}
	cleaned := cloneConnectionDoc(conn)
	if connectionsEqual(e.Doc.Connections[index], cleaned) {
		return nil
	}
	candidate := slices.Clone(e.Doc.Connections)
	candidate[index] = cleaned
	if err := e.validateConnections(candidate); err != nil {
		e.report(err)
		return err
	}
	e.History.Execute(&updateConnectionCmd{
		ed:      e,
		index:   index,
		oldConn: cloneConnectionDoc(e.Doc.Connections[index]),
		newConn: cleaned,
	})
	return nil
}

// RemoveConnection removes Doc.Connections[index] as an undoable command. Undo puts it back at its original index.
func (e *Editor) RemoveConnection(index int) error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	if e.playing {
		return ErrPlaying
	}
	if index < 0 || index >= len(e.Doc.Connections) {
		err := fmt.Errorf("editor: connection index %d out of range", index)
		e.report(err)
		return err
	}
	e.History.Execute(&removeConnectionCmd{
		ed:      e,
		index:   index,
		oldConn: cloneConnectionDoc(e.Doc.Connections[index]),
	})
	return nil
}

// JumpToConnectionOtherEnd selects the other endpoint of Doc.Connections[index] relative to SelectedID (To when From is selected, From when To is selected, or To otherwise). If the endpoint is an object inside an instanced scene (such as "door_1/door"), it selects its owning instance in Doc.
func (e *Editor) JumpToConnectionOtherEnd(index int) bool {
	if e == nil || e.Doc == nil || index < 0 || index >= len(e.Doc.Connections) {
		return false
	}
	c := e.Doc.Connections[index]
	other := c.To
	if e.SelectedID != "" && c.To == e.SelectedID && c.From != e.SelectedID {
		other = c.From
	}
	return e.jumpToConnectionObject(other)
}

func (e *Editor) jumpToConnectionObject(id string) bool {
	if e == nil || id == "" {
		return false
	}
	if e.SelectObject(id) {
		return true
	}
	// An instanced scene object such as "door_1/door" is not directly in e.Doc; walk up slash prefixes to select its owning instance.
	cur := id
	for {
		i := strings.LastIndexByte(cur, '/')
		if i <= 0 {
			break
		}
		cur = cur[:i]
		if e.SelectObject(cur) {
			return true
		}
	}
	e.report(fmt.Errorf("editor: object %q not found in open scene", id))
	return false
}

// BeginConnectionPick starts viewport and scene-tree target picking for the connections panel: the next object pressed in the 2D viewport, 3D viewport or scene tree is passed to onPick without changing SelectedID. Ignored while playing or when onPick is nil.
func (e *Editor) BeginConnectionPick(onPick func(targetID string)) {
	if e == nil || e.playing || onPick == nil {
		return
	}
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelPluginDrag2D()
	e.CancelComponentGizmoDrag3D()
	e.CancelPluginDrag3D()
	e.CancelTileStroke()
	e.connPick = &connPickState{
		sourceID: e.SelectedID,
		onPick:   onPick,
	}
	e.Status = "Pick target object in viewport or scene tree (Esc to cancel)"
	e.notify()
}

// PickingConnectionTarget reports whether connection target pick mode is active.
func (e *Editor) PickingConnectionTarget() bool {
	return e != nil && e.connPick != nil
}

// CancelConnectionPick exits connection target pick mode without selecting a target.
func (e *Editor) CancelConnectionPick() {
	if e == nil || e.connPick == nil {
		return
	}
	e.connPick = nil
	e.Status = "Cancelled target pick"
	e.notify()
}

func (e *Editor) finishConnectionPick(targetID string) bool {
	if e == nil || e.connPick == nil || targetID == "" {
		return false
	}
	cb := e.connPick.onPick
	e.connPick = nil
	if cb != nil {
		cb(targetID)
	}
	e.Status = "Picked target " + targetID
	e.notify()
	return true
}

func (e *Editor) findObjectForConnection(id string) *sceneformat.ObjectDoc {
	if obj, _, _ := e.FindObject(id); obj != nil {
		return obj
	}
	if e.Built != nil && e.Built.Doc != nil && e.Built.Doc != e.Doc {
		if loc := locateObject(e.Built.Doc, id); loc.obj != nil {
			return loc.obj
		}
	}
	return nil
}

// resolveObjectHandler resolves handlerRef (bare or "component.name") against objectID's registered components, returning the matching Handler and true when it binds unambiguously.
func (e *Editor) resolveObjectHandler(objectID, handlerRef string) (sceneformat.Handler, bool) {
	if e == nil || e.components == nil || objectID == "" || handlerRef == "" {
		return sceneformat.Handler{}, false
	}
	handlers := e.ObjectHandlers(objectID)
	if comp, name, qualified := strings.Cut(handlerRef, "."); qualified {
		for _, h := range handlers {
			if h.Component == comp && h.Handler.Name == name {
				return h.Handler, true
			}
		}
		return sceneformat.Handler{}, false
	}
	var found *sceneformat.Handler
	for i := range handlers {
		if handlers[i].Handler.Name == handlerRef {
			if found != nil {
				return sceneformat.Handler{}, false
			}
			found = &handlers[i].Handler
		}
	}
	if found == nil {
		return sceneformat.Handler{}, false
	}
	return *found, true
}

func (e *Editor) validateConnections(candidate []sceneformat.ConnectionDoc) error {
	if e.Doc == nil {
		return errors.New("editor: no open scene")
	}
	check := *e.Doc
	check.Connections = candidate
	if err := sceneformat.Validate(&check); err != nil {
		return err
	}
	if e.components == nil {
		return nil
	}
	if !check.RefersToScenes() {
		return sceneformat.ValidateConnections(&check, e.components)
	}
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		return nil
	}
	resolved, err := sceneformat.Resolve(&check, scenes)
	if err != nil {
		return err
	}
	return sceneformat.ValidateConnections(resolved, e.components)
}

func cloneConnectionDoc(c sceneformat.ConnectionDoc) sceneformat.ConnectionDoc {
	out := c
	if len(c.Args) == 0 {
		out.Args = nil
	} else {
		out.Args = cloneMap(c.Args)
	}
	return out
}

func connectionsEqual(a, b sceneformat.ConnectionDoc) bool {
	if a.From != b.From || a.Event != b.Event || a.To != b.To || a.Handler != b.Handler || a.Once != b.Once {
		return false
	}
	if len(a.Args) == 0 && len(b.Args) == 0 {
		return true
	}
	return reflect.DeepEqual(a.Args, b.Args)
}

func formatConnectionArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := sortedMapKeys(args)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + formatFieldValue(args[k])
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

type addConnectionCmd struct {
	ed    *Editor
	conn  sceneformat.ConnectionDoc
	index int
}

func (c *addConnectionCmd) Label() string {
	return fmt.Sprintf("Connect %s.%s -> %s.%s", c.conn.From, c.conn.Event, c.conn.To, c.conn.Handler)
}

func (c *addConnectionCmd) Do() {
	c.ed.Doc.Connections = append(c.ed.Doc.Connections, cloneConnectionDoc(c.conn))
	c.index = len(c.ed.Doc.Connections) - 1
	c.ed.Status = fmt.Sprintf("Connected %s.%s -> %s.%s", c.conn.From, c.conn.Event, c.conn.To, c.conn.Handler)
}

func (c *addConnectionCmd) Undo() {
	if c.index < 0 || c.index >= len(c.ed.Doc.Connections) {
		return
	}
	conns := slices.Delete(c.ed.Doc.Connections, c.index, c.index+1)
	if len(conns) == 0 {
		conns = nil
	}
	c.ed.Doc.Connections = conns
}

type updateConnectionCmd struct {
	ed      *Editor
	index   int
	oldConn sceneformat.ConnectionDoc
	newConn sceneformat.ConnectionDoc
}

func (c *updateConnectionCmd) Label() string {
	return fmt.Sprintf("Edit connection %s.%s -> %s.%s", c.newConn.From, c.newConn.Event, c.newConn.To, c.newConn.Handler)
}

func (c *updateConnectionCmd) Do() {
	if c.index < 0 || c.index >= len(c.ed.Doc.Connections) {
		return
	}
	c.ed.Doc.Connections[c.index] = cloneConnectionDoc(c.newConn)
	c.ed.Status = fmt.Sprintf("Updated connection %s.%s -> %s.%s", c.newConn.From, c.newConn.Event, c.newConn.To, c.newConn.Handler)
}

func (c *updateConnectionCmd) Undo() {
	if c.index < 0 || c.index >= len(c.ed.Doc.Connections) {
		return
	}
	c.ed.Doc.Connections[c.index] = cloneConnectionDoc(c.oldConn)
}

type removeConnectionCmd struct {
	ed      *Editor
	index   int
	oldConn sceneformat.ConnectionDoc
}

func (c *removeConnectionCmd) Label() string {
	return fmt.Sprintf("Remove connection %s.%s -> %s.%s", c.oldConn.From, c.oldConn.Event, c.oldConn.To, c.oldConn.Handler)
}

func (c *removeConnectionCmd) Do() {
	if c.index < 0 || c.index >= len(c.ed.Doc.Connections) {
		return
	}
	conns := slices.Delete(c.ed.Doc.Connections, c.index, c.index+1)
	if len(conns) == 0 {
		conns = nil
	}
	c.ed.Doc.Connections = conns
	c.ed.Status = fmt.Sprintf("Removed connection %s.%s -> %s.%s", c.oldConn.From, c.oldConn.Event, c.oldConn.To, c.oldConn.Handler)
}

func (c *removeConnectionCmd) Undo() {
	at := min(max(c.index, 0), len(c.ed.Doc.Connections))
	c.ed.Doc.Connections = slices.Insert(c.ed.Doc.Connections, at, cloneConnectionDoc(c.oldConn))
}
