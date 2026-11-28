package liveops

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultAgeGateKey is the core.Storage key used when NewAgeGate receives an empty key.
const DefaultAgeGateKey = "liveops.privacy.age"

// AgeBand is the coarse age range an age gate stores. It never stores a date of birth.
type AgeBand string

const (
	AgeUnknown AgeBand = ""
	AgeUnder13 AgeBand = "under13"
	Age13To15  AgeBand = "13to15"
	Age16To17  AgeBand = "16to17"
	AgeAdult   AgeBand = "adult"
)

var ErrInvalidAgeBand = errors.New("liveops: invalid age band")

// AgeGate persists a player's coarse age band and derives whether child mode applies. Invalid or unreadable storage fails closed to AgeUnknown.
type AgeGate struct {
	store core.Storage
	key   string
	band  AgeBand
}

type ageRecord struct {
	Version int     `json:"version"`
	Band    AgeBand `json:"band"`
}

// NewAgeGate loads a gate from storage. A zero storage value keeps the gate in memory.
func NewAgeGate(store core.Storage, key string) *AgeGate {
	if key == "" {
		key = DefaultAgeGateKey
	}
	g := &AgeGate{store: store, key: key}
	if raw, ok := store.GetItem(key); ok {
		var rec ageRecord
		dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
		dec.DisallowUnknownFields()
		if dec.Decode(&rec) == nil && rec.Version == 1 && validAgeBand(rec.Band) {
			g.band = rec.Band
		}
	}
	return g
}

// Band returns the stored age band, or AgeUnknown before a choice and after invalid storage.
func (g *AgeGate) Band() AgeBand {
	if g == nil {
		return AgeUnknown
	}
	return g.band
}

// ChildMode reports whether the selected band requires child-directed privacy restrictions.
func (g *AgeGate) ChildMode() bool { return g != nil && (g.band == AgeUnder13 || g.band == Age13To15) }

// SetBand validates and persists a new age band. AgeUnknown clears the stored choice.
func (g *AgeGate) SetBand(band AgeBand) error {
	if g == nil || !validAgeBand(band) {
		return ErrInvalidAgeBand
	}
	if band == AgeUnknown {
		g.band = AgeUnknown
		g.store.RemoveItem(g.key)
		return nil
	}
	raw, err := json.Marshal(ageRecord{Version: 1, Band: band})
	if err != nil {
		return err
	}
	if err := g.store.SetItem(g.key, string(raw)); err != nil {
		return err
	}
	g.band = band
	return nil
}

func validAgeBand(band AgeBand) bool {
	switch band {
	case AgeUnknown, AgeUnder13, Age13To15, Age16To17, AgeAdult:
		return true
	default:
		return false
	}
}
