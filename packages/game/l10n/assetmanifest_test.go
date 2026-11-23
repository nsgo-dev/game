package l10n_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/l10n"
)

const canonicalAssetManifest = `version: 1
base_locale: en
assets:
  sfx/click.wav:
    en: sfx/click.wav
  ui/title_banner.png:
    en: ui/title_banner.png
    zh-Hans: ui/title_banner.zh-Hans.png
  voice/intro.wav:
    en: locales/en/voice/intro.wav
    ja: locales/ja/voice/intro.wav
    zh-Hans: locales/zh-Hans/voice/intro.wav
groups:
  shared:
    - sfx/click.wav
  voice:
    - voice/intro.wav
`

func TestAssetCatalogLoadAssetManifestYAML(t *testing.T) {
	cat := l10n.NewAssetCatalog("fr")
	if err := cat.LoadAssetManifestYAML([]byte(canonicalAssetManifest)); err != nil {
		t.Fatalf("LoadAssetManifestYAML: %v", err)
	}
	if got := cat.BaseLocale(); got != "en" {
		t.Errorf("BaseLocale() = %q, want en", got)
	}
	if got, ok := cat.Resolve("voice/intro.wav", "ja"); !ok || got.Path != "locales/ja/voice/intro.wav" {
		t.Errorf("Resolve(voice/intro.wav, ja) = %+v, %v", got, ok)
	}
	if got, ok := cat.Resolve("ui/title_banner.png", "zh-Hans-CN"); !ok || got.Path != "ui/title_banner.zh-Hans.png" || !got.IsFallback {
		t.Errorf("Resolve(ui/title_banner.png, zh-Hans-CN) = %+v, %v", got, ok)
	}
	if got, ok := cat.Resolve("sfx/click.wav", "ja"); !ok || got.ResolvedLocale != "en" {
		t.Errorf("Resolve(sfx/click.wav, ja) = %+v, %v; want the base locale variant", got, ok)
	}
	if got := cat.Groups(); !slices.Equal(got, []string{"shared", "voice"}) {
		t.Errorf("Groups() = %v", got)
	}
	if got := cat.AffectedGroups("en", "ja"); !slices.Equal(got, []string{"voice"}) {
		t.Errorf("AffectedGroups(en, ja) = %v, want [voice]", got)
	}
}

func TestAssetManifestRoundTripIsLossless(t *testing.T) {
	m, err := l10n.ParseAssetManifest([]byte(canonicalAssetManifest))
	if err != nil {
		t.Fatalf("ParseAssetManifest: %v", err)
	}
	out, err := l10n.EncodeAssetManifest(m)
	if err != nil {
		t.Fatalf("EncodeAssetManifest: %v", err)
	}
	if string(out) != canonicalAssetManifest {
		t.Errorf("document round trip changed the manifest:\n--- got\n%s--- want\n%s", out, canonicalAssetManifest)
	}

	cat := l10n.NewAssetCatalog("")
	if err := cat.LoadAssetManifestYAML([]byte(canonicalAssetManifest)); err != nil {
		t.Fatalf("LoadAssetManifestYAML: %v", err)
	}
	saved, err := cat.MarshalAssetManifestYAML()
	if err != nil {
		t.Fatalf("MarshalAssetManifestYAML: %v", err)
	}
	if string(saved) != canonicalAssetManifest {
		t.Errorf("catalog load then save changed the manifest:\n--- got\n%s--- want\n%s", saved, canonicalAssetManifest)
	}
}

func TestAssetManifestEncodeIsCanonical(t *testing.T) {
	m := &l10n.AssetManifest{
		Version: l10n.AssetManifestVersion,
		Assets: map[string]map[string]string{
			"b.wav": {"ja": "b.ja.wav", "en": "b.wav"},
			"a.wav": {"en": "a.wav"},
		},
		Groups: map[string][]string{
			"z": {"b.wav", "a.wav"},
			"a": nil,
		},
	}
	first, err := l10n.EncodeAssetManifest(m)
	if err != nil {
		t.Fatalf("EncodeAssetManifest: %v", err)
	}
	want := `version: 1
assets:
  a.wav:
    en: a.wav
  b.wav:
    en: b.wav
    ja: b.ja.wav
groups:
  a: []
  z:
    - a.wav
    - b.wav
`
	if string(first) != want {
		t.Errorf("EncodeAssetManifest =\n%s\nwant\n%s", first, want)
	}
	if got := m.Groups["z"]; !slices.Equal(got, []string{"b.wav", "a.wav"}) {
		t.Errorf("EncodeAssetManifest reordered the caller's group slice: %v", got)
	}
	for range 3 {
		again, err := l10n.EncodeAssetManifest(m)
		if err != nil || !bytes.Equal(again, first) {
			t.Fatalf("EncodeAssetManifest is not deterministic: %v\n%s", err, again)
		}
	}
}

func TestAssetCatalogAssetManifestSnapshot(t *testing.T) {
	cat := l10n.NewAssetCatalog("en_us")
	cat.Register("voice/intro.wav", "ja", "  locales/ja/voice/intro.wav ")
	cat.RegisterGroup("voice", "voice/intro.wav", "voice/boss.wav")
	cat.RegisterGroup("empty")

	m := cat.AssetManifest()
	if m.Version != l10n.AssetManifestVersion || m.BaseLocale != "en-US" {
		t.Fatalf("AssetManifest() = %+v", m)
	}
	if got := m.Assets["voice/intro.wav"]["ja"]; got != "locales/ja/voice/intro.wav" {
		t.Errorf("snapshot path = %q", got)
	}
	if got := m.Groups["voice"]; !slices.Equal(got, []string{"voice/boss.wav", "voice/intro.wav"}) {
		t.Errorf("snapshot group = %v", got)
	}
	m.Assets["voice/intro.wav"]["ja"] = "changed"
	if got, _ := cat.Resolve("voice/intro.wav", "ja"); got.Path != "locales/ja/voice/intro.wav" {
		t.Errorf("mutating the snapshot changed the catalog: %+v", got)
	}

	saved, err := cat.MarshalAssetManifestYAML()
	if err != nil {
		t.Fatalf("MarshalAssetManifestYAML: %v", err)
	}
	reloaded := l10n.NewAssetCatalog("")
	if err := reloaded.LoadAssetManifestYAML(saved); err != nil {
		t.Fatalf("LoadAssetManifestYAML(saved): %v\n%s", err, saved)
	}
	again, err := reloaded.MarshalAssetManifestYAML()
	if err != nil || !bytes.Equal(again, saved) {
		t.Errorf("save, load, save is not stable: %v\n--- first\n%s--- second\n%s", err, saved, again)
	}
	if got := reloaded.GroupAssets("empty"); got != nil {
		t.Errorf("GroupAssets(empty) = %v, want none", got)
	}
	if got := reloaded.Groups(); !slices.Equal(got, []string{"empty", "voice"}) {
		t.Errorf("reloaded Groups() = %v", got)
	}
}

func TestAssetManifestMergesIntoCatalog(t *testing.T) {
	cat := l10n.NewAssetCatalog("en")
	cat.Register("voice/intro.wav", "en", "old/intro.wav")
	cat.RegisterGroup("voice", "voice/boss.wav")
	err := cat.LoadAssetManifestYAML([]byte(`version: 1
assets:
  voice/intro.wav:
    en: new/intro.wav
groups:
  voice: [voice/intro.wav]
`))
	if err != nil {
		t.Fatalf("LoadAssetManifestYAML: %v", err)
	}
	if got := cat.BaseLocale(); got != "en" {
		t.Errorf("BaseLocale() = %q, want en kept when the manifest omits base_locale", got)
	}
	if got, _ := cat.Resolve("voice/intro.wav", "en"); got.Path != "new/intro.wav" {
		t.Errorf("Resolve = %+v, want the manifest path", got)
	}
	if got := cat.GroupAssets("voice"); !slices.Equal(got, []string{"voice/boss.wav", "voice/intro.wav"}) {
		t.Errorf("GroupAssets(voice) = %v", got)
	}
}

func TestAssetManifestRefusesInvalidDocuments(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want string
	}{
		"empty":             {"", "empty asset manifest"},
		"not a mapping":     {"- a\n", "decode asset manifest"},
		"missing version":   {"assets:\n  a.wav:\n    en: a.wav\n", "missing version"},
		"future version":    {"version: 2\n", "unsupported asset manifest version 2"},
		"unknown field":     {"version: 1\nbaseLocale: en\n", "field baseLocale not found"},
		"unknown top field": {"version: 1\nlocales: [en]\n", "field locales not found"},
		"trailing document": {"version: 1\n---\nversion: 1\n", "trailing YAML document"},
		"duplicate key":     {"version: 1\nassets:\n  a.wav:\n    en: a.wav\n    en: b.wav\n", "already defined"},
		"non-string path":   {"version: 1\nassets:\n  a.wav:\n    en: [a.wav]\n", "decode asset manifest"},
		"empty path":        {"version: 1\nassets:\n  a.wav:\n    en: \"\"\n", "empty path"},
		"padded path":       {"version: 1\nassets:\n  a.wav:\n    en: \" a.wav\"\n", "surrounding spaces"},
		"no variants":       {"version: 1\nassets:\n  a.wav: {}\n", "no locale variants"},
		"null variants":     {"version: 1\nassets:\n  a.wav:\n", "no locale variants"},
		"bad asset name":    {"version: 1\nassets:\n  ../a.wav:\n    en: a.wav\n", "invalid asset name"},
		"unclean name":      {"version: 1\nassets:\n  ./ui/a.png:\n    en: a.png\n", `must be written as "ui/a.png"`},
		"unnormal locale":   {"version: 1\nassets:\n  a.wav:\n    zh_hans: a.wav\n", `must be written as "zh-Hans"`},
		"unnormal base":     {"version: 1\nbase_locale: EN\n", `must be written as "en"`},
		"empty group name":  {"version: 1\ngroups:\n  \"\": [a.wav]\n", "group name"},
		"duplicate member":  {"version: 1\ngroups:\n  voice: [a.wav, a.wav]\n", "more than once"},
		"bad member":        {"version: 1\ngroups:\n  voice: [\"\"]\n", "invalid asset name"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cat := l10n.NewAssetCatalog("en")
			err := cat.LoadAssetManifestYAML([]byte(tc.doc))
			if err == nil {
				t.Fatalf("LoadAssetManifestYAML(%q) = nil, want error containing %q", tc.doc, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadAssetManifestYAML(%q) = %v, want error containing %q", tc.doc, err, tc.want)
			}
			if got := cat.Names(); len(got) != 0 {
				t.Errorf("a refused manifest registered assets %v", got)
			}
		})
	}
	if err := l10n.ValidateAssetManifest(nil); err == nil {
		t.Error("ValidateAssetManifest(nil) = nil, want error")
	}
	if _, err := l10n.EncodeAssetManifest(&l10n.AssetManifest{}); err == nil {
		t.Error("EncodeAssetManifest without a version = nil error, want error")
	}
}

func FuzzAssetManifestRoundTrip(f *testing.F) {
	f.Add([]byte(canonicalAssetManifest))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\ngroups:\n  voice: [b.wav, a.wav]\n"))
	f.Add([]byte("version: 1\nassets:\n  \"1\":\n    en: \"null\"\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := l10n.ParseAssetManifest(data)
		if err != nil {
			return
		}
		first, err := l10n.EncodeAssetManifest(m)
		if err != nil {
			t.Fatalf("EncodeAssetManifest of a parsed manifest: %v", err)
		}
		m2, err := l10n.ParseAssetManifest(first)
		if err != nil {
			t.Fatalf("ParseAssetManifest of encoded output: %v\n%s", err, first)
		}
		second, err := l10n.EncodeAssetManifest(m2)
		if err != nil {
			t.Fatalf("EncodeAssetManifest second pass: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("encode is not a fixed point:\n--- first\n%s--- second\n%s", first, second)
		}
		cat := l10n.NewAssetCatalog("")
		if err := cat.ApplyAssetManifest(m2); err != nil {
			t.Fatalf("ApplyAssetManifest: %v", err)
		}
		saved, err := cat.MarshalAssetManifestYAML()
		if err != nil {
			t.Fatalf("MarshalAssetManifestYAML: %v", err)
		}
		if !bytes.Equal(saved, first) {
			t.Fatalf("catalog save differs from document encode:\n--- catalog\n%s--- document\n%s", saved, first)
		}
	})
}

func BenchmarkLoadAssetManifestYAML(b *testing.B) {
	data := []byte(canonicalAssetManifest)
	b.ReportAllocs()
	for b.Loop() {
		cat := l10n.NewAssetCatalog("en")
		if err := cat.LoadAssetManifestYAML(data); err != nil {
			b.Fatal(err)
		}
	}
}
