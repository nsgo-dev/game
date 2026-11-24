package undo_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/undo"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[undo.Log[*push]]{
		Decode: undo.DecodeLog[*push],
		Encode: undo.EncodeLog[*push],
		Files:  []string{"testdata/log.yaml"},
		Doc:    "undo",
	})
}
