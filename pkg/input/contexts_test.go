package input

import "testing"

func TestVectorNormalizesAndContextsToggle(t *testing.T) {
	var a Actions
	a.SetAxis("x", 1)
	a.SetAxis("y", 1)
	a.BindVector("move", VectorBinding{X: "x", Y: "y"})
	a.Step()
	v := a.Vector("move")
	if v.X < 0.70 || v.X > 0.71 {
		t.Fatalf("vector=%+v", v)
	}
	a.PushContext(InputContext{Name: "menu", Priority: 10})
	if len(a.Contexts()) != 1 {
		t.Fatal()
	}
	a.SetContextEnabled("menu", false)
	if len(a.Contexts()) != 0 {
		t.Fatal()
	}
}
