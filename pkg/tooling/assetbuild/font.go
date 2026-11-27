package assetbuild

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/importsettings"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/tooling/atlaspack"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// fontSettings is what a font target's key covers: the settings it bakes with, after its font's import settings are applied.
type fontSettings struct {
	Size    int
	Chars   string
	Padding int
}

// glyphFontSettings is what a glyph-image font target's key covers: the settings it bakes with, after the folder's import settings sidecar is applied. JSON orders a map's keys, so the key takes Glyphs in a fixed order.
type glyphFontSettings struct {
	Spacing int
	Align   string
	Padding int
	Glyphs  map[string]string
}

// maxFontPage bounds each page of a baked font, as game pack bounds an atlas page by default; a font's size is bounded by it too.
const maxFontPage = project.MaxFontSize

func (b *Builder) font(t project.FontTarget) (prepared, error) {
	if t.GlyphFolder() {
		return b.glyphFont(t)
	}
	settings, sc, err := b.sidecar(t.In)
	if err != nil {
		return prepared{}, err
	}
	if err := onlySection(settings, "font", t.In, "a font target bakes it as a font"); err != nil {
		return prepared{}, err
	}
	if settings != nil && settings.Font != nil {
		s := settings.Font
		if s.Size != 0 {
			t.Size = s.Size
		}
		if s.Chars != "" {
			t.Chars = s.Chars
		}
		if s.Padding != nil {
			t.Padding = s.Padding
		}
	}
	fs := fontSettings{Size: t.Size, Chars: t.Chars, Padding: 1}
	if fs.Size == 0 {
		fs.Size = project.DefaultFontSize
	}
	if fs.Chars == "" {
		fs.Chars = project.DefaultFontChars
	}
	if t.Padding != nil {
		fs.Padding = *t.Padding
	}
	// The key takes the characters sorted and once each, so reordering chars rebuilds nothing.
	fs.Chars = sortedChars(fs.Chars)
	runes := []rune(fs.Chars)
	k, err := newKeyer(b, KindFont, fs)
	if err != nil {
		return prepared{}, err
	}
	data, err := os.ReadFile(b.abs(t.In))
	if err != nil {
		return prepared{}, err
	}
	k.input(t.In, data)
	if sc != nil {
		k.input(importsettings.Path(t.In), sc)
	}
	return prepared{key: k.key(), build: func(root string) ([]string, error) {
		_, name := t.FontPages()
		baked, err := bakeFont(data, name, runes, fs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.In, err)
		}
		return writeBakedFont(root, t, baked)
	}}, nil
}

// glyphImage is one PNG image of a glyph-image font target: the character it draws, its slash-separated path relative to the target's folder and its content.
type glyphImage struct {
	r    rune
	name string
	data []byte
}

// glyphFont prepares a font target that bakes a folder of PNG glyph images, as font bakes a TTF or OTF target: each image is one glyph, a file whose base name's stem is one character draws it and glyphs maps every other name to its character.
func (b *Builder) glyphFont(t project.FontTarget) (prepared, error) {
	settings, sc, err := b.sidecar(t.In)
	if err != nil {
		return prepared{}, err
	}
	if err := onlySection(settings, "font", t.In, "a font target bakes it as a font"); err != nil {
		return prepared{}, err
	}
	if settings != nil && settings.Font != nil {
		s := settings.Font
		if s.Size != 0 || s.Chars != "" {
			return prepared{}, fmt.Errorf("%s: size and chars pick the glyphs of a TTF or OTF font, and %s is a folder of glyph images, whose files pick them; only padding applies", importsettings.Path(t.In), t.In)
		}
		if s.Padding != nil {
			t.Padding = s.Padding
		}
	}
	gs := glyphFontSettings{Spacing: t.Spacing, Align: t.Align, Padding: 1, Glyphs: t.Glyphs}
	if gs.Align == "" {
		gs.Align = "bottom"
	}
	if t.Padding != nil {
		gs.Padding = *t.Padding
	}
	k, err := newKeyer(b, KindFont, gs)
	if err != nil {
		return prepared{}, err
	}
	names, err := glyphImages(b.abs(t.In))
	if err != nil {
		return prepared{}, err
	}
	glyphs := make([]glyphImage, len(names))
	byChar := make(map[rune]string, len(names))
	for i, name := range names {
		r, err := glyphRune(name, gs.Glyphs)
		if err != nil {
			return prepared{}, fmt.Errorf("%s: %w", t.In, err)
		}
		if prev, dup := byChar[r]; dup {
			return prepared{}, fmt.Errorf("%s: %s and %s are both glyphs of %q; remove one of them", t.In, prev, name, string(r))
		}
		data, err := os.ReadFile(b.abs(path.Join(t.In, name)))
		if err != nil {
			return prepared{}, err
		}
		k.input(path.Join(t.In, name), data)
		byChar[r] = name
		glyphs[i] = glyphImage{r: r, name: name, data: data}
	}
	for name := range gs.Glyphs {
		if !slices.Contains(names, name) {
			return prepared{}, fmt.Errorf("%s: glyphs maps %s, which is not in the folder; remove the entry or add the image", t.In, name)
		}
	}
	if sc != nil {
		k.input(importsettings.Path(t.In), sc)
	}
	return prepared{key: k.key(), build: func(root string) ([]string, error) {
		_, name := t.FontPages()
		baked, err := bakeGlyphFont(glyphs, name, gs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.In, err)
		}
		return writeBakedFont(root, t, baked)
	}}, nil
}

// writeBakedFont writes a baked font's .fnt file and its pages under root, as the target's Out and FontPages say, and returns the files it wrote.
func writeBakedFont(root string, t project.FontTarget, baked *bakedFont) ([]string, error) {
	dir, _ := t.FontPages()
	dest := under(root, t.Out)
	if err := writeFile(dest, baked.fnt); err != nil {
		return nil, err
	}
	outs := []string{dest}
	for i, page := range baked.pages {
		p := under(root, path.Join(dir, baked.pageNames[i]))
		if err := writeFile(p, page); err != nil {
			return nil, err
		}
		outs = append(outs, p)
	}
	return outs, nil
}

// glyphImages returns the slash-separated paths relative to dir of the PNG glyph images in it, sorted, skipping files and folders whose names start with a dot and import settings sidecars, as pack.ScanDir does, and refusing any other file, such as an editor export left beside the images. It fails when dir is not a directory or holds no PNG image.
func glyphImages(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("glyph folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("glyph folder: %q is not a directory", dir)
	}
	var names []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || importsettings.IsSidecar(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.EqualFold(filepath.Ext(d.Name()), ".png") {
			return fmt.Errorf("glyph folder: %s is not a PNG image; only PNG glyph images and their sidecars go in the folder", rel)
		}
		names = append(names, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("glyph folder: no PNG images found in %q", dir)
	}
	return names, nil
}

// glyphRune returns the character the glyph image at name draws: its glyphs entry, or its base name's stem when that is one character. An unmapped name that is not one character and a control character are refused.
func glyphRune(name string, glyphs map[string]string) (rune, error) {
	if chars, ok := glyphs[name]; ok {
		// validateFontGlyphs checked each entry maps to one character.
		return []rune(chars)[0], nil
	}
	base := path.Base(name)
	stem := strings.TrimSuffix(base, path.Ext(base))
	rs := []rune(stem)
	if len(rs) == 1 && !unicode.IsControl(rs[0]) {
		return rs[0], nil
	}
	if len(rs) == 1 {
		return 0, fmt.Errorf("%s maps to a control character; rename it or map it with glyphs", name)
	}
	return 0, fmt.Errorf("%s is not one character and no glyphs entry maps it; add one, such as glyphs: {%q: \"+\"}", name, name)
}

// onlySection refuses a sidecar of the asset at rel that holds an import section other than want, saying why with what, such as "a font target bakes it as a font". A nil f or one holding only a credit passes.
func onlySection(f *importsettings.File, want, rel, what string) error {
	if f == nil {
		return nil
	}
	for _, s := range f.Sections() {
		if s != want {
			return fmt.Errorf("%s holds %s settings, but %s", importsettings.Path(rel), s, what)
		}
	}
	return nil
}

// bakedFont is a font baked into AngelCode BMFont text and its PNG pages.
type bakedFont struct {
	fnt       []byte
	pages     [][]byte
	pageNames []string
}

// fontGlyph is one character's glyph as baked: its image, nil for one with no ink such as the space, and its metrics in pixels.
type fontGlyph struct {
	r        rune
	index    sfnt.GlyphIndex
	img      *image.NRGBA
	xoffset  int
	yoffset  int
	xadvance int
}

// bakeFont draws each of runes the font in data has a glyph for at s.Size pixels, packs the glyphs onto pages named after name, and writes the BMFont text that text.LoadBMFont reads, with the font's kerning between the baked characters.
func bakeFont(data []byte, name string, runes []rune, s fontSettings) (*bakedFont, error) {
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(s.Size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer func() { _ = face.Close() }()
	var buf sfnt.Buffer
	family, err := f.Name(&buf, sfnt.NameIDFamily)
	if err != nil {
		family = strings.TrimSuffix(name, path.Ext(name))
	}
	family = bmfontFaceName(family)
	m := face.Metrics()
	ascent := m.Ascent.Ceil()
	lineHeight := m.Height.Ceil()
	if lineHeight <= 0 {
		lineHeight = (m.Ascent + m.Descent).Ceil()
	}
	if lineHeight <= 0 {
		return nil, fmt.Errorf("font reports no line height at size %d", s.Size)
	}

	var glyphs []fontGlyph
	var inputs []atlaspack.Input
	for _, r := range runes {
		gi, err := f.GlyphIndex(&buf, r)
		if err != nil || gi == 0 {
			continue // the font has no glyph for r
		}
		dr, mask, maskp, adv, ok := face.Glyph(fixed.P(0, ascent), r)
		if !ok {
			continue
		}
		g := fontGlyph{r: r, index: gi, xoffset: dr.Min.X, yoffset: dr.Min.Y, xadvance: adv.Round()}
		if !dr.Empty() {
			// The mask is valid only until the next Glyph call, so copy it now, as white ink with the mask as alpha.
			img := image.NewNRGBA(image.Rect(0, 0, dr.Dx(), dr.Dy()))
			for y := range dr.Dy() {
				for x := range dr.Dx() {
					_, _, _, a := mask.At(maskp.X+x, maskp.Y+y).RGBA()
					img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(a >> 8)})
				}
			}
			g.img = img
			inputs = append(inputs, atlaspack.Input{Name: frameName(r), Image: img})
		}
		glyphs = append(glyphs, g)
	}
	if len(glyphs) == 0 {
		return nil, fmt.Errorf("the font has a glyph for none of the %d characters to bake", len(runes))
	}

	frames := map[string]atlaspack.Frame{}
	var pageImages []*image.NRGBA
	var pageNames []string
	if len(inputs) == 0 {
		// Every glyph is blank, such as when only the space is baked, so the one page holds nothing.
		pageImages = []*image.NRGBA{image.NewNRGBA(image.Rect(0, 0, 1, 1))}
		pageNames = []string{name + ".png"}
	} else {
		res, err := atlaspack.PackImages(inputs, atlaspack.Options{Name: name, Padding: s.Padding, MaxWidth: maxFontPage, MaxHeight: maxFontPage, Scale: 1})
		if err != nil {
			return nil, err
		}
		for _, p := range res.Pages {
			pageImages = append(pageImages, p.Image)
			pageNames = append(pageNames, p.ImageName)
			for _, fr := range p.Frames {
				frames[fr.Name] = fr
			}
		}
	}

	var scaleW, scaleH int
	for _, img := range pageImages {
		scaleW = max(scaleW, img.Bounds().Dx())
		scaleH = max(scaleH, img.Bounds().Dy())
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "info face=\"%s\" size=%d bold=0 italic=0 charset=\"\" unicode=1 stretchH=100 smooth=1 aa=1 padding=0,0,0,0 spacing=%d,%d\n", family, s.Size, s.Padding, s.Padding)
	fmt.Fprintf(&out, "common lineHeight=%d base=%d scaleW=%d scaleH=%d pages=%d packed=0\n", lineHeight, ascent, scaleW, scaleH, len(pageImages))
	for i, n := range pageNames {
		fmt.Fprintf(&out, "page id=%d file=\"%s\"\n", i, n)
	}
	fmt.Fprintf(&out, "chars count=%d\n", len(glyphs))
	for _, g := range glyphs {
		var r image.Rectangle
		page := 0
		if g.img != nil {
			fr := frames[frameName(g.r)]
			r, page = fr.Frame, fr.Page
		}
		fmt.Fprintf(&out, "char id=%d x=%d y=%d width=%d height=%d xoffset=%d yoffset=%d xadvance=%d page=%d chnl=15\n", g.r, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), g.xoffset, g.yoffset, g.xadvance, page)
	}
	type kerning struct {
		first, second rune
		amount        int
	}
	var kernings []kerning
	// opentype's Face.Kern scales by the font's units per em rather than the face's size (golang.org/x/image v0.48.0 font/opentype/opentype.go:145), so the font is asked directly, at the baked size, with each glyph's index looked up once; every ordered pair is still asked, so a chars list of n characters costs n*n lookups (BenchmarkBakeFontKerning).
	ppem := fixed.I(s.Size)
	for _, a := range glyphs {
		for _, c := range glyphs {
			k, err := f.Kern(&buf, a.index, c.index, ppem, font.HintingFull)
			if errors.Is(err, sfnt.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("kerning of %q and %q: %w", a.r, c.r, err)
			}
			if k := k.Round(); k != 0 {
				kernings = append(kernings, kerning{a.r, c.r, k})
			}
		}
	}
	if len(kernings) > 0 {
		fmt.Fprintf(&out, "kernings count=%d\n", len(kernings))
		for _, k := range kernings {
			fmt.Fprintf(&out, "kerning first=%d second=%d amount=%d\n", k.first, k.second, k.amount)
		}
	}

	baked := &bakedFont{fnt: out.Bytes(), pageNames: pageNames}
	for _, img := range pageImages {
		var pb bytes.Buffer
		if err := png.Encode(&pb, img); err != nil {
			return nil, err
		}
		baked.pages = append(baked.pages, pb.Bytes())
	}
	return baked, nil
}

// sortedChars returns the characters of chars sorted by code point, each once.
func sortedChars(chars string) string {
	runes := []rune(chars)
	slices.Sort(runes)
	return string(slices.Compact(runes))
}

// bmfontFaceName cleans a face name for the BMFont text form, which has no escape for a quote inside a quoted value and writes each record on one line.
func bmfontFaceName(family string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '"':
			return '\''
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, family)
}

// bakeGlyphFont packs the glyph images named after name and writes the BMFont text that text.LoadBMFont reads. Each glyph is its image at its own pixels: the line height and baseline are the tallest image's height, the advance is the image's width plus s.Spacing and the vertical offset places it in the line as s.Align says. Hand-drawn glyph images carry no kerning, so the .fnt has none.
func bakeGlyphFont(glyphs []glyphImage, name string, s glyphFontSettings) (*bakedFont, error) {
	// The characters come out sorted by code point, as bakeFont writes them.
	slices.SortFunc(glyphs, func(a, b glyphImage) int { return cmp.Compare(a.r, b.r) })
	inputs := make([]atlaspack.Input, 0, len(glyphs))
	sizes := make([]image.Point, len(glyphs))
	for i, g := range glyphs {
		img, err := png.Decode(bytes.NewReader(g.data))
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", g.name, err)
		}
		b := img.Bounds()
		if b.Empty() {
			return nil, fmt.Errorf("%s is empty, a glyph image needs pixels to draw", g.name)
		}
		sizes[i] = b.Size()
		inputs = append(inputs, atlaspack.Input{Name: frameName(g.r), Image: img})
	}
	res, err := atlaspack.PackImages(inputs, atlaspack.Options{Name: name, Padding: s.Padding, MaxWidth: maxFontPage, MaxHeight: maxFontPage, Scale: 1})
	if err != nil {
		return nil, err
	}
	frames := map[string]atlaspack.Frame{}
	var pageImages []*image.NRGBA
	var pageNames []string
	for _, p := range res.Pages {
		pageImages = append(pageImages, p.Image)
		pageNames = append(pageNames, p.ImageName)
		for _, fr := range p.Frames {
			frames[fr.Name] = fr
		}
	}
	lineHeight := 0
	for _, size := range sizes {
		lineHeight = max(lineHeight, size.Y)
	}
	var scaleW, scaleH int
	for _, img := range pageImages {
		scaleW = max(scaleW, img.Bounds().Dx())
		scaleH = max(scaleH, img.Bounds().Dy())
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "info face=\"%s\" size=%d bold=0 italic=0 charset=\"\" unicode=1 stretchH=100 smooth=1 aa=1 padding=0,0,0,0 spacing=0,0\n", bmfontFaceName(name), lineHeight)
	fmt.Fprintf(&out, "common lineHeight=%d base=%d scaleW=%d scaleH=%d pages=%d packed=0\n", lineHeight, lineHeight, scaleW, scaleH, len(pageImages))
	for i, n := range pageNames {
		fmt.Fprintf(&out, "page id=%d file=\"%s\"\n", i, n)
	}
	fmt.Fprintf(&out, "chars count=%d\n", len(glyphs))
	for i, g := range glyphs {
		fr := frames[frameName(g.r)]
		var yoffset int
		switch s.Align {
		case "top":
			yoffset = 0
		case "center":
			yoffset = (lineHeight - sizes[i].Y) / 2
		default:
			yoffset = lineHeight - sizes[i].Y
		}
		fmt.Fprintf(&out, "char id=%d x=%d y=%d width=%d height=%d xoffset=0 yoffset=%d xadvance=%d page=%d chnl=15\n", g.r, fr.Frame.Min.X, fr.Frame.Min.Y, fr.Frame.Dx(), fr.Frame.Dy(), yoffset, sizes[i].X+s.Spacing, fr.Page)
	}
	baked := &bakedFont{fnt: out.Bytes(), pageNames: pageNames}
	for _, img := range pageImages {
		var pb bytes.Buffer
		if err := png.Encode(&pb, img); err != nil {
			return nil, err
		}
		baked.pages = append(baked.pages, pb.Bytes())
	}
	return baked, nil
}

// frameName names the packed frame of the glyph of r, unique per character.
func frameName(r rune) string {
	return fmt.Sprintf("U+%04X", r)
}
