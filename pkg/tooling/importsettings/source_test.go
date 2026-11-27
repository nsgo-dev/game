package importsettings_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/importsettings"
)

func TestAssetSourceRoundTripAndValidation(t *testing.T) {
	seed := int64(81515)
	src := &importsettings.AssetSource{
		Version:       importsettings.AssetSourceVersion,
		Generator:     "pixel-gen",
		Model:         "pixflux-1",
		Prompt:        "ice slime, 16 px pixel art, 4-colour shading, 1 px dark outline, facing right",
		Seed:          &seed,
		References:    []string{"art/characters/slime.png", "brief.yaml#art"},
		License:       "pixellab-commercial-1.2",
		CommercialUse: true,
		ApprovedBy:    "kim",
	}
	raw, err := importsettings.EncodeAssetSource(src)
	if err != nil {
		t.Fatalf("EncodeAssetSource: %v", err)
	}
	if !strings.HasPrefix(string(raw), importsettings.AssetSourceSchemaComment+"\n") {
		t.Fatalf("encoded source missing schema comment:\n%s", raw)
	}
	parsed, err := importsettings.ParseAssetSource(raw)
	if err != nil {
		t.Fatalf("ParseAssetSource: %v", err)
	}
	again, err := importsettings.EncodeAssetSource(parsed)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(raw, again) {
		t.Fatalf("round-trip mismatch:\ngot:\n%s\nwant:\n%s", again, raw)
	}
	if importsettings.IsUnknownLicense(parsed.License) {
		t.Fatalf("expected %q to be a known license", parsed.License)
	}
}

func TestAssetSourceUnknownLicenseAndErrors(t *testing.T) {
	for _, lic := range []string{"", "unknown", "Unknown", "NONE", "NOASSERTION", "tbd"} {
		if !importsettings.IsUnknownLicense(lic) {
			t.Errorf("IsUnknownLicense(%q) = false, want true", lic)
		}
	}
	for _, lic := range []string{"CC0-1.0", "pixellab-commercial-1.2", "LicenseRef-Studio"} {
		if importsettings.IsUnknownLicense(lic) {
			t.Errorf("IsUnknownLicense(%q) = true, want false", lic)
		}
	}

	// Unknown license must still parse so release/credits checks can report and block it.
	parsed, err := importsettings.ParseAssetSource([]byte("version: 1\ngenerator: sfx-gen\nprompt: torch flicker\nlicense: unknown\ncommercialUse: false\n"))
	if err != nil {
		t.Fatalf("ParseAssetSource with unknown license failed: %v", err)
	}
	if !importsettings.IsUnknownLicense(parsed.License) {
		t.Fatalf("expected parsed license %q to be unknown", parsed.License)
	}

	// Unknown field, anchors, duplicate keys, missing required fields are refused.
	badCases := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown field", "version: 1\ngenerator: pixel-gen\nprompt: slime\nextra: 1\n", "extra"},
		{"missing version", "generator: pixel-gen\nprompt: slime\n", "version"},
		{"future version", "version: 2\ngenerator: pixel-gen\nprompt: slime\n", "version"},
		{"missing generator", "version: 1\nprompt: slime\n", "generator"},
		{"missing prompt", "version: 1\ngenerator: pixel-gen\n", "prompt"},
		{"duplicate key", "version: 1\ngenerator: a\ngenerator: b\nprompt: x\n", "already defined"},
		{"anchor", "version: 1\ngenerator: &g pixel-gen\nprompt: *g\n", "anchor"},
	}
	for _, tc := range badCases {
		if _, err := importsettings.ParseAssetSource([]byte(tc.yaml)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got err=%v, want containing %q", tc.name, err, tc.want)
		}
	}
}
