package text

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
)

// FieldType is how a signed-distance-field atlas stores the distance to a glyph's outline.
type FieldType uint8

const (
	// FieldSDF is a true signed distance field in one channel, read from red (a grayscale image's gray).
	FieldSDF FieldType = iota
	// FieldPSDF is a pseudo signed distance field in one channel, read from red, as FieldSDF is.
	FieldPSDF
	// FieldMSDF is a multi-channel signed distance field: the outline is the median of red, green and blue, which keeps corners sharp.
	FieldMSDF
	// FieldMTSDF is FieldMSDF with a true signed distance field in alpha, which outlines read for rounder corners.
	FieldMTSDF
)

// String returns the name msdf-atlas-gen and msdf-bmfont-xml give the field type: sdf, psdf, msdf or mtsdf.
func (t FieldType) String() string {
	switch t {
	case FieldSDF:
		return "sdf"
	case FieldPSDF:
		return "psdf"
	case FieldMSDF:
		return "msdf"
	case FieldMTSDF:
		return "mtsdf"
	}
	return fmt.Sprintf("FieldType(%d)", uint8(t))
}

func parseFieldType(s string) (FieldType, error) {
	switch s {
	case "sdf":
		return FieldSDF, nil
	case "psdf":
		return FieldPSDF, nil
	case "msdf":
		return FieldMSDF, nil
	case "mtsdf":
		return FieldMTSDF, nil
	}
	return 0, fmt.Errorf("unknown distance field type %q", s)
}

func (t FieldType) channels() int {
	switch t {
	case FieldMSDF:
		return 3
	case FieldMTSDF:
		return 4
	}
	return 1
}

// SDFFont is a signed-distance-field font: glyph metrics in atlas pixels and, for each texture page, the distance field those glyphs are drawn from.
//
// Its glyphs stay sharp at any size, because a glyph is resolved from the field at the size it is drawn at rather than stretched from one bitmap. Draw and DrawWorld keep the glyphs they resolve in a cache bounded by CacheBytes, so an SDFFont is not safe for concurrent use: draw with it from one goroutine, as a canvas's Draw does.
type SDFFont struct {
	// Face is the font face name.
	Face string
	// Size is the font's em size in atlas pixels: the size the atlas was generated at.
	Size float32
	// LineHeight is the distance between consecutive lines, in atlas pixels.
	LineHeight float32
	// Base is the distance from the top of a line to the baseline, in atlas pixels.
	Base float32
	// Field is how the atlas stores the distance.
	Field FieldType
	// DistanceRange is the width, in atlas pixels, of the band around each outline the field covers. With a DistanceRangeMiddle of 0, a field value of 0 is DistanceRange/2 pixels outside the outline, 0.5 on it and 1 DistanceRange/2 pixels inside.
	DistanceRange float32
	// DistanceRangeMiddle is how far, in atlas pixels, the middle of the field's band lies inside the outline: msdf-atlas-gen's distanceRangeMiddle, non-zero for an atlas generated with an asymmetric range (-apxrange or -aemrange). The outline is at the field value 0.5 - DistanceRangeMiddle/DistanceRange, and the field reaches DistanceRange/2 - DistanceRangeMiddle atlas pixels outside it.
	DistanceRangeMiddle float32
	// Glyphs maps each rune to its metrics and its rectangle in its page, all in atlas pixels.
	//
	// Measure, Layout, Draw and LayoutWorld read Size, LineHeight, Base, Glyphs and Kernings on every call, and a glyph's image is cached under its rectangle and the field's settings, so these fields can be changed between calls.
	Glyphs map[rune]Glyph
	// Kernings maps pairs of consecutive runes to their horizontal kerning adjustment, in atlas pixels.
	Kernings map[KerningPair]float32
	// CacheBytes bounds the memory of resolved glyph images kept between draws; 0 means 16 MiB. The least recently drawn glyph images go first.
	CacheBytes int

	pages   map[int]*fieldPage
	cache   glyphCache
	missing missingGlyphState
}

// layoutFont returns the bitmap-font view of f's metrics that lays its text out, in atlas pixels.
func (f *SDFFont) layoutFont() *Font {
	return &Font{Face: f.Face, Size: f.Size, LineHeight: f.LineHeight, Base: f.Base, Glyphs: f.Glyphs, Kernings: f.Kernings, missing: &f.missing}
}

// fieldPage is one texture page's distance field: channels bytes per texel, row by row.
type fieldPage struct {
	w, h     int
	channels int
	pix      []uint8
}

// Kerning returns the horizontal kerning adjustment, in atlas pixels, when second follows first, or 0 if no kerning pair is defined.
func (f *SDFFont) Kerning(first, second rune) float32 {
	if f == nil || len(f.Kernings) == 0 {
		return 0
	}
	return f.Kernings[KerningPair{First: first, Second: second}]
}

// LoadSDFFont parses a signed-distance-field font in any of the forms the common generators write:
//
//   - msdf-atlas-gen's JSON (-json), whose glyphs are named by Unicode code point (-charset or the default, not -glyphset);
//   - AngelCode BMFont in plain text, XML or JSON with a distanceField record, as msdf-bmfont-xml writes.
//
// For each texture page, in ascending page ID order, page is called with the page ID and the file the font names for it (empty for msdf-atlas-gen's single atlas) and returns the page's decoded image, such as image/png's. LoadSDFFont reads the distance out of the images, so it does not keep them.
func LoadSDFFont(data []byte, page func(id int, file string) (image.Image, error)) (*SDFFont, error) {
	if page == nil {
		return nil, fmt.Errorf("text: sdf font: nil page loader")
	}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("text: sdf font: empty data")
	}
	var (
		f   *SDFFont
		err error
	)
	switch trimmed[0] {
	case '{':
		f, err = parseSDFJSON(trimmed, page)
	case '<':
		var raw bmfontData
		raw, err = parseBMFontXML(trimmed)
		if err == nil {
			f, err = buildSDFBMFont(raw, page)
		}
	default:
		var raw bmfontData
		raw, err = parseBMFontText(trimmed)
		if err == nil {
			f, err = buildSDFBMFont(raw, page)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("text: sdf font: %w", err)
	}
	return f, nil
}

// LoadSDFFontImage parses a single-page signed-distance-field font, as LoadSDFFont does, whose page is img. It returns an error if the font declares more than one page.
func LoadSDFFontImage(img image.Image, data []byte) (*SDFFont, error) {
	f, err := LoadSDFFont(data, func(int, string) (image.Image, error) { return img, nil })
	if err != nil {
		return nil, err
	}
	if len(f.pages) > 1 {
		return nil, fmt.Errorf("text: sdf font: font has %d pages; use LoadSDFFont", len(f.pages))
	}
	return f, nil
}

// --- msdf-atlas-gen JSON ---

type atlasGenJSON struct {
	Atlas *struct {
		Type          string   `json:"type"`
		DistanceRange *float32 `json:"distanceRange"`
		RangeMiddle   float32  `json:"distanceRangeMiddle"`
		Size          float32  `json:"size"`
		Width         int      `json:"width"`
		Height        int      `json:"height"`
		YOrigin       string   `json:"yOrigin"`
	} `json:"atlas"`
	Name    string `json:"name"`
	Metrics *struct {
		EmSize     float32 `json:"emSize"`
		LineHeight float32 `json:"lineHeight"`
		Ascender   float32 `json:"ascender"`
		Descender  float32 `json:"descender"`
	} `json:"metrics"`
	Glyphs []struct {
		Unicode     *int            `json:"unicode"`
		Index       *int            `json:"index"`
		Advance     float32         `json:"advance"`
		PlaneBounds *atlasGenBounds `json:"planeBounds"`
		AtlasBounds *atlasGenBounds `json:"atlasBounds"`
	} `json:"glyphs"`
	Kerning []struct {
		Unicode1 *int    `json:"unicode1"`
		Unicode2 *int    `json:"unicode2"`
		Advance  float32 `json:"advance"`
	} `json:"kerning"`
	Variants json.RawMessage `json:"variants"`
	// Fields of msdf-bmfont-xml's JSON, which LoadSDFFont reads as BMFont.
	Chars json.RawMessage `json:"chars"`
}

type atlasGenBounds struct {
	Left   float32 `json:"left"`
	Bottom float32 `json:"bottom"`
	Right  float32 `json:"right"`
	Top    float32 `json:"top"`
}

func parseSDFJSON(data []byte, page func(int, string) (image.Image, error)) (*SDFFont, error) {
	var doc atlasGenJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Atlas == nil {
		if doc.Chars != nil {
			raw, err := parseBMFontJSON(data)
			if err != nil {
				return nil, err
			}
			return buildSDFBMFont(raw, page)
		}
		return nil, fmt.Errorf("JSON has neither msdf-atlas-gen's atlas nor BMFont's chars")
	}
	if doc.Variants != nil {
		return nil, fmt.Errorf("msdf-atlas-gen: multiple font variants are not supported")
	}
	a := doc.Atlas
	field, err := parseFieldType(a.Type)
	if err != nil {
		return nil, fmt.Errorf("msdf-atlas-gen: %w (a hardmask or softmask atlas is not a distance field)", err)
	}
	if a.DistanceRange == nil || !(*a.DistanceRange > 0) || !finite32(*a.DistanceRange) {
		return nil, fmt.Errorf("msdf-atlas-gen: distanceRange must be positive")
	}
	if !finite32(a.RangeMiddle) || math.Abs(float64(a.RangeMiddle)) >= float64(*a.DistanceRange)/2 {
		return nil, fmt.Errorf("msdf-atlas-gen: distanceRangeMiddle %v must lie within half the distanceRange %v", a.RangeMiddle, *a.DistanceRange)
	}
	if !(a.Size > 0) || !finite32(a.Size) {
		return nil, fmt.Errorf("msdf-atlas-gen: size %v must be positive", a.Size)
	}
	if a.Width <= 0 || a.Height <= 0 {
		return nil, fmt.Errorf("msdf-atlas-gen: atlas size %dx%d must be positive", a.Width, a.Height)
	}
	// msdf-atlas-gen writes a top-origin atlas (-yorigin top) with every y negated: atlasBounds measured down from the atlas's top, and planeBounds and the metrics measured down from the baseline. topDown turns them back into the bottom-origin form read below.
	var topDown bool
	switch a.YOrigin {
	case "bottom", "":
	case "top":
		topDown = true
	default:
		return nil, fmt.Errorf("msdf-atlas-gen: unknown yOrigin %q", a.YOrigin)
	}
	if doc.Metrics == nil {
		return nil, fmt.Errorf("msdf-atlas-gen: missing metrics")
	}
	m := doc.Metrics
	if topDown {
		m.Ascender, m.Descender = -m.Ascender, -m.Descender
	}
	emSize := m.EmSize
	if emSize == 0 {
		emSize = 1
	}
	if !(emSize > 0) || !finite32(emSize) {
		return nil, fmt.Errorf("msdf-atlas-gen: emSize %v must be positive", m.EmSize)
	}
	// k converts the metrics' units (emSize to an em) into atlas pixels.
	k := a.Size / emSize
	if !(m.LineHeight > 0) || !finite32(m.LineHeight) {
		return nil, fmt.Errorf("msdf-atlas-gen: lineHeight %v must be positive", m.LineHeight)
	}
	if !finite32(m.Ascender) {
		return nil, fmt.Errorf("msdf-atlas-gen: ascender %v is not finite", m.Ascender)
	}
	if len(doc.Glyphs) == 0 {
		return nil, fmt.Errorf("msdf-atlas-gen: no glyphs defined")
	}

	img, err := page(0, "")
	if err != nil {
		return nil, fmt.Errorf("page 0: %w", err)
	}
	fp, err := readFieldPage(img, field)
	if err != nil {
		return nil, fmt.Errorf("page 0: %w", err)
	}
	if fp.w != a.Width || fp.h != a.Height {
		return nil, fmt.Errorf("page 0 image is %dx%d, atlas declares %dx%d", fp.w, fp.h, a.Width, a.Height)
	}

	f := &SDFFont{
		Face:                doc.Name,
		Size:                a.Size,
		LineHeight:          m.LineHeight * k,
		Base:                m.Ascender * k,
		Field:               field,
		DistanceRange:       *a.DistanceRange,
		DistanceRangeMiddle: a.RangeMiddle,
		Glyphs:              make(map[rune]Glyph, len(doc.Glyphs)),
		pages:               map[int]*fieldPage{0: fp},
	}
	for i, g := range doc.Glyphs {
		if g.Unicode == nil {
			if g.Index != nil {
				return nil, fmt.Errorf("msdf-atlas-gen: glyph %d is named by glyph index; generate the atlas with -charset", i)
			}
			return nil, fmt.Errorf("msdf-atlas-gen: glyph %d missing unicode", i)
		}
		r := rune(*g.Unicode)
		if *g.Unicode < 0 || *g.Unicode > math.MaxInt32 {
			return nil, fmt.Errorf("msdf-atlas-gen: glyph %d has invalid unicode %d", i, *g.Unicode)
		}
		if _, dup := f.Glyphs[r]; dup {
			return nil, fmt.Errorf("msdf-atlas-gen: duplicate glyph %d", *g.Unicode)
		}
		if !finite32(g.Advance) {
			return nil, fmt.Errorf("msdf-atlas-gen: glyph %d advance is not finite", *g.Unicode)
		}
		glyph := Glyph{ID: r, XAdvance: g.Advance * k}
		glyph.Frame.Name = glyphFrameName(r)
		if (g.PlaneBounds == nil) != (g.AtlasBounds == nil) {
			return nil, fmt.Errorf("msdf-atlas-gen: glyph %d has only one of planeBounds and atlasBounds", *g.Unicode)
		}
		if g.AtlasBounds != nil {
			ab, pb := *g.AtlasBounds, *g.PlaneBounds
			if !finiteBounds(ab) || !finiteBounds(pb) {
				return nil, fmt.Errorf("msdf-atlas-gen: glyph %d bounds are not finite", *g.Unicode)
			}
			if topDown {
				ab.Bottom, ab.Top = float32(a.Height)-ab.Bottom, float32(a.Height)-ab.Top
				pb.Bottom, pb.Top = -pb.Bottom, -pb.Top
			}
			x, w := ab.Left, ab.Right-ab.Left
			y, h := float32(a.Height)-ab.Top, ab.Top-ab.Bottom
			if w < 0 || h < 0 || x < 0 || y < 0 || x+w > float32(a.Width) || y+h > float32(a.Height) {
				return nil, fmt.Errorf("msdf-atlas-gen: glyph %d atlas bounds (%v,%v,%v,%v) lie outside the %dx%d atlas", *g.Unicode, x, y, w, h, a.Width, a.Height)
			}
			glyph.Frame.X, glyph.Frame.Y, glyph.Frame.W, glyph.Frame.H = x, y, w, h
			glyph.Frame.Width, glyph.Frame.Height = w, h
			glyph.XOffset = pb.Left * k
			glyph.YOffset = (m.Ascender - pb.Top) * k
		}
		f.Glyphs[r] = glyph
	}
	if len(doc.Kerning) > 0 {
		f.Kernings = make(map[KerningPair]float32, len(doc.Kerning))
		for i, kp := range doc.Kerning {
			if kp.Unicode1 == nil || kp.Unicode2 == nil {
				return nil, fmt.Errorf("msdf-atlas-gen: kerning %d missing unicode1 or unicode2", i)
			}
			if *kp.Unicode1 < 0 || *kp.Unicode2 < 0 || *kp.Unicode1 > math.MaxInt32 || *kp.Unicode2 > math.MaxInt32 || !finite32(kp.Advance) {
				return nil, fmt.Errorf("msdf-atlas-gen: invalid kerning %d", i)
			}
			f.Kernings[KerningPair{First: rune(*kp.Unicode1), Second: rune(*kp.Unicode2)}] = kp.Advance * k
		}
	}
	return f, nil
}

func finiteBounds(b atlasGenBounds) bool {
	return finite32(b.Left) && finite32(b.Bottom) && finite32(b.Right) && finite32(b.Top)
}

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// --- BMFont with a distance field (msdf-bmfont-xml) ---

type bmfontJSON struct {
	Info *struct {
		Face string  `json:"face"`
		Size float32 `json:"size"`
	} `json:"info"`
	Common *struct {
		LineHeight float32 `json:"lineHeight"`
		Base       float32 `json:"base"`
		Pages      int     `json:"pages"`
	} `json:"common"`
	Pages []string `json:"pages"`
	Chars []struct {
		ID       *int    `json:"id"`
		X        float32 `json:"x"`
		Y        float32 `json:"y"`
		Width    float32 `json:"width"`
		Height   float32 `json:"height"`
		XOffset  float32 `json:"xoffset"`
		YOffset  float32 `json:"yoffset"`
		XAdvance float32 `json:"xadvance"`
		Page     int     `json:"page"`
	} `json:"chars"`
	Kernings []struct {
		First  *int    `json:"first"`
		Second *int    `json:"second"`
		Amount float32 `json:"amount"`
	} `json:"kernings"`
	DistanceField *struct {
		FieldType     string  `json:"fieldType"`
		DistanceRange float32 `json:"distanceRange"`
	} `json:"distanceField"`
}

func parseBMFontJSON(data []byte) (bmfontData, error) {
	var doc bmfontJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return bmfontData{}, err
	}
	var out bmfontData
	if doc.Info != nil {
		out.face = doc.Info.Face
		out.size = doc.Info.Size
	}
	if doc.Common != nil {
		out.hasCommon = true
		out.lineHeight = doc.Common.LineHeight
		out.base = doc.Common.Base
		out.expectedPages = doc.Common.Pages
	}
	for i, file := range doc.Pages {
		out.pages = append(out.pages, bmfontPage{id: i, file: file})
	}
	for i, c := range doc.Chars {
		if c.ID == nil {
			return bmfontData{}, fmt.Errorf("char %d missing id", i)
		}
		out.chars = append(out.chars, bmfontChar{
			id: *c.ID, x: c.X, y: c.Y, w: c.Width, h: c.Height,
			xoffset: c.XOffset, yoffset: c.YOffset, xadvance: c.XAdvance, page: c.Page,
		})
	}
	for i, k := range doc.Kernings {
		if k.First == nil || k.Second == nil {
			return bmfontData{}, fmt.Errorf("kerning %d missing first or second", i)
		}
		out.kernings = append(out.kernings, bmfontKerning{first: *k.First, second: *k.Second, amount: k.Amount})
	}
	if doc.DistanceField != nil {
		out.field = &bmfontField{fieldType: doc.DistanceField.FieldType, distanceRange: doc.DistanceField.DistanceRange}
	}
	return out, nil
}

func buildSDFBMFont(raw bmfontData, page func(int, string) (image.Image, error)) (*SDFFont, error) {
	if raw.field == nil {
		return nil, fmt.Errorf("bmfont: missing distanceField record; the font is a bitmap font, load it with LoadBMFont")
	}
	field, err := parseFieldType(raw.field.fieldType)
	if err != nil {
		return nil, fmt.Errorf("bmfont: distanceField: %w", err)
	}
	if !(raw.field.distanceRange > 0) || !finite32(raw.field.distanceRange) {
		return nil, fmt.Errorf("bmfont: distanceField distanceRange %v must be positive", raw.field.distanceRange)
	}
	bm, err := buildBMFont(raw, nil)
	if err != nil {
		return nil, err
	}
	for r, g := range bm.Glyphs {
		if !finite32(g.Frame.X+g.Frame.Y+g.Frame.W+g.Frame.H) || !finite32(g.XOffset+g.YOffset+g.XAdvance) {
			return nil, fmt.Errorf("bmfont: char %d metrics are not finite", r)
		}
	}
	if !finite32(bm.LineHeight) || !finite32(bm.Base) {
		return nil, fmt.Errorf("bmfont: common metrics are not finite")
	}
	ids := make([]int, 0, len(raw.pages))
	files := make(map[int]string, len(raw.pages))
	for _, p := range raw.pages {
		ids = append(ids, p.id)
		files[p.id] = p.file
	}
	slices.Sort(ids)
	f := &SDFFont{
		Face:          bm.Face,
		Size:          bm.Size,
		LineHeight:    bm.LineHeight,
		Base:          bm.Base,
		Field:         field,
		DistanceRange: raw.field.distanceRange,
		Glyphs:        bm.Glyphs,
		Kernings:      bm.Kernings,
		pages:         make(map[int]*fieldPage, len(ids)),
	}
	if !finite32(f.Size) {
		return nil, fmt.Errorf("bmfont: info size %v is not finite", f.Size)
	}
	// BMFont writes a negative size when the size is the characters' height rather than the em's; either way it stands for the em.
	f.Size = float32(math.Abs(float64(f.Size)))
	if f.Size == 0 {
		// msdf-bmfont-xml always writes the size it rendered at; without one, the line height stands in for the em.
		f.Size = f.LineHeight
	}
	for _, id := range ids {
		img, err := page(id, files[id])
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", id, err)
		}
		fp, err := readFieldPage(img, field)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", id, err)
		}
		f.pages[id] = fp
	}
	for r, g := range f.Glyphs {
		fp := f.pages[g.Page]
		if g.Frame.X+g.Frame.W > float32(fp.w) || g.Frame.Y+g.Frame.H > float32(fp.h) {
			return nil, fmt.Errorf("bmfont: char %d (%v,%v,%v,%v) lies outside page %d's %dx%d image", r, g.Frame.X, g.Frame.Y, g.Frame.W, g.Frame.H, g.Page, fp.w, fp.h)
		}
	}
	return f, nil
}

// readFieldPage copies the channels of img that field reads into a compact field page.
func readFieldPage(img image.Image, field FieldType) (*fieldPage, error) {
	if img == nil {
		return nil, fmt.Errorf("no image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("empty image")
	}
	ch := field.channels()
	fp := &fieldPage{w: w, h: h, channels: ch, pix: make([]uint8, w*h*ch)}
	i := 0
	put := func(c color.NRGBA) {
		switch ch {
		case 1:
			fp.pix[i] = c.R
		case 3:
			fp.pix[i], fp.pix[i+1], fp.pix[i+2] = c.R, c.G, c.B
		default:
			fp.pix[i], fp.pix[i+1], fp.pix[i+2], fp.pix[i+3] = c.R, c.G, c.B, c.A
		}
		i += ch
	}
	switch src := img.(type) {
	case *image.Gray:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				v := src.GrayAt(x, y).Y
				put(color.NRGBA{R: v, G: v, B: v, A: 0xff})
			}
		}
	case *image.NRGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				put(src.NRGBAAt(x, y))
			}
		}
	default:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				put(color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA))
			}
		}
	}
	return fp, nil
}
