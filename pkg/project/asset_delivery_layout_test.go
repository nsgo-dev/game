package project_test

import (
	"github.com/nsgo-dev/game/pkg/project"
	"testing"
)

func TestAssetDeliveryPlacesGroupsDeterministically(t *testing.T) {
	d := &project.AssetDelivery{Main: []string{"boot"}, Subpackages: map[string][]string{"chapter": {"level"}}, Remote: []string{"music"}}
	got, err := d.PlaceGroups([]string{"music", "level", "boot", "ui"})
	if err != nil {
		t.Fatal(err)
	}
	want := []project.AssetPlacement{{Group: "boot", Package: "main"}, {Group: "level", Package: "subpackage/chapter"}, {Group: "music", Package: "remote"}, {Group: "ui", Package: "main"}}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("placements=%+v want %+v", got, want)
		}
	}
}
