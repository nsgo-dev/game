//go:build !js

package assets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func appResourcesTestOptions(t *testing.T) []nsgotest.Option {
	t.Helper()
	// When nsgo test runs from the module root (go tool nsgo test ./pkg/assets),
	// NSGO_TEST_RESOURCES points to <repoRoot>/resources, whereas this package's
	// test resources live in ./resources beside the test package.
	resDir, err := filepath.Abs("resources")
	if err != nil {
		t.Fatalf("resolve pkg/assets/resources: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resDir, "sprites", "hero.json")); err != nil {
		t.Fatalf("missing test resource fixture in %s: %v", resDir, err)
	}
	t.Setenv("NSGO_TEST_RESOURCES", resDir)
	return []nsgotest.Option{
		func(a *nsgotest.App) {
			a.ImageSizes = map[string]core.Size{
				"asset:sprites/hero.png": {Width: 32, Height: 16},
			}
		},
	}
}
