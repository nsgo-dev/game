package authtoken_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/online/accounts/authtoken"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*authtoken.KeySet]{
		Decode: authtoken.ParseKeySet,
		Encode: (*authtoken.KeySet).Marshal,
		Files:  []string{"testdata/keys.yaml"},
		Doc:    "online/accounts/authtoken",
	})
}
