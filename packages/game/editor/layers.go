package editor

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// LayerInfo is what the layers panel shows for one layer of the open scene (Layers).
type LayerInfo struct {
	// Name is the layer's name.
	Name string
	// Space is the layer's effective space, SpaceWorld or SpaceScreen.
	Space sceneformat.Space
	// Order is the layer's draw order value.
	Order int
	// Visible reports whether the layer draws, as its visible field says.
	Visible bool
	// Locked reports whether the layer is locked against picking and painting in the viewports for the session (SetLayerLocked).
	Locked bool
	// Active reports whether the layer is the editor's ActiveLayer.
	Active bool
	// Tilemap describes the layer's tilemap: the tileset and size of an inline grid ("tiles 16x12"), the name of the top-level tilemap it refers to, or the path of an external Tiled map; it is empty for a layer without a tilemap.
	Tilemap string
	// Objects is the number of objects on the layer, nested children included.
	Objects int
	// Inherited reports whether the layer extends a layer of the scene the open scene inherits (SceneDoc.Inherits), whose space, order, visibility and name that scene owns.
	Inherited bool
}

// layerDrawOrder returns the indices of layers in draw order: lowest Order first, ties in slice order.
func layerDrawOrder(layers []sceneformat.LayerDoc) []int {
	idx := make([]int, len(layers))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(layers[a].Order, layers[b].Order) })
	return idx
}

// effectiveLayers returns the layers of doc as the game draws them: doc's own, or for a scene that inherits another, those of the copy Resolve makes, the base's layers included. It falls back to doc's own layers when the scene does not resolve.
func (e *Editor) effectiveLayers() []sceneformat.LayerDoc {
	if e.Doc.Inherits != "" && e.Built != nil && e.BuildErr == nil && e.Built.Doc != nil {
		return e.Built.Doc.Layers
	}
	return e.Doc.Layers
}

// drawnNames returns the names of the open scene's own layers in the order layers, the scene's effective layers, draws them.
func drawnNames(own, layers []sceneformat.LayerDoc) []string {
	mine := make(map[string]bool, len(own))
	for i := range own {
		mine[own[i].Name] = true
	}
	out := make([]string, 0, len(own))
	for _, i := range layerDrawOrder(layers) {
		if mine[layers[i].Name] {
			out = append(out, layers[i].Name)
		}
	}
	return out
}

// Layers returns the open scene's layers in draw order, the first drawn first, with what the layers panel shows of each. In a scene that inherits another the space, order and visibility are those the resolved scene gives a layer, so a layer that extends one of the base's shows the base's, as its layer overrides change them.
func (e *Editor) Layers() []LayerInfo {
	if e.Doc == nil {
		return nil
	}
	eff := e.effectiveLayers()
	inherited := e.inheritedLayers()
	names := drawnNames(e.Doc.Layers, eff)
	out := make([]LayerInfo, 0, len(names))
	for _, name := range names {
		own := &e.Doc.Layers[e.layerIndex(name)]
		l := own
		if i := slices.IndexFunc(eff, func(l sceneformat.LayerDoc) bool { return l.Name == name }); i >= 0 {
			l = &eff[i]
		}
		n := 0
		sceneformat.WalkObjects(own.Objects, func(_, _ *sceneformat.ObjectDoc) bool { n++; return true })
		out = append(out, LayerInfo{
			Name:      name,
			Space:     l.EffectiveSpace(),
			Order:     l.Order,
			Visible:   l.IsVisible(),
			Locked:    e.layerLocked[name],
			Active:    e.ActiveLayer == name,
			Tilemap:   e.describeTilemap(l.Tilemap),
			Objects:   n,
			Inherited: inherited[name],
		})
	}
	return out
}

// describeTilemap returns LayerInfo.Tilemap for tm.
func (e *Editor) describeTilemap(tm *sceneformat.TilemapDoc) string {
	switch {
	case tm == nil:
		return ""
	case tm.IsRef():
		return tm.Ref
	case tm.IsExternal():
		return tm.Source
	case tm.Tileset != "":
		return fmt.Sprintf("%s %dx%d", tm.Tileset, tm.Width, tm.Height)
	default:
		return fmt.Sprintf("%dx%d", tm.Width, tm.Height)
	}
}

// inheritedLayers returns the names of the layers the scene the open scene inherits holds, and those its layer overrides name, or nil for a scene that inherits none. It loads the base scene through the Resolver once per revision.
func (e *Editor) inheritedLayers() map[string]bool {
	if e.Doc == nil || e.Doc.Inherits == "" {
		return nil
	}
	c := &e.baseLayers
	if c.names != nil && c.rev == e.revision && c.doc == e.Doc && c.inherits == e.Doc.Inherits {
		return c.names
	}
	names := make(map[string]bool)
	for name := range e.Doc.LayerOverrides {
		names[name] = true
	}
	if scenes, ok := e.Resolver.(sceneformat.SceneResolver); ok {
		if base, err := scenes.Scene(e.Doc.Inherits); err == nil {
			if resolved, err := sceneformat.Resolve(base, scenes); err == nil {
				for i := range resolved.Layers {
					names[resolved.Layers[i].Name] = true
				}
			}
		}
	}
	*c = baseLayerCache{rev: e.revision, doc: e.Doc, inherits: e.Doc.Inherits, names: names}
	return names
}

// baseLayerCache holds inheritedLayers' answer for one revision of one document.
type baseLayerCache struct {
	rev      uint64
	doc      *sceneformat.SceneDoc
	inherits string
	names    map[string]bool
}

// inheritedErr is the error a change that only the inherited scene, or the open scene's layer overrides, may make to the layer name returns.
func (e *Editor) inheritedErr(name, what string) error {
	return fmt.Errorf("editor: layer %q extends the layer of base scene %q, which owns its %s; change it in that scene or under layerOverrides", name, e.Doc.Inherits, what)
}

// checkLayerEdit returns why the scene candidate, the open scene with a layer edit applied, would not open or build: sceneformat.Validate's error, or for a scene that instances or inherits other scene files, sceneformat.Resolve's. On success it returns the layers the candidate draws (effectiveLayers).
func (e *Editor) checkLayerEdit(candidate *sceneformat.SceneDoc) ([]sceneformat.LayerDoc, error) {
	if err := sceneformat.Validate(candidate); err != nil {
		return nil, err
	}
	if !candidate.RefersToScenes() {
		return candidate.Layers, nil
	}
	scenes, ok := e.Resolver.(sceneformat.SceneResolver)
	if !ok {
		// Without a scene resolver nothing more can be checked, as for every other edit of such a scene.
		return candidate.Layers, nil
	}
	resolved, err := sceneformat.Resolve(candidate, scenes)
	if err != nil {
		return nil, err
	}
	return resolved.Layers, nil
}

// checkNewLayerName returns why name cannot name a new layer of the open scene, or a layer renamed to it.
func (e *Editor) checkNewLayerName(name string) error {
	if name == "" {
		return errors.New("editor: a layer name must not be empty")
	}
	if e.layerIndex(name) >= 0 {
		return fmt.Errorf("editor: the scene already has a layer %q", name)
	}
	if e.inheritedLayers()[name] {
		return fmt.Errorf("editor: base scene %q has a layer %q, which a layer of this name would extend", e.Doc.Inherits, name)
	}
	return nil
}

// newLayerName returns the first name of the form "layer_N", from N = number of layers + 1, that names no layer of the open scene or of the scene it inherits.
func (e *Editor) newLayerName() string {
	n := 1
	if e.Doc != nil {
		n = len(e.Doc.Layers) + 1
	}
	for ; ; n++ {
		name := "layer_" + strconv.Itoa(n)
		if e.checkNewLayerName(name) == nil {
			return name
		}
	}
}

// validSpace reports whether space is one a layer may set.
func validSpace(space sceneformat.Space) bool {
	return space == sceneformat.SpaceWorld || space == sceneformat.SpaceScreen
}

// AddLayer adds a layer named name in space (SpaceWorld or SpaceScreen; empty leaves the field unset, which is world) to the open scene as an undoable command. The new layer comes last in the file and draws over every other layer: its order is the highest order of the layers the scene draws, those of the scene it inherits included, which is left unset when that is 0. It refuses an empty name, a name another layer of the scene has, and a name of a layer of the scene the open scene inherits, which a layer of that name would extend rather than add. It does not change ActiveLayer.
func (e *Editor) AddLayer(name string, space sceneformat.Space) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: no scene is open")
	}
	if space != "" && !validSpace(space) {
		return fmt.Errorf("editor: layer space %q must be %q or %q", space, sceneformat.SpaceWorld, sceneformat.SpaceScreen)
	}
	if err := e.checkNewLayerName(name); err != nil {
		return err
	}
	order := 0
	for _, l := range e.effectiveLayers() {
		order = max(order, l.Order)
	}
	layer := sceneformat.LayerDoc{Name: name, Space: space, Order: order}
	candidate := e.Doc.Clone()
	candidate.Layers = append(candidate.Layers, layer)
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.History.Execute(&addLayerCmd{ed: e, layer: layer, wasNil: e.Doc.Layers == nil})
	return nil
}

type addLayerCmd struct {
	ed     *Editor
	layer  sceneformat.LayerDoc
	wasNil bool
	// wasLocked records whether the layer was locked when Undo took it away, so that a Redo locks it again and a later layer of the same name does not start locked.
	wasLocked bool
}

func (c *addLayerCmd) Label() string { return "Add layer " + c.layer.Name }

func (c *addLayerCmd) Do() {
	c.ed.Doc.Layers = append(c.ed.Doc.Layers, c.layer)
	if c.wasLocked {
		c.ed.setLayerLockedFlag(c.layer.Name, true)
	}
	c.ed.Status = "Added layer " + c.layer.Name
}

func (c *addLayerCmd) Undo() {
	if i := c.ed.layerIndex(c.layer.Name); i >= 0 {
		c.ed.Doc.Layers = slices.Delete(c.ed.Doc.Layers, i, i+1)
	}
	if len(c.ed.Doc.Layers) == 0 && c.wasNil {
		c.ed.Doc.Layers = nil
	}
	c.wasLocked = c.ed.layerLocked[c.layer.Name]
	c.ed.setLayerLockedFlag(c.layer.Name, false)
}

// RemoveLayer removes the layer name, with its objects and its tilemap, from the open scene as an undoable command, together with the scene's connections from or to its objects, their descendants or the objects of scenes they instance; Undo puts the layer back where it was, with them. When the layer is the active one, the layer that takes its place in the file, or the one before it, becomes active.
func (e *Editor) RemoveLayer(name string) error {
	if e.playing {
		return ErrPlaying
	}
	li := e.layerIndex(name)
	if li < 0 {
		return fmt.Errorf("editor: layer %q not found", name)
	}
	ids, instancing := subtreeIDs(e.Doc.Layers[li].Objects)
	candidate := e.Doc.Clone()
	candidate.Layers = slices.Delete(candidate.Layers, li, li+1)
	candidate.Connections = slices.DeleteFunc(candidate.Connections, func(c sceneformat.ConnectionDoc) bool {
		return namesSubtree(c.From, ids, instancing) || namesSubtree(c.To, ids, instancing)
	})
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&removeLayerCmd{
		ed:              e,
		index:           li,
		layer:           cloneLayerDoc(&e.Doc.Layers[li]),
		prevActive:      e.ActiveLayer,
		prevTile:        e.ActiveTileID,
		prevSelected:    e.SelectedID,
		prevSelectedIDs: e.SelectedIDs(),
	})
	return nil
}

// subtreeIDs returns the IDs of objs and their descendants, and those of them that instance a scene file, as namesSubtree takes them.
func subtreeIDs(objs []sceneformat.ObjectDoc) (ids, instancing map[string]struct{}) {
	ids = make(map[string]struct{})
	instancing = make(map[string]struct{})
	sceneformat.WalkObjects(objs, func(o, _ *sceneformat.ObjectDoc) bool {
		ids[o.ID] = struct{}{}
		if o.Scene != "" {
			instancing[o.ID] = struct{}{}
		}
		return true
	})
	return ids, instancing
}

// cloneLayerDoc returns a deep copy of l.
func cloneLayerDoc(l *sceneformat.LayerDoc) sceneformat.LayerDoc {
	doc := sceneformat.SceneDoc{Layers: []sceneformat.LayerDoc{*l}}
	return doc.Clone().Layers[0]
}

type removeLayerCmd struct {
	ed    *Editor
	index int
	layer sceneformat.LayerDoc
	// conns are the connections Do removed with the layer's objects, each with its index before the removal, in ascending order.
	conns           []removedConnection
	prevActive      string
	prevTile        int
	prevSelected    string
	prevSelectedIDs []string
	// tookActive records that Do made another layer active, and with it maybe another tile.
	tookActive bool
	wasLocked  bool
}

func (c *removeLayerCmd) Label() string { return "Remove layer " + c.layer.Name }

func (c *removeLayerCmd) Do() {
	e := c.ed
	li := e.layerIndex(c.layer.Name)
	if li < 0 {
		return
	}
	e.Doc.Layers = slices.Delete(e.Doc.Layers, li, li+1)
	ids, instancing := subtreeIDs(c.layer.Objects)
	c.conns = nil
	var kept []sceneformat.ConnectionDoc
	for i, conn := range e.Doc.Connections {
		if namesSubtree(conn.From, ids, instancing) || namesSubtree(conn.To, ids, instancing) {
			c.conns = append(c.conns, removedConnection{index: i, conn: conn})
			continue
		}
		kept = append(kept, conn)
	}
	if len(c.conns) > 0 {
		e.Doc.Connections = kept
	}
	c.wasLocked = e.layerLocked[c.layer.Name]
	delete(e.layerLocked, c.layer.Name)
	c.tookActive = e.ActiveLayer == c.layer.Name
	if c.tookActive {
		e.ActiveLayer = ""
		if n := len(e.Doc.Layers); n > 0 {
			e.ActiveLayer = e.Doc.Layers[min(li, n-1)].Name
		}
		// The layer taking over may paint another tileset, as SetActiveLayer keeps in mind.
		e.keepTileInPalette()
	}
	remaining := make([]string, 0, len(e.selectedIDs))
	for _, sid := range e.SelectedIDs() {
		if _, removed := ids[sid]; !removed {
			remaining = append(remaining, sid)
		}
	}
	primary := e.SelectedID
	if _, removed := ids[primary]; removed {
		if len(remaining) > 0 {
			primary = remaining[len(remaining)-1]
		} else {
			primary = ""
		}
	}
	e.setSelectionQuiet(remaining, primary)
	e.Status = "Removed layer " + c.layer.Name
}

func (c *removeLayerCmd) Undo() {
	e := c.ed
	at := min(c.index, len(e.Doc.Layers))
	e.Doc.Layers = slices.Insert(e.Doc.Layers, at, cloneLayerDoc(&c.layer))
	for _, rc := range c.conns {
		e.Doc.Connections = slices.Insert(e.Doc.Connections, min(rc.index, len(e.Doc.Connections)), rc.conn)
	}
	if c.wasLocked {
		e.setLayerLockedFlag(c.layer.Name, true)
	}
	if c.tookActive {
		e.ActiveLayer = c.prevActive
		e.ActiveTileID = c.prevTile
		e.keepTileInPalette()
	}
	e.setSelectionQuiet(c.prevSelectedIDs, c.prevSelected)
}

// RenameLayer renames the layer oldName to newName as an undoable command, and with it the ActiveLayer and the layer's lock. It refuses an empty name and one another layer has, and, in a scene that inherits another, renaming a layer that extends one of the base's or to the name of one of them, since the name is what ties the layer to the base and its layer overrides. Other scene files that inherit the open one and name the layer are not rewritten.
func (e *Editor) RenameLayer(oldName, newName string) error {
	if e.playing {
		return ErrPlaying
	}
	li := e.layerIndex(oldName)
	if li < 0 {
		return fmt.Errorf("editor: layer %q not found", oldName)
	}
	if oldName == newName {
		return nil
	}
	if e.inheritedLayers()[oldName] {
		return e.inheritedErr(oldName, "name")
	}
	if err := e.checkNewLayerName(newName); err != nil {
		return err
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Name = newName
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.endTileStrokeForChange()
	e.History.Execute(&renameLayerCmd{ed: e, oldName: oldName, newName: newName})
	return nil
}

type renameLayerCmd struct {
	ed               *Editor
	oldName, newName string
}

func (c *renameLayerCmd) Label() string { return "Rename layer " + c.oldName + " to " + c.newName }

func (c *renameLayerCmd) rename(from, to string) {
	e := c.ed
	if li := e.layerIndex(from); li >= 0 {
		e.Doc.Layers[li].Name = to
	}
	if e.ActiveLayer == from {
		e.ActiveLayer = to
	}
	if e.layerLocked[from] {
		delete(e.layerLocked, from)
		e.layerLocked[to] = true
	}
}

func (c *renameLayerCmd) Do() {
	c.rename(c.oldName, c.newName)
	c.ed.Status = "Renamed layer " + c.oldName + " to " + c.newName
}

func (c *renameLayerCmd) Undo() { c.rename(c.newName, c.oldName) }

// MoveLayer moves the layer name to position index of the draw order Layers lists, 0 drawing first, as an undoable command; an index below 0 puts it first and one past the end last. It moves the layer in the file next to the layer it is to draw after or before and changes its order only when the order it has would not draw it there, to the order of that neighbour, so the other layers keep their place and their orders. In a scene that inherits another it refuses to move a layer that extends one of the base's, whose order the base owns, and a move the resolved scene would not draw as asked.
func (e *Editor) MoveLayer(name string, index int) error {
	if e.playing {
		return ErrPlaying
	}
	li := e.layerIndex(name)
	if li < 0 {
		return fmt.Errorf("editor: layer %q not found", name)
	}
	if e.inheritedLayers()[name] {
		return e.inheritedErr(name, "order")
	}
	names := drawnNames(e.Doc.Layers, e.effectiveLayers())
	index = max(0, min(index, len(names)-1))
	if names[index] == name {
		return nil
	}
	newIndex, newOrder := planLayerMove(e.Doc.Layers, li, index)
	candidate := e.Doc.Clone()
	moveLayerAt(candidate, li, newIndex, newOrder)
	eff, err := e.checkLayerEdit(candidate)
	if err != nil {
		return err
	}
	if got := drawnNames(candidate.Layers, eff); got[index] != name {
		return fmt.Errorf("editor: layer %q cannot be drawn at position %d without changing the order of the layers it extends from base scene %q; set their orders under layerOverrides", name, index, e.Doc.Inherits)
	}
	e.History.Execute(&moveLayerCmd{ed: e, name: name, oldIndex: li, oldOrder: e.Doc.Layers[li].Order, newIndex: newIndex, newOrder: newOrder})
	return nil
}

// planLayerMove returns where in the slice layers the layer at m goes, counted after taking it out, and the order it then has, for it to draw at position to of the draw order of the others (layerDrawOrder).
func planLayerMove(layers []sceneformat.LayerDoc, m, to int) (newIndex, newOrder int) {
	var rest []int
	for _, i := range layerDrawOrder(layers) {
		if i != m {
			rest = append(rest, i)
		}
	}
	prev, next := -1, -1
	if to > 0 {
		prev = rest[to-1]
	}
	if to < len(rest) {
		next = rest[to]
	}
	newOrder = layers[m].Order
	if prev >= 0 && newOrder < layers[prev].Order || next >= 0 && newOrder > layers[next].Order {
		if prev >= 0 {
			newOrder = layers[prev].Order
		} else {
			newOrder = layers[next].Order
		}
	}
	// Slice positions after taking the moved layer out.
	pos := func(i int) int {
		if i > m {
			return i - 1
		}
		return i
	}
	switch {
	case next >= 0 && newOrder == layers[next].Order:
		// Ties draw in slice order: just before next, and so after prev when it ties too, since prev comes before next in the slice.
		return pos(next), newOrder
	case prev >= 0 && newOrder == layers[prev].Order:
		return pos(prev) + 1, newOrder
	default:
		// The order lies strictly between its neighbours', which no other layer has, so the slice position does not matter.
		return m, newOrder
	}
}

// moveLayerAt takes doc's layer at from out, sets its order and puts it back at to.
func moveLayerAt(doc *sceneformat.SceneDoc, from, to, order int) {
	l := doc.Layers[from]
	l.Order = order
	doc.Layers = slices.Delete(doc.Layers, from, from+1)
	doc.Layers = slices.Insert(doc.Layers, min(to, len(doc.Layers)), l)
}

type moveLayerCmd struct {
	ed                 *Editor
	name               string
	oldIndex, oldOrder int
	newIndex, newOrder int
}

func (c *moveLayerCmd) Label() string { return "Move layer " + c.name }

func (c *moveLayerCmd) Do() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		moveLayerAt(c.ed.Doc, li, c.newIndex, c.newOrder)
	}
	c.ed.Status = "Moved layer " + c.name
}

func (c *moveLayerCmd) Undo() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		moveLayerAt(c.ed.Doc, li, c.oldIndex, c.oldOrder)
	}
}

// SetLayerVisible shows or hides the layer name as an undoable command, writing its visible field: hiding writes visible: false, and showing leaves the field unset, its default, so hiding and showing a layer whose field was unset gives back the same file. Unlike the session's lock (SetLayerLocked), this is the scene's own setting: the game does not draw a hidden layer either. In a scene that inherits another it refuses a layer that extends one of the base's, whose visibility the base and the scene's layer overrides own.
func (e *Editor) SetLayerVisible(name string, visible bool) error {
	if e.playing {
		return ErrPlaying
	}
	li := e.layerIndex(name)
	if li < 0 {
		return fmt.Errorf("editor: layer %q not found", name)
	}
	if e.inheritedLayers()[name] {
		return e.inheritedErr(name, "visibility")
	}
	l := &e.Doc.Layers[li]
	if l.IsVisible() == visible {
		return nil
	}
	var v *bool
	if !visible {
		v = sceneformat.Bool(false)
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Visible = v
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	label := "Hide layer " + name
	if visible {
		label = "Show layer " + name
	} else {
		e.endTileStrokeForChange()
	}
	e.History.Execute(&layerVisibleCmd{ed: e, name: name, label: label, old: clonePtr(l.Visible), new: v})
	return nil
}

type layerVisibleCmd struct {
	ed       *Editor
	name     string
	label    string
	old, new *bool
}

func (c *layerVisibleCmd) Label() string { return c.label }

func (c *layerVisibleCmd) Do() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		c.ed.Doc.Layers[li].Visible = clonePtr(c.new)
	}
	c.ed.Status = c.label
}

func (c *layerVisibleCmd) Undo() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		c.ed.Doc.Layers[li].Visible = clonePtr(c.old)
	}
}

// clonePtr returns a pointer to a copy of *p, or nil for nil.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// SetLayerSpace sets the space the layer name draws and hit-tests in, SpaceWorld (under the scene's camera) or SpaceScreen (at virtual screen coordinates, as a HUD), as an undoable command; its objects keep their coordinates. It changes nothing when the layer already has that space, its field left unset included for SpaceWorld. In a scene that inherits another it refuses a layer that extends one of the base's, whose space the base and the scene's layer overrides own.
func (e *Editor) SetLayerSpace(name string, space sceneformat.Space) error {
	if e.playing {
		return ErrPlaying
	}
	if !validSpace(space) {
		return fmt.Errorf("editor: layer space %q must be %q or %q", space, sceneformat.SpaceWorld, sceneformat.SpaceScreen)
	}
	li := e.layerIndex(name)
	if li < 0 {
		return fmt.Errorf("editor: layer %q not found", name)
	}
	if e.inheritedLayers()[name] {
		return e.inheritedErr(name, "space")
	}
	l := &e.Doc.Layers[li]
	if l.EffectiveSpace() == space {
		return nil
	}
	candidate := e.Doc.Clone()
	candidate.Layers[li].Space = space
	if _, err := e.checkLayerEdit(candidate); err != nil {
		return err
	}
	e.History.Execute(&layerSpaceCmd{ed: e, name: name, old: l.Space, new: space})
	return nil
}

type layerSpaceCmd struct {
	ed       *Editor
	name     string
	old, new sceneformat.Space
}

func (c *layerSpaceCmd) Label() string { return "Set layer " + c.name + " space to " + string(c.new) }

func (c *layerSpaceCmd) Do() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		c.ed.Doc.Layers[li].Space = c.new
	}
	c.ed.Status = "Layer " + c.name + " space: " + string(c.new)
}

func (c *layerSpaceCmd) Undo() {
	if li := c.ed.layerIndex(c.name); li >= 0 {
		c.ed.Doc.Layers[li].Space = c.old
	}
}

// SetLayerLocked locks the layer name against the viewports, or unlocks it: a press in either viewport passes over its objects, so it picks what lies under them, its objects are not moved there (no gizmo, no Move-tool drag), and the Place, Paint, Erase and Fill tools do nothing while it is the active layer. Like SetObjectLocked, the lock belongs to the editing session: it is not an edit, is not undone, and is never written to the scene file; opening another scene clears it. The layers panel and the scene tree still select and edit the layer and its objects.
func (e *Editor) SetLayerLocked(name string, locked bool) error {
	if e.layerIndex(name) < 0 {
		return fmt.Errorf("editor: layer %q not found", name)
	}
	if e.layerLocked[name] == locked {
		return nil
	}
	if locked && e.stroke != nil && e.stroke.layer == name && e.stroke.mode != TileStrokePick {
		e.endTileStrokeForChange()
	}
	e.setLayerLockedFlag(name, locked)
	e.notify()
	return nil
}

func (e *Editor) setLayerLockedFlag(name string, locked bool) {
	if !locked {
		delete(e.layerLocked, name)
		return
	}
	if e.layerLocked == nil {
		e.layerLocked = make(map[string]bool)
	}
	e.layerLocked[name] = true
}

// LayerLocked reports whether the layer name is locked against the viewports (SetLayerLocked).
func (e *Editor) LayerLocked(name string) bool { return e.layerLocked[name] }

// keepActiveLayer makes the first layer the active one when ActiveLayer names no layer of the open scene, as after undoing the layer's addition, and clears it when the scene has none.
func (e *Editor) keepActiveLayer() {
	if e.Doc == nil || e.layerIndex(e.ActiveLayer) >= 0 {
		return
	}
	e.ActiveLayer = ""
	if len(e.Doc.Layers) > 0 {
		e.ActiveLayer = e.Doc.Layers[0].Name
	}
	e.keepTileInPalette()
}

// layerDrawn reports whether the open scene draws its layer name, as the visible field of the layer the scene draws says (effectiveLayers), so that in a scene that inherits another the base and the layer overrides count; it reports true for a name no layer has.
func (e *Editor) layerDrawn(name string) bool {
	layers := e.effectiveLayers()
	if i := slices.IndexFunc(layers, func(l sceneformat.LayerDoc) bool { return l.Name == name }); i >= 0 {
		return layers[i].IsVisible()
	}
	return true
}

// anyLayerHidden reports whether the open scene leaves one of its layers undrawn (layerDrawn).
func (e *Editor) anyLayerHidden() bool {
	if e.Doc == nil {
		return false
	}
	return slices.ContainsFunc(e.effectiveLayers(), func(l sceneformat.LayerDoc) bool { return !l.IsVisible() })
}

// lockedLayerErr returns why a viewport tool cannot edit the active layer, or nil when it can.
func (e *Editor) lockedLayerErr() error {
	if e.layerLocked[e.ActiveLayer] {
		return fmt.Errorf("editor: layer %q is locked", e.ActiveLayer)
	}
	return nil
}

// activeLayerReadout is how the status bar names the active layer: its name, with "hidden" when the scene does not draw it and "locked" when it is locked (SetLayerLocked), or "none" for a scene without layers.
func (e *Editor) activeLayerReadout() string {
	li := e.layerIndex(e.ActiveLayer)
	if li < 0 {
		return "none"
	}
	var flags []string
	if !e.layerDrawn(e.Doc.Layers[li].Name) {
		flags = append(flags, "hidden")
	}
	if e.layerLocked[e.ActiveLayer] {
		flags = append(flags, "locked")
	}
	if len(flags) == 0 {
		return e.ActiveLayer
	}
	return e.ActiveLayer + " (" + strings.Join(flags, ", ") + ")"
}
