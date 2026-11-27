//go:build ios || android || js

package editor

import (
	"context"

	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

func runFirstRunPlaytestCommand(_ context.Context, _ string) (*playtest.Report, error) {
	return nil, errFirstRunPlaytestUnsupported
}
