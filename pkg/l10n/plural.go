package l10n

import (
	"slices"
	"strings"
)

//go:generate sh -c "cd ../../tools && go run ./cldrplurals"

// PluralCategory is one of the six Unicode CLDR cardinal plural categories.
type PluralCategory uint8

const (
	// PluralOther is the default fallback CLDR plural category.
	PluralOther PluralCategory = iota
	// PluralZero is the CLDR zero plural category.
	PluralZero
	// PluralOne is the CLDR one plural category.
	PluralOne
	// PluralTwo is the CLDR two plural category.
	PluralTwo
	// PluralFew is the CLDR few plural category.
	PluralFew
	// PluralMany is the CLDR many plural category.
	PluralMany
)

// canonicalPluralOrder lists all six CLDR plural categories in canonical order.
var canonicalPluralOrder = []PluralCategory{
	PluralZero,
	PluralOne,
	PluralTwo,
	PluralFew,
	PluralMany,
	PluralOther,
}

// String returns the canonical CLDR category name ("zero", "one", "two", "few", "many", or "other").
func (c PluralCategory) String() string {
	switch c {
	case PluralZero:
		return "zero"
	case PluralOne:
		return "one"
	case PluralTwo:
		return "two"
	case PluralFew:
		return "few"
	case PluralMany:
		return "many"
	default:
		return "other"
	}
}

// pluralRuleID resolves locale along its normalized BCP-47 subtag chain
// (for example "pt-PT" -> "pt-PT", "pt-BR" -> "pt", "cs-CZ" -> "cs") and returns
// the matching CLDR integer cardinal rule ID, falling back to CLDR root (0, other-only).
func pluralRuleID(locale string) uint8 {
	norm := NormalizeLocale(locale)
	if norm == "" {
		return 0
	}
	if id, ok := cldrPluralRuleIndex[norm]; ok {
		return id
	}
	for tag := norm; ; {
		idx := strings.LastIndexByte(tag, '-')
		if idx < 0 {
			break
		}
		tag = tag[:idx]
		if id, ok := cldrPluralRuleIndex[tag]; ok {
			return id
		}
	}
	return 0
}

// PluralCategoryFor returns the Unicode CLDR cardinal plural category for integer
// count in locale (using CLDRVersion rules with decimal operands v = w = f = t = c = e = 0).
//
// Negative counts are evaluated using their absolute value, per CLDR TR35.
// Locales are matched along their normalized BCP-47 subtag chain (for example
// "pt-PT" uses European Portuguese rules, "pt-BR" falls back to "pt", and "cs-CZ"
// falls back to "cs"). Unlisted locales fall back to CLDR root (PluralOther).
func PluralCategoryFor(locale string, count int) PluralCategory {
	var n uint64
	if count < 0 {
		n = uint64(-(count + 1)) + 1
	} else {
		n = uint64(count)
	}
	return evalCLDRPluralRule(pluralRuleID(locale), n)
}

// PluralCategories returns the CLDR cardinal plural categories reachable by
// integer counts for locale, in canonical CLDR order (zero, one, two, few, many, other).
func PluralCategories(locale string) []PluralCategory {
	mask := cldrRuleCategoryMasks[pluralRuleID(locale)]
	out := make([]PluralCategory, 0, 6)
	for _, cat := range canonicalPluralOrder {
		if mask&(1<<cat) != 0 {
			out = append(out, cat)
		}
	}
	return out
}

// UnusedPluralForm identifies a plural form supplied in a localization table
// whose CLDR category is never selected by Plural for integer counts in Locale.
type UnusedPluralForm struct {
	Locale   string
	Key      string
	Category PluralCategory
}

// UnusedPluralForms returns the plural categories set on forms that are never
// selected by Plural for integer counts in locale, in canonical CLDR order.
//
// PluralZero is never reported because Plural always honors an explicit zero
// form when count == 0 for every locale. PluralOther is never reported because
// Other is the universal fallback template.
func UnusedPluralForms(locale string, forms PluralForms) []PluralCategory {
	mask := cldrRuleCategoryMasks[pluralRuleID(locale)]
	checks := []struct {
		cat PluralCategory
		val string
	}{
		{PluralOne, forms.One},
		{PluralTwo, forms.Two},
		{PluralFew, forms.Few},
		{PluralMany, forms.Many},
	}
	var unused []PluralCategory
	for _, ch := range checks {
		if ch.val != "" && mask&(1<<ch.cat) == 0 {
			unused = append(unused, ch.cat)
		}
	}
	return unused
}

// CheckStringTablePlurals audits plural entries in t and returns every form
// whose CLDR category is never selected for integer counts in t.Locale, sorted
// by key and canonical CLDR category order.
func CheckStringTablePlurals(t *StringTable) []UnusedPluralForm {
	if t == nil || len(t.Plurals) == 0 {
		return nil
	}
	loc := NormalizeLocale(t.Locale)
	var out []UnusedPluralForm
	for _, key := range sortedKeys(t.Plurals) {
		for _, cat := range UnusedPluralForms(loc, t.Plurals[key]) {
			out = append(out, UnusedPluralForm{
				Locale:   loc,
				Key:      key,
				Category: cat,
			})
		}
	}
	return out
}

// CheckPlurals audits plural entries in c across locales (or all locales in c
// when none are given) and returns every form whose CLDR category is never
// selected for integer counts in that locale, sorted by locale, key, and
// canonical CLDR category order.
func (c *Catalog) CheckPlurals(locales ...string) []UnusedPluralForm {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var targets []string
	if len(locales) == 0 {
		targets = make([]string, 0, len(c.tables))
		for loc := range c.tables {
			targets = append(targets, loc)
		}
		slices.Sort(targets)
	} else {
		seen := make(map[string]struct{}, len(locales))
		for _, raw := range locales {
			norm := NormalizeLocale(raw)
			if norm == "" {
				continue
			}
			if _, dup := seen[norm]; !dup {
				seen[norm] = struct{}{}
				targets = append(targets, norm)
			}
		}
		slices.Sort(targets)
	}

	var out []UnusedPluralForm
	for _, loc := range targets {
		tbl := c.tables[loc]
		if len(tbl) == 0 {
			continue
		}
		keys := make([]string, 0, len(tbl))
		for k, e := range tbl {
			if e.isPlural {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			for _, cat := range UnusedPluralForms(loc, tbl[k].plural) {
				out = append(out, UnusedPluralForm{
					Locale:   loc,
					Key:      k,
					Category: cat,
				})
			}
		}
	}
	return out
}
