package animclip

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// BindDoc returns a Binder that writes into the objects of doc. A track's target is an object ID anywhere in the scene, and an empty target is self, the object the clip plays on. Binding fails for an object the scene does not have.
func BindDoc(doc *sceneformat.SceneDoc, self string) Binder {
	return func(target string, p Property) (func(float64), error) {
		id := targetID(target, self)
		o := findObject(doc, id)
		if o == nil {
			return nil, fmt.Errorf("scene has no object %q", id)
		}
		return func(v float64) { p.Set(o, v) }, nil
	}
}

// BindScene returns a Binder that writes into the objects of a built scene, as BindDoc does, and copies each written transform, flip and alpha onto the object's sprite so the next Draw shows it; alpha is multiplied by the object's material opacity as Build does.
func BindScene(scene *sceneformat.BuiltScene, self string) Binder {
	return func(target string, p Property) (func(float64), error) {
		id := targetID(target, self)
		bo := scene.Object(id)
		if bo == nil || bo.Doc == nil {
			return nil, fmt.Errorf("scene has no object %q", id)
		}
		return func(v float64) {
			p.Set(bo.Doc, v)
			syncSprite(bo, p)
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
	for li := range doc.Layers {
		objs := doc.Layers[li].Objects
		for oi := range objs {
			if objs[oi].ID == id {
				return &objs[oi]
			}
		}
	}
	return nil
}

// syncSprite copies the object field p wrote from the object's document onto its sprite.
func syncSprite(bo *sceneformat.BuiltObject, p Property) {
	sp, o := bo.Sprite, bo.Doc
	if sp == nil || p.IsComponent() {
		return
	}
	switch p.Name {
	case "x":
		sp.X = o.X
	case "y":
		sp.Y = o.Y
	case "rotation":
		sp.Rotation = o.Rotation
	case "scaleX":
		sp.ScaleX = o.EffectiveScaleX()
	case "scaleY":
		sp.ScaleY = o.EffectiveScaleY()
	case "alpha":
		a := o.EffectiveAlpha()
		if bo.Material != nil {
			a *= bo.Material.EffectiveOpacity()
		}
		sp.Alpha = a
	case "originX":
		sp.OriginX = o.OriginX
	case "originY":
		sp.OriginY = o.OriginY
	case "flipX":
		sp.FlipX = o.FlipX
	case "flipY":
		sp.FlipY = o.FlipY
	}
}
