package accounts_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/accounts"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[accounts.Config]{
		Decode: accounts.ParseConfig,
		Encode: accounts.Config.Marshal,
		Files:  []string{"testdata/accounts.yaml"},
	})
}
