package main

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// recordsPrefix is the storage key prefix of the game's saves.
	recordsPrefix = "pulselanes"
	// recordsSlot is the save slot that holds the player's records.
	recordsSlot = "records"
	// recordsVersion is the version of Records' saved form; bump it and register a save.Migration when the form changes.
	recordsVersion = 1
	// calibrationKey is the storage key rhythm.OffsetStore keeps each device's calibrated offsets under.
	calibrationKey = "pulselanes.calibration"
)

// Records are what the game keeps between runs: the best clear of each chart, by chartKey.
type Records struct {
	// Best maps a chart's key to its best clear; a chart not in it has not been cleared.
	Best map[string]Record `json:"best"`
}

// Record is a chart's best clear.
type Record struct {
	// Score is the best score of a clear.
	Score int `json:"score"`
	// MaxCombo is the longest combo of that clear.
	MaxCombo int `json:"maxCombo"`
}

// chartKey is how the records name a chart: its song's ID and its own, which renaming or reordering either keeps.
func chartKey(song *Song, chart *Chart) string { return song.ID + "/" + chart.ID }

// recordStore saves Records to a save.Store, a versioned JSON slot in the app's storage on every platform.
type recordStore struct {
	store *save.Store
}

// newRecordStore returns a store of Records in storage.
func newRecordStore(storage core.Storage) *recordStore {
	return &recordStore{store: save.NewStorageStore(storage, recordsPrefix, save.WithVersion(recordsVersion))}
}

// Load returns the saved records, or none when there are none; it returns the error of a save it cannot read along with no records, so the game still starts.
func (s *recordStore) Load() (Records, error) {
	var r Records
	err := s.store.Load(recordsSlot, &r)
	if errors.Is(err, save.ErrNotFound) {
		err = nil
	}
	if err != nil || r.Best == nil {
		r = Records{Best: map[string]Record{}}
	}
	return r, err
}

// Save writes r.
func (s *recordStore) Save(r Records) error {
	return s.store.Save(recordsSlot, r)
}

// Record keeps a clear of the chart key with score and maxCombo when it beats the best held, and reports whether it did.
func (r *Records) Record(key string, score, maxCombo int) bool {
	if old, ok := r.Best[key]; ok && score <= old.Score {
		return false
	}
	r.Best[key] = Record{Score: score, MaxCombo: maxCombo}
	return true
}
