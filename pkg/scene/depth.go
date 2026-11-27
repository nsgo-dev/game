package scene

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// SortMode controls how items and sprites are depth-sorted within a Layer during Draw and HitTest.
type SortMode uint8

const (
	// SortNone preserves Z and slice insertion order (default).
	SortNone SortMode = iota
	// SortY sorts drawables and sprites in painter's order: primary Z, then ascending Y (or SortKey / SpriteSortKey), then insertion order.
	SortY
	// SortCustom sorts items with Layer.SortFunc (or Layer.SortKey / Z) and sprites with Layer.SpriteSortKey (or Z).
	SortCustom
)

type drawEntryKind uint8

const (
	entryTilemap drawEntryKind = iota
	entryTileLayer
	entryImageLayer
	entryYSortTile
	entryBatch
	entrySprite
	entryDrawable
)

type drawEntry struct {
	kind       drawEntryKind
	itemZ      int
	subZ       int
	key        float32
	tilemap    *tilemap.Tilemap
	tileLayer  *tilemap.TileLayer
	imageLayer *tilemap.ImageLayer
	tileIdx    int
	batch      *sprite.Batch
	sprite     *sprite.Sprite
	drawable   *Drawable
}

func (l *Layer) hasYSortTilemaps() bool {
	for _, it := range l.Items {
		if it.Tilemap == nil {
			continue
		}
		if it.TileLayer != nil {
			if it.TileLayer.YSort && it.TileLayer.Visible && it.TileLayer.Opacity > 0 {
				return true
			}
			continue
		}
		if it.Tilemap.HasYSortLayers() {
			return true
		}
	}
	return false
}

func (l *Layer) buildDrawEntries(dst []drawEntry, tmCam *tilemap.Camera, collectTiles bool) []drawEntry {
	dst = dst[:0]
	if collectTiles {
		l.ysortTiles = l.ysortTiles[:0]
	}
	if l == nil || len(l.Items) == 0 {
		return dst
	}

	switch l.DepthSort {
	case SortY:
		hasYSort := collectTiles && l.hasYSortTilemaps()
		if l.SortKey != nil && l.SpriteSortKey == nil && !hasYSort {
			items := slices.Clone(l.Items)
			slices.SortStableFunc(items, func(a, b Item) int {
				if c := cmp.Compare(a.z(), b.z()); c != 0 {
					return c
				}
				return cmp.Compare(l.SortKey(a), l.SortKey(b))
			})
			for _, it := range items {
				if it.Tilemap != nil {
					if it.TileLayer != nil {
						dst = append(dst, drawEntry{kind: entryTileLayer, tilemap: it.Tilemap, tileLayer: it.TileLayer})
					} else {
						dst = append(dst, drawEntry{kind: entryTilemap, tilemap: it.Tilemap})
					}
				}
				if it.Batch != nil && len(it.Batch.Sprites) > 0 {
					sprites := make([]*sprite.Sprite, 0, len(it.Batch.Sprites))
					for _, sp := range it.Batch.Sprites {
						if sp != nil {
							sprites = append(sprites, sp)
						}
					}
					slices.SortStableFunc(sprites, func(a, b *sprite.Sprite) int {
						if c := cmp.Compare(a.Z, b.Z); c != 0 {
							return c
						}
						return cmp.Compare(a.Y, b.Y)
					})
					for _, sp := range sprites {
						dst = append(dst, drawEntry{kind: entrySprite, batch: it.Batch, sprite: sp})
					}
				}
				if it.Drawable != nil {
					dst = append(dst, drawEntry{kind: entryDrawable, drawable: it.Drawable})
				}
			}
			return dst
		}

		for _, it := range l.Items {
			iz := it.z()
			if it.Tilemap != nil {
				bgKey := float32(-math.MaxFloat32)
				if l.SortKey != nil {
					bgKey = l.SortKey(it)
				}
				if it.TileLayer != nil {
					if collectTiles && it.TileLayer.YSort {
						start := len(l.ysortTiles)
						l.ysortTiles = it.Tilemap.AppendYSortTiles(l.ysortTiles, it.TileLayer, tmCam)
						for i := start; i < len(l.ysortTiles); i++ {
							dst = append(dst, drawEntry{
								kind:    entryYSortTile,
								itemZ:   iz,
								subZ:    0,
								key:     l.ysortTiles[i].SortY,
								tileIdx: i,
							})
						}
					} else {
						dst = append(dst, drawEntry{
							kind:      entryTileLayer,
							itemZ:     iz,
							subZ:      0,
							key:       bgKey,
							tilemap:   it.Tilemap,
							tileLayer: it.TileLayer,
						})
					}
				} else if collectTiles && it.Tilemap.HasYSortLayers() {
					ysortRemaining := 0
					for i := range it.Tilemap.Layers {
						tl := &it.Tilemap.Layers[i]
						if tl.YSort && tl.Visible && tl.Opacity > 0 {
							ysortRemaining++
						}
					}
					seenYSort := false
					it.Tilemap.VisitLayers(func(tl *tilemap.TileLayer, il *tilemap.ImageLayer) {
						if tl != nil {
							if !tl.Visible || tl.Opacity <= 0 {
								return
							}
							if tl.YSort {
								seenYSort = true
								ysortRemaining--
								start := len(l.ysortTiles)
								l.ysortTiles = it.Tilemap.AppendYSortTiles(l.ysortTiles, tl, tmCam)
								for i := start; i < len(l.ysortTiles); i++ {
									dst = append(dst, drawEntry{
										kind:    entryYSortTile,
										itemZ:   iz,
										subZ:    0,
										key:     l.ysortTiles[i].SortY,
										tileIdx: i,
									})
								}
								return
							}
							key := bgKey
							if seenYSort && ysortRemaining == 0 {
								key = math.MaxFloat32
							}
							dst = append(dst, drawEntry{
								kind:      entryTileLayer,
								itemZ:     iz,
								subZ:      0,
								key:       key,
								tilemap:   it.Tilemap,
								tileLayer: tl,
							})
							return
						}
						if il != nil && il.Visible && il.Opacity > 0 {
							key := bgKey
							if seenYSort && ysortRemaining == 0 {
								key = math.MaxFloat32
							}
							dst = append(dst, drawEntry{
								kind:       entryImageLayer,
								itemZ:      iz,
								subZ:       0,
								key:        key,
								tilemap:    it.Tilemap,
								imageLayer: il,
							})
						}
					})
				} else {
					dst = append(dst, drawEntry{
						kind:    entryTilemap,
						itemZ:   iz,
						subZ:    0,
						key:     bgKey,
						tilemap: it.Tilemap,
					})
				}
			}
			if it.Batch != nil && len(it.Batch.Sprites) > 0 {
				for _, sp := range it.Batch.Sprites {
					if sp == nil {
						continue
					}
					key := sp.Y
					if l.SpriteSortKey != nil {
						key = l.SpriteSortKey(sp)
					}
					dst = append(dst, drawEntry{
						kind:   entrySprite,
						itemZ:  iz,
						subZ:   sp.Z,
						key:    key,
						batch:  it.Batch,
						sprite: sp,
					})
				}
			}
			if it.Drawable != nil {
				key := it.Drawable.Y
				if l.SortKey != nil {
					key = l.SortKey(it)
				}
				dst = append(dst, drawEntry{
					kind:     entryDrawable,
					itemZ:    iz,
					subZ:     0,
					key:      key,
					drawable: it.Drawable,
				})
			}
		}
		slices.SortStableFunc(dst, func(a, b drawEntry) int {
			if c := cmp.Compare(a.itemZ, b.itemZ); c != 0 {
				return c
			}
			if c := cmp.Compare(a.subZ, b.subZ); c != 0 {
				return c
			}
			return cmp.Compare(a.key, b.key)
		})
		return dst

	case SortCustom:
		items := slices.Clone(l.Items)
		switch {
		case l.SortFunc != nil:
			slices.SortStableFunc(items, l.SortFunc)
		case l.SortKey != nil:
			slices.SortStableFunc(items, func(a, b Item) int {
				return cmp.Compare(l.SortKey(a), l.SortKey(b))
			})
		default:
			slices.SortStableFunc(items, func(a, b Item) int {
				return cmp.Compare(a.z(), b.z())
			})
		}
		for _, it := range items {
			if it.Tilemap != nil {
				if it.TileLayer != nil {
					dst = append(dst, drawEntry{kind: entryTileLayer, tilemap: it.Tilemap, tileLayer: it.TileLayer})
				} else {
					dst = append(dst, drawEntry{kind: entryTilemap, tilemap: it.Tilemap})
				}
			}
			if it.Batch != nil {
				if l.SpriteSortKey != nil {
					sprites := make([]*sprite.Sprite, 0, len(it.Batch.Sprites))
					for _, sp := range it.Batch.Sprites {
						if sp != nil {
							sprites = append(sprites, sp)
						}
					}
					slices.SortStableFunc(sprites, func(a, b *sprite.Sprite) int {
						return cmp.Compare(l.SpriteSortKey(a), l.SpriteSortKey(b))
					})
					for _, sp := range sprites {
						dst = append(dst, drawEntry{kind: entrySprite, batch: it.Batch, sprite: sp})
					}
				} else {
					dst = append(dst, drawEntry{kind: entryBatch, batch: it.Batch})
				}
			}
			if it.Drawable != nil {
				dst = append(dst, drawEntry{kind: entryDrawable, drawable: it.Drawable})
			}
		}
		return dst

	default: // SortNone
		items := slices.Clone(l.Items)
		slices.SortStableFunc(items, func(a, b Item) int {
			return cmp.Compare(a.z(), b.z())
		})
		for _, it := range items {
			if it.Tilemap != nil {
				if it.TileLayer != nil {
					dst = append(dst, drawEntry{kind: entryTileLayer, tilemap: it.Tilemap, tileLayer: it.TileLayer})
				} else {
					dst = append(dst, drawEntry{kind: entryTilemap, tilemap: it.Tilemap})
				}
			}
			if it.Batch != nil {
				dst = append(dst, drawEntry{kind: entryBatch, batch: it.Batch})
			}
			if it.Drawable != nil {
				dst = append(dst, drawEntry{kind: entryDrawable, drawable: it.Drawable})
			}
		}
		return dst
	}
}

func (l *Layer) drawEntries(d *core.Drawing, entries []drawEntry, tmCam *tilemap.Camera) {
	for _, e := range entries {
		if e.kind == entryYSortTile {
			l.ysortDrawer.Push(d, &l.ysortTiles[e.tileIdx])
			continue
		}
		l.ysortDrawer.Flush(d)
		switch e.kind {
		case entryTilemap:
			e.tilemap.Draw(d, tmCam)
		case entryTileLayer:
			e.tilemap.DrawLayer(d, e.tileLayer, tmCam)
		case entryImageLayer:
			e.tilemap.DrawImageLayer(d, e.imageLayer, tmCam)
		case entryBatch:
			if e.batch.Atlas != nil {
				e.batch.Draw(d)
			}
		case entrySprite:
			if e.batch.Atlas != nil && e.sprite != nil {
				l.scratchBatch.Atlas = e.batch.Atlas
				l.scratchOne[0] = e.sprite
				l.scratchBatch.Sprites = l.scratchOne[:]
				l.scratchBatch.Draw(d)
				l.scratchOne[0] = nil
				l.scratchBatch.Atlas = nil
			}
		case entryDrawable:
			if e.drawable.Visible && e.drawable.Draw != nil {
				d.Save()
				e.drawable.Draw(d)
				d.Restore()
			}
		}
	}
	l.ysortDrawer.Flush(d)
}
