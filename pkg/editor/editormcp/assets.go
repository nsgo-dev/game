package editormcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/nsgo-dev/game/pkg/editor"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

const ToolAssetRead = "asset_read"

func assetTools() []Tool {
	return []Tool{{Name: ToolAssetRead, Title: "Read project assets", Description: "Read up to 12 project images as one labeled contact sheet, with source hashes and labels.", InputSchema: []byte(`{"type":"object","additionalProperties":false,"required":["paths"],"properties":{"paths":{"type":"array","minItems":1,"maxItems":12,"items":{"type":"string"}},"maxEdge":{"type":"integer","minimum":128,"maximum":1024}}}`), Action: ActionRead, Handler: assetRead}}
}

type assetReadItem struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Label  string `json:"label"`
}

func assetRead(ctx context.Context, c *Call) (*Result, error) {
	var a struct {
		Paths   []string `json:"paths"`
		MaxEdge int      `json:"maxEdge"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if len(a.Paths) == 0 || len(a.Paths) > 12 {
		return nil, fmt.Errorf("asset_read: paths must contain 1 to 12 items")
	}
	if a.MaxEdge == 0 {
		a.MaxEdge = 512
	}
	root := ""
	if err := c.Editor(ctx, func(ed *editor.Editor) error {
		if ed.ProjectPath != "" {
			root = filepath.Dir(ed.ProjectPath)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if root == "" {
		return nil, fmt.Errorf("asset_read: no project open")
	}
	imgs := make([]image.Image, 0, len(a.Paths))
	items := make([]assetReadItem, 0, len(a.Paths))
	for i, p := range a.Paths {
		p = filepath.ToSlash(filepath.Clean(p))
		if p == "." || strings.HasPrefix(p, "../") || filepath.IsAbs(p) {
			return nil, fmt.Errorf("asset_read: path %q is outside project", p)
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		img, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("asset_read: %s: %w", p, err)
		}
		imgs = append(imgs, img)
		items = append(items, assetReadItem{Path: p, SHA256: fmt.Sprintf("%x", sum[:]), Label: fmt.Sprintf("%d · %s", i+1, p)})
	}
	sheet := contactSheet(imgs, items, a.MaxEdge)
	var buf bytes.Buffer
	if err := png.Encode(&buf, sheet); err != nil {
		return nil, err
	}
	return &Result{Text: fmt.Sprintf("%d image assets in labeled contact sheet", len(items)), Structured: map[string]any{"items": items, "width": sheet.Bounds().Dx(), "height": sheet.Bounds().Dy()}, PNG: buf.Bytes()}, nil
}
func contactSheet(imgs []image.Image, items []assetReadItem, max int) image.Image {
	cell := max / 2
	if cell < 128 {
		cell = 128
	}
	out := image.NewRGBA(image.Rect(0, 0, cell*2, cell*((len(imgs)+1)/2)))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{70, 70, 70, 255}}, image.Point{}, draw.Src)
	for i, img := range imgs {
		b := img.Bounds()
		scale := float64(cell-8) / float64(maxInt(b.Dx(), b.Dy()))
		w, h := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
		x := (i%2)*cell + (cell-w)/2
		y := (i/2)*cell + (cell-h)/2
		draw.Draw(out, image.Rect(x, y, x+w, y+h), img, b.Min, draw.Over)
	}
	return out
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
