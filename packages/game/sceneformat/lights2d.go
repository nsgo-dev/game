package sceneformat

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/tileshape"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// Lights2DComponentVersion is the schema version of the built-in 2D lighting components (light2d and occluder2d).
const Lights2DComponentVersion = 1

// Built-in 2D lighting component type names in scene YAML files.
const (
	// Light2DComponentName is the built-in 2D point or spotlight component.
	Light2DComponentName = "light2d"
	// Occluder2DComponentName is the built-in 2D shadow-casting occluder component (rect, polygon, or tilemap collision).
	Occluder2DComponentName = "occluder2d"
)

var (
	light2DKinds     = []string{"point", "spot"}
	occluder2DShapes = []string{"rect", "polygon", "tilemap"}
)

// Lights2DComponentNames returns the names of all built-in 2D lighting component types in canonical order.
func Lights2DComponentNames() []string {
	return []string{
		Light2DComponentName,
		Occluder2DComponentName,
	}
}

// IsLights2DComponent reports whether name is one of the built-in 2D lighting component types.
func IsLights2DComponent(name string) bool {
	return name == Light2DComponentName || name == Occluder2DComponentName
}

// Light2DComponent returns the ComponentType schema for the built-in "light2d" component.
func Light2DComponent() ComponentType {
	return ComponentType{
		Name: Light2DComponentName,
		Doc:  "2D point light or directional spotlight rendered by lights2d.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: Lights2DComponentVersion, Min: Float64(Lights2DComponentVersion), Max: Float64(Lights2DComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "kind", Kind: FieldString, Default: "point", Enum: light2DKinds, Doc: `Light type: "point" (omnidirectional) or "spot" (directional cone).`},
			{Name: "color", Kind: FieldString, Default: "#ffffff", Doc: "CSS color string parsed by core.ParseColor (alpha further scales brightness)."},
			{Name: "radius", Kind: FieldFloat, Default: 128.0, Min: Float64(0), Step: 4, Doc: "Maximum reach of the light in local points."},
			{Name: "intensity", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Step: 0.1, Doc: "Brightness multiplier (>= 0)."},
			{Name: "falloff", Kind: FieldFloat, Default: 2.0, Min: Float64(0), Step: 0.25, Doc: "Distance attenuation exponent in (1 - d/radius)^falloff (0 defaults to 2)."},
			{Name: "direction", Kind: FieldFloat, Default: 0.0, Step: 0.1, Doc: "Local spotlight aim angle in radians (0 = +X, pi/2 = +Y), added to the object's rotation."},
			{Name: "angle", Kind: FieldFloat, Default: 1.0, Min: Float64(0), Max: Float64(2 * math.Pi), Step: 0.05, Doc: "Total angular width of a spotlight cone in radians (ignored for point lights)."},
			{Name: "castShadows", Kind: FieldBool, Default: true, Doc: "Clips the light against scene occluders when true."},
			{Name: "enabled", Kind: FieldBool, Default: true, Doc: "Turns the light on or off."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local horizontal offset from the object origin."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local vertical offset from the object origin."},
		},
	}
}

// Occluder2DComponent returns the ComponentType schema for the built-in "occluder2d" component.
func Occluder2DComponent() ComponentType {
	return ComponentType{
		Name: Occluder2DComponentName,
		Doc:  "2D shadow-casting occluder: a local rectangle, closed polygon, or tilemap collision geometry.",
		Fields: []Field{
			{Name: "version", Kind: FieldInt, Default: Lights2DComponentVersion, Min: Float64(Lights2DComponentVersion), Max: Float64(Lights2DComponentVersion), Step: 1, Doc: "Component schema version (1)."},
			{Name: "shape", Kind: FieldString, Default: "rect", Enum: occluder2DShapes, Doc: `Occluder geometry: "rect", "polygon", or "tilemap" (taken from tile collision via tileshape).`},
			{Name: "width", Kind: FieldFloat, Default: 32.0, Min: Float64(0), Step: 1, Doc: "Local rectangle width in points (shape: rect)."},
			{Name: "height", Kind: FieldFloat, Default: 32.0, Min: Float64(0), Step: 1, Doc: "Local rectangle height in points (shape: rect)."},
			{Name: "offsetX", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local horizontal offset from the object origin (rect and polygon)."},
			{Name: "offsetY", Kind: FieldFloat, Default: 0.0, Step: 1, Doc: "Local vertical offset from the object origin (rect and polygon)."},
			{Name: "points", Kind: FieldPointList, Doc: "Local [x, y] vertices of a closed polygon (shape: polygon)."},
			{Name: "layer", Kind: FieldString, Default: "", Doc: "Scene layer whose tilemap provides collision occluders (shape: tilemap; empty uses this object's layer or all visible tilemap layers)."},
			{Name: "tileLayers", Kind: FieldStringList, Doc: "Optional tile layer names inside the tilemap (shape: tilemap; empty uses all tile layers)."},
			{Name: "enabled", Kind: FieldBool, Default: true, Doc: "Turns shadow casting for this occluder on or off."},
		},
	}
}

// RegisterLights2DComponents registers the built-in light2d and occluder2d component types on reg.
func RegisterLights2DComponents(reg *ComponentRegistry) error {
	for _, ct := range []ComponentType{
		Light2DComponent(),
		Occluder2DComponent(),
	} {
		if err := reg.Register(ct); err != nil {
			return err
		}
	}
	return nil
}

// Lights2DComponentRegistry returns a new ComponentRegistry pre-populated with the built-in 2D lighting components.
func Lights2DComponentRegistry() *ComponentRegistry {
	reg := NewComponentRegistry()
	if err := RegisterLights2DComponents(reg); err != nil {
		panic(err)
	}
	return reg
}

var builtinLights2DReg = Lights2DComponentRegistry()

func checkPointList(v any) ([][2]float64, string) {
	if v == nil {
		return nil, ""
	}
	switch xs := v.(type) {
	case [][2]float64:
		out := make([][2]float64, len(xs))
		for i, p := range xs {
			if !isFinite64(p[0]) || !isFinite64(p[1]) {
				return nil, fmt.Sprintf("point %d must have finite coordinates", i)
			}
			out[i] = p
		}
		return out, ""
	case [][2]float32:
		out := make([][2]float64, len(xs))
		for i, p := range xs {
			x, y := float64(p[0]), float64(p[1])
			if !isFinite64(x) || !isFinite64(y) {
				return nil, fmt.Sprintf("point %d must have finite coordinates", i)
			}
			out[i] = [2]float64{x, y}
		}
		return out, ""
	case [][]float64:
		out := make([][2]float64, len(xs))
		for i, p := range xs {
			if len(p) != 2 || !isFinite64(p[0]) || !isFinite64(p[1]) {
				return nil, fmt.Sprintf("point %d must be a finite [x, y] pair", i)
			}
			out[i] = [2]float64{p[0], p[1]}
		}
		return out, ""
	case [][]float32:
		out := make([][2]float64, len(xs))
		for i, p := range xs {
			if len(p) != 2 {
				return nil, fmt.Sprintf("point %d must be a finite [x, y] pair", i)
			}
			x, y := float64(p[0]), float64(p[1])
			if !isFinite64(x) || !isFinite64(y) {
				return nil, fmt.Sprintf("point %d must be a finite [x, y] pair", i)
			}
			out[i] = [2]float64{x, y}
		}
		return out, ""
	case []any:
		out := make([][2]float64, len(xs))
		for i, elem := range xs {
			pt, ok := parsePointPair(elem)
			if !ok {
				return nil, fmt.Sprintf("point %d must be a finite [x, y] pair, got %s", i, describeValue(elem))
			}
			out[i] = pt
		}
		return out, ""
	default:
		return nil, "want list of [x, y] points, got " + describeValue(v)
	}
}

func parsePointPair(v any) ([2]float64, bool) {
	switch p := v.(type) {
	case [2]float64:
		if !isFinite64(p[0]) || !isFinite64(p[1]) {
			return [2]float64{}, false
		}
		return p, true
	case [2]float32:
		x, y := float64(p[0]), float64(p[1])
		if !isFinite64(x) || !isFinite64(y) {
			return [2]float64{}, false
		}
		return [2]float64{x, y}, true
	case []float64:
		if len(p) != 2 || !isFinite64(p[0]) || !isFinite64(p[1]) {
			return [2]float64{}, false
		}
		return [2]float64{p[0], p[1]}, true
	case []float32:
		if len(p) != 2 {
			return [2]float64{}, false
		}
		x, y := float64(p[0]), float64(p[1])
		if !isFinite64(x) || !isFinite64(y) {
			return [2]float64{}, false
		}
		return [2]float64{x, y}, true
	case []any:
		if len(p) != 2 {
			return [2]float64{}, false
		}
		x, okX := toFloat(p[0])
		y, okY := toFloat(p[1])
		if !okX || !okY || !isFinite64(x) || !isFinite64(y) {
			return [2]float64{}, false
		}
		return [2]float64{x, y}, true
	default:
		return [2]float64{}, false
	}
}

// canonicalComponents returns a shallow copy of doc with point-list component fields (such as occluder2d.points and any [][2]float64 value) wrapped in flowPointList so YAML round-trips deterministically in compact flow form [[x, y], ...].
func canonicalComponents(doc *SceneDoc) *SceneDoc {
	out := *doc
	out.Overrides = canonicalComponentOverrides(doc.Overrides)
	if doc.Layers != nil {
		out.Layers = make([]LayerDoc, len(doc.Layers))
		for i, l := range doc.Layers {
			l.Objects = canonicalComponentObjects(l.Objects)
			out.Layers[i] = l
		}
	}
	return &out
}

func canonicalComponentObjects(objs []ObjectDoc) []ObjectDoc {
	if objs == nil {
		return nil
	}
	out := make([]ObjectDoc, len(objs))
	for i, o := range objs {
		o.Components = canonicalComponentMap(o.Components)
		o.Overrides = canonicalComponentOverrides(o.Overrides)
		o.Children = canonicalComponentObjects(o.Children)
		out[i] = o
	}
	return out
}

func canonicalComponentOverrides(m map[string]ObjectOverride) map[string]ObjectOverride {
	if m == nil {
		return nil
	}
	out := make(map[string]ObjectOverride, len(m))
	for k, ov := range m {
		ov.Components = canonicalComponentMap(ov.Components)
		out[k] = ov
	}
	return out
}

func canonicalComponentMap(comps map[string]any) map[string]any {
	if comps == nil {
		return nil
	}
	var out map[string]any
	for cn, raw := range comps {
		cm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		var cmOut map[string]any
		for fk, fv := range cm {
			var wrapped any
			if pts, ok := fv.([][2]float64); ok {
				wrapped = flowPointList(pts)
			} else if (cn == Occluder2DComponentName || cn == NavRegion2DComponentName) && fk == "points" && fv != nil {
				if pts, msg := checkPointList(fv); msg == "" {
					wrapped = flowPointList(pts)
				}
			}
			if wrapped != nil {
				if cmOut == nil {
					cmOut = make(map[string]any, len(cm))
					for k2, v2 := range cm {
						cmOut[k2] = v2
					}
				}
				cmOut[fk] = wrapped
			}
		}
		if cmOut != nil {
			if out == nil {
				out = make(map[string]any, len(comps))
				for k2, v2 := range comps {
					out[k2] = v2
				}
			}
			out[cn] = cmOut
		}
	}
	if out == nil {
		return comps
	}
	return out
}

// flowPointList marshals a slice of 2D [x, y] points as a compact deterministic YAML flow sequence [[x, y], ...].
type flowPointList [][2]float64

func (pts flowPointList) MarshalYAML() (any, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	seq.Content = make([]*yaml.Node, len(pts))
	for i, p := range pts {
		pair := &yaml.Node{
			Kind:  yaml.SequenceNode,
			Tag:   "!!seq",
			Style: yaml.FlowStyle,
			Content: []*yaml.Node{
				pointScalarNode(p[0]),
				pointScalarNode(p[1]),
			},
		}
		seq.Content[i] = pair
	}
	return seq, nil
}

func pointScalarNode(v float64) *yaml.Node {
	if v == 0 {
		v = 0 // normalize -0 to 0
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	tag := "!!float"
	if isWholeNumber(s) {
		tag = "!!int"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: s}
}

func resolveLights2DComponent(objID, compName string, raw any) (map[string]any, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sceneformat: object %q component %q must be a mapping", objID, compName)
	}
	res, err := builtinLights2DReg.Resolve(compName, m)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: object %q %w", objID, err)
	}
	return res, nil
}

// Light2DDebug describes a resolved light2d component in local and world space for editor overlays and handles.
type Light2DDebug struct {
	Kind        string
	Color       core.Color
	Radius      float32
	WorldRadius float32
	Intensity   float32
	Falloff     float32
	Direction   float32
	Angle       float32
	CastShadows bool
	Enabled     bool
	OffsetX     float32
	OffsetY     float32
	WorldX      float32
	WorldY      float32
	WorldAngle  float32
}

// Occluder2DDebug describes a resolved occluder2d component in local and world space for editor overlays and shape handles.
type Occluder2DDebug struct {
	Shape      string
	Width      float32
	Height     float32
	OffsetX    float32
	OffsetY    float32
	Points     [][2]float64
	Layer      string
	TileLayers []string
	Enabled    bool
	// WorldVertices are the transformed closed polygon corners/vertices in world space (for rect and polygon shapes).
	WorldVertices []core.Point
}

// ExtractLight2D resolves a light2d component from o at world (or o's local transform when world is nil), returning the built *lights2d.Light and debug metadata.
func ExtractLight2D(o *ObjectDoc, world *math2d.Affine) (*lights2d.Light, Light2DDebug, error) {
	if o == nil || len(o.Components) == 0 {
		return nil, Light2DDebug{}, nil
	}
	raw, ok := o.Components[Light2DComponentName]
	if !ok {
		return nil, Light2DDebug{}, nil
	}
	res, err := resolveLights2DComponent(o.ID, Light2DComponentName, raw)
	if err != nil {
		return nil, Light2DDebug{}, err
	}
	colorStr := res["color"].(string)
	col, err := core.ParseColor(colorStr)
	if err != nil {
		return nil, Light2DDebug{}, fmt.Errorf("sceneformat: object %q component %q field \"color\" %q: %w", o.ID, Light2DComponentName, colorStr, err)
	}
	m := o.LocalTransform()
	if world != nil {
		m = *world
	}
	ox := float32(res["offsetX"].(float64))
	oy := float32(res["offsetY"].(float64))
	radius := float32(res["radius"].(float64))
	intensity := float32(res["intensity"].(float64))
	falloff := float32(res["falloff"].(float64))
	dir := float32(res["direction"].(float64))
	angle := float32(res["angle"].(float64))
	kind := res["kind"].(string)
	castShadows := res["castShadows"].(bool)
	enabled := res["enabled"].(bool)

	pos := m.TransformPoint(math2d.Vec2{X: ox, Y: oy})
	scaleX := math.Hypot(float64(m.A), float64(m.B))
	scaleY := math.Hypot(float64(m.C), float64(m.D))
	worldScale := float32(math.Max(scaleX, scaleY))
	if worldScale == 0 {
		worldScale = 1
	}
	worldRadius := radius * worldScale
	if math.IsNaN(float64(worldRadius)) || math.IsInf(float64(worldRadius), 0) {
		return nil, Light2DDebug{}, fmt.Errorf("sceneformat: object %q component %q world radius overflows float32", o.ID, Light2DComponentName)
	}

	sinD, cosD := math.Sincos(float64(dir))
	dirVec := m.TransformVector(math2d.Vec2{X: float32(cosD), Y: float32(sinD)})
	worldAngle := float32(math.Atan2(float64(dirVec.Y), float64(dirVec.X)))

	l := &lights2d.Light{
		X:           pos.X,
		Y:           pos.Y,
		Radius:      worldRadius,
		Color:       col,
		Intensity:   intensity,
		Falloff:     falloff,
		CastShadows: castShadows,
		Enabled:     enabled,
	}
	if kind == "spot" {
		l.ConeAngle = worldAngle
		l.ConeSpread = angle
	}
	dbg := Light2DDebug{
		Kind:        kind,
		Color:       col,
		Radius:      radius,
		WorldRadius: worldRadius,
		Intensity:   intensity,
		Falloff:     falloff,
		Direction:   dir,
		Angle:       angle,
		CastShadows: castShadows,
		Enabled:     enabled,
		OffsetX:     ox,
		OffsetY:     oy,
		WorldX:      pos.X,
		WorldY:      pos.Y,
		WorldAngle:  worldAngle,
	}
	return l, dbg, nil
}

// ExtractOccluder2D resolves an occluder2d component from o at world (or o's local transform when world is nil).
func ExtractOccluder2D(o *ObjectDoc, world *math2d.Affine) (Occluder2DDebug, bool, error) {
	if o == nil || len(o.Components) == 0 {
		return Occluder2DDebug{}, false, nil
	}
	raw, ok := o.Components[Occluder2DComponentName]
	if !ok {
		return Occluder2DDebug{}, false, nil
	}
	res, err := resolveLights2DComponent(o.ID, Occluder2DComponentName, raw)
	if err != nil {
		return Occluder2DDebug{}, false, err
	}
	m := o.LocalTransform()
	if world != nil {
		m = *world
	}
	shape := res["shape"].(string)
	w := float32(res["width"].(float64))
	h := float32(res["height"].(float64))
	ox := float32(res["offsetX"].(float64))
	oy := float32(res["offsetY"].(float64))
	pts, _ := res["points"].([][2]float64)
	layer := res["layer"].(string)
	tileLayers, _ := res["tileLayers"].([]string)
	enabled := res["enabled"].(bool)

	dbg := Occluder2DDebug{
		Shape:      shape,
		Width:      w,
		Height:     h,
		OffsetX:    ox,
		OffsetY:    oy,
		Points:     slices.Clone(pts),
		Layer:      layer,
		TileLayers: slices.Clone(tileLayers),
		Enabled:    enabled,
	}
	switch shape {
	case "rect":
		if w > 0 && h > 0 {
			hw, hh := w*0.5, h*0.5
			local := [4]math2d.Vec2{
				{X: ox - hw, Y: oy - hh},
				{X: ox + hw, Y: oy - hh},
				{X: ox + hw, Y: oy + hh},
				{X: ox - hw, Y: oy + hh},
			}
			dbg.WorldVertices = make([]core.Point, 4)
			for i, lp := range local {
				wp := m.TransformPoint(lp)
				dbg.WorldVertices[i] = core.Point{X: core.Points(wp.X), Y: core.Points(wp.Y)}
			}
		}
	case "polygon":
		if len(pts) > 0 {
			dbg.WorldVertices = make([]core.Point, len(pts))
			for i, lp := range pts {
				wp := m.TransformPoint(math2d.Vec2{X: ox + float32(lp[0]), Y: oy + float32(lp[1])})
				dbg.WorldVertices[i] = core.Point{X: core.Points(wp.X), Y: core.Points(wp.Y)}
			}
		}
	}
	return dbg, true, nil
}

// BuildLights2D constructs a *lights2d.Scene from a BuiltScene's AmbientColor, built-in light2d and occluder2d components on visible world-space layers, and any tilemap collision occluders referenced by occluder2d (shape: "tilemap").
// It returns (nil, nil) when the scene declares no ambientColor, no light2d components, and no occluder2d components.
func BuildLights2D(scene *BuiltScene) (*lights2d.Scene, error) {
	if scene == nil {
		return nil, nil
	}
	ls := &lights2d.Scene{}
	hasAny := false
	if scene.HasAmbient {
		ls.Ambient = scene.AmbientColor
		hasAny = true
	}

	var walk func(layer *BuiltLayer, objs []*BuiltObject) error
	walk = func(layer *BuiltLayer, objs []*BuiltObject) error {
		for _, bo := range objs {
			if bo == nil || bo.Doc == nil {
				continue
			}
			if _, hasLight := bo.Doc.Components[Light2DComponentName]; hasLight {
				l, _, err := ExtractLight2D(bo.Doc, &bo.World)
				if err != nil {
					return err
				}
				if l != nil {
					ls.Lights = append(ls.Lights, l)
					hasAny = true
				}
			}
			if _, hasOcc := bo.Doc.Components[Occluder2DComponentName]; hasOcc {
				occ, ok, err := ExtractOccluder2D(bo.Doc, &bo.World)
				if err != nil {
					return err
				}
				if ok {
					hasAny = true
					if occ.Enabled {
						switch occ.Shape {
						case "rect", "polygon":
							if len(occ.WorldVertices) >= 2 {
								ls.Occluders = append(ls.Occluders, lights2d.PolygonOccluder(occ.WorldVertices...))
							}
						case "tilemap":
							ls.Occluders = appendTilemapOccluders(ls.Occluders, scene, layer, occ.Layer, occ.TileLayers)
						}
					}
				}
			}
			if len(bo.Children) > 0 {
				if err := walk(layer, bo.Children); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, layer := range scene.Layers {
		if !layer.Visible || layer.Space != SpaceWorld {
			continue
		}
		if err := walk(layer, layer.Objects); err != nil {
			return nil, err
		}
	}

	if !hasAny {
		return nil, nil
	}
	return ls, nil
}

func appendTilemapOccluders(dst []lights2d.Occluder, scene *BuiltScene, currentLayer *BuiltLayer, layerName string, tileLayers []string) []lights2d.Occluder {
	opts := tileshape.Options{LayerNames: tileLayers}
	if layerName != "" {
		if target := scene.Layer(layerName); target != nil && target.Map != nil {
			v := tileshape.New(target.Map, opts)
			return append(dst, v.Occluders()...)
		}
		return dst
	}
	if currentLayer != nil && currentLayer.Map != nil {
		v := tileshape.New(currentLayer.Map, opts)
		return append(dst, v.Occluders()...)
	}
	for _, l := range scene.Layers {
		if l.Visible && l.Space == SpaceWorld && l.Map != nil {
			v := tileshape.New(l.Map, opts)
			dst = append(dst, v.Occluders()...)
		}
	}
	return dst
}

// SyncLights2D rebuilds s.Lights2D from the scene's current object transforms, component values, ambient color, and tilemaps, preserving LightComposite and DisableShadows when a Lights2D scene already existed.
func (s *BuiltScene) SyncLights2D() error {
	if s == nil {
		return nil
	}
	var prevComp core.CompositeOp
	var prevDisable bool
	if s.Lights2D != nil {
		prevComp = s.Lights2D.LightComposite
		prevDisable = s.Lights2D.DisableShadows
	}
	next, err := BuildLights2D(s)
	if err != nil {
		return err
	}
	if next != nil {
		next.LightComposite = prevComp
		next.DisableShadows = prevDisable
	}
	s.Lights2D = next
	return nil
}

func (s *BuiltScene) drawLights2D(d *core.Drawing, vw, vh float32) {
	if s == nil || s.Lights2D == nil || d == nil {
		return
	}
	bounds, ok := s.lights2DWorldBounds(vw, vh)
	if !ok {
		return
	}
	if s.Camera == nil {
		s.Lights2D.Draw(d, bounds)
		return
	}
	cam := s.Camera
	zoom := cam.EffectiveZoom()
	cx, cy := vw/2, vh/2
	camX, camY := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
	if zoom == 1 && cam.Rotation == 0 && cx-camX == 0 && cy-camY == 0 {
		s.Lights2D.Draw(d, bounds)
		return
	}
	d.Save()
	if zoom == 1 && cam.Rotation == 0 {
		d.Translate(cx-camX, cy-camY)
	} else {
		if cx != 0 || cy != 0 {
			d.Translate(cx, cy)
		}
		if zoom != 1 {
			d.Scale(zoom, zoom)
		}
		if cam.Rotation != 0 {
			d.Rotate(-cam.Rotation)
		}
		if camX != 0 || camY != 0 {
			d.Translate(-camX, -camY)
		}
	}
	s.Lights2D.Draw(d, bounds)
	d.Restore()
}

func (s *BuiltScene) lights2DWorldBounds(vw, vh float32) (core.Bounds, bool) {
	if s.Lights2DBounds != nil {
		b := *s.Lights2DBounds
		return b, b.Size.Width > 0 && b.Size.Height > 0
	}
	if vw > 0 && vh > 0 {
		if s.Camera == nil {
			return core.Bounds{
				Size: core.Size{Width: core.Points(vw), Height: core.Points(vh)},
			}, true
		}
		cam := s.Camera
		zoom := cam.EffectiveZoom()
		if zoom <= 0 {
			zoom = 1
		}
		cx, cy := vw/2, vh/2
		camX, camY := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
		sinR, cosR := math.Sincos(float64(cam.Rotation))
		toWorld := func(sx, sy float32) (float32, float32) {
			rx := (sx - cx) / zoom
			ry := (sy - cy) / zoom
			if cam.Rotation == 0 {
				return camX + rx, camY + ry
			}
			wx := camX + float32(float64(rx)*cosR-float64(ry)*sinR)
			wy := camY + float32(float64(rx)*sinR+float64(ry)*cosR)
			return wx, wy
		}
		corners := [4][2]float32{{0, 0}, {vw, 0}, {vw, vh}, {0, vh}}
		minX, minY := toWorld(corners[0][0], corners[0][1])
		maxX, maxY := minX, minY
		for _, c := range corners[1:] {
			wx, wy := toWorld(c[0], c[1])
			minX, maxX = min(minX, wx), max(maxX, wx)
			minY, maxY = min(minY, wy), max(maxY, wy)
		}
		return core.Bounds{
			Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
			Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
		}, maxX > minX && maxY > minY
	}
	// Fallback when virtual viewport size is 0: bound by all lights and occluder vertices.
	have := false
	var minX, minY, maxX, maxY float32
	addPt := func(x, y float32) {
		if !have {
			minX, maxX, minY, maxY = x, x, y, y
			have = true
			return
		}
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	for _, l := range s.Lights2D.Lights {
		if l == nil {
			continue
		}
		addPt(l.X-l.Radius, l.Y-l.Radius)
		addPt(l.X+l.Radius, l.Y+l.Radius)
	}
	for _, occ := range s.Lights2D.Occluders {
		for _, p := range occ.Vertices {
			addPt(float32(p.X), float32(p.Y))
		}
		for _, seg := range occ.Segments {
			addPt(float32(seg.A.X), float32(seg.A.Y))
			addPt(float32(seg.B.X), float32(seg.B.Y))
		}
	}
	if !have || maxX <= minX || maxY <= minY {
		return core.Bounds{}, false
	}
	return core.Bounds{
		Origin: core.Point{X: core.Points(minX), Y: core.Points(minY)},
		Size:   core.Size{Width: core.Points(maxX - minX), Height: core.Points(maxY - minY)},
	}, true
}
