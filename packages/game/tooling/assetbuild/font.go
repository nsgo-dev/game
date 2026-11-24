package assetbuild

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/pack"
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

// maxFontPage bounds each page of a baked font, as game pack bounds an atlas page by default; a font's size is bounded by it too.
const maxFontPage = project.MaxFontSize

func (b *Builder) font(t project.FontTarget) (prepared, error) {
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
		dir, name := t.FontPages()
		baked, err := bakeFont(data, name, runes, fs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.In, err)
		}
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
	}}, nil
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
	// The text form of BMFont has no escape for a quote inside a quoted value, and a record is one line.
	family = strings.Map(func(r rune) rune {
		switch {
		case r == '"':
			return '\''
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, family)
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
	var inputs []pack.Input
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
			inputs = append(inputs, pack.Input{Name: frameName(r), Image: img})
		}
		glyphs = append(glyphs, g)
	}
	if len(glyphs) == 0 {
		return nil, fmt.Errorf("the font has a glyph for none of the %d characters to bake", len(runes))
	}

	frames := map[string]pack.Frame{}
	var pageImages []*image.NRGBA
	var pageNames []string
	if len(inputs) == 0 {
		// Every glyph is blank, such as when only the space is baked, so the one page holds nothing.
		pageImages = []*image.NRGBA{image.NewNRGBA(image.Rect(0, 0, 1, 1))}
		pageNames = []string{name + ".png"}
	} else {
		res, err := pack.PackImages(inputs, pack.Options{Name: name, Padding: s.Padding, MaxWidth: maxFontPage, MaxHeight: maxFontPage, Scale: 1})
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

// frameName names the packed frame of the glyph of r, unique per character.
func frameName(r rune) string {
	return fmt.Sprintf("U+%04X", r)
}
