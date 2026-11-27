package main

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/idle"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// saveVersion is the schema version of savedGame; raise it with a save.WithMigration when the fields change.
	saveVersion = 1
	// savePrefix keys the game's saves in the app's storage.
	savePrefix = "hearthkeeper"
	// saveSlot is the one slot the game keeps.
	saveSlot = "hearth"
)

// savedGame is the JSON the game keeps in its save slot: numbers as text ("1.5e21"), generators and upgrades by id so reordering hearth.yaml keeps saves, and the wall-clock time the player was last seen, which offline progress counts from.
type savedGame struct {
	Embers     idle.Number      `json:"embers"`
	Earned     idle.Number      `json:"earned"`
	Ash        idle.Number      `json:"ash"`
	Generators map[string]int64 `json:"generators,omitempty"`
	Upgrades   []string         `json:"upgrades,omitempty"`
	Fuel       float64          `json:"fuel"`
	Cold       bool             `json:"cold,omitempty"`
	Won        bool             `json:"won,omitempty"`
	Runs       int              `json:"runs,omitempty"`
	Played     string           `json:"played"`
	Notation   string           `json:"notation"`
	LastSeen   time.Time        `json:"lastSeen"`
}

// newSaveStore returns the store the game saves to in storage.
func newSaveStore(storage core.Storage) *save.Store {
	return save.NewStorageStore(storage, savePrefix, save.WithVersion(saveVersion))
}

// snapshot returns h's state as a save, with the notation and last-seen time the game keeps beside it.
func snapshot(h *hearth, notation idle.Notation, seen time.Time) savedGame {
	s := savedGame{
		Embers:     h.embers,
		Earned:     h.earned,
		Ash:        h.ash,
		Generators: map[string]int64{},
		Fuel:       h.fuel,
		Cold:       h.cold,
		Won:        h.won,
		Runs:       h.runs,
		Played:     h.played.String(),
		Notation:   notation.String(),
		LastSeen:   seen.UTC(),
	}
	for i, g := range h.data.Generators {
		if h.owned[i] > 0 {
			s.Generators[g.ID] = h.owned[i]
		}
	}
	for j, u := range h.data.Upgrades {
		if h.bought[j] {
			s.Upgrades = append(s.Upgrades, u.ID)
		}
	}
	return s
}

// check reports the first value of s a game cannot run with, so save.Store.LoadChecked falls back to the backup copy.
func (s *savedGame) check(data *hearthData) error {
	if s.Embers.Sign() < 0 || s.Earned.Sign() < 0 || s.Ash.Sign() < 0 {
		return errors.New("embers, earned and ash must not be negative")
	}
	if !(s.Fuel >= 0) || s.Fuel > data.Fuel.Capacity {
		return fmt.Errorf("fuel %v is outside 0 to %v", s.Fuel, data.Fuel.Capacity)
	}
	for id, n := range s.Generators {
		if n < 0 {
			return fmt.Errorf("generator %s: %d owned", id, n)
		}
	}
	if d, err := time.ParseDuration(s.Played); err != nil || d < 0 {
		return fmt.Errorf("played %q is not a duration", s.Played)
	}
	if _, ok := parseNotation(s.Notation); !ok {
		return fmt.Errorf("notation %q is not one of %v", s.Notation, notations)
	}
	if s.Runs < 0 {
		return fmt.Errorf("runs %d is negative", s.Runs)
	}
	return nil
}

// restore returns the game s holds on data, its notation and its last-seen time. Generators and upgrades hearth.yaml no longer has are dropped. s must have passed check.
func (s *savedGame) restore(data *hearthData) (*hearth, idle.Notation, time.Time) {
	h := newHearth(data)
	h.embers, h.earned, h.ash = s.Embers, s.Earned, s.Ash
	for i, g := range data.Generators {
		h.owned[i] = s.Generators[g.ID]
	}
	for j, u := range data.Upgrades {
		h.bought[j] = slices.Contains(s.Upgrades, u.ID)
	}
	h.fuel, h.cold, h.won, h.runs = s.Fuel, s.Cold, s.Won, s.Runs
	h.played, _ = time.ParseDuration(s.Played)
	n, _ := parseNotation(s.Notation)
	return h, n, s.LastSeen
}

// notations are the notations the player cycles through, in order.
var notations = []idle.Notation{idle.Short, idle.Letters, idle.Scientific, idle.Engineering}

// parseNotation returns the notation String names.
func parseNotation(name string) (idle.Notation, bool) {
	for _, n := range notations {
		if n.String() == name {
			return n, true
		}
	}
	return 0, false
}

// nextNotation returns the notation after n in notations.
func nextNotation(n idle.Notation) idle.Notation {
	i := slices.Index(notations, n)
	return notations[(i+1)%len(notations)]
}
