package yamlzero_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/internal/yamlzero"
)

func negZero32() float32 { return float32(math.Copysign(0, -1)) }
func negZero64() float64 { return math.Copysign(0, -1) }

type namedFloat float32

type unexportedEmbed struct {
	Embedded float32
}

type sample struct {
	unexportedEmbed
	F32     float32
	F64     float64
	Named   namedFloat
	Ptr32   *float32
	Ptr64   *float64
	Arr     [2]float32
	Slice   []float64
	MapF32  map[string]float32
	MapArr  map[string][2]float32
	MapAny  map[string]any
	SliceAn []any
	hidden  float64
}

func TestNormalizeAndClean(t *testing.T) {
	p32 := negZero32()
	p64 := negZero64()
	orig := &sample{
		unexportedEmbed: unexportedEmbed{Embedded: negZero32()},
		F32:             negZero32(),
		F64:             negZero64(),
		Named:           namedFloat(negZero32()),
		Ptr32:           &p32,
		Ptr64:           &p64,
		Arr:             [2]float32{negZero32(), 1},
		Slice:           []float64{negZero64(), 2},
		MapF32:          map[string]float32{"a": negZero32(), "b": 3},
		MapArr:          map[string][2]float32{"uv": {negZero32(), 1}},
		MapAny: map[string]any{
			"f64": negZero64(),
			"f32": negZero32(),
			"arr": [2]float32{negZero32(), 4},
			"sub": map[string]any{"x": negZero64()},
			"lst": []any{negZero64(), "keep"},
		},
		SliceAn: []any{negZero32(), negZero64()},
		hidden:  negZero64(),
	}

	if !yamlzero.Has(orig) {
		t.Fatal("Has(orig) = false, want true")
	}

	// Clean returns a normalized copy without mutating orig.
	cleaned := yamlzero.Clean(orig)
	if cleaned == orig {
		t.Fatal("Clean returned the same pointer for a value with negative zero")
	}
	if !math.Signbit(float64(orig.F32)) || !math.Signbit(float64(*orig.Ptr32)) || !math.Signbit(float64(orig.MapArr["uv"][0])) {
		t.Fatal("Clean mutated the original value")
	}
	if yamlzero.Has(cleaned) {
		t.Fatal("Has(cleaned) = true, want false")
	}
	if !math.Signbit(cleaned.hidden) {
		t.Fatal("Clean dropped unexported field value")
	}
	checkNoNegZero(t, "cleaned", cleaned)

	// Normalize mutates in place.
	yamlzero.Normalize(orig)
	if yamlzero.Has(orig) {
		t.Fatal("Has(orig) after Normalize = true, want false")
	}
	checkNoNegZero(t, "normalized", orig)

	// Fast path: Clean on a value with no negative zero returns the exact same pointer.
	same := yamlzero.Clean(orig)
	if same != orig {
		t.Fatal("Clean allocated a copy for a value without negative zero")
	}
}

func checkNoNegZero(t *testing.T, label string, s *sample) {
	t.Helper()
	if math.Signbit(float64(s.Embedded)) {
		t.Errorf("%s.Embedded is -0", label)
	}
	if math.Signbit(float64(s.F32)) {
		t.Errorf("%s.F32 is -0", label)
	}
	if math.Signbit(s.F64) {
		t.Errorf("%s.F64 is -0", label)
	}
	if math.Signbit(float64(s.Named)) {
		t.Errorf("%s.Named is -0", label)
	}
	if math.Signbit(float64(*s.Ptr32)) {
		t.Errorf("%s.Ptr32 is -0", label)
	}
	if math.Signbit(*s.Ptr64) {
		t.Errorf("%s.Ptr64 is -0", label)
	}
	if math.Signbit(float64(s.Arr[0])) || s.Arr[1] != 1 {
		t.Errorf("%s.Arr = %v", label, s.Arr)
	}
	if math.Signbit(s.Slice[0]) || s.Slice[1] != 2 {
		t.Errorf("%s.Slice = %v", label, s.Slice)
	}
	if math.Signbit(float64(s.MapF32["a"])) || s.MapF32["b"] != 3 {
		t.Errorf("%s.MapF32 = %v", label, s.MapF32)
	}
	if math.Signbit(float64(s.MapArr["uv"][0])) || s.MapArr["uv"][1] != 1 {
		t.Errorf("%s.MapArr = %v", label, s.MapArr)
	}
	if f, ok := s.MapAny["f64"].(float64); !ok || math.Signbit(f) {
		t.Errorf("%s.MapAny[f64] = %#v", label, s.MapAny["f64"])
	}
	if f, ok := s.MapAny["f32"].(float32); !ok || math.Signbit(float64(f)) {
		t.Errorf("%s.MapAny[f32] = %#v", label, s.MapAny["f32"])
	}
	if arr, ok := s.MapAny["arr"].([2]float32); !ok || math.Signbit(float64(arr[0])) || arr[1] != 4 {
		t.Errorf("%s.MapAny[arr] = %#v", label, s.MapAny["arr"])
	}
	sub := s.MapAny["sub"].(map[string]any)
	if f, ok := sub["x"].(float64); !ok || math.Signbit(f) {
		t.Errorf("%s.MapAny[sub][x] = %#v", label, sub["x"])
	}
	lst := s.MapAny["lst"].([]any)
	if f, ok := lst[0].(float64); !ok || math.Signbit(f) || lst[1] != "keep" {
		t.Errorf("%s.MapAny[lst] = %#v", label, lst)
	}
	if f, ok := s.SliceAn[0].(float32); !ok || math.Signbit(float64(f)) {
		t.Errorf("%s.SliceAn[0] = %#v", label, s.SliceAn[0])
	}
	if f, ok := s.SliceAn[1].(float64); !ok || math.Signbit(f) {
		t.Errorf("%s.SliceAn[1] = %#v", label, s.SliceAn[1])
	}
}

func TestNilAndInterfaceValues(t *testing.T) {
	if yamlzero.Has(nil) {
		t.Fatal("Has(nil) = true")
	}
	yamlzero.Normalize(nil)
	var nilPtr *sample
	if yamlzero.Has(nilPtr) {
		t.Fatal("Has(nilPtr) = true")
	}
	yamlzero.Normalize(nilPtr)
	if got := yamlzero.Clean(nilPtr); got != nil {
		t.Fatalf("Clean(nilPtr) = %v, want nil", got)
	}
	var iface any = &sample{F32: negZero32()}
	cleaned := yamlzero.Clean(iface)
	if yamlzero.Has(cleaned) {
		t.Fatal("Clean(interface) left negative zero")
	}
	if !yamlzero.Has(iface) {
		t.Fatal("Clean(interface) mutated original")
	}
}

func TestZeroAllocsWhenClean(t *testing.T) {
	v := float32(0.5)
	s := &sample{
		F32:   1,
		F64:   2,
		Ptr32: &v,
		Arr:   [2]float32{0, 1},
		Slice: []float64{0, 2},
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if yamlzero.Has(s) {
			t.Fatal("unexpected true")
		}
	}); allocs != 0 {
		t.Fatalf("Has allocs = %v, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		yamlzero.Normalize(s)
	}); allocs != 0 {
		t.Fatalf("Normalize allocs = %v, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		_ = yamlzero.Clean(s)
	}); allocs != 0 {
		t.Fatalf("Clean allocs = %v, want 0", allocs)
	}
}
