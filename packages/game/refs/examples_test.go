package refs_test

import (
	"os"
	"testing"

	"github.com/nsgo-dev/game/packages/game/refs"
)

// TestExamplesIndexed keeps the sample games and the starter template indexed: every scene and resource file in them has a uid and every project index is up to date, so game index has nothing left to do. When it fails, run go run ./cmd/game index on the directory it names, from packages/game.
func TestExamplesIndexed(t *testing.T) {
	for _, dir := range []string{"../../../examples", "../../../template/template"} {
		pl, err := refs.PlanIndex(os.DirFS(dir), refs.IndexOptions{})
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if !pl.Empty() {
			var files []string
			for _, w := range pl.Writes {
				files = append(files, w.Path)
			}
			t.Errorf("%s is not indexed: game index would write %v and move %v; run go run ./cmd/game index %s", dir, files, pl.Moves, dir)
		}
	}
}
