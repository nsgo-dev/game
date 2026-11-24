package assetbuild

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// AddKernTable is addKernTable for the package's external tests.
var AddKernTable = addKernTable

// addKernTable returns the TrueType font ttf with a format 0 kern table of pairs added, each the code points of two characters and the kerning between their glyphs in font units, for tests that need kerning from a font that has none, as Go Regular has none.
func addKernTable(ttf []byte, pairs [][3]int) ([]byte, error) {
	otf, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	var buf sfnt.Buffer
	type entry struct {
		key   uint32
		value int16
	}
	var entries []entry
	for _, p := range pairs {
		l, err := otf.GlyphIndex(&buf, rune(p[0]))
		if err != nil {
			return nil, err
		}
		r, err := otf.GlyphIndex(&buf, rune(p[1]))
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{uint32(l)<<16 | uint32(r), int16(p[2])})
	}
	slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(a.key, b.key) })
	entries = slices.CompactFunc(entries, func(a, b entry) bool { return a.key == b.key })
	be := binary.BigEndian
	n := len(entries)
	var kern []byte
	for _, v := range []int{0, 1, 0, 14 + 6*n, 0x0001, n, 6, 0, 6*n - 6} {
		// version, one subtable, its version, its length, format 0 horizontal, pairs, and the binary search fields, which the reader ignores
		kern = be.AppendUint16(kern, uint16(v))
	}
	for _, e := range entries {
		kern = be.AppendUint32(kern, e.key)
		kern = be.AppendUint16(kern, uint16(e.value))
	}

	numTables := int(be.Uint16(ttf[4:]))
	dir := ttf[12 : 12+16*numTables]
	if bytes.Contains(dir, []byte("kern")) {
		return nil, errors.New("the font already has a kern table")
	}
	body := slices.Clone(ttf[12+16*numTables:])
	for len(body)%4 != 0 {
		body = append(body, 0)
	}
	// The directory, sorted by tag, grows by one 16-byte record, so every table moves 16 bytes on.
	record := be.AppendUint32(append([]byte("kern"), 0, 0, 0, 0), uint32(12+16*(numTables+1)+len(body)))
	record = be.AppendUint32(record, uint32(len(kern)))
	out := be.AppendUint16(slices.Clone(ttf[:4]), uint16(numTables+1))
	out = append(out, ttf[6:12]...)
	added := false
	for i := range numTables {
		rec := dir[16*i : 16*i+16]
		if !added && string(rec[:4]) > "kern" {
			out, added = append(out, record...), true
		}
		out = append(out, rec[:8]...)
		out = be.AppendUint32(out, be.Uint32(rec[8:])+16)
		out = append(out, rec[12:]...)
	}
	if !added {
		out = append(out, record...)
	}
	out = append(out, body...)
	return append(out, kern...), nil
}

// BenchmarkBakeFontKerning bakes the Latin, Greek and Cyrillic characters Go Regular has, several hundred glyphs, from the font with a kern table of every pair of ASCII letters added, so its kerning asks the table for every ordered pair of the baked glyphs.
func BenchmarkBakeFontKerning(b *testing.B) {
	var letters []rune
	for r := 'A'; r <= 'Z'; r++ {
		letters = append(letters, r, r+'a'-'A')
	}
	var pairs [][3]int
	for _, l := range letters {
		for _, r := range letters {
			pairs = append(pairs, [3]int{int(l), int(r), -64})
		}
	}
	ttf, err := addKernTable(goregular.TTF, pairs)
	if err != nil {
		b.Fatal(err)
	}
	var runes []rune
	for r := rune(0x20); r < 0x530; r++ {
		runes = append(runes, r)
	}
	s := fontSettings{Size: 16, Padding: 1}
	for b.Loop() {
		if _, err := bakeFont(ttf, "hud", runes, s); err != nil {
			b.Fatal(err)
		}
	}
}
