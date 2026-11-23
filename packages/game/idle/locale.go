package idle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// LocaleVersion is the locale file schema version this package reads and writes.
const LocaleVersion = 1

// maxUnitExp bounds a unit's power of ten so a locale file cannot describe nonsense.
const maxUnitExp = 1_000_000

// Locale describes how numbers read in one language: the decimal and grouping separators, the exponent marker, and the units the Short notation divides by, such as K and M in English or 万 and 亿 in Chinese.
//
// A Locale is a versioned YAML document, so a game can add a language, or name its own units, without code:
//
//	version: 1
//	name: de
//	decimal: ","
//	group: "."
//	exponent: e
//	units:
//	  - exp: 3
//	    suffix: " Tsd."
//	  - exp: 6
//	    suffix: " Mio."
//
// Format reads a Locale without validating it; one from ParseLocale or the built-in constructors is always valid, and one built in code should pass Validate first.
type Locale struct {
	// Version is the schema version; it must equal LocaleVersion.
	Version int `yaml:"version"`
	// Name is the locale's BCP 47 tag, such as "en" or "zh-Hans".
	Name string `yaml:"name"`
	// Decimal separates the integer and fraction digits.
	Decimal string `yaml:"decimal"`
	// Group separates each three integer digits in positional output; empty means no grouping.
	Group string `yaml:"group"`
	// Exponent marks the power of ten in scientific and engineering output, such as "e" in 1.23e45.
	Exponent string `yaml:"exponent"`
	// Units lists the Short notation's units in ascending Exp order.
	Units []Unit `yaml:"units"`
}

// Unit is one Short notation unit: a value of at least 10^Exp is shown divided by 10^Exp and followed by Suffix.
type Unit struct {
	// Exp is the unit's power of ten, such as 3 for K or 4 for 万.
	Exp int `yaml:"exp"`
	// Suffix follows the scaled digits, such as "K", "万" or " million".
	Suffix string `yaml:"suffix"`
}

// Validate reports whether l is a well-formed version 1 locale: separators and suffixes that are non-empty where required, valid UTF-8 and free of ASCII digits and signs, a decimal separator that differs from the group separator, an exponent marker that differs from both, unit suffixes that are neither separator and do not start with the exponent marker, and unit exponents from 1 to 1000000 in strictly ascending order with distinct suffixes.
func (l *Locale) Validate() error {
	if l == nil {
		return errors.New("idle: nil locale")
	}
	if l.Version != LocaleVersion {
		return fmt.Errorf("idle: locale: unsupported version %d (want %d)", l.Version, LocaleVersion)
	}
	if l.Name == "" {
		return errors.New("idle: locale: name is required")
	}
	if !utf8.ValidString(l.Name) {
		return errors.New("idle: locale: name is not valid UTF-8")
	}
	for _, f := range []struct {
		field, value string
		required     bool
	}{
		{"decimal", l.Decimal, true},
		{"group", l.Group, false},
		{"exponent", l.Exponent, true},
	} {
		if err := checkMarker(f.value, f.required); err != nil {
			return fmt.Errorf("idle: locale %s: %s %w", l.Name, f.field, err)
		}
	}
	if l.Decimal == l.Group {
		return fmt.Errorf("idle: locale %s: decimal and group separators are both %q", l.Name, l.Decimal)
	}
	if l.Exponent == l.Decimal || l.Exponent == l.Group {
		return fmt.Errorf("idle: locale %s: exponent %q is also a separator", l.Name, l.Exponent)
	}
	seen := make(map[string]bool, len(l.Units))
	for i, u := range l.Units {
		if u.Exp < 1 || u.Exp > maxUnitExp {
			return fmt.Errorf("idle: locale %s: units[%d].exp %d is outside 1..%d", l.Name, i, u.Exp, maxUnitExp)
		}
		if i > 0 && u.Exp <= l.Units[i-1].Exp {
			return fmt.Errorf("idle: locale %s: units[%d].exp %d is not above the previous unit's %d", l.Name, i, u.Exp, l.Units[i-1].Exp)
		}
		if err := checkMarker(u.Suffix, true); err != nil {
			return fmt.Errorf("idle: locale %s: units[%d].suffix %w", l.Name, i, err)
		}
		if u.Suffix == l.Decimal || u.Suffix == l.Group || strings.HasPrefix(u.Suffix, l.Exponent) {
			return fmt.Errorf("idle: locale %s: units[%d].suffix %q could be read as a separator or exponent", l.Name, i, u.Suffix)
		}
		if seen[u.Suffix] {
			return fmt.Errorf("idle: locale %s: units[%d].suffix %q is repeated", l.Name, i, u.Suffix)
		}
		seen[u.Suffix] = true
	}
	return nil
}

// checkMarker refuses a separator or suffix that could be read as part of a number.
func checkMarker(s string, required bool) error {
	if s == "" {
		if required {
			return errors.New("is required")
		}
		return nil
	}
	if !utf8.ValidString(s) {
		return errors.New("is not valid UTF-8")
	}
	if strings.ContainsAny(s, "0123456789+-") {
		return fmt.Errorf("%q contains a digit or sign", s)
	}
	return nil
}

// ParseLocale decodes a YAML locale document and validates it. Unknown fields and trailing documents are refused.
func ParseLocale(data []byte) (*Locale, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("idle: empty locale document")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var l Locale
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("idle: decode locale: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("idle: unexpected trailing YAML document in locale")
		}
		return nil, fmt.Errorf("idle: decode locale: %w", err)
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// EncodeLocale validates l and writes it as deterministic 2-space YAML with fields in a fixed order, so ParseLocale followed by EncodeLocale reproduces the same bytes.
func EncodeLocale(l *Locale) ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	out := *l
	if out.Units == nil {
		out.Units = []Unit{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("idle: encode locale: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("idle: encode locale: %w", err)
	}
	return buf.Bytes(), nil
}

// Clone returns a deep copy of l, so a built-in locale can be adjusted without affecting other users.
func (l *Locale) Clone() *Locale {
	c := *l
	c.Units = append([]Unit(nil), l.Units...)
	return &c
}

func units(exp0 int, suffixes ...string) []Unit {
	out := make([]Unit, len(suffixes))
	step := 3
	if exp0 == 4 {
		step = 4
	}
	for i, s := range suffixes {
		out[i] = Unit{Exp: exp0 + i*step, Suffix: s}
	}
	return out
}

var (
	english = &Locale{
		Version:  LocaleVersion,
		Name:     "en",
		Decimal:  ".",
		Group:    ",",
		Exponent: "e",
		Units:    units(3, "K", "M", "B", "T", "Qa", "Qi", "Sx", "Sp", "Oc", "No", "Dc", "Ud", "Dd", "Td", "Qad", "Qid", "Sxd", "Spd", "Ocd", "Nod", "Vg"),
	}
	chineseSimplified = &Locale{
		Version:  LocaleVersion,
		Name:     "zh-Hans",
		Decimal:  ".",
		Group:    ",",
		Exponent: "e",
		Units:    units(4, "万", "亿", "兆", "京", "垓", "秭", "穰", "沟", "涧", "正", "载"),
	}
	chineseTraditional = &Locale{
		Version:  LocaleVersion,
		Name:     "zh-Hant",
		Decimal:  ".",
		Group:    ",",
		Exponent: "e",
		Units:    units(4, "萬", "億", "兆", "京", "垓", "秭", "穰", "溝", "澗", "正", "載"),
	}
)

// English returns a new English locale: "." and ",", and the units K, M, B, T, Qa, Qi, Sx, Sp, Oc, No, Dc, Ud, Dd, Td, Qad, Qid, Sxd, Spd, Ocd, Nod and Vg for 10^3 to 10^63.
func English() *Locale { return english.Clone() }

// ChineseSimplified returns a new Simplified Chinese locale: "." and ",", and the myriad units 万, 亿, 兆, 京, 垓, 秭, 穰, 沟, 涧, 正 and 载 for 10^4 to 10^44, each 10^4 times the last.
func ChineseSimplified() *Locale { return chineseSimplified.Clone() }

// ChineseTraditional returns a new Traditional Chinese locale with the myriad units 萬, 億, 兆, 京, 垓, 秭, 穰, 溝, 澗, 正 and 載 for 10^4 to 10^44.
func ChineseTraditional() *Locale { return chineseTraditional.Clone() }

// LocaleFor returns a new built-in locale for a BCP 47 tag: Traditional Chinese for zh with the Hant script or the TW, HK or MO region, Simplified Chinese for other zh tags, and English for everything else. Case and "_" or "-" separators do not matter.
func LocaleFor(tag string) *Locale {
	parts := strings.FieldsFunc(strings.ToLower(tag), func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 || parts[0] != "zh" {
		return English()
	}
	for _, p := range parts[1:] {
		switch p {
		case "hans":
			return ChineseSimplified()
		case "hant", "tw", "hk", "mo":
			return ChineseTraditional()
		}
	}
	return ChineseSimplified()
}
