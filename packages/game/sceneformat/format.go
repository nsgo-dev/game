package sceneformat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the scene format schema version supported by this package.
const CurrentVersion = 1

// SchemaURL is where the JSON Schema of version 1 scene documents is published. Encode names it in a yaml-language-server comment on the first line, so editors that run the YAML language server (such as VS Code's YAML extension) and AI tools validate and complete a scene file as it is written.
const SchemaURL = "https://game.nsgo.dev/schemas/scene.v1.json"

// SchemaComment is the first line Encode writes: a YAML comment that points a YAML language server at SchemaURL. Parse ignores it like any other comment.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

// Space is the coordinate space a layer draws in.
type Space string

const (
	// SpaceWorld draws the layer under the scene's camera transform.
	SpaceWorld Space = "world"
	// SpaceScreen draws the layer directly in canvas coordinates, ignoring the camera.
	SpaceScreen Space = "screen"
)

// SceneDoc is the root of a versioned YAML scene document.
type SceneDoc struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version" schema:"required,const=1"`
	// Name is the scene's human-readable name.
	Name string `yaml:"name,omitempty"`
	// VirtualWidth and VirtualHeight are the scene's virtual viewport size in points. Both must be 0 or both positive.
	VirtualWidth  float32 `yaml:"virtualWidth,omitempty" schema:"minimum=0"`
	VirtualHeight float32 `yaml:"virtualHeight,omitempty" schema:"minimum=0"`
	// BackgroundColor is an optional CSS color string parsed with core.ParseColor.
	BackgroundColor string `yaml:"backgroundColor,omitempty"`
	// Camera holds the default camera transform for world-space layers. Nil leaves world space untransformed.
	Camera *CameraDoc `yaml:"camera,omitempty"`
	// Tilemaps defines reusable named tilemaps referenced by layers via TilemapDoc.Ref.
	Tilemaps map[string]*TilemapDoc `yaml:"tilemaps,omitempty"`
	// Layers are the scene's layers, drawn lowest Order first and in slice order for equal Order.
	Layers []LayerDoc `yaml:"layers,omitempty"`
}

// CameraDoc describes a 2D camera transform for world-space layers.
type CameraDoc struct {
	// X and Y are the world coordinates centered in the virtual viewport (or mapped to the origin when virtual dimensions are 0).
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Zoom is the camera zoom factor; nil defaults to 1. Must be positive when set.
	Zoom *float32 `yaml:"zoom,omitempty" schema:"exclusiveMinimum=0"`
	// Rotation turns the camera clockwise in radians; world geometry rotates by -Rotation around (X, Y).
	Rotation float32 `yaml:"rotation,omitempty"`
	// Bounds optionally clamps the camera view inside a world rectangle.
	Bounds *BoundsDoc `yaml:"bounds,omitempty"`
}

// EffectiveZoom returns the camera's zoom factor, defaulting to 1 when Zoom is nil.
func (c *CameraDoc) EffectiveZoom() float32 {
	if c == nil || c.Zoom == nil {
		return 1
	}
	return *c.Zoom
}

// BoundsDoc is an axis-aligned rectangle in world coordinates.
type BoundsDoc struct {
	// X and Y are the rectangle's top-left corner.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// Width and Height are the rectangle's size; both must be positive.
	Width  float32 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	Height float32 `yaml:"height" schema:"required,exclusiveMinimum=0"`
}

// LayerDoc is one ordered layer in a scene.
type LayerDoc struct {
	// Name uniquely identifies the layer within the scene.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Space is SpaceWorld or SpaceScreen; empty defaults to SpaceWorld.
	Space Space `yaml:"space,omitempty" schema:"enum=world|screen"`
	// Order sets draw order across layers: lower values draw first, ties keep document order.
	Order int `yaml:"order,omitempty"`
	// Visible controls whether the layer draws; nil defaults to true.
	Visible *bool `yaml:"visible,omitempty"`
	// Tilemap optionally attaches a tilemap (by Ref, external Source, or inline grid) drawn beneath Objects.
	Tilemap *TilemapDoc `yaml:"tilemap,omitempty"`
	// Objects are the placed objects in this layer.
	Objects []ObjectDoc `yaml:"objects,omitempty"`
}

// IsVisible reports whether the layer is visible, defaulting to true when Visible is nil.
func (l *LayerDoc) IsVisible() bool {
	return l == nil || l.Visible == nil || *l.Visible
}

// EffectiveSpace returns SpaceWorld when Space is empty, and l.Space otherwise.
func (l *LayerDoc) EffectiveSpace() Space {
	if l == nil || l.Space == "" {
		return SpaceWorld
	}
	return l.Space
}

// TilemapDoc describes either a reference to a named top-level tilemap (Ref), an external Tiled JSON map (Source), or an inline tile grid.
type TilemapDoc struct {
	// Ref names an entry in SceneDoc.Tilemaps. Only valid on a layer's Tilemap.
	Ref string `yaml:"ref,omitempty"`
	// Source is a path to an external Tiled .tmj file resolved by a TilemapResolver.
	Source string `yaml:"source,omitempty"`
	// TileWidth and TileHeight are each tile's size in points for an inline grid.
	TileWidth  int `yaml:"tileWidth,omitempty" schema:"minimum=0"`
	TileHeight int `yaml:"tileHeight,omitempty" schema:"minimum=0"`
	// Width and Height are the inline grid dimensions in tiles.
	Width  int `yaml:"width,omitempty" schema:"minimum=0"`
	Height int `yaml:"height,omitempty" schema:"minimum=0"`
	// Tileset is the atlas name providing frames keyed by 1-based tile ID ("1", "2", ...).
	Tileset string `yaml:"tileset,omitempty"`
	// Data holds Width*Height row-major tile IDs; 0 is an empty cell.
	Data []int `yaml:"data,omitempty,flow"`
}

// IsRef reports whether t references a top-level tilemap by name.
func (t *TilemapDoc) IsRef() bool { return t != nil && t.Ref != "" }

// IsExternal reports whether t references an external tilemap source file.
func (t *TilemapDoc) IsExternal() bool { return t != nil && t.Source != "" }

// IsInline reports whether t carries an inline tile grid.
func (t *TilemapDoc) IsInline() bool {
	return t != nil && (t.TileWidth != 0 || t.TileHeight != 0 || t.Width != 0 || t.Height != 0 || t.Tileset != "" || len(t.Data) > 0)
}

// ObjectDoc is one entity or visual object placed on a layer.
type ObjectDoc struct {
	// ID uniquely identifies the object across the entire scene.
	ID string `yaml:"id" schema:"required,minLength=1"`
	// Name is an optional human-readable display name.
	Name string `yaml:"name,omitempty"`
	// Prefab optionally names a prefab or archetype template.
	Prefab string `yaml:"prefab,omitempty"`
	// X and Y are where the object's origin sits in its parent's space, which for a top-level object is its layer's coordinate space.
	X float32 `yaml:"x,omitempty"`
	Y float32 `yaml:"y,omitempty"`
	// PosZ is the optional 3D Z coordinate (depth in 3D scenes) in its parent's space, distinct from 2D draw order Z.
	PosZ float32 `yaml:"posZ,omitempty"`
	// OriginX and OriginY are the pivot point inside the sprite's untrimmed frame.
	OriginX float32 `yaml:"originX,omitempty"`
	OriginY float32 `yaml:"originY,omitempty"`
	// Rotation turns the object clockwise in radians about its origin in 2D space.
	Rotation float32 `yaml:"rotation,omitempty"`
	// RotX, RotY, and RotZ are optional 3D Euler rotations in radians about the X, Y, and Z axes, applied in that order about fixed axes (the rotation Rz * Ry * Rx).
	RotX float32 `yaml:"rotX,omitempty"`
	RotY float32 `yaml:"rotY,omitempty"`
	RotZ float32 `yaml:"rotZ,omitempty"`
	// ScaleX, ScaleY, and ScaleZ scale the object about its origin; nil defaults to 1. Negative mirrors.
	ScaleX *float32 `yaml:"scaleX,omitempty"`
	ScaleY *float32 `yaml:"scaleY,omitempty"`
	ScaleZ *float32 `yaml:"scaleZ,omitempty"`
	// Depth is the optional 3D extent along the Z axis in world units; 0 uses a default extent.
	Depth float32 `yaml:"depth,omitempty" schema:"minimum=0"`
	// Mesh optionally names a 3D mesh primitive or asset path for 3D scene views.
	Mesh string `yaml:"mesh,omitempty"`
	// FlipX and FlipY mirror the sprite inside its untrimmed frame box.
	FlipX bool `yaml:"flipX,omitempty"`
	FlipY bool `yaml:"flipY,omitempty"`
	// Alpha is the object's opacity in [0, 1]; nil defaults to 1.
	Alpha *float32 `yaml:"alpha,omitempty" schema:"minimum=0,maximum=1"`
	// Z orders objects within the layer, children included; lower Z draws first, and ties keep document order with each parent before its children.
	Z int `yaml:"z,omitempty"`
	// Sprite optionally binds the object to a frame in a sprite.Atlas.
	Sprite *SpriteRef `yaml:"sprite,omitempty"`
	// Material optionally names a shared material resource file by its slash-separated path from the project root, such as "materials/stone.material.yaml". Build resolves it through a MaterialResolver; the sprite draws with the material's opacity and blend mode, so every scene that uses the file changes when it is edited.
	Material string `yaml:"material,omitempty"`
	// Components holds structured component data keyed by component type name.
	Components map[string]any `yaml:"components,omitempty"`
	// Properties holds arbitrary custom metadata for gameplay scripts or editors.
	Properties map[string]any `yaml:"properties,omitempty"`
	// Children are objects attached to this one, in document order. A child's transform (X, Y, Rotation, ScaleX, ScaleY, and in 3D PosZ, RotX, RotY, RotZ, ScaleZ) is local to this object, and its alpha multiplies this object's. Children belong to their parent's layer; their IDs share the scene-wide namespace, and nesting is limited to MaxObjectDepth levels.
	Children []ObjectDoc `yaml:"children,omitempty"`
}

// EffectiveScaleX returns ScaleX, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleX() float32 {
	if o == nil || o.ScaleX == nil {
		return 1
	}
	return *o.ScaleX
}

// EffectiveScaleY returns ScaleY, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleY() float32 {
	if o == nil || o.ScaleY == nil {
		return 1
	}
	return *o.ScaleY
}

// EffectiveScaleZ returns ScaleZ, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveScaleZ() float32 {
	if o == nil || o.ScaleZ == nil {
		return 1
	}
	return *o.ScaleZ
}

// LocalTransform returns the object's 2D transform relative to its parent (or to its layer for a top-level object): translate by (X, Y), rotate by Rotation, then scale by (ScaleX, ScaleY). OriginX, OriginY and the flips place a sprite inside this space and are not part of it.
func (o *ObjectDoc) LocalTransform() math2d.Affine {
	if o == nil {
		return math2d.Identity()
	}
	return math2d.TRS(math2d.Vec2{X: o.X, Y: o.Y}, o.Rotation, math2d.Vec2{X: o.EffectiveScaleX(), Y: o.EffectiveScaleY()})
}

// EffectiveAlpha returns Alpha, defaulting to 1 when nil.
func (o *ObjectDoc) EffectiveAlpha() float32 {
	if o == nil || o.Alpha == nil {
		return 1
	}
	return *o.Alpha
}

// SpriteRef identifies one frame inside a named sprite.Atlas.
type SpriteRef struct {
	// Atlas names the sprite.Atlas an AssetResolver looks up.
	Atlas string `yaml:"atlas" schema:"required,minLength=1"`
	// Frame names the frame inside the atlas.
	Frame string `yaml:"frame" schema:"required,minLength=1"`
}

// MaxObjectDepth is how many levels of objects a layer may nest: a top-level object is at depth 1, its children at depth 2, and so on. Validate refuses a deeper tree.
const MaxObjectDepth = 64

// WalkObjects calls fn for every object in objs and its descendants, depth first with each parent before its children and siblings in document order. parent is nil for an object in objs. The walk stops when fn returns false, and WalkObjects reports whether it visited every object.
func WalkObjects(objs []ObjectDoc, fn func(o, parent *ObjectDoc) bool) bool {
	return walkObjects(objs, nil, fn)
}

func walkObjects(objs []ObjectDoc, parent *ObjectDoc, fn func(o, parent *ObjectDoc) bool) bool {
	for i := range objs {
		o := &objs[i]
		if !fn(o, parent) {
			return false
		}
		if !walkObjects(o.Children, o, fn) {
			return false
		}
	}
	return true
}

// errShearedSprite explains why a sprite whose composed world transform is sheared is refused.
var errShearedSprite = errors.New("its world transform is sheared (a rotated object under a non-uniformly scaled parent), which a sprite cannot draw")

// CheckObjectTree checks the shape of the tree rooted at o, taking o as a top-level object: nesting no deeper than MaxObjectDepth levels, and no sprite whose composed world transform is sheared. Validate makes the same checks for every layer of a scene; CheckObjectTree lets a caller such as an editor refuse a tree or a transform change before it reaches a document. It does not check IDs or field values.
func CheckObjectTree(o *ObjectDoc) error {
	return checkObjectTree(o, nil, 1)
}

func checkObjectTree(o *ObjectDoc, parentWorld *math2d.Affine, depth int) error {
	if depth > MaxObjectDepth {
		return fmt.Errorf("sceneformat: object %q nests deeper than %d levels", o.ID, MaxObjectDepth)
	}
	world, ok := composeObjectWorld(parentWorld, o)
	if !ok {
		return fmt.Errorf("sceneformat: object %q: %w", o.ID, errShearedSprite)
	}
	for i := range o.Children {
		if err := checkObjectTree(&o.Children[i], &world, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// composeObjectWorld returns o's world transform under a parent whose world transform is parentWorld (nil for a top-level object), composed the way Build composes BuiltObject.World. It reports false when o has a sprite that this world transform shears, which only a child can be.
func composeObjectWorld(parentWorld *math2d.Affine, o *ObjectDoc) (math2d.Affine, bool) {
	world := o.LocalTransform()
	if parentWorld == nil {
		return world, true
	}
	world = parentWorld.Mul(world)
	if o.Sprite != nil {
		if _, _, _, _, _, ok := decomposeTRS(world); !ok {
			return world, false
		}
	}
	return world, true
}

// Bool returns a pointer to v for optional YAML boolean fields such as LayerDoc.Visible.
func Bool(v bool) *bool { return &v }

// Float32 returns a pointer to v for optional YAML float fields such as ScaleX, ScaleY, Alpha and Zoom.
func Float32(v float32) *float32 { return &v }

// Parse decodes a YAML scene document and validates it. Unknown YAML fields and trailing YAML documents are rejected. Component payloads are not checked against any registry; use ParseWith to check them.
func Parse(data []byte) (*SceneDoc, error) {
	return ParseWith(data, ParseOptions{})
}

// ParseOptions configures ParseWith.
type ParseOptions struct {
	// File names the source in errors, such as "scenes/level1.scene.yaml". Empty leaves errors unprefixed.
	File string
	// Components, when non-nil, checks every object's components against the registered types: an unregistered type, a misspelled or unknown field, a value of the wrong kind or out of range fails the load with a *ComponentError carrying File, Line and Column.
	Components *ComponentRegistry
}

// ParseWith is Parse with a source name for errors and an optional component registry. Defaults of unset component fields are not written into the document, so saving it again changes nothing; read them with ComponentRegistry.Resolve.
func ParseWith(data []byte, opts ParseOptions) (*SceneDoc, error) {
	doc, err := parse(data, opts.Components)
	if err != nil {
		var ce *ComponentError
		if errors.As(err, &ce) {
			ce.File = opts.File
			return nil, ce
		}
		if opts.File != "" {
			return nil, fmt.Errorf("%s: %w", opts.File, err)
		}
		return nil, err
	}
	return doc, nil
}

func parse(data []byte, reg *ComponentRegistry) (*SceneDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("sceneformat: empty scene document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc SceneDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("sceneformat: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("sceneformat: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("sceneformat: decode: %w", err)
	}
	if err := Validate(&doc); err != nil {
		return nil, err
	}
	if reg != nil {
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("sceneformat: decode: %w", err)
		}
		if err := checkComponentNodes(&root, &doc, reg); err != nil {
			return nil, err
		}
		// The node walk above locates errors in plain mappings; this pass also covers values it could not see key by key, such as merge keys.
		if err := ValidateComponents(&doc, reg); err != nil {
			return nil, err
		}
	}
	return &doc, nil
}

// Encode validates doc and serializes it as deterministic 2-space YAML, starting with the SchemaComment line.
func Encode(doc *SceneDoc) ([]byte, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(SchemaComment + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("sceneformat: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("sceneformat: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// FieldError is the error Validate returns: what is wrong with a scene document and which field it is about, so a tool such as `game check` can point at the line that holds it.
type FieldError struct {
	// Path leads from the document's root to the field, one mapping key or decimal sequence index per step, such as ["layers", "2", "objects", "0", "alpha"]. It is empty when the error is about the whole document. A step may name a field the document leaves out, such as a required tileWidth, in which case the nearest written field is the one to point at.
	Path []string
	// Err is the error itself; FieldError's message is Err's.
	Err error
}

// Error returns Err's message.
func (e *FieldError) Error() string { return e.Err.Error() }

// Unwrap returns Err.
func (e *FieldError) Unwrap() error { return e.Err }

// fieldErr returns a FieldError at path with a formatted message.
func fieldErr(path []string, format string, args ...any) error {
	return &FieldError{Path: path, Err: fmt.Errorf(format, args...)}
}

// at returns base followed by steps, in a slice of its own so that paths built from one base never share storage.
func at(base []string, steps ...string) []string {
	p := make([]string, 0, len(base)+len(steps))
	p = append(p, base...)
	return append(p, steps...)
}

// Validate checks that doc conforms to version 1 schema rules. Every error it returns is a *FieldError naming the field at fault. When several fields are wrong it reports the first in document order, taking the entries of Tilemaps in key order.
func Validate(doc *SceneDoc) error {
	if doc == nil {
		return fieldErr(nil, "sceneformat: nil scene document")
	}
	if doc.Version != CurrentVersion {
		return fieldErr([]string{"version"}, "sceneformat: unsupported version %d (want %d)", doc.Version, CurrentVersion)
	}
	if !isFinite(doc.VirtualWidth) || !isFinite(doc.VirtualHeight) || doc.VirtualWidth < 0 || doc.VirtualHeight < 0 {
		field := "virtualHeight"
		if !isFinite(doc.VirtualWidth) || doc.VirtualWidth < 0 {
			field = "virtualWidth"
		}
		return fieldErr([]string{field}, "sceneformat: virtual size (%v, %v) must be finite and non-negative", doc.VirtualWidth, doc.VirtualHeight)
	}
	if (doc.VirtualWidth == 0) != (doc.VirtualHeight == 0) {
		// Point at the size that is written: the one left out has no line.
		field := "virtualWidth"
		if doc.VirtualWidth == 0 {
			field = "virtualHeight"
		}
		return fieldErr([]string{field}, "sceneformat: virtualWidth (%v) and virtualHeight (%v) must both be positive or both be 0", doc.VirtualWidth, doc.VirtualHeight)
	}
	if doc.BackgroundColor != "" {
		if _, err := core.ParseColor(doc.BackgroundColor); err != nil {
			return fieldErr([]string{"backgroundColor"}, "sceneformat: backgroundColor %q: %w", doc.BackgroundColor, err)
		}
	}
	if doc.Camera != nil {
		if err := validateCamera(doc.Camera); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Tilemaps)) {
		if name == "" {
			return fieldErr([]string{"tilemaps"}, "sceneformat: tilemaps contains an empty key")
		}
		if err := validateTilemap([]string{"tilemaps", name}, fmt.Sprintf("tilemap %q", name), doc.Tilemaps[name], false, nil); err != nil {
			return err
		}
	}

	layerNames := make(map[string]struct{}, len(doc.Layers))
	objectIDs := make(map[string]struct{})
	for i := range doc.Layers {
		l := &doc.Layers[i]
		lp := []string{"layers", strconv.Itoa(i)}
		if l.Name == "" {
			return fieldErr(at(lp, "name"), "sceneformat: layer %d has an empty name", i)
		}
		if _, dup := layerNames[l.Name]; dup {
			return fieldErr(at(lp, "name"), "sceneformat: duplicate layer name %q", l.Name)
		}
		layerNames[l.Name] = struct{}{}

		switch l.Space {
		case "", SpaceWorld, SpaceScreen:
		default:
			return fieldErr(at(lp, "space"), "sceneformat: layer %q has invalid space %q (want %q or %q)", l.Name, l.Space, SpaceWorld, SpaceScreen)
		}
		if l.Tilemap != nil {
			if err := validateTilemap(at(lp, "tilemap"), fmt.Sprintf("layer %q tilemap", l.Name), l.Tilemap, true, doc.Tilemaps); err != nil {
				return err
			}
		}
		if err := validateObjects(at(lp, "objects"), l.Name, "", nil, l.Objects, 1, objectIDs); err != nil {
			return err
		}
	}
	return nil
}

// validateObjects checks objs, found at path in the document (the top-level objects of layer when parent is empty, or the children of the object parent, whose world transform is parentWorld), at nesting depth and their descendants, recording every ID in ids.
func validateObjects(path []string, layer, parent string, parentWorld *math2d.Affine, objs []ObjectDoc, depth int, ids map[string]struct{}) error {
	if len(objs) > 0 && depth > MaxObjectDepth {
		return fieldErr(path, "sceneformat: layer %q object %q nests children deeper than %d levels", layer, parent, MaxObjectDepth)
	}
	for j := range objs {
		o := &objs[j]
		op := at(path, strconv.Itoa(j))
		if o.ID == "" {
			if parent == "" {
				return fieldErr(at(op, "id"), "sceneformat: layer %q object %d has an empty id", layer, j)
			}
			return fieldErr(at(op, "id"), "sceneformat: layer %q object %q child %d has an empty id", layer, parent, j)
		}
		if _, dup := ids[o.ID]; dup {
			return fieldErr(at(op, "id"), "sceneformat: duplicate object id %q", o.ID)
		}
		ids[o.ID] = struct{}{}
		if err := validateObject(op, layer, o); err != nil {
			return err
		}
		world, ok := composeObjectWorld(parentWorld, o)
		if !ok {
			return fieldErr(op, "sceneformat: layer %q object %q: %w", layer, o.ID, errShearedSprite)
		}
		if err := validateObjects(at(op, "children"), layer, o.ID, &world, o.Children, depth+1, ids); err != nil {
			return err
		}
	}
	return nil
}

func validateCamera(c *CameraDoc) error {
	cp := []string{"camera"}
	if !isFinite(c.X) || !isFinite(c.Y) || !isFinite(c.Rotation) {
		return fieldErr(cp, "sceneformat: camera x, y and rotation must be finite")
	}
	if c.Zoom != nil {
		if !isFinite(*c.Zoom) || *c.Zoom <= 0 {
			return fieldErr(at(cp, "zoom"), "sceneformat: camera zoom %v must be finite and positive", *c.Zoom)
		}
	}
	if c.Bounds != nil {
		b := c.Bounds
		if !isFinite(b.X) || !isFinite(b.Y) || !isFinite(b.Width) || !isFinite(b.Height) {
			return fieldErr(at(cp, "bounds"), "sceneformat: camera bounds must be finite")
		}
		if b.Width <= 0 || b.Height <= 0 {
			return fieldErr(at(cp, "bounds"), "sceneformat: camera bounds size (%v, %v) must be positive", b.Width, b.Height)
		}
	}
	return nil
}

// validateTilemap checks tm, found at path in the document and described in messages as where.
func validateTilemap(path []string, where string, tm *TilemapDoc, allowRef bool, table map[string]*TilemapDoc) error {
	if tm == nil {
		return fieldErr(path, "sceneformat: %s is nil", where)
	}
	isRef := tm.IsRef()
	isExt := tm.IsExternal()
	isInline := tm.IsInline()

	modes := 0
	if isRef {
		modes++
	}
	if isExt {
		modes++
	}
	if isInline {
		modes++
	}
	if modes == 0 {
		return fieldErr(path, "sceneformat: %s must specify ref, source or inline tile grid", where)
	}
	if modes > 1 {
		return fieldErr(path, "sceneformat: %s cannot combine ref, source and inline tile grid", where)
	}
	if isRef {
		if !allowRef {
			return fieldErr(at(path, "ref"), "sceneformat: %s cannot use ref in top-level tilemaps", where)
		}
		if _, ok := table[tm.Ref]; !ok {
			return fieldErr(at(path, "ref"), "sceneformat: %s references unknown tilemap %q", where, tm.Ref)
		}
		return nil
	}
	if isExt {
		return nil
	}
	return validateInlineTilemap(path, where, tm)
}

// validateInlineTilemap checks the inline grid tm, found at path in the document (nil when it was loaded from a source file) and described in messages as where.
func validateInlineTilemap(path []string, where string, tm *TilemapDoc) error {
	if tm.TileWidth <= 0 || tm.TileHeight <= 0 {
		field := "tileHeight"
		if tm.TileWidth <= 0 {
			field = "tileWidth"
		}
		return fieldErr(at(path, field), "sceneformat: %s tileWidth (%d) and tileHeight (%d) must be positive", where, tm.TileWidth, tm.TileHeight)
	}
	if tm.Width <= 0 || tm.Height <= 0 {
		field := "height"
		if tm.Width <= 0 {
			field = "width"
		}
		return fieldErr(at(path, field), "sceneformat: %s width (%d) and height (%d) must be positive", where, tm.Width, tm.Height)
	}
	if tm.Tileset == "" {
		return fieldErr(at(path, "tileset"), "sceneformat: %s requires a tileset name", where)
	}
	// Multiply as int64 and cap the grid before comparing lengths: a crafted width and height could otherwise overflow to a small product, pass the length check and panic the runtime builder on its tile index.
	const maxTilesPerTilemap = 1 << 24
	if int64(tm.Width)*int64(tm.Height) > maxTilesPerTilemap {
		return fieldErr(at(path, "width"), "sceneformat: %s is %dx%d, more than %d tiles", where, tm.Width, tm.Height, maxTilesPerTilemap)
	}
	want := tm.Width * tm.Height
	if len(tm.Data) != want {
		return fieldErr(at(path, "data"), "sceneformat: %s has %d tile entries, want %d (%dx%d)", where, len(tm.Data), want, tm.Width, tm.Height)
	}
	for i, gid := range tm.Data {
		if gid < 0 {
			return fieldErr(at(path, "data", strconv.Itoa(i)), "sceneformat: %s tile at index %d has negative id %d", where, i, gid)
		}
	}
	return nil
}

// validateObject checks o, found at path in the document on the layer named layer.
func validateObject(path []string, layer string, o *ObjectDoc) error {
	transform := []struct {
		field string
		v     float32
	}{
		{"x", o.X}, {"y", o.Y}, {"posZ", o.PosZ}, {"originX", o.OriginX}, {"originY", o.OriginY},
		{"rotation", o.Rotation}, {"rotX", o.RotX}, {"rotY", o.RotY}, {"rotZ", o.RotZ},
	}
	for _, f := range transform {
		if !isFinite(f.v) {
			return fieldErr(at(path, f.field), "sceneformat: layer %q object %q has non-finite transform", layer, o.ID)
		}
	}
	if !isFinite(o.Depth) || o.Depth < 0 {
		return fieldErr(at(path, "depth"), "sceneformat: layer %q object %q depth %v must be finite and non-negative", layer, o.ID, o.Depth)
	}
	if o.ScaleX != nil && !isFinite(*o.ScaleX) {
		return fieldErr(at(path, "scaleX"), "sceneformat: layer %q object %q has non-finite scaleX", layer, o.ID)
	}
	if o.ScaleY != nil && !isFinite(*o.ScaleY) {
		return fieldErr(at(path, "scaleY"), "sceneformat: layer %q object %q has non-finite scaleY", layer, o.ID)
	}
	if o.ScaleZ != nil && !isFinite(*o.ScaleZ) {
		return fieldErr(at(path, "scaleZ"), "sceneformat: layer %q object %q has non-finite scaleZ", layer, o.ID)
	}
	if o.Alpha != nil {
		if !isFinite(*o.Alpha) || *o.Alpha < 0 || *o.Alpha > 1 {
			return fieldErr(at(path, "alpha"), "sceneformat: layer %q object %q alpha %v must be in [0, 1]", layer, o.ID, *o.Alpha)
		}
	}
	if o.Sprite != nil {
		if o.Sprite.Atlas == "" || o.Sprite.Frame == "" {
			return fieldErr(at(path, "sprite"), "sceneformat: layer %q object %q sprite requires both atlas and frame", layer, o.ID)
		}
	}
	if o.Material != "" && (!fs.ValidPath(o.Material) || o.Material == ".") {
		return fieldErr(at(path, "material"), "sceneformat: layer %q object %q material %q must be a slash-separated path relative to the project root", layer, o.ID, o.Material)
	}
	return nil
}

func isFinite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
