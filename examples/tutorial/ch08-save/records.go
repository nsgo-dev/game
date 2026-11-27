package main

import (
	"errors"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

// The records live in the save slot recordsSlot, under the storage keys that start with savePrefix. recordsVersion is the slot's schema version: raise it, with a save.WithMigration from the old one, when records changes shape.
const (
	savePrefix     = "starcatcher"
	recordsSlot    = "records"
	recordsVersion = 1
)

// records is what the game remembers between launches.
type records struct {
	Best  int `json:"best"`
	Games int `json:"games"`
}

// newRecordsStore keeps records in store, which is the app's core.Storage on every target, the web's included.
func newRecordsStore(store core.Storage) *save.Store {
	return save.NewStorageStore(store, savePrefix, save.WithVersion(recordsVersion))
}

// loadRecords reads the records, or none for a first launch. A slot that cannot be read, even from its backup, starts afresh rather than stopping the game; the next save replaces it. A slot written by a newer version of the game holds records this build cannot read, so loadRecords returns none and reports newer, and the game must not save over that slot.
func loadRecords(st *save.Store) (r records, newer bool) {
	if err := st.Load(recordsSlot, &r); err != nil {
		return records{}, errors.Is(err, save.ErrFutureVersion)
	}
	return r, false
}

// add counts a finished game that scored score, and reports whether it beat the best.
func (r *records) add(score int) (best bool) {
	r.Games++
	if score > r.Best {
		r.Best = score
		return true
	}
	return false
}
