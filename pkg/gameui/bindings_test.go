package gameui

import "testing"

func TestBindingsTypedAndFunc(t *testing.T) {
	b := NewBindings()
	b.Set("player.score", 42)
	if n, e := b.Int("player.score"); e != nil || n != 42 {
		t.Fatal(n, e)
	}
	b.Func("hud.visible", func() any { return true })
	if ok, e := b.Bool("hud.visible"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := b.String("player.score"); e == nil {
		t.Fatal("type mismatch accepted")
	}
}
