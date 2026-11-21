package assets

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestGroupResetForgetsPreviousSpecs(t *testing.T) {
	var mgr *Manager
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mgr = NewManager(cx)
		return core.View{}
	})
	g := mgr.Group("dlc").AddBytes("old", "asset:old.txt")
	if got := g.Total(); got != 1 {
		t.Fatalf("Total before Reset = %d, want 1", got)
	}
	g.Reset()
	if got := g.Total(); got != 0 {
		t.Fatalf("Total after Reset = %d, want 0", got)
	}
	g.AddBytes("new", "asset:new.txt")
	if got := g.Total(); got != 1 {
		t.Fatalf("Total after re-adding = %d, want 1", got)
	}
}
