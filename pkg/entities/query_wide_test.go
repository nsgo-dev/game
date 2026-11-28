package entities

import "testing"

func TestEach4Intersection(t *testing.T) {
	w := NewWorld()
	e := w.Spawn()
	Set(w, e, 1)
	Set(w, e, "x")
	Set(w, e, true)
	Set(w, e, float64(2))
	n := 0
	Each4[int, string, bool, float64](w, func(Entity, *int, *string, *bool, *float64) bool { n++; return true })
	if n != 1 {
		t.Fatal(n)
	}
}
