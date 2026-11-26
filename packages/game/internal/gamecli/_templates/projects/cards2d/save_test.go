package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// The storage keys of the game's save slots, as save.NewStorageStore names them.
const (
	recordsKey = savePrefix + ".slot." + recordsSlot
	tableKey   = savePrefix + ".slot." + tableSlot
)

// recordsJSON is the stored form of records r, as the game writes it.
func recordsJSON(t *testing.T, r records) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return `{"version":1,"data":` + string(data) + `}`
}

func TestSnapshotRestoresEveryPile(t *testing.T) {
	tb := newTestTable(t, winDeal)
	for range 25 {
		m, _ := nextMove(tb)
		apply(tb, m)
	}
	want := tb.snapshot()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got tableSave
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	other := newTestTable(t, 99)
	if err := other.restore(got); err != nil {
		t.Fatal(err)
	}
	if again := other.snapshot(); !reflect.DeepEqual(again, want) {
		t.Fatalf("restored\n%+v\nwant\n%+v", again, want)
	}
	checkConserved(t, other)
	// The restored game plays on exactly as the saved one.
	if a, b := playOut(t, tb, nextMove), playOut(t, other, nextMove); a != b {
		t.Fatalf("the saved game ended %v and the restored one %v", a, b)
	}
}

func TestRestoreRefusesGamesTheRulesCannotReach(t *testing.T) {
	base := newTestTable(t, winDeal)
	for range 25 {
		m, _ := nextMove(base)
		apply(base, m)
	}
	for name, edit := range map[string]func(*tableSave){
		"no deal":          func(s *tableSave) { s.Deal = 0 },
		"too many redeals": func(s *tableSave) { s.Redeals = 3 },
		"negative redeals": func(s *tableSave) { s.Redeals = -1 },
		"missing ladder":   func(s *tableSave) { s.Ladders = s.Ladders[:3] },
		"unknown card":     func(s *tableSave) { s.Deck[0] = "wind-1" },
		"card twice":       func(s *tableSave) { s.Deck[0] = s.Deck[1] },
		"card missing":     func(s *tableSave) { s.Deck = s.Deck[1:] },
		"hand too big": func(s *tableSave) {
			for len(s.Hand) <= base.hand.Capacity {
				s.Hand, s.Deck = append(s.Hand, s.Deck[0]), s.Deck[1:]
			}
		},
		"ladder out of turn": func(s *tableSave) {
			s.Ladders[0] = append(s.Ladders[0], s.Deck...)
			s.Deck = nil
		},
		"game over": func(s *tableSave) {
			for i, suit := range base.rules.Suits {
				s.Ladders[i] = nil
				for r := 1; r <= base.rules.Ranks; r++ {
					s.Ladders[i] = append(s.Ladders[i], cardID(suit.Name, r))
				}
			}
			s.Deck, s.Hand, s.Discard = nil, nil, nil
		},
	} {
		s := base.snapshot()
		s.Deck, s.Hand, s.Discard = slices.Clone(s.Deck), slices.Clone(s.Hand), slices.Clone(s.Discard)
		for i := range s.Ladders {
			s.Ladders[i] = slices.Clone(s.Ladders[i])
		}
		edit(&s)
		tb := newTestTable(t, 99)
		before := tb.snapshot()
		if err := tb.restore(s); err == nil {
			t.Errorf("%s: restore accepted it", name)
		} else if after := tb.snapshot(); !reflect.DeepEqual(after, before) {
			t.Errorf("%s: a refused restore changed the table", name)
		}
	}
}

// TestGameResumesAfterARestart plays a few moves, starts the app again on the same storage and checks that the same game comes back.
func TestGameResumesAfterARestart(t *testing.T) {
	app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: winDeal})})
	keys := keyControls{app: app, g: g}
	for range 6 {
		m, _ := nextMove(g.table)
		keys.make(m)
	}
	want := g.table.snapshot()
	saved := map[string]string{}
	for _, key := range []string{recordsKey, tableKey} {
		v, ok := app.Storage().GetItem(key)
		if !ok {
			t.Fatalf("nothing saved under %s", key)
		}
		saved[key] = v
	}

	again, g2 := startGame(t, saved)
	if got := g2.table.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("resumed\n%+v\nwant\n%+v", got, want)
	}
	if got := again.Find("id:deal").Text; got != "Deal 1" {
		t.Fatalf("deal = %q, want Deal 1", got)
	}
	if g2.records.NextDeal != winDeal+1 {
		t.Fatalf("next deal %d, want %d", g2.records.NextDeal, winDeal+1)
	}
}

// TestDamagedRecordsNumberOnFromTheResumedDeal starts on a game in progress whose records slot is damaged and checks that the next deal follows the resumed one rather than starting again at deal 1.
func TestDamagedRecordsNumberOnFromTheResumedDeal(t *testing.T) {
	app, g := startGame(t, map[string]string{recordsKey: recordsJSON(t, records{NextDeal: 4})})
	keys := keyControls{app: app, g: g}
	m, _ := nextMove(g.table)
	keys.make(m)
	table, ok := app.Storage().GetItem(tableKey)
	if !ok {
		t.Fatal("nothing saved under " + tableKey)
	}

	again, g2 := startGame(t, map[string]string{recordsKey: `{"version":1,"data":`, tableKey: table})
	if got := again.Find("id:deal").Text; got != "Deal 4" {
		t.Fatalf("deal = %q, want Deal 4", got)
	}
	if g2.records.NextDeal != 5 {
		t.Fatalf("next deal %d, want 5", g2.records.NextDeal)
	}
	again.Press("role:button New deal")
	if got := again.Find("id:deal").Text; got != "Deal 5" {
		t.Fatalf("after New deal, deal = %q, want Deal 5", got)
	}
}

// TestDamagedTableSaveDealsAgain starts on a table slot that does not hold a game and checks that the next deal is dealt instead.
func TestDamagedTableSaveDealsAgain(t *testing.T) {
	app, _ := startGame(t, map[string]string{
		recordsKey: recordsJSON(t, records{NextDeal: 5, Wins: 2, Losses: 1}),
		tableKey:   `{"version":1,"data":{"deal":4,"redeals":2,"deck":["flame-1"],"hand":[],"discard":[],"ladders":[[],[],[],[]]}}`,
	})
	if got := app.Find("id:deal").Text; got != "Deal 5" {
		t.Fatalf("deal = %q, want Deal 5", got)
	}
	if got := app.Find("id:record").Text; got != "Won 2 · Lost 1" {
		t.Fatalf("record = %q", got)
	}
}

// TestNewerSaveIsNotOverwritten starts on records a newer version of the game wrote and checks that playing leaves them as they were.
func TestNewerSaveIsNotOverwritten(t *testing.T) {
	newer := `{"version":2,"data":{"nextDeal":40,"wins":30,"losses":9,"streak":3}}`
	app, g := startGame(t, map[string]string{recordsKey: newer})
	keys := keyControls{app: app, g: g}
	m, _ := nextMove(g.table)
	keys.make(m)
	app.Press("role:button New deal")
	if raw, _ := app.Storage().GetItem(recordsKey); raw != newer {
		t.Fatalf("records = %s, want the newer version's left alone", raw)
	}
	if _, ok := app.Storage().GetItem(tableKey); ok {
		t.Fatal("a table was saved beside a newer version's records")
	}
}
