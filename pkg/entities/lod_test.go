package entities_test

import (
	"github.com/nsgo-dev/game/pkg/entities"
	"testing"
)

func TestResolveLODHysteresisAndNearestViewer(t *testing.T) {
	bands := []entities.LODBand{{MaxDistance: 10, Level: 0}, {MaxDistance: 20, Level: 1}, {MaxDistance: 40, Level: 2}}
	if got := entities.ResolveLOD(0, 19, bands, .1); got != 0 {
		t.Fatal(got)
	}
	if got := entities.ResolveLOD(0, 22, bands, .1); got != 1 {
		t.Fatal(got)
	}
	if d := entities.NearestViewerDistance(10, 0, []entities.Viewer{{X: 0}, {X: 9}}); d != 1 {
		t.Fatal(d)
	}
}
