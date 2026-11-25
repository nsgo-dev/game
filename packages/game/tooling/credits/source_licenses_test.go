package credits_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/credits"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

func TestCheckGeneratedLicensesBlocksUnknown(t *testing.T) {
	root, a := newProject(t)
	for _, rel := range wantAssets {
		writeFile(t, root, importsettings.Path(rel), []byte("version: 1\ncredit: {author: Studio, license: CC0-1.0}\n"))
	}
	writeFile(t, root, "audio/sfx/torch_flicker.wav", []byte("RIFF"))
	writeFile(t, root, "audio/sfx/torch_flicker.wav.source.yaml", []byte("version: 1\ngenerator: sfx-gen\nprompt: torch flicker\nlicense: unknown\ncommercialUse: false\n"))

	_, err := credits.Collect(root, a)
	var unk *credits.UnknownLicenseError
	if !errors.As(err, &unk) {
		t.Fatalf("Collect error = %v, want *UnknownLicenseError", err)
	}
	if !slices.Equal(unk.Assets, []string{"audio/sfx/torch_flicker.wav"}) {
		t.Fatalf("UnknownLicenseError.Assets = %v, want [audio/sfx/torch_flicker.wav]", unk.Assets)
	}
	if !strings.Contains(err.Error(), "audio/sfx/torch_flicker.wav") {
		t.Fatalf("error %q does not name blocked asset path", err.Error())
	}

	// Empty license also blocks release.
	writeFile(t, root, "audio/sfx/torch_flicker.wav.source.yaml", []byte("version: 1\ngenerator: sfx-gen\nprompt: torch flicker\ncommercialUse: false\n"))
	if _, err := credits.CheckGeneratedLicenses(root); !errors.As(err, &unk) || !strings.Contains(err.Error(), "audio/sfx/torch_flicker.wav") {
		t.Fatalf("CheckGeneratedLicenses(empty) = %v, want *UnknownLicenseError naming audio/sfx/torch_flicker.wav", err)
	}

	// Setting a known license unblocks Collect.
	writeFile(t, root, "audio/sfx/torch_flicker.wav.source.yaml", []byte("version: 1\ngenerator: sfx-gen\nmodel: sfx-v1\nprompt: torch flicker\nlicense: CC0-1.0\ncommercialUse: true\napprovedBy: kim\n"))
	if _, err := credits.Collect(root, a); err != nil {
		t.Fatalf("Collect after setting license: %v", err)
	}
}

func TestCollectUsesKnownSourceSidecarCredit(t *testing.T) {
	root, a := newProject(t)
	for _, rel := range wantAssets {
		if rel == "art/sky.png" {
			writeFile(t, root, importsettings.AssetSourcePath(rel), []byte("version: 1\ngenerator: pixel-gen\nmodel: pixflux-1\nprompt: night sky\nlicense: pixellab-commercial-1.2\ncommercialUse: true\napprovedBy: kim\n"))
			continue
		}
		writeFile(t, root, importsettings.Path(rel), []byte("version: 1\ncredit: {author: Studio, license: CC0-1.0}\n"))
	}
	entries, err := credits.Collect(root, a)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Asset == "art/sky.png" {
			found = true
			if e.Credit.License != "pixellab-commercial-1.2" || e.Credit.Author != "pixel-gen" {
				t.Fatalf("art/sky.png credit = %+v", e.Credit)
			}
		}
	}
	if !found {
		t.Fatal("art/sky.png missing from collected credits")
	}
}
