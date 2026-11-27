package datatable_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/datatable"
)

func TestYAMLNegativeZeroRoundTrip(t *testing.T) {
	type row struct {
		ID   string  `table:"id"`
		Rate float64 `table:"rate"`
		Bias float32 `table:"bias"`
	}

	const raw = "version: 1\nrows:\n  - id: a\n    rate: -0.0\n    bias: -0.0\n"
	parsed, err := datatable.LoadYAML[row]("test.yaml", []byte(raw))
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if math.Signbit(parsed[0].Rate) || math.Signbit(float64(parsed[0].Bias)) {
		t.Fatalf("LoadYAML kept negative zero: %+v", parsed[0])
	}
	first, err := datatable.MarshalYAML(parsed)
	if err != nil {
		t.Fatalf("MarshalYAML 1: %v", err)
	}
	if strings.Contains(string(first), "-0") {
		t.Fatalf("MarshalYAML wrote negative zero:\n%s", first)
	}
	again, err := datatable.LoadYAML[row]("test.yaml", first)
	if err != nil {
		t.Fatalf("LoadYAML 2: %v", err)
	}
	second, err := datatable.MarshalYAML(again)
	if err != nil {
		t.Fatalf("MarshalYAML 2: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed bytes:\n--- first\n%s\n--- second\n%s", first, second)
	}

	nz := math.Copysign(0, -1)
	inMem, err := datatable.MarshalYAML([]row{{ID: "b", Rate: nz, Bias: float32(nz)}})
	if err != nil {
		t.Fatalf("MarshalYAML in-memory: %v", err)
	}
	if strings.Contains(string(inMem), "-0") {
		t.Fatalf("MarshalYAML in-memory wrote negative zero:\n%s", inMem)
	}
	if v := datatable.FloatValue(nz); v.Text != "0" {
		t.Fatalf("FloatValue(-0.0).Text = %q, want %q", v.Text, "0")
	}
}
