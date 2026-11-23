package devimage_test

import (
	"image"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/devimage"
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
