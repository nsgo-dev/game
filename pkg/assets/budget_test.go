package assets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestManagerBudgetRejects(t *testing.T) {
	done := make(chan error, 1)
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		m := assets.NewManager(cx, assets.WithReadAsset(func(_ context.Context, _ string) ([]byte, error) { return []byte("12345"), nil }))
		m.SetBudget(4, 4)
		m.Group("level").AddBytes("data", "asset:data")
		m.LoadGroup("level", func(err error) { done <- err })
		return nil
	})
	err := <-done
	var budget *assets.MemoryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("error = %v, want MemoryBudgetError", err)
	}
}
