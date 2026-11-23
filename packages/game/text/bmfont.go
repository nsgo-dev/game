// Package text draws bitmap fonts (AngelCode BMFont) through sprite batches, signed-distance-field fonts that stay sharp at any size on the screen or in 3D world space (SDFFont, WorldText), and platform-rendered text through core.Drawing.
package text

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// KerningPair identifies an ordered pair of consecutive runes that carry a
// horizontal kerning adjustment.
type KerningPair struct {
	First, Second rune
}

// Glyph is one character in a bitmap font: its atlas frame, placement offsets,
// cursor advance, and texture page ID.
type Glyph struct {
	// ID is the Unicode code point of the glyph.
	ID rune
	// Frame is the glyph's rectangle in its page's sprite.Atlas.
	Frame sprite.Frame
	// XOffset and YOffset displace the glyph's drawn rectangle from the cursor.
	XOffset, YOffset float32
	// XAdvance is how far the cursor advances after this glyph.
	XAdvance float32
	// Page is the BMFont texture page ID holding this glyph.
	Page int
}

// Font is a parsed AngelCode BMFont: one sprite.Atlas per texture page, glyph
// metrics, and kerning pairs.
type Font struct {
	// Face is the font face name from the info record.
	Face string
	// Size is the font size from the info record.
	Size float32
	// LineHeight is the distance between consecutive lines, in points.
	LineHeight float32
	// Base is the distance from the top of a line to the characters' baseline, in points.
	Base float32
	// Pages maps each BMFont page ID to the sprite.Atlas holding that page's glyphs.
	Pages map[int]*sprite.Atlas
	// Glyphs maps each rune to its Glyph metrics and frame.
	Glyphs map[rune]Glyph
	// Kernings maps pairs of consecutive runes to their horizontal kerning adjustment.
	Kernings map[KerningPair]float32
}

// Kerning returns the horizontal kerning adjustment when second follows first,
// or 0 if no kerning pair is defined.
func (f *Font) Kerning(first, second rune) float32 {
	if f == nil || len(f.Kernings) == 0 {
		return 0
	}
	return f.Kernings[KerningPair{First: first, Second: second}]
}

// LoadBMFont parses AngelCode BMFont data in either plain-text or XML format.
//
// For each texture page defined in the font, pageImage is called with the page
// ID and filename (in ascending page ID order) to obtain its core.ImageSource.
// If pageImage is nil, each page's atlas receives a zero core.ImageSource.
func LoadBMFont(data []byte, pageImage func(id int, file string) core.ImageSource) (*Font, error) {
	trimmed := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed = bytes.TrimSpace(trimmed)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("text: bmfont: empty data")
	}

	var raw bmfontData
	var err error
	if trimmed[0] == '<' {
		raw, err = parseBMFontXML(trimmed)
	} else {
		raw, err = parseBMFontText(trimmed)
	}
	if err != nil {
		return nil, fmt.Errorf("text: bmfont: %w", err)
	}
	return buildBMFont(raw, pageImage)
}

// LoadBMFontImage parses a single-page AngelCode BMFont (plain-text or XML)
// backed by img. It returns an error if the font declares more than one page.
func LoadBMFontImage(img core.ImageSource, data []byte) (*Font, error) {
	f, err := LoadBMFont(data, func(int, string) core.ImageSource { return img })
	if err != nil {
		return nil, err
	}
	if len(f.Pages) > 1 {
		return nil, fmt.Errorf("text: bmfont: font has %d pages; use LoadBMFont", len(f.Pages))
	}
	return f, nil
}

type bmfontData struct {
	face          string
	size          float32
	hasCommon     bool
	lineHeight    float32
	base          float32
	expectedPages int
	pages         []bmfontPage
	chars         []bmfontChar
	kernings      []bmfontKerning
	// field is the distanceField record that msdf-bmfont-xml writes for signed-distance-field fonts; LoadBMFont ignores it and LoadSDFFont requires it.
	field *bmfontField
}

type bmfontField struct {
	fieldType     string
	distanceRange float32
}

type bmfontPage struct {
	id   int
	file string
}

type bmfontChar struct {
	id               int
	x, y, w, h       float32
	xoffset, yoffset float32
	xadvance         float32
	page             int
}

type bmfontKerning struct {
	first, second int
	amount        float32
}

func buildBMFont(raw bmfontData, pageImage func(id int, file string) core.ImageSource) (*Font, error) {
	if !raw.hasCommon {
		return nil, fmt.Errorf("text: bmfont: missing common record")
	}
	if raw.lineHeight <= 0 {
		return nil, fmt.Errorf("text: bmfont: lineHeight %v must be positive", raw.lineHeight)
	}
	if raw.expectedPages < 0 {
		return nil, fmt.Errorf("text: bmfont: invalid page count %d", raw.expectedPages)
	}
	if len(raw.pages) == 0 {
		return nil, fmt.Errorf("text: bmfont: no pages defined")
	}
	if raw.expectedPages > 0 && len(raw.pages) != raw.expectedPages {
		return nil, fmt.Errorf("text: bmfont: common declares %d pages, found %d", raw.expectedPages, len(raw.pages))
	}
	if len(raw.chars) == 0 {
		return nil, fmt.Errorf("text: bmfont: no glyphs defined")
	}

	pageFiles := make(map[int]string, len(raw.pages))
	pageIDs := make([]int, 0, len(raw.pages))
	for _, p := range raw.pages {
		if p.id < 0 {
			return nil, fmt.Errorf("text: bmfont: invalid page id %d", p.id)
		}
		if _, dup := pageFiles[p.id]; dup {
			return nil, fmt.Errorf("text: bmfont: duplicate page id %d", p.id)
		}
		pageFiles[p.id] = p.file
		pageIDs = append(pageIDs, p.id)
	}
	slices.Sort(pageIDs)

	f := &Font{
		Face:       raw.face,
		Size:       raw.size,
		LineHeight: raw.lineHeight,
		Base:       raw.base,
		Pages:      make(map[int]*sprite.Atlas, len(pageIDs)),
		Glyphs:     make(map[rune]Glyph, len(raw.chars)),
	}
	for _, id := range pageIDs {
		var img core.ImageSource
		if pageImage != nil {
			img = pageImage(id, pageFiles[id])
		}
		f.Pages[id] = &sprite.Atlas{
			Image:  img,
			Frames: make(map[string]sprite.Frame),
		}
	}

	for _, c := range raw.chars {
		if c.id < 0 {
			return nil, fmt.Errorf("text: bmfont: invalid char id %d", c.id)
		}
		if c.x < 0 || c.y < 0 || c.w < 0 || c.h < 0 {
			return nil, fmt.Errorf("text: bmfont: char %d has negative bounds (%v,%v,%v,%v)", c.id, c.x, c.y, c.w, c.h)
		}
		atlas, ok := f.Pages[c.page]
		if !ok {
			return nil, fmt.Errorf("text: bmfont: char %d references unknown page %d", c.id, c.page)
		}
		r := rune(c.id)
		if _, dup := f.Glyphs[r]; dup {
			return nil, fmt.Errorf("text: bmfont: duplicate char id %d", c.id)
		}
		name := glyphFrameName(r)
		frame := sprite.Frame{
			Name:   name,
			X:      c.x,
			Y:      c.y,
			W:      c.w,
			H:      c.h,
			Width:  c.w,
			Height: c.h,
		}
		atlas.Frames[name] = frame
		f.Glyphs[r] = Glyph{
			ID:       r,
			Frame:    frame,
			XOffset:  c.xoffset,
			YOffset:  c.yoffset,
			XAdvance: c.xadvance,
			Page:     c.page,
		}
	}

	if len(raw.kernings) > 0 {
		f.Kernings = make(map[KerningPair]float32, len(raw.kernings))
		for _, k := range raw.kernings {
			if k.first < 0 || k.second < 0 {
				return nil, fmt.Errorf("text: bmfont: invalid kerning pair (%d, %d)", k.first, k.second)
			}
			f.Kernings[KerningPair{First: rune(k.first), Second: rune(k.second)}] = k.amount
		}
	}

	return f, nil
}

func glyphFrameName(r rune) string {
	if utf8.ValidRune(r) {
		return string(r)
	}
	return strconv.Itoa(int(r))
}

// --- Plain-text BMFont parser ---

func parseBMFontText(data []byte) (bmfontData, error) {
	var out bmfontData
	lines := strings.Split(string(data), "\n")
	for lineNo, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		tag, rest, _ := cutField(line)
		kvs, err := parseKeyValues(rest)
		if err != nil {
			return bmfontData{}, fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		switch tag {
		case "info":
			if v, ok := kvs["face"]; ok {
				out.face = v
			}
			if v, ok := kvs["size"]; ok {
				n, err := parseFloat32(v)
				if err != nil {
					return bmfontData{}, fmt.Errorf("line %d: info size: %w", lineNo+1, err)
				}
				out.size = n
			}
		case "common":
			out.hasCommon = true
			if v, ok := kvs["lineHeight"]; ok {
				n, err := parseFloat32(v)
				if err != nil {
					return bmfontData{}, fmt.Errorf("line %d: common lineHeight: %w", lineNo+1, err)
				}
				out.lineHeight = n
			}
			if v, ok := kvs["base"]; ok {
				n, err := parseFloat32(v)
				if err != nil {
					return bmfontData{}, fmt.Errorf("line %d: common base: %w", lineNo+1, err)
				}
				out.base = n
			}
			if v, ok := kvs["pages"]; ok {
				n, err := strconv.Atoi(v)
				if err != nil {
					return bmfontData{}, fmt.Errorf("line %d: common pages: %w", lineNo+1, err)
				}
				out.expectedPages = n
			}
		case "page":
			idStr, ok := kvs["id"]
			if !ok {
				return bmfontData{}, fmt.Errorf("line %d: page missing id", lineNo+1)
			}
			id, err := strconv.Atoi(idStr)
			if err != nil {
				return bmfontData{}, fmt.Errorf("line %d: page id: %w", lineNo+1, err)
			}
			out.pages = append(out.pages, bmfontPage{
				id:   id,
				file: kvs["file"],
			})
		case "chars":
			if v, ok := kvs["count"]; ok {
				if _, err := strconv.Atoi(v); err != nil {
					return bmfontData{}, fmt.Errorf("line %d: chars count: %w", lineNo+1, err)
				}
			}
		case "char":
			c, err := parseCharKVs(kvs)
			if err != nil {
				return bmfontData{}, fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			out.chars = append(out.chars, c)
		case "kernings":
			if v, ok := kvs["count"]; ok {
				if _, err := strconv.Atoi(v); err != nil {
					return bmfontData{}, fmt.Errorf("line %d: kernings count: %w", lineNo+1, err)
				}
			}
		case "distanceField":
			if out.field != nil {
				return bmfontData{}, fmt.Errorf("line %d: duplicate distanceField record", lineNo+1)
			}
			field := &bmfontField{fieldType: kvs["fieldType"]}
			if v, ok := kvs["distanceRange"]; ok {
				n, err := parseFloat32(v)
				if err != nil {
					return bmfontData{}, fmt.Errorf("line %d: distanceField distanceRange: %w", lineNo+1, err)
				}
				field.distanceRange = n
			}
			out.field = field
		case "kerning":
			k, err := parseKerningKVs(kvs)
			if err != nil {
				return bmfontData{}, fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			out.kernings = append(out.kernings, k)
		default:
			return bmfontData{}, fmt.Errorf("line %d: unknown record %q", lineNo+1, tag)
		}
	}
	return out, nil
}

func parseCharKVs(kvs map[string]string) (bmfontChar, error) {
	idStr, ok := kvs["id"]
	if !ok {
		return bmfontChar{}, fmt.Errorf("char missing id")
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return bmfontChar{}, fmt.Errorf("char id: %w", err)
	}
	c := bmfontChar{id: id}
	for _, f := range []struct {
		key string
		dst *float32
	}{
		{"x", &c.x},
		{"y", &c.y},
		{"width", &c.w},
		{"height", &c.h},
		{"xoffset", &c.xoffset},
		{"yoffset", &c.yoffset},
		{"xadvance", &c.xadvance},
	} {
		if v, ok := kvs[f.key]; ok {
			n, err := parseFloat32(v)
			if err != nil {
				return bmfontChar{}, fmt.Errorf("char %s: %w", f.key, err)
			}
			*f.dst = n
		}
	}
	if v, ok := kvs["page"]; ok {
		p, err := strconv.Atoi(v)
		if err != nil {
			return bmfontChar{}, fmt.Errorf("char page: %w", err)
		}
		c.page = p
	}
	return c, nil
}

func parseKerningKVs(kvs map[string]string) (bmfontKerning, error) {
	firstStr, ok := kvs["first"]
	if !ok {
		return bmfontKerning{}, fmt.Errorf("kerning missing first")
	}
	secondStr, ok := kvs["second"]
	if !ok {
		return bmfontKerning{}, fmt.Errorf("kerning missing second")
	}
	first, err := strconv.Atoi(firstStr)
	if err != nil {
		return bmfontKerning{}, fmt.Errorf("kerning first: %w", err)
	}
	second, err := strconv.Atoi(secondStr)
	if err != nil {
		return bmfontKerning{}, fmt.Errorf("kerning second: %w", err)
	}
	var amount float32
	if v, ok := kvs["amount"]; ok {
		n, err := parseFloat32(v)
		if err != nil {
			return bmfontKerning{}, fmt.Errorf("kerning amount: %w", err)
		}
		amount = n
	}
	return bmfontKerning{first: first, second: second, amount: amount}, nil
}

// cutField splits off the first whitespace-delimited token from s.
func cutField(s string) (first, rest string, ok bool) {
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	if i == len(s) {
		return s, "", false
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), true
}

// parseKeyValues parses space-separated key=value pairs where values may be
// double-quoted strings containing spaces.
func parseKeyValues(s string) (map[string]string, error) {
	kvs := make(map[string]string)
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		keyStart := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return nil, fmt.Errorf("expected key=value near %q", s[keyStart:])
		}
		key := s[keyStart:i]
		if key == "" {
			return nil, fmt.Errorf("empty key near %q", s[keyStart:])
		}
		i++ // skip '='
		var val string
		if i < len(s) && s[i] == '"' {
			i++ // skip opening '"'
			valStart := i
			for i < len(s) && s[i] != '"' {
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated quoted value for %q", key)
			}
			val = s[valStart:i]
			i++ // skip closing '"'
			if i < len(s) && s[i] != ' ' && s[i] != '\t' {
				return nil, fmt.Errorf("unexpected trailing characters after quoted value for %q", key)
			}
		} else {
			valStart := i
			for i < len(s) && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			val = s[valStart:i]
		}
		kvs[key] = val
	}
	return kvs, nil
}

func parseFloat32(s string) (float32, error) {
	v, err := strconv.ParseFloat(s, 32)
	return float32(v), err
}

// --- XML BMFont parser ---

type xmlBMFont struct {
	XMLName xml.Name `xml:"font"`
	Info    *struct {
		Face string  `xml:"face,attr"`
		Size float32 `xml:"size,attr"`
	} `xml:"info"`
	Common *struct {
		LineHeight float32 `xml:"lineHeight,attr"`
		Base       float32 `xml:"base,attr"`
		Pages      int     `xml:"pages,attr"`
	} `xml:"common"`
	Pages struct {
		Pages []struct {
			ID   *int   `xml:"id,attr"`
			File string `xml:"file,attr"`
		} `xml:"page"`
	} `xml:"pages"`
	Chars struct {
		Chars []struct {
			ID       *int    `xml:"id,attr"`
			X        float32 `xml:"x,attr"`
			Y        float32 `xml:"y,attr"`
			Width    float32 `xml:"width,attr"`
			Height   float32 `xml:"height,attr"`
			XOffset  float32 `xml:"xoffset,attr"`
			YOffset  float32 `xml:"yoffset,attr"`
			XAdvance float32 `xml:"xadvance,attr"`
			Page     int     `xml:"page,attr"`
		} `xml:"char"`
	} `xml:"chars"`
	DistanceField *struct {
		FieldType     string  `xml:"fieldType,attr"`
		DistanceRange float32 `xml:"distanceRange,attr"`
	} `xml:"distanceField"`
	Kernings struct {
		Kernings []struct {
			First  *int    `xml:"first,attr"`
			Second *int    `xml:"second,attr"`
			Amount float32 `xml:"amount,attr"`
		} `xml:"kerning"`
	} `xml:"kernings"`
}

func parseBMFontXML(data []byte) (bmfontData, error) {
	var doc xmlBMFont
	if err := xml.Unmarshal(data, &doc); err != nil {
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
	if doc.DistanceField != nil {
		out.field = &bmfontField{fieldType: doc.DistanceField.FieldType, distanceRange: doc.DistanceField.DistanceRange}
	}
	for i, p := range doc.Pages.Pages {
		if p.ID == nil {
			return bmfontData{}, fmt.Errorf("page %d missing id", i)
		}
		out.pages = append(out.pages, bmfontPage{id: *p.ID, file: p.File})
	}
	for i, c := range doc.Chars.Chars {
		if c.ID == nil {
			return bmfontData{}, fmt.Errorf("char %d missing id", i)
		}
		out.chars = append(out.chars, bmfontChar{
			id:       *c.ID,
			x:        c.X,
			y:        c.Y,
			w:        c.Width,
			h:        c.Height,
			xoffset:  c.XOffset,
			yoffset:  c.YOffset,
			xadvance: c.XAdvance,
			page:     c.Page,
		})
	}
	for i, k := range doc.Kernings.Kernings {
		if k.First == nil || k.Second == nil {
			return bmfontData{}, fmt.Errorf("kerning %d missing first or second", i)
		}
		out.kernings = append(out.kernings, bmfontKerning{
			first:  *k.First,
			second: *k.Second,
			amount: k.Amount,
		})
	}
	return out, nil
}
