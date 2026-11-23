package storetest_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/accounts"
	"github.com/nsgo-dev/game/packages/game/accounts/storetest"
)

// A store implementation's tests call Run from a test function with a function that opens an empty store, and Run checks it against the Store contract in subtests. Run needs the *testing.T of a running test, so this example is compiled but not run.
func ExampleRun() {
	testMemoryStore := func(t *testing.T) {
		storetest.Run(t, func(t *testing.T) accounts.Store { return accounts.NewMemoryStore() })
	}
	_ = testMemoryStore
}
