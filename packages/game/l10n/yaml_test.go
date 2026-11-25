package l10n_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
)

func TestLoadYAMLMatchesEquivalentJSON(t *testing.T) {
	enJSON := []byte(`{
		"title": "Dungeon Quest",
		"hud.score": "Score: {score}",
		"menu": {
			"play": "Play",
			"settings": {
				"audio": "Audio",
				"language": "Language"
			}
		},
		"coins": {
			"zero": "No coins",
			"one": "{count} coin",
			"other": "{count} coins"
		},
		"inventory": {
			"items": {
				"one": "{count} item in {bag}",
				"other": "{count} items in {bag}"
			}
		}
	}`)
	zhJSON := []byte(`{
		"title": "地牢探险",
		"hud.score": "得分：{score}",
		"menu": {"play": "开始游戏"},
		"coins": {"other": "{count} 枚金币"}
	}`)
	arJSON := []byte(`{
		"gems": {
			"zero": "لا جواهر",
			"one": "جوهرة واحدة",
			"two": "جوهرتان",
			"few": "{count} جواهر",
			"many": "{count} جوهرة",
			"other": "{count} جوهرة (other)"
		}
	}`)
	ruJSON := []byte(`{
		"keys": {
			"one": "{count} ключ",
			"few": "{count} ключа",
			"many": "{count} ключей",
			"other": "{count} ключей"
		}
	}`)

	enYAML := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json
version: 1
locale: en
strings:
  hud.score: "Score: {score}"
  menu.play: Play
  menu.settings.audio: Audio
  menu.settings.language: Language
  title: Dungeon Quest
plurals:
  coins:
    zero: No coins
    one: "{count} coin"
    other: "{count} coins"
  inventory.items:
    one: "{count} item in {bag}"
    other: "{count} items in {bag}"
`)
	zhYAML := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json
version: 1
locale: zh-Hans
strings:
  hud.score: 得分：{score}
  menu.play: 开始游戏
  title: 地牢探险
plurals:
  coins:
    other: "{count} 枚金币"
`)
	arYAML := []byte(`version: 1
locale: ar
plurals:
  gems:
    zero: لا جواهر
    one: جوهرة واحدة
    two: جوهرتان
    few: "{count} جواهر"
    many: "{count} جوهرة"
    other: "{count} جوهرة (other)"
`)
	ruYAML := []byte(`version: 1
locale: ru
plurals:
  keys:
    one: "{count} ключ"
    few: "{count} ключа"
    many: "{count} ключей"
    other: "{count} ключей"
`)

	jsonCat := l10n.NewCatalog("en")
	if err := jsonCat.LoadJSON("en", enJSON); err != nil {
		t.Fatalf("LoadJSON en: %v", err)
	}
	if err := jsonCat.LoadJSON("zh-Hans", zhJSON); err != nil {
		t.Fatalf("LoadJSON zh-Hans: %v", err)
	}
	if err := jsonCat.LoadJSON("ar", arJSON); err != nil {
		t.Fatalf("LoadJSON ar: %v", err)
	}
	if err := jsonCat.LoadJSON("ru", ruJSON); err != nil {
		t.Fatalf("LoadJSON ru: %v", err)
	}

	yamlCat := l10n.NewCatalog("en")
	for _, raw := range [][]byte{enYAML, zhYAML, arYAML, ruYAML} {
		if err := yamlCat.LoadYAML(raw); err != nil {
			t.Fatalf("LoadYAML: %v\n%s", err, raw)
		}
	}

	if got, want := strings.Join(yamlCat.Locales(), ","), strings.Join(jsonCat.Locales(), ","); got != want {
		t.Fatalf("Locales = %q, want %q", got, want)
	}

	tCases := []struct {
		locale, key string
		args        []l10n.Arg
	}{
		{"en", "title", nil},
		{"en", "hud.score", []l10n.Arg{l10n.Param("score", 42)}},
		{"en", "menu.play", nil},
		{"en", "menu.settings.audio", nil},
		{"en", "menu.settings.language", nil},
		{"zh-Hans-CN", "title", nil},
		{"zh-Hans-CN", "menu.play", nil},
		{"zh-Hans-CN", "menu.settings.audio", nil}, // falls back to en
		{"zh-Hans", "hud.score", []l10n.Arg{l10n.Param("score", 99)}},
		{"en", "coins", nil},
		{"en", "missing.key", nil},
	}
	for _, tc := range tCases {
		got := yamlCat.T(tc.locale, tc.key, tc.args...)
		want := jsonCat.T(tc.locale, tc.key, tc.args...)
		if got != want {
			t.Errorf("T(%q, %q) = %q, want %q", tc.locale, tc.key, got, want)
		}
		if yamlCat.Has(tc.locale, tc.key) != jsonCat.Has(tc.locale, tc.key) {
			t.Errorf("Has(%q, %q) mismatch", tc.locale, tc.key)
		}
	}

	pluralCases := []struct {
		locale, key string
		count       int
		args        []l10n.Arg
	}{
		{"en", "coins", 0, nil},
		{"en", "coins", 1, nil},
		{"en", "coins", 5, nil},
		{"en", "inventory.items", 1, []l10n.Arg{l10n.Param("bag", "satchel")}},
		{"en", "inventory.items", 4, []l10n.Arg{l10n.Param("bag", "satchel")}},
		{"zh-Hans-CN", "coins", 3, nil},
		{"ar", "gems", 0, nil},
		{"ar", "gems", 1, nil},
		{"ar", "gems", 2, nil},
		{"ar", "gems", 5, nil},
		{"ar", "gems", 25, nil},
		{"ar", "gems", 100, nil},
		{"ru", "keys", 1, nil},
		{"ru", "keys", 2, nil},
		{"ru", "keys", 5, nil},
		{"ru", "keys", 11, nil},
		{"ru", "keys", 21, nil},
	}
	for _, tc := range pluralCases {
		got := yamlCat.Plural(tc.locale, tc.key, tc.count, tc.args...)
		want := jsonCat.Plural(tc.locale, tc.key, tc.count, tc.args...)
		if got != want {
			t.Errorf("Plural(%q, %q, %d) = %q, want %q", tc.locale, tc.key, tc.count, got, want)
		}
	}
}

func TestStringTableRoundTripAndDeterminism(t *testing.T) {
	tbl := &l10n.StringTable{
		Version: l10n.StringTableVersion,
		Locale:  "en",
		Strings: map[string]string{
			"z.last":    "Last",
			"a.first":   "First",
			"hud.score": "Score: {score}",
		},
		Plurals: map[string]l10n.PluralForms{
			"coins": {
				Zero:  "No coins",
				One:   "{count} coin",
				Other: "{count} coins",
			},
			"apples": {
				One:   "{count} apple",
				Other: "{count} apples",
			},
		},
	}

	b, err := l10n.EncodeYAML(tbl)
	if err != nil {
		t.Fatalf("EncodeYAML: %v", err)
	}
	if !bytes.HasPrefix(b, []byte(l10n.SchemaComment+"\n")) {
		t.Fatalf("EncodeYAML output missing schema comment:\n%s", b)
	}

	// Verify sorted key order in YAML output.
	s := string(b)
	if strings.Index(s, "a.first:") > strings.Index(s, "hud.score:") ||
		strings.Index(s, "hud.score:") > strings.Index(s, "z.last:") {
		t.Fatalf("strings keys are not sorted:\n%s", s)
	}
	if strings.Index(s, "apples:") > strings.Index(s, "coins:") {
		t.Fatalf("plurals keys are not sorted:\n%s", s)
	}

	// Byte-for-byte lossless round-trip: EncodeYAML(ParseYAML(b)) == b.
	parsed, err := l10n.ParseYAML(b)
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	again, err := l10n.EncodeYAML(parsed)
	if err != nil {
		t.Fatalf("EncodeYAML after ParseYAML: %v", err)
	}
	if !bytes.Equal(again, b) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", again, b)
	}

	// Catalog.MarshalYAML produces the exact same canonical bytes.
	cat := l10n.NewCatalog("en")
	if err := cat.LoadYAML(b); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	marshaled, err := cat.MarshalYAML("en")
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if !bytes.Equal(marshaled, b) {
		t.Fatalf("MarshalYAML mismatch:\ngot:\n%s\nwant:\n%s", marshaled, b)
	}

	if _, err := cat.MarshalYAML("ja"); err == nil {
		t.Fatal("MarshalYAML accepted missing locale")
	}
}

func TestLoadYAMLMergesAndIsAtomicOnError(t *testing.T) {
	cat := l10n.NewCatalog("en")
	if err := cat.LoadYAML([]byte("version: 1\nlocale: en\nstrings:\n  a: one\n  b: two\n")); err != nil {
		t.Fatalf("LoadYAML 1: %v", err)
	}
	if err := cat.LoadYAML([]byte("version: 1\nlocale: en\nstrings:\n  b: TWO\n  c: three\n")); err != nil {
		t.Fatalf("LoadYAML 2: %v", err)
	}
	if cat.T("en", "a") != "one" || cat.T("en", "b") != "TWO" || cat.T("en", "c") != "three" {
		t.Fatalf("merge failed: a=%q b=%q c=%q", cat.T("en", "a"), cat.T("en", "b"), cat.T("en", "c"))
	}

	// Invalid YAML must not touch existing entries.
	if err := cat.LoadYAML([]byte("version: 1\nlocale: en\nstrings:\n  a: corrupted\nunknown: 1\n")); err == nil {
		t.Fatal("LoadYAML accepted unknown field")
	}
	if cat.T("en", "a") != "one" {
		t.Fatalf("invalid LoadYAML mutated catalog: a=%q", cat.T("en", "a"))
	}
}

func TestParseYAMLRefusesInvalidDocuments(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{"empty", "", "empty"},
		{"comments only", "# just a comment\n", "empty"},
		{"missing version", "locale: en\nstrings:\n  a: b\n", "version"},
		{"zero version", "version: 0\nlocale: en\n", "version"},
		{"future version", "version: 2\nlocale: en\n", "version"},
		{"hex version", "version: 0x1\nlocale: en\n", "version"},
		{"missing locale", "version: 1\nstrings:\n  a: b\n", "locale"},
		{"non-canonical locale", "version: 1\nlocale: zh_hans\n", "zh-Hans"},
		{"invalid locale tag", "version: 1\nlocale: en/us\n", "BCP-47"},
		{"unknown top-level field", "version: 1\nlocale: en\nextra: true\n", "extra"},
		{"unknown plural category", "version: 1\nlocale: en\nplurals:\n  coins:\n    singular: 1 coin\n    other: coins\n", "singular"},
		{"duplicate key in strings", "version: 1\nlocale: en\nstrings:\n  a: one\n  a: two\n", "already defined"},
		{"overlap strings and plurals", "version: 1\nlocale: en\nstrings:\n  coins: Coins\nplurals:\n  coins:\n    other: \"{count} coins\"\n", "both strings and plurals"},
		{"empty key", "version: 1\nlocale: en\nstrings:\n  \"\": empty\n", "empty message key"},
		{"padded key", "version: 1\nlocale: en\nstrings:\n  \" a \": padded\n", "surrounding spaces"},
		{"empty plural entry", "version: 1\nlocale: en\nplurals:\n  coins: {}\n", "no plural forms"},
		{"null string value", "version: 1\nlocale: en\nstrings:\n  a: null\n", "null"},
		{"null plural value", "version: 1\nlocale: en\nplurals:\n  coins:\n    one: null\n    other: coins\n", "null"},
		{"trailing document", "version: 1\nlocale: en\n---\nversion: 1\nlocale: ja\n", "trailing"},
		{"anchor and alias", "version: 1\nlocale: en\nstrings:\n  a: &v Hello\n  b: *v\n", "anchor"},
		{"merge key", "version: 1\nlocale: en\nstrings:\n  <<: {a: b}\n", "merge"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := l10n.ParseYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("ParseYAML accepted %s:\n%s", tc.name, tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("ParseYAML error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestLoadYAMLErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, yaml, field string
	}{
		{"locale", "version: 1\nlocale: zh_hans\n", "locale"},
		{"version", "version: 2\nlocale: en\n", "version"},
		{"overlap", "version: 1\nlocale: en\nstrings:\n  coins: Coins\nplurals:\n  coins:\n    other: coins\n", "plurals.coins"},
		{"empty plural", "version: 1\nlocale: en\nplurals:\n  coins: {}\n", "plurals.coins"},
		{"null string", "version: 1\nlocale: en\nstrings:\n  menu.play: null\n", `strings["menu.play"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.yaml)
			err := l10n.NewCatalog("en").LoadYAML(data)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "locales/en.yaml"), "locales/en.yaml", data, tc.field)
		})
	}
	t.Run("unknown plural form", func(t *testing.T) {
		err := l10n.NewCatalog("en").LoadYAML([]byte("version: 1\nlocale: en\nplurals:\n  coins:\n    singular: 1\n    other: n\n"))
		loaderrtest.Expect(t, loaderr.WithFile(err, "locales/en.yaml"), "locales/en.yaml", 5)
	})
}
