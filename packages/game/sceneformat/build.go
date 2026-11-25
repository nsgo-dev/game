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

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/lights2d"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/navbake"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/skeletal"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// AssetResolver resolves sprite atlases named in a SceneDoc when building a BuiltScene.
type AssetResolver interface {
	Atlas(name string) (*sprite.Atlas, error)
}

// TilemapResolver may optionally be implemented by an AssetResolver to load external tilemap sources into an inline TilemapDoc. Build uses it, when the resolver implements it, for every TilemapDoc.Source that names no Level, ahead of reading the source itself through a FileResolver. A source with a Level (an LDtk level) is always read through a FileResolver, since Tilemap has no way to receive the level.
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

// ParticlesResolver may optionally be implemented by an AssetResolver to load particle emitter definitions named by ObjectDoc.Particles. When not implemented, Build reads the file through FileResolver (such as FSAssets) and decodes it with particles.UnmarshalConfigYAML.
type ParticlesResolver interface {
	Particles(path string) (particles.Config, error)
}

// LDtkOptionsResolver may optionally be implemented by an AssetResolver to provide tilemap.LDtkOptions
// (such as IntGrid-to-TileFlags mappings from project settings) when Build loads an LDtk (.ldtk) map.
type LDtkOptionsResolver interface {
	LDtkOptions() tilemap.LDtkOptions
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
	// AmbientColor is the parsed 2D lighting ambient color; HasAmbient reports whether the scene sets one.
	AmbientColor core.Color
	HasAmbient   bool
	// Lights2D is the 2D lighting scene built from SceneDoc.AmbientColor, built-in light2d and occluder2d components, and tilemap collision shapes, or nil when the scene defines none.
	Lights2D *lights2d.Scene
	// SkipLights2D disables drawing Lights2D inside Draw when true (for example when an editor toggles lighting preview off).
	SkipLights2D bool
	// Lights2DBounds optionally overrides the world-space bounds passed to Lights2D.Draw inside Draw. Nil uses the scene's virtual viewport through Camera (or the union of light/occluder extents when the virtual size is 0).
	Lights2DBounds *core.Bounds
	// NavBake2D is the baked 2D navigation mesh produced from built-in navregion2d components (and optional tilemap collision subtraction), or nil when the scene defines none.
	NavBake2D *navbake.Mesh
	// NavMesh2D is the runtime 2D ai.NavMesh constructed from NavBake2D (including any authored 2D nav_link components), or nil when the scene defines no navregion2d components.
	NavMesh2D *ai.NavMesh
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
	// ParallaxX and ParallaxY are the layer's horizontal and vertical parallax scroll factors, defaulting to 1.
	ParallaxX float32
	ParallaxY float32
	// RepeatX and RepeatY report whether a world-space layer tiles horizontally or vertically across the camera's visible region.
	RepeatX bool
	RepeatY bool
	// RepeatWidth and RepeatHeight are the horizontal and vertical repeat periods in layer points; when RepeatX or RepeatY is true and the document leaves a period 0, Build fills it from the layer's tilemap pixel size or the scene's virtual size.
	RepeatWidth  float32
	RepeatHeight float32
	// Tilemap is the sprite batch for the layer's tilemap when it is a single grid of atlas frames (TilemapDoc.Tileset and Data), or nil.
	Tilemap *sprite.Batch
	// Map is the tilemap built from the layer's tilemap when it is a layered grid (TilemapDoc.TileLayers) or a Tiled or LDtk source, or nil. It holds the tileset's flags, properties, shapes, animations and terrains, so in the layer's coordinates it answers FlagsAt, TileProperties, TilesInRect and Raycast and serves tileshape.New; Draw draws only its tiles inside the view, and BuiltScene.Update animates them.
	Map *tilemap.Tilemap
	// Batches are the sprite batches for the layer's sprite objects, grouped in exact Z order.
	Batches []*sprite.Batch
	// Emitters are the layer's built objects that carry a live 2D particle emitter (ObjectDoc.Particles), in document order.
	Emitters []*BuiltObject
	// Skeletons are the layer's built objects that carry a live 2D skeletal rig (BuiltObject.Skeleton), in Z order.
	Skeletons []*BuiltObject
	// Objects are the layer's top-level built objects in document order; each one's descendants hang from BuiltObject.Children.
	Objects []*BuiltObject

	// drawItems interleaves sprite batches, particle emitters, and skeletal rigs in exact ascending Z order.
	drawItems []layerDrawItem
}

type layerDrawItem struct {
	batch   *sprite.Batch
	emitter *BuiltObject
	skel    *BuiltObject
}

// BuiltObject is one object instantiated from an ObjectDoc.
type BuiltObject struct {
	// Doc is the source object definition.
	Doc *ObjectDoc
	// ID is the object's unique scene ID.
	ID string
	// Layer is the name of the layer containing this object.
	Layer string
	// Atlas is the resolved atlas for Sprite or Skeleton, or nil when the object has neither.
	Atlas *sprite.Atlas
	// Sprite is the live sprite drawn by its layer's batch, or nil when the object has no sprite. Its X, Y, Rotation, ScaleX, ScaleY and Alpha are the object's world values, composed through its ancestors.
	Sprite *sprite.Sprite
	// Skeleton is the live 2D skeletal rig built from the object's "skeletal" component, or nil when none is configured. Its root transform, flips, alpha and Z track the object's world values and BuiltScene.Update advances its playback.
	Skeleton *skeletal.Skeleton
	// Material is the shared material resolved from Doc.Material, or nil when the object names none. It is the resolver's value, not a copy, so an edit reloaded into it reaches every scene that uses it.
	Material *resource.Material
	// Emitter is the live 2D particle emitter built from Doc.Particles, or nil when the object has none. BuiltScene.Update steps it and BuiltScene.Draw draws it at the object's world transform.
	Emitter *particles.Emitter
	// Parent is the object this one is a child of, or nil for a top-level object.
	Parent *BuiltObject
	// Children are the object's built children in document order.
	Children []*BuiltObject
	// World is the object's transform composed with its ancestors', mapping its local space to its layer's space (World = Parent.World * local).
	World math2d.Affine
	// WorldAlpha is the object's alpha multiplied by every ancestor's.
	WorldAlpha float32

	emitterBaseRot float32
	emitterZ       int
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
	if doc.AmbientColor != "" {
		c, err := core.ParseColor(doc.AmbientColor)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: ambientColor %q: %w", doc.AmbientColor, err)
		}
		built.AmbientColor = c
		built.HasAmbient = true
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

	particleConfigs := make(map[string]particles.Config)
	getParticles := func(file string) (particles.Config, error) {
		if cfg, ok := particleConfigs[file]; ok {
			return cfg, nil
		}
		if pr, ok := resolver.(ParticlesResolver); ok {
			cfg, err := pr.Particles(file)
			if err != nil {
				return particles.Config{}, fmt.Errorf("sceneformat: resolve particles %q: %w", file, err)
			}
			particleConfigs[file] = cfg
			return cfg, nil
		}
		fr, ok := resolver.(FileResolver)
		if !ok {
			return particles.Config{}, fmt.Errorf("sceneformat: particles %q requires a ParticlesResolver or FileResolver (such as FSAssets)", file)
		}
		data, err := fr.ReadFile(file)
		if err != nil {
			return particles.Config{}, fmt.Errorf("sceneformat: read particles %q: %w", file, err)
		}
		cfg, err := particles.UnmarshalConfigYAML(data)
		if err != nil {
			return particles.Config{}, fmt.Errorf("sceneformat: parse particles %q: %w", file, loaderr.WithFile(err, file))
		}
		particleConfigs[file] = cfg
		return cfg, nil
	}

	getSkeleton := makeSkeletonLoader(resolver)
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
			Doc:          ld,
			Name:         ld.Name,
			Space:        ld.EffectiveSpace(),
			Order:        ld.Order,
			Visible:      ld.IsVisible(),
			ParallaxX:    ld.EffectiveParallaxX(),
			ParallaxY:    ld.EffectiveParallaxY(),
			RepeatX:      ld.RepeatX,
			RepeatY:      ld.RepeatY,
			RepeatWidth:  ld.RepeatWidth,
			RepeatHeight: ld.RepeatHeight,
			Objects:      make([]*BuiltObject, 0, len(ld.Objects)),
		}

		if ld.Tilemap != nil {
			tmW, tmH, err := tiles.build(bl, ld.Tilemap)
			if err != nil {
				return nil, err
			}
			if bl.RepeatX && bl.RepeatWidth == 0 {
				bl.RepeatWidth = tmW
			}
			if bl.RepeatY && bl.RepeatHeight == 0 {
				bl.RepeatHeight = tmH
			}
		}
		if bl.RepeatX && bl.RepeatWidth == 0 {
			bl.RepeatWidth = doc.VirtualWidth
		}
		if bl.RepeatY && bl.RepeatHeight == 0 {
			bl.RepeatHeight = doc.VirtualHeight
		}

		var drawables []layerDrawable
		for j := range ld.Objects {
			bo, err := buildObject(built, bl, nil, &ld.Objects[j], resolver, getAtlas, getMaterial, getParticles, getSkeleton, &drawables)
			if err != nil {
				return nil, err
			}
			bl.Objects = append(bl.Objects, bo)
		}

		bl.Batches, bl.Skeletons, bl.drawItems = groupLayerDrawables(drawables)
		built.Layers = append(built.Layers, bl)
	}

	lights, err := BuildLights2D(built)
	if err != nil {
		return nil, err
	}
	built.Lights2D = lights

	navBake, navMesh, err := BuildNavMesh2D(built)
	if err != nil {
		return nil, err
	}
	built.NavBake2D = navBake
	built.NavMesh2D = navMesh

	return built, nil
}

type layerDrawable struct {
	z       int
	sprite  *BuiltObject
	emitter *BuiltObject
	skel    *BuiltObject
}

// buildObject builds od under parent (nil for a top-level object), composing its world transform, then builds its children. It records every built object in built.Objects and appends the ones with a sprite, particle emitter, or skeletal rig to drawables in depth-first order, each parent before its children.
func buildObject(built *BuiltScene, bl *BuiltLayer, parent *BuiltObject, od *ObjectDoc, resolver AssetResolver, getAtlas func(string) (*sprite.Atlas, error), getMaterial func(string) (*resource.Material, error), getParticles func(string) (particles.Config, error), getSkeleton func(string) (*skeletal.SkeletonData, error), drawables *[]layerDrawable) (*BuiltObject, error) {
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
		*drawables = append(*drawables, layerDrawable{z: sp.Z, sprite: bo})
	}
	if od.Particles != nil {
		cfg, err := getParticles(od.Particles.File)
		if err != nil {
			return nil, fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, od.ID, err)
		}
		em := &particles.Emitter{}
		if od.Particles.Atlas != "" {
			atlas, err := getAtlas(od.Particles.Atlas)
			if err != nil {
				return nil, fmt.Errorf("sceneformat: layer %q object %q: particles atlas: %w", bl.Name, od.ID, err)
			}
			if err := checkParticleFrames(atlas, od.Particles.Atlas, cfg); err != nil {
				return nil, fmt.Errorf("sceneformat: layer %q object %q: %w", bl.Name, od.ID, err)
			}
			em.Atlas = atlas
		}
		em.ReloadConfig(cfg)
		wx, wy, wrot := emitterWorldTransform(parent, od, bo.World)
		em.X, em.Y = wx, wy
		em.Rotation = wrot + cfg.Rotation
		if od.Z != 0 {
			em.Z = od.Z
		}
		bo.Emitter = em
		bo.emitterBaseRot = cfg.Rotation
		bo.emitterZ = em.Z
		bl.Emitters = append(bl.Emitters, bo)
		*drawables = append(*drawables, layerDrawable{z: bo.emitterZ, emitter: bo})
	}
	if err := buildObjectSkeleton(bl, bo, resolver, getAtlas, getSkeleton); err != nil {
		return nil, err
	}
	if bo.Skeleton != nil {
		*drawables = append(*drawables, layerDrawable{z: od.Z, skel: bo})
	}
	built.Objects[od.ID] = bo
	for i := range od.Children {
		child, err := buildObject(built, bl, bo, &od.Children[i], resolver, getAtlas, getMaterial, getParticles, getSkeleton, drawables)
		if err != nil {
			return nil, err
		}
		bo.Children = append(bo.Children, child)
	}
	return bo, nil
}

func checkParticleFrames(atlas *sprite.Atlas, atlasName string, cfg particles.Config) error {
	for _, fn := range cfg.FrameNames {
		if _, ok := atlas.Frames[fn]; !ok {
			return fmt.Errorf("particles atlas %q has no frame %q", atlasName, fn)
		}
	}
	for _, sub := range cfg.SubEmitters {
		if err := checkParticleFrames(atlas, atlasName, sub.Config); err != nil {
			return err
		}
	}
	return nil
}

func emitterWorldTransform(parent *BuiltObject, od *ObjectDoc, world math2d.Affine) (x, y, rot float32) {
	if parent == nil {
		return od.X, od.Y, od.Rotation
	}
	if tx, ty, r, _, _, ok := decomposeTRS(world); ok {
		return tx, ty, r
	}
	return world.Tx, world.Ty, float32(math.Atan2(float64(world.B), float64(world.A)))
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

// build builds layer bl's tilemap tm into bl.Tilemap or bl.Map and returns the tilemap's pixel width and height.
func (b *tilemapBuilder) build(bl *BuiltLayer, tm *TilemapDoc) (float32, float32, error) {
	resolved := tm
	if resolved.IsRef() {
		resolved = b.doc.Tilemaps[resolved.Ref]
	}
	if resolved.IsExternal() {
		where := fmt.Sprintf("layer %q tilemap source %q", bl.Name, resolved.Source)
		// Tilemap(source) cannot receive a level, so a source that names one is always built by the loader here.
		if tr, ok := b.resolver.(TilemapResolver); ok && resolved.Level == "" {
			loaded, err := tr.Tilemap(resolved.Source)
			if err != nil {
				return 0, 0, fmt.Errorf("sceneformat: %s: %w", where, err)
			}
			if loaded == nil {
				return 0, 0, fmt.Errorf("sceneformat: %s returned nil", where)
			}
			if err := validateInlineTilemap(nil, fmt.Sprintf("layer %q tilemap %q", bl.Name, resolved.Source), loaded); err != nil {
				// The loaded grid is not part of the document, so its error keeps no path into it.
				return 0, 0, err.(*FieldError).Err
			}
			if resolved.OffsetX != 0 || resolved.OffsetY != 0 {
				shifted := *loaded
				shifted.OffsetX += resolved.OffsetX
				shifted.OffsetY += resolved.OffsetY
				loaded = &shifted
			}
			resolved = loaded
		} else {
			m, err := b.external(where, resolved)
			if err != nil {
				return 0, 0, err
			}
			bl.Map = m
			return float32(m.Width) * m.TileWidth, float32(m.Height) * m.TileHeight, nil
		}
	}
	if resolved.IsLayered() {
		m, err := b.layered(bl.Name, resolved)
		if err != nil {
			return 0, 0, err
		}
		bl.Map = m
		return float32(m.Width) * m.TileWidth, float32(m.Height) * m.TileHeight, nil
	}
	if resolved.HasTilesetFile() {
		m, err := b.singleTileset(bl.Name, resolved)
		if err != nil {
			return 0, 0, err
		}
		bl.Map = m
		return float32(m.Width) * m.TileWidth, float32(m.Height) * m.TileHeight, nil
	}
	batch, w, h, err := buildTilemapBatch(bl.Name, resolved, b.getAtlas)
	if err != nil {
		return 0, 0, err
	}
	bl.Tilemap = batch
	return w, h, nil
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
		var ldtkOpts tilemap.LDtkOptions
		if lr, ok := b.resolver.(LDtkOptionsResolver); ok {
			ldtkOpts = lr.LDtkOptions()
		}
		ldtkOpts.ResolveImage = func(rel string) core.ImageSource {
			img, err := b.image(path.Join(dir, rel))
			if err != nil && imgErr == nil {
				imgErr = fmt.Errorf("image %q: %w", path.Join(dir, rel), err)
			}
			return img
		}
		m, err = tilemap.LoadLDtkWith(data, tm.Level, ldtkOpts)
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
	if tm.OffsetX != 0 || tm.OffsetY != 0 {
		m.Origin.X += core.Points(tm.OffsetX)
		m.Origin.Y += core.Points(tm.OffsetY)
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
	if tm.OffsetX != 0 || tm.OffsetY != 0 {
		m.Origin = core.Point{X: core.Points(tm.OffsetX), Y: core.Points(tm.OffsetY)}
	}
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

// singleTileset builds a single inline grid (Data) that names a YAML tileset (TilesetFile) in place of a sprite atlas into a tilemap.Tilemap with one layer named layerName.
func (b *tilemapBuilder) singleTileset(layerName string, tm *TilemapDoc) (*tilemap.Tilemap, error) {
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
	count := doc.Count()
	cells := make([]uint32, len(tm.Data))
	for i, gid := range tm.Data {
		if gid == 0 {
			continue
		}
		if gid > count {
			return nil, fmt.Errorf("sceneformat: %s row %d column %d places tile %d, but tileset %q has %d tiles", where, i/tm.Width, i%tm.Width, gid-1, tm.TilesetFile, count)
		}
		cells[i] = uint32(gid)
	}
	m.Layers = append(m.Layers, tilemap.NewTileLayer(layerName, tm.Width, tm.Height, cells))
	return m, nil
}

func buildTilemapBatch(layerName string, resolved *TilemapDoc, getAtlas func(string) (*sprite.Atlas, error)) (*sprite.Batch, float32, float32, error) {
	atlas, err := getAtlas(resolved.Tileset)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("sceneformat: layer %q tilemap: %w", layerName, err)
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
				return nil, 0, 0, fmt.Errorf("sceneformat: layer %q tilemap tileset %q has no frame %q", layerName, resolved.Tileset, key)
			}
			sp := sprite.NewSprite(frame)
			sp.X = resolved.OffsetX + float32(col)*float32(resolved.TileWidth)
			sp.Y = resolved.OffsetY + float32(row)*float32(resolved.TileHeight)
			sprites = append(sprites, &sp)
		}
	}
	return &sprite.Batch{Atlas: atlas, Sprites: sprites}, float32(resolved.Width) * float32(resolved.TileWidth), float32(resolved.Height) * float32(resolved.TileHeight), nil
}

// groupLayerDrawables sorts drawables stably by Z, groups consecutive sprites sharing an atlas into sprite.Batch values, and returns the sprite batches list, the layer's skeleton list, and the interleaved draw items preserving exact Z order across sprites, particle emitters, and skeletal rigs.
func groupLayerDrawables(drawables []layerDrawable) ([]*sprite.Batch, []*BuiltObject, []layerDrawItem) {
	if len(drawables) == 0 {
		return nil, nil, nil
	}
	slices.SortStableFunc(drawables, func(a, b layerDrawable) int {
		return cmp.Compare(a.z, b.z)
	})
	var batches []*sprite.Batch
	var skeletons []*BuiltObject
	var items []layerDrawItem
	for _, dr := range drawables {
		if dr.emitter != nil {
			items = append(items, layerDrawItem{emitter: dr.emitter})
			continue
		}
		if dr.skel != nil {
			skeletons = append(skeletons, dr.skel)
			items = append(items, layerDrawItem{skel: dr.skel})
			continue
		}
		bo := dr.sprite
		if n := len(items); n > 0 && items[n-1].batch != nil && items[n-1].batch.Atlas == bo.Atlas {
			items[n-1].batch.Sprites = append(items[n-1].batch.Sprites, bo.Sprite)
			continue
		}
		b := &sprite.Batch{
			Atlas:   bo.Atlas,
			Sprites: []*sprite.Sprite{bo.Sprite},
		}
		batches = append(batches, b)
		items = append(items, layerDrawItem{batch: b})
	}
	return batches, skeletons, items
}

// syncSkeleton copies the object's composed world transform, flips, alpha and Z onto its live skeletal rig and updates its bone world transforms. It reports false when the composed world transform is sheared.
func (bo *BuiltObject) syncSkeleton() bool {
	if bo.Skeleton == nil || bo.Doc == nil {
		return true
	}
	od := bo.Doc
	sk := bo.Skeleton
	if bo.Parent == nil {
		sk.X, sk.Y = od.X, od.Y
		sk.Rotation = od.Rotation
		sk.ScaleX, sk.ScaleY = od.EffectiveScaleX(), od.EffectiveScaleY()
	} else if x, y, rot, sx, sy, drawable := decomposeTRS(bo.World); drawable {
		sk.X, sk.Y, sk.Rotation, sk.ScaleX, sk.ScaleY = x, y, rot, sx, sy
	} else {
		return false
	}
	sk.FlipX = od.FlipX
	sk.FlipY = od.FlipY
	sk.Alpha = bo.WorldAlpha
	sk.Z = od.Z
	sk.UpdateWorldTransforms()
	return true
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
	if bo.Emitter != nil {
		wx, wy, wrot := emitterWorldTransform(bo.Parent, od, bo.World)
		bo.Emitter.Follow(wx, wy)
		bo.Emitter.Rotation = wrot + bo.emitterBaseRot
	}
	if !bo.syncSkeleton() {
		ok = false
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

	lastWorld := -1
	for i, layer := range s.Layers {
		if layer.Visible && layer.Space == SpaceWorld {
			lastWorld = i
		}
	}
	lightsDrawn := false
	drawLights := func() {
		if lightsDrawn || s.SkipLights2D || s.Lights2D == nil {
			return
		}
		lightsDrawn = true
		s.drawLights2D(d, vw, vh)
	}
	if lastWorld < 0 {
		drawLights()
	}

	for i, layer := range s.Layers {
		if !layer.Visible {
			continue
		}
		if layer.Tilemap != nil || layer.Map != nil || len(layer.Batches) > 0 || len(layer.Emitters) > 0 || len(layer.Skeletons) > 0 {
			view := s.tileView(layer.Space, vw, vh)
			if layer.Space == SpaceWorld && s.Camera != nil {
				s.drawWithCamera(d, vw, vh, layer, view)
			} else {
				drawLayer(d, layer, view)
			}
		}
		if i == lastWorld {
			drawLights()
		}
	}
	drawLights()
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

// Update advances the animated tiles of every layer's map (BuiltLayer.Map), every placed particle emitter (BuiltLayer.Emitters), and every placed 2D skeletal rig (BuiltLayer.Skeletons) by dt, so the next Draw shows their current frames and poses.
func (s *BuiltScene) Update(dt time.Duration) {
	if s == nil {
		return
	}
	for _, l := range s.Layers {
		if l.Map != nil {
			l.Map.Update(dt)
		}
		for _, bo := range l.Emitters {
			if bo.Emitter != nil {
				bo.Emitter.Update(dt)
			}
		}
		for _, bo := range l.Skeletons {
			if bo.Skeleton != nil {
				bo.Skeleton.Update(dt)
			}
		}
	}
}

func (s *BuiltScene) drawWithCamera(d *core.Drawing, vw, vh float32, layer *BuiltLayer, view *tilemap.Camera) {
	cam := s.Camera
	zoom := cam.EffectiveZoom()
	cx, cy := vw/2, vh/2
	camX, camY := clampCamera(cam.X, cam.Y, vw, vh, zoom, cam.Bounds)
	pcamX, pcamY := camX*layer.ParallaxX, camY*layer.ParallaxY

	layerView := view
	if layerView != nil {
		pview := *layerView
		pview.X = pcamX
		pview.Y = pcamY
		layerView = &pview
	}
	minCol, maxCol, minRow, maxRow, rx, ry := layer.RepeatTileRanges(pcamX, pcamY, vw, vh, zoom, cam.Rotation)

	// Identity camera without repeat needs no Save/Restore wrapper.
	if zoom == 1 && cam.Rotation == 0 && cx-pcamX == 0 && cy-pcamY == 0 && !rx && !ry {
		drawLayer(d, layer, layerView)
		return
	}

	d.Save()
	if zoom == 1 && cam.Rotation == 0 {
		if cx-pcamX != 0 || cy-pcamY != 0 {
			d.Translate(cx-pcamX, cy-pcamY)
		}
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
		if pcamX != 0 || pcamY != 0 {
			d.Translate(-pcamX, -pcamY)
		}
	}
	if !rx && !ry {
		drawLayer(d, layer, layerView)
	} else {
		for row := minRow; row <= maxRow; row++ {
			var ty float32
			if ry {
				ty = float32(row) * layer.RepeatHeight
			}
			for col := minCol; col <= maxCol; col++ {
				var tx float32
				if rx {
					tx = float32(col) * layer.RepeatWidth
				}
				tileView := layerView
				if tileView != nil && (tx != 0 || ty != 0) {
					tv := *tileView
					tv.X -= tx
					tv.Y -= ty
					tileView = &tv
				}
				if tx != 0 || ty != 0 {
					d.Save()
					d.Translate(tx, ty)
				}
				drawLayer(d, layer, tileView)
				if tx != 0 || ty != 0 {
					d.Restore()
				}
			}
		}
	}
	d.Restore()
}

const (
	// maxRepeatIndex keeps repeat tile indices exact in float32 and safely inside signed 32-bit int on all architectures.
	maxRepeatIndex = 1 << 24
	// maxRepeatPerAxis bounds repeat draws along a single axis when repeat periods are tiny or zoom is extreme.
	maxRepeatPerAxis = 256
	// maxRepeatTotal bounds total 2D repeat draws (cols * rows) so a tiny 2D repeat period cannot stall rendering.
	maxRepeatTotal = 4096
)

func repeatAxisRange(cam, half, period float64, limit int) (lo, hi int, ok bool) {
	if !isFinite64(cam) || !isFinite64(half) || !isFinite64(period) || period <= 0 || half <= 0 || limit <= 0 {
		return 0, 0, false
	}
	loF := math.Floor((cam - half) / period)
	hiF := math.Ceil((cam+half)/period) - 1
	if !isFinite64(loF) || !isFinite64(hiF) {
		return 0, 0, false
	}
	if hiF < loF {
		hiF = loF
	}
	if hiF-loF >= float64(limit) {
		center := math.Floor(cam / period)
		if !isFinite64(center) {
			return 0, 0, false
		}
		loF = center - float64(limit/2)
		hiF = loF + float64(limit-1)
	}
	if loF < -maxRepeatIndex || hiF > maxRepeatIndex {
		return 0, 0, false
	}
	return int(loF), int(hiF), true
}

// RepeatTileRanges computes the tile column and row ranges covering the visible bounds of a camera at (camX, camY) with viewport (vw, vh), zoom and rotation, matching scene.Layer's repeat tiling while guarding non-finite inputs, integer overflow, and extreme repeat counts.
func (l *BuiltLayer) RepeatTileRanges(camX, camY, vw, vh, zoom, rot float32) (minCol, maxCol, minRow, maxRow int, rx, ry bool) {
	if l == nil {
		return 0, 0, 0, 0, false, false
	}
	repeatX := l.RepeatX && isFinite(l.RepeatWidth) && l.RepeatWidth > 0
	repeatY := l.RepeatY && isFinite(l.RepeatHeight) && l.RepeatHeight > 0
	if !repeatX && !repeatY {
		return 0, 0, 0, 0, false, false
	}
	if !isFinite(camX) || !isFinite(camY) || !isFinite(vw) || !isFinite(vh) || !isFinite(rot) {
		return 0, 0, 0, 0, false, false
	}
	if !isFinite(zoom) || zoom <= 0 {
		zoom = 1
	}
	z := float64(zoom)
	hw := math.Max(0, float64(vw)/2)
	hh := math.Max(0, float64(vh)/2)
	halfW, halfH := hw/z, hh/z
	if rot != 0 {
		sin, cos := math.Sincos(float64(rot))
		acos, asin := math.Abs(cos), math.Abs(sin)
		halfW = (hw*acos + hh*asin) / z
		halfH = (hw*asin + hh*acos) / z
	}
	if repeatX {
		minCol, maxCol, rx = repeatAxisRange(float64(camX), halfW, float64(l.RepeatWidth), maxRepeatPerAxis)
	}
	if repeatY {
		minRow, maxRow, ry = repeatAxisRange(float64(camY), halfH, float64(l.RepeatHeight), maxRepeatPerAxis)
	}
	if rx && ry {
		cols := maxCol - minCol + 1
		rows := maxRow - minRow + 1
		if cols*rows > maxRepeatTotal {
			const max2DAxis = 64 // 64*64 == maxRepeatTotal
			if cols > max2DAxis {
				c := (minCol + maxCol) / 2
				minCol = c - max2DAxis/2
				maxCol = minCol + max2DAxis - 1
				cols = max2DAxis
			}
			maxRows := max(1, maxRepeatTotal/cols)
			if rows > maxRows {
				c := (minRow + maxRow) / 2
				minRow = c - maxRows/2
				maxRow = minRow + maxRows - 1
			}
		}
	}
	return minCol, maxCol, minRow, maxRow, rx, ry
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

// DrawObjects draws the layer's sprite batches, particle emitters, and skeletal rigs into d in exact Z order.
func (l *BuiltLayer) DrawObjects(d *core.Drawing) {
	if l == nil || d == nil {
		return
	}
	if len(l.drawItems) > 0 {
		for _, item := range l.drawItems {
			if item.batch != nil {
				item.batch.Draw(d)
			} else if item.emitter != nil && item.emitter.Emitter != nil {
				item.emitter.Emitter.Draw(d)
			} else if item.skel != nil && item.skel.Skeleton != nil {
				item.skel.Skeleton.Draw(d)
			}
		}
		return
	}
	for _, b := range l.Batches {
		b.Draw(d)
	}
	for _, bo := range l.Emitters {
		if bo.Emitter != nil {
			bo.Emitter.Draw(d)
		}
	}
	for _, bo := range l.Skeletons {
		if bo.Skeleton != nil {
			bo.Skeleton.Draw(d)
		}
	}
}

// Draw draws the layer's tilemap, layered map, and Z-order interleaved sprite batches, particle emitters, and skeletal rigs into d. When view is nil, every tile of Map draws without culling.
func (l *BuiltLayer) Draw(d *core.Drawing, view *tilemap.Camera) {
	if l == nil || d == nil {
		return
	}
	if l.Tilemap != nil {
		l.Tilemap.Draw(d)
	}
	if l.Map != nil {
		l.Map.Draw(d, view)
	}
	l.DrawObjects(d)
}

func drawLayer(d *core.Drawing, layer *BuiltLayer, view *tilemap.Camera) {
	layer.Draw(d, view)
}
