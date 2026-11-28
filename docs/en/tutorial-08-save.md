# Tutorial 8: Saving

Keep the player's best score and the number of games they played across launches, in a versioned save slot from `game/save`, and show the best on the title and game-over screens.

The finished code is in [`examples/tutorial/ch08-save`](../../examples/tutorial/ch08-save/).


**Files changed in this chapter:** [`main.go`](../../examples/tutorial/ch08-save/main.go), [`game.go`](../../examples/tutorial/ch08-save/game.go), [`world.go`](../../examples/tutorial/ch08-save/world.go), [`art.go`](../../examples/tutorial/ch08-save/art.go), [`audio.go`](../../examples/tutorial/ch08-save/audio.go), [`records.go`](../../examples/tutorial/ch08-save/records.go), [`screens.go`](../../examples/tutorial/ch08-save/screens.go), [`game_test.go`](../../examples/tutorial/ch08-save/game_test.go), [`world_test.go`](../../examples/tutorial/ch08-save/world_test.go), [`records_test.go`](../../examples/tutorial/ch08-save/records_test.go). The linked chapter directory contains each complete file.

## What to keep

The records are a small struct with JSON tags, in `records.go`:

```go
type records struct {
	Best  int `json:"best"`
	Games int `json:"games"`
}

func (r *records) add(score int) (best bool) {
	r.Games++
	if score > r.Best {
		r.Best = score
		return true
	}
	return false
}
```

## A save store

`save.NewStorageStore` keeps save slots in the app's `core.Storage`, which every target has, the web included:

```go
const (
	savePrefix     = "starcatcher"
	recordsSlot    = "records"
	recordsVersion = 1
)

func newRecordsStore(store core.Storage) *save.Store {
	return save.NewStorageStore(store, savePrefix, save.WithVersion(recordsVersion))
}
```

`Save` writes the slot as JSON inside a versioned envelope, `{"version":1,"data":{"best":12,"games":5}}`, under the key `starcatcher.slot.records`, and keeps the previous good copy as a backup, which `Load` falls back to if the latest copy is damaged. When `records` changes shape in a later release, raise `recordsVersion` and give the store a `save.WithMigration` from the old version; `Load` then upgrades old saves as it reads them. The [save guide](save.md) covers migrations and file-backed stores.

## Load at launch, save at the end

Loading happens once, when the game starts:

```go
func loadRecords(st *save.Store) (r records, newer bool) {
	if err := st.Load(recordsSlot, &r); err != nil {
		return records{}, errors.Is(err, save.ErrFutureVersion)
	}
	return r, false
}
```

On a first launch `Load` returns `save.ErrNotFound`, and the game starts with no records. A slot that cannot be read even from its backup also starts afresh rather than keeping the player out of the game, and the next save replaces it. A slot written by a newer version of the game is different: this build cannot read it, but it holds the player's records, as when an older web bundle is still cached after a newer one ran. The game plays without records, and `loadRecords` reports `newer`, which the game keeps as `keepSlot`.

When a game ends, the game counts it and saves:

```go
func (s *starCatcher) finish() {
	s.newBest.Set(s.records.add(s.world.score))
	s.best.Set(s.records.Best)
	if !s.keepSlot {
		_ = s.store.Save(recordsSlot, s.records)
	}
	s.setPhase(phaseOver)
}
```

`Save` fails only when the storage is full, and then loses no more than this game's record. While `keepSlot` is set, the game does not save at all, so the newer version finds its records as it left them.

## Show the best

The title screen shows the best once there is one, and the game-over screen says "New best!" when the player beat it:

```go
core.Show{
	When: s.newBest.Get,
	Then: func(*core.Scope) core.Node {
		return core.Text{TestID: "new-best", Content: "New best!", Style: core.Style{Color: bestColor, FontSize: 16, FontWeight: core.FontWeightBold}}
	},
	Else: func(*core.Scope) core.Node { return s.bestLine() },
},
```

Run the chapter, play a game, close the app and open it again: the title shows your best.

## Test the save

`nsgotest` gives every app its own storage, empty unless the test fills it with `nsgotest.Storage`, and `app.Storage()` lets the test read what the game wrote. The tests save and load records across two stores, check the exact envelope in storage, and check that a corrupt slot and one from a future version both start afresh, and that only the future one is reported as newer:

```go
func TestUnreadableRecordsStartAfresh(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		newer     bool
	}{
		{"corrupt", "{not json", false},
		{"future", `{"version":99,"data":{"best":5,"games":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, empty, nsgotest.Storage(map[string]string{recordsKey: tc.raw}))
			got, newer := loadRecords(newRecordsStore(app.Storage()))
			if got != (records{}) || newer != tc.newer {
				t.Fatalf("records = %+v, newer %v; want none, newer %v", got, newer, tc.newer)
			}
		})
	}
}
```

App tests play two games, check that the first is a new best and the second, lower one is not, start an app whose storage already holds a best of 12 to check that the title shows it, and play a game over a slot from a future version to check that the slot is left untouched.

## Next

The game is complete. [Chapter 9](tutorial-09-ship.md) gets it ready for players.
