package debug_test

import (
	"bytes"
	"github.com/nsgo-dev/game/pkg/debug"
	"testing"
)

func TestVisualLogJSONLRoundTripAndFilters(t *testing.T) {
	l := debug.New(8)
	l.Log(2, "enemy", "ai", debug.LevelWarn, "blocked")
	l.Shape(2, "enemy", "path", [][3]float32{{1, 2, 3}})
	if len(l.Entries("enemy", "ai", 2, 2)) != 1 {
		t.Fatal("filter failed")
	}
	var b bytes.Buffer
	if err := l.WriteJSONL(&b); err != nil {
		t.Fatal(err)
	}
	e, s, err := debug.ReadJSONL(&b)
	if err != nil || len(e) != 1 || len(s) != 1 {
		t.Fatalf("round trip entries=%v shapes=%v err=%v", e, s, err)
	}
}
