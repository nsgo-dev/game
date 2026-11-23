package save_test

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

type progress struct {
	Level int `json:"level"`
	Coins int `json:"coins"`
}

// A Store saves values in named slots as versioned JSON; here in the app's key-value storage, as a web or mobile build does.
func ExampleStore() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	store := save.NewStorageStore(root.Storage(), "saves")
	if err := store.Save("slot1", progress{Level: 3, Coins: 120}); err != nil {
		fmt.Println(err)
		return
	}
	var p progress
	fmt.Println(store.Load("slot1", &p), p)
	slots, _ := store.Slots()
	fmt.Println(slots, errors.Is(store.Load("slot2", &p), save.ErrNotFound))
	// Output:
	// <nil> {3 120}
	// [slot1] true
}

// A migration upgrades a save written by an older version of the game before it is read, one version at a time.
func ExampleWithMigration() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	// Version 1 of the game stored only the level.
	old := save.NewStorageStore(root.Storage(), "saves")
	old.Save("slot1", map[string]int{"level": 2})

	// Version 2 adds coins and gives old saves a starting amount.
	store := save.NewStorageStore(root.Storage(), "saves",
		save.WithVersion(2),
		save.WithMigration(1, func(data json.RawMessage) (json.RawMessage, error) {
			var v map[string]any
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, err
			}
			v["coins"] = 50
			return json.Marshal(v)
		}),
	)
	var p progress
	fmt.Println(store.Load("slot1", &p), p)
	// Output:
	// <nil> {2 50}
}

// A SettingsStore keeps player settings apart from save slots and fills in defaults when none were saved.
func ExampleSettingsStore() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()

	settings := save.NewStorageSettingsStore(root.Storage(), "settings")
	s, err := settings.Load()
	fmt.Println(err, s.Audio.Master, s.Audio.Muted)
	s.Audio.Master = 0.4
	s.Locale = "ja"
	if err := settings.Save(s); err != nil {
		fmt.Println(err)
		return
	}
	again, _ := settings.Load()
	fmt.Println(again.Audio.Master, again.Locale)
	// Output:
	// <nil> 1 false
	// 0.4 ja
}
