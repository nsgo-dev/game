package editor

import (
	"math"
	"slices"
	"strings"
	"testing"
)

func TestFormatThousandsEdgeCases(t *testing.T) {
	if got := formatThousands(0); got != "0" {
		t.Fatalf("formatThousands(0) = %q, want 0", got)
	}
	if got := formatThousands(1284); got != "1,284" {
		t.Fatalf("formatThousands(1284) = %q, want 1,284", got)
	}
	if got := formatThousands(-1284); got != "-1,284" {
		t.Fatalf("formatThousands(-1284) = %q, want -1,284", got)
	}
	gotMin := formatThousands(math.MinInt)
	if !strings.HasPrefix(gotMin, "-") || !strings.Contains(gotMin, ",") {
		t.Fatalf("formatThousands(math.MinInt) = %q, want negative comma-separated string", gotMin)
	}
}

func TestModelAssets3DCachesPerProjectAndRev(t *testing.T) {
	ed, err := New(DefaultSceneDoc(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ed.ProjectPath = t.TempDir() + "/game.yaml"

	a1 := ed.modelAssets3D()
	a2 := ed.modelAssets3D()
	if a1.files == nil || a1.files != a2.files {
		t.Fatalf("modelAssets3D did not reuse cached *scenemodel.Assets across calls: %p vs %p", a1.files, a2.files)
	}

	ed.assetsRev++
	a3 := ed.modelAssets3D()
	if a3.files == nil || a3.files == a1.files {
		t.Fatalf("modelAssets3D did not refresh *scenemodel.Assets after assetsRev bump: %p vs %p", a1.files, a3.files)
	}
}

func TestModelThumbsNotifyReadyDeterministicOrder(t *testing.T) {
	var th modelThumbs
	var order []int
	unsubs := make([]func(), 0, 5)
	for i := 1; i <= 5; i++ {
		id := i
		unsubs = append(unsubs, th.subscribe(func() {
			order = append(order, id)
		}))
	}
	for range 10 {
		order = order[:0]
		th.notifyReady()
		if !slices.Equal(order, []int{1, 2, 3, 4, 5}) {
			t.Fatalf("notifyReady order = %v, want [1 2 3 4 5]", order)
		}
	}
	for _, u := range unsubs {
		u()
	}
}
