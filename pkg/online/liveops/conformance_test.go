package liveops_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/liveops"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*liveops.RemindersConfig]{
		Decode: liveops.ParseReminders,
		Encode: liveops.EncodeReminders,
		Files:  []string{"testdata/reminders.yaml"},
		Doc:    "online/liveops#reminders",
	})
}
