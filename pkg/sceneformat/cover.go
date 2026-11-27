package sceneformat

import (
	"fmt"
	"math"
	"strings"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
)

// CoverPointComponentName is the canonical scene YAML component key for authored cover points.
const CoverPointComponentName = "cover_point"

// CoverPointComponent returns the ComponentType schema for authoring ai.CoverPoint objects in scene YAML.
//
// Register it on a ComponentRegistry via RegisterCoverPoint so ParseWith validates cover_point fields
// and rejects unknown keys, wrong types, or out-of-range facing components.
func CoverPointComponent() ComponentType {
	return ComponentType{
		Name: CoverPointComponentName,
		Doc:  "Tactical cover point with facing direction, cover height (half or full), and optional tags.",
		Fields: []Field{
			{
				Name:    "height",
				Kind:    FieldString,
				Default: "full",
				Doc:     `Cover height: "half" or "full".`,
			},
			{
				Name:    "facingX",
				Kind:    FieldFloat,
				Default: 0.0,
				Min:     Float64(-1),
				Max:     Float64(1),
				Step:    0.1,
				Doc:     "X component of the cover facing direction (pointing toward the obstacle).",
			},
			{
				Name:    "facingY",
				Kind:    FieldFloat,
				Default: -1.0,
				Min:     Float64(-1),
				Max:     Float64(1),
				Step:    0.1,
				Doc:     "Y (or 3D Z) component of the cover facing direction (pointing toward the obstacle).",
			},
			{
				Name:    "tags",
				Kind:    FieldString,
				Default: "",
				Doc:     "Optional comma-separated tactical tags.",
			},
		},
	}
}

// RegisterCoverPoint registers the built-in "cover_point" component type on reg.
func RegisterCoverPoint(reg *ComponentRegistry) error {
	return reg.Register(CoverPointComponent())
}

// ExtractCoverPoints extracts all authored "cover_point" components from doc in deterministic
// document order (including nested child objects, composing parent 2D transforms).
//
// Each extracted CoverPoint receives a sequential 1-based ID, normalized world-space Facing
// (rotated by composed object rotation), validated CoverHeight ("half" or "full"), and parsed Tags.
func ExtractCoverPoints(doc *SceneDoc) ([]ai.CoverPoint, error) {
	if doc == nil {
		return nil, nil
	}
	reg := NewComponentRegistry()
	reg.MustRegister(CoverPointComponent())

	var out []ai.CoverPoint
	var nextID uint64 = 1

	var visit func(objs []ObjectDoc, parentAffine math2d.Affine, parentZ float32) error
	visit = func(objs []ObjectDoc, parentAffine math2d.Affine, parentZ float32) error {
		for i := range objs {
			o := &objs[i]
			local := objectLocalAffine(o)
			world := parentAffine.Mul(local)
			worldZ := parentZ + o.PosZ

			if raw, ok := o.Components[CoverPointComponentName]; ok {
				cp, err := decodeCoverPoint(reg, o.ID, raw, world, worldZ, nextID)
				if err != nil {
					return err
				}
				out = append(out, cp)
				nextID++
			}
			if len(o.Children) > 0 {
				if err := visit(o.Children, world, worldZ); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for li := range doc.Layers {
		if err := visit(doc.Layers[li].Objects, math2d.Identity(), 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ExtractBuiltCoverPoints extracts all authored "cover_point" components from a resolved BuiltScene.
func ExtractBuiltCoverPoints(scene *BuiltScene) ([]ai.CoverPoint, error) {
	if scene == nil || scene.Doc == nil {
		return nil, nil
	}
	return ExtractCoverPoints(scene.Doc)
}

func objectLocalAffine(o *ObjectDoc) math2d.Affine {
	sx := float32(1)
	if o.ScaleX != nil {
		sx = *o.ScaleX
	}
	sy := float32(1)
	if o.ScaleY != nil {
		sy = *o.ScaleY
	}
	return math2d.TRS(math2d.V2(o.X, o.Y), o.Rotation, math2d.V2(sx, sy))
}

func decodeCoverPoint(
	reg *ComponentRegistry,
	objectID string,
	raw any,
	world math2d.Affine,
	worldZ float32,
	id uint64,
) (ai.CoverPoint, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return ai.CoverPoint{}, &ComponentError{
			Object:    objectID,
			Component: CoverPointComponentName,
			Msg:       fmt.Sprintf("must be a mapping of fields, got %s", describeValue(raw)),
		}
	}
	resolved, err := reg.Resolve(CoverPointComponentName, m)
	if err != nil {
		if ce, ok := err.(*ComponentError); ok {
			ce.Object = objectID
		}
		return ai.CoverPoint{}, err
	}

	heightStr, _ := resolved["height"].(string)
	height, ok := ai.ParseCoverHeight(heightStr)
	if !ok {
		return ai.CoverPoint{}, &ComponentError{
			Object:    objectID,
			Component: CoverPointComponentName,
			Field:     "height",
			Msg:       fmt.Sprintf(`must be "half" or "full", got %q`, heightStr),
		}
	}

	fx, _ := resolved["facingX"].(float64)
	fy, _ := resolved["facingY"].(float64)
	facing := world.TransformVector(math2d.Vec2{X: float32(fx), Y: float32(fy)})
	if math.Hypot(float64(facing.X), float64(facing.Y)) <= 1e-6 {
		return ai.CoverPoint{}, &ComponentError{
			Object:    objectID,
			Component: CoverPointComponentName,
			Msg:       "facing vector (facingX, facingY) must be non-zero",
		}
	}
	facing = facing.Normalize()

	var tags []string
	if rawTags, _ := resolved["tags"].(string); rawTags != "" {
		for part := range strings.SplitSeq(rawTags, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				tags = append(tags, trimmed)
			}
		}
	}

	return ai.CoverPoint{
		ID:        id,
		Pos:       world.TransformPoint(math2d.Zero()),
		Facing:    facing,
		Height:    height,
		Elevation: worldZ,
		Tags:      tags,
	}, nil
}
