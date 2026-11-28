package l10n

import (
	"slices"
	"strings"
	"unicode"
)

// ValidateLocale reports whether locale is a non-empty canonical BCP-47 locale tag
// (matching NormalizeLocale, with ASCII alphanumeric subtags of length 1 to 8).
func ValidateLocale(locale string) error {
	return checkCanonicalLocale(locale)
}

// StripPlaceholders removes {name} interpolation placeholders from tmpl while
// unescaping doubled braces ("{{" -> "{", "}}" -> "}"), matching Catalog.T and
// Catalog.Plural. Unclosed braces are kept literally.
func StripPlaceholders(tmpl string) string {
	if !strings.ContainsAny(tmpl, "{}") {
		return tmpl
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	for i := 0; i < len(tmpl); {
		switch tmpl[i] {
		case '{':
			if i+1 < len(tmpl) && tmpl[i+1] == '{' {
				b.WriteByte('{')
				i += 2
				continue
			}
			end := -1
			for j := i + 1; j < len(tmpl); j++ {
				ch := tmpl[j]
				if ch == '}' {
					end = j
					break
				}
				if ch == '{' || ch == '\n' || ch == '\r' {
					break
				}
			}
			if end > i+1 {
				// Skip "{name}".
				i = end + 1
				continue
			}
			b.WriteByte('{')
			i++
		case '}':
			if i+1 < len(tmpl) && tmpl[i+1] == '}' {
				b.WriteByte('}')
				i += 2
				continue
			}
			b.WriteByte('}')
			i++
		default:
			b.WriteByte(tmpl[i])
			i++
		}
	}
	return b.String()
}

// EachForm calls fn for each non-empty plural form template in p in canonical
// CLDR category order (zero, one, two, few, many, other).
func (p PluralForms) EachForm(fn func(cat PluralCategory, tmpl string)) {
	for _, entry := range []struct {
		cat  PluralCategory
		tmpl string
	}{
		{PluralZero, p.Zero},
		{PluralOne, p.One},
		{PluralTwo, p.Two},
		{PluralFew, p.Few},
		{PluralMany, p.Many},
		{PluralOther, p.Other},
	} {
		if entry.tmpl != "" {
			fn(entry.cat, entry.tmpl)
		}
	}
}

// CollectChars returns the sorted, deduplicated non-control characters used by
// every string and plural form in t after stripping {placeholders}.
func (t *StringTable) CollectChars() string {
	if t == nil {
		return ""
	}
	seen := make(map[rune]struct{})
	add := func(s string) {
		for _, r := range StripPlaceholders(s) {
			if !unicode.IsControl(r) {
				seen[r] = struct{}{}
			}
		}
	}
	for _, v := range t.Strings {
		add(v)
	}
	for _, forms := range t.Plurals {
		forms.EachForm(func(_ PluralCategory, tmpl string) {
			add(tmpl)
		})
	}
	if len(seen) == 0 {
		return ""
	}
	runes := make([]rune, 0, len(seen))
	for r := range seen {
		runes = append(runes, r)
	}
	slices.Sort(runes)
	return string(runes)
}
