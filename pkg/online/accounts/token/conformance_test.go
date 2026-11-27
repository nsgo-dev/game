package token_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/accounts/token"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*token.KeySet]{
		Decode: token.ParseKeySet,
		Encode: (*token.KeySet).Marshal,
		Files:  []string{"testdata/keys.yaml"},
		Doc:    "online/accounts/token",
	})
}
