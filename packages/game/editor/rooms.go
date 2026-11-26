package editor

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrNoRooms is returned by room-map edits and saves when no room map is open.
var ErrNoRooms = errors.New("editor: no room map is open")

// ErrNoRoomsFile is returned by SaveRooms when the open room map has no file path and no OnSaveRooms callback.
var ErrNoRoomsFile = errors.New("editor: the room map has no file; save it under a path first")

const minRoomSize float32 = 1

type roomMapState struct {
	doc            *rooms.Map
	path           string
	fromDisk       bool
	saved          []byte
	selectedRoomID string
	snapToGrid     bool
	createMode     bool
	gridW, gridH   float32
	lastErr        error
}

type roomDragState struct {
	active      bool
	moved       bool
	creating    bool
	roomID      string
	handle      RectHandle
	startWorldX float32
	startWorldY float32
	origRoom    rooms.Room
	previewX    float32
	previewY    float32
	previewW    float32
	previewH    float32
}

// isRoomsFile reports whether rel names a *.rooms.yaml or *.rooms.yml file.
func isRoomsFile(rel string) bool {
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".rooms.yaml") || strings.HasSuffix(lower, ".rooms.yml")
}

// Rooms returns a deep copy of the currently open room map, or nil when none is open.
func (e *Editor) Rooms() *rooms.Map {
	if e == nil || e.rooms == nil || e.rooms.doc == nil {
		return nil
	}
	return cloneRoomMap(e.rooms.doc)
}

// RoomsPath returns the file path or logical name of the open room map, or "".
func (e *Editor) RoomsPath() string {
	if e == nil || e.rooms == nil {
		return ""
	}
	return e.rooms.path
}

// RoomsLabel returns a short display label for the open room map (project-relative path
// when inside the open project, else file path, else "Untitled rooms").
func (e *Editor) RoomsLabel() string {
	if e == nil || e.rooms == nil {
		return ""
	}
	if e.rooms.path != "" {
		if rel, ok := e.projectRel(e.rooms.path); ok {
			return rel
		}
		return e.rooms.path
	}
	return "Untitled rooms"
}

// RoomsDirty reports whether an open room map differs from what it was opened from or last saved to.
func (e *Editor) RoomsDirty() bool {
	if e == nil || e.rooms == nil || e.rooms.doc == nil {
		return false
	}
	data, err := rooms.Encode(e.rooms.doc)
	return err != nil || !bytes.Equal(data, e.rooms.saved)
}

// RoomsError returns the most recent validation error from editing the open room map, or nil.
func (e *Editor) RoomsError() error {
	if e == nil || e.rooms == nil {
		return nil
	}
	return e.rooms.lastErr
}

// ProjectRoomsFiles lists all *.rooms.yaml and *.rooms.yml files in the open project.
func (e *Editor) ProjectRoomsFiles() []string {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil
	}
	files, err := e.ProjectFiles()
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range files {
		if !f.Dir && isRoomsFile(f.Path) {
			out = append(out, f.Path)
		}
	}
	return out
}

// OpenRoomsFile reads a *.rooms.yaml file from path, parses it with rooms.Parse, and opens
// it beside the current scene in the 2D viewport.
func (e *Editor) OpenRoomsFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return e.openRooms(filePath, data, true)
}

// OpenRoomsProjectFile opens relPath (slash-separated from the open project's root) beside the scene.
func (e *Editor) OpenRoomsProjectFile(relPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	abs, err := e.projectFile(relPath)
	if err != nil {
		return err
	}
	if err := e.checkProjectSymlink(abs, true); err != nil {
		return err
	}
	return e.OpenRoomsFile(abs)
}

// checkProjectSymlink verifies that absPath does not escape the open project's root via symlinks.
// When mustContain is true (project-relative reads), absPath must resolve strictly inside the project root.
// When mustContain is false (writes via SaveRoomsFile), containment is enforced whenever absPath is
// lexically inside the project root so symlinks inside the project cannot redirect writes outside.
func (e *Editor) checkProjectSymlink(absPath string, mustContain bool) error {
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	root := filepath.Clean(filepath.Dir(e.ProjectPath))
	cleanPath := filepath.Clean(absPath)
	if !mustContain {
		lexRel, err := filepath.Rel(root, cleanPath)
		if err != nil || lexRel == ".." || strings.HasPrefix(lexRel, ".."+string(filepath.Separator)) || filepath.IsAbs(lexRel) {
			return nil
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	var realPath string
	if mustContain {
		realPath, err = filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return err
		}
	} else {
		realPath, err = evalSymlinksOrParent(cleanPath)
		if err != nil {
			return err
		}
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("editor: room map %q resolves outside the project", absPath)
	}
	return nil
}

func evalSymlinksOrParent(p string) (string, error) {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if fi, lerr := os.Lstat(p); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("editor: refusing to follow unresolved symlink %q", p)
	}
	cur := p
	var tail []string
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			slices.Reverse(tail)
			parts := append([]string{real}, tail...)
			return filepath.Join(parts...), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if fi, lerr := os.Lstat(cur); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("editor: refusing to follow unresolved symlink %q", cur)
		}
	}
}

// OpenRoomsYAML parses data with rooms.Parse and opens the room map beside the current scene.
func (e *Editor) OpenRoomsYAML(name string, data []byte) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	return e.openRooms(name, data, false)
}

func (e *Editor) openRooms(filePath string, data []byte, fromDisk bool) error {
	m, err := rooms.Parse(data)
	if err != nil {
		return err
	}
	encoded, err := rooms.Encode(m)
	if err != nil {
		return err
	}
	e.CancelRoomDrag()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.rooms = &roomMapState{
		doc:        m,
		path:       filePath,
		fromDisk:   fromDisk,
		saved:      encoded,
		snapToGrid: true,
	}
	if filePath != "" {
		e.Status = "Opened rooms " + filePath
	} else {
		e.Status = "Opened room map"
	}
	e.notify()
	return nil
}

// NewRoomMap creates a new valid room map beside the current scene with one starter room
// sized to the scene's virtual viewport (snapped to the tile grid when present) and selects it.
func (e *Editor) NewRoomMap(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelRoomDrag()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()

	vw, vh := e.ViewportSize()
	w, h := vw, vh
	if gw, gh, ok := e.RoomGridSize(); ok {
		w = snapCoord(w, gw)
		if w < gw {
			w = gw
		}
		h = snapCoord(h, gh)
		if h < gh {
			h = gh
		}
	}
	m := &rooms.Map{
		Version: rooms.FormatVersion,
		Rooms: []rooms.Room{
			{
				ID:     "room_1",
				X:      0,
				Y:      0,
				Width:  w,
				Height: h,
			},
		},
	}
	e.rooms = &roomMapState{
		doc:            m,
		path:           filePath,
		fromDisk:       filePath != "",
		saved:          nil, // dirty until saved
		selectedRoomID: "room_1",
		snapToGrid:     true,
	}
	e.clearSelectionFields()
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	e.Status = "Created new room map"
	e.notify()
	return nil
}

// CloseRooms closes the open room map beside the scene.
func (e *Editor) CloseRooms() {
	if e == nil || e.rooms == nil {
		return
	}
	e.CancelRoomDrag()
	e.rooms = nil
	e.Status = "Closed room map"
	e.notify()
}

// SaveRoomsYAML validates and encodes the open room map to deterministic YAML using rooms.Encode.
func (e *Editor) SaveRoomsYAML() ([]byte, error) {
	if e == nil || e.rooms == nil || e.rooms.doc == nil {
		return nil, ErrNoRooms
	}
	if !e.playing {
		e.CancelRoomDrag()
	}
	data, err := rooms.Encode(e.rooms.doc)
	if err != nil {
		e.rooms.lastErr = err
		return nil, err
	}
	e.rooms.lastErr = nil
	return data, nil
}

// SaveRoomsFile encodes the open room map to deterministic YAML and writes it atomically to filePath.
func (e *Editor) SaveRoomsFile(filePath string) error {
	if e == nil || e.rooms == nil {
		return ErrNoRooms
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkProjectSymlink(filePath, false); err != nil {
		return err
	}
	data, err := e.SaveRoomsYAML()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(filePath, data); err != nil {
		return err
	}
	e.rooms.path = filePath
	e.rooms.fromDisk = true
	e.rooms.saved = data
	e.rooms.lastErr = nil
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved rooms " + filePath
	e.notify()
	return nil
}

// SaveRooms persists the open room map to OnSaveRooms (when set) or its file path.
func (e *Editor) SaveRooms() error {
	if e == nil || e.rooms == nil {
		return ErrNoRooms
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelRoomDrag()
	if e.OnSaveRooms != nil {
		data, err := e.SaveRoomsYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveRooms(data); err != nil {
			return err
		}
		e.rooms.saved = data
		e.rooms.lastErr = nil
		e.Status = "Saved rooms"
		e.notify()
		return nil
	}
	if e.rooms.path == "" {
		return ErrNoRoomsFile
	}
	return e.SaveRoomsFile(e.rooms.path)
}

// SelectedRoomID returns the ID of the currently selected room in the open room map, or "".
func (e *Editor) SelectedRoomID() string {
	if e == nil || e.rooms == nil {
		return ""
	}
	return e.rooms.selectedRoomID
}

// SelectedRoom returns a copy of the currently selected room, or nil when none is selected.
func (e *Editor) SelectedRoom() *rooms.Room {
	if e == nil || e.rooms == nil || e.rooms.doc == nil || e.rooms.selectedRoomID == "" {
		return nil
	}
	r := e.rooms.doc.Room(e.rooms.selectedRoomID)
	if r == nil {
		return nil
	}
	cp := cloneRoom(*r)
	return &cp
}

// SelectRoom selects the room with ID id in the open room map (or clears room selection
// when id == ""). Selecting a room deselects any scene object, layer, or asset file so the
// inspector displays the room's properties.
func (e *Editor) SelectRoom(id string) bool {
	if e == nil || e.rooms == nil || e.rooms.doc == nil {
		return id == ""
	}
	if id == "" {
		if e.rooms.selectedRoomID != "" {
			e.CancelRoomDrag()
			e.rooms.selectedRoomID = ""
			e.notify()
		}
		return true
	}
	if e.rooms.doc.Room(id) == nil {
		return false
	}
	if e.rooms.selectedRoomID != id || e.SelectedID != "" || e.inspectLayer || e.inspectAssetFile || e.connPick != nil {
		e.CancelDrag2D()
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
		e.clearSelectionFields()
		e.inspectAssetFile = false
		e.inspectLayer = false
		e.connPick = nil
		e.rooms.selectedRoomID = id
		e.Status = "Selected room " + id
		e.notify()
	}
	return true
}

// RoomSnapToGrid reports whether moving, resizing, and drawing rooms in the 2D viewport
// snaps room edges to the scene's tile grid (default true).
func (e *Editor) RoomSnapToGrid() bool {
	if e == nil || e.rooms == nil {
		return true
	}
	return e.rooms.snapToGrid
}

// SetRoomSnapToGrid enables or disables snapping room edges to the tile grid.
func (e *Editor) SetRoomSnapToGrid(on bool) {
	if e == nil || e.rooms == nil || e.rooms.snapToGrid == on {
		return
	}
	e.rooms.snapToGrid = on
	if on {
		e.Status = "Room grid snap on"
	} else {
		e.Status = "Room grid snap off"
	}
	e.notify()
}

// RoomCreateMode reports whether dragging in the 2D viewport draws a new room rectangle.
func (e *Editor) RoomCreateMode() bool {
	return e != nil && e.rooms != nil && e.rooms.createMode
}

// SetRoomCreateMode toggles viewport drag-to-create mode for rooms in the open room map.
func (e *Editor) SetRoomCreateMode(on bool) {
	if e == nil || e.rooms == nil || e.rooms.createMode == on {
		return
	}
	e.CancelRoomDrag()
	e.CancelCameraDrag()
	e.CancelCameraBoundsDrag()
	e.CancelDrag2D()
	e.rooms.createMode = on
	if on {
		e.Status = "Draw Room: drag in the 2D viewport to create a room"
	} else {
		e.Status = "Draw Room off"
	}
	e.notify()
}

// SetRoomGridSize sets an explicit tile-grid snap size (w, h) for rooms when the scene has
// no tilemap or to override the tilemap's tile size; pass (0, 0) to follow the scene's tilemap.
func (e *Editor) SetRoomGridSize(w, h float32) {
	if e == nil || e.rooms == nil {
		return
	}
	if w <= 0 || h <= 0 || !isFiniteFloat32(w) || !isFiniteFloat32(h) {
		e.rooms.gridW, e.rooms.gridH = 0, 0
	} else {
		e.rooms.gridW, e.rooms.gridH = w, h
	}
	e.notify()
}

// RoomGridSize returns the active tile grid step (w, h) used to snap room edges: an
// explicit override set by SetRoomGridSize, else the active/first inline tilemap's tile
// size, else the first built external tilemap's tile size. Returns (0, 0, false) when
// neither an explicit grid nor a tilemap is present.
func (e *Editor) RoomGridSize() (w, h float32, ok bool) {
	if e == nil {
		return 0, 0, false
	}
	if e.rooms != nil && e.rooms.gridW > 0 && e.rooms.gridH > 0 {
		return e.rooms.gridW, e.rooms.gridH, true
	}
	if _, tm, err := e.resolveInlineTilemap(""); err == nil && tm != nil && tm.TileWidth > 0 && tm.TileHeight > 0 {
		return float32(tm.TileWidth), float32(tm.TileHeight), true
	}
	if scene := e.ActiveScene(); scene != nil {
		for _, layer := range scene.Layers {
			if layer.Map != nil && layer.Map.TileWidth > 0 && layer.Map.TileHeight > 0 {
				return layer.Map.TileWidth, layer.Map.TileHeight, true
			}
		}
	}
	return 0, 0, false
}

func (e *Editor) effectiveRoomSnap() (float32, float32) {
	if !e.RoomSnapToGrid() {
		return 0, 0
	}
	gw, gh, ok := e.RoomGridSize()
	if !ok {
		return 0, 0
	}
	return gw, gh
}

func (e *Editor) generateRoomID(base string) string {
	if base == "" {
		base = "room"
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if e.rooms == nil || e.rooms.doc == nil || e.rooms.doc.Room(candidate) == nil {
			return candidate
		}
	}
}

func (e *Editor) validateRoomMapCandidate(candidate *rooms.Map, allowEmptyRooms bool) error {
	if allowEmptyRooms && candidate != nil && len(candidate.Rooms) == 0 {
		if candidate.Version != rooms.FormatVersion {
			return fmt.Errorf("%w: unsupported version %d (want %d)", rooms.ErrInvalidMap, candidate.Version, rooms.FormatVersion)
		}
		if candidate.Transition != nil {
			probe := &rooms.Map{
				Version:    candidate.Version,
				Transition: candidate.Transition,
				Rooms:      []rooms.Room{{ID: "probe", Width: 1, Height: 1}},
			}
			return rooms.Validate(probe)
		}
		return nil
	}
	return rooms.Validate(candidate)
}

func (e *Editor) failRoom(err error) {
	if e != nil && e.rooms != nil {
		e.rooms.lastErr = err
	}
}

// AddRoom appends r to the open room map as an undoable command, generating a unique ID
// when r.ID is empty, validating the map with rooms.Validate, and selecting the new room.
func (e *Editor) AddRoom(r rooms.Room) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return "", ErrNoRooms
	}
	e.CancelRoomDrag()
	added := cloneRoom(r)
	if added.ID == "" {
		added.ID = e.generateRoomID("room")
	}
	added.X = normalizeZero(added.X)
	added.Y = normalizeZero(added.Y)
	added.Width = normalizeZero(added.Width)
	added.Height = normalizeZero(added.Height)

	next := cloneRoomMap(e.rooms.doc)
	next.Rooms = append(next.Rooms, added)
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		return "", err
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:         e,
		target:     e.rooms,
		label:      "Add room " + added.ID,
		status:     "Added room " + added.ID,
		before:     cloneRoomMap(e.rooms.doc),
		after:      next,
		beforeSel:  e.rooms.selectedRoomID,
		afterSel:   added.ID,
		selectRoom: true,
	})
	return added.ID, nil
}

// AddRoomAtViewCenter adds a new room centered on the current 2D viewport view, sized to
// the scene's virtual viewport (or snapped to the tile grid), as an undoable command.
func (e *Editor) AddRoomAtViewCenter() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return "", ErrNoRooms
	}
	v := e.View2D()
	vw, vh := e.ViewportSize()
	snapW, snapH := e.effectiveRoomSnap()
	w := snapCoord(vw, snapW)
	if w < max(minRoomSize, snapW) {
		w = max(minRoomSize, snapW)
	}
	h := snapCoord(vh, snapH)
	if h < max(minRoomSize, snapH) {
		h = max(minRoomSize, snapH)
	}
	x := snapCoord(v.X-w/2, snapW)
	y := snapCoord(v.Y-h/2, snapH)
	return e.AddRoom(rooms.Room{
		X:      x,
		Y:      y,
		Width:  w,
		Height: h,
	})
}

// DeleteRoom removes the room with ID id from the open room map as an undoable command.
func (e *Editor) DeleteRoom(id string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	idx := slices.IndexFunc(e.rooms.doc.Rooms, func(r rooms.Room) bool { return r.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: room %q not found", id)
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Rooms = append(next.Rooms[:idx], next.Rooms[idx+1:]...)
	afterSel := e.rooms.selectedRoomID
	if afterSel == id {
		afterSel = ""
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:         e,
		target:     e.rooms,
		label:      "Delete room " + id,
		status:     "Deleted room " + id,
		before:     cloneRoomMap(e.rooms.doc),
		after:      next,
		beforeSel:  e.rooms.selectedRoomID,
		afterSel:   afterSel,
		selectRoom: true,
	})
	return nil
}

// SetRoomRect updates room id's rectangle (x, y, w, h) in world points as an undoable command.
func (e *Editor) SetRoomRect(id string, x, y, w, h float32) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	idx := slices.IndexFunc(e.rooms.doc.Rooms, func(r rooms.Room) bool { return r.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: room %q not found", id)
	}
	x, y, w, h = normalizeZero(x), normalizeZero(y), normalizeZero(w), normalizeZero(h)
	cur := &e.rooms.doc.Rooms[idx]
	if cur.X == x && cur.Y == y && cur.Width == w && cur.Height == h {
		return nil
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Rooms[idx].X = x
	next.Rooms[idx].Y = y
	next.Rooms[idx].Width = w
	next.Rooms[idx].Height = h
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		return err
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:        e,
		target:    e.rooms,
		label:     "Resize room " + id,
		status:    "Updated room " + id,
		before:    cloneRoomMap(e.rooms.doc),
		after:     next,
		beforeSel: e.rooms.selectedRoomID,
		afterSel:  e.rooms.selectedRoomID,
	})
	return nil
}

// SetRoomID renames room oldID to newID as an undoable command, refusing empty, whitespace-padded,
// or duplicate IDs via rooms.Validate.
func (e *Editor) SetRoomID(oldID, newID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	idx := slices.IndexFunc(e.rooms.doc.Rooms, func(r rooms.Room) bool { return r.ID == oldID })
	if idx < 0 {
		return fmt.Errorf("editor: room %q not found", oldID)
	}
	if oldID == newID {
		return nil
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Rooms[idx].ID = newID
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		return err
	}
	afterSel := e.rooms.selectedRoomID
	if afterSel == oldID {
		afterSel = newID
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:         e,
		target:     e.rooms,
		label:      "Rename room " + oldID,
		status:     "Renamed room to " + newID,
		before:     cloneRoomMap(e.rooms.doc),
		after:      next,
		beforeSel:  e.rooms.selectedRoomID,
		afterSel:   afterSel,
		selectRoom: true,
	})
	return nil
}

// SetRoomName updates room id's display name or localization key as an undoable command.
func (e *Editor) SetRoomName(id, name string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	idx := slices.IndexFunc(e.rooms.doc.Rooms, func(r rooms.Room) bool { return r.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: room %q not found", id)
	}
	if e.rooms.doc.Rooms[idx].Name == name {
		return nil
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Rooms[idx].Name = name
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		return err
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:        e,
		target:    e.rooms,
		label:     "Set room name " + id,
		status:    "Updated room name",
		before:    cloneRoomMap(e.rooms.doc),
		after:     next,
		beforeSel: e.rooms.selectedRoomID,
		afterSel:  e.rooms.selectedRoomID,
	})
	return nil
}

// SetRoomTransition sets or clears (when tr is nil) room id's transition override as an
// undoable command, checked by rooms.Validate.
func (e *Editor) SetRoomTransition(id string, tr *rooms.Transition) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	idx := slices.IndexFunc(e.rooms.doc.Rooms, func(r rooms.Room) bool { return r.ID == id })
	if idx < 0 {
		return fmt.Errorf("editor: room %q not found", id)
	}
	if transitionEqual(e.rooms.doc.Rooms[idx].Transition, tr) {
		return nil
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Rooms[idx].Transition = cloneTransition(tr)
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		return err
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:        e,
		target:    e.rooms,
		label:     "Set room transition " + id,
		status:    "Updated room transition",
		before:    cloneRoomMap(e.rooms.doc),
		after:     next,
		beforeSel: e.rooms.selectedRoomID,
		afterSel:  e.rooms.selectedRoomID,
	})
	return nil
}

// SetRoomMapTransition sets or clears (when tr is nil) the open room map's default transition
// as an undoable command, checked by rooms.Validate.
func (e *Editor) SetRoomMapTransition(tr *rooms.Transition) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rooms == nil || e.rooms.doc == nil {
		return ErrNoRooms
	}
	e.CancelRoomDrag()
	if transitionEqual(e.rooms.doc.Transition, tr) {
		return nil
	}
	next := cloneRoomMap(e.rooms.doc)
	next.Transition = cloneTransition(tr)
	if err := e.validateRoomMapCandidate(next, true); err != nil {
		e.failRoom(err)
		return err
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:        e,
		target:    e.rooms,
		label:     "Set map transition",
		status:    "Updated room map transition",
		before:    cloneRoomMap(e.rooms.doc),
		after:     next,
		beforeSel: e.rooms.selectedRoomID,
		afterSel:  e.rooms.selectedRoomID,
	})
	return nil
}

// --- Viewport hit-testing and drag for rooms ---

// RoomHandleCanvasPos returns the canvas coordinates of handle h on roomID in the open room map.
func (e *Editor) RoomHandleCanvasPos(roomID string, h RectHandle) (x, y float32, ok bool) {
	if e == nil || e.rooms == nil || e.rooms.doc == nil || e.playing || e.ViewMode != ViewMode2D {
		return 0, 0, false
	}
	r := e.rooms.doc.Room(roomID)
	if r == nil {
		return 0, 0, false
	}
	wx, wy := rectHandleWorldPos(h, r.X, r.Y, r.Width, r.Height)
	cx, cy := e.WorldToCanvas(wx, wy)
	return cx, cy, true
}

// HitTestRoom returns the ID of the first room in list order containing canvas point
// (canvasX, canvasY), matching rooms.Map.RoomAt semantics, or "" if none does.
func (e *Editor) HitTestRoom(canvasX, canvasY float32) string {
	if e == nil || e.rooms == nil || e.rooms.doc == nil || e.playing || e.ViewMode != ViewMode2D {
		return ""
	}
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	r := e.rooms.doc.RoomAt(core.Point{X: core.Points(wx), Y: core.Points(wy)})
	if r == nil {
		return ""
	}
	return r.ID
}

// HitTestRoomHandle tests canvas point (canvasX, canvasY) against the selected room's 8
// resize handles, then its interior/border, then any other room's interior.
func (e *Editor) HitTestRoomHandle(canvasX, canvasY float32) (string, RectHandle) {
	if e == nil || e.rooms == nil || e.rooms.doc == nil || e.playing || e.ViewMode != ViewMode2D {
		return "", RectHandleNone
	}
	if e.rooms.selectedRoomID != "" {
		if r := e.rooms.doc.Room(e.rooms.selectedRoomID); r != nil {
			if h := e.hitTestWorldRect(r.X, r.Y, r.Width, r.Height, canvasX, canvasY, true, true, true); h != RectHandleNone {
				return r.ID, h
			}
		}
	}
	if id := e.HitTestRoom(canvasX, canvasY); id != "" {
		return id, RectHandleBody
	}
	return "", RectHandleNone
}

// DraggingRoom reports the room ID and handle currently being dragged, or ("", RectHandleNone).
func (e *Editor) DraggingRoom() (string, RectHandle) {
	if e == nil || !e.roomDrag.active {
		return "", RectHandleNone
	}
	return e.roomDrag.roomID, e.roomDrag.handle
}

// BeginRoomDrag starts dragging a room handle, moving a room, or drawing a new room (when
// RoomCreateMode is active) in the 2D viewport.
func (e *Editor) BeginRoomDrag(canvasX, canvasY float32) bool {
	if e == nil || e.rooms == nil || e.rooms.doc == nil || e.playing || e.ViewMode != ViewMode2D {
		return false
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	if e.rooms.createMode {
		wx, wy := e.CanvasToWorld(canvasX, canvasY)
		snapW, snapH := e.effectiveRoomSnap()
		sx, sy := snapCoord(wx, snapW), snapCoord(wy, snapH)
		e.roomDrag = roomDragState{
			active:      true,
			creating:    true,
			startWorldX: sx,
			startWorldY: sy,
			previewX:    sx,
			previewY:    sy,
			previewW:    max(minRoomSize, snapW),
			previewH:    max(minRoomSize, snapH),
		}
		e.notify()
		return true
	}
	if e.Tool != ToolSelect && e.Tool != ToolMove {
		return false
	}
	roomID, h := e.HitTestRoomHandle(canvasX, canvasY)
	if roomID == "" || h == RectHandleNone {
		return false
	}
	r := e.rooms.doc.Room(roomID)
	if r == nil {
		return false
	}
	if e.rooms.selectedRoomID != roomID || e.SelectedID != "" || e.inspectLayer || e.inspectAssetFile {
		e.clearSelectionFields()
		e.inspectLayer = false
		e.inspectAssetFile = false
		e.connPick = nil
		e.rooms.selectedRoomID = roomID
	}
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	e.roomDrag = roomDragState{
		active:      true,
		roomID:      roomID,
		handle:      h,
		startWorldX: wx,
		startWorldY: wy,
		origRoom:    cloneRoom(*r),
	}
	e.notify()
	return true
}

// UpdateRoomDrag updates the live room move/resize or room-creation preview to follow
// canvas point (canvasX, canvasY), snapping room edges to the tile grid when enabled.
func (e *Editor) UpdateRoomDrag(canvasX, canvasY float32) bool {
	if e == nil || !e.roomDrag.active || e.playing || e.rooms == nil || e.rooms.doc == nil {
		return false
	}
	if !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	snapW, snapH := e.effectiveRoomSnap()

	if e.roomDrag.creating {
		ex, ey := snapCoord(wx, snapW), snapCoord(wy, snapH)
		left := min(e.roomDrag.startWorldX, ex)
		top := min(e.roomDrag.startWorldY, ey)
		right := max(e.roomDrag.startWorldX, ex)
		bottom := max(e.roomDrag.startWorldY, ey)
		minW := max(minRoomSize, snapW)
		minH := max(minRoomSize, snapH)
		if right-left < minW {
			right = left + minW
		}
		if bottom-top < minH {
			bottom = top + minH
		}
		e.roomDrag.previewX = normalizeZero(left)
		e.roomDrag.previewY = normalizeZero(top)
		e.roomDrag.previewW = normalizeZero(right - left)
		e.roomDrag.previewH = normalizeZero(bottom - top)
		e.roomDrag.moved = true
		e.notify()
		return true
	}

	r := e.rooms.doc.Room(e.roomDrag.roomID)
	if r == nil {
		e.CancelRoomDrag()
		return false
	}
	orig := e.roomDrag.origRoom
	nx, ny, nw, nh := applyRectDrag(
		e.roomDrag.handle,
		orig.X, orig.Y, orig.Width, orig.Height,
		e.roomDrag.startWorldX, e.roomDrag.startWorldY,
		wx, wy,
		minRoomSize, minRoomSize,
		snapW, snapH,
	)
	if !isFiniteFloat32(nx) || !isFiniteFloat32(ny) || !isFiniteFloat32(nw) || !isFiniteFloat32(nh) {
		return false
	}
	if nx != orig.X || ny != orig.Y || nw != orig.Width || nh != orig.Height {
		e.roomDrag.moved = true
	}
	r.X, r.Y, r.Width, r.Height = nx, ny, nw, nh
	e.notify()
	return true
}

// EndRoomDrag finishes the active room drag and commits the move, resize, or newly created
// room as a single undoable History step. Returns true when a change was committed.
func (e *Editor) EndRoomDrag() bool {
	if e == nil || !e.roomDrag.active {
		return false
	}
	st := e.roomDrag
	e.roomDrag = roomDragState{}

	if e.rooms == nil || e.rooms.doc == nil {
		e.notify()
		return false
	}

	if st.creating {
		if !st.moved || st.previewW <= 0 || st.previewH <= 0 {
			e.notify()
			return false
		}
		_, err := e.AddRoom(rooms.Room{
			X:      st.previewX,
			Y:      st.previewY,
			Width:  st.previewW,
			Height: st.previewH,
		})
		if err != nil {
			e.report(err)
			return false
		}
		e.rooms.createMode = false
		e.notify()
		return true
	}

	r := e.rooms.doc.Room(st.roomID)
	if r == nil {
		e.notify()
		return false
	}
	afterRect := [4]float32{r.X, r.Y, r.Width, r.Height}
	// Restore pre-drag room before committing through History.
	*r = cloneRoom(st.origRoom)
	if !st.moved || (afterRect[0] == st.origRoom.X && afterRect[1] == st.origRoom.Y && afterRect[2] == st.origRoom.Width && afterRect[3] == st.origRoom.Height) {
		e.notify()
		return false
	}
	next := cloneRoomMap(e.rooms.doc)
	nr := next.Room(st.roomID)
	nr.X, nr.Y, nr.Width, nr.Height = afterRect[0], afterRect[1], afterRect[2], afterRect[3]
	if err := rooms.Validate(next); err != nil {
		e.failRoom(err)
		e.report(err)
		return false
	}
	label := "Resize room " + st.roomID
	if st.handle == RectHandleBody {
		label = "Move room " + st.roomID
	}
	e.rooms.lastErr = nil
	e.History.Execute(&roomMapEditCmd{
		ed:        e,
		target:    e.rooms,
		label:     label,
		status:    label,
		before:    cloneRoomMap(e.rooms.doc),
		after:     next,
		beforeSel: e.rooms.selectedRoomID,
		afterSel:  e.rooms.selectedRoomID,
	})
	return true
}

// CancelRoomDrag aborts any active room drag or room-create drag and restores the pre-drag room.
func (e *Editor) CancelRoomDrag() {
	if e == nil || !e.roomDrag.active {
		return
	}
	st := e.roomDrag
	e.roomDrag = roomDragState{}
	if !st.creating && e.rooms != nil && e.rooms.doc != nil {
		if r := e.rooms.doc.Room(st.roomID); r != nil {
			*r = cloneRoom(st.origRoom)
		}
	}
	e.notify()
}

// --- Viewport overlay drawing for rooms ---

func (e *Editor) drawRoomsOverlay(d *core.Drawing) {
	if e == nil || e.rooms == nil || e.rooms.doc == nil {
		return
	}
	d.Save()
	font := core.Font{Size: 11, Weight: 700}

	// Draw subtle tile grid when drawing a new room or actively dragging a room with grid snap enabled.
	if (e.rooms.createMode || e.roomDrag.active) && e.RoomSnapToGrid() {
		if gw, gh, ok := e.RoomGridSize(); ok {
			e.drawRoomSnapGrid(d, gw, gh)
		}
	}

	spec := e.activeSpecPalette()
	for i := range e.rooms.doc.Rooms {
		r := &e.rooms.doc.Rooms[i]
		selected := r.ID == e.rooms.selectedRoomID
		pts := worldRectCanvasCorners(e, r.X, r.Y, r.Width, r.Height)

		// Fill room rectangle.
		if selected {
			d.SetFillColor(specAlpha(spec.Added.Fg, 0.20))
		} else {
			d.SetFillColor(specAlpha(spec.Added.Fg, 0.10))
		}
		d.BeginPath().MoveTo(pts[0].X, pts[0].Y)
		for _, p := range pts[1:] {
			d.LineTo(p.X, p.Y)
		}
		d.ClosePath().Fill()

		// Stroke room border.
		if selected {
			d.SetStrokeColor(spec.Added.Edge).SetLineWidth(2)
		} else {
			d.SetStrokeColor(specAlpha(spec.Added.Edge, 0.85)).SetLineWidth(1.5)
		}
		strokePolygon(d, pts)

		// Draw room label (ID / Name + transition tag) near the top-left corner.
		label := r.ID
		if r.Name != "" && r.Name != r.ID {
			label = r.ID + " · " + r.Name
		}
		tr := e.rooms.doc.TransitionInto(r)
		if r.Transition != nil {
			label += " [" + string(r.Transition.Kind) + "]"
		} else if e.rooms.doc.Transition != nil {
			label += " (" + string(tr.Kind) + ")"
		}
		lx, ly := e.WorldToCanvas(r.X, r.Y)
		d.SetFillColor(specAlpha(spec.PillBg, 0.88))
		badgeW := float32(len(label))*6.5 + 10
		d.FillRect(lx+4, ly+4, badgeW, 18)
		d.SetFillColor(spec.Added.Fg).
			SetTextAlign(core.TextAlignLeft).
			SetTextBaseline(core.BaselineTop).
			FillText(label, lx+9, ly+7, font)

		// Draw 8 resize handles on the selected room.
		if selected && (e.Tool == ToolSelect || e.Tool == ToolMove) && !e.rooms.createMode {
			drawWorldRectHandles(e, d, r.X, r.Y, r.Width, r.Height, e.roomDrag.handle, spec.TextPrimary, spec.Added.Edge, spec.Added.Fg)
		}
	}

	// Draw live creation preview rectangle while dragging in Draw Room mode.
	if e.roomDrag.active && e.roomDrag.creating && e.roomDrag.moved {
		pts := worldRectCanvasCorners(e, e.roomDrag.previewX, e.roomDrag.previewY, e.roomDrag.previewW, e.roomDrag.previewH)
		d.SetFillColor(specAlpha(spec.Added.Fg, 0.22))
		d.BeginPath().MoveTo(pts[0].X, pts[0].Y)
		for _, p := range pts[1:] {
			d.LineTo(p.X, p.Y)
		}
		d.ClosePath().Fill()
		d.SetStrokeColor(spec.Added.Edge).SetLineWidth(1.5).SetLineDash(5, 3)
		strokePolygon(d, pts)
		d.SetLineDash()
	}

	d.Restore()
}

func (e *Editor) drawRoomSnapGrid(d *core.Drawing, gw, gh float32) {
	if !isFiniteFloat32(gw) || !isFiniteFloat32(gh) || gw <= 0 || gh <= 0 {
		return
	}
	cw, ch := e.CanvasSize()
	if !isFiniteFloat32(cw) || !isFiniteFloat32(ch) || cw <= 0 || ch <= 0 {
		return
	}
	// Find world AABB visible on canvas.
	corners := [4][2]float32{{0, 0}, {cw, 0}, {cw, ch}, {0, ch}}
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, c := range corners {
		wx, wy := e.CanvasToWorld(c[0], c[1])
		if !isFiniteFloat32(wx) || !isFiniteFloat32(wy) {
			return
		}
		minX = min(minX, wx)
		maxX = max(maxX, wx)
		minY = min(minY, wy)
		maxY = max(maxY, wy)
	}
	if !isFiniteFloat32(minX) || !isFiniteFloat32(maxX) || !isFiniteFloat32(minY) || !isFiniteFloat32(maxY) {
		return
	}
	// Cap line count in float64 so zooming far out never overflows int or draws thousands of lines.
	spanX := float64(maxX) - float64(minX)
	spanY := float64(maxY) - float64(minY)
	if spanX < 0 || spanY < 0 || spanX/float64(gw) > 256 || spanY/float64(gh) > 256 {
		return
	}
	startX := float32(math.Floor(float64(minX)/float64(gw)) * float64(gw))
	startY := float32(math.Floor(float64(minY)/float64(gh)) * float64(gh))
	if !isFiniteFloat32(startX) || !isFiniteFloat32(startY) {
		return
	}
	d.Save()
	d.SetStrokeColor(specAlpha(e.activeSpecPalette().Added.Edge, 0.16)).SetLineWidth(1)
	d.BeginPath()
	for x, n := startX, 0; x <= maxX && n <= 258; x, n = x+gw, n+1 {
		x0, y0 := e.WorldToCanvas(x, minY)
		x1, y1 := e.WorldToCanvas(x, maxY)
		d.MoveTo(x0, y0).LineTo(x1, y1)
	}
	for y, n := startY, 0; y <= maxY && n <= 258; y, n = y+gh, n+1 {
		x0, y0 := e.WorldToCanvas(minX, y)
		x1, y1 := e.WorldToCanvas(maxX, y)
		d.MoveTo(x0, y0).LineTo(x1, y1)
	}
	d.Stroke().Restore()
}

// --- Commands & cloning helpers ---

type roomMapEditCmd struct {
	ed         *Editor
	target     *roomMapState
	label      string
	status     string
	before     *rooms.Map
	after      *rooms.Map
	beforeSel  string
	afterSel   string
	selectRoom bool
}

func (c *roomMapEditCmd) Label() string { return c.label }

func (c *roomMapEditCmd) Do() {
	if c.ed.rooms == nil || c.ed.rooms != c.target {
		return
	}
	c.ed.rooms.doc = cloneRoomMap(c.after)
	if c.selectRoom {
		c.ed.rooms.selectedRoomID = c.afterSel
		if c.afterSel != "" {
			c.ed.clearSelectionFields()
			c.ed.inspectLayer = false
			c.ed.inspectAssetFile = false
		}
	}
	c.ed.Status = c.status
}

func (c *roomMapEditCmd) Undo() {
	if c.ed.rooms == nil || c.ed.rooms != c.target {
		return
	}
	c.ed.rooms.doc = cloneRoomMap(c.before)
	if c.selectRoom {
		c.ed.rooms.selectedRoomID = c.beforeSel
	}
}

func cloneRoomMap(m *rooms.Map) *rooms.Map {
	if m == nil {
		return nil
	}
	cp := &rooms.Map{
		Version:    m.Version,
		Transition: cloneTransition(m.Transition),
		Rooms:      make([]rooms.Room, len(m.Rooms)),
	}
	for i := range m.Rooms {
		cp.Rooms[i] = cloneRoom(m.Rooms[i])
	}
	return cp
}

func cloneRoom(r rooms.Room) rooms.Room {
	r.Transition = cloneTransition(r.Transition)
	return r
}

func cloneTransition(t *rooms.Transition) *rooms.Transition {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func transitionEqual(a, b *rooms.Transition) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// parseRoomDuration parses a duration string for a room transition: empty returns 0,
// non-empty requires a Go duration with a unit (matching rooms.Duration.UnmarshalYAML).
func parseRoomDuration(raw string) (rooms.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("rooms: duration %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("rooms: duration %s must not be negative", d)
	}
	return rooms.Duration(d), nil
}

// roomDefaultPath suggests a *.rooms.yaml path beside the open scene when saving a new room map.
func (e *Editor) roomDefaultPath() string {
	if e != nil && e.rooms != nil && e.rooms.path != "" {
		return e.rooms.path
	}
	if e != nil && e.FilePath != "" {
		base := e.FilePath
		for _, ext := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
			if strings.HasSuffix(strings.ToLower(base), ext) {
				base = base[:len(base)-len(ext)]
				break
			}
		}
		return base + ".rooms.yaml"
	}
	if e != nil && e.ProjectPath != "" {
		return filepath.Join(filepath.Dir(e.ProjectPath), "world.rooms.yaml")
	}
	return path.Clean("world.rooms.yaml")
}
