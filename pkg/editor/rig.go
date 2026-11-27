package editor

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/skeletal"
	"github.com/nsgo-dev/game/pkg/sprite"
)

// ErrNoRig is returned by skeletal rig editor operations when no rig is open.
var ErrNoRig = errors.New("editor: no skeletal rig is open")

type rigState struct {
	data               *skeletal.SkeletonData
	path               string
	fromDisk           bool
	raw                []byte
	dirty              bool
	atlasName          string
	skin               string
	animation          string
	selectedBone       string
	selectedSlot       string
	preferAttachOrigin bool
	time               time.Duration
	playing            bool
	stop               func()
	skel               *skeletal.Skeleton
	err                error
	viewX              float32
	viewY              float32
	viewZoom           float32
	canvasW            float32
	canvasH            float32
	drag               rigDragState
}

func (s *rigState) snapshot() *rigState {
	if s == nil {
		return nil
	}
	return &rigState{
		data:               cloneSkeletonData(s.data),
		path:               s.path,
		fromDisk:           s.fromDisk,
		raw:                bytes.Clone(s.raw),
		dirty:              s.dirty,
		atlasName:          s.atlasName,
		skin:               s.skin,
		animation:          s.animation,
		selectedBone:       s.selectedBone,
		selectedSlot:       s.selectedSlot,
		preferAttachOrigin: s.preferAttachOrigin,
		time:               s.time,
		viewX:              s.viewX,
		viewY:              s.viewY,
		viewZoom:           s.viewZoom,
		canvasW:            s.canvasW,
		canvasH:            s.canvasH,
	}
}

func cloneSkeletonData(sd *skeletal.SkeletonData) *skeletal.SkeletonData {
	if sd == nil {
		return nil
	}
	out := &skeletal.SkeletonData{
		Version: sd.Version,
		Name:    sd.Name,
		Bones:   slices.Clone(sd.Bones),
		Slots:   slices.Clone(sd.Slots),
	}
	if sd.Skins != nil {
		out.Skins = make(map[string]skeletal.Skin, len(sd.Skins))
		for k, skin := range sd.Skins {
			cp := make(skeletal.Skin, len(skin))
			maps.Copy(cp, skin)
			out.Skins[k] = cp
		}
	}
	if sd.Animations != nil {
		out.Animations = make(map[string]*skeletal.Animation, len(sd.Animations))
		for k, a := range sd.Animations {
			if a == nil {
				continue
			}
			ca := &skeletal.Animation{
				Name:     a.Name,
				Duration: a.Duration,
				Loop:     a.Loop,
			}
			if a.Bones != nil {
				ca.Bones = make(map[string]skeletal.BoneTimeline, len(a.Bones))
				for bn, bt := range a.Bones {
					ca.Bones[bn] = skeletal.BoneTimeline{
						Translate: slices.Clone(bt.Translate),
						Rotate:    slices.Clone(bt.Rotate),
						Scale:     slices.Clone(bt.Scale),
					}
				}
			}
			if a.Slots != nil {
				ca.Slots = make(map[string]skeletal.SlotTimeline, len(a.Slots))
				for sn, st := range a.Slots {
					ca.Slots[sn] = skeletal.SlotTimeline{
						Attachment: slices.Clone(st.Attachment),
						Alpha:      slices.Clone(st.Alpha),
					}
				}
			}
			out.Animations[k] = ca
		}
	}
	_ = out.Compile()
	return out
}

// isRigFile reports whether p names a .skel.yaml or .skel.yml file.
func isRigFile(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".skel.yaml") || strings.HasSuffix(lower, ".skel.yml")
}

// RigOpen reports whether a 2D skeletal rig is open in the rig editor.
func (e *Editor) RigOpen() bool {
	return e != nil && e.rig != nil && e.rig.data != nil
}

// RigData returns a deep copy of the open skeletal rig data, or nil when no rig is open.
func (e *Editor) RigData() *skeletal.SkeletonData {
	if !e.RigOpen() {
		return nil
	}
	return cloneSkeletonData(e.rig.data)
}

// RigPath returns the file path or name of the open rig, or "".
func (e *Editor) RigPath() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.path
}

// RigDirty reports whether the open rig has been edited since it was opened or last saved.
func (e *Editor) RigDirty() bool {
	if !e.RigOpen() {
		return false
	}
	return e.rig.dirty
}

// RigAtlas returns the name of the atlas currently used to preview the open rig.
func (e *Editor) RigAtlas() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.atlasName
}

// RigSkin returns the active preview skin name in the rig editor, or "".
func (e *Editor) RigSkin() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.skin
}

// RigAnimation returns the active animation name in the rig editor, or "" for bind pose.
func (e *Editor) RigAnimation() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.animation
}

// SelectedRigBone returns the name of the currently selected bone in the rig editor, or "".
func (e *Editor) SelectedRigBone() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.selectedBone
}

// SelectedRigSlot returns the name of the currently selected slot in the rig editor, or "".
func (e *Editor) SelectedRigSlot() string {
	if !e.RigOpen() {
		return ""
	}
	return e.rig.selectedSlot
}

// RigTime returns the current playhead time in the rig editor.
func (e *Editor) RigTime() time.Duration {
	if !e.RigOpen() {
		return 0
	}
	return e.rig.time
}

// RigPlaying reports whether the rig editor is currently playing an animation.
func (e *Editor) RigPlaying() bool {
	return e.RigOpen() && e.rig.playing
}

// RigBlending reports whether the rig editor's runtime skeleton is currently crossfading.
func (e *Editor) RigBlending() bool {
	return e.RigOpen() && e.rig.skel != nil && e.rig.skel.Blending()
}

// RigSkeleton returns the live runtime skeletal.Skeleton used by the rig editor preview, or nil.
func (e *Editor) RigSkeleton() *skeletal.Skeleton {
	if !e.RigOpen() {
		return nil
	}
	return e.rig.skel
}

// RigError returns the last rig preview or validation error, or nil.
func (e *Editor) RigError() error {
	if !e.RigOpen() {
		return nil
	}
	return e.rig.err
}

// RigSkinNames returns the sorted skin names defined in the open rig.
func (e *Editor) RigSkinNames() []string {
	if !e.RigOpen() || len(e.rig.data.Skins) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(e.rig.data.Skins))
}

// RigAnimationNames returns the sorted animation names defined in the open rig.
func (e *Editor) RigAnimationNames() []string {
	if !e.RigOpen() || len(e.rig.data.Animations) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(e.rig.data.Animations))
}

// RigAtlasFrames returns the sorted frame names in the rig editor's active atlas.
func (e *Editor) RigAtlasFrames() []string {
	if !e.RigOpen() {
		return nil
	}
	atlas := e.resolveRigAtlas(e.rig.atlasName)
	if atlas == nil || len(atlas.Frames) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(atlas.Frames))
}

func (e *Editor) defaultRigAtlasName() string {
	if e == nil || e.Resolver == nil {
		return ""
	}
	switch r := e.Resolver.(type) {
	case sceneformat.AtlasMap:
		names := slices.Sorted(maps.Keys(r))
		if len(names) > 0 {
			return names[0]
		}
	case AtlasLister:
		names := slices.Clone(r.AtlasNames())
		slices.Sort(names)
		if len(names) > 0 {
			return names[0]
		}
	}
	return ""
}

func (e *Editor) resolveRigAtlas(name string) *sprite.Atlas {
	if e == nil || e.Resolver == nil || name == "" {
		return nil
	}
	a, err := e.Resolver.Atlas(name)
	if err != nil {
		return nil
	}
	return a
}

// NewRig creates and opens a new minimal skeletal rig with one root bone as an undoable command.
func (e *Editor) NewRig(name, atlasName string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if atlasName == "" {
		atlasName = e.defaultRigAtlasName()
	}
	sd := &skeletal.SkeletonData{
		Version: skeletal.FormatVersion,
		Name:    name,
		Bones: []skeletal.BoneData{
			{Name: "root", ScaleX: 1, ScaleY: 1},
		},
	}
	if err := sd.Compile(); err != nil {
		return err
	}
	next := &rigState{
		data:         sd,
		dirty:        true,
		atlasName:    atlasName,
		selectedBone: "root",
		viewZoom:     2,
	}
	e.swapRig("New rig", next)
	return nil
}

// OpenRigYAML parses data as a .skel.yaml rig named name and opens it in the rig editor as an undoable command.
// Opening and saving without edits returns the exact original bytes unchanged.
func (e *Editor) OpenRigYAML(name string, data []byte, atlasName string) error {
	return e.openRig(name, data, atlasName, false)
}

// OpenRigFile reads the .skel.yaml rig file at filePath and opens it in the rig editor as an undoable command.
func (e *Editor) OpenRigFile(filePath, atlasName string) error {
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
	return e.openRig(filePath, data, atlasName, true)
}

// OpenRigProjectFile opens relPath (slash-separated from the open project's root) in the rig editor.
func (e *Editor) OpenRigProjectFile(relPath, atlasName string) error {
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
	root := filepath.Dir(e.ProjectPath)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return fmt.Errorf("editor: rig path %q escapes project root via symlink", relPath)
		}
	}
	return e.OpenRigFile(abs, atlasName)
}

func (e *Editor) openRig(filePath string, data []byte, atlasName string, fromDisk bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	sd, err := skeletal.Load(data)
	if err != nil {
		if filePath != "" {
			return fmt.Errorf("%s: %w", filePath, err)
		}
		return err
	}
	if atlasName == "" {
		atlasName = e.defaultRigAtlasName()
	}
	skin := ""
	if _, ok := sd.Skins["default"]; ok {
		skin = "default"
	} else if len(sd.Skins) > 0 {
		skin = slices.Sorted(maps.Keys(sd.Skins))[0]
	}
	selBone := ""
	if len(sd.Bones) > 0 {
		selBone = sd.Bones[0].Name
	}
	selSlot := ""
	if len(sd.Slots) > 0 {
		selSlot = sd.Slots[0].Name
	}
	next := &rigState{
		data:         sd,
		path:         filePath,
		fromDisk:     fromDisk,
		raw:          bytes.Clone(data),
		dirty:        false,
		atlasName:    atlasName,
		skin:         skin,
		selectedBone: selBone,
		selectedSlot: selSlot,
		viewZoom:     2,
	}
	label := "Open rig"
	if filePath != "" {
		label += " " + filePath
	}
	e.swapRig(label, next)
	return nil
}

// CloseRig closes the open skeletal rig as an undoable command.
func (e *Editor) CloseRig() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.rig == nil {
		return nil
	}
	e.swapRig("Close rig", nil)
	return nil
}

// SaveRigYAML encodes the open rig as deterministic .skel.yaml bytes.
// When the rig was opened from existing YAML and has not been modified (or all edits were undone),
// SaveRigYAML returns the exact original bytes unchanged.
func (e *Editor) SaveRigYAML() ([]byte, error) {
	if !e.RigOpen() {
		return nil, ErrNoRig
	}
	if !e.rig.dirty && len(e.rig.raw) > 0 {
		return bytes.Clone(e.rig.raw), nil
	}
	return skeletal.Encode(e.rig.data)
}

// SaveRigFile writes the open rig atomically to filePath and remembers filePath as RigPath.
func (e *Editor) SaveRigFile(filePath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.RigOpen() {
		return ErrNoRig
	}
	e.CancelRigDrag()
	data, err := e.SaveRigYAML()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filePath, data); err != nil {
		return err
	}
	e.rig.path = filePath
	e.rig.fromDisk = true
	e.rig.raw = bytes.Clone(data)
	e.rig.dirty = false
	e.rig.err = nil
	e.filesRev++
	e.invalidateProjectIndex()
	e.Status = "Saved rig " + filePath
	e.notify()
	e.CheckProblems()
	return nil
}

func (e *Editor) saveRigFromPanel() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.RigOpen() {
		return ErrNoRig
	}
	if e.OnSaveRig != nil {
		e.CancelRigDrag()
		data, err := e.SaveRigYAML()
		if err != nil {
			return err
		}
		if err := e.OnSaveRig(data); err != nil {
			return err
		}
		e.rig.raw = bytes.Clone(data)
		e.rig.dirty = false
		e.rig.err = nil
		e.Status = "Saved rig"
		e.notify()
		return nil
	}
	if !e.rig.fromDisk || e.rig.path == "" {
		return errors.New("editor: no rig path; open the rig from a file or set OnSaveRig")
	}
	return e.SaveRigFile(e.rig.path)
}

func (e *Editor) followRigMove(moveFile func(*string)) {
	if e.rig != nil && e.rig.fromDisk {
		moveFile(&e.rig.path)
	}
}

func (e *Editor) swapRig(label string, next *rigState) {
	e.stopRigLoop()
	e.CancelRigDrag()
	e.History.Execute(&swapRigCmd{
		ed:     e,
		label:  label,
		before: e.rig.snapshot(),
		after:  next.snapshot(),
	})
}

type swapRigCmd struct {
	ed            *Editor
	label         string
	before, after *rigState
}

func (c *swapRigCmd) Label() string { return c.label }
func (c *swapRigCmd) Do()           { c.ed.applyRigState(c.after, c.label) }
func (c *swapRigCmd) Undo()         { c.ed.applyRigState(c.before, "Undo "+c.label) }

func (e *Editor) applyRigState(st *rigState, status string) {
	e.stopRigLoop()
	if st == nil {
		e.rig = nil
		e.Status = status
		return
	}
	prevW, prevH := float32(0), float32(0)
	prevVX, prevVY, prevVZ := float32(0), float32(0), float32(2)
	if e.rig != nil {
		prevW, prevH = e.rig.canvasW, e.rig.canvasH
		prevVX, prevVY, prevVZ = e.rig.viewX, e.rig.viewY, e.rig.viewZoom
	}
	cp := st.snapshot()
	if cp.canvasW == 0 && prevW > 0 {
		cp.canvasW, cp.canvasH = prevW, prevH
	}
	if cp.viewZoom <= 0 {
		cp.viewX, cp.viewY, cp.viewZoom = prevVX, prevVY, prevVZ
		if cp.viewZoom <= 0 {
			cp.viewZoom = 2
		}
	}
	e.rig = cp
	e.rebuildRigPreview()
	e.Status = status
}

func (e *Editor) editRig(label string, mutate func(sd *skeletal.SkeletonData, st *rigState) error) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.RigOpen() {
		return ErrNoRig
	}
	e.CancelRigDrag()
	before := e.rig.snapshot()
	after := e.rig.snapshot()
	if err := mutate(after.data, after); err != nil {
		e.rig.err = err
		e.notify()
		return err
	}
	if err := after.data.Compile(); err != nil {
		e.rig.err = err
		e.notify()
		return err
	}
	if _, err := skeletal.Encode(after.data); err != nil {
		e.rig.err = err
		e.notify()
		return err
	}
	after.dirty = true
	after.err = nil
	e.clampRigSelections(after)
	e.History.Execute(&swapRigCmd{
		ed:     e,
		label:  label,
		before: before,
		after:  after,
	})
	return nil
}

func (e *Editor) clampRigSelections(st *rigState) {
	if st == nil || st.data == nil {
		return
	}
	if st.selectedBone != "" && findRigBone(st.data, st.selectedBone) < 0 {
		st.selectedBone = ""
		if len(st.data.Bones) > 0 {
			st.selectedBone = st.data.Bones[0].Name
		}
	}
	if st.selectedSlot != "" && findRigSlot(st.data, st.selectedSlot) < 0 {
		st.selectedSlot = ""
		if len(st.data.Slots) > 0 {
			st.selectedSlot = st.data.Slots[0].Name
		}
	}
	if st.skin != "" {
		if _, ok := st.data.Skins[st.skin]; !ok {
			if _, hasDef := st.data.Skins["default"]; hasDef {
				st.skin = "default"
			} else if len(st.data.Skins) > 0 {
				st.skin = slices.Sorted(maps.Keys(st.data.Skins))[0]
			} else {
				st.skin = ""
			}
		}
	}
	if st.animation != "" {
		if a, ok := st.data.Animations[st.animation]; !ok || a == nil {
			st.animation = ""
			st.time = 0
		} else if st.time > a.Duration {
			st.time = a.Duration
		}
	}
}

func (e *Editor) rebuildRigPreview() {
	if !e.RigOpen() {
		return
	}
	st := e.rig
	if err := st.data.Compile(); err != nil {
		st.skel = nil
		st.err = err
		return
	}
	atlas := e.resolveRigAtlas(st.atlasName)
	skel := skeletal.NewSkeleton(st.data, atlas)
	if st.skin != "" {
		_ = skel.SetSkin(st.skin)
	}
	if st.animation != "" {
		if a := st.data.Animations[st.animation]; a != nil {
			_ = skel.Play(st.animation, a.Loop)
			if st.time > 0 {
				skel.Update(st.time)
			}
		}
	} else {
		skel.ResetToBindPose()
	}
	st.skel = skel
	st.err = nil
}

// SetRigAtlas selects which sprite.Atlas supplies attachment frames for the rig editor preview.
func (e *Editor) SetRigAtlas(atlasName string) error {
	if !e.RigOpen() {
		return ErrNoRig
	}
	if atlasName != "" && e.Resolver != nil {
		if _, err := e.Resolver.Atlas(atlasName); err != nil {
			return fmt.Errorf("editor: resolve atlas %q: %w", atlasName, err)
		}
	}
	e.rig.atlasName = atlasName
	e.rebuildRigPreview()
	e.notify()
	return nil
}

// SetRigName sets the open rig's name as an undoable command.
func (e *Editor) SetRigName(name string) error {
	return e.editRig("Rename rig", func(sd *skeletal.SkeletonData, _ *rigState) error {
		sd.Name = name
		return nil
	})
}

// SelectRigBone selects bone name in the rig editor, or clears the selection when empty.
func (e *Editor) SelectRigBone(name string) {
	if !e.RigOpen() {
		return
	}
	if name != "" && findRigBone(e.rig.data, name) < 0 {
		return
	}
	e.rig.selectedBone = name
	if name != "" {
		e.rig.preferAttachOrigin = false
	}
	e.notify()
}

// SelectRigSlot selects slot name in the rig editor, or clears the selection when empty.
func (e *Editor) SelectRigSlot(name string) {
	if !e.RigOpen() {
		return
	}
	if name != "" && findRigSlot(e.rig.data, name) < 0 {
		return
	}
	e.rig.selectedSlot = name
	if name != "" {
		e.rig.preferAttachOrigin = true
	}
	e.notify()
}

// --- Bones ---

func findRigBone(sd *skeletal.SkeletonData, name string) int {
	if sd == nil {
		return -1
	}
	for i := range sd.Bones {
		if sd.Bones[i].Name == name {
			return i
		}
	}
	return -1
}

func findRigSlot(sd *skeletal.SkeletonData, name string) int {
	if sd == nil {
		return -1
	}
	for i := range sd.Slots {
		if sd.Slots[i].Name == name {
			return i
		}
	}
	return -1
}

// AddRigBone adds a new bone with unit scale (1, 1) to the open rig as an undoable command.
func (e *Editor) AddRigBone(name, parent string, x, y, rotation, length float32) error {
	if err := checkName("bone name", name); err != nil {
		return err
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(rotation) || !isFiniteFloat32(length) || length < 0 {
		return fmt.Errorf("editor: bone %q transform must be finite and length non-negative", name)
	}
	return e.editRig("Add bone "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if findRigBone(sd, name) >= 0 {
			return fmt.Errorf("editor: duplicate bone %q", name)
		}
		if parent != "" && findRigBone(sd, parent) < 0 {
			return fmt.Errorf("editor: unknown parent bone %q", parent)
		}
		sd.Bones = append(sd.Bones, skeletal.BoneData{
			Name:     name,
			Parent:   parent,
			X:        x,
			Y:        y,
			Rotation: rotation,
			ScaleX:   1,
			ScaleY:   1,
			Length:   length,
		})
		st.selectedBone = name
		return nil
	})
}

// DeleteRigBone deletes bone name from the open rig as an undoable command.
// Child bones are reparented to name's parent and bone timelines for name are removed.
// It refuses to delete a bone that still has slots bound to it.
func (e *Editor) DeleteRigBone(name string) error {
	return e.editRig("Delete bone "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		for _, sl := range sd.Slots {
			if sl.Bone == name {
				return fmt.Errorf("editor: bone %q is used by slot %q; rebind or delete the slot first", name, sl.Name)
			}
		}
		grandparent := sd.Bones[idx].Parent
		sd.Bones = slices.Delete(sd.Bones, idx, idx+1)
		for i := range sd.Bones {
			if sd.Bones[i].Parent == name {
				sd.Bones[i].Parent = grandparent
			}
		}
		for _, anim := range sd.Animations {
			if anim != nil && anim.Bones != nil {
				delete(anim.Bones, name)
				if len(anim.Bones) == 0 {
					anim.Bones = nil
				}
			}
		}
		if st.selectedBone == name {
			st.selectedBone = grandparent
		}
		return nil
	})
}

// RenameRigBone renames bone oldName to newName across parent references, slots, and animations as an undoable command.
func (e *Editor) RenameRigBone(oldName, newName string) error {
	if err := checkName("bone name", newName); err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	return e.editRig("Rename bone "+oldName, func(sd *skeletal.SkeletonData, st *rigState) error {
		idx := findRigBone(sd, oldName)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", oldName)
		}
		if findRigBone(sd, newName) >= 0 {
			return fmt.Errorf("editor: duplicate bone %q", newName)
		}
		sd.Bones[idx].Name = newName
		for i := range sd.Bones {
			if sd.Bones[i].Parent == oldName {
				sd.Bones[i].Parent = newName
			}
		}
		for i := range sd.Slots {
			if sd.Slots[i].Bone == oldName {
				sd.Slots[i].Bone = newName
			}
		}
		for _, anim := range sd.Animations {
			if anim != nil && anim.Bones != nil {
				if tl, ok := anim.Bones[oldName]; ok {
					delete(anim.Bones, oldName)
					anim.Bones[newName] = tl
				}
			}
		}
		if st.selectedBone == oldName {
			st.selectedBone = newName
		}
		return nil
	})
}

// ReparentRigBone sets bone name's parent to newParent (empty for a root bone) as an undoable command.
// Cycles and unknown parents are refused. When keepWorld is true, local X, Y, Rotation, ScaleX and ScaleY
// are recomputed so the bone keeps its bind-pose world transform under newParent.
func (e *Editor) ReparentRigBone(name, newParent string, keepWorld bool) error {
	return e.editRig("Reparent bone "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		if newParent != "" && findRigBone(sd, newParent) < 0 {
			return fmt.Errorf("editor: unknown parent bone %q", newParent)
		}
		if newParent == name {
			return fmt.Errorf("editor: bone %q cannot be its own parent", name)
		}
		// Detect cycle before modifying transforms.
		for cur := newParent; cur != ""; {
			if cur == name {
				return fmt.Errorf("editor: reparenting %q under %q creates a cycle", name, newParent)
			}
			ci := findRigBone(sd, cur)
			if ci < 0 {
				break
			}
			cur = sd.Bones[ci].Parent
		}
		if sd.Bones[idx].Parent == newParent {
			return nil
		}
		if keepWorld {
			if err := sd.Compile(); err != nil {
				return err
			}
			bind := skeletal.NewSkeleton(sd, nil)
			bind.ResetToBindPose()
			worldSelf, _ := bind.WorldBone(name)
			parentWorld := skeletal.BoneTransform{ScaleX: 1, ScaleY: 1}
			if newParent != "" {
				parentWorld, _ = bind.WorldBone(newParent)
			}
			lx, ly, lrot, lsx, lsy, ok := worldToBoneLocal(parentWorld, worldSelf)
			if !ok {
				return fmt.Errorf("editor: cannot preserve world transform under zero-scale parent %q", newParent)
			}
			sd.Bones[idx].X = lx
			sd.Bones[idx].Y = ly
			sd.Bones[idx].Rotation = lrot
			sd.Bones[idx].ScaleX = lsx
			sd.Bones[idx].ScaleY = lsy
		}
		sd.Bones[idx].Parent = newParent
		return nil
	})
}

func worldToBoneLocal(parent, world skeletal.BoneTransform) (lx, ly, lrot, lsx, lsy float32, ok bool) {
	if parent.ScaleX == 0 || parent.ScaleY == 0 {
		return 0, 0, 0, 0, 0, false
	}
	dx := float64(world.X - parent.X)
	dy := float64(world.Y - parent.Y)
	sn, cs := math.Sincos(float64(-parent.Rotation))
	rx := cs*dx - sn*dy
	ry := sn*dx + cs*dy
	lx = float32(rx / float64(parent.ScaleX))
	ly = float32(ry / float64(parent.ScaleY))
	lrot = world.Rotation - parent.Rotation
	if parent.ScaleX*parent.ScaleY < 0 {
		lrot = parent.Rotation - world.Rotation
	}
	lsx = world.ScaleX / parent.ScaleX
	lsy = world.ScaleY / parent.ScaleY
	if !isFiniteFloat32(lx) || !isFiniteFloat32(ly) || !isFiniteFloat32(lrot) || !isFiniteFloat32(lsx) || !isFiniteFloat32(lsy) {
		return 0, 0, 0, 0, 0, false
	}
	return lx, ly, lrot, lsx, lsy, true
}

// MoveRigBone sets bone name's bind-pose local offset (x, y) as an undoable command.
func (e *Editor) MoveRigBone(name string, x, y float32) error {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return fmt.Errorf("editor: bone %q position must be finite", name)
	}
	return e.editRig("Move bone "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		sd.Bones[idx].X = x
		sd.Bones[idx].Y = y
		return nil
	})
}

// RotateRigBone sets bone name's bind-pose local rotation (radians, clockwise) as an undoable command.
func (e *Editor) RotateRigBone(name string, rotation float32) error {
	if !isFiniteFloat32(rotation) {
		return fmt.Errorf("editor: bone %q rotation must be finite", name)
	}
	return e.editRig("Rotate bone "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		sd.Bones[idx].Rotation = rotation
		return nil
	})
}

// ScaleRigBone sets bone name's bind-pose local scale (scaleX, scaleY) as an undoable command.
func (e *Editor) ScaleRigBone(name string, scaleX, scaleY float32) error {
	if !isFiniteFloat32(scaleX) || !isFiniteFloat32(scaleY) {
		return fmt.Errorf("editor: bone %q scale must be finite", name)
	}
	return e.editRig("Scale bone "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		sd.Bones[idx].ScaleX = scaleX
		sd.Bones[idx].ScaleY = scaleY
		return nil
	})
}

// SetRigBoneLength sets bone name's length (non-negative, finite) as an undoable command.
func (e *Editor) SetRigBoneLength(name string, length float32) error {
	if !isFiniteFloat32(length) || length < 0 {
		return fmt.Errorf("editor: bone %q length %v must be finite and non-negative", name, length)
	}
	return e.editRig("Set bone length "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigBone(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown bone %q", name)
		}
		sd.Bones[idx].Length = length
		return nil
	})
}

// --- Slots, Skins, Attachments ---

// AddRigSlot adds a draw slot bound to bone as an undoable command.
func (e *Editor) AddRigSlot(name, bone, defaultAttachment string, alpha float32) error {
	if err := checkName("slot name", name); err != nil {
		return err
	}
	if !(alpha >= 0 && alpha <= 1) {
		return fmt.Errorf("editor: slot %q alpha %v must be in [0, 1]", name, alpha)
	}
	return e.editRig("Add slot "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if findRigSlot(sd, name) >= 0 {
			return fmt.Errorf("editor: duplicate slot %q", name)
		}
		if findRigBone(sd, bone) < 0 {
			return fmt.Errorf("editor: unknown bone %q", bone)
		}
		sd.Slots = append(sd.Slots, skeletal.SlotData{
			Name:       name,
			Bone:       bone,
			Attachment: defaultAttachment,
			Alpha:      alpha,
		})
		st.selectedSlot = name
		return nil
	})
}

// DeleteRigSlot removes slot name and its entries in skins and animations as an undoable command.
func (e *Editor) DeleteRigSlot(name string) error {
	return e.editRig("Delete slot "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		idx := findRigSlot(sd, name)
		if idx < 0 {
			return fmt.Errorf("editor: unknown slot %q", name)
		}
		sd.Slots = slices.Delete(sd.Slots, idx, idx+1)
		for skinName, skin := range sd.Skins {
			delete(skin, name)
			sd.Skins[skinName] = skin
		}
		for _, anim := range sd.Animations {
			if anim != nil && anim.Slots != nil {
				delete(anim.Slots, name)
				if len(anim.Slots) == 0 {
					anim.Slots = nil
				}
			}
		}
		if st.selectedSlot == name {
			st.selectedSlot = ""
		}
		return nil
	})
}

// SetRigSlotBone rebinds slot to bone as an undoable command.
func (e *Editor) SetRigSlotBone(slot, bone string) error {
	return e.editRig("Rebind slot "+slot, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigSlot(sd, slot)
		if idx < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if findRigBone(sd, bone) < 0 {
			return fmt.Errorf("editor: unknown bone %q", bone)
		}
		sd.Slots[idx].Bone = bone
		return nil
	})
}

// SetRigSlotAlpha sets slot's bind-pose opacity in [0, 1] as an undoable command.
func (e *Editor) SetRigSlotAlpha(slot string, alpha float32) error {
	if !(alpha >= 0 && alpha <= 1) {
		return fmt.Errorf("editor: slot %q alpha %v must be in [0, 1]", slot, alpha)
	}
	return e.editRig("Set slot alpha "+slot, func(sd *skeletal.SkeletonData, _ *rigState) error {
		idx := findRigSlot(sd, slot)
		if idx < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		sd.Slots[idx].Alpha = alpha
		return nil
	})
}

// MoveRigSlotOrder moves slot to newIndex in draw order (back to front) as an undoable command.
func (e *Editor) MoveRigSlotOrder(slot string, newIndex int) error {
	return e.editRig("Reorder slot "+slot, func(sd *skeletal.SkeletonData, _ *rigState) error {
		oldIdx := findRigSlot(sd, slot)
		if oldIdx < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if newIndex < 0 || newIndex >= len(sd.Slots) {
			return fmt.Errorf("editor: slot index %d out of range [0, %d)", newIndex, len(sd.Slots))
		}
		if oldIdx == newIndex {
			return nil
		}
		sl := sd.Slots[oldIdx]
		sd.Slots = slices.Delete(sd.Slots, oldIdx, oldIdx+1)
		sd.Slots = slices.Insert(sd.Slots, newIndex, sl)
		return nil
	})
}

// AddRigSkin adds a new skin named name to the open rig and selects it as an undoable command.
func (e *Editor) AddRigSkin(name string) error {
	if err := checkName("skin name", name); err != nil {
		return err
	}
	return e.editRig("Add skin "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if _, exists := sd.Skins[name]; exists {
			return fmt.Errorf("editor: duplicate skin %q", name)
		}
		if sd.Skins == nil {
			sd.Skins = make(map[string]skeletal.Skin)
		}
		sd.Skins[name] = make(skeletal.Skin)
		st.skin = name
		return nil
	})
}

// DeleteRigSkin deletes skin name from the open rig as an undoable command.
func (e *Editor) DeleteRigSkin(name string) error {
	return e.editRig("Delete skin "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if _, exists := sd.Skins[name]; !exists {
			return fmt.Errorf("editor: unknown skin %q", name)
		}
		delete(sd.Skins, name)
		if len(sd.Skins) == 0 {
			sd.Skins = nil
		}
		if st.skin == name {
			st.skin = ""
		}
		return nil
	})
}

// SelectRigSkin switches the preview skin in the rig editor.
func (e *Editor) SelectRigSkin(name string) error {
	if !e.RigOpen() {
		return ErrNoRig
	}
	if name != "" {
		if _, ok := e.rig.data.Skins[name]; !ok {
			return fmt.Errorf("editor: unknown skin %q", name)
		}
	}
	e.rig.skin = name
	if e.rig.skel != nil && name != "" {
		_ = e.rig.skel.SetSkin(name)
	} else {
		e.rebuildRigPreview()
	}
	e.notify()
	return nil
}

// SetRigAttachment sets the full skeletal.Attachment for slot in skin (creating skin if needed) as an undoable command.
func (e *Editor) SetRigAttachment(skin, slot string, att skeletal.Attachment) error {
	if err := checkName("skin name", skin); err != nil {
		return err
	}
	if !isFiniteFloat32(att.X) || !isFiniteFloat32(att.Y) || !isFiniteFloat32(att.Rotation) ||
		!isFiniteFloat32(att.ScaleX) || !isFiniteFloat32(att.ScaleY) ||
		!isFiniteFloat32(att.OriginX) || !isFiniteFloat32(att.OriginY) {
		return fmt.Errorf("editor: attachment for slot %q has non-finite values", slot)
	}
	return e.editRig("Set attachment "+skin+"/"+slot, func(sd *skeletal.SkeletonData, st *rigState) error {
		if findRigSlot(sd, slot) < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if sd.Skins == nil {
			sd.Skins = make(map[string]skeletal.Skin)
		}
		sk := sd.Skins[skin]
		if sk == nil {
			sk = make(skeletal.Skin)
			sd.Skins[skin] = sk
		}
		sk[slot] = att
		st.skin = skin
		st.selectedSlot = slot
		return nil
	})
}

// AttachRigFrame attaches atlas frame frame to slot in skin with pivot (originX, originY) as an undoable command.
// If skin[slot] already exists, its offset, rotation and scale are kept; otherwise scale defaults to (1, 1).
func (e *Editor) AttachRigFrame(skin, slot, frame string, originX, originY float32) error {
	if !e.RigOpen() {
		return ErrNoRig
	}
	if skin == "" {
		skin = e.rig.skin
		if skin == "" {
			skin = "default"
		}
	}
	if frame != "" {
		if atlas := e.resolveRigAtlas(e.rig.atlasName); atlas != nil {
			if _, ok := atlas.Frames[frame]; !ok {
				return fmt.Errorf("editor: atlas %q has no frame %q", e.rig.atlasName, frame)
			}
		}
	}
	att := skeletal.Attachment{ScaleX: 1, ScaleY: 1}
	if sk, ok := e.rig.data.Skins[skin]; ok {
		if prev, ok := sk[slot]; ok {
			att = prev
		}
	}
	att.Frame = frame
	att.OriginX = originX
	att.OriginY = originY
	return e.SetRigAttachment(skin, slot, att)
}

// SetRigAttachmentOrigin sets the pivot (originX, originY) of slot's attachment in skin as an undoable command.
func (e *Editor) SetRigAttachmentOrigin(skin, slot string, originX, originY float32) error {
	if !isFiniteFloat32(originX) || !isFiniteFloat32(originY) {
		return fmt.Errorf("editor: attachment origin for slot %q must be finite", slot)
	}
	return e.editRig("Set attachment origin "+slot, func(sd *skeletal.SkeletonData, st *rigState) error {
		if findRigSlot(sd, slot) < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if skin == "" {
			skin = st.skin
			if skin == "" {
				skin = "default"
			}
		}
		sk, ok := sd.Skins[skin]
		if !ok {
			return fmt.Errorf("editor: unknown skin %q", skin)
		}
		att, ok := sk[slot]
		if !ok {
			return fmt.Errorf("editor: skin %q has no attachment for slot %q", skin, slot)
		}
		att.OriginX = originX
		att.OriginY = originY
		sk[slot] = att
		return nil
	})
}

// SetRigAttachmentTransform sets the local offset, rotation and scale of slot's attachment in skin as an undoable command.
func (e *Editor) SetRigAttachmentTransform(skin, slot string, x, y, rotation, scaleX, scaleY float32) error {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(rotation) || !isFiniteFloat32(scaleX) || !isFiniteFloat32(scaleY) {
		return fmt.Errorf("editor: attachment transform for slot %q must be finite", slot)
	}
	return e.editRig("Set attachment transform "+slot, func(sd *skeletal.SkeletonData, st *rigState) error {
		if findRigSlot(sd, slot) < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if skin == "" {
			skin = st.skin
			if skin == "" {
				skin = "default"
			}
		}
		sk, ok := sd.Skins[skin]
		if !ok {
			return fmt.Errorf("editor: unknown skin %q", skin)
		}
		att, ok := sk[slot]
		if !ok {
			return fmt.Errorf("editor: skin %q has no attachment for slot %q", skin, slot)
		}
		att.X, att.Y, att.Rotation, att.ScaleX, att.ScaleY = x, y, rotation, scaleX, scaleY
		sk[slot] = att
		return nil
	})
}

// RemoveRigAttachment removes slot's attachment entry from skin as an undoable command.
func (e *Editor) RemoveRigAttachment(skin, slot string) error {
	return e.editRig("Remove attachment "+skin+"/"+slot, func(sd *skeletal.SkeletonData, _ *rigState) error {
		sk, ok := sd.Skins[skin]
		if !ok {
			return fmt.Errorf("editor: unknown skin %q", skin)
		}
		if _, ok := sk[slot]; !ok {
			return fmt.Errorf("editor: skin %q has no attachment for slot %q", skin, slot)
		}
		delete(sk, slot)
		return nil
	})
}

// --- Animations & Keyframes ---

func checkRigTime(at time.Duration) error {
	if at < 0 || at > skeletal.MaxSeconds*time.Second {
		return fmt.Errorf("editor: keyframe time %v out of range [0, %ds]", at, skeletal.MaxSeconds)
	}
	return nil
}

func checkRigCurve(c skeletal.CurveType, bz [4]float32) error {
	switch c {
	case skeletal.CurveLinear, skeletal.CurveStepped:
		if bz != [4]float32{} {
			return errors.New("editor: bezier control points are only valid on CurveBezier")
		}
		return nil
	case skeletal.CurveBezier:
		for _, v := range bz {
			if !isFiniteFloat32(v) {
				return errors.New("editor: bezier control points must be finite")
			}
		}
		if bz[0] < 0 || bz[0] > 1 || bz[2] < 0 || bz[2] > 1 {
			return errors.New("editor: bezier x control points must lie in [0, 1]")
		}
		return nil
	default:
		return fmt.Errorf("editor: unknown curve type %d", c)
	}
}

// AddRigAnimation adds a new animation named name to the open rig and selects it as an undoable command.
func (e *Editor) AddRigAnimation(name string, duration time.Duration, loop bool) error {
	if err := checkName("animation name", name); err != nil {
		return err
	}
	if err := checkRigTime(duration); err != nil {
		return err
	}
	return e.editRig("Add animation "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if _, exists := sd.Animations[name]; exists {
			return fmt.Errorf("editor: duplicate animation %q", name)
		}
		if sd.Animations == nil {
			sd.Animations = make(map[string]*skeletal.Animation)
		}
		sd.Animations[name] = &skeletal.Animation{
			Name:     name,
			Duration: duration,
			Loop:     loop,
		}
		st.animation = name
		st.time = 0
		return nil
	})
}

// DeleteRigAnimation removes animation name from the open rig as an undoable command.
func (e *Editor) DeleteRigAnimation(name string) error {
	return e.editRig("Delete animation "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		if _, exists := sd.Animations[name]; !exists {
			return fmt.Errorf("editor: unknown animation %q", name)
		}
		delete(sd.Animations, name)
		if len(sd.Animations) == 0 {
			sd.Animations = nil
		}
		if st.animation == name {
			st.animation = ""
			st.time = 0
		}
		return nil
	})
}

// SelectRigAnimation selects animation name in the rig editor (empty returns to bind pose) and updates the live preview.
func (e *Editor) SelectRigAnimation(name string) error {
	if !e.RigOpen() {
		return ErrNoRig
	}
	e.stopRigLoop()
	if name != "" {
		if a, ok := e.rig.data.Animations[name]; !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", name)
		}
	}
	e.rig.animation = name
	e.rig.time = 0
	e.rebuildRigPreview()
	e.notify()
	return nil
}

// SetRigAnimationDuration sets animation name's duration as an undoable command.
func (e *Editor) SetRigAnimationDuration(name string, duration time.Duration) error {
	if err := checkRigTime(duration); err != nil {
		return err
	}
	return e.editRig("Set animation duration "+name, func(sd *skeletal.SkeletonData, st *rigState) error {
		anim, ok := sd.Animations[name]
		if !ok || anim == nil {
			return fmt.Errorf("editor: unknown animation %q", name)
		}
		anim.Duration = duration
		if st.animation == name && st.time > duration {
			st.time = duration
		}
		return nil
	})
}

// SetRigAnimationLoop sets animation name's default loop flag as an undoable command.
func (e *Editor) SetRigAnimationLoop(name string, loop bool) error {
	return e.editRig("Set animation loop "+name, func(sd *skeletal.SkeletonData, _ *rigState) error {
		anim, ok := sd.Animations[name]
		if !ok || anim == nil {
			return fmt.Errorf("editor: unknown animation %q", name)
		}
		anim.Loop = loop
		return nil
	})
}

func upsertVec2Key(keys []skeletal.Vec2Keyframe, k skeletal.Vec2Keyframe) []skeletal.Vec2Keyframe {
	idx, found := slices.BinarySearchFunc(keys, k.Time, func(a skeletal.Vec2Keyframe, t time.Duration) int {
		if a.Time < t {
			return -1
		}
		if a.Time > t {
			return 1
		}
		return 0
	})
	if found {
		keys[idx] = k
		return keys
	}
	return slices.Insert(keys, idx, k)
}

func upsertFloatKey(keys []skeletal.FloatKeyframe, k skeletal.FloatKeyframe) []skeletal.FloatKeyframe {
	idx, found := slices.BinarySearchFunc(keys, k.Time, func(a skeletal.FloatKeyframe, t time.Duration) int {
		if a.Time < t {
			return -1
		}
		if a.Time > t {
			return 1
		}
		return 0
	})
	if found {
		keys[idx] = k
		return keys
	}
	return slices.Insert(keys, idx, k)
}

func upsertAttachKey(keys []skeletal.AttachmentKeyframe, k skeletal.AttachmentKeyframe) []skeletal.AttachmentKeyframe {
	idx, found := slices.BinarySearchFunc(keys, k.Time, func(a skeletal.AttachmentKeyframe, t time.Duration) int {
		if a.Time < t {
			return -1
		}
		if a.Time > t {
			return 1
		}
		return 0
	})
	if found {
		keys[idx] = k
		return keys
	}
	return slices.Insert(keys, idx, k)
}

// KeyRigBoneTranslate keys bone's relative translation (x, y) at time at in animation anim as an undoable command.
func (e *Editor) KeyRigBoneTranslate(anim, bone string, at time.Duration, x, y float32, curve skeletal.CurveType, bezier [4]float32) error {
	if err := checkRigTime(at); err != nil {
		return err
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return fmt.Errorf("editor: translate keyframe for bone %q must be finite", bone)
	}
	if err := checkRigCurve(curve, bezier); err != nil {
		return err
	}
	return e.editRig("Key "+bone+" translate", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		if findRigBone(sd, bone) < 0 {
			return fmt.Errorf("editor: unknown bone %q", bone)
		}
		if a.Bones == nil {
			a.Bones = make(map[string]skeletal.BoneTimeline)
		}
		tl := a.Bones[bone]
		tl.Translate = upsertVec2Key(tl.Translate, skeletal.Vec2Keyframe{Time: at, X: x, Y: y, Curve: curve, Bezier: bezier})
		a.Bones[bone] = tl
		if at > a.Duration {
			a.Duration = at
		}
		return nil
	})
}

// KeyRigBoneRotate keys bone's relative rotation angle (radians) at time at in animation anim as an undoable command.
func (e *Editor) KeyRigBoneRotate(anim, bone string, at time.Duration, angle float32, curve skeletal.CurveType, bezier [4]float32) error {
	if err := checkRigTime(at); err != nil {
		return err
	}
	if !isFiniteFloat32(angle) {
		return fmt.Errorf("editor: rotate keyframe for bone %q must be finite", bone)
	}
	if err := checkRigCurve(curve, bezier); err != nil {
		return err
	}
	return e.editRig("Key "+bone+" rotate", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		if findRigBone(sd, bone) < 0 {
			return fmt.Errorf("editor: unknown bone %q", bone)
		}
		if a.Bones == nil {
			a.Bones = make(map[string]skeletal.BoneTimeline)
		}
		tl := a.Bones[bone]
		tl.Rotate = upsertFloatKey(tl.Rotate, skeletal.FloatKeyframe{Time: at, Value: angle, Curve: curve, Bezier: bezier})
		a.Bones[bone] = tl
		if at > a.Duration {
			a.Duration = at
		}
		return nil
	})
}

// KeyRigBoneScale keys bone's relative scale (scaleX, scaleY) at time at in animation anim as an undoable command.
func (e *Editor) KeyRigBoneScale(anim, bone string, at time.Duration, scaleX, scaleY float32, curve skeletal.CurveType, bezier [4]float32) error {
	if err := checkRigTime(at); err != nil {
		return err
	}
	if !isFiniteFloat32(scaleX) || !isFiniteFloat32(scaleY) {
		return fmt.Errorf("editor: scale keyframe for bone %q must be finite", bone)
	}
	if err := checkRigCurve(curve, bezier); err != nil {
		return err
	}
	return e.editRig("Key "+bone+" scale", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		if findRigBone(sd, bone) < 0 {
			return fmt.Errorf("editor: unknown bone %q", bone)
		}
		if a.Bones == nil {
			a.Bones = make(map[string]skeletal.BoneTimeline)
		}
		tl := a.Bones[bone]
		tl.Scale = upsertVec2Key(tl.Scale, skeletal.Vec2Keyframe{Time: at, X: scaleX, Y: scaleY, Curve: curve, Bezier: bezier})
		a.Bones[bone] = tl
		if at > a.Duration {
			a.Duration = at
		}
		return nil
	})
}

// KeyRigSlotAttachment keys slot's active atlas frame (empty hides the slot) at time at in animation anim as an undoable command.
func (e *Editor) KeyRigSlotAttachment(anim, slot string, at time.Duration, frame string) error {
	if err := checkRigTime(at); err != nil {
		return err
	}
	if e.RigOpen() && frame != "" {
		if atlas := e.resolveRigAtlas(e.rig.atlasName); atlas != nil {
			if _, ok := atlas.Frames[frame]; !ok {
				return fmt.Errorf("editor: atlas %q has no frame %q", e.rig.atlasName, frame)
			}
		}
	}
	return e.editRig("Key "+slot+" attachment", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		if findRigSlot(sd, slot) < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if a.Slots == nil {
			a.Slots = make(map[string]skeletal.SlotTimeline)
		}
		tl := a.Slots[slot]
		tl.Attachment = upsertAttachKey(tl.Attachment, skeletal.AttachmentKeyframe{Time: at, Frame: frame})
		a.Slots[slot] = tl
		if at > a.Duration {
			a.Duration = at
		}
		return nil
	})
}

// KeyRigSlotAlpha keys slot's opacity alpha in [0, 1] at time at in animation anim as an undoable command.
func (e *Editor) KeyRigSlotAlpha(anim, slot string, at time.Duration, alpha float32, curve skeletal.CurveType, bezier [4]float32) error {
	if err := checkRigTime(at); err != nil {
		return err
	}
	if !(alpha >= 0 && alpha <= 1) {
		return fmt.Errorf("editor: slot %q alpha %v must be in [0, 1]", slot, alpha)
	}
	if err := checkRigCurve(curve, bezier); err != nil {
		return err
	}
	return e.editRig("Key "+slot+" alpha", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		if findRigSlot(sd, slot) < 0 {
			return fmt.Errorf("editor: unknown slot %q", slot)
		}
		if a.Slots == nil {
			a.Slots = make(map[string]skeletal.SlotTimeline)
		}
		tl := a.Slots[slot]
		tl.Alpha = upsertFloatKey(tl.Alpha, skeletal.FloatKeyframe{Time: at, Value: alpha, Curve: curve, Bezier: bezier})
		a.Slots[slot] = tl
		if at > a.Duration {
			a.Duration = at
		}
		return nil
	})
}

// DeleteRigBoneKeyframe deletes keyframe index from bone's track ("translate", "rotate", or "scale") in anim as an undoable command.
func (e *Editor) DeleteRigBoneKeyframe(anim, bone, track string, index int) error {
	return e.editRig("Delete "+bone+" "+track+" key", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		tl, ok := a.Bones[bone]
		if !ok {
			return fmt.Errorf("editor: animation %q has no timeline for bone %q", anim, bone)
		}
		switch track {
		case "translate":
			if index < 0 || index >= len(tl.Translate) {
				return fmt.Errorf("editor: translate key %d out of range", index)
			}
			tl.Translate = slices.Delete(tl.Translate, index, index+1)
		case "rotate":
			if index < 0 || index >= len(tl.Rotate) {
				return fmt.Errorf("editor: rotate key %d out of range", index)
			}
			tl.Rotate = slices.Delete(tl.Rotate, index, index+1)
		case "scale":
			if index < 0 || index >= len(tl.Scale) {
				return fmt.Errorf("editor: scale key %d out of range", index)
			}
			tl.Scale = slices.Delete(tl.Scale, index, index+1)
		default:
			return fmt.Errorf("editor: unknown bone track %q", track)
		}
		if len(tl.Translate) == 0 && len(tl.Rotate) == 0 && len(tl.Scale) == 0 {
			delete(a.Bones, bone)
			if len(a.Bones) == 0 {
				a.Bones = nil
			}
		} else {
			a.Bones[bone] = tl
		}
		return nil
	})
}

// DeleteRigSlotKeyframe deletes keyframe index from slot's track ("attachment" or "alpha") in anim as an undoable command.
func (e *Editor) DeleteRigSlotKeyframe(anim, slot, track string, index int) error {
	return e.editRig("Delete "+slot+" "+track+" key", func(sd *skeletal.SkeletonData, _ *rigState) error {
		a, ok := sd.Animations[anim]
		if !ok || a == nil {
			return fmt.Errorf("editor: unknown animation %q", anim)
		}
		tl, ok := a.Slots[slot]
		if !ok {
			return fmt.Errorf("editor: animation %q has no timeline for slot %q", anim, slot)
		}
		switch track {
		case "attachment":
			if index < 0 || index >= len(tl.Attachment) {
				return fmt.Errorf("editor: attachment key %d out of range", index)
			}
			tl.Attachment = slices.Delete(tl.Attachment, index, index+1)
		case "alpha":
			if index < 0 || index >= len(tl.Alpha) {
				return fmt.Errorf("editor: alpha key %d out of range", index)
			}
			tl.Alpha = slices.Delete(tl.Alpha, index, index+1)
		default:
			return fmt.Errorf("editor: unknown slot track %q", track)
		}
		if len(tl.Attachment) == 0 && len(tl.Alpha) == 0 {
			delete(a.Slots, slot)
			if len(a.Slots) == 0 {
				a.Slots = nil
			}
		} else {
			a.Slots[slot] = tl
		}
		return nil
	})
}

// --- Runtime Playback & Crossfade Preview ---

func (e *Editor) activeRigAnim() *skeletal.Animation {
	if !e.RigOpen() || e.rig.animation == "" {
		return nil
	}
	return e.rig.data.Animations[e.rig.animation]
}

// SeekRig moves the rig editor's playhead to at and evaluates the pose through the runtime skeletal.Skeleton.
func (e *Editor) SeekRig(at time.Duration) {
	if !e.RigOpen() {
		return
	}
	a := e.activeRigAnim()
	if a == nil {
		e.rig.time = 0
		if e.rig.skel != nil {
			e.rig.skel.ResetToBindPose()
		}
		e.notify()
		return
	}
	e.rig.time = clampDuration(at, a.Duration)
	if e.rig.skel == nil {
		e.rebuildRigPreview()
	} else {
		_ = e.rig.skel.Play(a.Name, a.Loop)
		if e.rig.time > 0 {
			e.rig.skel.Update(e.rig.time)
		}
	}
	e.notify()
}

// PlayRig starts playing the active rig animation through the runtime skeletal.Skeleton.
// With a core.Scope attached (EditorView or AttachScope), a game.Loop advances it; otherwise call StepRig.
func (e *Editor) PlayRig() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.RigOpen() {
		return ErrNoRig
	}
	a := e.activeRigAnim()
	if a == nil {
		return errors.New("editor: select a rig animation to play")
	}
	if e.rig.skel == nil {
		e.rebuildRigPreview()
		if e.rig.skel == nil {
			return errors.New("editor: rig preview is unavailable")
		}
	}
	if !a.Loop && e.rig.time >= a.Duration {
		e.rig.time = 0
	}
	if !e.rig.skel.Blending() {
		if err := e.rig.skel.Play(a.Name, a.Loop); err != nil {
			return err
		}
		if e.rig.time > 0 {
			e.rig.skel.Update(e.rig.time)
		}
	}
	e.rig.playing = true
	e.startRigLoop()
	e.notify()
	return nil
}

// PauseRig pauses rig animation playback at the current playhead.
func (e *Editor) PauseRig() {
	if !e.RigOpen() {
		return
	}
	e.stopRigLoop()
	e.notify()
}

// StopRig stops rig playback and rewinds the playhead to 0.
func (e *Editor) StopRig() {
	if !e.RigOpen() {
		return
	}
	e.stopRigLoop()
	e.SeekRig(0)
}

// StepRig advances the rig preview's live runtime skeletal.Skeleton by dt.
func (e *Editor) StepRig(dt time.Duration) {
	if !e.RigOpen() || dt <= 0 {
		return
	}
	a := e.activeRigAnim()
	if a == nil || e.rig.skel == nil {
		return
	}
	e.rig.skel.Update(dt)
	if a.Duration > 0 {
		e.rig.time += dt
		if e.rig.time >= a.Duration {
			if a.Loop {
				e.rig.time %= a.Duration
			} else {
				e.rig.time = a.Duration
				if e.rig.playing && !e.rig.skel.Blending() {
					e.stopRigLoop()
				}
			}
		}
	} else {
		e.rig.time = 0
		if !a.Loop && e.rig.playing && !e.rig.skel.Blending() {
			e.stopRigLoop()
		}
	}
	e.rigStepRev = e.revision + 1
	e.notify()
}

// CrossfadeRig initiates a smooth crossfade into toAnim over duration through the runtime skeletal.Skeleton.Crossfade.
func (e *Editor) CrossfadeRig(toAnim string, duration time.Duration) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.RigOpen() {
		return ErrNoRig
	}
	a, ok := e.rig.data.Animations[toAnim]
	if !ok || a == nil {
		return fmt.Errorf("editor: unknown animation %q", toAnim)
	}
	if duration < 0 {
		return fmt.Errorf("editor: crossfade duration %v must be non-negative", duration)
	}
	if e.rig.skel == nil {
		e.rebuildRigPreview()
		if e.rig.skel == nil {
			return errors.New("editor: rig preview is unavailable")
		}
	}
	if err := e.rig.skel.Crossfade(toAnim, duration, a.Loop); err != nil {
		return err
	}
	e.rig.animation = toAnim
	e.rig.time = 0
	e.rig.playing = true
	e.startRigLoop()
	e.notify()
	return nil
}

func (e *Editor) startRigLoop() {
	if !e.RigOpen() || e.scope == nil || e.rig.stop != nil {
		return
	}
	cx := e.scope
	loop := &game.Loop{
		Rate: e.PlayRate,
		Update: func(dt time.Duration) {
			if !e.RigPlaying() {
				return
			}
			e.StepRig(dt)
		},
	}
	cx.Run(func() {
		if e.RigPlaying() && e.rig.stop == nil {
			e.rig.stop = loop.Run(cx)
		}
	})
}

func (e *Editor) stopRigLoop() {
	if e == nil || e.rig == nil {
		return
	}
	e.rig.playing = false
	if e.rig.stop != nil {
		e.rig.stop()
		e.rig.stop = nil
	}
}

// --- Scene Placement ---

// PlaceRig places a new object with the built-in "skeletal" component referencing rigPath, atlas,
// skin and animation at (x, y) on layer (active layer when empty) as an undoable PlaceObject command.
func (e *Editor) PlaceRig(layer, rigPath, atlas, skin, animation string, x, y float32) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if strings.TrimSpace(rigPath) == "" {
		return "", errors.New("editor: rig path must not be empty")
	}
	if layer == "" {
		layer = e.ActiveLayer
	}
	comp := map[string]any{
		"version": sceneformat.SkeletalComponentVersion,
		"rig":     rigPath,
	}
	if atlas != "" {
		comp["atlas"] = atlas
	}
	if skin != "" {
		comp["skin"] = skin
	}
	if animation != "" {
		comp["animation"] = animation
	}
	name := path.Base(rigPath)
	for _, suf := range []string{".skel.yaml", ".skel.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(name), suf) {
			name = name[:len(name)-len(suf)]
			break
		}
	}
	if name == "" {
		name = "rig"
	}
	return e.PlaceObject(layer, sceneformat.ObjectDoc{
		Name: name,
		X:    x,
		Y:    y,
		Components: map[string]any{
			sceneformat.SkeletalComponentName: comp,
		},
	})
}

// PlaceOpenRig places the rig currently open in the rig editor into layer (active layer when empty)
// at (x, y), using its project-relative path (or RigPath), active atlas, active skin, and active animation.
func (e *Editor) PlaceOpenRig(layer string, x, y float32) (string, error) {
	if !e.RigOpen() {
		return "", ErrNoRig
	}
	rigPath := e.rig.path
	if rigPath == "" {
		return "", errors.New("editor: save the rig to a file before placing it in a scene")
	}
	if rel, ok := e.projectRel(rigPath); ok {
		rigPath = rel
	} else {
		rigPath = filepath.ToSlash(rigPath)
		if !validScenePath(rigPath) || strings.ContainsRune(rigPath, '\\') || filepath.IsAbs(e.rig.path) {
			return "", fmt.Errorf("editor: rig path %q must be a slash-separated path inside the project before placing it in a scene", e.rig.path)
		}
	}
	return e.PlaceRig(layer, rigPath, e.rig.atlasName, e.rig.skin, e.rig.animation, x, y)
}
