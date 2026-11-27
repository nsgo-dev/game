package save_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/save"
)

type playerPartData struct {
	Name string `json:"name"`
	HP   int    `json:"hp"`
}

type plainTestPart struct {
	val      string
	failLoad bool
	loadLog  *[]string
	saveLog  *[]string
	name     string
}

func (p *plainTestPart) SaveState() (any, error) {
	if p.saveLog != nil {
		*p.saveLog = append(*p.saveLog, p.name)
	}
	return p.val, nil
}

func (p *plainTestPart) LoadState(raw json.RawMessage) error {
	if p.loadLog != nil {
		*p.loadLog = append(*p.loadLog, p.name)
	}
	if p.failLoad {
		return errors.New("boom")
	}
	return json.Unmarshal(raw, &p.val)
}

func TestGameSaveLoadDeterministicAndOrdered(t *testing.T) {
	var saveOrder, loadOrder []string
	a := &plainTestPart{name: "zebra", val: "z1", saveLog: &saveOrder, loadLog: &loadOrder}
	b := &plainTestPart{name: "alpha", val: "a1", saveLog: &saveOrder, loadLog: &loadOrder}

	g := save.NewGame()
	g.Register("zebra", 1, a)
	g.Register("alpha", 2, b)

	if got := g.Parts(); !slices.Equal(got, []string{"zebra", "alpha"}) {
		t.Fatalf("Parts = %v, want [zebra alpha]", got)
	}
	if v, ok := g.PartVersion("alpha"); !ok || v != 2 {
		t.Fatalf("PartVersion(alpha) = (%d, %v), want (2, true)", v, ok)
	}

	dir := t.TempDir()
	store := save.NewFileStore(dir)
	if err := g.Save(store, "slot1"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !slices.Equal(saveOrder, []string{"zebra", "alpha"}) {
		t.Fatalf("Save order = %v, want [zebra alpha]", saveOrder)
	}
	firstBytes, err := os.ReadFile(filepath.Join(dir, "slot1.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Repeated save of identical state must write identical bytes.
	if err := g.Save(store, "slot2"); err != nil {
		t.Fatalf("Save slot2: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(dir, "slot2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("non-deterministic save bytes:\n%s\nvs\n%s", firstBytes, secondBytes)
	}

	a.val = "dirty-z"
	b.val = "dirty-a"
	if err := g.Load(store, "slot1"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !slices.Equal(loadOrder, []string{"zebra", "alpha"}) {
		t.Fatalf("Load order = %v, want [zebra alpha]", loadOrder)
	}
	if a.val != "z1" || b.val != "a1" {
		t.Fatalf("loaded = (%q, %q), want (z1, a1)", a.val, b.val)
	}
}

func TestGameLoadAllOrNothingStaged(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	p1 := playerPartData{Name: "SavedHero", HP: 42}
	score := 900
	src := save.NewGame()
	src.Register("player", 1, save.ValuePart(&p1))
	src.Register("score", 1, save.ValuePart(&score))
	if err := src.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}

	// Destination game has valid current state; its "score" part rejects scores > 500.
	dstPlayer := playerPartData{Name: "CurrentHero", HP: 100}
	dstScore := 120
	applyCalled := false
	dst := save.NewGame()
	dst.Register("player", 1, save.StagedFuncPart(
		func() (playerPartData, error) { return dstPlayer, nil },
		func(v playerPartData) (func(), error) {
			return func() {
				applyCalled = true
				dstPlayer = v
			}, nil
		},
	))
	dst.Register("score", 1, save.StagedFuncPart(
		func() (int, error) { return dstScore, nil },
		func(v int) (func(), error) {
			if v > 500 {
				return nil, errors.New("score out of range")
			}
			return func() { dstScore = v }, nil
		},
	))

	err := dst.Load(store, "slot1")
	if err == nil {
		t.Fatal("expected load failure when second staged part fails")
	}
	if applyCalled {
		t.Fatal("first staged part's apply ran even though second staged part failed PrepareLoad")
	}
	if dstPlayer != (playerPartData{Name: "CurrentHero", HP: 100}) || dstScore != 120 {
		t.Fatalf("destination mutated on failed load: player=%+v score=%d", dstPlayer, dstScore)
	}
}

func TestGameLoadAllOrNothingPlainRollback(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	srcA := &plainTestPart{name: "a", val: "saved-a"}
	srcB := &plainTestPart{name: "b", val: "saved-b"}
	src := save.NewGame()
	src.Register("a", 1, srcA)
	src.Register("b", 1, srcB)
	if err := src.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}

	// Mixed staged + plain where the last plain part fails: earlier parts must roll back completely.
	stagedVal := "orig-staged"
	plainA := &plainTestPart{name: "a", val: "orig-a"}
	plainB := &plainTestPart{name: "b", val: "orig-b", failLoad: true}
	dst := save.NewGame()
	dst.Register("s", 1, save.ValuePart(&stagedVal))
	dst.Register("a", 1, plainA)
	dst.Register("b", 1, plainB)

	if err := dst.Load(store, "slot1"); err == nil {
		t.Fatal("expected load error when plain part b fails")
	}
	if stagedVal != "orig-staged" || plainA.val != "orig-a" || plainB.val != "orig-b" {
		t.Fatalf("state not rolled back: staged=%q a=%q b=%q", stagedVal, plainA.val, plainB.val)
	}
}

func TestGamePartAddedAfterSlotWritten(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	// V1 game only has "player".
	v1Player := playerPartData{Name: "OldHero", HP: 15}
	g1 := save.NewGame()
	g1.Register("player", 1, save.ValuePart(&v1Player))
	if err := g1.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}

	// V2 game adds:
	// - "crafting" (ValuePart -> Resetter resets dirty value to zero value)
	// - "settings" (with WithPartDefault -> initialized to explicit default)
	// - "plain" (plain Part without Resetter or default -> stays at its current value)
	v2Player := playerPartData{Name: "Current", HP: 99}
	crafting := map[string]int{"sword": 3}
	difficulty := "hard"
	plain := &plainTestPart{name: "plain", val: "keep-default"}

	g2 := save.NewGame()
	g2.Register("player", 1, save.ValuePart(&v2Player))
	g2.Register("crafting", 1, save.ValuePart(&crafting))
	g2.Register("difficulty", 1, save.ValuePart(&difficulty), save.WithPartDefault("normal"))
	g2.Register("plain", 1, plain)

	if err := g2.Load(store, "slot1"); err != nil {
		t.Fatalf("Load older slot: %v", err)
	}
	if v2Player != v1Player {
		t.Fatalf("player = %+v, want %+v", v2Player, v1Player)
	}
	if len(crafting) != 0 {
		t.Fatalf("crafting = %v, want reset empty map", crafting)
	}
	if difficulty != "normal" {
		t.Fatalf("difficulty = %q, want default \"normal\"", difficulty)
	}
	if plain.val != "keep-default" {
		t.Fatalf("plain = %q, want untouched \"keep-default\"", plain.val)
	}
}

func TestGamePerPartVersionsAndMigrations(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	type playerV1 struct {
		Name string `json:"name"`
		Gold int    `json:"gold"`
	}
	type playerV3 struct {
		Name  string `json:"name"`
		Coins int    `json:"coins"`
		Level int    `json:"level"`
	}

	// Write a v1 slot.
	p1 := playerV1{Name: "Mira", Gold: 50}
	g1 := save.NewGame()
	g1.Register("player", 1, save.ValuePart(&p1))
	if err := g1.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}

	// Load with v3 + two sequential part migrations (1->2 renames gold->coins, 2->3 adds level=1).
	var p3 playerV3
	g3 := save.NewGame()
	g3.RegisterMigration("player", 2, func(raw json.RawMessage) (json.RawMessage, error) {
		var v2 struct {
			Name  string `json:"name"`
			Coins int    `json:"coins"`
		}
		if err := json.Unmarshal(raw, &v2); err != nil {
			return nil, err
		}
		return json.Marshal(playerV3{Name: v2.Name, Coins: v2.Coins, Level: 1})
	})
	g3.Register("player", 3, save.ValuePart(&p3), save.WithPartMigration(1, func(raw json.RawMessage) (json.RawMessage, error) {
		var old playerV1
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Name  string `json:"name"`
			Coins int    `json:"coins"`
		}{Name: old.Name, Coins: old.Gold})
	}))

	if err := g3.Load(store, "slot1"); err != nil {
		t.Fatalf("Load migrated: %v", err)
	}
	want := playerV3{Name: "Mira", Coins: 50, Level: 1}
	if p3 != want {
		t.Fatalf("p3 = %+v, want %+v", p3, want)
	}

	// PeekPart also runs per-part migrations without touching live state.
	p3.Coins = 999
	var peeked playerV3
	if err := g3.PeekPart(store, "slot1", "player", &peeked); err != nil {
		t.Fatalf("PeekPart: %v", err)
	}
	if peeked != want {
		t.Fatalf("peeked = %+v, want %+v", peeked, want)
	}
	if p3.Coins != 999 {
		t.Fatal("PeekPart mutated live state")
	}

	// Missing migration returns ErrMissingMigration without falling back to backup.
	var missingDst playerV3
	gMissing := save.NewGame()
	gMissing.Register("player", 2, save.ValuePart(&missingDst))
	err := gMissing.Load(store, "slot1")
	if !errors.Is(err, save.ErrMissingMigration) {
		t.Fatalf("missing migration err = %v, want ErrMissingMigration", err)
	}
	if le, ok := loaderr.As(err); !ok || le.Field != "data.parts.player.version" {
		t.Fatalf("loaderr = %+v (%v), want field data.parts.player.version", le, ok)
	}

	// Save v3 then try loading with v1 -> ErrFutureVersion (must NOT fall back to older backup).
	if err := g3.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}
	var oldDst playerV1
	gOld := save.NewGame()
	gOld.Register("player", 1, save.ValuePart(&oldDst))
	err = gOld.Load(store, "slot1")
	if !errors.Is(err, save.ErrFutureVersion) {
		t.Fatalf("future part version err = %v, want ErrFutureVersion", err)
	}
}

func TestGameLoadFallsBackToBackupOnPartValidationFailure(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir)

	hp := 50
	g := save.NewGame()
	g.Register("hp", 1, save.StagedFuncPart(
		func() (int, error) { return hp, nil },
		func(v int) (func(), error) {
			if v < 0 {
				return nil, errors.New("negative hp")
			}
			return func() { hp = v }, nil
		},
	))

	// First save: valid (becomes backup when second save overwrites primary).
	if err := g.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}
	// Second save: writes an invalid negative HP that passes JSON encoding but fails Load validation.
	hp = -5
	if err := g.Save(store, "slot1"); err != nil {
		t.Fatal(err)
	}

	hp = 10
	if err := g.Load(store, "slot1"); err != nil {
		t.Fatalf("Load should recover from backup: %v", err)
	}
	if hp != 50 {
		t.Fatalf("hp = %d, want 50 from backup", hp)
	}
}

func TestGameIgnoresRemovedPartAndEncodePartsHelper(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileStore(dir,
		save.WithVersion(2),
		save.WithMigration(1, func(raw json.RawMessage) (json.RawMessage, error) {
			var legacy struct {
				Coins int `json:"coins"`
			}
			if err := json.Unmarshal(raw, &legacy); err != nil {
				return nil, err
			}
			return save.EncodeParts(map[string]save.PartEntry{
				"coins":  {Version: 1, Data: legacy.Coins},
				"legacy": {Version: 1, Data: "dropped-in-v2"},
			})
		}),
	)

	// Write a legacy v1 flat slot directly.
	v1Store := save.NewFileStore(dir, save.WithVersion(1))
	if err := v1Store.Save("slot1", map[string]int{"coins": 77}); err != nil {
		t.Fatal(err)
	}

	var coins int
	g := save.NewGame()
	g.Register("coins", 1, save.ValuePart(&coins))
	if err := g.Load(store, "slot1"); err != nil {
		t.Fatalf("Load migrated legacy slot: %v", err)
	}
	if coins != 77 {
		t.Fatalf("coins = %d, want 77", coins)
	}
}

func TestGameRefusesCorruptSlotLayout(t *testing.T) {
	cases := map[string]string{
		"not object":     `{"version":1,"data":[]}`,
		"missing parts":  `{"version":1,"data":{}}`,
		"unknown field":  `{"version":1,"data":{"parts":{},"extra":1}}`,
		"parts not obj":  `{"version":1,"data":{"parts":[]}}`,
		"part not obj":   `{"version":1,"data":{"parts":{"a":42}}}`,
		"part version 0": `{"version":1,"data":{"parts":{"a":{"version":0,"data":1}}}}`,
		"part missing d": `{"version":1,"data":{"parts":{"a":{"version":1}}}}`,
		"part dup field": `{"version":1,"data":{"parts":{"a":{"version":1,"version":1,"data":1}}}}`,
		"part unknown f": `{"version":1,"data":{"parts":{"a":{"version":1,"data":1,"extra":true}}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			var v int
			g := save.NewGame()
			g.Register("a", 1, save.ValuePart(&v))
			err := g.Load(save.NewFileStore(dir), "slot1")
			if !errors.Is(err, save.ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestEqualJSON(t *testing.T) {
	if !save.EqualJSON(json.RawMessage(`{"b":1,"a":[true,2.0]}`), json.RawMessage(`{ "a": [true, 2], "b": 1 }`)) {
		t.Fatal("expected EqualJSON true for equivalent JSON")
	}
	if save.EqualJSON(json.RawMessage(`{"a":1}`), json.RawMessage(`{"a":2}`)) {
		t.Fatal("expected EqualJSON false for differing values")
	}
	if !reflect.DeepEqual(save.NewGame().Parts(), []string(nil)) {
		t.Fatal("empty game Parts should be nil")
	}
}
