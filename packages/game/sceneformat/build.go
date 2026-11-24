package sceneformat

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// AssetResolver resolves sprite atlases named in a SceneDoc when building a BuiltScene.
type AssetResolver interface {
	Atlas(name string) (*sprite.Atlas, error)
}

// TilemapResolver may optionally be implemented by an AssetResolver to load external tilemap sources (such as Tiled .tmj files) into an inline TilemapDoc.
type TilemapResolver interface {
	Tilemap(source string) (*TilemapDoc, error)
}

// MaterialResolver may optionally be implemented by an AssetResolver to load the shared material files objects name in ObjectDoc.Material. A resolver that returns the same pointer for the same path, as resource.Library does, lets every scene using one file see its edits.
type MaterialResolver interface {
	Material(path string) (*resource.Material, error)
}

// AtlasMap is an AssetResolver backed by a map of atlas names to atlases.
type AtlasMap map[string]*sprite.Atlas

// Atlas returns the atlas named name or an error when it is absent or nil.
func (m AtlasMap) Atlas(name string) (*sprite.Atlas, error) {
	a, ok := m[name]
	if !ok || a == nil {
		return nil, fmt.Errorf("atlas %q not found", name)
	}
	return a, nil
}

// BuiltScene is a SceneDoc resolved against atlases and ready to draw into a core.Drawing.
type BuiltScene struct {
	// Doc is the validated SceneDoc this scene was built from: the one passed to Build, or for a scene that instances or inherits other scene files, the self-contained copy Resolve made of it, whose objects the BuiltObjects point into.
	Doc *SceneDoc
	// BackgroundColor is the parsed background color; HasBackground reports whether Draw paints it.
	BackgroundColor core.Color
	HasBackground   bool
	// Camera is the active camera transform for world-space layers; nil leaves world space untransformed.
	Camera *CameraDoc
	// Layers are the scene's layers in draw order (lowest Order first, ties in document order).
	Layers []*BuiltLayer
	// Objects indexes every built object in the scene, children included, by its unique ID.
	Objects map[string]*BuiltObject
	// Events is the EventBus wired from Doc.Connections when built with BuildWith and BuildOptions.Events (or via BindEvents), or nil otherwise.
	Events *EventBus

	// materialed are the sprite objects with a material, in build order, which SyncMaterials updates.
	materialed []*BuiltObject
}

// BuiltLayer is one resolved layer of a BuiltScene.
type BuiltLayer struct {
	// Doc is the source layer definition.
	Doc *LayerDoc
	// Name is the layer's unique name.
	Name string
	// Space is SpaceWorld or SpaceScreen.
	Space Space
	// Order is the layer's draw order.
	Order int
	// Visible controls whether Draw renders this layer.
	Visible bool
	// Tilemap is the sprite batch for the layer's tilemap, or nil if the layer has none.
	Tilemap *sprite.Batch
	// Batches are the sprite batches for the layer's sprite objects, grouped in exact Z order.
	Batches []*sprite.Batch
	// Objects are the layer's top-level built objects in document order; each one's descendants hang from BuiltObject.Children.
	Objects []*BuiltObject
}

// BuiltObject is one object instantiated from an ObjectDoc.
type BuiltObject struct {
	// Doc is the source object definition.
	Doc *ObjectDoc
	// ID is the object's unique scene ID.
	ID string
	// Layer is the name of the layer containing this object.
	Layer string
	// Atlas is the resolved atlas for Sprite, or nil when the object has no sprite.
	Atlas *sprite.Atlas
	// Sprite is the live sprite drawn by its layer's batch, or nil when the object has no sprite. Its X, Y, Rotation, ScaleX, ScaleY and Alpha are the object's world values, composed through its ancestors.
	Sprite *sprite.Sprite
	// Material is the shared material resolved from Doc.Material, or nil when the object names none. It is the resolver's value, not a copy, so an edit reloaded into it reaches every scene that uses it.
	Material *resource.Material
	// Parent is the object this one is a child of, or nil for a top-level object.
	Parent *BuiltObject
	// Children are the object's built children in document order.
	Children []*BuiltObject
	// World is the object's transform composed with its ancestors', mapping its local space to its layer's space (World = Parent.World * local).
	World math2d.Affine
	// WorldAlpha is the object's alpha multiplied by every ancestor's.
	WorldAlpha float32
}

// BuildOptions configures BuildWith.
type BuildOptions struct {
	// Events, when non-nil, validates the resolved scene's connections against it and binds them into BuiltScene.Events, linked to the built objects.
	Events *EventRegistry
}

// Build validates doc, resolves its referenced atlases and tilemaps through resolver, and returns a BuiltScene ready to draw. A scene that instances or inherits other scene files (SceneDoc.RefersToScenes) is first resolved with Resolve, which loads them through resolver when it implements SceneResolver.
func Build(doc *SceneDoc, resolver AssetResolver) (*BuiltScene, error) {
	return BuildWith(doc, resolver, BuildOptions{})
}

// BuildWith is Build with BuildOptions, such as binding declared event connections into BuiltScene.Events.
func BuildWith(doc *SceneDoc, resolver AssetResolver, opts BuildOptions) (*BuiltScene, error) {
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
	if opts.Events != nil {
		if err := ValidateConnections(doc, opts.Events); err != nil {
			return nil, err
		}
	}

	built := &BuiltScene{
		Doc:     doc,
		Objects: make(map[string]*BuiltObject),
	}
	if doc.BackgroundColor != "" {
		c, err := core.ParseColor(doc.BackgroundColor)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: backgroundColor %q: %w", doc.BackgroundColor, err)
		}
		built.BackgroundColor = c
		built.HasBackground = true
	}
	if doc.Camera != nil {
		cam := *doc.Camera
		if doc.Camera.Zoom != nil {
			z := *doc.Camera.Zoom
			cam.Zoom = &z
		}
		if doc.Camera.Bounds != nil {
			b := *doc.Camera.Bounds
			cam.Bounds = &b
		}
		built.Camera = &cam
	}

	atlases := make(map[string]*sprite.Atlas)
	getAtlas := func(name string) (*sprite.Atlas, error) {
		if a, ok := atlases[name]; ok {
			return a, nil
		}
		if resolver == nil {
			return nil, fmt.Errorf("sceneformat: cannot resolve atlas %q with nil resolver", name)
		}
		a, err := resolver.Atlas(name)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: resolve atlas %q: %w", name, err)
		}
		if a == nil {
			return nil, fmt.Errorf("sceneformat: resolver returned nil atlas for %q", name)
		}
		atlases[name] = a
		return a, nil
	}

	materials := make(map[string]*resource.Material)
	getMaterial := func(path string) (*resource.Material, error) {
		if m, ok := materials[path]; ok {
			return m, nil
		}
		mr, ok := resolver.(MaterialResolver)
		if !ok {
			return nil, fmt.Errorf("sceneformat: material %q requires a MaterialResolver", path)
		}
		m, err := mr.Material(path)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: resolve material %q: %w", path, err)
		}
		if m == nil {
			return nil, fmt.Errorf("sceneformat: resolver returned nil material for %q", path)
		}
		materials[path] = m
		return m, nil
	}

	// Sort layer indices stably by Order so doc.Layers is never mutated.
	order := make([]int, len(doc.Layers))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int {
		return cmp.Compare(doc.Layers[i].Order, doc.Layers[j].Order)
	})

	built.Layers = make([]*BuiltLayer, 0, len(doc.Layers))
	for _, idx := range order {
		ld := &doc.Layers[idx]
		bl := &BuiltLayer{
			Doc:     ld,
			Name:    ld.Name,
			Space:   ld.EffectiveSpace(),
			Order:   ld.Order,
			Visible: ld.IsVisible(),
			Objects: make([]*BuiltObject, 0, len(ld.Objects)),
		}

		if ld.Tilemap != nil {
			batch, err := buildTilemapBatch(doc, ld.Name, ld.Tilemap, resolver, getAtlas)
			if err != nil {
				return nil, err
			}
			bl.Tilemap = batch
		}

		var drawables []*BuiltObject
		for j := range ld.Objects {
			bo, err := buildObject(built, bl, nil, &ld.Objects[j], getAtlas, getMaterial, &drawables)
			if err != nil {
				return nil, err
			}
			bl.Objects = append(bl.Objects, bo)
		}

		bl.Batches = groupSpriteBatches(drawables)
		built.Layers = append(built.Layers, bl)
	}

	if opts.Events != nil {
		if _, err := built.BindEvents(opts.Events); err != nil {
			return nil, err
		}
	}

	return built, nil
}

// BindEvents validates s.Doc's connections against reg, wires them into an EventBus whose EventCall values carry s, SourceObject and TargetObject, stores the bus on s.Events, and returns it.
func (s *BuiltScene) BindEvents(reg *EventRegistry) (*EventBus, error) {
	if s == nil {
		return nil, fmt.Errorf("sceneformat: nil built scene")
	}
	bus, err := reg.bind(s.Doc, nil, nil, s)
	if err != nil {
		return nil, err
	}
	s.Events = bus
	return bus, nil
}

// buildObject builds od under parent (nil for a top-level object), composing its world transform, then builds its children. It records every built object in built.Objects and appends the ones with a sprite to drawables in depth-first order, each parent before its children.
func buildObject(built *BuiltScene, bl *BuiltLayer, parent *BuiltObject, od *ObjectDoc, getAtlas func(string) (*sprite.Atlas, error), getMaterial func(string) (*resource.Material, error), drawables *[]*BuiltObject) (*BuiltObject, error) {
	bo := &BuiltObject{
		Doc:        od,
		ID:         od.ID,
		Layer:      bl.Name,
		Parent:     parent,
		World:      od.LocalTransform(),
		WorldAlpha: od.EffectiveAlpha(),
	}
	if parent != nil {
		bo.World = parent.World.Mul(bo.World)
		bo.WorldAlpha *= parent.WorldAlpha
	}
	if od.Material != "" {
		mat, err := getMaterial(od.Material)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, od.ID, err)
		}
		bo.Material = mat
	}
	if od.Sprite != nil {
		atlas, err := getAtlas(od.Sprite.Atlas)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, od.ID, err)
		}
		frame, ok := atlas.Frames[od.Sprite.Frame]
		if !ok {
			return nil, fmt.Errorf("sceneformat: layer %q object %q: atlas %q has no frame %q", bl.Name, od.ID, od.Sprite.Atlas, od.Sprite.Frame)
		}
		sp := sprite.NewSprite(frame)
		if parent == nil {
			// A top-level object draws with its own fields exactly, without a round trip through a matrix.
			sp.X, sp.Y = od.X, od.Y
			sp.Rotation = od.Rotation
			sp.ScaleX, sp.ScaleY = od.EffectiveScaleX(), od.EffectiveScaleY()
		} else {
			x, y, rot, sx, sy, ok := decomposeTRS(bo.World)
			if !ok {
				return nil, fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, od.ID, errShearedSprite)
			}
			sp.X, sp.Y, sp.Rotation, sp.ScaleX, sp.ScaleY = x, y, rot, sx, sy
		}
		sp.OriginX = od.OriginX
		sp.OriginY = od.OriginY
		sp.FlipX = od.FlipX
		sp.FlipY = od.FlipY
		sp.Alpha = bo.WorldAlpha
		sp.Z = od.Z
		bo.Atlas = atlas
		bo.Sprite = &sp
		if bo.Material != nil {
			bo.syncMaterial()
			built.materialed = append(built.materialed, bo)
		}
		*drawables = append(*drawables, bo)
	}
	built.Objects[od.ID] = bo
	for i := range od.Children {
		child, err := buildObject(built, bl, bo, &od.Children[i], getAtlas, getMaterial, drawables)
		if err != nil {
			return nil, err
		}
		bo.Children = append(bo.Children, child)
	}
	return bo, nil
}

// decomposeTRS splits m into a translation, a rotation and axis scales that sprite.Sprite can draw, so that TRS(x, y, rot, sx, sy) equals m. When m's columns are not orthogonal but are parallel, m is rank-deficient (a sprite under a parent collapsed to zero size along one axis, such as scaleX: 0) and covers no area, so it comes back with zero scales and draws nothing. decomposeTRS reports false when m's columns are neither orthogonal nor parallel, a shear no such split can carry.
func decomposeTRS(m math2d.Affine) (x, y, rot, sx, sy float32, ok bool) {
	a, b, c, d := float64(m.A), float64(m.B), float64(m.C), float64(m.D)
	scaleX := math.Hypot(a, b)
	scaleY := math.Hypot(c, d)
	if math.Abs(a*c+b*d) > 1e-4*scaleX*scaleY {
		if math.Abs(a*d-b*c) > 1e-4*scaleX*scaleY {
			return 0, 0, 0, 0, 0, false
		}
		return m.Tx, m.Ty, 0, 0, 0, true
	}
	if a*d-b*c < 0 {
		scaleY = -scaleY
	}
	switch {
	case scaleX > 1e-12:
		rot = float32(math.Atan2(b, a))
	case scaleY != 0:
		rot = float32(math.Atan2(-c/scaleY, d/scaleY))
	}
	return m.Tx, m.Ty, rot, float32(scaleX), float32(scaleY), true
}

func buildTilemapBatch(doc *SceneDoc, layerName string, tm *TilemapDoc, resolver AssetResolver, getAtlas func(string) (*sprite.Atlas, error)) (*sprite.Batch, error) {
	resolved := tm
	if resolved.IsRef() {
		resolved = doc.Tilemaps[resolved.Ref]
	}
	if resolved.IsExternal() {
		tr, ok := resolver.(TilemapResolver)
		if !ok {
			return nil, fmt.Errorf("sceneformat: layer %q tilemap source %q requires a TilemapResolver", layerName, resolved.Source)
		}
		loaded, err := tr.Tilemap(resolved.Source)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: layer %q tilemap source %q: %w", layerName, resolved.Source, err)
		}
		if loaded == nil {
			return nil, fmt.Errorf("sceneformat: layer %q tilemap source %q returned nil", layerName, resolved.Source)
		}
		if err := validateInlineTilemap(nil, fmt.Sprintf("layer %q tilemap %q", layerName, resolved.Source), loaded); err != nil {
			// The loaded grid is not part of the document, so its error keeps no path into it.
			return nil, err.(*FieldError).Err
		}
		resolved = loaded
	}

	atlas, err := getAtlas(resolved.Tileset)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: layer %q tilemap: %w", layerName, err)
	}
	sprites := make([]*sprite.Sprite, 0, len(resolved.Data))
	for row := 0; row < resolved.Height; row++ {
		for col := 0; col < resolved.Width; col++ {
			gid := resolved.Data[row*resolved.Width+col]
			if gid == 0 {
				continue
			}
			key := strconv.Itoa(gid)
			frame, ok := atlas.Frames[key]
			if !ok {
				return nil, fmt.Errorf("sceneformat: layer %q tilemap tileset %q has no frame %q", layerName, resolved.Tileset, key)
			}
			sp := sprite.NewSprite(frame)
			sp.X = float32(col * resolved.TileWidth)
			sp.Y = float32(row * resolved.TileHeight)
			sprites = append(sprites, &sp)
		}
	}
	return &sprite.Batch{Atlas: atlas, Sprites: sprites}, nil
}

// groupSpriteBatches sorts drawables stably by Z and groups consecutive sprites sharing an atlas into sprite.Batch values, preserving exact Z order even across multiple atlases.
func groupSpriteBatches(drawables []*BuiltObject) []*sprite.Batch {
	if len(drawables) == 0 {
		return nil
	}
	slices.SortStableFunc(drawables, func(a, b *BuiltObject) int {
		return cmp.Compare(a.Sprite.Z, b.Sprite.Z)
	})
	var batches []*sprite.Batch
	for _, bo := range drawables {
		if n := len(batches); n > 0 && batches[n-1].Atlas == bo.Atlas {
			batches[n-1].Sprites = append(batches[n-1].Sprites, bo.Sprite)
			continue
		}
		batches = append(batches, &sprite.Batch{
			Atlas:   bo.Atlas,
			Sprites: []*sprite.Sprite{bo.Sprite},
		})
	}
	return batches
}

// syncMaterial sets the object's sprite to draw with its material: the object's world alpha (its alpha times every ancestor's) times the material's opacity, and the material's blend mode.
func (bo *BuiltObject) syncMaterial() {
	if bo.Sprite == nil || bo.Material == nil {
		return
	}
	bo.Sprite.Alpha = bo.WorldAlpha * bo.Material.EffectiveOpacity()
	bo.Sprite.Blend = bo.Material.BlendOp()
}

// Sync recomputes the object's World and WorldAlpha from its document and its parent's, copies its transform, origin, flip and alpha onto its sprite (times its material's opacity, as Build does), and does the same for its descendants, so an edit to the document fields of a built object, such as one an animation makes, shows on the next Draw. It reports false, and leaves the sprite's transform as it was, for a sprite whose composed transform is sheared, which a sprite cannot draw.
func (bo *BuiltObject) Sync() bool {
	od := bo.Doc
	bo.World = od.LocalTransform()
	bo.WorldAlpha = od.EffectiveAlpha()
	if bo.Parent != nil {
		bo.World = bo.Parent.World.Mul(bo.World)
		bo.WorldAlpha *= bo.Parent.WorldAlpha
	}
	ok := true
	if sp := bo.Sprite; sp != nil {
		if bo.Parent == nil {
			sp.X, sp.Y = od.X, od.Y
			sp.Rotation = od.Rotation
			sp.ScaleX, sp.ScaleY = od.EffectiveScaleX(), od.EffectiveScaleY()
		} else if x, y, rot, sx, sy, drawable := decomposeTRS(bo.World); drawable {
			sp.X, sp.Y, sp.Rotation, sp.ScaleX, sp.ScaleY = x, y, rot, sx, sy
		} else {
			ok = false
		}
		sp.OriginX, sp.OriginY = od.OriginX, od.OriginY
		sp.FlipX, sp.FlipY = od.FlipX, od.FlipY
		sp.Alpha = bo.WorldAlpha
		bo.syncMaterial()
	}
	for _, c := range bo.Children {
		if !c.Sync() {
			ok = false
		}
	}
	return ok
}

// SyncMaterials copies the current state of every object's material onto its sprite, so a material reloaded in place (resource.Library.Reload) shows on the next draw. Draw calls it first; call it directly to read the sprites without drawing.
func (s *BuiltScene) SyncMaterials() {
	if s == nil {
		return
	}
	for _, bo := range s.materialed {
		bo.syncMaterial()
	}
}

// Layer returns the built layer named name, or nil if none exists.
func (s *BuiltScene) Layer(name string) *BuiltLayer {
	if s == nil {
		return nil
	}
	for _, l := range s.Layers {
		if l.Name == name {
			return l
		}
	}
	return nil
}

// Object returns the built object with ID id, or nil if none exists.
func (s *BuiltScene) Object(id string) *BuiltObject {
	if s == nil {
		return nil
	}
	return s.Objects[id]
}

// Draw draws the scene into d: it syncs materials (SyncMaterials), then draws the background fill over the virtual viewport (when configured), then each visible layer in order. World-space layers draw under the camera transform; screen-space layers draw in canvas coordinates. Draw leaves d's transform and paint state as it found it.
func (s *BuiltScene) Draw(d *core.Drawing) {
	if s == nil || d == nil {
		return
	}
	s.SyncMaterials()
	var vw, vh float32
	if s.Doc != nil {
		vw, vh = s.Doc.VirtualWidth, s.Doc.VirtualHeight
	}
	if s.HasBackground && vw > 0 && vh > 0 {
		d.Save().SetFillColor(s.BackgroundColor).FillRect(0, 0, vw, vh).Restore()
	}

	for _, layer := range s.Layers {
		if !layer.Visible {
			continue
		}
		if layer.Tilemap == nil && len(layer.Batches) == 0 {
			continue
		}
		if layer.Space == SpaceWorld && s.Camera != nil {
			s.drawWithCamera(d, vw, vh, layer)
			continue
		}
		drawLayer(d, layer)
	}
}

func (s *BuiltScene) drawWithCamera(d *core.Drawing, vw, vh float32, layer *BuiltLayer) {
	cam := s.Camera
	zoom := cam.EffectiveZoom()
	cx, cy := vw/2, vh/2
	camX, camY := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)

	// Identity camera needs no Save/Restore wrapper.
	if zoom == 1 && cam.Rotation == 0 && cx-camX == 0 && cy-camY == 0 {
		drawLayer(d, layer)
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
	drawLayer(d, layer)
	d.Restore()
}

func clampCamera(x, y, vw, vh, zoom float32, b *BoundsDoc) (float32, float32) {
	if b == nil || zoom <= 0 {
		return x, y
	}
	halfW := (vw / 2) / zoom
	halfH := (vh / 2) / zoom

	minX, maxX := b.X+halfW, b.X+b.Width-halfW
	if minX > maxX {
		x = b.X + b.Width/2
	} else {
		x = min(max(x, minX), maxX)
	}

	minY, maxY := b.Y+halfH, b.Y+b.Height-halfH
	if minY > maxY {
		y = b.Y + b.Height/2
	} else {
		y = min(max(y, minY), maxY)
	}
	return x, y
}

func drawLayer(d *core.Drawing, layer *BuiltLayer) {
	if layer.Tilemap != nil {
		layer.Tilemap.Draw(d)
	}
	for _, b := range layer.Batches {
		b.Draw(d)
	}
}
