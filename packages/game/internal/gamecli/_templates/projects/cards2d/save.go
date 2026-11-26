package main

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/cards"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

// The game keeps two save slots under the storage keys that start with savePrefix: recordsSlot, the player's wins and losses and the next deal, and tableSlot, the game in progress, written after every move so closing the app never loses it. saveVersion is their schema version: raise it, with a save.WithMigration from the old one, when either changes shape.
const (
	savePrefix  = "suitladders"
	recordsSlot = "records"
	tableSlot   = "table"
	saveVersion = 1
)

// records is what the game remembers across games.
type records struct {
	// NextDeal is the number of the deal a new game plays; the first is 1.
	NextDeal int `json:"nextDeal"`
	// Wins and Losses count the games finished; starting a new deal before a game is over counts as a loss.
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
}

// tableSave is a game in progress: the deal, the redeals left, and the cards of every pile by ID, each pile bottom first and the hand left to right.
type tableSave struct {
	Deal    int        `json:"deal"`
	Redeals int        `json:"redeals"`
	Deck    []string   `json:"deck"`
	Hand    []string   `json:"hand"`
	Discard []string   `json:"discard"`
	Ladders [][]string `json:"ladders"`
}

// newStore keeps the game's slots in store, which is the app's core.Storage on every target, the web's included.
func newStore(store core.Storage) *save.Store {
	return save.NewStorageStore(store, savePrefix, save.WithVersion(saveVersion))
}

// loadRecords reads the records, or the first launch's. A slot that cannot be read, even from its backup, starts afresh rather than stopping the game. A slot written by a newer version of the game holds records this build cannot read, so loadRecords returns the first launch's and reports newer, and the game must not save over that slot.
func loadRecords(st *save.Store) (r records, newer bool) {
	if err := st.Load(recordsSlot, &r); err != nil {
		return records{NextDeal: 1}, errors.Is(err, save.ErrFutureVersion)
	}
	if r.NextDeal < 1 {
		r.NextDeal = 1
	}
	return r, false
}

// snapshot records where every card of t is.
func (t *table) snapshot() tableSave {
	ids := func(s *cards.Slot) []string {
		out := make([]string, 0, s.Len())
		for _, c := range s.Cards() {
			out = append(out, c.ID)
		}
		return out
	}
	s := tableSave{Deal: t.deal, Redeals: t.redeals, Deck: ids(t.deck), Hand: ids(t.hand), Discard: ids(t.discard)}
	for _, l := range t.ladders {
		s.Ladders = append(s.Ladders, ids(l))
	}
	return s
}

// restore puts the cards where s says, after checking that s is a game these rules could reach: every card of the deck exactly once, a hand within its capacity, each ladder climbing from 1 in its own suit, no more redeals than the rules give, and a game that is not over. On an error t is unchanged.
func (t *table) restore(s tableSave) error {
	if s.Deal < 1 {
		return fmt.Errorf("saved deal %d is not a deal number", s.Deal)
	}
	if s.Redeals < 0 || s.Redeals > t.rules.Redeals {
		return fmt.Errorf("saved redeals %d outside 0 to %d", s.Redeals, t.rules.Redeals)
	}
	if len(s.Ladders) != len(t.ladders) {
		return fmt.Errorf("saved %d ladders, want %d", len(s.Ladders), len(t.ladders))
	}
	if len(s.Hand) > t.hand.Capacity {
		return fmt.Errorf("saved hand of %d cards, the most is %d", len(s.Hand), t.hand.Capacity)
	}
	byID := make(map[string]*cards.Card, len(t.all))
	for _, c := range t.all {
		byID[c.ID] = c
	}
	seen := make(map[string]bool, len(t.all))
	pile := func(ids []string) ([]*cards.Card, error) {
		out := make([]*cards.Card, 0, len(ids))
		for _, id := range ids {
			c, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("saved card %q is not in the deck", id)
			}
			if seen[id] {
				return nil, fmt.Errorf("saved card %q appears twice", id)
			}
			seen[id] = true
			out = append(out, c)
		}
		return out, nil
	}
	deck, err := pile(s.Deck)
	if err != nil {
		return err
	}
	hand, err := pile(s.Hand)
	if err != nil {
		return err
	}
	discard, err := pile(s.Discard)
	if err != nil {
		return err
	}
	ladders := make([][]*cards.Card, len(s.Ladders))
	for i, ids := range s.Ladders {
		if ladders[i], err = pile(ids); err != nil {
			return err
		}
		for j, c := range ladders[i] {
			if f := faceOf(c); f.suit != i || f.rank != j+1 {
				return fmt.Errorf("saved ladder %s holds %s at step %d", t.rules.Suits[i].Name, c.ID, j+1)
			}
		}
	}
	if len(seen) != len(t.all) {
		return fmt.Errorf("saved game holds %d of the deck's %d cards", len(seen), len(t.all))
	}
	if t.judge(func(i int) int { return len(ladders[i]) }, len(deck), s.Redeals, hand) != playing {
		return errors.New("saved game is already over")
	}
	t.place(s.Deal, s.Redeals, deck, hand, discard, ladders)
	return nil
}
