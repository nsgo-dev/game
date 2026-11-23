package cloudsave_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/cloudsave"
	"github.com/nsgo-dev/game/packages/game/cloudsave/storetest"
)

func TestMemoryStoreContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) cloudsave.Store { return cloudsave.NewMemoryStore() })
}
