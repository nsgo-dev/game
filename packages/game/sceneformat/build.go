package sceneformat

import (
	"cmp"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// AssetResolver resolves sprite atlases named in a SceneDoc when building a BuiltScene.
type AssetResolver interface {
	Atlas(name string) (*sprite.Atlas, error)
}

// TilemapResolver may optionally be implemented by an AssetResolver to load external tilemap sources into an inline TilemapDoc. Build uses it, when the resolver implements it, for every TilemapDoc.Source, ahead of reading the source itself through a FileResolver.
type TilemapResolver interface {
	Tilemap(source string) (*TilemapDoc, error)
}

// FileResolver may optionally be implemented by an AssetResolver to read the project's files by their slash-separated paths from the project root. Build reads through it the YAML tilesets that layered grids name (TilemapDoc.TilesetFile) and, with no TilemapResolver, the Tiled (.tmj, .tmx) and LDtk (.ldtk) maps that tilemap sources name, with the tilesets and images those refer to, so a scene uses them with no code of the game's own. FSAssets implements it.
type FileResolver interface {
	ReadFile(path string) ([]byte, error)
}

// ImageResolver may optionally be implemented by an AssetResolver to load the images that YAML tilesets and Tiled and LDtk maps draw their tiles from, given each one's slash-separated path from the project root. Without it an image is core.ImageSource{URI: path}.
type ImageResolver interface {
	Image(path string) (core.ImageSource, error)
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

	// DrawAllTiles makes Draw draw every tile of the layers' maps (BuiltLayer.Map). By default Draw culls them to the scene's view, its virtual viewport through the camera, which a view that shows more of the world, such as an editor's panned past the camera, sets it to avoid.
	DrawAllTiles bool

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
	// Tilemap is the sprite batch for the layer's tilemap when it is a single grid of atlas frames (TilemapDoc.Tileset and Data), or nil.
	Tilemap *sprite.Batch
	// Map is the tilemap built from the layer's tilemap when it is a layered grid (TilemapDoc.TileLayers) or a Tiled or LDtk source, or nil. It holds the tileset's flags, properties, shapes, animations and terrains, so in the layer's coordinates it answers FlagsAt, TileProperties, TilesInRect and Raycast and serves tileshape.New; Draw draws only its tiles inside the view, and BuiltScene.Update animates them.
	Map *tilemap.Tilemap
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

// Build validates doc, resolves its referenced atlases and tilemaps through resolver, and returns a BuiltScene ready to draw. A single grid of atlas frames becomes a sprite batch (BuiltLayer.Tilemap); a layered grid, with the YAML tileset it names read through a FileResolver, and a Tiled or LDtk source, read through a TilemapResolver when the resolver implements one and otherwise through a FileResolver, become a tilemap.Tilemap (BuiltLayer.Map). A scene that instances or inherits other scene files (SceneDoc.RefersToScenes) is first resolved with Resolve, which loads them through resolver when it implements SceneResolver.
func Build(doc *SceneDoc, resolver AssetResolver) (*BuiltScene, error) {
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

	tiles := &tilemapBuilder{doc: doc, resolver: resolver, getAtlas: getAtlas, tilesets: make(map[string]*tilemap.TilesetDoc)}

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
			if err := tiles.build(bl, ld.Tilemap); err != nil {
				return nil, err
			}
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

	return built, nil
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

// tilemapBuilder builds the tilemaps of one Build call's layers, reading each YAML tileset once.
type tilemapBuilder struct {
	doc      *SceneDoc
	resolver AssetResolver
	getAtlas func(string) (*sprite.Atlas, error)
	tilesets map[string]*tilemap.TilesetDoc
}

// build builds layer bl's tilemap tm into bl.Tilemap or bl.Map.
func (b *tilemapBuilder) build(bl *BuiltLayer, tm *TilemapDoc) error {
	resolved := tm
	if resolved.IsRef() {
		resolved = b.doc.Tilemaps[resolved.Ref]
	}
	if resolved.IsExternal() {
		where := fmt.Sprintf("layer %q tilemap source %q", bl.Name, resolved.Source)
		if tr, ok := b.resolver.(TilemapResolver); ok {
			loaded, err := tr.Tilemap(resolved.Source)
			if err != nil {
				return fmt.Errorf("sceneformat: %s: %w", where, err)
			}
			if loaded == nil {
				return fmt.Errorf("sceneformat: %s returned nil", where)
			}
			if err := validateInlineTilemap(nil, fmt.Sprintf("layer %q tilemap %q", bl.Name, resolved.Source), loaded); err != nil {
				// The loaded grid is not part of the document, so its error keeps no path into it.
				return err.(*FieldError).Err
			}
			resolved = loaded
		} else {
			m, err := b.external(where, resolved)
			if err != nil {
				return err
			}
			bl.Map = m
			return nil
		}
	}
	if resolved.IsLayered() {
		m, err := b.layered(bl.Name, resolved)
		if err != nil {
			return err
		}
		bl.Map = m
		return nil
	}
	batch, err := buildTilemapBatch(bl.Name, resolved, b.getAtlas)
	if err != nil {
		return err
	}
	bl.Tilemap = batch
	return nil
}

// readFile reads the project file at p through the resolver's FileResolver.
func (b *tilemapBuilder) readFile(where, p string) ([]byte, error) {
	fr, ok := b.resolver.(FileResolver)
	if !ok {
		return nil, fmt.Errorf("sceneformat: %s: reading %q requires a FileResolver (such as FSAssets) or, for a tilemap source, a TilemapResolver", where, p)
	}
	data, err := fr.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: %s: read %q: %w", where, p, err)
	}
	return data, nil
}

// image returns the image at the project path p through the resolver's ImageResolver, or as a URI.
func (b *tilemapBuilder) image(p string) (core.ImageSource, error) {
	if ir, ok := b.resolver.(ImageResolver); ok {
		return ir.Image(p)
	}
	return core.ImageSource{URI: p}, nil
}

// external loads the Tiled or LDtk map that tm.Source names, through the resolver's FileResolver.
func (b *tilemapBuilder) external(where string, tm *TilemapDoc) (*tilemap.Tilemap, error) {
	ext := strings.ToLower(pathExt(tm.Source))
	if ext != ".tmj" && ext != ".tmx" && ext != ".ldtk" {
		return nil, fmt.Errorf("sceneformat: %s is not a Tiled map (.tmj, .tmx) or LDtk project (.ldtk)", where)
	}
	data, err := b.readFile(where, tm.Source)
	if err != nil {
		return nil, err
	}
	dir := path.Dir(tm.Source)
	var m *tilemap.Tilemap
	if ext == ".ldtk" {
		var imgErr error
		m, err = tilemap.LoadLDtk(data, tm.Level, func(rel string) core.ImageSource {
			img, err := b.image(path.Join(dir, rel))
			if err != nil && imgErr == nil {
				imgErr = fmt.Errorf("image %q: %w", path.Join(dir, rel), err)
			}
			return img
		})
		if err == nil {
			err = imgErr
		}
	} else {
		m, err = tilemap.LoadTiled(data, tilemap.TilesetLoader{
			LoadTileset: func(source string) ([]byte, error) {
				return b.readFile(where, path.Join(dir, source))
			},
			LoadImage: func(p string) (core.ImageSource, error) {
				return b.image(path.Join(dir, p))
			},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("sceneformat: %s: %w", where, loaderr.WithFile(err, tm.Source))
	}
	return m, nil
}

// tileset returns the YAML tileset at the project path p, reading and parsing it on first use.
func (b *tilemapBuilder) tileset(where, p string) (*tilemap.TilesetDoc, error) {
	if ts, ok := b.tilesets[p]; ok {
		return ts, nil
	}
	data, err := b.readFile(where, p)
	if err != nil {
		return nil, err
	}
	ts, err := tilemap.ParseTileset(data)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: %s: %w", where, loaderr.WithFile(err, p))
	}
	b.tilesets[p] = ts
	return ts, nil
}

// layered builds the layered grid tm of layer layerName into a tilemap.Tilemap: its tileset at GID 1, then its tile layers in order.
func (b *tilemapBuilder) layered(layerName string, tm *TilemapDoc) (*tilemap.Tilemap, error) {
	where := fmt.Sprintf("layer %q tilemap", layerName)
	doc, err := b.tileset(where, tm.TilesetFile)
	if err != nil {
		return nil, err
	}
	m := tilemap.New(tm.Width, tm.Height, float32(tm.TileWidth), float32(tm.TileHeight))
	assets := tilemap.TilesetAssets{Image: b.image, Atlas: b.getAtlas}
	if _, err := m.AddTilesetDoc(doc, 1, assets); err != nil {
		return nil, fmt.Errorf("sceneformat: %s tileset %q: %w", where, tm.TilesetFile, err)
	}
	count := uint32(doc.Count())
	m.Layers = make([]tilemap.TileLayer, 0, len(tm.TileLayers))
	for i := range tm.TileLayers {
		l := &tm.TileLayers[i]
		cells, err := l.Cells()
		if err != nil {
			return nil, fmt.Errorf("sceneformat: %s: %w", where, err)
		}
		for j, cell := range cells {
			if id := cell & tilemap.TileIDMask; id > count {
				return nil, fmt.Errorf("sceneformat: %s tile layer %q row %d column %d places tile %d, but tileset %q has %d tiles", where, l.Name, j/tm.Width, j%tm.Width, id-1, tm.TilesetFile, count)
			}
		}
		tl := tilemap.NewTileLayer(l.Name, tm.Width, tm.Height, cells)
		tl.Visible = l.IsVisible()
		tl.Opacity = l.EffectiveOpacity()
		m.Layers = append(m.Layers, tl)
	}
	return m, nil
}

func buildTilemapBatch(layerName string, resolved *TilemapDoc, getAtlas func(string) (*sprite.Atlas, error)) (*sprite.Batch, error) {
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

// Sync recomputes the object's World and WorldAlpha from its document and its parent's, copies its transform, sprite frame (when found in Atlas), origin, flip and alpha onto its sprite (times its material's opacity, as Build does), and does the same for its descendants, so an edit to the document fields of a built object, such as one an animation makes, shows on the next Draw. It reports false, and leaves the sprite's transform as it was, for a sprite whose composed transform is sheared, which a sprite cannot draw.
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
		if od.Sprite != nil && bo.Atlas != nil {
			if frame, exists := bo.Atlas.Frames[od.Sprite.Frame]; exists {
				sp.Frame = frame
			}
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

// Draw draws the scene into d: it syncs materials (SyncMaterials), then draws the background fill over the virtual viewport (when configured), then each visible layer in order, its tilemap beneath its objects. World-space layers draw under the camera transform; screen-space layers draw in canvas coordinates. A layer's map (BuiltLayer.Map) draws only the tiles inside the virtual viewport, seen through the camera for a world-space layer, unless the scene has no virtual size or DrawAllTiles is set. Draw leaves d's transform and paint state as it found it.
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
		if layer.Tilemap == nil && layer.Map == nil && len(layer.Batches) == 0 {
			continue
		}
		view := s.tileView(layer.Space, vw, vh)
		if layer.Space == SpaceWorld && s.Camera != nil {
			s.drawWithCamera(d, vw, vh, layer, view)
			continue
		}
		drawLayer(d, layer, view)
	}
}

// tileView returns the camera that culls the tiles of a layer in space to what the scene shows, its virtual viewport of vw by vh points seen through the scene camera for a world-space layer, or nil to draw every tile when the scene has no virtual size or DrawAllTiles is set.
func (s *BuiltScene) tileView(space Space, vw, vh float32) *tilemap.Camera {
	if s.DrawAllTiles || vw <= 0 || vh <= 0 {
		return nil
	}
	if space == SpaceWorld && s.Camera != nil {
		cam := s.Camera
		zoom := cam.EffectiveZoom()
		x, y := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
		return &tilemap.Camera{X: x, Y: y, Zoom: zoom, Rotation: cam.Rotation, Width: vw, Height: vh}
	}
	return &tilemap.Camera{X: vw / 2, Y: vh / 2, Zoom: 1, Width: vw, Height: vh}
}

// Update advances the animated tiles of every layer's map (BuiltLayer.Map) by dt, so the next Draw shows their current frames.
func (s *BuiltScene) Update(dt time.Duration) {
	if s == nil {
		return
	}
	for _, l := range s.Layers {
		if l.Map != nil {
			l.Map.Update(dt)
		}
	}
}

func (s *BuiltScene) drawWithCamera(d *core.Drawing, vw, vh float32, layer *BuiltLayer, view *tilemap.Camera) {
	cam := s.Camera
	zoom := cam.EffectiveZoom()
	cx, cy := vw/2, vh/2
	camX, camY := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)

	// Identity camera needs no Save/Restore wrapper.
	if zoom == 1 && cam.Rotation == 0 && cx-camX == 0 && cy-camY == 0 {
		drawLayer(d, layer, view)
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
	drawLayer(d, layer, view)
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

func drawLayer(d *core.Drawing, layer *BuiltLayer, view *tilemap.Camera) {
	if layer.Tilemap != nil {
		layer.Tilemap.Draw(d)
	}
	if layer.Map != nil {
		layer.Map.Draw(d, view)
	}
	for _, b := range layer.Batches {
		b.Draw(d)
	}
}
