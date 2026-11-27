package hotreload_test

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/hotreload"
)

func TestMaterialNegativeZeroRoundTrip(t *testing.T) {
	negZero := float32(math.Copysign(0, -1))

	t.Run("parse_and_resave_yaml", func(t *testing.T) {
		raw := []byte("version: 1\nname: matte\nshader: shaders/lit.wgsl\nscalars:\n  rough: -0.0\nvec2:\n  uv: [-0.0, 1]\nvec3:\n  dir: [0, -0.0, 1]\nvec4:\n  tint: [1, 0, -0.0, 1]\n")
		m, err := hotreload.ParseMaterial(raw)
		if err != nil {
			t.Fatalf("ParseMaterial: %v", err)
		}
		if math.Signbit(float64(m.Scalars["rough"])) {
			t.Fatal("parsed Scalars[rough] kept negative zero")
		}
		if math.Signbit(float64(m.Vec2["uv"][0])) {
			t.Fatal("parsed Vec2[uv][0] kept negative zero")
		}
		if math.Signbit(float64(m.Vec3["dir"][1])) {
			t.Fatal("parsed Vec3[dir][1] kept negative zero")
		}
		if math.Signbit(float64(m.Vec4["tint"][2])) {
			t.Fatal("parsed Vec4[tint][2] kept negative zero")
		}

		first, err := hotreload.EncodeMaterial(m)
		if err != nil {
			t.Fatalf("EncodeMaterial 1: %v", err)
		}
		if strings.Contains(string(first), "-0") {
			t.Fatalf("EncodeMaterial wrote negative zero:\n%s", first)
		}
		again, err := hotreload.ParseMaterial(first)
		if err != nil {
			t.Fatalf("ParseMaterial 2: %v\n%s", err, first)
		}
		second, err := hotreload.EncodeMaterial(again)
		if err != nil {
			t.Fatalf("EncodeMaterial 2: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("material round trip changed bytes:\n--- first\n%s\n--- second\n%s", first, second)
		}
	})

	t.Run("encode_in_memory_negzero", func(t *testing.T) {
		orig := &hotreload.MaterialDef{
			Version: hotreload.MaterialVersion,
			Shader:  "shaders/lit.wgsl",
			Scalars: map[string]float32{"rough": negZero},
			Vec2:    map[string][2]float32{"uv": {negZero, 1}},
			Vec3:    map[string][3]float32{"dir": {0, negZero, 1}},
			Vec4:    map[string][4]float32{"tint": {1, 0, negZero, 1}},
		}
		first, err := hotreload.EncodeMaterial(orig)
		if err != nil {
			t.Fatalf("EncodeMaterial 1: %v", err)
		}
		if strings.Contains(string(first), "-0") {
			t.Fatalf("EncodeMaterial wrote negative zero:\n%s", first)
		}
		if !math.Signbit(float64(orig.Scalars["rough"])) || !math.Signbit(float64(orig.Vec2["uv"][0])) {
			t.Fatal("EncodeMaterial mutated caller MaterialDef")
		}
		parsed, err := hotreload.ParseMaterial(first)
		if err != nil {
			t.Fatalf("ParseMaterial: %v", err)
		}
		second, err := hotreload.EncodeMaterial(parsed)
		if err != nil {
			t.Fatalf("EncodeMaterial 2: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("material round trip changed bytes:\n--- first\n%s\n--- second\n%s", first, second)
		}
	})
}

func FuzzParseMaterialRoundTrip(f *testing.F) {
	if golden, err := os.ReadFile("testdata/crate.material.yaml"); err == nil {
		f.Add(golden)
	}
	f.Add([]byte("version: 1\nshader: shaders/lit.wgsl\n"))
	f.Add([]byte("version: 1\nshader: shaders/lit.wgsl\nscalars:\n  rough: -0.0\nvec2:\n  uv: [-0.0, 1]\nvec3:\n  dir: [0, -0.0, 1]\nvec4:\n  tint: [1, 0, -0.0, 1]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := hotreload.ParseMaterial(data)
		if err != nil {
			return
		}
		first, err := hotreload.EncodeMaterial(m)
		if err != nil {
			t.Fatalf("EncodeMaterial of parsed material failed: %v", err)
		}
		again, err := hotreload.ParseMaterial(first)
		if err != nil {
			t.Fatalf("ParseMaterial of encoded output failed: %v\n%s", err, first)
		}
		second, err := hotreload.EncodeMaterial(again)
		if err != nil {
			t.Fatalf("second EncodeMaterial failed: %v", err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("material encoding is not stable:\n%s\n---\n%s", first, second)
		}
	})
}
