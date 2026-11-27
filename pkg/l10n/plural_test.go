package l10n_test

import (
	"math"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/l10n"
)

func TestPluralCategoryForLocales(t *testing.T) {
	tests := []struct {
		locale string
		count  int
		want   l10n.PluralCategory
	}{
		// Czech & Slovak: one=1, few=2..4, other=0,5+ (many is decimal-only in CLDR).
		{"cs", 0, l10n.PluralOther},
		{"cs", 1, l10n.PluralOne},
		{"cs", 2, l10n.PluralFew},
		{"cs", 3, l10n.PluralFew},
		{"cs", 4, l10n.PluralFew},
		{"cs", 5, l10n.PluralOther},
		{"cs", 22, l10n.PluralOther},
		{"sk", 1, l10n.PluralOne},
		{"sk", 2, l10n.PluralFew},
		{"sk", 4, l10n.PluralFew},
		{"sk", 5, l10n.PluralOther},

		// Lithuanian: one=x1 (!11..19), few=x2..x9 (!11..19), other=0,10..20,30...
		{"lt", 0, l10n.PluralOther},
		{"lt", 1, l10n.PluralOne},
		{"lt", 2, l10n.PluralFew},
		{"lt", 5, l10n.PluralFew},
		{"lt", 9, l10n.PluralFew},
		{"lt", 10, l10n.PluralOther},
		{"lt", 11, l10n.PluralOther},
		{"lt", 19, l10n.PluralOther},
		{"lt", 20, l10n.PluralOther},
		{"lt", 21, l10n.PluralOne},
		{"lt", 25, l10n.PluralFew},

		// Latvian: zero=x0 or 11..19, one=x1 (!11), other=2..9,22..29...
		{"lv", 0, l10n.PluralZero},
		{"lv", 1, l10n.PluralOne},
		{"lv", 2, l10n.PluralOther},
		{"lv", 10, l10n.PluralZero},
		{"lv", 11, l10n.PluralZero},
		{"lv", 19, l10n.PluralZero},
		{"lv", 20, l10n.PluralZero},
		{"lv", 21, l10n.PluralOne},
		{"lv", 22, l10n.PluralOther},

		// Romanian & Moldovan: one=1, few=0 or (n!=1 && n%100 in 1..19), other=20..100...
		{"ro", 0, l10n.PluralFew},
		{"ro", 1, l10n.PluralOne},
		{"ro", 2, l10n.PluralFew},
		{"ro", 19, l10n.PluralFew},
		{"ro", 20, l10n.PluralOther},
		{"ro", 100, l10n.PluralOther},
		{"ro", 101, l10n.PluralFew},
		{"ro", 119, l10n.PluralFew},
		{"ro", 120, l10n.PluralOther},
		{"mo", 2, l10n.PluralFew},

		// Slovenian: one=n%100==1, two=n%100==2, few=n%100 in 3..4, other=0,5..100...
		{"sl", 0, l10n.PluralOther},
		{"sl", 1, l10n.PluralOne},
		{"sl", 2, l10n.PluralTwo},
		{"sl", 3, l10n.PluralFew},
		{"sl", 4, l10n.PluralFew},
		{"sl", 5, l10n.PluralOther},
		{"sl", 101, l10n.PluralOne},
		{"sl", 102, l10n.PluralTwo},
		{"sl", 103, l10n.PluralFew},
		{"sl", 105, l10n.PluralOther},

		// Polish: one=1, few=2..4,22..24(!12..14), many=0,5..21,25..31...
		{"pl", 0, l10n.PluralMany},
		{"pl", 1, l10n.PluralOne},
		{"pl", 2, l10n.PluralFew},
		{"pl", 5, l10n.PluralMany},
		{"pl", 12, l10n.PluralMany},
		{"pl", 21, l10n.PluralMany},
		{"pl", 22, l10n.PluralFew},

		// Russian, Ukrainian, Belarusian: one=x1(!11), few=x2..x4(!12..14), many=rest.
		{"ru", 0, l10n.PluralMany},
		{"ru", 1, l10n.PluralOne},
		{"ru", 2, l10n.PluralFew},
		{"ru", 5, l10n.PluralMany},
		{"ru", 11, l10n.PluralMany},
		{"ru", 21, l10n.PluralOne},
		{"uk", 24, l10n.PluralFew},
		{"be", 14, l10n.PluralMany},

		// Bosnian, Croatian, Serbian: one, few, other (not many).
		{"bs", 1, l10n.PluralOne},
		{"bs", 2, l10n.PluralFew},
		{"bs", 5, l10n.PluralOther},
		{"hr", 21, l10n.PluralOne},
		{"hr", 12, l10n.PluralOther},
		{"sr", 0, l10n.PluralOther},
		{"sr", 24, l10n.PluralFew},

		// Arabic: zero, one, two, few (3..10 mod 100), many (11..99 mod 100), other (100..102...).
		{"ar", 0, l10n.PluralZero},
		{"ar", 1, l10n.PluralOne},
		{"ar", 2, l10n.PluralTwo},
		{"ar", 3, l10n.PluralFew},
		{"ar", 10, l10n.PluralFew},
		{"ar", 11, l10n.PluralMany},
		{"ar", 99, l10n.PluralMany},
		{"ar", 100, l10n.PluralOther},
		{"ar", 103, l10n.PluralFew},

		// French, Portuguese (default/BR vs pt-PT), Italian, Spanish.
		{"fr", 0, l10n.PluralOne},
		{"fr", 1, l10n.PluralOne},
		{"fr", 2, l10n.PluralOther},
		{"fr", 1_000_000, l10n.PluralMany},
		{"pt", 0, l10n.PluralOne},
		{"pt", 1, l10n.PluralOne},
		{"pt", 2, l10n.PluralOther},
		{"pt-BR", 0, l10n.PluralOne},
		{"pt-PT", 0, l10n.PluralOther},
		{"pt-PT", 1, l10n.PluralOne},
		{"pt-PT", 1_000_000, l10n.PluralMany},
		{"it", 0, l10n.PluralOther},
		{"it", 1, l10n.PluralOne},
		{"it", 2_000_000, l10n.PluralMany},
		{"es", 0, l10n.PluralOther},
		{"es", 1, l10n.PluralOne},
		{"es", 1_000_000, l10n.PluralMany},

		// Irish (ga), Welsh (cy), Maltese (mt), Scottish Gaelic (gd), Breton (br).
		{"ga", 0, l10n.PluralOther},
		{"ga", 1, l10n.PluralOne},
		{"ga", 2, l10n.PluralTwo},
		{"ga", 3, l10n.PluralFew},
		{"ga", 6, l10n.PluralFew},
		{"ga", 7, l10n.PluralMany},
		{"ga", 10, l10n.PluralMany},
		{"ga", 11, l10n.PluralOther},
		{"cy", 0, l10n.PluralZero},
		{"cy", 1, l10n.PluralOne},
		{"cy", 2, l10n.PluralTwo},
		{"cy", 3, l10n.PluralFew},
		{"cy", 4, l10n.PluralOther},
		{"cy", 6, l10n.PluralMany},
		{"mt", 0, l10n.PluralFew},
		{"mt", 1, l10n.PluralOne},
		{"mt", 2, l10n.PluralTwo},
		{"mt", 10, l10n.PluralFew},
		{"mt", 11, l10n.PluralMany},
		{"mt", 19, l10n.PluralMany},
		{"mt", 20, l10n.PluralOther},
		{"gd", 1, l10n.PluralOne},
		{"gd", 11, l10n.PluralOne},
		{"gd", 2, l10n.PluralTwo},
		{"gd", 12, l10n.PluralTwo},
		{"gd", 3, l10n.PluralFew},
		{"gd", 19, l10n.PluralFew},
		{"gd", 20, l10n.PluralOther},
		{"br", 1, l10n.PluralOne},
		{"br", 71, l10n.PluralOther},
		{"br", 2, l10n.PluralTwo},
		{"br", 9, l10n.PluralFew},
		{"br", 1_000_000, l10n.PluralMany},

		// Icelandic & Macedonian: one = x1 (!11), other = rest.
		{"is", 1, l10n.PluralOne},
		{"is", 11, l10n.PluralOther},
		{"is", 21, l10n.PluralOne},
		{"mk", 21, l10n.PluralOne},
		{"mk", 11, l10n.PluralOther},

		// Hebrew & legacy code iw.
		{"he", 1, l10n.PluralOne},
		{"he", 2, l10n.PluralTwo},
		{"he", 3, l10n.PluralOther},
		{"iw", 2, l10n.PluralTwo},

		// East Asian and other plural-less CLDR locales: always other.
		{"zh", 1, l10n.PluralOther},
		{"ja", 1, l10n.PluralOther},
		{"ko", 1, l10n.PluralOther},
		{"yue", 1, l10n.PluralOther},
		{"lo", 1, l10n.PluralOther},
		{"my", 1, l10n.PluralOther},
		{"km", 1, l10n.PluralOther},

		// BCP-47 subtag fallback and normalization.
		{"cs-CZ", 3, l10n.PluralFew},
		{"cs_cz", 3, l10n.PluralFew},
		{"sk-SK", 2, l10n.PluralFew},
		{"lt-LT", 21, l10n.PluralOne},
		{"ro-RO", 2, l10n.PluralFew},
		{"sl-SI", 2, l10n.PluralTwo},
		{"zh-Hans-CN", 1, l10n.PluralOther},
		{"ar-SA", 2, l10n.PluralTwo},
		{"kok-Latn-IN", 0, l10n.PluralOne},
		// Unknown / unlisted locale falls back to CLDR root (other).
		{"qaa", 1, l10n.PluralOther},
		{"", 1, l10n.PluralOther},

		// Negative integers evaluate on absolute value, including math.MinInt without overflow.
		{"cs", -3, l10n.PluralFew},
		{"ru", -21, l10n.PluralOne},
		{"en", math.MinInt, l10n.PluralOther},
		{"ru", math.MinInt, l10n.PluralMany},
	}

	for _, tc := range tests {
		if got := l10n.PluralCategoryFor(tc.locale, tc.count); got != tc.want {
			t.Errorf("PluralCategoryFor(%q, %d) = %s, want %s", tc.locale, tc.count, got, tc.want)
		}
	}
}

func TestCatalogPluralExplicitZeroAndFallback(t *testing.T) {
	cat := l10n.NewCatalog("en")
	cat.SetPlural("en", "coins", l10n.PluralForms{
		Zero:  "No coins",
		One:   "{count} coin",
		Other: "{count} coins",
	})
	cat.SetPlural("en", "no_zero", l10n.PluralForms{
		One:   "{count} coin",
		Other: "{count} coins",
	})
	cat.SetPlural("fr", "items", l10n.PluralForms{
		One:   "{count} objet",
		Other: "{count} objets",
	})
	cat.SetPlural("fr", "items_zero", l10n.PluralForms{
		Zero:  "Aucun objet",
		One:   "{count} objet",
		Many:  "{count} d'objets",
		Other: "{count} objets",
	})
	cat.SetPlural("cs", "coins", l10n.PluralForms{
		One:   "{count} mince",
		Few:   "{count} mince (few)",
		Other: "{count} mincí",
	})
	cat.SetPlural("zh", "coins", l10n.PluralForms{
		Zero:  "没有金币",
		Other: "{count} 枚金币",
	})

	cases := []struct {
		locale, key string
		count       int
		want        string
	}{
		// Explicit zero override honored for every locale when count == 0.
		{"en", "coins", 0, "No coins"},
		{"en", "no_zero", 0, "0 coins"},
		{"fr", "items", 0, "0 objet"},
		{"fr", "items_zero", 0, "Aucun objet"},
		{"zh-Hans", "coins", 0, "没有金币"},
		{"zh-Hans", "coins", 1, "1 枚金币"},
		// French 1_000_000 selects Many when present, falls back to Other when omitted.
		{"fr", "items", 1_000_000, "1000000 objets"},
		{"fr", "items_zero", 1_000_000, "1000000 d'objets"},
		// Czech via BCP-47 region tag.
		{"cs-CZ", "coins", 1, "1 mince"},
		{"cs-CZ", "coins", 3, "3 mince (few)"},
		{"cs-CZ", "coins", 5, "5 mincí"},
	}

	for _, tc := range cases {
		if got := cat.Plural(tc.locale, tc.key, tc.count); got != tc.want {
			t.Errorf("Plural(%q, %q, %d) = %q, want %q", tc.locale, tc.key, tc.count, got, tc.want)
		}
	}
}

func TestPluralCategoriesAndDeadFormAudit(t *testing.T) {
	catChecks := []struct {
		locale string
		want   []l10n.PluralCategory
	}{
		{"en", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralOther}},
		{"zh-Hans", []l10n.PluralCategory{l10n.PluralOther}},
		{"cs-CZ", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralOther}},
		{"lt", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralOther}},
		{"lv", []l10n.PluralCategory{l10n.PluralZero, l10n.PluralOne, l10n.PluralOther}},
		{"ro", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralOther}},
		{"sl", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralTwo, l10n.PluralFew, l10n.PluralOther}},
		{"pl", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralMany}},
		{"ru", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralMany}},
		{"sr", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralFew, l10n.PluralOther}},
		{"fr", []l10n.PluralCategory{l10n.PluralOne, l10n.PluralMany, l10n.PluralOther}},
		{"ar", []l10n.PluralCategory{l10n.PluralZero, l10n.PluralOne, l10n.PluralTwo, l10n.PluralFew, l10n.PluralMany, l10n.PluralOther}},
		{"cy", []l10n.PluralCategory{l10n.PluralZero, l10n.PluralOne, l10n.PluralTwo, l10n.PluralFew, l10n.PluralMany, l10n.PluralOther}},
	}
	for _, tc := range catChecks {
		got := l10n.PluralCategories(tc.locale)
		if !slices.Equal(got, tc.want) {
			t.Errorf("PluralCategories(%q) = %v, want %v", tc.locale, got, tc.want)
		}
	}

	// Dead-form audit on a Catalog.
	cat := l10n.NewCatalog("en")
	cat.SetPlural("en", "valid", l10n.PluralForms{
		Zero:  "none",
		One:   "one",
		Other: "other",
	})
	cat.SetPlural("en", "dead", l10n.PluralForms{
		One:   "one",
		Few:   "few",
		Many:  "many",
		Other: "other",
	})
	cat.SetPlural("cs", "decimal_many", l10n.PluralForms{
		One:   "one",
		Few:   "few",
		Many:  "many (decimal only)",
		Other: "other",
	})
	cat.SetPlural("zh-Hans", "unused_one", l10n.PluralForms{
		Zero:  "zero allowed",
		One:   "never selected in zh",
		Other: "other",
	})
	cat.SetPlural("ru", "valid_ru", l10n.PluralForms{
		One:   "one",
		Few:   "few",
		Many:  "many",
		Other: "fallback other allowed",
	})
	cat.SetPlural("sr", "dead_many", l10n.PluralForms{
		One:   "one",
		Few:   "few",
		Many:  "serbian has no many",
		Other: "other",
	})

	want := []l10n.UnusedPluralForm{
		{Locale: "cs", Key: "decimal_many", Category: l10n.PluralMany},
		{Locale: "en", Key: "dead", Category: l10n.PluralFew},
		{Locale: "en", Key: "dead", Category: l10n.PluralMany},
		{Locale: "sr", Key: "dead_many", Category: l10n.PluralMany},
		{Locale: "zh-Hans", Key: "unused_one", Category: l10n.PluralOne},
	}
	got := cat.CheckPlurals()
	if !slices.Equal(got, want) {
		t.Fatalf("CheckPlurals() = %+v, want %+v", got, want)
	}

	// Scoped CheckPlurals audits only the requested locales.
	gotEN := cat.CheckPlurals("en", "ru")
	wantEN := []l10n.UnusedPluralForm{
		{Locale: "en", Key: "dead", Category: l10n.PluralFew},
		{Locale: "en", Key: "dead", Category: l10n.PluralMany},
	}
	if !slices.Equal(gotEN, wantEN) {
		t.Fatalf("CheckPlurals(en, ru) = %+v, want %+v", gotEN, wantEN)
	}

	// CheckStringTablePlurals matches Catalog.CheckPlurals.
	tbl, ok := cat.StringTable("en")
	if !ok {
		t.Fatal("StringTable(en) not found")
	}
	if gotTbl := l10n.CheckStringTablePlurals(tbl); !slices.Equal(gotTbl, wantEN) {
		t.Fatalf("CheckStringTablePlurals(en) = %+v, want %+v", gotTbl, wantEN)
	}
}
