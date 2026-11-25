package animclip

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// BindDoc returns a Binder that writes into the objects of doc. A track's target is an object ID anywhere in the scene, and an empty target is self, the object the clip plays on. Binding fails for an object the scene does not have, or for a sprite.frame track on an object without a sprite.
func BindDoc(doc *sceneformat.SceneDoc, self string) Binder {
	return func(target string, p Property) (func(float64), error) {
		id := targetID(target, self)
		o := findObject(doc, id)
		if o == nil {
			return nil, fmt.Errorf("scene has no object %q", id)
		}
		if p.Kind == KindFrame {
			if o.Sprite == nil {
				return nil, fmt.Errorf("object %q has no sprite", id)
			}
			return func(v float64) { p.SetFrame(o, p.Frame(v)) }, nil
		}
		return func(v float64) { p.Set(o, v) }, nil
	}
}

// BindScene returns a Binder that writes into the objects of a built scene, as BindDoc does, and syncs each object whose field it writes (sceneformat.BuiltObject.Sync), so the next Draw shows the object and its children at their new world transform, sprite frame and alpha, alpha multiplied by the material's opacity as Build does. For a sprite.frame track, binding checks that the object has a sprite and that its atlas holds every keyed frame name.
func BindScene(scene *sceneformat.BuiltScene, self string) Binder {
	return func(target string, p Property) (func(float64), error) {
		id := targetID(target, self)
		bo := scene.Object(id)
		if bo == nil || bo.Doc == nil {
			return nil, fmt.Errorf("scene has no object %q", id)
		}
		if p.Kind == KindFrame {
			if bo.Doc.Sprite == nil || bo.Sprite == nil || bo.Atlas == nil {
				return nil, fmt.Errorf("object %q has no sprite", id)
			}
			frames := make([]sprite.Frame, len(p.Frames))
			for i, name := range p.Frames {
				fr, ok := bo.Atlas.Frames[name]
				if !ok {
					return nil, fmt.Errorf("object %q atlas %q has no frame %q", id, bo.Doc.Sprite.Atlas, name)
				}
				frames[i] = fr
			}
			return func(v float64) {
				idx := int(v)
				if idx < 0 || idx >= len(frames) {
					return
				}
				bo.Doc.Sprite.Frame = frames[idx].Name
				bo.Sprite.Frame = frames[idx]
			}, nil
		}
		return func(v float64) {
			p.Set(bo.Doc, v)
			if !p.IsComponent() {
				bo.Sync()
			}
		}, nil
	}
}

func targetID(target, self string) string {
	if target == "" {
		return self
	}
	return target
}

func findObject(doc *sceneformat.SceneDoc, id string) *sceneformat.ObjectDoc {
	if doc == nil || id == "" {
		return nil
	}
	var found *sceneformat.ObjectDoc
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.ID == id {
				found = o
			}
			return found == nil
		})
		if found != nil {
			return found
		}
	}
	return nil
}
