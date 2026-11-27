package l10n_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

func TestLoadJSONErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, json, field string
	}{
		{"not an object", `null`, ""},
		{"number value", `{"menu": {"play": "Play", "quit": 3}}`, "menu.quit"},
		{"empty key", `{"hud": {"": "x"}}`, `hud[""]`},
		{"array value", `{"title": "Quest", "tags": ["a"]}`, "tags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := loaderrtest.Indent(tc.json)
			err := l10n.NewCatalog("en").LoadJSON("ja", data)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "locales/ja.json"), "locales/ja.json", data, tc.field)
		})
	}
	t.Run("syntax", func(t *testing.T) {
		err := l10n.NewCatalog("en").LoadJSON("ja", []byte("{\n  \"a\": \"b\"\n  \"c\": \"d\"\n}"))
		loaderrtest.Expect(t, loaderr.WithFile(err, "locales/ja.json"), "locales/ja.json", 3)
	})
}

func TestLoadAssetManifestErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, yaml, field string
	}{
		{"locale", "version: 1\nassets:\n  voice/intro.wav:\n    en: a.wav\n    zh_hans: b.wav\n", `assets["voice/intro.wav"].zh_hans`},
		{"empty path", "version: 1\nassets:\n  intro:\n    en: \"\"\n", "assets.intro.en"},
		{"no variants", "version: 1\nassets:\n  intro: {}\n", "assets.intro"},
		{"duplicate member", "version: 1\ngroups:\n  voice:\n    - a\n    - a\n", "groups.voice[1]"},
		{"version", "version: 2\n", "version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.yaml)
			err := l10n.NewAssetCatalog("en").LoadAssetManifestYAML(data)
			loaderrtest.ExpectField(t, loaderr.WithFile(err, "locales/assets.yaml"), "locales/assets.yaml", data, tc.field)
		})
	}
	t.Run("unknown field", func(t *testing.T) {
		err := l10n.NewAssetCatalog("en").LoadAssetManifestYAML([]byte("version: 1\nbaseLocale: en\n"))
		loaderrtest.Expect(t, loaderr.WithFile(err, "locales/assets.yaml"), "locales/assets.yaml", 2)
	})
}
