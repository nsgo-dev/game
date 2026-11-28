//go:build release

package gamedebug_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

type releaseCategory struct{ t *testing.T }

func (c releaseCategory) Name() string { c.t.Error("Category.Name called in release build"); return "" }
func (c releaseCategory) Snapshot(string) []gamedebug.Field {
	c.t.Error("Category.Snapshot called in release build")
	return nil
}

func TestReleaseVLogIsNoOp(t *testing.T) {
	l := gamedebug.NewVLog(8)
	l.Log(1, "enemy", "ai", gamedebug.VLogWarn, "blocked")
	l.Snapshot(1, "enemy", releaseCategory{t})
	l.Shape(1, "enemy", "path", [][3]float32{{1, 2, 3}})
	if l.Entries("", "", 0, 0) != nil || l.Shapes("", 0, 0) != nil {
		t.Fatal("release VLog returned records")
	}
	var b bytes.Buffer
	if err := l.WriteJSONL(&b); err != nil || b.Len() != 0 {
		t.Fatalf("release WriteJSONL wrote %q, err %v; want nothing", b.String(), err)
	}
	if _, _, err := gamedebug.ReadVLog(strings.NewReader("")); err == nil {
		t.Fatal("release ReadVLog succeeded, want an error")
	}
}
