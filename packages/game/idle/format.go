package idle

import (
	"strconv"
)

// Notation selects how Format writes a number.
type Notation uint8

const (
	// Short divides by the locale's largest unit at or below the value: 1.23K and 4.5B in English, 1.23万 and 4.5亿 in Chinese. Values below the first unit are positional, and values past the last unit's range switch to Scientific.
	Short Notation = iota
	// Scientific writes one integer digit and a power of ten, such as 1.23e45. Values below 1000 are positional.
	Scientific
	// Engineering writes a power of ten that is a multiple of three and one to three integer digits, such as 12.3e45. Values below 1000 are positional.
	Engineering
	// Letters uses K, M, B and T, then aa, ab, … zz, aaa and so on for each further factor of 1000, so it never runs out. Values below 1000 are positional; only the locale's decimal separator applies.
	Letters
	// Full writes every integer digit with grouping separators, such as 1,234,567, up to 10^21, and Scientific beyond.
	Full
)

// String returns the notation's name, such as "short".
func (n Notation) String() string {
	switch n {
	case Short:
		return "short"
	case Scientific:
		return "scientific"
	case Engineering:
		return "engineering"
	case Letters:
		return "letters"
	case Full:
		return "full"
	}
	return "Notation(" + strconv.Itoa(int(n)) + ")"
}

// DefaultDigits is the number of significant digits Format shows when Format.Digits is 0.
const DefaultDigits = 3

// maxDigits is the most significant digits a float64 mantissa carries.
const maxDigits = 17

// Format writes Numbers for display in a notation and locale. The zero Format writes Short notation with three significant digits in English: 1234 is "1.23K", 1e45 is "1Qad".
//
// Digits is the number of significant digits shown; integer digits are never dropped, so 123456 is "123K" and 1234 in Full is "1,234" even with Digits 3. Trailing fraction zeros are trimmed unless Fixed is set, which keeps the width steady as a counter climbs: "1.20K" instead of "1.2K".
//
// Rounding happens before the unit is chosen, so 999999 with three digits is "1M", never "1000K". Positional output covers exponents from -6 up; smaller magnitudes are scientific.
type Format struct {
	// Notation is the notation; the zero value is Short.
	Notation Notation
	// Digits is the number of significant digits, from 1 to 17; 0 means DefaultDigits and larger values are treated as 17.
	Digits int
	// Fixed keeps trailing zeros in the fraction.
	Fixed bool
	// Locale supplies separators and units; nil means English. It must be valid; see Locale.Validate.
	Locale *Locale
}

// Format returns n written in f's notation and locale.
func (f Format) Format(n Number) string {
	var buf [64]byte
	return string(f.Append(buf[:0], n))
}

// Append appends n written in f's notation and locale to dst, allocating nothing when dst has room, so a HUD can redraw a counter every frame.
func (f Format) Append(dst []byte, n Number) []byte {
	loc := f.Locale
	if loc == nil {
		loc = english
	}
	digits := f.Digits
	if digits <= 0 {
		digits = DefaultDigits
	}
	if digits > maxDigits {
		digits = maxDigits
	}
	if n.m < 0 {
		dst = append(dst, '-')
	}
	d := n.shortest()
	e := d.e
	lay := f.layout(loc, e)
	count := lay.sig(digits, e)
	if count < d.nd {
		d.set(n.m, n.e, count-1)
		if d.e != e {
			// Rounding carried into a new power of ten, so the digits are 1 followed by zeros: lay them out for the new magnitude, padding or dropping zeros as it needs.
			e = d.e
			lay = f.layout(loc, e)
			count = lay.sig(digits, e)
		}
	}
	return lay.write(dst, loc, &d, count, f.Fixed)
}

// layoutKind is the shape a value takes.
type layoutKind uint8

const (
	positional layoutKind = iota
	scaled
	scientific
)

// layout is the shape chosen for one magnitude: positional digits, digits scaled by 10^unit with a suffix, or a mantissa and exponent.
type layout struct {
	kind   layoutKind
	unit   int64
	suffix string
	letter int64 // the Letters group index, or 0
	group  bool
	expTag bool // scaled output writes the exponent, as Engineering does
}

// sig returns how many significant digits to round to for a value whose leading digit is 10^e: the requested digits, or more when the integer part has more.
func (l layout) sig(digits int, e int64) int {
	var intDigits int64
	switch l.kind {
	case positional, scaled:
		intDigits = e - l.unit + 1
	case scientific:
		intDigits = 1
	}
	s := int64(digits)
	if intDigits > s {
		s = intDigits
	}
	if s > maxDigits {
		s = maxDigits
	}
	return int(s)
}

func (f Format) layout(loc *Locale, e int64) layout {
	if e < -6 {
		return layout{kind: scientific}
	}
	switch f.Notation {
	case Scientific:
		if e < 3 {
			return layout{kind: positional, group: true}
		}
		return layout{kind: scientific}
	case Engineering:
		if e < 3 {
			return layout{kind: positional, group: true}
		}
		return layout{kind: scaled, unit: e - e%3, expTag: true}
	case Letters:
		if e < 3 {
			return layout{kind: positional, group: true}
		}
		return layout{kind: scaled, unit: e - e%3, letter: e / 3}
	case Full:
		if e < 21 {
			return layout{kind: positional, group: true}
		}
		return layout{kind: scientific}
	}
	us := loc.Units
	if len(us) == 0 {
		if e < 21 {
			return layout{kind: positional, group: true}
		}
		return layout{kind: scientific}
	}
	if e < int64(us[0].Exp) {
		return layout{kind: positional, group: true}
	}
	i := len(us) - 1
	for int64(us[i].Exp) > e {
		i--
	}
	if i == len(us)-1 {
		step := us[i].Exp
		if i > 0 {
			step = us[i].Exp - us[i-1].Exp
		}
		if e >= int64(us[i].Exp+step) {
			return layout{kind: scientific}
		}
	}
	return layout{kind: scaled, unit: int64(us[i].Exp), suffix: us[i].Suffix}
}

// digit returns the i-th significant digit, '0' past the stored ones.
func (d *digits) digit(i int) byte {
	if i < d.nd {
		return d.d[i]
	}
	return '0'
}

// write lays out the first count digits of d.
func (l layout) write(dst []byte, loc *Locale, d *digits, count int, fixed bool) []byte {
	switch l.kind {
	case scientific:
		dst = writeDigits(dst, loc, d, count, 1, false, fixed)
		dst = append(dst, loc.Exponent...)
		return strconv.AppendInt(dst, d.e, 10)
	case scaled:
		dst = writeDigits(dst, loc, d, count, d.e-l.unit+1, false, fixed)
		switch {
		case l.expTag:
			dst = append(dst, loc.Exponent...)
			return strconv.AppendInt(dst, l.unit, 10)
		case l.letter > 0:
			return appendLetters(dst, l.letter)
		}
		return append(dst, l.suffix...)
	}
	return writeDigits(dst, loc, d, count, d.e+1, l.group, fixed)
}

// writeDigits writes count of d's digits with intDigits of them before the decimal separator; a zero or negative intDigits writes leading zeros after "0.".
func writeDigits(dst []byte, loc *Locale, d *digits, count int, intDigits int64, group bool, fixed bool) []byte {
	n := int64(count)
	if intDigits <= 0 {
		dst = append(dst, '0')
	} else {
		for i := int64(0); i < intDigits; i++ {
			if group && loc.Group != "" && i > 0 && (intDigits-i)%3 == 0 {
				dst = append(dst, loc.Group...)
			}
			dst = append(dst, d.digit(int(min(i, maxDigits))))
		}
	}
	end := n
	if !fixed {
		for end > intDigits && end > 0 && d.digit(int(end-1)) == '0' {
			end--
		}
	}
	if end <= intDigits {
		return dst
	}
	dst = append(dst, loc.Decimal...)
	for i := intDigits; i < 0; i++ {
		dst = append(dst, '0')
	}
	for i := max(intDigits, 0); i < end; i++ {
		dst = append(dst, d.digit(int(i)))
	}
	return dst
}

// appendLetters writes the Letters suffix for 1000^g: K, M, B, T, then two letters aa to zz, three letters aaa to zzz and so on.
func appendLetters(dst []byte, g int64) []byte {
	switch g {
	case 1:
		return append(dst, 'K')
	case 2:
		return append(dst, 'M')
	case 3:
		return append(dst, 'B')
	case 4:
		return append(dst, 'T')
	}
	idx := g - 5
	width := 2
	block := int64(26 * 26)
	for idx >= block {
		idx -= block
		width++
		block *= 26
	}
	var tmp [16]byte
	for i := width - 1; i >= 0; i-- {
		tmp[i] = byte('a' + idx%26)
		idx /= 26
	}
	return append(dst, tmp[:width]...)
}
