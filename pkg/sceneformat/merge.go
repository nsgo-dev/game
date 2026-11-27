package sceneformat

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// MergeResult is the outcome of a three-way scene merge.
type MergeResult struct {
	// Doc is the merged scene document. When len(Conflicts) == 0, Doc passes
	// Validate and can be written directly with Encode. When len(Conflicts) > 0,
	// Doc holds the best-effort merged scene (preferring ours on conflicted
	// fields) for tools such as an editor conflict resolver.
	Doc *SceneDoc
	// Merged lists items joined cleanly by identity from one or both sides,
	// sorted deterministically.
	Merged []MergedItem
	// Conflicts lists every conflict encountered, sorted deterministically.
	// Empty on a clean merge.
	Conflicts []MergeConflict
}

// HasConflicts reports whether the merge encountered any conflicts.
func (r *MergeResult) HasConflicts() bool {
	return r != nil && len(r.Conflicts) > 0
}

// ConflictDoc returns a versioned ConflictDoc for scenePath when r has
// conflicts, or nil when the merge is clean.
func (r *MergeResult) ConflictDoc(scenePath string) *ConflictDoc {
	if !r.HasConflicts() {
		return nil
	}
	return normalizeConflictDoc(&ConflictDoc{
		Version:   ConflictVersion,
		Path:      scenePath,
		Merged:    r.Merged,
		Conflicts: r.Conflicts,
	})
}

// Merge performs an identity-based three-way merge of base, ours and theirs.
//
// Layers are matched by name, objects by id anywhere in the scene tree
// (including across reparenting and layer moves), tilemaps and overrides by
// key, and connections by (from, event, to, handler). Fields are merged one by
// one so edits to different objects or different fields on the same object
// combine cleanly.
//
// base may be nil for an add/add merge where both branches created the scene
// independently. ours and theirs must be non-nil valid SceneDoc values (and
// base, if non-nil, must be valid). The schema version merges monotonically to
// the highest version across base, ours and theirs (never below base) rather
// than conflicting when two branches upgrade independently. The combined result
// is validated with Validate; a merged document that fails validation is
// reported as a ConflictValidation conflict.
func Merge(base, ours, theirs *SceneDoc) (*MergeResult, error) {
	if ours == nil {
		return nil, errors.New("sceneformat: merge: ours scene is nil")
	}
	if theirs == nil {
		return nil, errors.New("sceneformat: merge: theirs scene is nil")
	}
	if base != nil {
		if err := Validate(base); err != nil {
			return nil, fmt.Errorf("sceneformat: merge: invalid base scene: %w", err)
		}
	}
	if err := Validate(ours); err != nil {
		return nil, fmt.Errorf("sceneformat: merge: invalid ours scene: %w", err)
	}
	if err := Validate(theirs); err != nil {
		return nil, fmt.Errorf("sceneformat: merge: invalid theirs scene: %w", err)
	}

	m := &sceneMerger{
		hasBase: base != nil,
		base:    base,
		ours:    ours,
		theirs:  theirs,
	}
	if m.base == nil {
		m.base = &SceneDoc{}
	}
	return m.run(), nil
}

type sceneMerger struct {
	hasBase bool
	base    *SceneDoc
	ours    *SceneDoc
	theirs  *SceneDoc

	merged    []MergedItem
	conflicts []MergeConflict
}

func (m *sceneMerger) addMerged(scope, target, side, action string) {
	m.merged = append(m.merged, MergedItem{
		Scope:  scope,
		Target: target,
		Side:   side,
		Action: action,
	})
}

func (m *sceneMerger) addConflict(c MergeConflict) {
	if !m.hasBase && c.Kind == ConflictField && c.Base == nil {
		c.Kind = ConflictAddAdd
	}
	m.conflicts = append(m.conflicts, c)
}

func (m *sceneMerger) run() *MergeResult {
	out := &SceneDoc{}

	// 1. Top-level scene fields.
	sceneSide := sideTracker{}
	out.Version = mergeVersion(&sceneSide, m.base.Version, m.ours.Version, m.theirs.Version)
	out.UID = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "uid", m.base.UID, m.ours.UID, m.theirs.UID)
	out.Inherits = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "inherits", m.base.Inherits, m.ours.Inherits, m.theirs.Inherits)
	out.Name = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "name", m.base.Name, m.ours.Name, m.theirs.Name)
	out.NavMesh = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "navMesh", m.base.NavMesh, m.ours.NavMesh, m.theirs.NavMesh)
	out.VirtualWidth = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "virtualWidth", m.base.VirtualWidth, m.ours.VirtualWidth, m.theirs.VirtualWidth)
	out.VirtualHeight = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "virtualHeight", m.base.VirtualHeight, m.ours.VirtualHeight, m.theirs.VirtualHeight)
	out.BackgroundColor = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "backgroundColor", m.base.BackgroundColor, m.ours.BackgroundColor, m.theirs.BackgroundColor)
	out.AmbientColor = mergeScalar(m, &sceneSide, conflictCtx{scope: "scene"}, "ambientColor", m.base.AmbientColor, m.ours.AmbientColor, m.theirs.AmbientColor)
	out.Camera = m.mergeCamera(&sceneSide, m.base.Camera, m.ours.Camera, m.theirs.Camera)
	out.Environment = m.mergeEnvironment(&sceneSide, m.base.Environment, m.ours.Environment, m.theirs.Environment)
	if side := sceneSide.side(); side != "" {
		m.addMerged("scene", "", side, "changed")
	}

	// 2. Top-level maps: tilemaps, layerOverrides, overrides.
	out.Tilemaps = m.mergeTilemapMap(m.base.Tilemaps, m.ours.Tilemaps, m.theirs.Tilemaps)
	out.LayerOverrides = m.mergeLayerOverrides(m.base.LayerOverrides, m.ours.LayerOverrides, m.theirs.LayerOverrides)
	out.Overrides = m.mergeObjectOverrides(nil, conflictCtx{scope: "override"}, "", m.base.Overrides, m.ours.Overrides, m.theirs.Overrides)

	// 3. Layers (matched by Name) and Objects (matched by ID anywhere in the tree).
	out.Layers = m.mergeLayersAndObjects()

	// 4. Connections (matched by from, event, to, handler).
	out.Connections = m.mergeConnections()

	// 5. Validation gate: any merged scene that fails Validate is a conflict.
	if len(m.conflicts) == 0 {
		if err := Validate(out); err != nil {
			field := ""
			var fe *FieldError
			if errors.As(err, &fe) && len(fe.Path) > 0 {
				field = strings.Join(fe.Path, ".")
			}
			m.conflicts = append(m.conflicts, MergeConflict{
				Kind:    ConflictValidation,
				Scope:   "scene",
				Field:   field,
				Message: err.Error(),
			})
		}
	}

	sortMergedItems(m.merged)
	sortConflicts(m.conflicts)

	return &MergeResult{
		Doc:       out,
		Merged:    m.merged,
		Conflicts: m.conflicts,
	}
}

type conflictCtx struct {
	scope  string
	layer  string
	object string
	target string
}

type sideTracker struct {
	ours   bool
	theirs bool
}

func (s *sideTracker) mark(ours, theirs bool) {
	if s == nil {
		return
	}
	if ours {
		s.ours = true
	}
	if theirs {
		s.theirs = true
	}
}

func (s *sideTracker) side() string {
	switch {
	case s.ours && s.theirs:
		return "both"
	case s.ours:
		return "ours"
	case s.theirs:
		return "theirs"
	default:
		return ""
	}
}

// mergeVersion combines schema versions monotonically: it picks the highest
// version across base, ours and theirs (never dropping below base) and never
// emits a field conflict on version itself. Compatibility of the combined
// document at that version is enforced by the post-merge Validate gate.
func mergeVersion(st *sideTracker, base, ours, theirs int) int {
	v := max(base, ours, theirs)
	if v != base {
		st.mark(ours > base, theirs > base)
	}
	return v
}

func mergeScalar[T comparable](m *sceneMerger, st *sideTracker, ctx conflictCtx, field string, base, ours, theirs T) T {
	if ours == theirs {
		if ours != base {
			st.mark(true, true)
		}
		return ours
	}
	if ours == base {
		st.mark(false, true)
		return theirs
	}
	if theirs == base {
		st.mark(true, false)
		return ours
	}
	var baseVal any = base
	if !m.hasBase {
		baseVal = nil
	}
	m.addConflict(MergeConflict{
		Kind:   ConflictField,
		Scope:  ctx.scope,
		Layer:  ctx.layer,
		Object: ctx.object,
		Target: ctx.target,
		Field:  field,
		Base:   toYAMLValue(baseVal),
		Ours:   toYAMLValue(ours),
		Theirs: toYAMLValue(theirs),
	})
	return ours
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func mergePtr[T comparable](m *sceneMerger, st *sideTracker, ctx conflictCtx, field string, base, ours, theirs *T) *T {
	if ptrEqual(ours, theirs) {
		if !ptrEqual(ours, base) {
			st.mark(true, true)
		}
		return clonePtr(ours)
	}
	if ptrEqual(ours, base) {
		st.mark(false, true)
		return clonePtr(theirs)
	}
	if ptrEqual(theirs, base) {
		st.mark(true, false)
		return clonePtr(ours)
	}
	var baseVal any
	if m.hasBase && base != nil {
		baseVal = *base
	}
	var oursVal, theirsVal any
	if ours != nil {
		oursVal = *ours
	}
	if theirs != nil {
		theirsVal = *theirs
	}
	m.addConflict(MergeConflict{
		Kind:   ConflictField,
		Scope:  ctx.scope,
		Layer:  ctx.layer,
		Object: ctx.object,
		Target: ctx.target,
		Field:  field,
		Base:   toYAMLValue(baseVal),
		Ours:   toYAMLValue(oursVal),
		Theirs: toYAMLValue(theirsVal),
	})
	return clonePtr(ours)
}

func (m *sceneMerger) mergeCamera(st *sideTracker, base, ours, theirs *CameraDoc) *CameraDoc {
	if cameraEqual(ours, theirs) {
		if !cameraEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneCamera(ours)
	}
	if cameraEqual(ours, base) {
		st.mark(false, true)
		return cloneCamera(theirs)
	}
	if cameraEqual(theirs, base) {
		st.mark(true, false)
		return cloneCamera(ours)
	}
	// Both changed camera.
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   "scene",
			Field:   "camera",
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneCamera(ours)
	}
	// Both non-nil (base may be nil if both added a camera): merge field by field.
	b := base
	if b == nil {
		b = &CameraDoc{}
	}
	ctx := conflictCtx{scope: "scene"}
	out := &CameraDoc{
		X:        mergeScalar(m, st, ctx, "camera.x", b.X, ours.X, theirs.X),
		Y:        mergeScalar(m, st, ctx, "camera.y", b.Y, ours.Y, theirs.Y),
		Zoom:     mergePtr(m, st, ctx, "camera.zoom", b.Zoom, ours.Zoom, theirs.Zoom),
		Rotation: mergeScalar(m, st, ctx, "camera.rotation", b.Rotation, ours.Rotation, theirs.Rotation),
		Bounds:   m.mergeBounds(st, b.Bounds, ours.Bounds, theirs.Bounds),
	}
	return out
}

func (m *sceneMerger) mergeBounds(st *sideTracker, base, ours, theirs *BoundsDoc) *BoundsDoc {
	if boundsEqual(ours, theirs) {
		if !boundsEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneBounds(ours)
	}
	if boundsEqual(ours, base) {
		st.mark(false, true)
		return cloneBounds(theirs)
	}
	if boundsEqual(theirs, base) {
		st.mark(true, false)
		return cloneBounds(ours)
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   "scene",
			Field:   "camera.bounds",
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneBounds(ours)
	}
	b := base
	if b == nil {
		b = &BoundsDoc{}
	}
	ctx := conflictCtx{scope: "scene"}
	return &BoundsDoc{
		X:      mergeScalar(m, st, ctx, "camera.bounds.x", b.X, ours.X, theirs.X),
		Y:      mergeScalar(m, st, ctx, "camera.bounds.y", b.Y, ours.Y, theirs.Y),
		Width:  mergeScalar(m, st, ctx, "camera.bounds.width", b.Width, ours.Width, theirs.Width),
		Height: mergeScalar(m, st, ctx, "camera.bounds.height", b.Height, ours.Height, theirs.Height),
	}
}

func (m *sceneMerger) mergeEnvironment(st *sideTracker, base, ours, theirs *EnvironmentDoc) *EnvironmentDoc {
	if environmentEqual(ours, theirs) {
		if !environmentEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if environmentEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if environmentEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   "scene",
			Field:   "environment",
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil {
		b = &EnvironmentDoc{}
	}
	ctx := conflictCtx{scope: "scene"}
	return &EnvironmentDoc{
		Sky:              m.mergeSky(st, b.Sky, ours.Sky, theirs.Sky),
		AmbientColor:     mergeScalar(m, st, ctx, "environment.ambientColor", b.AmbientColor, ours.AmbientColor, theirs.AmbientColor),
		AmbientIntensity: mergePtr(m, st, ctx, "environment.ambientIntensity", b.AmbientIntensity, ours.AmbientIntensity, theirs.AmbientIntensity),
		IBLIntensity:     mergePtr(m, st, ctx, "environment.iblIntensity", b.IBLIntensity, ours.IBLIntensity, theirs.IBLIntensity),
		Fog:              m.mergeFog(st, b.Fog, ours.Fog, theirs.Fog),
		Tonemapping:      mergeScalar(m, st, ctx, "environment.tonemapping", b.Tonemapping, ours.Tonemapping, theirs.Tonemapping),
		Exposure:         mergePtr(m, st, ctx, "environment.exposure", b.Exposure, ours.Exposure, theirs.Exposure),
	}
}

func (m *sceneMerger) mergeSky(st *sideTracker, base, ours, theirs *SkyDoc) *SkyDoc {
	if skyEqual(ours, theirs) {
		if !skyEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if skyEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if skyEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   "scene",
			Field:   "environment.sky",
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	if ours.Kind != theirs.Kind {
		m.addConflict(MergeConflict{
			Kind:    ConflictField,
			Scope:   "scene",
			Field:   "environment.sky",
			Message: "both sides changed sky kind differently",
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil || b.Kind != ours.Kind {
		b = &SkyDoc{Kind: ours.Kind}
	}
	ctx := conflictCtx{scope: "scene"}
	return &SkyDoc{
		Kind:    ours.Kind,
		Color:   mergeScalar(m, st, ctx, "environment.sky.color", b.Color, ours.Color, theirs.Color),
		Zenith:  mergeScalar(m, st, ctx, "environment.sky.zenith", b.Zenith, ours.Zenith, theirs.Zenith),
		Horizon: mergeScalar(m, st, ctx, "environment.sky.horizon", b.Horizon, ours.Horizon, theirs.Horizon),
		Ground:  mergeScalar(m, st, ctx, "environment.sky.ground", b.Ground, ours.Ground, theirs.Ground),
		HDR:     mergeScalar(m, st, ctx, "environment.sky.hdr", b.HDR, ours.HDR, theirs.HDR),
	}
}

func (m *sceneMerger) mergeFog(st *sideTracker, base, ours, theirs *FogDoc) *FogDoc {
	if fogEqual(ours, theirs) {
		if !fogEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if fogEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if fogEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   "scene",
			Field:   "environment.fog",
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil {
		b = &FogDoc{}
	}
	ctx := conflictCtx{scope: "scene"}
	return &FogDoc{
		Mode:          mergeScalar(m, st, ctx, "environment.fog.mode", b.Mode, ours.Mode, theirs.Mode),
		Color:         mergeScalar(m, st, ctx, "environment.fog.color", b.Color, ours.Color, theirs.Color),
		Start:         mergeScalar(m, st, ctx, "environment.fog.start", b.Start, ours.Start, theirs.Start),
		End:           mergeScalar(m, st, ctx, "environment.fog.end", b.End, ours.End, theirs.End),
		Density:       mergeScalar(m, st, ctx, "environment.fog.density", b.Density, ours.Density, theirs.Density),
		MaxOpacity:    mergePtr(m, st, ctx, "environment.fog.maxOpacity", b.MaxOpacity, ours.MaxOpacity, theirs.MaxOpacity),
		HeightEnabled: mergeScalar(m, st, ctx, "environment.fog.heightEnabled", b.HeightEnabled, ours.HeightEnabled, theirs.HeightEnabled),
		HeightBase:    mergeScalar(m, st, ctx, "environment.fog.heightBase", b.HeightBase, ours.HeightBase, theirs.HeightBase),
		HeightFalloff: mergeScalar(m, st, ctx, "environment.fog.heightFalloff", b.HeightFalloff, ours.HeightFalloff, theirs.HeightFalloff),
		HeightDensity: mergeScalar(m, st, ctx, "environment.fog.heightDensity", b.HeightDensity, ours.HeightDensity, theirs.HeightDensity),
	}
}

func (m *sceneMerger) mergeTilemapMap(base, ours, theirs map[string]*TilemapDoc) map[string]*TilemapDoc {
	keys := unionKeys(base, ours, theirs)
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]*TilemapDoc)
	for _, k := range keys {
		b, hasB := base[k]
		o, hasO := ours[k]
		t, hasT := theirs[k]
		ctx := conflictCtx{scope: "tilemap", target: k}
		switch {
		case !hasB && hasO && !hasT:
			out[k] = cloneTilemap(o)
			m.addMerged("tilemap", k, "ours", "added")
		case !hasB && !hasO && hasT:
			out[k] = cloneTilemap(t)
			m.addMerged("tilemap", k, "theirs", "added")
		case !hasB && hasO && hasT:
			if tilemapEqual(o, t) {
				out[k] = cloneTilemap(o)
				m.addMerged("tilemap", k, "both", "added")
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  "tilemap",
					Target: k,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				out[k] = cloneTilemap(o)
			}
		case hasB && !hasO && !hasT:
			// Deleted on both sides.
		case hasB && !hasO && hasT:
			if tilemapEqual(b, t) {
				m.addMerged("tilemap", k, "ours", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "tilemap",
					Target:  k,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(b),
					Ours:    nil,
					Theirs:  toYAMLValue(t),
				})
			}
		case hasB && hasO && !hasT:
			if tilemapEqual(b, o) {
				m.addMerged("tilemap", k, "theirs", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "tilemap",
					Target:  k,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(b),
					Ours:    toYAMLValue(o),
					Theirs:  nil,
				})
				out[k] = cloneTilemap(o)
			}
		case hasB && hasO && hasT:
			st := sideTracker{}
			merged := m.mergeTilemap(&st, ctx, "", b, o, t)
			out[k] = merged
			if side := st.side(); side != "" {
				m.addMerged("tilemap", k, side, "changed")
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *sceneMerger) mergeTilemap(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *TilemapDoc) *TilemapDoc {
	if tilemapEqual(ours, theirs) {
		if !tilemapEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneTilemap(ours)
	}
	if tilemapEqual(ours, base) {
		st.mark(false, true)
		return cloneTilemap(theirs)
	}
	if tilemapEqual(theirs, base) {
		st.mark(true, false)
		return cloneTilemap(ours)
	}
	fieldName := func(sub string) string {
		if prefix == "" {
			return sub
		}
		return prefix + "." + sub
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneTilemap(ours)
	}
	b := base
	if b == nil {
		b = &TilemapDoc{}
	}
	out := &TilemapDoc{
		Ref:         mergeScalar(m, st, ctx, fieldName("ref"), b.Ref, ours.Ref, theirs.Ref),
		Source:      mergeScalar(m, st, ctx, fieldName("source"), b.Source, ours.Source, theirs.Source),
		Level:       mergeScalar(m, st, ctx, fieldName("level"), b.Level, ours.Level, theirs.Level),
		OffsetX:     mergeScalar(m, st, ctx, fieldName("offsetX"), b.OffsetX, ours.OffsetX, theirs.OffsetX),
		OffsetY:     mergeScalar(m, st, ctx, fieldName("offsetY"), b.OffsetY, ours.OffsetY, theirs.OffsetY),
		TileWidth:   mergeScalar(m, st, ctx, fieldName("tileWidth"), b.TileWidth, ours.TileWidth, theirs.TileWidth),
		TileHeight:  mergeScalar(m, st, ctx, fieldName("tileHeight"), b.TileHeight, ours.TileHeight, theirs.TileHeight),
		Width:       mergeScalar(m, st, ctx, fieldName("width"), b.Width, ours.Width, theirs.Width),
		Height:      mergeScalar(m, st, ctx, fieldName("height"), b.Height, ours.Height, theirs.Height),
		Tileset:     mergeScalar(m, st, ctx, fieldName("tileset"), b.Tileset, ours.Tileset, theirs.Tileset),
		TilesetFile: mergeScalar(m, st, ctx, fieldName("tilesetFile"), b.TilesetFile, ours.TilesetFile, theirs.TilesetFile),
	}
	// Merge Data slice atomically.
	switch {
	case slices.Equal(ours.Data, theirs.Data):
		if !slices.Equal(ours.Data, b.Data) {
			st.mark(true, true)
		}
		out.Data = slices.Clone(ours.Data)
	case slices.Equal(ours.Data, b.Data):
		st.mark(false, true)
		out.Data = slices.Clone(theirs.Data)
	case slices.Equal(theirs.Data, b.Data):
		st.mark(true, false)
		out.Data = slices.Clone(ours.Data)
	default:
		var baseVal any = slices.Clone(b.Data)
		if !m.hasBase || base == nil {
			baseVal = nil
		}
		m.addConflict(MergeConflict{
			Kind:   ConflictField,
			Scope:  ctx.scope,
			Layer:  ctx.layer,
			Object: ctx.object,
			Target: ctx.target,
			Field:  fieldName("data"),
			Base:   toYAMLValue(baseVal),
			Ours:   toYAMLValue(ours.Data),
			Theirs: toYAMLValue(theirs.Data),
		})
		out.Data = slices.Clone(ours.Data)
	}
	// Merge TileLayers slice atomically.
	switch {
	case tileLayersEqual(ours.TileLayers, theirs.TileLayers):
		if !tileLayersEqual(ours.TileLayers, b.TileLayers) {
			st.mark(true, true)
		}
		out.TileLayers = cloneTileLayers(ours.TileLayers)
	case tileLayersEqual(ours.TileLayers, b.TileLayers):
		st.mark(false, true)
		out.TileLayers = cloneTileLayers(theirs.TileLayers)
	case tileLayersEqual(theirs.TileLayers, b.TileLayers):
		st.mark(true, false)
		out.TileLayers = cloneTileLayers(ours.TileLayers)
	default:
		var baseVal any = cloneTileLayers(b.TileLayers)
		if !m.hasBase || base == nil {
			baseVal = nil
		}
		m.addConflict(MergeConflict{
			Kind:   ConflictField,
			Scope:  ctx.scope,
			Layer:  ctx.layer,
			Object: ctx.object,
			Target: ctx.target,
			Field:  fieldName("tileLayers"),
			Base:   toYAMLValue(baseVal),
			Ours:   toYAMLValue(ours.TileLayers),
			Theirs: toYAMLValue(theirs.TileLayers),
		})
		out.TileLayers = cloneTileLayers(ours.TileLayers)
	}
	return out
}

func (m *sceneMerger) mergeLayerOverrides(base, ours, theirs map[string]LayerOverride) map[string]LayerOverride {
	keys := unionKeys(base, ours, theirs)
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]LayerOverride)
	for _, k := range keys {
		b, hasB := base[k]
		o, hasO := ours[k]
		t, hasT := theirs[k]
		ctx := conflictCtx{scope: "layerOverride", target: k}
		switch {
		case !hasB && hasO && !hasT:
			out[k] = cloneLayerOverride(o)
			m.addMerged("layerOverride", k, "ours", "added")
		case !hasB && !hasO && hasT:
			out[k] = cloneLayerOverride(t)
			m.addMerged("layerOverride", k, "theirs", "added")
		case !hasB && hasO && hasT:
			if layerOverrideEqual(o, t) {
				out[k] = cloneLayerOverride(o)
				m.addMerged("layerOverride", k, "both", "added")
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  "layerOverride",
					Target: k,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				out[k] = cloneLayerOverride(o)
			}
		case hasB && !hasO && !hasT:
		case hasB && !hasO && hasT:
			if layerOverrideEqual(b, t) {
				m.addMerged("layerOverride", k, "ours", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "layerOverride",
					Target:  k,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(b),
					Ours:    nil,
					Theirs:  toYAMLValue(t),
				})
			}
		case hasB && hasO && !hasT:
			if layerOverrideEqual(b, o) {
				m.addMerged("layerOverride", k, "theirs", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "layerOverride",
					Target:  k,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(b),
					Ours:    toYAMLValue(o),
					Theirs:  nil,
				})
				out[k] = cloneLayerOverride(o)
			}
		case hasB && hasO && hasT:
			st := sideTracker{}
			out[k] = LayerOverride{
				Space:        mergeScalar(m, &st, ctx, "space", b.Space, o.Space, t.Space),
				Order:        mergePtr(m, &st, ctx, "order", b.Order, o.Order, t.Order),
				Visible:      mergePtr(m, &st, ctx, "visible", b.Visible, o.Visible, t.Visible),
				ParallaxX:    mergePtr(m, &st, ctx, "parallaxX", b.ParallaxX, o.ParallaxX, t.ParallaxX),
				ParallaxY:    mergePtr(m, &st, ctx, "parallaxY", b.ParallaxY, o.ParallaxY, t.ParallaxY),
				RepeatX:      mergePtr(m, &st, ctx, "repeatX", b.RepeatX, o.RepeatX, t.RepeatX),
				RepeatY:      mergePtr(m, &st, ctx, "repeatY", b.RepeatY, o.RepeatY, t.RepeatY),
				RepeatWidth:  mergePtr(m, &st, ctx, "repeatWidth", b.RepeatWidth, o.RepeatWidth, t.RepeatWidth),
				RepeatHeight: mergePtr(m, &st, ctx, "repeatHeight", b.RepeatHeight, o.RepeatHeight, t.RepeatHeight),
				Tilemap:      m.mergeTilemap(&st, ctx, "tilemap", b.Tilemap, o.Tilemap, t.Tilemap),
			}
			if side := st.side(); side != "" {
				m.addMerged("layerOverride", k, side, "changed")
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *sceneMerger) mergeObjectOverrides(parentSide *sideTracker, parentCtx conflictCtx, fieldPrefix string, base, ours, theirs map[string]ObjectOverride) map[string]ObjectOverride {
	keys := unionKeys(base, ours, theirs)
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]ObjectOverride)
	isTopLevel := fieldPrefix == ""
	for _, k := range keys {
		b, hasB := base[k]
		o, hasO := ours[k]
		t, hasT := theirs[k]
		ctx := parentCtx
		prefix := ""
		if isTopLevel {
			ctx.target = k
		} else {
			prefix = fieldPrefix + "." + k
		}
		switch {
		case !hasB && hasO && !hasT:
			out[k] = cloneObjectOverride(o)
			parentSide.mark(true, false)
			if isTopLevel {
				m.addMerged("override", k, "ours", "added")
			}
		case !hasB && !hasO && hasT:
			out[k] = cloneObjectOverride(t)
			parentSide.mark(false, true)
			if isTopLevel {
				m.addMerged("override", k, "theirs", "added")
			}
		case !hasB && hasO && hasT:
			if objectOverrideEqual(o, t) {
				out[k] = cloneObjectOverride(o)
				parentSide.mark(true, true)
				if isTopLevel {
					m.addMerged("override", k, "both", "added")
				}
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  ctx.scope,
					Layer:  ctx.layer,
					Object: ctx.object,
					Target: ctx.target,
					Field:  prefix,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				out[k] = cloneObjectOverride(o)
			}
		case hasB && !hasO && !hasT:
			parentSide.mark(true, true)
		case hasB && !hasO && hasT:
			if objectOverrideEqual(b, t) {
				parentSide.mark(true, false)
				if isTopLevel {
					m.addMerged("override", k, "ours", "deleted")
				}
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   ctx.scope,
					Layer:   ctx.layer,
					Object:  ctx.object,
					Target:  ctx.target,
					Field:   prefix,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(b),
					Ours:    nil,
					Theirs:  toYAMLValue(t),
				})
			}
		case hasB && hasO && !hasT:
			if objectOverrideEqual(b, o) {
				parentSide.mark(false, true)
				if isTopLevel {
					m.addMerged("override", k, "theirs", "deleted")
				}
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   ctx.scope,
					Layer:   ctx.layer,
					Object:  ctx.object,
					Target:  ctx.target,
					Field:   prefix,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(b),
					Ours:    toYAMLValue(o),
					Theirs:  nil,
				})
				out[k] = cloneObjectOverride(o)
			}
		case hasB && hasO && hasT:
			st := sideTracker{}
			out[k] = m.mergeObjectOverrideFields(&st, ctx, prefix, b, o, t)
			parentSide.mark(st.ours, st.theirs)
			if isTopLevel {
				if side := st.side(); side != "" {
					m.addMerged("override", k, side, "changed")
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *sceneMerger) mergeObjectOverrideFields(st *sideTracker, ctx conflictCtx, prefix string, b, o, t ObjectOverride) ObjectOverride {
	fn := func(sub string) string {
		if prefix == "" {
			return sub
		}
		return prefix + "." + sub
	}
	return ObjectOverride{
		Name:            mergePtr(m, st, ctx, fn("name"), b.Name, o.Name, t.Name),
		Prefab:          mergePtr(m, st, ctx, fn("prefab"), b.Prefab, o.Prefab, t.Prefab),
		X:               mergePtr(m, st, ctx, fn("x"), b.X, o.X, t.X),
		Y:               mergePtr(m, st, ctx, fn("y"), b.Y, o.Y, t.Y),
		PosZ:            mergePtr(m, st, ctx, fn("posZ"), b.PosZ, o.PosZ, t.PosZ),
		OriginX:         mergePtr(m, st, ctx, fn("originX"), b.OriginX, o.OriginX, t.OriginX),
		OriginY:         mergePtr(m, st, ctx, fn("originY"), b.OriginY, o.OriginY, t.OriginY),
		Rotation:        mergePtr(m, st, ctx, fn("rotation"), b.Rotation, o.Rotation, t.Rotation),
		RotX:            mergePtr(m, st, ctx, fn("rotX"), b.RotX, o.RotX, t.RotX),
		RotY:            mergePtr(m, st, ctx, fn("rotY"), b.RotY, o.RotY, t.RotY),
		RotZ:            mergePtr(m, st, ctx, fn("rotZ"), b.RotZ, o.RotZ, t.RotZ),
		ScaleX:          mergePtr(m, st, ctx, fn("scaleX"), b.ScaleX, o.ScaleX, t.ScaleX),
		ScaleY:          mergePtr(m, st, ctx, fn("scaleY"), b.ScaleY, o.ScaleY, t.ScaleY),
		ScaleZ:          mergePtr(m, st, ctx, fn("scaleZ"), b.ScaleZ, o.ScaleZ, t.ScaleZ),
		Depth:           mergePtr(m, st, ctx, fn("depth"), b.Depth, o.Depth, t.Depth),
		Mesh:            mergePtr(m, st, ctx, fn("mesh"), b.Mesh, o.Mesh, t.Mesh),
		Model:           mergePtr(m, st, ctx, fn("model"), b.Model, o.Model, t.Model),
		ModelNode:       mergePtr(m, st, ctx, fn("modelNode"), b.ModelNode, o.ModelNode, t.ModelNode),
		ModelMesh:       mergePtr(m, st, ctx, fn("modelMesh"), b.ModelMesh, o.ModelMesh, t.ModelMesh),
		GridMap:         mergePtr(m, st, ctx, fn("gridMap"), b.GridMap, o.GridMap, t.GridMap),
		Occluder:        m.mergeOccluder(st, ctx, fn("occluder"), b.Occluder, o.Occluder, t.Occluder),
		FlipX:           mergePtr(m, st, ctx, fn("flipX"), b.FlipX, o.FlipX, t.FlipX),
		FlipY:           mergePtr(m, st, ctx, fn("flipY"), b.FlipY, o.FlipY, t.FlipY),
		Alpha:           mergePtr(m, st, ctx, fn("alpha"), b.Alpha, o.Alpha, t.Alpha),
		Z:               mergePtr(m, st, ctx, fn("z"), b.Z, o.Z, t.Z),
		Sprite:          m.mergeSprite(st, ctx, fn("sprite"), b.Sprite, o.Sprite, t.Sprite),
		Material:        mergePtr(m, st, ctx, fn("material"), b.Material, o.Material, t.Material),
		Particles:       m.mergeParticles(st, ctx, fn("particles"), b.Particles, o.Particles, t.Particles),
		Light:           m.mergeLight(st, ctx, fn("light"), b.Light, o.Light, t.Light),
		Camera:          m.mergeCamera3D(st, ctx, fn("camera"), b.Camera, o.Camera, t.Camera),
		ReflectionProbe: m.mergeReflectionProbe(st, ctx, fn("reflectionProbe"), b.ReflectionProbe, o.ReflectionProbe, t.ReflectionProbe),
		LightProbeGrid:  m.mergeLightProbeGrid(st, ctx, fn("lightProbeGrid"), b.LightProbeGrid, o.LightProbeGrid, t.LightProbeGrid),
		Components:      m.mergeAnyMap(st, ctx, fn("components"), b.Components, o.Components, t.Components),
		Properties:      m.mergeAnyMap(st, ctx, fn("properties"), b.Properties, o.Properties, t.Properties),
	}
}

func (m *sceneMerger) mergeSprite(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *SpriteRef) *SpriteRef {
	if spriteEqual(ours, theirs) {
		if !spriteEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneSprite(ours)
	}
	if spriteEqual(ours, base) {
		st.mark(false, true)
		return cloneSprite(theirs)
	}
	if spriteEqual(theirs, base) {
		st.mark(true, false)
		return cloneSprite(ours)
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneSprite(ours)
	}
	b := base
	if b == nil {
		b = &SpriteRef{}
	}
	return &SpriteRef{
		Atlas: mergeScalar(m, st, ctx, prefix+".atlas", b.Atlas, ours.Atlas, theirs.Atlas),
		Frame: mergeScalar(m, st, ctx, prefix+".frame", b.Frame, ours.Frame, theirs.Frame),
	}
}

func (m *sceneMerger) mergeOccluder(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *OccluderDoc) *OccluderDoc {
	if occluderEqual(ours, theirs) {
		if !occluderEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneOccluder(ours)
	}
	if occluderEqual(ours, base) {
		st.mark(false, true)
		return cloneOccluder(theirs)
	}
	if occluderEqual(theirs, base) {
		st.mark(true, false)
		return cloneOccluder(ours)
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneOccluder(ours)
	}
	if ours.Mode != theirs.Mode {
		m.addConflict(MergeConflict{
			Kind:    ConflictField,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: "both sides changed occluder mode differently",
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneOccluder(ours)
	}
	b := base
	if b == nil || b.Mode != ours.Mode {
		b = &OccluderDoc{Mode: ours.Mode}
	}
	return &OccluderDoc{
		Mode:        ours.Mode,
		Center:      mergePtr(m, st, ctx, prefix+".center", b.Center, ours.Center, theirs.Center),
		Size:        mergePtr(m, st, ctx, prefix+".size", b.Size, ours.Size, theirs.Size),
		DoubleSided: mergeScalar(m, st, ctx, prefix+".doubleSided", b.DoubleSided, ours.DoubleSided, theirs.DoubleSided),
	}
}

func (m *sceneMerger) mergeParticles(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *ParticlesRef) *ParticlesRef {
	if particlesEqual(ours, theirs) {
		if !particlesEqual(ours, base) {
			st.mark(true, true)
		}
		return cloneParticles(ours)
	}
	if particlesEqual(ours, base) {
		st.mark(false, true)
		return cloneParticles(theirs)
	}
	if particlesEqual(theirs, base) {
		st.mark(true, false)
		return cloneParticles(ours)
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return cloneParticles(ours)
	}
	b := base
	if b == nil {
		b = &ParticlesRef{}
	}
	return &ParticlesRef{
		File:  mergeScalar(m, st, ctx, prefix+".file", b.File, ours.File, theirs.File),
		Atlas: mergeScalar(m, st, ctx, prefix+".atlas", b.Atlas, ours.Atlas, theirs.Atlas),
	}
}

func (m *sceneMerger) mergeLight(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *LightDoc) *LightDoc {
	if lightEqual(ours, theirs) {
		if !lightEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if lightEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if lightEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	if ours.Kind != theirs.Kind {
		m.addConflict(MergeConflict{
			Kind:    ConflictField,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: "both sides changed light kind differently",
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil || b.Kind != ours.Kind {
		b = &LightDoc{Kind: ours.Kind}
	}
	return &LightDoc{
		Kind:      ours.Kind,
		Color:     mergeScalar(m, st, ctx, prefix+".color", b.Color, ours.Color, theirs.Color),
		Intensity: mergePtr(m, st, ctx, prefix+".intensity", b.Intensity, ours.Intensity, theirs.Intensity),
		Range:     mergeScalar(m, st, ctx, prefix+".range", b.Range, ours.Range, theirs.Range),
		InnerCone: mergeScalar(m, st, ctx, prefix+".innerCone", b.InnerCone, ours.InnerCone, theirs.InnerCone),
		OuterCone: mergePtr(m, st, ctx, prefix+".outerCone", b.OuterCone, ours.OuterCone, theirs.OuterCone),
		Shadows:   mergeScalar(m, st, ctx, prefix+".shadows", b.Shadows, ours.Shadows, theirs.Shadows),
	}
}

func (m *sceneMerger) mergeCamera3D(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *Camera3DDoc) *Camera3DDoc {
	if camera3DEqual(ours, theirs) {
		if !camera3DEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if camera3DEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if camera3DEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	if ours.EffectiveProjection() != theirs.EffectiveProjection() {
		m.addConflict(MergeConflict{
			Kind:    ConflictField,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: "both sides changed 3D camera projection differently",
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil || b.EffectiveProjection() != ours.EffectiveProjection() {
		b = &Camera3DDoc{Projection: ours.Projection}
	}
	return &Camera3DDoc{
		Projection:  mergeScalar(m, st, ctx, prefix+".projection", b.Projection, ours.Projection, theirs.Projection),
		FOV:         mergePtr(m, st, ctx, prefix+".fov", b.FOV, ours.FOV, theirs.FOV),
		OrthoHeight: mergePtr(m, st, ctx, prefix+".orthoHeight", b.OrthoHeight, ours.OrthoHeight, theirs.OrthoHeight),
		Near:        mergePtr(m, st, ctx, prefix+".near", b.Near, ours.Near, theirs.Near),
		Far:         mergePtr(m, st, ctx, prefix+".far", b.Far, ours.Far, theirs.Far),
		Current:     mergeScalar(m, st, ctx, prefix+".current", b.Current, ours.Current, theirs.Current),
	}
}

func (m *sceneMerger) mergeReflectionProbe(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *ReflectionProbeDoc) *ReflectionProbeDoc {
	if reflectionProbeEqual(ours, theirs) {
		if !reflectionProbeEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if reflectionProbeEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if reflectionProbeEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil {
		b = &ReflectionProbeDoc{}
	}
	return &ReflectionProbeDoc{
		Center:        mergePtr(m, st, ctx, prefix+".center", b.Center, ours.Center, theirs.Center),
		Size:          mergePtr(m, st, ctx, prefix+".size", b.Size, ours.Size, theirs.Size),
		BlendDistance: mergeScalar(m, st, ctx, prefix+".blendDistance", b.BlendDistance, ours.BlendDistance, theirs.BlendDistance),
		Intensity:     mergePtr(m, st, ctx, prefix+".intensity", b.Intensity, ours.Intensity, theirs.Intensity),
		Priority:      mergeScalar(m, st, ctx, prefix+".priority", b.Priority, ours.Priority, theirs.Priority),
		BoxProjection: mergePtr(m, st, ctx, prefix+".boxProjection", b.BoxProjection, ours.BoxProjection, theirs.BoxProjection),
		Resolution:    mergeScalar(m, st, ctx, prefix+".resolution", b.Resolution, ours.Resolution, theirs.Resolution),
		Cubemap:       mergeScalar(m, st, ctx, prefix+".cubemap", b.Cubemap, ours.Cubemap, theirs.Cubemap),
	}
}

func (m *sceneMerger) mergeLightProbeGrid(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs *LightProbeGridDoc) *LightProbeGridDoc {
	if lightProbeGridEqual(ours, theirs) {
		if !lightProbeGridEqual(ours, base) {
			st.mark(true, true)
		}
		return ours.Clone()
	}
	if lightProbeGridEqual(ours, base) {
		st.mark(false, true)
		return theirs.Clone()
	}
	if lightProbeGridEqual(theirs, base) {
		st.mark(true, false)
		return ours.Clone()
	}
	if base != nil && (ours == nil || theirs == nil) {
		msg := "deleted on ours, modified on theirs"
		if theirs == nil {
			msg = "modified on ours, deleted on theirs"
		}
		m.addConflict(MergeConflict{
			Kind:    ConflictDeleteModify,
			Scope:   ctx.scope,
			Layer:   ctx.layer,
			Object:  ctx.object,
			Target:  ctx.target,
			Field:   prefix,
			Message: msg,
			Base:    toYAMLValue(base),
			Ours:    toYAMLValue(ours),
			Theirs:  toYAMLValue(theirs),
		})
		return ours.Clone()
	}
	b := base
	if b == nil {
		b = &LightProbeGridDoc{}
	}
	return &LightProbeGridDoc{
		Center:    mergePtr(m, st, ctx, prefix+".center", b.Center, ours.Center, theirs.Center),
		Size:      mergePtr(m, st, ctx, prefix+".size", b.Size, ours.Size, theirs.Size),
		Counts:    mergePtr(m, st, ctx, prefix+".counts", b.Counts, ours.Counts, theirs.Counts),
		Intensity: mergePtr(m, st, ctx, prefix+".intensity", b.Intensity, ours.Intensity, theirs.Intensity),
		Data:      mergeScalar(m, st, ctx, prefix+".data", b.Data, ours.Data, theirs.Data),
	}
}
func (m *sceneMerger) mergeAnyMap(st *sideTracker, ctx conflictCtx, prefix string, base, ours, theirs map[string]any) map[string]any {
	keys := unionKeys(base, ours, theirs)
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]any)
	for _, k := range keys {
		b, hasB := base[k]
		o, hasO := ours[k]
		t, hasT := theirs[k]
		field := prefix + "." + k
		switch {
		case !hasB && hasO && !hasT:
			out[k] = cloneAny(o)
			st.mark(true, false)
		case !hasB && !hasO && hasT:
			out[k] = cloneAny(t)
			st.mark(false, true)
		case !hasB && hasO && hasT:
			if anyEqual(o, t) {
				out[k] = cloneAny(o)
				st.mark(true, true)
			} else if om, okO := o.(map[string]any); okO {
				if tm, okT := t.(map[string]any); okT {
					out[k] = m.mergeAnyMap(st, ctx, field, nil, om, tm)
					continue
				}
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  ctx.scope,
					Layer:  ctx.layer,
					Object: ctx.object,
					Target: ctx.target,
					Field:  field,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				out[k] = cloneAny(o)
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  ctx.scope,
					Layer:  ctx.layer,
					Object: ctx.object,
					Target: ctx.target,
					Field:  field,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				out[k] = cloneAny(o)
			}
		case hasB && !hasO && !hasT:
			st.mark(true, true)
		case hasB && !hasO && hasT:
			if anyEqual(b, t) {
				st.mark(true, false)
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   ctx.scope,
					Layer:   ctx.layer,
					Object:  ctx.object,
					Target:  ctx.target,
					Field:   field,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(b),
					Ours:    nil,
					Theirs:  toYAMLValue(t),
				})
			}
		case hasB && hasO && !hasT:
			if anyEqual(b, o) {
				st.mark(false, true)
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   ctx.scope,
					Layer:   ctx.layer,
					Object:  ctx.object,
					Target:  ctx.target,
					Field:   field,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(b),
					Ours:    toYAMLValue(o),
					Theirs:  nil,
				})
				out[k] = cloneAny(o)
			}
		case hasB && hasO && hasT:
			if anyEqual(o, t) {
				if !anyEqual(o, b) {
					st.mark(true, true)
				}
				out[k] = cloneAny(o)
			} else if anyEqual(o, b) {
				st.mark(false, true)
				out[k] = cloneAny(t)
			} else if anyEqual(t, b) {
				st.mark(true, false)
				out[k] = cloneAny(o)
			} else {
				bm, okB := b.(map[string]any)
				om, okO := o.(map[string]any)
				tm, okT := t.(map[string]any)
				if okB && okO && okT {
					out[k] = m.mergeAnyMap(st, ctx, field, bm, om, tm)
				} else {
					m.addConflict(MergeConflict{
						Kind:   ConflictField,
						Scope:  ctx.scope,
						Layer:  ctx.layer,
						Object: ctx.object,
						Target: ctx.target,
						Field:  field,
						Base:   toYAMLValue(b),
						Ours:   toYAMLValue(o),
						Theirs: toYAMLValue(t),
					})
					out[k] = cloneAny(o)
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parentRef identifies where an object sits in the scene tree: either top-level
// inside a named layer (parent == "") or directly under a parent object ID
// (layer == "", inheriting its root ancestor's layer).
type parentRef struct {
	layer  string
	parent string
}

func (p parentRef) String() string {
	if p.parent != "" {
		return "object:" + p.parent
	}
	return "layer:" + p.layer
}

type indexedObj struct {
	doc   *ObjectDoc
	ref   parentRef
	layer string // effective layer name in that document
}

func indexMergeObjects(doc *SceneDoc) (map[string]indexedObj, map[parentRef][]string) {
	byID := make(map[string]indexedObj)
	order := make(map[parentRef][]string)
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		var walk func(objs []ObjectDoc, ref parentRef)
		walk = func(objs []ObjectDoc, ref parentRef) {
			for i := range objs {
				o := &objs[i]
				byID[o.ID] = indexedObj{doc: o, ref: ref, layer: layer.Name}
				order[ref] = append(order[ref], o.ID)
				if len(o.Children) > 0 {
					walk(o.Children, parentRef{parent: o.ID})
				}
			}
		}
		walk(layer.Objects, parentRef{layer: layer.Name})
	}
	return byID, order
}

func indexLayers(doc *SceneDoc) (map[string]*LayerDoc, []string) {
	byName := make(map[string]*LayerDoc, len(doc.Layers))
	names := make([]string, 0, len(doc.Layers))
	for i := range doc.Layers {
		l := &doc.Layers[i]
		byName[l.Name] = l
		names = append(names, l.Name)
	}
	return byName, names
}

func (m *sceneMerger) mergeLayersAndObjects() []LayerDoc {
	baseLayers, baseLayerOrder := indexLayers(m.base)
	oursLayers, oursLayerOrder := indexLayers(m.ours)
	theirsLayers, theirsLayerOrder := indexLayers(m.theirs)

	baseObjs, baseChildOrder := indexMergeObjects(m.base)
	oursObjs, oursChildOrder := indexMergeObjects(m.ours)
	theirsObjs, theirsChildOrder := indexMergeObjects(m.theirs)

	// Step A: Merge all objects globally by ID.
	allObjIDs := unionKeys(baseObjs, oursObjs, theirsObjs)
	survivingObjs := make(map[string]*ObjectDoc)
	survivingParent := make(map[string]parentRef)
	deletedBySide := make(map[string]string) // id -> "ours" or "theirs"
	deleteModifyObjs := make(map[string]bool)

	for _, id := range allObjIDs {
		b, hasB := baseObjs[id]
		o, hasO := oursObjs[id]
		t, hasT := theirsObjs[id]

		switch {
		case !hasB && hasO && !hasT:
			survivingObjs[id] = cloneObjectOwn(o.doc)
			survivingParent[id] = o.ref
			m.addMerged("object", id, "ours", "added")

		case !hasB && !hasO && hasT:
			survivingObjs[id] = cloneObjectOwn(t.doc)
			survivingParent[id] = t.ref
			m.addMerged("object", id, "theirs", "added")

		case !hasB && hasO && hasT:
			if o.ref == t.ref && objectOwnEqual(o.doc, t.doc) {
				survivingObjs[id] = cloneObjectOwn(o.doc)
				survivingParent[id] = o.ref
				m.addMerged("object", id, "both", "added")
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  "object",
					Layer:  o.layer,
					Object: id,
					Base:   nil,
					Ours:   toYAMLValue(cloneObjectOwn(o.doc)),
					Theirs: toYAMLValue(cloneObjectOwn(t.doc)),
				})
				survivingObjs[id] = cloneObjectOwn(o.doc)
				survivingParent[id] = o.ref
			}

		case hasB && !hasO && !hasT:
			// Deleted on both sides.

		case hasB && !hasO && hasT:
			if t.ref == b.ref && objectOwnEqual(b.doc, t.doc) {
				deletedBySide[id] = "ours"
				m.addMerged("object", id, "ours", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "object",
					Layer:   b.layer,
					Object:  id,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(cloneObjectOwn(b.doc)),
					Ours:    nil,
					Theirs:  toYAMLValue(cloneObjectOwn(t.doc)),
				})
			}

		case hasB && hasO && !hasT:
			if o.ref == b.ref && objectOwnEqual(b.doc, o.doc) {
				deletedBySide[id] = "theirs"
				m.addMerged("object", id, "theirs", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "object",
					Layer:   b.layer,
					Object:  id,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(cloneObjectOwn(b.doc)),
					Ours:    toYAMLValue(cloneObjectOwn(o.doc)),
					Theirs:  nil,
				})
				deleteModifyObjs[id] = true
				survivingObjs[id] = cloneObjectOwn(o.doc)
				survivingParent[id] = o.ref
			}

		case hasB && hasO && hasT:
			// Merge parent/layer placement.
			moveTracker := sideTracker{}
			var finalRef parentRef
			switch {
			case o.ref == t.ref:
				if o.ref != b.ref {
					moveTracker.mark(true, true)
				}
				finalRef = o.ref
			case o.ref == b.ref:
				moveTracker.mark(false, true)
				finalRef = t.ref
			case t.ref == b.ref:
				moveTracker.mark(true, false)
				finalRef = o.ref
			default:
				field := "parent"
				if b.ref.parent == "" && o.ref.parent == "" && t.ref.parent == "" {
					field = "layer"
				}
				m.addConflict(MergeConflict{
					Kind:   ConflictMove,
					Scope:  "object",
					Layer:  b.layer,
					Object: id,
					Field:  field,
					Base:   formatLocation(b.layer, b.ref.parent),
					Ours:   formatLocation(o.layer, o.ref.parent),
					Theirs: formatLocation(t.layer, t.ref.parent),
				})
				finalRef = o.ref
			}

			// Determine effective layer label for conflict reporting.
			layerLabel := o.layer
			if finalRef.layer != "" {
				layerLabel = finalRef.layer
			}

			fieldTracker := sideTracker{}
			ctx := conflictCtx{scope: "object", layer: layerLabel, object: id}
			mergedObj := m.mergeObjectFields(&fieldTracker, ctx, b.doc, o.doc, t.doc)
			survivingObjs[id] = mergedObj
			survivingParent[id] = finalRef

			if side := moveTracker.side(); side != "" {
				m.addMerged("object", id, side, "moved")
			}
			if side := fieldTracker.side(); side != "" {
				m.addMerged("object", id, side, "changed")
			}
		}
	}

	// Step B: Merge layers by Name (metadata: Space, Order, Visible, Tilemap).
	allLayerNames := unionKeys(baseLayers, oursLayers, theirsLayers)
	survivingLayers := make(map[string]*LayerDoc)
	deletedLayerBySide := make(map[string]string)

	for _, name := range allLayerNames {
		b, hasB := baseLayers[name]
		o, hasO := oursLayers[name]
		t, hasT := theirsLayers[name]
		ctx := conflictCtx{scope: "layer", layer: name}

		switch {
		case !hasB && hasO && !hasT:
			survivingLayers[name] = cloneLayerMeta(o)
			m.addMerged("layer", name, "ours", "added")

		case !hasB && !hasO && hasT:
			survivingLayers[name] = cloneLayerMeta(t)
			m.addMerged("layer", name, "theirs", "added")

		case !hasB && hasO && hasT:
			if layerMetaEqual(o, t) {
				survivingLayers[name] = cloneLayerMeta(o)
				m.addMerged("layer", name, "both", "added")
			} else {
				// Both added a layer with the same name but differing metadata: merge metadata field by field (conflicts recorded per field).
				st := sideTracker{}
				zero := &LayerDoc{Name: name}
				survivingLayers[name] = m.mergeLayerMetaFields(&st, ctx, zero, o, t)
			}

		case hasB && !hasO && !hasT:
			// Deleted on both sides.

		case hasB && !hasO && hasT:
			if layerMetaEqual(b, t) {
				deletedLayerBySide[name] = "ours"
				m.addMerged("layer", name, "ours", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "layer",
					Layer:   name,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(cloneLayerMeta(b)),
					Ours:    nil,
					Theirs:  toYAMLValue(cloneLayerMeta(t)),
				})
			}

		case hasB && hasO && !hasT:
			if layerMetaEqual(b, o) {
				deletedLayerBySide[name] = "theirs"
				m.addMerged("layer", name, "theirs", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "layer",
					Layer:   name,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(cloneLayerMeta(b)),
					Ours:    toYAMLValue(cloneLayerMeta(o)),
					Theirs:  nil,
				})
				survivingLayers[name] = cloneLayerMeta(o)
			}

		case hasB && hasO && hasT:
			st := sideTracker{}
			survivingLayers[name] = m.mergeLayerMetaFields(&st, ctx, b, o, t)
			if side := st.side(); side != "" {
				m.addMerged("layer", name, side, "changed")
			}
		}
	}

	// Step C: Verify every surviving object resolves to a surviving layer without
	// dangling parents or reparenting cycles.
	dangling := make(map[string]bool)
	for _, id := range slices.Sorted(maps.Keys(survivingObjs)) {
		ref := survivingParent[id]
		if ref.parent != "" {
			if _, ok := survivingObjs[ref.parent]; !ok {
				if !deleteModifyObjs[id] {
					side := deletedBySide[ref.parent]
					if side == "" {
						side = "other"
					}
					m.addConflict(MergeConflict{
						Kind:    ConflictDeleteModify,
						Scope:   "object",
						Object:  id,
						Field:   "parent",
						Message: fmt.Sprintf("parent object %q was deleted on %s", ref.parent, side),
						Base:    parentLocationOrNil(baseObjs, id),
						Ours:    parentLocationOrNil(oursObjs, id),
						Theirs:  parentLocationOrNil(theirsObjs, id),
					})
				}
				dangling[id] = true
			}
		} else if _, ok := survivingLayers[ref.layer]; !ok {
			if !deleteModifyObjs[id] {
				side := deletedLayerBySide[ref.layer]
				if side == "" {
					side = "other"
				}
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "object",
					Layer:   ref.layer,
					Object:  id,
					Field:   "layer",
					Message: fmt.Sprintf("layer %q was deleted on %s", ref.layer, side),
					Base:    parentLocationOrNil(baseObjs, id),
					Ours:    parentLocationOrNil(oursObjs, id),
					Theirs:  parentLocationOrNil(theirsObjs, id),
				})
			}
			dangling[id] = true
		}
	}

	// Detect reparenting cycles among surviving objects.
	inCycle := make(map[string]bool)
	for _, id := range slices.Sorted(maps.Keys(survivingObjs)) {
		if dangling[id] || inCycle[id] {
			continue
		}
		visited := make(map[string]bool)
		cur := id
		for {
			if visited[cur] {
				// Mark all members of the cycle.
				cycleNode := cur
				for !inCycle[cycleNode] {
					inCycle[cycleNode] = true
					cycleNode = survivingParent[cycleNode].parent
				}
				break
			}
			visited[cur] = true
			ref := survivingParent[cur]
			if ref.parent == "" || dangling[ref.parent] {
				break
			}
			if _, ok := survivingObjs[ref.parent]; !ok {
				break
			}
			cur = ref.parent
		}
	}
	for _, id := range slices.Sorted(maps.Keys(inCycle)) {
		m.addConflict(MergeConflict{
			Kind:    ConflictMove,
			Scope:   "object",
			Object:  id,
			Field:   "parent",
			Message: fmt.Sprintf("reparenting object %q creates a cycle", id),
			Base:    parentLocationOrNil(baseObjs, id),
			Ours:    parentLocationOrNil(oursObjs, id),
			Theirs:  parentLocationOrNil(theirsObjs, id),
		})
	}

	// Step D: Group surviving children by parentRef and order deterministically.
	childrenByParent := make(map[parentRef][]string)
	for id, ref := range survivingParent {
		if dangling[id] || inCycle[id] {
			continue
		}
		childrenByParent[ref] = append(childrenByParent[ref], id)
	}
	for ref, ids := range childrenByParent {
		childrenByParent[ref] = mergeOrder(ids, baseChildOrder[ref], oursChildOrder[ref], theirsChildOrder[ref])
	}

	var buildChildren func(ref parentRef) []ObjectDoc
	buildChildren = func(ref parentRef) []ObjectDoc {
		ids := childrenByParent[ref]
		if len(ids) == 0 {
			return nil
		}
		out := make([]ObjectDoc, 0, len(ids))
		for _, id := range ids {
			obj := *survivingObjs[id]
			obj.Children = buildChildren(parentRef{parent: id})
			out = append(out, obj)
		}
		return out
	}

	survivingLayerNames := slices.Collect(maps.Keys(survivingLayers))
	orderedLayers := mergeOrder(survivingLayerNames, baseLayerOrder, oursLayerOrder, theirsLayerOrder)
	if len(orderedLayers) == 0 {
		return nil
	}
	layersOut := make([]LayerDoc, 0, len(orderedLayers))
	for _, name := range orderedLayers {
		l := *survivingLayers[name]
		l.Objects = buildChildren(parentRef{layer: name})
		layersOut = append(layersOut, l)
	}
	return layersOut
}

func formatLocation(layer, parent string) string {
	if parent == "" {
		return layer
	}
	return layer + "/" + parent
}

func parentLocationOrNil(idx map[string]indexedObj, id string) any {
	o, ok := idx[id]
	if !ok {
		return nil
	}
	return formatLocation(o.layer, o.ref.parent)
}

func (m *sceneMerger) mergeLayerMetaFields(st *sideTracker, ctx conflictCtx, b, o, t *LayerDoc) *LayerDoc {
	return &LayerDoc{
		Name:         o.Name,
		Space:        mergeScalar(m, st, ctx, "space", b.Space, o.Space, t.Space),
		Order:        mergeScalar(m, st, ctx, "order", b.Order, o.Order, t.Order),
		Visible:      mergePtr(m, st, ctx, "visible", b.Visible, o.Visible, t.Visible),
		ParallaxX:    mergePtr(m, st, ctx, "parallaxX", b.ParallaxX, o.ParallaxX, t.ParallaxX),
		ParallaxY:    mergePtr(m, st, ctx, "parallaxY", b.ParallaxY, o.ParallaxY, t.ParallaxY),
		RepeatX:      mergeScalar(m, st, ctx, "repeatX", b.RepeatX, o.RepeatX, t.RepeatX),
		RepeatY:      mergeScalar(m, st, ctx, "repeatY", b.RepeatY, o.RepeatY, t.RepeatY),
		RepeatWidth:  mergeScalar(m, st, ctx, "repeatWidth", b.RepeatWidth, o.RepeatWidth, t.RepeatWidth),
		RepeatHeight: mergeScalar(m, st, ctx, "repeatHeight", b.RepeatHeight, o.RepeatHeight, t.RepeatHeight),
		Tilemap:      m.mergeTilemap(st, ctx, "tilemap", b.Tilemap, o.Tilemap, t.Tilemap),
	}
}

func (m *sceneMerger) mergeObjectFields(st *sideTracker, ctx conflictCtx, b, o, t *ObjectDoc) *ObjectDoc {
	return &ObjectDoc{
		ID:              o.ID,
		Name:            mergeScalar(m, st, ctx, "name", b.Name, o.Name, t.Name),
		Prefab:          mergeScalar(m, st, ctx, "prefab", b.Prefab, o.Prefab, t.Prefab),
		X:               mergeScalar(m, st, ctx, "x", b.X, o.X, t.X),
		Y:               mergeScalar(m, st, ctx, "y", b.Y, o.Y, t.Y),
		PosZ:            mergeScalar(m, st, ctx, "posZ", b.PosZ, o.PosZ, t.PosZ),
		OriginX:         mergeScalar(m, st, ctx, "originX", b.OriginX, o.OriginX, t.OriginX),
		OriginY:         mergeScalar(m, st, ctx, "originY", b.OriginY, o.OriginY, t.OriginY),
		Rotation:        mergeScalar(m, st, ctx, "rotation", b.Rotation, o.Rotation, t.Rotation),
		RotX:            mergeScalar(m, st, ctx, "rotX", b.RotX, o.RotX, t.RotX),
		RotY:            mergeScalar(m, st, ctx, "rotY", b.RotY, o.RotY, t.RotY),
		RotZ:            mergeScalar(m, st, ctx, "rotZ", b.RotZ, o.RotZ, t.RotZ),
		ScaleX:          mergePtr(m, st, ctx, "scaleX", b.ScaleX, o.ScaleX, t.ScaleX),
		ScaleY:          mergePtr(m, st, ctx, "scaleY", b.ScaleY, o.ScaleY, t.ScaleY),
		ScaleZ:          mergePtr(m, st, ctx, "scaleZ", b.ScaleZ, o.ScaleZ, t.ScaleZ),
		Depth:           mergeScalar(m, st, ctx, "depth", b.Depth, o.Depth, t.Depth),
		Mesh:            mergeScalar(m, st, ctx, "mesh", b.Mesh, o.Mesh, t.Mesh),
		Model:           mergeScalar(m, st, ctx, "model", b.Model, o.Model, t.Model),
		ModelNode:       mergeScalar(m, st, ctx, "modelNode", b.ModelNode, o.ModelNode, t.ModelNode),
		ModelMesh:       mergeScalar(m, st, ctx, "modelMesh", b.ModelMesh, o.ModelMesh, t.ModelMesh),
		GridMap:         mergeScalar(m, st, ctx, "gridMap", b.GridMap, o.GridMap, t.GridMap),
		Occluder:        m.mergeOccluder(st, ctx, "occluder", b.Occluder, o.Occluder, t.Occluder),
		FlipX:           mergeScalar(m, st, ctx, "flipX", b.FlipX, o.FlipX, t.FlipX),
		FlipY:           mergeScalar(m, st, ctx, "flipY", b.FlipY, o.FlipY, t.FlipY),
		Alpha:           mergePtr(m, st, ctx, "alpha", b.Alpha, o.Alpha, t.Alpha),
		Z:               mergeScalar(m, st, ctx, "z", b.Z, o.Z, t.Z),
		Sprite:          m.mergeSprite(st, ctx, "sprite", b.Sprite, o.Sprite, t.Sprite),
		Material:        mergeScalar(m, st, ctx, "material", b.Material, o.Material, t.Material),
		Particles:       m.mergeParticles(st, ctx, "particles", b.Particles, o.Particles, t.Particles),
		Light:           m.mergeLight(st, ctx, "light", b.Light, o.Light, t.Light),
		Camera:          m.mergeCamera3D(st, ctx, "camera", b.Camera, o.Camera, t.Camera),
		ReflectionProbe: m.mergeReflectionProbe(st, ctx, "reflectionProbe", b.ReflectionProbe, o.ReflectionProbe, t.ReflectionProbe),
		LightProbeGrid:  m.mergeLightProbeGrid(st, ctx, "lightProbeGrid", b.LightProbeGrid, o.LightProbeGrid, t.LightProbeGrid),
		Components:      m.mergeAnyMap(st, ctx, "components", b.Components, o.Components, t.Components),
		Properties:      m.mergeAnyMap(st, ctx, "properties", b.Properties, o.Properties, t.Properties),
		Scene:           mergeScalar(m, st, ctx, "scene", b.Scene, o.Scene, t.Scene),
		Overrides:       m.mergeObjectOverrides(st, ctx, "overrides", b.Overrides, o.Overrides, t.Overrides),
	}
}

type connKey struct {
	from    string
	event   string
	to      string
	handler string
	occ     int
}

func (k connKey) String() string {
	s := fmt.Sprintf("%s.%s -> %s.%s", k.from, k.event, k.to, k.handler)
	if k.occ > 0 {
		return fmt.Sprintf("%s#%d", s, k.occ+1)
	}
	return s
}

func indexConnections(conns []ConnectionDoc) (map[connKey]ConnectionDoc, []connKey) {
	byKey := make(map[connKey]ConnectionDoc, len(conns))
	order := make([]connKey, 0, len(conns))
	seen := make(map[connKey]int, len(conns))
	for _, c := range conns {
		baseKey := connKey{from: c.From, event: c.Event, to: c.To, handler: c.Handler}
		occ := seen[baseKey]
		seen[baseKey] = occ + 1
		k := connKey{from: c.From, event: c.Event, to: c.To, handler: c.Handler, occ: occ}
		byKey[k] = c
		order = append(order, k)
	}
	return byKey, order
}

func (m *sceneMerger) mergeConnections() []ConnectionDoc {
	baseMap, baseOrder := indexConnections(m.base.Connections)
	oursMap, oursOrder := indexConnections(m.ours.Connections)
	theirsMap, theirsOrder := indexConnections(m.theirs.Connections)

	keys := unionConnKeys(baseMap, oursMap, theirsMap)
	if len(keys) == 0 {
		return nil
	}

	surviving := make(map[connKey]ConnectionDoc)
	for _, k := range keys {
		b, hasB := baseMap[k]
		o, hasO := oursMap[k]
		t, hasT := theirsMap[k]
		label := k.String()
		ctx := conflictCtx{scope: "connection", target: label}

		switch {
		case !hasB && hasO && !hasT:
			surviving[k] = cloneConnection(o)
			m.addMerged("connection", label, "ours", "added")

		case !hasB && !hasO && hasT:
			surviving[k] = cloneConnection(t)
			m.addMerged("connection", label, "theirs", "added")

		case !hasB && hasO && hasT:
			if connectionEqual(o, t) {
				surviving[k] = cloneConnection(o)
				m.addMerged("connection", label, "both", "added")
			} else {
				m.addConflict(MergeConflict{
					Kind:   ConflictAddAdd,
					Scope:  "connection",
					Target: label,
					Base:   nil,
					Ours:   toYAMLValue(o),
					Theirs: toYAMLValue(t),
				})
				surviving[k] = cloneConnection(o)
			}

		case hasB && !hasO && !hasT:
			// Deleted on both sides.

		case hasB && !hasO && hasT:
			if connectionEqual(b, t) {
				m.addMerged("connection", label, "ours", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "connection",
					Target:  label,
					Message: "deleted on ours, modified on theirs",
					Base:    toYAMLValue(b),
					Ours:    nil,
					Theirs:  toYAMLValue(t),
				})
			}

		case hasB && hasO && !hasT:
			if connectionEqual(b, o) {
				m.addMerged("connection", label, "theirs", "deleted")
			} else {
				m.addConflict(MergeConflict{
					Kind:    ConflictDeleteModify,
					Scope:   "connection",
					Target:  label,
					Message: "modified on ours, deleted on theirs",
					Base:    toYAMLValue(b),
					Ours:    toYAMLValue(o),
					Theirs:  nil,
				})
				surviving[k] = cloneConnection(o)
			}

		case hasB && hasO && hasT:
			st := sideTracker{}
			merged := ConnectionDoc{
				From:    o.From,
				Event:   o.Event,
				To:      o.To,
				Handler: o.Handler,
				Args:    m.mergeAnyMap(&st, ctx, "args", b.Args, o.Args, t.Args),
				Once:    mergeScalar(m, &st, ctx, "once", b.Once, o.Once, t.Once),
			}
			surviving[k] = merged
			if side := st.side(); side != "" {
				m.addMerged("connection", label, side, "changed")
			}
		}
	}

	if len(surviving) == 0 {
		return nil
	}
	ordered := mergeConnOrder(surviving, baseOrder, oursOrder, theirsOrder)
	out := make([]ConnectionDoc, 0, len(ordered))
	for _, k := range ordered {
		out = append(out, surviving[k])
	}
	return out
}

// mergeOrder deterministically orders surviving string IDs using base, ours and
// theirs orders: if ours kept base's relative order for common items while
// theirs reordered them, theirs's order leads; otherwise ours's order leads,
// with theirs-only additions inserted after their nearest surviving predecessor
// in theirs (or at the end).
func mergeOrder(surviving, baseOrder, oursOrder, theirsOrder []string) []string {
	keep := make(map[string]bool, len(surviving))
	for _, id := range surviving {
		keep[id] = true
	}
	filter := func(seq []string) []string {
		var res []string
		for _, id := range seq {
			if keep[id] {
				res = append(res, id)
			}
		}
		return res
	}
	b := filter(baseOrder)
	o := filter(oursOrder)
	t := filter(theirsOrder)

	// Check whether only theirs reordered the surviving base elements.
	primary, secondary := o, t
	if len(b) > 1 {
		bSet := make(map[string]bool, len(b))
		for _, id := range b {
			bSet[id] = true
		}
		oBase := filterSet(o, bSet)
		tBase := filterSet(t, bSet)
		if slices.Equal(oBase, b) && !slices.Equal(tBase, b) {
			primary, secondary = t, o
		}
	}

	result := slices.Clone(primary)
	inResult := make(map[string]bool, len(result))
	for _, id := range result {
		inResult[id] = true
	}

	// Insert remaining items from secondary in secondary's order, placing each
	// right after its nearest preceding item from secondary already in result.
	for i, id := range secondary {
		if inResult[id] {
			continue
		}
		insertAt := len(result)
		for j := i - 1; j >= 0; j-- {
			if pos := slices.Index(result, secondary[j]); pos >= 0 {
				insertAt = pos + 1
				break
			}
		}
		result = slices.Insert(result, insertAt, id)
		inResult[id] = true
	}

	// Append any remaining surviving IDs (e.g. moved in from elsewhere) in sorted order.
	var rest []string
	for _, id := range surviving {
		if !inResult[id] {
			rest = append(rest, id)
		}
	}
	slices.Sort(rest)
	return append(result, rest...)
}

func filterSet(seq []string, set map[string]bool) []string {
	var out []string
	for _, id := range seq {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

func mergeConnOrder(surviving map[connKey]ConnectionDoc, baseOrder, oursOrder, theirsOrder []connKey) []connKey {
	toStr := func(ks []connKey) []string {
		out := make([]string, len(ks))
		for i, k := range ks {
			out[i] = k.String()
		}
		return out
	}
	byStr := make(map[string]connKey, len(surviving))
	survStr := make([]string, 0, len(surviving))
	for k := range surviving {
		s := k.String()
		byStr[s] = k
		survStr = append(survStr, s)
	}
	orderedStr := mergeOrder(survStr, toStr(baseOrder), toStr(oursOrder), toStr(theirsOrder))
	out := make([]connKey, 0, len(orderedStr))
	for _, s := range orderedStr {
		out = append(out, byStr[s])
	}
	return out
}

func unionKeys[V any](a, b, c map[string]V) []string {
	set := make(map[string]struct{}, len(a)+len(b)+len(c))
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	for k := range c {
		set[k] = struct{}{}
	}
	return slices.Sorted(maps.Keys(set))
}

func unionConnKeys(a, b, c map[connKey]ConnectionDoc) []connKey {
	set := make(map[connKey]struct{}, len(a)+len(b)+len(c))
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	for k := range c {
		set[k] = struct{}{}
	}
	keys := slices.Collect(maps.Keys(set))
	slices.SortFunc(keys, func(x, y connKey) int {
		return strings.Compare(x.String(), y.String())
	})
	return keys
}

// --- Equality & cloning helpers ---

func cameraEqual(a, b *CameraDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.X == b.X && a.Y == b.Y && a.Rotation == b.Rotation &&
		ptrEqual(a.Zoom, b.Zoom) && boundsEqual(a.Bounds, b.Bounds)
}

func boundsEqual(a, b *BoundsDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func environmentEqual(a, b *EnvironmentDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return skyEqual(a.Sky, b.Sky) &&
		a.AmbientColor == b.AmbientColor &&
		ptrEqual(a.AmbientIntensity, b.AmbientIntensity) &&
		ptrEqual(a.IBLIntensity, b.IBLIntensity) &&
		fogEqual(a.Fog, b.Fog) &&
		a.Tonemapping == b.Tonemapping &&
		ptrEqual(a.Exposure, b.Exposure)
}

func skyEqual(a, b *SkyDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func fogEqual(a, b *FogDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Mode == b.Mode &&
		a.Color == b.Color &&
		a.Start == b.Start &&
		a.End == b.End &&
		a.Density == b.Density &&
		ptrEqual(a.MaxOpacity, b.MaxOpacity) &&
		a.HeightEnabled == b.HeightEnabled &&
		a.HeightBase == b.HeightBase &&
		a.HeightFalloff == b.HeightFalloff &&
		a.HeightDensity == b.HeightDensity
}

func lightEqual(a, b *LightDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Kind == b.Kind &&
		a.Color == b.Color &&
		ptrEqual(a.Intensity, b.Intensity) &&
		a.Range == b.Range &&
		a.InnerCone == b.InnerCone &&
		ptrEqual(a.OuterCone, b.OuterCone) &&
		a.Shadows == b.Shadows
}

func camera3DEqual(a, b *Camera3DDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Projection == b.Projection &&
		ptrEqual(a.FOV, b.FOV) &&
		ptrEqual(a.OrthoHeight, b.OrthoHeight) &&
		ptrEqual(a.Near, b.Near) &&
		ptrEqual(a.Far, b.Far) &&
		a.Current == b.Current
}

func reflectionProbeEqual(a, b *ReflectionProbeDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return ptrEqual(a.Center, b.Center) &&
		ptrEqual(a.Size, b.Size) &&
		a.BlendDistance == b.BlendDistance &&
		ptrEqual(a.Intensity, b.Intensity) &&
		a.Priority == b.Priority &&
		ptrEqual(a.BoxProjection, b.BoxProjection) &&
		a.Resolution == b.Resolution &&
		a.Cubemap == b.Cubemap
}

func lightProbeGridEqual(a, b *LightProbeGridDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return ptrEqual(a.Center, b.Center) &&
		ptrEqual(a.Size, b.Size) &&
		ptrEqual(a.Counts, b.Counts) &&
		ptrEqual(a.Intensity, b.Intensity) &&
		a.Data == b.Data
}

func tilemapEqual(a, b *TilemapDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Ref == b.Ref && a.Source == b.Source && a.Level == b.Level &&
		a.OffsetX == b.OffsetX && a.OffsetY == b.OffsetY &&
		a.TileWidth == b.TileWidth && a.TileHeight == b.TileHeight &&
		a.Width == b.Width && a.Height == b.Height &&
		a.Tileset == b.Tileset && a.TilesetFile == b.TilesetFile &&
		slices.Equal(a.Data, b.Data) && tileLayersEqual(a.TileLayers, b.TileLayers)
}

func tileLayersEqual(a, b []TileLayerDoc) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name ||
			!ptrEqual(a[i].Visible, b[i].Visible) ||
			!ptrEqual(a[i].Opacity, b[i].Opacity) ||
			!slices.Equal(a[i].Rows, b[i].Rows) {
			return false
		}
	}
	return true
}

func cloneTileLayers(src []TileLayerDoc) []TileLayerDoc {
	if len(src) == 0 {
		return nil
	}
	out := make([]TileLayerDoc, len(src))
	for i, l := range src {
		out[i] = TileLayerDoc{
			Name:    l.Name,
			Visible: clonePtr(l.Visible),
			Opacity: clonePtr(l.Opacity),
			Rows:    slices.Clone(l.Rows),
		}
	}
	return out
}

func spriteEqual(a, b *SpriteRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func particlesEqual(a, b *ParticlesRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func layerMetaEqual(a, b *LayerDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Name == b.Name && a.Space == b.Space && a.Order == b.Order &&
		ptrEqual(a.Visible, b.Visible) &&
		ptrEqual(a.ParallaxX, b.ParallaxX) && ptrEqual(a.ParallaxY, b.ParallaxY) &&
		a.RepeatX == b.RepeatX && a.RepeatY == b.RepeatY &&
		a.RepeatWidth == b.RepeatWidth && a.RepeatHeight == b.RepeatHeight &&
		tilemapEqual(a.Tilemap, b.Tilemap)
}

func layerOverrideEqual(a, b LayerOverride) bool {
	return a.Space == b.Space && ptrEqual(a.Order, b.Order) &&
		ptrEqual(a.Visible, b.Visible) &&
		ptrEqual(a.ParallaxX, b.ParallaxX) && ptrEqual(a.ParallaxY, b.ParallaxY) &&
		ptrEqual(a.RepeatX, b.RepeatX) && ptrEqual(a.RepeatY, b.RepeatY) &&
		ptrEqual(a.RepeatWidth, b.RepeatWidth) && ptrEqual(a.RepeatHeight, b.RepeatHeight) &&
		tilemapEqual(a.Tilemap, b.Tilemap)
}

func objectOverrideEqual(a, b ObjectOverride) bool {
	return ptrEqual(a.Name, b.Name) &&
		ptrEqual(a.Prefab, b.Prefab) &&
		ptrEqual(a.X, b.X) &&
		ptrEqual(a.Y, b.Y) &&
		ptrEqual(a.PosZ, b.PosZ) &&
		ptrEqual(a.OriginX, b.OriginX) &&
		ptrEqual(a.OriginY, b.OriginY) &&
		ptrEqual(a.Rotation, b.Rotation) &&
		ptrEqual(a.RotX, b.RotX) &&
		ptrEqual(a.RotY, b.RotY) &&
		ptrEqual(a.RotZ, b.RotZ) &&
		ptrEqual(a.ScaleX, b.ScaleX) &&
		ptrEqual(a.ScaleY, b.ScaleY) &&
		ptrEqual(a.ScaleZ, b.ScaleZ) &&
		ptrEqual(a.Depth, b.Depth) &&
		ptrEqual(a.Mesh, b.Mesh) &&
		ptrEqual(a.Model, b.Model) &&
		ptrEqual(a.ModelNode, b.ModelNode) &&
		ptrEqual(a.ModelMesh, b.ModelMesh) &&
		ptrEqual(a.GridMap, b.GridMap) &&
		occluderEqual(a.Occluder, b.Occluder) &&
		ptrEqual(a.FlipX, b.FlipX) &&
		ptrEqual(a.FlipY, b.FlipY) &&
		ptrEqual(a.Alpha, b.Alpha) &&
		ptrEqual(a.Z, b.Z) &&
		spriteEqual(a.Sprite, b.Sprite) &&
		ptrEqual(a.Material, b.Material) &&
		particlesEqual(a.Particles, b.Particles) &&
		lightEqual(a.Light, b.Light) &&
		camera3DEqual(a.Camera, b.Camera) &&
		reflectionProbeEqual(a.ReflectionProbe, b.ReflectionProbe) &&
		lightProbeGridEqual(a.LightProbeGrid, b.LightProbeGrid) &&
		anyMapEqual(a.Components, b.Components) &&
		anyMapEqual(a.Properties, b.Properties)
}

func objectOwnEqual(a, b *ObjectDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID &&
		a.Name == b.Name &&
		a.Prefab == b.Prefab &&
		a.X == b.X &&
		a.Y == b.Y &&
		a.PosZ == b.PosZ &&
		a.OriginX == b.OriginX &&
		a.OriginY == b.OriginY &&
		a.Rotation == b.Rotation &&
		a.RotX == b.RotX &&
		a.RotY == b.RotY &&
		a.RotZ == b.RotZ &&
		ptrEqual(a.ScaleX, b.ScaleX) &&
		ptrEqual(a.ScaleY, b.ScaleY) &&
		ptrEqual(a.ScaleZ, b.ScaleZ) &&
		a.Depth == b.Depth &&
		a.Mesh == b.Mesh &&
		a.Model == b.Model &&
		a.ModelNode == b.ModelNode &&
		a.ModelMesh == b.ModelMesh &&
		a.GridMap == b.GridMap &&
		occluderEqual(a.Occluder, b.Occluder) &&
		a.FlipX == b.FlipX &&
		a.FlipY == b.FlipY &&
		ptrEqual(a.Alpha, b.Alpha) &&
		a.Z == b.Z &&
		spriteEqual(a.Sprite, b.Sprite) &&
		a.Material == b.Material &&
		particlesEqual(a.Particles, b.Particles) &&
		lightEqual(a.Light, b.Light) &&
		camera3DEqual(a.Camera, b.Camera) &&
		reflectionProbeEqual(a.ReflectionProbe, b.ReflectionProbe) &&
		lightProbeGridEqual(a.LightProbeGrid, b.LightProbeGrid) &&
		anyMapEqual(a.Components, b.Components) &&
		anyMapEqual(a.Properties, b.Properties) &&
		a.Scene == b.Scene &&
		objectOverrideMapEqual(a.Overrides, b.Overrides)
}

func objectOverrideMapEqual(a, b map[string]ObjectOverride) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !objectOverrideEqual(va, vb) {
			return false
		}
	}
	return true
}

func connectionEqual(a, b ConnectionDoc) bool {
	return a.From == b.From && a.Event == b.Event && a.To == b.To &&
		a.Handler == b.Handler && a.Once == b.Once && anyMapEqual(a.Args, b.Args)
}

func anyMapEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !anyEqual(va, vb) {
			return false
		}
	}
	return true
}

func anyEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func cloneBounds(b *BoundsDoc) *BoundsDoc {
	return clonePtr(b)
}

func cloneSprite(s *SpriteRef) *SpriteRef {
	return clonePtr(s)
}

func occluderEqual(a, b *OccluderDoc) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Mode == b.Mode &&
		ptrEqual(a.Center, b.Center) &&
		ptrEqual(a.Size, b.Size) &&
		a.DoubleSided == b.DoubleSided
}

func cloneParticles(p *ParticlesRef) *ParticlesRef {
	return clonePtr(p)
}
func cloneLayerMeta(l *LayerDoc) *LayerDoc {
	if l == nil {
		return nil
	}
	return &LayerDoc{
		Name:         l.Name,
		Space:        l.Space,
		Order:        l.Order,
		Visible:      clonePtr(l.Visible),
		ParallaxX:    clonePtr(l.ParallaxX),
		ParallaxY:    clonePtr(l.ParallaxY),
		RepeatX:      l.RepeatX,
		RepeatY:      l.RepeatY,
		RepeatWidth:  l.RepeatWidth,
		RepeatHeight: l.RepeatHeight,
		Tilemap:      cloneTilemap(l.Tilemap),
	}
}

func cloneLayerOverride(o LayerOverride) LayerOverride {
	return LayerOverride{
		Space:        o.Space,
		Order:        clonePtr(o.Order),
		Visible:      clonePtr(o.Visible),
		ParallaxX:    clonePtr(o.ParallaxX),
		ParallaxY:    clonePtr(o.ParallaxY),
		RepeatX:      clonePtr(o.RepeatX),
		RepeatY:      clonePtr(o.RepeatY),
		RepeatWidth:  clonePtr(o.RepeatWidth),
		RepeatHeight: clonePtr(o.RepeatHeight),
		Tilemap:      cloneTilemap(o.Tilemap),
	}
}

func cloneObjectOverride(o ObjectOverride) ObjectOverride {
	return ObjectOverride{
		Name:            clonePtr(o.Name),
		Prefab:          clonePtr(o.Prefab),
		X:               clonePtr(o.X),
		Y:               clonePtr(o.Y),
		PosZ:            clonePtr(o.PosZ),
		OriginX:         clonePtr(o.OriginX),
		OriginY:         clonePtr(o.OriginY),
		Rotation:        clonePtr(o.Rotation),
		RotX:            clonePtr(o.RotX),
		RotY:            clonePtr(o.RotY),
		RotZ:            clonePtr(o.RotZ),
		ScaleX:          clonePtr(o.ScaleX),
		ScaleY:          clonePtr(o.ScaleY),
		ScaleZ:          clonePtr(o.ScaleZ),
		Depth:           clonePtr(o.Depth),
		Mesh:            clonePtr(o.Mesh),
		Model:           clonePtr(o.Model),
		ModelNode:       clonePtr(o.ModelNode),
		ModelMesh:       clonePtr(o.ModelMesh),
		GridMap:         clonePtr(o.GridMap),
		Occluder:        cloneOccluder(o.Occluder),
		FlipX:           clonePtr(o.FlipX),
		FlipY:           clonePtr(o.FlipY),
		Alpha:           clonePtr(o.Alpha),
		Z:               clonePtr(o.Z),
		Sprite:          cloneSprite(o.Sprite),
		Material:        clonePtr(o.Material),
		Particles:       cloneParticles(o.Particles),
		Light:           o.Light.Clone(),
		Camera:          o.Camera.Clone(),
		ReflectionProbe: o.ReflectionProbe.Clone(),
		LightProbeGrid:  o.LightProbeGrid.Clone(),
		Components:      cloneAnyMap(o.Components),
		Properties:      cloneAnyMap(o.Properties),
	}
}

func cloneObjectOwn(o *ObjectDoc) *ObjectDoc {
	if o == nil {
		return nil
	}
	cp := *o
	cp.ScaleX = clonePtr(o.ScaleX)
	cp.ScaleY = clonePtr(o.ScaleY)
	cp.ScaleZ = clonePtr(o.ScaleZ)
	cp.Alpha = clonePtr(o.Alpha)
	cp.Sprite = cloneSprite(o.Sprite)
	cp.Occluder = cloneOccluder(o.Occluder)
	cp.Particles = cloneParticles(o.Particles)
	cp.Light = o.Light.Clone()
	cp.Camera = o.Camera.Clone()
	cp.ReflectionProbe = o.ReflectionProbe.Clone()
	cp.LightProbeGrid = o.LightProbeGrid.Clone()
	cp.Components = cloneAnyMap(o.Components)
	cp.Properties = cloneAnyMap(o.Properties)
	if len(o.Overrides) > 0 {
		cp.Overrides = make(map[string]ObjectOverride, len(o.Overrides))
		for k, ov := range o.Overrides {
			cp.Overrides[k] = cloneObjectOverride(ov)
		}
	} else {
		cp.Overrides = nil
	}
	cp.Children = nil
	return &cp
}

func cloneConnection(c ConnectionDoc) ConnectionDoc {
	c.Args = cloneAnyMap(c.Args)
	return c
}

// toYAMLValue converts a Go value (including SceneDoc sub-structs and typed
// pointers) into plain YAML-native values (nil, bool, int, float64, string,
// []any, map[string]any) so ConflictDoc round-trips deterministically through
// ParseConflict and EncodeConflict.
func toYAMLValue(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return nil
	}
	switch x := v.(type) {
	case string, bool, int, int64, uint64, float64:
		return x
	case Space:
		return string(x)
	case LightKind:
		return string(x)
	case ProjectionKind:
		return string(x)
	case SkyKind:
		return string(x)
	case FogMode:
		return string(x)
	case TonemapMode:
		return string(x)
	case float32:
		// Marshal via YAML so float32 formatting matches sceneformat.Encode (no float64 noise like 0.30000001192092896).
		raw, err := yaml.Marshal(x)
		if err != nil {
			return float64(x)
		}
		var out any
		if err := yaml.Unmarshal(raw, &out); err != nil {
			return float64(x)
		}
		return out
	}
	raw, err := yaml.Marshal(floatValue(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	var out any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		return fmt.Sprint(v)
	}
	return out
}
