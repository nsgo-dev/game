// Package scene organizes a game into ordered world- and screen-space layers, hit-tests sprites and custom shapes in view coordinates, and manages scene lifecycles, stacks, and transitions.
package scene

import (
	"cmp"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Space names the coordinate system a Layer draws and hit-tests in.
type Space uint8

const (
	// SpaceWorld draws under the Scene's Camera and converts view points to world space for hit testing.
	SpaceWorld Space = iota
	// SpaceScreen draws directly in view coordinates, unaffected by the Scene's Camera.
	SpaceScreen
)

// Drawable is a custom layer item with optional draw, update, hit-test, and input callbacks.
//
// Its zero value is hidden (Visible is false); NewDrawable returns a visible Drawable.
type Drawable struct {
	// Draw records custom drawing operations into d, running between Save and Restore.
	Draw func(d *core.Drawing)
	// Update advances custom state by one fixed step dt when Scene.Update runs.
	Update func(dt time.Duration)
	// Hit reports whether a point in the layer's coordinate space hits this drawable.
	Hit func(p core.Point) bool
	// OnPress runs when Scene.DispatchPress hits this drawable as the topmost target.
	OnPress func(hit HitResult, ev core.PressEvent)
	// OnTouch runs when Scene.DispatchTouch hits this drawable as the topmost target.
	OnTouch func(hit HitResult, ev core.TouchEvent)
	// Z orders the drawable relative to other items in its layer: a higher Z draws over a lower one.
	Z int
	// Y is the vertical baseline coordinate used when its Layer's DepthSort is SortY (unless overridden by Layer.SortKey).
	Y float32
	// Visible controls whether the drawable is drawn and hit-tested.
	Visible bool
}

// NewDrawable returns a visible Drawable with draw as its Draw callback.
func NewDrawable(draw func(d *core.Drawing)) *Drawable {
	return &Drawable{Draw: draw, Visible: true}
}

// Item is one entry in a Layer: a sprite batch, a tilemap, or a custom Drawable.
//
// Z orders items within their layer (lowest first, ties keeping slice order). For a Drawable, a zero Item.Z falls back to Drawable.Z so changing Drawable.Z takes effect immediately.
type Item struct {
	Batch    *sprite.Batch
	Tilemap  *tilemap.Tilemap
	Drawable *Drawable
	Z        int
}

func (it Item) z() int {
	if it.Drawable != nil && it.Z == 0 {
		return it.Drawable.Z
	}
	return it.Z
}

// Layer holds an ordered list of items drawn in either world or screen space.
//
// Its zero value is hidden (Visible is false); NewLayer returns a visible Layer.
type Layer struct {
	// Name identifies the layer for Scene.Layer lookups.
	Name string
	// Space selects whether the layer draws under Scene.Camera (SpaceWorld) or directly in view space (SpaceScreen).
	Space Space
	// Visible controls whether the layer is drawn and hit-tested.
	Visible bool
	// Order sorts layers in a Scene: a higher Order draws over a lower one, ties keeping the Scene.Layers slice order.
	Order int
	// Items are the batches, tilemaps, and custom drawables in the layer.
	Items []Item

	// ParallaxX and ParallaxY scale how fast a SpaceWorld layer scrolls relative to Scene.Camera.
	// On a struct literal without SetParallax/NewLayer/NewParallaxLayer, zero values default to 1.0.
	ParallaxX, ParallaxY float32
	// OffsetX and OffsetY shift the layer's content in layer coordinates.
	OffsetX, OffsetY float32
	// RepeatX and RepeatY tile a SpaceWorld layer seamlessly across the camera's visible region using RepeatWidth and RepeatHeight as periods.
	RepeatX, RepeatY bool
	// RepeatWidth and RepeatHeight are the horizontal and vertical repeat periods in layer points when RepeatX / RepeatY are enabled.
	RepeatWidth, RepeatHeight float32

	// DepthSort controls how items and sprites within the layer are depth-sorted during Draw and HitTest.
	DepthSort SortMode
	// SortKey optionally supplies a custom depth key for items in SortY (or SortCustom when SortFunc is nil).
	SortKey func(Item) float32
	// SpriteSortKey optionally supplies a custom depth key for sprites in SortY (after Sprite.Z) or SortCustom.
	SpriteSortKey func(*sprite.Sprite) float32
	// SortFunc supplies a custom item comparator when DepthSort is SortCustom.
	SortFunc func(a, b Item) int

	parallaxSet  bool
	order        []Item
	entries      []drawEntry
	scratchBatch sprite.Batch
	scratchOne   [1]*sprite.Sprite
}

// NewLayer returns a visible Layer with the given name, coordinate space, and draw order.
func NewLayer(name string, space Space, order int) *Layer {
	return &Layer{
		Name:        name,
		Space:       space,
		Visible:     true,
		Order:       order,
		ParallaxX:   1,
		ParallaxY:   1,
		parallaxSet: true,
	}
}

// AddBatch appends a sprite batch to the layer.
func (l *Layer) AddBatch(b *sprite.Batch) {
	if b == nil {
		return
	}
	l.Items = append(l.Items, Item{Batch: b})
}

// AddTilemap appends a tilemap to the layer.
func (l *Layer) AddTilemap(m *tilemap.Tilemap) {
	if m == nil {
		return
	}
	l.Items = append(l.Items, Item{Tilemap: m})
}

// AddDrawable appends a custom Drawable to the layer.
func (l *Layer) AddDrawable(d *Drawable) {
	if d == nil {
		return
	}
	l.Items = append(l.Items, Item{Drawable: d})
}

// AddItem appends an explicit Item to the layer.
func (l *Layer) AddItem(item Item) {
	l.Items = append(l.Items, item)
}

func (l *Layer) sortedItems() []Item {
	l.order = append(l.order[:0], l.Items...)
	slices.SortStableFunc(l.order, func(a, b Item) int {
		return cmp.Compare(a.z(), b.z())
	})
	return l.order
}

func (l *Layer) draw(d *core.Drawing, cam *tilemap.Camera) {
	if l == nil || !l.Visible || len(l.Items) == 0 {
		return
	}
	l.entries = l.buildDrawEntries(l.entries)
	defer clear(l.entries)
	l.drawWithParallax(d, cam, l.entries)
}

func (l *Layer) update(dt time.Duration) {
	if l == nil || len(l.Items) == 0 {
		return
	}
	items := l.sortedItems()
	defer clear(l.order)
	for _, it := range items {
		if it.Tilemap != nil {
			it.Tilemap.Update(dt)
		}
		if it.Drawable != nil && it.Drawable.Update != nil {
			it.Drawable.Update(dt)
		}
	}
}

// Scene composes ordered world- and screen-space Layers sharing a 2D Camera.
type Scene struct {
	// Camera transforms SpaceWorld layers during Draw and HitTest; nil leaves world coordinates untransformed.
	Camera *tilemap.Camera
	// Layers are the scene's layers, drawn in stable ascending Layer.Order.
	Layers []*Layer

	order      []*Layer
	spriteHits map[*sprite.Sprite]HitOptions
}

// NewScene returns an empty Scene using cam for its world-space layers.
func NewScene(cam *tilemap.Camera) *Scene {
	return &Scene{Camera: cam}
}

// AddLayer appends l to the scene and returns l.
func (s *Scene) AddLayer(l *Layer) *Layer {
	if l == nil {
		return nil
	}
	s.Layers = append(s.Layers, l)
	return l
}

// Layer returns the first layer named name, or nil if none matches.
func (s *Scene) Layer(name string) *Layer {
	for _, l := range s.Layers {
		if l != nil && l.Name == name {
			return l
		}
	}
	return nil
}

func (s *Scene) sortedLayers() []*Layer {
	s.order = append(s.order[:0], s.Layers...)
	slices.SortStableFunc(s.order, func(a, b *Layer) int {
		var ao, bo int
		if a != nil {
			ao = a.Order
		}
		if b != nil {
			bo = b.Order
		}
		return cmp.Compare(ao, bo)
	})
	return s.order
}

// Update advances the scene's Camera, tilemaps, and custom drawables by dt in layer order.
func (s *Scene) Update(dt time.Duration) {
	if s == nil {
		return
	}
	if s.Camera != nil {
		s.Camera.Update(dt)
	}
	layers := s.sortedLayers()
	defer clear(s.order)
	for _, l := range layers {
		l.update(dt)
	}
}

// Draw draws every visible layer in stable ascending Order into d, leaving d's transform and paint state as it found it.
func (s *Scene) Draw(d *core.Drawing) {
	if s == nil || d == nil {
		return
	}
	layers := s.sortedLayers()
	defer clear(s.order)
	for _, l := range layers {
		l.draw(d, s.Camera)
	}
}
