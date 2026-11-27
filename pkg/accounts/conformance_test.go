package accounts_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/accounts"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[accounts.Config]{
		Decode: accounts.ParseConfig,
		Encode: accounts.Config.Marshal,
		Files:  []string{"testdata/accounts.yaml"},
		Doc:    "accounts",
	})
}
