// Package savetest provides test helpers that verify save.Game registries round-trip every
// registered part through a save.Store without losing or altering state.
package savetest

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
)

// RoundTrip saves g to a temporary save.Store, loads the slot back into a fresh *save.Game,
// and compares every registered part's serialized state before and after the load.
//
// Pass a fresh factory (func() *save.Game) to load into a newly constructed game instance and
// catch parts whose LoadState is a no-op or fails to restore state. When fresh is omitted,
// RoundTrip resets any registered parts that implement save.Resetter on g before loading back
// into g.
func RoundTrip(t testing.TB, g *save.Game, fresh ...func() *save.Game) {
	t.Helper()
	if err := CheckRoundTrip(t.TempDir(), g, fresh...); err != nil {
		t.Fatal(err)
	}
}

// RoundTripInto saves src to a temporary save.Store, loads the slot into dst, and compares every
// registered part's serialized state between src and dst.
func RoundTripInto(t testing.TB, src, dst *save.Game) {
	t.Helper()
	if err := CheckRoundTripInto(t.TempDir(), src, dst); err != nil {
		t.Fatal(err)
	}
}

// CheckRoundTrip performs the same verification as RoundTrip using dir as the temporary store
// directory and returns an error instead of failing a testing.TB.
func CheckRoundTrip(dir string, g *save.Game, fresh ...func() *save.Game) error {
	if g == nil {
		return errors.New("savetest: nil save.Game")
	}
	if len(fresh) > 1 {
		return fmt.Errorf("savetest: expected at most one fresh factory, got %d", len(fresh))
	}
	if len(fresh) == 1 && fresh[0] != nil {
		return CheckRoundTripInto(dir, g, fresh[0]())
	}
	store := save.NewFileStore(dir)
	before, err := g.PartSnapshots()
	if err != nil {
		return fmt.Errorf("savetest: snapshot before save: %w", err)
	}
	if err := g.Save(store, "roundtrip"); err != nil {
		return fmt.Errorf("savetest: save: %w", err)
	}
	g.ResetParts()
	if err := g.Load(store, "roundtrip"); err != nil {
		return fmt.Errorf("savetest: load: %w", err)
	}
	after, err := g.PartSnapshots()
	if err != nil {
		return fmt.Errorf("savetest: snapshot after load: %w", err)
	}
	return compareParts(g.Parts(), before, after)
}

// CheckRoundTripInto saves src into dir, loads the saved slot into dst, verifies both games
// register the same parts and versions, and checks that every part's JSON state matches.
func CheckRoundTripInto(dir string, src, dst *save.Game) error {
	if src == nil {
		return errors.New("savetest: nil source save.Game")
	}
	if dst == nil {
		return errors.New("savetest: nil destination save.Game")
	}
	srcParts := src.Parts()
	dstParts := dst.Parts()
	if !slices.Equal(srcParts, dstParts) {
		return fmt.Errorf("savetest: part registration mismatch: source has %v, destination has %v", srcParts, dstParts)
	}
	for _, name := range srcParts {
		sv, _ := src.PartVersion(name)
		dv, _ := dst.PartVersion(name)
		if sv != dv {
			return fmt.Errorf("savetest: part %q version mismatch: source v%d, destination v%d", name, sv, dv)
		}
	}
	store := save.NewFileStore(dir)
	before, err := src.PartSnapshots()
	if err != nil {
		return fmt.Errorf("savetest: snapshot source: %w", err)
	}
	if err := src.Save(store, "roundtrip"); err != nil {
		return fmt.Errorf("savetest: save: %w", err)
	}
	if err := dst.Load(store, "roundtrip"); err != nil {
		return fmt.Errorf("savetest: load into fresh game: %w", err)
	}
	after, err := dst.PartSnapshots()
	if err != nil {
		return fmt.Errorf("savetest: snapshot destination: %w", err)
	}
	return compareParts(srcParts, before, after)
}

func compareParts(names []string, before, after map[string]json.RawMessage) error {
	for _, name := range names {
		b := before[name]
		a := after[name]
		if !save.EqualJSON(b, a) {
			return fmt.Errorf("savetest: part %q did not round-trip:\n  before: %s\n  after:  %s", name, string(b), string(a))
		}
	}
	return nil
}
