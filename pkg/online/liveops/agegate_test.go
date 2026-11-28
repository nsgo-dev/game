package liveops_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAgeGateStoresBandsAndFailsClosed(t *testing.T) {
	var first, second *liveops.AgeGate
	var storage core.Storage
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		storage = cx.Storage()
		first = liveops.NewAgeGate(storage, "privacy.age")
		if first.Band() != liveops.AgeUnknown || first.ChildMode() {
			t.Fatalf("initial gate = %q child=%v", first.Band(), first.ChildMode())
		}
		if err := first.SetBand(liveops.Age13To15); err != nil {
			t.Fatal(err)
		}
		return core.View{}
	})
	second = liveops.NewAgeGate(storage, "privacy.age")
	if second.Band() != liveops.Age13To15 || !second.ChildMode() {
		t.Fatalf("reloaded gate = %q child=%v", second.Band(), second.ChildMode())
	}
	if err := second.SetBand(liveops.AgeAdult); err != nil {
		t.Fatal(err)
	}
	if second.ChildMode() {
		t.Fatal("adult gate is in child mode")
	}
	if err := second.SetBand(liveops.AgeBand("future")); err != liveops.ErrInvalidAgeBand {
		t.Fatalf("invalid band error = %v", err)
	}
	if err := storage.SetItem("privacy.age", `{"version":2,"band":"under13"}`); err != nil {
		t.Fatal(err)
	}
	if got := liveops.NewAgeGate(storage, "privacy.age"); got.Band() != liveops.AgeUnknown {
		t.Fatalf("future record band = %q, want unknown", got.Band())
	}
}
