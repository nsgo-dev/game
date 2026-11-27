//go:build js && wasm

package assets_test

import (
	"testing"

	// Link NSGo's web host so `nsgo test -target web` runs these tests in the
	// real browser runtime, fetching both image and byte assets from resources/
	// over HTTP with nothing embedded.
	_ "github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func appResourcesTestOptions(t *testing.T) []nsgotest.Option {
	t.Helper()
	return nil
}
