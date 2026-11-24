package token_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/accounts/token"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*token.KeySet]{
		Decode: token.ParseKeySet,
		Encode: (*token.KeySet).Marshal,
		Files:  []string{"testdata/keys.yaml"},
	})
}
