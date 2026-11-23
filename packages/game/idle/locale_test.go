package idle_test

import (
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/idle"
)

// deLocale is a German locale in its canonical form, as EncodeLocale writes it.
const deLocale = `version: 1
name: de
decimal: ','
group: .
exponent: E
units:
  - exp: 3
    suffix: ' Tsd.'
  - exp: 6
    suffix: ' Mio.'
`

func mustLocale(t testing.TB, text string) *idle.Locale {
	t.Helper()
	l, err := idle.ParseLocale([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLocaleRoundTrip(t *testing.T) {
	de := mustLocale(t, deLocale)
	out, err := idle.EncodeLocale(de)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != deLocale {
		t.Errorf("EncodeLocale changed the canonical text:\n%s\nwant\n%s", out, deLocale)
	}

	for _, l := range []*idle.Locale{idle.English(), idle.ChineseSimplified(), idle.ChineseTraditional()} {
		if err := l.Validate(); err != nil {
			t.Fatalf("built-in %s: %v", l.Name, err)
		}
		first, err := idle.EncodeLocale(l)
		if err != nil {
			t.Fatal(err)
		}
		back, err := idle.ParseLocale(first)
		if err != nil {
			t.Fatalf("%s: %v\n%s", l.Name, err, first)
		}
		second, err := idle.EncodeLocale(back)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Errorf("%s does not round trip:\n%s\n%s", l.Name, first, second)
		}
	}

	zh, err := idle.EncodeLocale(idle.ChineseSimplified())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zh), "suffix: 万\n") {
		t.Errorf("Chinese suffixes are not written as plain UTF-8:\n%s", zh)
	}

	// A person's own spacing and quoting is normalized, and an empty unit list is written explicitly.
	loose := "version: 1\nname: plain\ndecimal: \".\"\ngroup: \"\"\nexponent: \"e\"\nunits: []\n"
	want := "version: 1\nname: plain\ndecimal: .\ngroup: \"\"\nexponent: e\nunits: []\n"
	l := mustLocale(t, loose)
	out, err = idle.EncodeLocale(l)
	if err != nil || string(out) != want {
		t.Errorf("EncodeLocale = %q, %v; want %q", out, err, want)
	}
	l.Units = nil
	if out, err = idle.EncodeLocale(l); err != nil || string(out) != want {
		t.Errorf("EncodeLocale with nil units = %q, %v; want %q", out, err, want)
	}
}

func TestParseLocaleRefuses(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"unknown field":   strings.Replace(deLocale, "group: .", "group: .\ncolor: red", 1),
		"wrong version":   strings.Replace(deLocale, "version: 1", "version: 2", 1),
		"no name":         strings.Replace(deLocale, "name: de", "name: \"\"", 1),
		"no decimal":      strings.Replace(deLocale, "decimal: ','", "decimal: \"\"", 1),
		"no exponent":     strings.Replace(deLocale, "exponent: E", "exponent: \"\"", 1),
		"same separators": strings.Replace(deLocale, "group: .", "group: ','", 1),
		"digit separator": strings.Replace(deLocale, "group: .", "group: \"0\"", 1),
		"sign exponent":   strings.Replace(deLocale, "exponent: E", "exponent: \"-\"", 1),
		"descending":      strings.Replace(deLocale, "exp: 6", "exp: 2", 1),
		"repeated exp":    strings.Replace(deLocale, "exp: 6", "exp: 3", 1),
		"zero exp":        strings.Replace(deLocale, "exp: 3", "exp: 0", 1),
		"huge exp":        strings.Replace(deLocale, "exp: 6", "exp: 1000001", 1),
		"empty suffix":    strings.Replace(deLocale, "' Mio.'", "\"\"", 1),
		"digit suffix":    strings.Replace(deLocale, "' Mio.'", "M2", 1),
		"repeated suffix": strings.Replace(deLocale, "' Mio.'", "' Tsd.'", 1),
		"trailing doc":    deLocale + "---\nversion: 1\n",
		"not a mapping":   "- 1\n",
	}
	for name, text := range cases {
		if _, err := idle.ParseLocale([]byte(text)); err == nil {
			t.Errorf("%s: ParseLocale accepted\n%s", name, text)
		}
	}
	if err := (*idle.Locale)(nil).Validate(); err == nil {
		t.Error("nil locale validated")
	}
	bad := idle.English()
	bad.Version = 0
	if _, err := idle.EncodeLocale(bad); err == nil {
		t.Error("EncodeLocale accepted version 0")
	}
}

func TestLocaleFor(t *testing.T) {
	cases := map[string]string{
		"":           "en",
		"en":         "en",
		"en-US":      "en",
		"fr":         "en",
		"zh":         "zh-Hans",
		"zh-CN":      "zh-Hans",
		"zh_Hans_SG": "zh-Hans",
		"zh-Hant":    "zh-Hant",
		"zh-TW":      "zh-Hant",
		"ZH_hk":      "zh-Hant",
		"zh-MO":      "zh-Hant",
		"zh-Hans-HK": "zh-Hans",
	}
	for tag, want := range cases {
		if got := idle.LocaleFor(tag).Name; got != want {
			t.Errorf("LocaleFor(%q) = %s, want %s", tag, got, want)
		}
	}
	// Built-ins are copies: changing one does not change the next.
	l := idle.English()
	l.Units[0].Suffix = "k"
	if idle.English().Units[0].Suffix != "K" {
		t.Error("English() shares its units")
	}
}

func FuzzParseLocale(f *testing.F) {
	f.Add([]byte(deLocale))
	f.Add([]byte("version: 1\nname: x\ndecimal: .\ngroup: \"\"\nexponent: e\nunits: []\n"))
	f.Add([]byte("version: 1\nunits: [{exp: 3, suffix: K}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := idle.ParseLocale(data)
		if err != nil {
			return
		}
		first, err := idle.EncodeLocale(l)
		if err != nil {
			t.Fatalf("a parsed locale does not encode: %v", err)
		}
		back, err := idle.ParseLocale(first)
		if err != nil {
			t.Fatalf("an encoded locale does not parse: %v\n%s", err, first)
		}
		second, err := idle.EncodeLocale(back)
		if err != nil || string(second) != string(first) {
			t.Fatalf("locale does not round trip:\n%s\n%s", first, second)
		}
		_ = idle.Format{Locale: l}.Format(idle.NewExp(1.234, 50))
	})
}
