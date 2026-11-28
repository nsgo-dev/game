package editor

import (
	"testing"
)

func TestMapGeneratorParametersAliasesEnumsAndBounds(t *testing.T) {
	schema, err := ParseGeneratorParameterSchema([]byte(`{"type":"object","properties":{"aspect_ratio":{"type":"string","enum":["1:1","16:9"]},"num_images":{"type":"integer","minimum":1,"maximum":8},"image_size":{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MapGeneratorParameters(schema, map[string]any{"aspectRatio": "16:9", "count": 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["aspect_ratio"] != "16:9" || got["num_images"] != 4 {
		t.Fatalf("mapped = %#v", got)
	}
	nested, err := MapGeneratorParameters(schema, map[string]any{"width": 512, "height": 256}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := nested["image_size"].(map[string]any); !ok || got["width"] != 512 || got["height"] != 256 {
		t.Fatalf("nested size = %#v", nested)
	}
	if _, err := MapGeneratorParameters(schema, map[string]any{"count": 9}, nil); err == nil {
		t.Fatal("out-of-range count accepted")
	}
	if _, err := MapGeneratorParameters(schema, map[string]any{"aspectRatio": "4:3"}, nil); err == nil {
		t.Fatal("unknown aspect ratio accepted")
	}
}

func TestMapGeneratorParametersOverrideAndLegacyKeys(t *testing.T) {
	schema, err := ParseGeneratorParameterSchema([]byte(`{"type":"object","properties":{"description":{"type":"string"},"n":{"type":"integer"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MapGeneratorParameters(schema, map[string]any{"prompt": "a knight", "count": 2}, map[string]string{"count": "n"})
	if err != nil {
		t.Fatal(err)
	}
	if got["description"] != "a knight" || got["n"] != 2 {
		t.Fatalf("mapped = %#v", got)
	}
	legacy, err := MapGeneratorParameters(GeneratorParameterSchema{}, map[string]any{"target": "hero.png", "prompt": "hero", "count": 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if legacy["target"] != "hero.png" || legacy["prompt"] != "hero" || legacy["count"] != 1 {
		t.Fatalf("legacy = %#v", legacy)
	}
}

func TestCanonicalParameterMapRejectsUnknown(t *testing.T) {
	if _, err := CanonicalParameterMap(map[string]any{"aspectRatio": "1:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalParameterMap(map[string]any{"providerOnly": true}); err == nil {
		t.Fatal("unknown canonical parameter accepted")
	}
}
