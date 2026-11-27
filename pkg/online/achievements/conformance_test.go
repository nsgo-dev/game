package achievements_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/achievements"
)

func TestFormatConformance(t *testing.T) {
	t.Run("definitions", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*achievements.Set]{
			Decode: achievements.Parse,
			Encode: achievements.Encode,
			Files:  []string{"testdata/achievements.yaml"},
			Doc:    "online/achievements",
		})
	})
	t.Run("progress", func(t *testing.T) {
		formattest.Run(t, formattest.Format[*achievements.Progress]{
			Decode: achievements.ParseProgress,
			Encode: achievements.EncodeProgress,
			Files:  []string{"testdata/progress.yaml"},
			Doc:    "online/achievements#progress",
		})
	})
}
