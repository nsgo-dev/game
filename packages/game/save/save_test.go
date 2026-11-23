package save_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type playerState struct {
	Name  string   `json:"name"`
	Level int      `json:"level"`
	Gold  int      `json:"gold"`
	Items []string `json:"items,omitempty"`
}

func equalPlayer(a, b playerState) bool {
	return a.Name == b.Name && a.Level == b.Level && a.Gold == b.Gold && slices.Equal(a.Items, b.Items)
}

func startStorageApp(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *save.Store, string) {
	t.Helper()
	var store *save.Store
	var dataDir string
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store = save.NewStorageStore(cx.Storage(), "save")
		dataDir = cx.DataDir()
		return core.View{}
	}, opts...)
	return app, store, dataDir
}

func TestStorageRoundTripAndRestart(t *testing.T) {
	app, store, _ := startStorageApp(t)

	if store.Exists("slot1") {
		t.Fatal("slot1 exists before Save")
	}
	if slots, err := store.Slots(); err != nil || len(slots) != 0 {
		t.Fatalf("Slots before Save = (%v, %v), want empty", slots, err)
	}

	want1 := playerState{Name: "Ada", Level: 4, Gold: 120, Items: []string{"key", "lamp"}}
	want2 := playerState{Name: "Lin", Level: 9, Gold: 850}
	if err := store.Save("slot2", want2); err != nil {
		t.Fatalf("Save slot2: %v", err)
	}
	if err := store.Save("slot1", want1); err != nil {
		t.Fatalf("Save slot1: %v", err)
	}
	// Overwrite slot1 to also exercise backup creation without duplicating Slots.
	want1.Gold = 250
	if err := store.Save("slot1", want1); err != nil {
		t.Fatalf("Save slot1 overwrite: %v", err)
	}

	if !store.Exists("slot1") || !store.Exists("slot2") {
		t.Fatal("saved slots not reported by Exists")
	}
	slots, err := store.Slots()
	if err != nil {
		t.Fatalf("Slots: %v", err)
	}
	if !slices.Equal(slots, []string{"slot1", "slot2"}) {
		t.Fatalf("Slots = %v, want [slot1 slot2]", slots)
	}

	var got playerState
	if err := store.Load("slot1", &got); err != nil {
		t.Fatalf("Load slot1: %v", err)
	}
	if got.Name != want1.Name || got.Level != want1.Level || got.Gold != want1.Gold || !slices.Equal(got.Items, want1.Items) {
		t.Fatalf("Load slot1 = %+v, want %+v", got, want1)
	}

	// Copy storage entries into a fresh app to simulate a restart.
	kept := map[string]string{}
	st := app.Storage()
	for _, k := range st.Keys() {
		kept[k], _ = st.GetItem(k)
	}

	_, again, _ := startStorageApp(t, nsgotest.Storage(kept))
	if !again.Exists("slot1") {
		t.Fatal("slot1 missing after restart")
	}
	var restarted playerState
	if err := again.Load("slot1", &restarted); err != nil {
		t.Fatalf("Load slot1 after restart: %v", err)
	}
	if restarted.Name != want1.Name || restarted.Gold != want1.Gold {
		t.Fatalf("after restart got %+v, want %+v", restarted, want1)
	}

	// Delete removes primary and backup so slot is gone.
	if err := again.Delete("slot1"); err != nil {
		t.Fatalf("Delete slot1: %v", err)
	}
	if again.Exists("slot1") {
		t.Fatal("slot1 still exists after Delete")
	}
	if err := again.Load("slot1", &restarted); !errors.Is(err, save.ErrNotFound) {
		t.Fatalf("Load deleted slot1 err = %v, want ErrNotFound", err)
	}
	if err := again.Delete("slot1"); err != nil {
		t.Fatalf("deleting absent slot1 returned %v, want nil", err)
	}
	slots, err = again.Slots()
	if err != nil || !slices.Equal(slots, []string{"slot2"}) {
		t.Fatalf("Slots after delete = (%v, %v), want [slot2]", slots, err)
	}
}

func TestFileBackendRoundTripTempDirAndDataDir(t *testing.T) {
	t.Run("TempDir", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "saves")
		store := save.NewFileStore(dir)

		// Nonexistent directory starts empty without error.
		if slots, err := store.Slots(); err != nil || len(slots) != 0 {
			t.Fatalf("Slots on missing dir = (%v, %v), want empty", slots, err)
		}
		if store.Exists("auto") {
			t.Fatal("auto exists before Save")
		}

		first := playerState{Name: "Kai", Level: 1, Gold: 10}
		second := playerState{Name: "Kai", Level: 2, Gold: 55}
		if err := store.Save("auto", first); err != nil {
			t.Fatalf("Save first: %v", err)
		}
		if err := store.Save("auto", second); err != nil {
			t.Fatalf("Save second: %v", err)
		}
		if err := store.Save("profile.1", playerState{Name: "Mina", Level: 7}); err != nil {
			t.Fatalf("Save profile.1: %v", err)
		}
		// Unrelated files and temp files in the directory must be ignored by Slots.
		if err := os.WriteFile(filepath.Join(dir, ".save-leftover.tmp"), []byte("junk"), 0o600); err != nil {
			t.Fatalf("write stray tmp: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o600); err != nil {
			t.Fatalf("write stray txt: %v", err)
		}

		slots, err := store.Slots()
		if err != nil {
			t.Fatalf("Slots: %v", err)
		}
		if !slices.Equal(slots, []string{"auto", "profile.1"}) {
			t.Fatalf("Slots = %v, want [auto profile.1]", slots)
		}

		var got playerState
		if err := store.Load("auto", &got); err != nil {
			t.Fatalf("Load auto: %v", err)
		}
		if !equalPlayer(got, second) {
			t.Fatalf("Load auto = %+v, want %+v", got, second)
		}

		if err := store.Delete("auto"); err != nil {
			t.Fatalf("Delete auto: %v", err)
		}
		if store.Exists("auto") {
			t.Fatal("auto still exists after Delete")
		}
		if err := store.Load("auto", &got); !errors.Is(err, save.ErrNotFound) {
			t.Fatalf("Load deleted auto err = %v, want ErrNotFound", err)
		}
	})

	t.Run("DataDir", func(t *testing.T) {
		_, _, dataDir := startStorageApp(t)
		if dataDir == "" {
			t.Skip("DataDir is empty on this platform")
		}
		saveDir := filepath.Join(dataDir, "save_test_slots")
		t.Cleanup(func() { _ = os.RemoveAll(saveDir) })

		store := save.NewStore(save.FileBackend(saveDir))
		want := playerState{Name: "Nora", Level: 12, Gold: 999}
		if err := store.Save("main", want); err != nil {
			t.Fatalf("Save to DataDir: %v", err)
		}
		var got playerState
		if err := store.Load("main", &got); err != nil {
			t.Fatalf("Load from DataDir: %v", err)
		}
		if !equalPlayer(got, want) {
			t.Fatalf("Load from DataDir = %+v, want %+v", got, want)
		}
	})
}

func TestMigrationsSequentialV1ToV3(t *testing.T) {
	type saveV1 struct {
		HP int `json:"hp"`
	}
	type saveV2 struct {
		HP    int `json:"hp"`
		MaxHP int `json:"max_hp"`
	}
	type saveV3 struct {
		Health int `json:"health"`
		MaxHP  int `json:"max_hp"`
		Shield int `json:"shield"`
	}

	var steps []string
	m1to2 := func(raw json.RawMessage) (json.RawMessage, error) {
		steps = append(steps, "1->2")
		var old saveV1
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		return json.Marshal(saveV2{HP: old.HP, MaxHP: max(old.HP, 100)})
	}
	m2to3 := func(raw json.RawMessage) (json.RawMessage, error) {
		steps = append(steps, "2->3")
		var old saveV2
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		return json.Marshal(saveV3{Health: old.HP, MaxHP: old.MaxHP, Shield: 25})
	}

	run := func(t *testing.T, makeBackend func() save.Backend) {
		b := makeBackend()
		v1Store := save.NewStore(b, save.WithVersion(1))
		if err := v1Store.Save("hero", saveV1{HP: 65}); err != nil {
			t.Fatalf("Save v1: %v", err)
		}

		steps = nil
		v3Store := save.NewStore(b,
			save.WithVersion(3),
			save.WithMigration(1, m1to2),
		)
		v3Store.RegisterMigration(2, m2to3)

		var got saveV3
		if err := v3Store.Load("hero", &got); err != nil {
			t.Fatalf("Load v1 into v3 store: %v", err)
		}
		want := saveV3{Health: 65, MaxHP: 100, Shield: 25}
		if got != want {
			t.Fatalf("migrated save = %+v, want %+v", got, want)
		}
		if !slices.Equal(steps, []string{"1->2", "2->3"}) {
			t.Fatalf("migration order = %v, want [1->2 2->3]", steps)
		}

		// Saving again at v3 needs no migrations on subsequent Load.
		steps = nil
		if err := v3Store.Save("hero", want); err != nil {
			t.Fatalf("Save v3: %v", err)
		}
		var gotV3 saveV3
		if err := v3Store.Load("hero", &gotV3); err != nil {
			t.Fatalf("Load v3: %v", err)
		}
		if len(steps) != 0 || gotV3 != want {
			t.Fatalf("v3 load ran steps %v and got %+v", steps, gotV3)
		}
	}

	t.Run("FileBackend", func(t *testing.T) {
		dir := t.TempDir()
		run(t, func() save.Backend { return save.FileBackend(dir) })
	})
	t.Run("StorageBackend", func(t *testing.T) {
		app, _, _ := startStorageApp(t)
		run(t, func() save.Backend { return save.StorageBackend(app.Storage(), "mig") })
	})
}

func TestFutureVersionReturnsErrFutureVersionWithoutBackupFallback(t *testing.T) {
	dir := t.TempDir()
	b := save.FileBackend(dir)

	// Write a v1 save first so a v1 .bak will exist after v2 overwrites it.
	v1 := save.NewStore(b, save.WithVersion(1))
	if err := v1.Save("slot1", playerState{Name: "Old", Level: 1}); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	v2 := save.NewStore(b, save.WithVersion(2))
	if err := v2.Save("slot1", playerState{Name: "New", Level: 2}); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	// Loading with a v1 store must return ErrFutureVersion and not silently fall back to the v1 backup.
	var got playerState
	err := v1.Load("slot1", &got)
	if !errors.Is(err, save.ErrFutureVersion) {
		t.Fatalf("Load future version err = %v, want ErrFutureVersion", err)
	}
	if !equalPlayer(got, playerState{}) {
		t.Fatalf("dst was modified on ErrFutureVersion: %+v", got)
	}
}

func TestMissingMigrationReturnsError(t *testing.T) {
	dir := t.TempDir()
	b := save.FileBackend(dir)

	v1 := save.NewStore(b, save.WithVersion(1))
	if err := v1.Save("slot1", map[string]int{"hp": 10}); err != nil {
		t.Fatalf("Save v1: %v", err)
	}

	// Register 1->2 but omit 2->3.
	v3 := save.NewStore(b,
		save.WithVersion(3),
		save.WithMigrations(map[int]save.Migration{
			1: func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil },
		}),
	)
	var out map[string]int
	err := v3.Load("slot1", &out)
	if !errors.Is(err, save.ErrMissingMigration) {
		t.Fatalf("Load with missing migration err = %v, want ErrMissingMigration", err)
	}
}

func TestMigrationErrorsAndInvalidOutput(t *testing.T) {
	dir := t.TempDir()
	b := save.FileBackend(dir)
	v1 := save.NewStore(b, save.WithVersion(1))
	if err := v1.Save("s", map[string]int{"x": 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	boom := errors.New("boom")
	failStore := save.NewStore(b, save.WithVersion(2), save.WithMigration(1, func(json.RawMessage) (json.RawMessage, error) {
		return nil, boom
	}))
	var out map[string]int
	if err := failStore.Load("s", &out); !errors.Is(err, boom) {
		t.Fatalf("Load with failing migration err = %v, want boom", err)
	}

	badJSONStore := save.NewStore(b, save.WithVersion(2), save.WithMigration(1, func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{not json`), nil
	}))
	if err := badJSONStore.Load("s", &out); !errors.Is(err, save.ErrCorrupt) {
		t.Fatalf("Load with invalid migration JSON err = %v, want ErrCorrupt", err)
	}
}

func TestFileBackendCorruptPrimaryRecoversFromBackup(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(t *testing.T, primary string)
	}{
		{
			name: "TruncatedJSON",
			corrupt: func(t *testing.T, primary string) {
				if err := os.WriteFile(primary, []byte(`{"version":1,"data":{"name":"Tra`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "GarbageBytes",
			corrupt: func(t *testing.T, primary string) {
				if err := os.WriteFile(primary, []byte{0x00, 0xff, 0xfe, 0x10}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "InvalidEnvelope",
			corrupt: func(t *testing.T, primary string) {
				if err := os.WriteFile(primary, []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "NullPayload",
			corrupt: func(t *testing.T, primary string) {
				if err := os.WriteFile(primary, []byte(`{"version":1,"data":null}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "PrimaryRemoved",
			corrupt: func(t *testing.T, primary string) {
				if err := os.Remove(primary); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "TypeCorruptedPayload",
			corrupt: func(t *testing.T, primary string) {
				if err := os.WriteFile(primary, []byte(`{"version":1,"data":{"name":"BadName","level":"not_an_int"}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := save.NewFileStore(dir)

			first := playerState{Name: "GoodBackup", Level: 5, Gold: 300}
			second := playerState{Name: "Latest", Level: 6, Gold: 400}
			if err := store.Save("slot1", first); err != nil {
				t.Fatalf("Save first: %v", err)
			}
			if err := store.Save("slot1", second); err != nil {
				t.Fatalf("Save second: %v", err)
			}

			tc.corrupt(t, filepath.Join(dir, "slot1.json"))

			if !store.Exists("slot1") {
				t.Fatal("Exists returned false when backup is available")
			}
			slots, err := store.Slots()
			if err != nil || !slices.Equal(slots, []string{"slot1"}) {
				t.Fatalf("Slots = (%v, %v), want [slot1]", slots, err)
			}

			var got playerState
			if err := store.Load("slot1", &got); err != nil {
				t.Fatalf("Load failed to recover from backup: %v", err)
			}
			if !equalPlayer(got, first) {
				t.Fatalf("recovered = %+v, want %+v", got, first)
			}
		})
	}
}

func TestFileBackendCorruptPrimaryDoesNotClobberGoodBackupOnSave(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	first := playerState{Name: "First", Level: 1}
	second := playerState{Name: "Second", Level: 2}
	third := playerState{Name: "Third", Level: 3}
	if err := store.Save("slot1", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("slot1", second); err != nil {
		t.Fatal(err)
	}
	// Corrupt primary, then write third; .bak should remain the valid first save, not the corrupted bytes.
	primary := filepath.Join(dir, "slot1.json")
	if err := os.WriteFile(primary, []byte("corrupt!!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("slot1", third); err != nil {
		t.Fatalf("Save third over corrupt primary: %v", err)
	}

	var got playerState
	if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, third) {
		t.Fatalf("Load third = (%+v, %v), want %+v", got, err, third)
	}
	// Now corrupt primary again and verify .bak is still valid (first).
	if err := os.WriteFile(primary, []byte("corrupt-again"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = playerState{}
	if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, first) {
		t.Fatalf("Load after second corruption = (%+v, %v), want %+v", got, err, first)
	}
}

func TestFileBackendBothPrimaryAndBackupCorruptReturnsErrCorrupt(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)
	if err := store.Save("slot1", playerState{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("slot1", playerState{Name: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slot1.json.bak"), []byte("{also bad"), 0o600); err != nil {
		t.Fatal(err)
	}

	var got playerState
	if err := store.Load("slot1", &got); !errors.Is(err, save.ErrCorrupt) {
		t.Fatalf("Load with both corrupt err = %v, want ErrCorrupt", err)
	}
}

func TestStorageBackendCorruptPrimaryRecoversFromBackup(t *testing.T) {
	app, store, _ := startStorageApp(t)

	first := playerState{Name: "Safe", Level: 3, Gold: 75}
	second := playerState{Name: "Updated", Level: 4, Gold: 150}
	if err := store.Save("slot1", first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := store.Save("slot1", second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	// Corrupt the primary key directly in core.Storage.
	st := app.Storage()
	corrupted := false
	for _, k := range st.Keys() {
		if strings.HasPrefix(k, "save.slot.") {
			if err := st.SetItem(k, `{"version":1,"data":BROKEN`); err != nil {
				t.Fatalf("corrupt primary key: %v", err)
			}
			corrupted = true
		}
	}
	if !corrupted {
		t.Fatal("did not find primary key to corrupt")
	}

	var got playerState
	if err := store.Load("slot1", &got); err != nil {
		t.Fatalf("Load failed to recover from storage backup: %v", err)
	}
	if !equalPlayer(got, first) {
		t.Fatalf("recovered = %+v, want %+v", got, first)
	}
}

func TestFailedWriteLeavesPreviousSaveIntact(t *testing.T) {
	t.Run("NonMarshalableValue", func(t *testing.T) {
		dir := t.TempDir()
		store := save.NewFileStore(dir)
		want := playerState{Name: "Intact", Level: 8, Gold: 500}
		if err := store.Save("slot1", want); err != nil {
			t.Fatalf("Save valid: %v", err)
		}
		if err := store.Save("slot1", make(chan int)); err == nil {
			t.Fatal("Save non-marshalable value succeeded, want error")
		}
		var got playerState
		if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, want) {
			t.Fatalf("Load after failed marshal = (%+v, %v), want %+v", got, err, want)
		}
	})

	t.Run("StorageFullError", func(t *testing.T) {
		var store *save.Store
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cx.ProvideStorage("tight-save-quota", 240)
			store = save.NewStorageStore(cx.Storage(), "save")
			return core.View{}
		})

		want := playerState{Name: "Small", Level: 2, Gold: 40}
		if err := store.Save("slot1", want); err != nil {
			t.Fatalf("Save initial small state: %v", err)
		}

		// Try to save a payload that exceeds the 240-byte quota.
		huge := playerState{Name: strings.Repeat("X", 400), Level: 99}
		err := store.Save("slot1", huge)
		var fullErr *core.StorageFullError
		if !errors.As(err, &fullErr) {
			t.Fatalf("Save oversized err = %v, want *core.StorageFullError", err)
		}

		// Previous save must still be intact and loadable.
		var got playerState
		if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, want) {
			t.Fatalf("Load after StorageFullError = (%+v, %v), want %+v", got, err, want)
		}
	})

	t.Run("FailingBackendWrite", func(t *testing.T) {
		dir := t.TempDir()
		fb := save.FileBackend(dir)
		want := playerState{Name: "BeforeFail", Level: 5}
		if err := save.NewStore(fb).Save("slot1", want); err != nil {
			t.Fatal(err)
		}

		// Wrap FileBackend to simulate an I/O failure on Write.
		writeErr := errors.New("disk full")
		failing := &failWriteBackend{Backend: fb, err: writeErr}
		store := save.NewStore(failing)
		if err := store.Save("slot1", playerState{Name: "NeverWritten"}); !errors.Is(err, writeErr) {
			t.Fatalf("Save on failing backend err = %v, want disk full", err)
		}

		var got playerState
		if err := store.Load("slot1", &got); err != nil || !equalPlayer(got, want) {
			t.Fatalf("Load after write failure = (%+v, %v), want %+v", got, err, want)
		}
	})
}

type failWriteBackend struct {
	save.Backend
	err error
}

func (f *failWriteBackend) Write(string, []byte) error { return f.err }

func TestInvalidSlotNamesRejected(t *testing.T) {
	invalid := []string{
		"",
		".",
		"..",
		"../escape",
		"sub/slot",
		`sub\slot`,
		"a..b",
		".hidden",
		"trailing.",
		" leading",
		"trailing ",
		"bad\x00null",
		"bad\nline",
		"bad:colon",
		"bad*star",
		"bad?quest",
		"bad<angle",
		"bad>angle",
		`bad"quote`,
		"bad|pipe",
		"CON",
		"con.1",
		"nul",
		"PRN",
		"AUX",
		"COM1",
		"lpt9",
		strings.Repeat("a", 201),
	}

	dir := t.TempDir()
	_, storageStore, _ := startStorageApp(t)
	stores := []struct {
		name  string
		store *save.Store
	}{
		{"File", save.NewFileStore(dir)},
		{"Storage", storageStore},
	}

	for _, st := range stores {
		for _, slot := range invalid {
			if err := st.store.Save(slot, playerState{}); !errors.Is(err, save.ErrInvalidSlot) {
				t.Errorf("%s Save(%q) err = %v, want ErrInvalidSlot", st.name, slot, err)
			}
			var got playerState
			if err := st.store.Load(slot, &got); !errors.Is(err, save.ErrInvalidSlot) {
				t.Errorf("%s Load(%q) err = %v, want ErrInvalidSlot", st.name, slot, err)
			}
			if err := st.store.Delete(slot); !errors.Is(err, save.ErrInvalidSlot) {
				t.Errorf("%s Delete(%q) err = %v, want ErrInvalidSlot", st.name, slot, err)
			}
			if st.store.Exists(slot) {
				t.Errorf("%s Exists(%q) = true, want false", st.name, slot)
			}
		}
	}

	// Valid slot names including dots, spaces, hyphens, underscores and ".bak" suffix work cleanly.
	valid := []string{"slot1", "slot-1", "slot_1", "profile.1", "auto save", "foo.bak", "COM10", "console"}
	for _, st := range stores {
		for _, slot := range valid {
			want := playerState{Name: slot}
			if err := st.store.Save(slot, want); err != nil {
				t.Errorf("%s Save(%q) unexpected err: %v", st.name, slot, err)
				continue
			}
			var got playerState
			if err := st.store.Load(slot, &got); err != nil || !equalPlayer(got, want) {
				t.Errorf("%s Load(%q) = (%+v, %v), want %+v", st.name, slot, got, err, want)
			}
		}
	}
}

func TestStoragePrefixesAreIsolated(t *testing.T) {
	app, _, _ := startStorageApp(t)
	a := save.NewStorageStore(app.Storage(), "gameA")
	b := save.NewStorageStore(app.Storage(), "gameB")

	if err := a.Save("slot1", playerState{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Save("slot1", playerState{Name: "B"}); err != nil {
		t.Fatal(err)
	}

	var gotA, gotB playerState
	if err := a.Load("slot1", &gotA); err != nil || gotA.Name != "A" {
		t.Fatalf("gameA Load = (%+v, %v)", gotA, err)
	}
	if err := b.Load("slot1", &gotB); err != nil || gotB.Name != "B" {
		t.Fatalf("gameB Load = (%+v, %v)", gotB, err)
	}
	if err := a.Delete("slot1"); err != nil {
		t.Fatal(err)
	}
	if !b.Exists("slot1") {
		t.Fatal("deleting gameA slot1 affected gameB")
	}
}

func TestEmptyFileBackendDirReturnsErrors(t *testing.T) {
	store := save.NewFileStore("")
	if store.Exists("slot1") {
		t.Fatal("Exists on empty dir returned true")
	}
	if err := store.Save("slot1", playerState{}); err == nil {
		t.Fatal("Save on empty dir succeeded, want error")
	}
	var got playerState
	if err := store.Load("slot1", &got); err == nil {
		t.Fatal("Load on empty dir succeeded, want error")
	}
	if err := store.Delete("slot1"); err == nil {
		t.Fatal("Delete on empty dir succeeded, want error")
	}
	if _, err := store.Slots(); err == nil {
		t.Fatal("Slots on empty dir succeeded, want error")
	}
}

func TestStorageBackendEscapesSlotKeys(t *testing.T) {
	// #546: prefix "save" with slot "slot.x" used to share a key with prefix
	// "save.slot" and slot "x".
	app, _, _ := startStorageApp(t)
	s1 := save.NewStore(save.StorageBackend(app.Storage(), "save"))
	s2 := save.NewStore(save.StorageBackend(app.Storage(), "save.slot"))
	if err := s1.Save("slot.x", playerState{Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s2.Save("x", playerState{Name: "second"}); err != nil {
		t.Fatal(err)
	}
	var got1, got2 playerState
	if err := s1.Load("slot.x", &got1); err != nil {
		t.Fatal(err)
	}
	if err := s2.Load("x", &got2); err != nil {
		t.Fatal(err)
	}
	if got1.Name != "first" || got2.Name != "second" {
		t.Fatalf("loads = %q and %q, want first and second", got1.Name, got2.Name)
	}
}

func TestFileBackendRefusesCaseAndNormalizationCollisions(t *testing.T) {
	// #546: Autosave/autosave address one file on a case-insensitive file
	// system, as do composed and decomposed spellings after normalization.
	store := save.NewStore(save.FileBackend(t.TempDir()))
	if err := store.Save("Autosave", playerState{Name: "upper"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("autosave", playerState{Name: "lower"}); !errors.Is(err, save.ErrInvalidSlot) {
		t.Fatalf("case-only slot save = %v, want ErrInvalidSlot", err)
	}
	if err := store.Save("café", playerState{Name: "nfc"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("cafe\u0301", playerState{Name: "nfd"}); !errors.Is(err, save.ErrInvalidSlot) {
		t.Fatalf("normalization-only slot save = %v, want ErrInvalidSlot", err)
	}
}

func TestStorageBackendSlotsReturnsSlotNames(t *testing.T) {
	// Slots must list the names a Store accepts, not their escaped storage keys.
	app, store, _ := startStorageApp(t)
	for _, slot := range []string{"profile.1", "a%2Eb", "main.conflict-tablet"} {
		if err := store.Save(slot, playerState{Name: slot}); err != nil {
			t.Fatalf("Save %q: %v", slot, err)
		}
		// A second save also leaves a backup key behind.
		if err := store.Save(slot, playerState{Name: slot, Level: 2}); err != nil {
			t.Fatalf("Save %q: %v", slot, err)
		}
	}
	// A key escapeSlotKey cannot produce belongs to no slot.
	_ = app.Storage().SetItem("save.slot.raw.dot", "{}")
	slots, err := store.Slots()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a%2Eb", "main.conflict-tablet", "profile.1"}; !slices.Equal(slots, want) {
		t.Fatalf("Slots = %q, want %q", slots, want)
	}
	for _, slot := range slots {
		var got playerState
		if err := store.Load(slot, &got); err != nil || got.Name != slot {
			t.Fatalf("Load %q = %+v, %v", slot, got, err)
		}
	}
}
