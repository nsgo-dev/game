package main

import (
	"errors"

	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// progressPrefix is the storage key prefix of the game's saves.
	progressPrefix = "sparkslide"
	// progressSlot is the save slot that holds the player's progress.
	progressSlot = "progress"
	// progressVersion is the version of Progress's saved form; bump it and register a save.Migration when the form changes.
	progressVersion = 1
)

// Progress is what the game keeps between runs: the best stars earned on each level, by level ID.
type Progress struct {
	// Stars maps a level's ID to the most stars a win on it has earned; a level not in it has not been won.
	Stars map[string]int `json:"stars"`
}

// progressStore saves Progress to a save.Store, a versioned JSON slot in the app's storage on every platform.
type progressStore struct {
	store *save.Store
}

// newProgressStore returns a store of Progress in storage.
func newProgressStore(storage core.Storage) *progressStore {
	return &progressStore{store: save.NewStorageStore(storage, progressPrefix, save.WithVersion(progressVersion))}
}

// Load returns the saved progress, or empty progress when there is none; it returns the error of a save it cannot read along with empty progress, so the game still starts.
func (s *progressStore) Load() (Progress, error) {
	var p Progress
	err := s.store.Load(progressSlot, &p)
	if errors.Is(err, save.ErrNotFound) {
		err = nil
	}
	if err != nil || p.Stars == nil {
		p = Progress{Stars: map[string]int{}}
	}
	// A save edited by hand, or written by a later version, may hold any number: keep only the 1 to 3 stars a win earns.
	for id, n := range p.Stars {
		if n < 1 || n > 3 {
			delete(p.Stars, id)
		}
	}
	return p, err
}

// Save writes p.
func (s *progressStore) Save(p Progress) error {
	return s.store.Save(progressSlot, p)
}

// Record keeps stars for the level id when they beat what p holds, and reports whether they did.
func (p *Progress) Record(id string, stars int) bool {
	if stars <= p.Stars[id] {
		return false
	}
	p.Stars[id] = stars
	return true
}

// Unlocked reports whether the level at index i of levels can be played: the first always, and every other once the one before it is won.
func (p *Progress) Unlocked(levels []Level, i int) bool {
	return i == 0 || i > 0 && i < len(levels) && p.Stars[levels[i-1].ID] > 0
}
