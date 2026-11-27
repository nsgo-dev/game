package sceneformat_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestNegativeZeroRoundTrip(t *testing.T) {
	const raw = `version: 4
layers:
  - name: world
    parallaxX: -0.0
    parallaxY: -0.0
    objects:
      - id: hero
        x: -0.0
        alpha: -0.0
        properties:
          speed: -0.0
`
	doc, err := sceneformat.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if math.Signbit(float64(*doc.Layers[0].ParallaxX)) {
		t.Fatal("parsed ParallaxX kept negative zero")
	}
	if math.Signbit(float64(*doc.Layers[0].ParallaxY)) {
		t.Fatal("parsed ParallaxY kept negative zero")
	}
	if math.Signbit(float64(*doc.Layers[0].Objects[0].Alpha)) {
		t.Fatal("parsed Alpha kept negative zero")
	}
	if v, ok := doc.Layers[0].Objects[0].Properties["speed"].(float64); !ok || math.Signbit(v) {
		t.Fatalf("parsed property speed = %#v", doc.Layers[0].Objects[0].Properties["speed"])
	}

	first, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode 1: %v", err)
	}
	if strings.Contains(string(first), "-0") {
		t.Fatalf("Encode wrote negative zero:\n%s", first)
	}
	again, err := sceneformat.Parse(first)
	if err != nil {
		t.Fatalf("Parse 2: %v\n%s", err, first)
	}
	second, err := sceneformat.Encode(again)
	if err != nil {
		t.Fatalf("Encode 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed bytes:\n--- first\n%s\n--- second\n%s", first, second)
	}
}
