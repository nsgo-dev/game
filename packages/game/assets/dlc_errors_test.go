package assets_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/assets"
	"github.com/nsgo-dev/game/packages/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/packages/game/loaderr"
)

func TestParsePackManifestErrorsNameFileLineAndFix(t *testing.T) {
	cases := []struct {
		name, json string
		line       int
	}{
		{"object", "{\n  \"packs\": [\n    {\"id\": \"chapter-2\", \"size\": \"big\"}\n  ]\n}", 3},
		{"array", "\n\n[\n  {\"id\": \"chapter-2\"},\n  {\"id\": 2}\n]", 5},
		{"syntax", "{\n  \"packs\": [\n    {\"id\": \"chapter-2\",}\n  ]\n}", 3},
		{"empty", "  \n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assets.ParsePackManifest([]byte(tc.json))
			loaderrtest.Expect(t, loaderr.WithFile(err, "dlc.json"), "dlc.json", tc.line)
		})
	}
}
