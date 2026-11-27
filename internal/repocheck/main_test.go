package repocheck_test

import (
	"os"
	"testing"

	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain runs the tests from the repository root, so every path they read and every path in their messages is relative to it.
func TestMain(m *testing.M) {
	if err := os.Chdir("../.."); err != nil {
		panic(err)
	}
	nsgotest.Main(m)
}
