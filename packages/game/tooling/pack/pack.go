// Package pack packs a folder of PNG images into sprite atlas pages and TexturePacker JSON-hash metadata that sprite.LoadAtlas reads directly.
package pack

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultName      = "atlas"
	defaultMaxWidth  = 2048
	defaultMaxHeight = 2048
	defaultScale     = 1
)

// Options controls how images are trimmed, padded, extruded and packed into atlas pages.
type Options struct {
	// Name is the base name for each page's PNG and JSON files ("atlas" when empty).
	Name string
	// Padding is the number of transparent pixels kept between adjacent frames.
	Padding int
	// Extrude is the number of pixels each frame's border is repeated outward to prevent texture bleeding.
	Extrude int
	// MaxWidth and MaxHeight limit each atlas page in pixels (2048 when 0).
	MaxWidth  int
	MaxHeight int
	// Scale is written to meta.scale so sprite.LoadAtlas converts pixels to points (1 when 0).
	Scale int
	// Trim removes transparent borders around each input image before packing.
	Trim bool
}

// DefaultOptions returns Options with the packer's defaults: 1px padding, no extrusion, 2048x2048 pages, scale 1 and trimming enabled.
func DefaultOptions() Options {
	return Options{
		Name:      defaultName,
		Padding:   1,
		Extrude:   0,
		MaxWidth:  defaultMaxWidth,
		MaxHeight: defaultMaxHeight,
		Scale:     defaultScale,
		Trim:      true,
	}
}

// Input is one named image to pack into an atlas.
type Input struct {
	// Name is the frame name written to the atlas JSON, such as "hero.png" or "enemies/slime.png".
	Name string
	// Image is the source picture.
	Image image.Image
}

// Frame describes where one input image landed in a packed atlas page.
type Frame struct {
	// Name is the frame's key in the atlas JSON.
	Name string
	// Frame is the frame's pixel rectangle inside the atlas page image, excluding any extrusion or padding.
	Frame image.Rectangle
	// SpriteSourceSize is where the trimmed content sits inside the untrimmed source image.
	SpriteSourceSize image.Rectangle
	// SourceSize is the untrimmed source image's pixel dimensions.
	SourceSize image.Point
	// Trimmed reports whether transparent borders were removed from the source image.
	Trimmed bool
	// Page is the zero-based page index holding this frame.
	Page int
}

// Page is one packed atlas image and its TexturePacker JSON-hash metadata.
type Page struct {
	// Index is the page's zero-based index.
	Index int
	// ImageName is the PNG file name written to meta.image (for example "atlas.png" or "atlas-0.png").
	ImageName string
	// JSONName is the JSON file name (for example "atlas.json" or "atlas-0.json").
	JSONName string
	// Image is the packed RGBA8888 atlas image.
	Image *image.NRGBA
	// JSON is the encoded TexturePacker JSON-hash metadata for this page.
	JSON []byte
	// Frames lists the frames packed onto this page, sorted by Name.
	Frames []Frame
}

// Result holds all atlas pages produced by PackDir or PackImages.
type Result struct {
	Pages []*Page
}

// Write writes every page's PNG image and JSON metadata into dir, creating dir if needed, and returns the paths written.
func (r *Result) Write(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("pack: create output dir: %w", err)
	}
	var written []string
	for _, p := range r.Pages {
		imgPath := filepath.Join(dir, p.ImageName)
		if err := writePNG(imgPath, p.Image); err != nil {
			return nil, fmt.Errorf("pack: write %s: %w", imgPath, err)
		}
		written = append(written, imgPath)

		jsonPath := filepath.Join(dir, p.JSONName)
		if err := os.WriteFile(jsonPath, p.JSON, 0o644); err != nil {
			return nil, fmt.Errorf("pack: write %s: %w", jsonPath, err)
		}
		written = append(written, jsonPath)
	}
	return written, nil
}

// PackDir walks dir for PNG images in deterministic slash-separated relative path order and packs them with opts.
func PackDir(dir string, opts Options) (*Result, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("pack: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pack: %q is not a directory", dir)
	}

	var relPaths []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".png") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		relPaths = append(relPaths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pack: walk %q: %w", dir, err)
	}
	slices.Sort(relPaths)
	if len(relPaths) == 0 {
		return nil, fmt.Errorf("pack: no PNG files found in %q", dir)
	}

	inputs := make([]Input, 0, len(relPaths))
	for _, rel := range relPaths {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		img, err := readPNG(full)
		if err != nil {
			return nil, fmt.Errorf("pack: decode %q: %w", rel, err)
		}
		inputs = append(inputs, Input{Name: rel, Image: img})
	}
	return PackImages(inputs, opts)
}

// PackImages trims and packs inputs into one or more atlas pages with opts.
func PackImages(inputs []Input, opts Options) (*Result, error) {
	cfg, err := normalizeOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, errors.New("pack: no input images")
	}

	prepared := make([]preparedInput, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for i, in := range inputs {
		if in.Name == "" {
			return nil, fmt.Errorf("pack: input %d has an empty name", i)
		}
		if _, dup := seen[in.Name]; dup {
			return nil, fmt.Errorf("pack: duplicate input name %q", in.Name)
		}
		seen[in.Name] = struct{}{}
		if in.Image == nil {
			return nil, fmt.Errorf("pack: input %q has a nil image", in.Name)
		}

		b := in.Image.Bounds()
		srcSize := b.Size()
		if srcSize.X <= 0 || srcSize.Y <= 0 {
			return nil, fmt.Errorf("pack: input %q has empty bounds %v", in.Name, b)
		}

		item := preparedInput{
			name:    in.Name,
			img:     in.Image,
			srcSize: srcSize,
			trim:    image.Rect(0, 0, srcSize.X, srcSize.Y),
		}
		if cfg.Trim {
			trimRect, hasPixels := trimBounds(in.Image)
			if !hasPixels {
				item.trim = image.Rectangle{}
				item.trimmed = true
				item.empty = true
			} else {
				item.trim = trimRect
				item.trimmed = trimRect.Min.X > 0 || trimRect.Min.Y > 0 || trimRect.Dx() < srcSize.X || trimRect.Dy() < srcSize.Y
			}
		}

		if !item.empty {
			needW := item.trim.Dx() + 2*cfg.Extrude
			needH := item.trim.Dy() + 2*cfg.Extrude
			if needW > cfg.MaxWidth || needH > cfg.MaxHeight {
				return nil, fmt.Errorf("pack: image %q (%dx%d with %dpx extrusion) exceeds max page size %dx%d",
					in.Name, item.trim.Dx(), item.trim.Dy(), cfg.Extrude, cfg.MaxWidth, cfg.MaxHeight)
			}
		}
		prepared = append(prepared, item)
	}

	// Sort deterministically by largest dimension, then area, then name for high packing density.
	slices.SortFunc(prepared, func(a, b preparedInput) int {
		aMax := max(a.trim.Dx(), a.trim.Dy())
		bMax := max(b.trim.Dx(), b.trim.Dy())
		if c := cmp.Compare(bMax, aMax); c != 0 {
			return c
		}
		if c := cmp.Compare(b.trim.Dx()*b.trim.Dy(), a.trim.Dx()*a.trim.Dy()); c != 0 {
			return c
		}
		return cmp.Compare(a.name, b.name)
	})

	type openPage struct {
		bin    *bin
		placed []placedItem
		maxX   int
		maxY   int
	}
	var pages []*openPage
	var emptyItems []preparedInput

	newPage := func() *openPage {
		// Adding Padding to the bin's bounds lets a single MaxWidth x MaxHeight sprite fit while keeping Padding pixels between adjacent sprites.
		p := &openPage{bin: newBin(cfg.MaxWidth+cfg.Padding, cfg.MaxHeight+cfg.Padding)}
		pages = append(pages, p)
		return p
	}

	for _, item := range prepared {
		if item.empty {
			emptyItems = append(emptyItems, item)
			continue
		}
		blockW := item.trim.Dx() + 2*cfg.Extrude
		blockH := item.trim.Dy() + 2*cfg.Extrude
		reqW := blockW + cfg.Padding
		reqH := blockH + cfg.Padding

		var chosen *openPage
		var pos image.Point
		for _, p := range pages {
			if pt, ok := p.bin.insert(reqW, reqH); ok {
				chosen = p
				pos = pt
				break
			}
		}
		if chosen == nil {
			chosen = newPage()
			var ok bool
			pos, ok = chosen.bin.insert(reqW, reqH)
			if !ok {
				return nil, fmt.Errorf("pack: image %q could not fit in an empty %dx%d page", item.name, cfg.MaxWidth, cfg.MaxHeight)
			}
		}
		chosen.placed = append(chosen.placed, placedItem{item: item, blockPos: pos})
		chosen.maxX = max(chosen.maxX, pos.X+blockW)
		chosen.maxY = max(chosen.maxY, pos.Y+blockH)
	}

	if len(pages) == 0 {
		newPage()
	}
	for _, item := range emptyItems {
		pages[0].placed = append(pages[0].placed, placedItem{item: item})
	}

	res := &Result{Pages: make([]*Page, len(pages))}
	multi := len(pages) > 1
	for i, op := range pages {
		pageW := max(op.maxX, 1)
		pageH := max(op.maxY, 1)
		canvas := image.NewNRGBA(image.Rect(0, 0, pageW, pageH))

		frames := make([]Frame, 0, len(op.placed))
		for _, pl := range op.placed {
			var frameRect image.Rectangle
			if !pl.item.empty {
				dstPos := image.Pt(pl.blockPos.X+cfg.Extrude, pl.blockPos.Y+cfg.Extrude)
				srcMin := pl.item.img.Bounds().Min
				srcRect := pl.item.trim.Add(srcMin)
				blitWithExtrude(canvas, pl.item.img, srcRect, dstPos, cfg.Extrude)
				frameRect = image.Rect(dstPos.X, dstPos.Y, dstPos.X+pl.item.trim.Dx(), dstPos.Y+pl.item.trim.Dy())
			}
			frames = append(frames, Frame{
				Name:             pl.item.name,
				Frame:            frameRect,
				SpriteSourceSize: pl.item.trim,
				SourceSize:       pl.item.srcSize,
				Trimmed:          pl.item.trimmed,
				Page:             i,
			})
		}
		slices.SortFunc(frames, func(a, b Frame) int { return cmp.Compare(a.Name, b.Name) })

		imgName := cfg.Name + ".png"
		jsonName := cfg.Name + ".json"
		if multi {
			imgName = fmt.Sprintf("%s-%d.png", cfg.Name, i)
			jsonName = fmt.Sprintf("%s-%d.json", cfg.Name, i)
		}

		data, err := encodeAtlasJSON(imgName, image.Pt(pageW, pageH), cfg.Scale, frames)
		if err != nil {
			return nil, fmt.Errorf("pack: encode metadata for %s: %w", jsonName, err)
		}
		res.Pages[i] = &Page{
			Index:     i,
			ImageName: imgName,
			JSONName:  jsonName,
			Image:     canvas,
			JSON:      data,
			Frames:    frames,
		}
	}
	return res, nil
}

type preparedInput struct {
	name    string
	img     image.Image
	srcSize image.Point
	trim    image.Rectangle
	trimmed bool
	empty   bool
}

type placedItem struct {
	item     preparedInput
	blockPos image.Point
}

func normalizeOptions(opts Options) (Options, error) {
	if opts.Padding < 0 {
		return Options{}, fmt.Errorf("pack: padding %d must be non-negative", opts.Padding)
	}
	if opts.Extrude < 0 {
		return Options{}, fmt.Errorf("pack: extrude %d must be non-negative", opts.Extrude)
	}
	if opts.MaxWidth < 0 {
		return Options{}, fmt.Errorf("pack: max width %d must be positive", opts.MaxWidth)
	}
	if opts.MaxHeight < 0 {
		return Options{}, fmt.Errorf("pack: max height %d must be positive", opts.MaxHeight)
	}
	if opts.Scale < 0 {
		return Options{}, fmt.Errorf("pack: scale %d must be positive", opts.Scale)
	}
	if opts.Name == "" {
		opts.Name = defaultName
	}
	if opts.MaxWidth == 0 {
		opts.MaxWidth = defaultMaxWidth
	}
	if opts.MaxHeight == 0 {
		opts.MaxHeight = defaultMaxHeight
	}
	if opts.Scale == 0 {
		opts.Scale = defaultScale
	}
	return opts, nil
}

// trimBounds returns the tightest non-transparent rectangle relative to img.Bounds().Min, or false if every pixel has zero alpha.
func trimBounds(img image.Image) (image.Rectangle, bool) {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1

	switch m := img.(type) {
	case *image.NRGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - m.Rect.Min.Y) * m.Stride
			for x := b.Min.X; x < b.Max.X; x++ {
				if m.Pix[row+(x-m.Rect.Min.X)*4+3] != 0 {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	case *image.RGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			row := (y - m.Rect.Min.Y) * m.Stride
			for x := b.Min.X; x < b.Max.X; x++ {
				if m.Pix[row+(x-m.Rect.Min.X)*4+3] != 0 {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	default:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
					minX = min(minX, x)
					minY = min(minY, y)
					maxX = max(maxX, x)
					maxY = max(maxY, y)
				}
			}
		}
	}

	if maxX < minX || maxY < minY {
		return image.Rectangle{}, false
	}
	return image.Rect(minX-b.Min.X, minY-b.Min.Y, maxX+1-b.Min.X, maxY+1-b.Min.Y), true
}

// blitWithExtrude copies srcRect from src to dstPos in dst without premultiplying alpha, then repeats the outer edge pixels by extrude pixels on all sides.
func blitWithExtrude(dst *image.NRGBA, src image.Image, srcRect image.Rectangle, dstPos image.Point, extrude int) {
	w, h := srcRect.Dx(), srcRect.Dy()
	if w <= 0 || h <= 0 {
		return
	}

	if m, ok := src.(*image.NRGBA); ok {
		for y := range h {
			srcOff := m.PixOffset(srcRect.Min.X, srcRect.Min.Y+y)
			dstOff := dst.PixOffset(dstPos.X, dstPos.Y+y)
			copy(dst.Pix[dstOff:dstOff+w*4], m.Pix[srcOff:srcOff+w*4])
		}
	} else {
		for y := range h {
			for x := range w {
				c := color.NRGBAModel.Convert(src.At(srcRect.Min.X+x, srcRect.Min.Y+y)).(color.NRGBA)
				dst.SetNRGBA(dstPos.X+x, dstPos.Y+y, c)
			}
		}
	}

	if extrude <= 0 {
		return
	}

	for x := range w {
		top := dst.NRGBAAt(dstPos.X+x, dstPos.Y)
		bot := dst.NRGBAAt(dstPos.X+x, dstPos.Y+h-1)
		for e := 1; e <= extrude; e++ {
			dst.SetNRGBA(dstPos.X+x, dstPos.Y-e, top)
			dst.SetNRGBA(dstPos.X+x, dstPos.Y+h-1+e, bot)
		}
	}
	for y := range h {
		left := dst.NRGBAAt(dstPos.X, dstPos.Y+y)
		right := dst.NRGBAAt(dstPos.X+w-1, dstPos.Y+y)
		for e := 1; e <= extrude; e++ {
			dst.SetNRGBA(dstPos.X-e, dstPos.Y+y, left)
			dst.SetNRGBA(dstPos.X+w-1+e, dstPos.Y+y, right)
		}
	}

	tl := dst.NRGBAAt(dstPos.X, dstPos.Y)
	tr := dst.NRGBAAt(dstPos.X+w-1, dstPos.Y)
	bl := dst.NRGBAAt(dstPos.X, dstPos.Y+h-1)
	br := dst.NRGBAAt(dstPos.X+w-1, dstPos.Y+h-1)
	for ey := 1; ey <= extrude; ey++ {
		for ex := 1; ex <= extrude; ex++ {
			dst.SetNRGBA(dstPos.X-ex, dstPos.Y-ey, tl)
			dst.SetNRGBA(dstPos.X+w-1+ex, dstPos.Y-ey, tr)
			dst.SetNRGBA(dstPos.X-ex, dstPos.Y+h-1+ey, bl)
			dst.SetNRGBA(dstPos.X+w-1+ex, dstPos.Y+h-1+ey, br)
		}
	}
}

type jsonAtlas struct {
	Frames map[string]jsonFrame `json:"frames"`
	Meta   jsonMeta             `json:"meta"`
}

type jsonFrame struct {
	Frame            jsonRect `json:"frame"`
	Rotated          bool     `json:"rotated"`
	Trimmed          bool     `json:"trimmed"`
	SpriteSourceSize jsonRect `json:"spriteSourceSize"`
	SourceSize       jsonSize `json:"sourceSize"`
}

type jsonRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type jsonSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

type jsonMeta struct {
	App     string   `json:"app"`
	Version string   `json:"version"`
	Image   string   `json:"image"`
	Format  string   `json:"format"`
	Size    jsonSize `json:"size"`
	Scale   string   `json:"scale"`
}

func encodeAtlasJSON(imageName string, size image.Point, scale int, frames []Frame) ([]byte, error) {
	doc := jsonAtlas{
		Frames: make(map[string]jsonFrame, len(frames)),
		Meta: jsonMeta{
			App:     "gamepack",
			Version: "1",
			Image:   imageName,
			Format:  "RGBA8888",
			Size:    jsonSize{W: size.X, H: size.Y},
			Scale:   strconv.Itoa(scale),
		},
	}
	for _, f := range frames {
		doc.Frames[f.Name] = jsonFrame{
			Frame: jsonRect{
				X: f.Frame.Min.X,
				Y: f.Frame.Min.Y,
				W: f.Frame.Dx(),
				H: f.Frame.Dy(),
			},
			Rotated: false,
			Trimmed: f.Trimmed,
			SpriteSourceSize: jsonRect{
				X: f.SpriteSourceSize.Min.X,
				Y: f.SpriteSourceSize.Min.Y,
				W: f.SpriteSourceSize.Dx(),
				H: f.SpriteSourceSize.Dy(),
			},
			SourceSize: jsonSize{
				W: f.SourceSize.X,
				H: f.SourceSize.Y,
			},
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
