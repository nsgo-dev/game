package entities_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
)

type parallelPosition struct{ X int }
type parallelVelocity struct{ X int }

func TestParEach2IsStableAcrossWorkerCounts(t *testing.T) {
	w := entities.NewWorld()
	for i := range 100 {
		e := w.Spawn()
		entities.Component[parallelPosition](w).Set(e, parallelPosition{X: i})
		entities.Component[parallelVelocity](w).Set(e, parallelVelocity{X: 1})
	}
	for _, workers := range []int{1, 2, 8} {
		entities.ParEach2(w, entities.ParOptions{Workers: workers, ChunkSize: 7}, func(_ entities.Entity, p *parallelPosition, v *parallelVelocity) {
			p.X += v.X
		})
	}
	entities.Each1(w, func(_ entities.Entity, p *parallelPosition) bool {
		if p.X < 3 {
			t.Fatalf("position = %d, want at least 3", p.X)
		}
		return true
	})
}
