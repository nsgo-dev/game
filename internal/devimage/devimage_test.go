package devimage_test

import (
	"image"
	"testing"

	"github.com/nsgo-dev/game/internal/devimage"
	"github.com/nsgo-dev/nsgo/core"
)

func TestWithDataKeepsNameAndSizeAndDropsOtherSources(t *testing.T) {
	src := core.ImageSource{
		URI:      "asset:hero@2x.png",
		Size:     core.Size{Width: 64, Height: 32},
		Scales:   3,
		Variants: map[float32]string{2: "data:x"},
		Pixels:   image.NewRGBA(image.Rect(0, 0, 1, 1)),
		Version:  4,
	}
	data := []byte("png bytes")
	got := devimage.WithData(src, data)
	if got.URI != src.URI || got.Size != src.Size {
		t.Fatalf("URI/Size = %q/%v, want %q/%v", got.URI, got.Size, src.URI, src.Size)
	}
	if &got.Data[0] != &data[0] {
		t.Fatal("Data is not the bytes given")
	}
	if got.Scales != 0 || got.Variants != nil || got.Pixels != nil || got.Offscreen != nil || got.Version != 0 {
		t.Fatalf("other sources kept: %+v", got)
	}
	if got.Density() != 2 {
		t.Fatalf("Density = %v, want 2", got.Density())
	}
	if got.Key() == src.Key() || got.Key() == (core.ImageSource{URI: src.URI}).Key() {
		t.Fatalf("Key %q does not name the new bytes", got.Key())
	}
}

func TestShownPicksTheFileTheScreenLoads(t *testing.T) {
	for _, tc := range []struct {
		name  string
		src   core.ImageSource
		ratio float32
		want  string
	}{
		{"single file", core.ImageSource{URI: "asset:hero.png"}, 2, "asset:hero.png"},
		{"1x screen", core.ImageSource{URI: "asset:ui/hero.png", Scales: core.ScalesOf(1, 2)}, 1, "asset:ui/hero.png"},
		{"2x screen", core.ImageSource{URI: "asset:ui/hero.png", Scales: core.ScalesOf(1, 2)}, 2, "asset:ui/hero@2x.png"},
		{"largest past the ratio", core.ImageSource{URI: "asset:hero.png", Scales: core.ScalesOf(1, 1.5)}, 3, "asset:hero@1.5x.png"},
		{"variants", core.ImageSource{URI: "asset:hero.png", Scales: core.ScalesOf(1, 2), Variants: map[float32]string{2: "data:2x"}}, 2, "data:2x"},
		{"not an asset", core.ImageSource{URI: "https://example.com/hero.png", Scales: core.ScalesOf(1, 2)}, 2, "https://example.com/hero.png"},
	} {
		if got := devimage.Shown(tc.src, tc.ratio); got != tc.want {
			t.Errorf("%s: Shown = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestReloadedReplacesOnlyTheDensityShown(t *testing.T) {
	same := func(a, b string) bool { return a == b }
	src := core.ImageSource{URI: "asset:hero.png", Scales: core.ScalesOf(1, 2), Size: core.Size{Width: 16, Height: 16}}
	if _, ok := devimage.Reloaded(src, "asset:hero.png", []byte("1x"), 2, same); ok {
		t.Fatal("the 1x file replaced the image a 2x screen shows")
	}
	got, ok := devimage.Reloaded(src, "asset:hero@2x.png", []byte("2x"), 2, same)
	if !ok || got.URI != "asset:hero@2x.png" || string(got.Data) != "2x" || got.Density() != 2 || got.Size != src.Size || got.Scales != 0 {
		t.Fatalf("2x reload = %+v, %v", got, ok)
	}
	got, ok = devimage.Reloaded(src, "asset:hero.png", []byte("1x"), 1, same)
	if !ok || got.URI != "asset:hero.png" || got.Density() != 1 {
		t.Fatalf("1x reload on a 1x screen = %+v, %v", got, ok)
	}
	if _, ok := devimage.Reloaded(core.ImageSource{Data: []byte("x")}, "", []byte("y"), 1, same); ok {
		t.Fatal("an image without a URI was reloaded")
	}
}
