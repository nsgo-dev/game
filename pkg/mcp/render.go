package mcp

import (
	"bytes"
	"fmt"
	"image/png"

	"github.com/nsgo-dev/nsgo/core"
)

// RenderedPNG is a drawing RenderPNG rasterized: the PNG and what the software rasterizer could not draw, so a caller can say the picture is partial.
type RenderedPNG struct {
	// PNG is the encoded image.
	PNG []byte
	// Width and Height are the image's size in pixels.
	Width, Height int
	// SkippedOps counts the drawing operations the rasterizer does not support, such as text, by their core.DrawKind name.
	SkippedOps map[string]int
	// SkippedImages counts the images it could not draw because their source is not in memory (a URI, asset: or offscreen source rather than Pixels or Data).
	SkippedImages int
}

// RenderPNG plays ops, a recorded core.Drawing's operations, into a width by height pixel image over background and encodes it as PNG, with the software rasterizer game_screenshot uses when it draws a frame itself, so tools outside a running game, such as the editor's viewport_image, get the same picture on every platform and headless. Zero background leaves the image transparent. It refuses a size that is not positive, exceeds 8192 pixels on a side or 4096 × 4096 pixels in all.
func RenderPNG(ops []core.DrawOp, width, height int, background core.Color) (RenderedPNG, error) {
	if err := checkScreenshotDims(width, height); err != nil {
		return RenderedPNG{}, err
	}
	if width <= 0 || height <= 0 {
		return RenderedPNG{}, fmt.Errorf("mcp: render size %dx%d must be positive", width, height)
	}
	img, skipped := rasterizeOps(width, height, ops, background)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return RenderedPNG{}, fmt.Errorf("mcp: encode png: %w", err)
	}
	return RenderedPNG{PNG: buf.Bytes(), Width: width, Height: height, SkippedOps: skipped.ops, SkippedImages: skipped.images}, nil
}
