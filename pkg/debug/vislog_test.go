package debug

import (
	"bytes"
	"testing"
)

func TestVLogJSONLRoundTripAndScrub(t *testing.T) {
	v := NewVLog(4)
	v.Logf(1, "hero", "combat", "info", "hp=%d", 3)
	v.Shape(2, "hero", "nav", "path", map[string]float64{"x": 4})
	var b bytes.Buffer
	if err := v.WriteJSONL(&b); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJSONL(&b)
	if err != nil || len(got) != 2 {
		t.Fatalf("entries=%+v err=%v", got, err)
	}
	if len(v.Entries("hero", 2, 2)) != 1 {
		t.Fatal("scrub filter")
	}
}
func TestVLogRingBuffer(t *testing.T) {
	v := NewVLog(2)
	for i := int64(0); i < 3; i++ {
		v.Logf(i, "e", "c", "", "x")
	}
	e := v.Entries("", -1, -1)
	if len(e) != 2 || e[0].Step != 1 {
		t.Fatalf("entries=%+v", e)
	}
}
