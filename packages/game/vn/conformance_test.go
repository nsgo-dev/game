package vn_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/vn"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*vn.Cast]{
		Decode: vn.ParseCast,
		Encode: vn.EncodeCast,
		Files:  []string{"testdata/cast.yaml"},
		Doc:    "vn",
	})
}
