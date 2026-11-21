package tilemap

import (
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// LayerCache holds a static tile layer baked into a premultiplied RGBA pixel buffer so DrawLayer can emit a single DrawImageFrom op.
type LayerCache struct {
	Pixels  *image.RGBA
	Source  core.ImageSource
	OriginX float32
	OriginY float32
	Width   float32
	Height  float32
	Dirty   bool
	Version uint64
}

// LayerCache returns the static cache entry for layerIndex, or nil if that layer has not been cached.
func (m *Tilemap) LayerCache(layerIndex int) *LayerCache {
	if m.caches == nil {
		return nil
	}
	return m.caches[layerIndex]
}

// InvalidateLayerCache marks any cached image for layerIndex dirty so subsequent Draw calls fall back to live tile drawing until recached.
func (m *Tilemap) InvalidateLayerCache(layerIndex int) {
	if m.caches == nil {
		return
	}
	if c, ok := m.caches[layerIndex]; ok && c != nil {
		c.Dirty = true
	}
}

// ClearLayerCache removes any cached image for layerIndex.
func (m *Tilemap) ClearLayerCache(layerIndex int) {
	if m.caches == nil {
		return
	}
	delete(m.caches, layerIndex)
}

func (m *Tilemap) layerCacheFor(layer *TileLayer) *LayerCache {
	if m.caches == nil || layer == nil {
		return nil
	}
	for i := range m.Layers {
		if &m.Layers[i] == layer {
			return m.caches[i]
		}
	}
	return nil
}

// CacheStaticLayer bakes a static (non-animated) tile layer into a premultiplied *image.RGBA buffer and attaches it to the map so Draw and DrawLayer draw the layer in a single DrawImageFrom operation.
//
// images maps tileset image URIs (with or without an "asset:" prefix) or tileset names to decoded image.Image pixel data; tilesets whose core.ImageSource.Pixels is already populated do not need an entry in images.
// Modifying the layer via SetTile or FillRect automatically marks the cache dirty.
func (m *Tilemap) CacheStaticLayer(layerIndex int, images map[string]image.Image) (*image.RGBA, core.ImageSource, error) {
	if layerIndex < 0 || layerIndex >= len(m.Layers) {
		return nil, core.ImageSource{}, fmt.Errorf("tilemap: cache: layer index %d out of range [0, %d)", layerIndex, len(m.Layers))
	}
	layer := &m.Layers[layerIndex]
	lw, lh := m.layerSize(layer)
	if lw <= 0 || lh <= 0 || m.TileWidth <= 0 || m.TileHeight <= 0 {
		return nil, core.ImageSource{}, fmt.Errorf("tilemap: cache: layer %d (%q) has non-positive dimensions", layerIndex, layer.Name)
	}

	// Validate no animated tiles exist in this layer and compute tight pixel bounds in layer-local space.
	base := m.projectedWorldBounds(lw, lh)
	minX := float64(base.Origin.X)
	minY := float64(base.Origin.Y)
	maxX := minX + float64(base.Size.Width)
	maxY := minY + float64(base.Size.Height)

	type bakedTile struct {
		src        image.Image
		sx, sy     int
		sw, sh     int
		ox, oy     int
		dstX, dstY float32
		tw, th     float32
		flags      uint32
	}

	var tiles []bakedTile
	visitCell := func(col, row int) error {
		idx := row*lw + col
		if idx < 0 || idx >= len(layer.Data) {
			return nil
		}
		gid := layer.Data[idx]
		if gid == 0 {
			return nil
		}
		ts, localID := m.TilesetForGID(gid)
		if ts == nil {
			return nil
		}
		if ts.Animations != nil {
			if anim, ok := ts.Animations[localID]; ok && anim != nil && len(anim.Frames) > 0 {
				return fmt.Errorf("tilemap: cache: layer %d (%q) contains animated tile %d", layerIndex, layer.Name, gid&TileIDMask)
			}
		}
		imgSrc, sx, sy, sw, sh, ox, oy, tw, th, ok := ts.tileSource(localID)
		if !ok {
			return nil
		}
		srcImg := resolveTilesetImage(ts, imgSrc, images)
		if srcImg == nil {
			return fmt.Errorf("tilemap: cache: missing image for tileset %q (URI %q)", ts.Name, imgSrc.URI)
		}
		tx, ty := m.tileOrigin(col, row)
		dstX := tx
		dstY := ty + m.TileHeight - th

		minX = min(minX, float64(dstX))
		minY = min(minY, float64(dstY))
		maxX = max(maxX, float64(dstX+tw))
		maxY = max(maxY, float64(dstY+th))

		tiles = append(tiles, bakedTile{
			src:   srcImg,
			sx:    int(math.Round(float64(sx))),
			sy:    int(math.Round(float64(sy))),
			sw:    int(math.Round(float64(sw))),
			sh:    int(math.Round(float64(sh))),
			ox:    int(math.Round(float64(ox))),
			oy:    int(math.Round(float64(oy))),
			dstX:  dstX,
			dstY:  dstY,
			tw:    tw,
			th:    th,
			flags: gid & FlipMask,
		})
		return nil
	}

	// Collect tiles in painter's order matching DrawLayer.
	switch m.Orientation {
	case OrientationIsometric:
		for s := 0; s <= (lw-1)+(lh-1); s++ {
			c0 := max(0, s-(lh-1))
			c1 := min(lw-1, s)
			for col := c0; col <= c1; col++ {
				if err := visitCell(col, s-col); err != nil {
					return nil, core.ImageSource{}, err
				}
			}
		}
	case OrientationStaggered, OrientationHexagonal:
		if m.StaggerAxis == StaggerX {
			for row := range lh {
				for col := range lw {
					if !isStaggered(col, m.StaggerIndex) {
						if err := visitCell(col, row); err != nil {
							return nil, core.ImageSource{}, err
						}
					}
				}
				for col := range lw {
					if isStaggered(col, m.StaggerIndex) {
						if err := visitCell(col, row); err != nil {
							return nil, core.ImageSource{}, err
						}
					}
				}
			}
		} else {
			for row := range lh {
				for col := range lw {
					if err := visitCell(col, row); err != nil {
						return nil, core.ImageSource{}, err
					}
				}
			}
		}
	default:
		for row := range lh {
			for col := range lw {
				if err := visitCell(col, row); err != nil {
					return nil, core.ImageSource{}, err
				}
			}
		}
	}

	originX := math.Floor(minX)
	originY := math.Floor(minY)
	pixelW := max(1, int(math.Ceil(maxX-originX)))
	pixelH := max(1, int(math.Ceil(maxY-originY)))

	rgba := image.NewRGBA(image.Rect(0, 0, pixelW, pixelH))
	for _, bt := range tiles {
		dx := int(math.Round(float64(bt.dstX) - originX))
		dy := int(math.Round(float64(bt.dstY) - originY))
		tw := int(math.Round(float64(bt.tw)))
		th := int(math.Round(float64(bt.th)))
		blitTileRGBA(rgba, bt.src, bt.sx, bt.sy, bt.sw, bt.sh, bt.ox, bt.oy, dx, dy, tw, th, bt.flags)
	}

	var version uint64 = 1
	if m.caches != nil {
		if prev, ok := m.caches[layerIndex]; ok && prev != nil {
			version = prev.Version + 1
		}
	} else {
		m.caches = make(map[int]*LayerCache)
	}

	src := core.ImageSource{
		Pixels:  rgba,
		Version: version,
		Size:    core.Size{Width: core.Points(pixelW), Height: core.Points(pixelH)},
	}
	m.caches[layerIndex] = &LayerCache{
		Pixels:  rgba,
		Source:  src,
		OriginX: float32(originX),
		OriginY: float32(originY),
		Width:   float32(pixelW),
		Height:  float32(pixelH),
		Dirty:   false,
		Version: version,
	}
	return rgba, src, nil
}

func resolveTilesetImage(ts *Tileset, imgSrc core.ImageSource, images map[string]image.Image) image.Image {
	if imgSrc.Pixels != nil {
		return imgSrc.Pixels
	}
	if len(images) == 0 {
		return nil
	}
	if imgSrc.URI != "" {
		if img, ok := images[imgSrc.URI]; ok && img != nil {
			return img
		}
		if trimmed, ok := strings.CutPrefix(imgSrc.URI, "asset:"); ok {
			if img, ok := images[trimmed]; ok && img != nil {
				return img
			}
		}
	}
	if ts.Name != "" {
		if img, ok := images[ts.Name]; ok && img != nil {
			return img
		}
	}
	return nil
}

// blitTileRGBA composites one tile (with optional Tiled flip/diagonal bits) onto premultiplied dst using Porter-Duff Over.
func blitTileRGBA(dst *image.RGBA, src image.Image, sx, sy, sw, sh, ox, oy, dx, dy, tw, th int, flags uint32) {
	if sw <= 0 || sh <= 0 {
		return
	}
	if tw <= 0 {
		tw = sw
	}
	if th <= 0 {
		th = sh
	}
	srcBounds := src.Bounds()
	dstBounds := dst.Bounds()

	for py := range sh {
		srcY := srcBounds.Min.Y + sy + py
		if srcY < srcBounds.Min.Y || srcY >= srcBounds.Max.Y {
			continue
		}
		for px := range sw {
			srcX := srcBounds.Min.X + sx + px
			if srcX < srcBounds.Min.X || srcX >= srcBounds.Max.X {
				continue
			}
			sr, sg, sb, sa := src.At(srcX, srcY).RGBA()
			if sa == 0 {
				continue
			}

			lx := ox + px
			ly := oy + py
			dw, dh := tw, th
			if flags&FlipDiagonal != 0 {
				lx, ly = ly, lx
				dw, dh = dh, dw
			}
			if flags&FlipHorizontal != 0 {
				lx = dw - 1 - lx
			}
			if flags&FlipVertical != 0 {
				ly = dh - 1 - ly
			}
			outX := dx + lx
			outY := dy + ly
			if outX < dstBounds.Min.X || outX >= dstBounds.Max.X || outY < dstBounds.Min.Y || outY >= dstBounds.Max.Y {
				continue
			}

			off := dst.PixOffset(outX, outY)
			if sa == 0xffff {
				dst.Pix[off+0] = uint8(sr >> 8)
				dst.Pix[off+1] = uint8(sg >> 8)
				dst.Pix[off+2] = uint8(sb >> 8)
				dst.Pix[off+3] = 255
				continue
			}
			invA := 0xffff - sa
			dr := uint32(dst.Pix[off+0]) * 0x101
			dg := uint32(dst.Pix[off+1]) * 0x101
			db := uint32(dst.Pix[off+2]) * 0x101
			da := uint32(dst.Pix[off+3]) * 0x101

			or := sr + (dr*invA+0x7fff)/0xffff
			og := sg + (dg*invA+0x7fff)/0xffff
			ob := sb + (db*invA+0x7fff)/0xffff
			oa := sa + (da*invA+0x7fff)/0xffff

			dst.Pix[off+0] = uint8(min(or, 0xffff) >> 8)
			dst.Pix[off+1] = uint8(min(og, 0xffff) >> 8)
			dst.Pix[off+2] = uint8(min(ob, 0xffff) >> 8)
			dst.Pix[off+3] = uint8(min(oa, 0xffff) >> 8)
		}
	}
}

func (m *Tilemap) drawCachedLayer(d *core.Drawing, layer *TileLayer, cache *LayerCache, cam *Camera, offX, offY float32) {
	dstX := offX + cache.OriginX
	dstY := offY + cache.OriginY
	sx, sy := float32(0), float32(0)
	sw, sh := cache.Width, cache.Height

	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		vx0 := float32(vb.Origin.X)
		vy0 := float32(vb.Origin.Y)
		vx1 := vx0 + float32(vb.Size.Width)
		vy1 := vy0 + float32(vb.Size.Height)

		ix0 := max(dstX, vx0)
		iy0 := max(dstY, vy0)
		ix1 := min(dstX+sw, vx1)
		iy1 := min(dstY+sh, vy1)
		if ix0 >= ix1 || iy0 >= iy1 {
			return
		}
		sx = ix0 - dstX
		sy = iy0 - dstY
		sw = ix1 - ix0
		sh = iy1 - iy0
		dstX = ix0
		dstY = iy0
	}

	if layer.Opacity < 1 {
		d.Save().SetAlpha(layer.Opacity).DrawImageFrom(cache.Source, sx, sy, sw, sh, dstX, dstY, sw, sh).Restore()
		return
	}
	d.DrawImageFrom(cache.Source, sx, sy, sw, sh, dstX, dstY, sw, sh)
}
