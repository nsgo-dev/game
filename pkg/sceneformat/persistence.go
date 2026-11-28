package sceneformat

import (
	"maps"
	"slices"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/save"
)

// ApplyPersistence returns a copy of doc with persisted object state for scenePath applied from p.
// Objects marked Destroyed in p are omitted along with their descendants and any connections
// referencing them; moved objects receive their persisted X, Y, PosZ and Rotation overrides; and
// persisted component field overrides are merged into each object's Components map.
//
// The input doc is never mutated, so a game can keep one parsed SceneDoc and rebuild it on every
// scene re-entry with current Persistence state.
func ApplyPersistence(doc *SceneDoc, p *save.Persistence, scenePath string) *SceneDoc {
	if doc == nil {
		return nil
	}
	out := doc.Clone()
	if p == nil || scenePath == "" {
		return out
	}
	states := p.Scene(scenePath)
	if len(states) == 0 {
		return out
	}
	destroyed := make(map[string]struct{})
	for li := range out.Layers {
		out.Layers[li].Objects = applyPersistenceObjects(out.Layers[li].Objects, states, destroyed)
	}
	if len(destroyed) > 0 && len(out.Connections) > 0 {
		out.Connections = slices.DeleteFunc(out.Connections, func(c ConnectionDoc) bool {
			_, fromDead := destroyed[c.From]
			_, toDead := destroyed[c.To]
			return fromDead || toDead
		})
	}
	return out
}

func applyPersistenceObjects(objs []ObjectDoc, states map[string]save.ObjectState, destroyed map[string]struct{}) []ObjectDoc {
	if len(objs) == 0 {
		return objs
	}
	kept := make([]ObjectDoc, 0, len(objs))
	for _, o := range objs {
		st, ok := states[o.ID]
		if ok && st.Destroyed {
			markSubtreeDestroyed(&o, destroyed)
			continue
		}
		if ok {
			if st.X != nil {
				o.X = *st.X
			}
			if st.Y != nil {
				o.Y = *st.Y
			}
			if st.PosZ != nil {
				o.PosZ = *st.PosZ
			}
			if st.Rotation != nil {
				o.Rotation = *st.Rotation
			}
			if len(st.Components) > 0 {
				if o.Components == nil {
					o.Components = make(map[string]any, len(st.Components))
				}
				for comp, fields := range st.Components {
					if len(fields) == 0 {
						continue
					}
					existing, _ := o.Components[comp].(map[string]any)
					merged := make(map[string]any, len(existing)+len(fields))
					maps.Copy(merged, existing)
					maps.Copy(merged, fields)
					o.Components[comp] = merged
				}
			}
		}
		if len(o.Children) > 0 {
			o.Children = applyPersistenceObjects(o.Children, states, destroyed)
		}
		kept = append(kept, o)
	}
	return kept
}

func markSubtreeDestroyed(o *ObjectDoc, destroyed map[string]struct{}) {
	destroyed[o.ID] = struct{}{}
	for i := range o.Children {
		markSubtreeDestroyed(&o.Children[i], destroyed)
	}
}

// BuildWithPersistence resolves doc (when it instances or inherits other scenes), applies any
// persisted object state for scenePath from p, and builds the resulting BuiltScene.
func BuildWithPersistence(doc *SceneDoc, resolver AssetResolver, p *save.Persistence, scenePath string) (*BuiltScene, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	if doc.RefersToScenes() {
		scenes, _ := resolver.(SceneResolver)
		resolved, err := Resolve(doc, scenes)
		if err != nil {
			return nil, err
		}
		doc = resolved
	}
	if p != nil && scenePath != "" {
		doc = ApplyPersistence(doc, p, scenePath)
	}
	return Build(doc, resolver)
}

// CapturePersistence records into p the current destroyed and moved states of doc's objects from w and spawned (as returned by SpawnEntities / SpawnEntitiesWith) before leaving scenePath. Pass the scene as parsed from its file, not a copy returned by ApplyPersistence, since positions are compared against doc's values.
//
// Any object in doc whose entity is missing from spawned or no longer alive in w is marked destroyed; any live object whose Transform2D position or rotation differs from its SceneDoc values has its new position or rotation recorded, and one whose position or rotation matches doc again has that override cleared. Existing component overrides, switches and vars on live objects are preserved.
func CapturePersistence(doc *SceneDoc, w *entities.World, spawned map[string]entities.Entity, p *save.Persistence, scenePath string) {
	if doc == nil || w == nil || p == nil || scenePath == "" {
		return
	}
	for li := range doc.Layers {
		WalkObjects(doc.Layers[li].Objects, func(o, _ *ObjectDoc) bool {
			e, ok := spawned[o.ID]
			if !ok || !w.Alive(e) {
				p.MarkDestroyed(scenePath, o.ID)
				return true
			}
			if tr, ok := entities.Get[entities.Transform2D](w, e); ok {
				// Compare against the scene file's own values: an object returned to where the file places it drops the override recorded on an earlier visit instead of keeping the stale one.
				st, _ := p.Object(scenePath, o.ID)
				if tr.X != o.X || tr.Y != o.Y {
					x, y := tr.X, tr.Y
					st.X, st.Y = &x, &y
				} else {
					st.X, st.Y = nil, nil
				}
				if tr.Rotation != o.Rotation {
					rot := tr.Rotation
					st.Rotation = &rot
				} else {
					st.Rotation = nil
				}
				p.SetObject(scenePath, o.ID, st)
			}
			return true
		})
	}
}
