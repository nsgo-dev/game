package input_test

import (
	"github.com/nsgo-dev/game/pkg/input"
	"testing"
)

func TestContextsPriorityConsumptionAndVectorNormalization(t *testing.T) {
	c := input.NewContexts()
	c.Add(input.Context{Name: "game", Priority: 1, Bindings: map[string][]input.Input{"confirm": {{Kind: input.InputKey, Key: "space"}}}})
	c.Add(input.Context{Name: "menu", Priority: 2, Bindings: map[string][]input.Input{"confirm": {{Kind: input.InputKey, Key: "enter"}}}})
	c.Push("game")
	c.Push("menu")
	if got := c.Binding("confirm"); len(got) != 1 || got[0].Key != "enter" {
		t.Fatalf("binding=%v", got)
	}
	c.Pop("menu")
	if got := c.Binding("confirm"); len(got) != 1 || got[0].Key != "space" {
		t.Fatalf("fallback=%v", got)
	}
	v := input.NormalizeVector(input.Vector2{X: 3, Y: 4})
	if v.X != .6 || v.Y != .8 {
		t.Fatalf("vector=%v", v)
	}
}
