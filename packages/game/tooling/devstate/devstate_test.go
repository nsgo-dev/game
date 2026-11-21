//go:build !release

package devstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/tooling/devstate"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type vec2 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type gameStateV1 struct {
	PlayerPos vec2           `json:"player_pos"`
	HP        int            `json:"hp"`
	Inventory []string       `json:"inventory"`
	Flags     map[string]int `json:"flags"`
}

func TestComputeSchemaHashDetectsStructChanges(t *testing.T) {
	baseVal := devstate.ComputeSchemaHash(gameStateV1{})
	basePtr := devstate.ComputeSchemaHash(&gameStateV1{})
	if baseVal == "" || len(baseVal) != 64 {
		t.Fatalf("unexpected hash %q", baseVal)
	}
	if baseVal != basePtr {
		t.Fatalf("value hash %q != pointer hash %q", baseVal, basePtr)
	}

	type addedField struct {
		PlayerPos vec2           `json:"player_pos"`
		HP        int            `json:"hp"`
		Mana      int            `json:"mana"`
		Inventory []string       `json:"inventory"`
		Flags     map[string]int `json:"flags"`
	}
	type removedField struct {
		PlayerPos vec2           `json:"player_pos"`
		HP        int            `json:"hp"`
		Flags     map[string]int `json:"flags"`
	}
	type renamedField struct {
		PlayerPos vec2           `json:"player_pos"`
		Health    int            `json:"hp"`
		Inventory []string       `json:"inventory"`
		Flags     map[string]int `json:"flags"`
	}
	type retypedField struct {
		PlayerPos vec2           `json:"player_pos"`
		HP        float64        `json:"hp"`
		Inventory []string       `json:"inventory"`
		Flags     map[string]int `json:"flags"`
	}
	type nestedChanged struct {
		X float32 `json:"x"`
		Y float64 `json:"y"`
	}
	type retypedNested struct {
		PlayerPos nestedChanged  `json:"player_pos"`
		HP        int            `json:"hp"`
		Inventory []string       `json:"inventory"`
		Flags     map[string]int `json:"flags"`
	}
	type recursiveNode struct {
		Name string
		Next *recursiveNode
	}

	cases := []struct {
		name   string
		sample any
	}{
		{"added field", addedField{}},
		{"removed field", removedField{}},
		{"renamed field", renamedField{}},
		{"retyped field", retypedField{}},
		{"nested retyped field", retypedNested{}},
	}
	for _, tc := range cases {
		h := devstate.ComputeSchemaHash(tc.sample)
		if h == baseVal {
			t.Errorf("%s produced identical hash %q, want distinct hash", tc.name, h)
		}
	}

	// Recursive types must not infinite-loop.
	if h := devstate.ComputeSchemaHash(recursiveNode{}); len(h) != 64 {
		t.Fatalf("recursive struct hash = %q, want 64-char sha256", h)
	}
}

func TestStoreSaveAndRestoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "session.json")
	store := devstate.NewStore(path)

	want := gameStateV1{
		PlayerPos: vec2{X: 142.5, Y: -36.25},
		HP:        85,
		Inventory: []string{"sword", "potion"},
		Flags:     map[string]int{"boss_door": 1},
	}
	elapsed := 12*time.Second + 500*time.Millisecond
	if err := store.Save("dungeon_floor_2", 750, elapsed, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var got gameStateV1
	res, err := store.Restore(&got)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !res.Restored {
		t.Fatalf("expected Restored=true, got false (SkipReason=%q)", res.SkipReason)
	}
	if res.SceneID != "dungeon_floor_2" || res.LoopTick != 750 || res.LoopElapsed != elapsed {
		t.Fatalf("unexpected restore metadata: %+v", res)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restored state = %+v, want %+v", got, want)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	var afterClear gameStateV1
	res, err = store.Restore(&afterClear)
	if err != nil {
		t.Fatalf("Restore after Clear failed: %v", err)
	}
	if res.Restored || res.SkipReason == "" {
		t.Fatalf("expected Restored=false with reason after Clear, got %+v", res)
	}
}

func TestStoreIncompatibleSchemaStartsFreshAndLogsReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	store := devstate.NewStore(path)

	var (
		mu   sync.Mutex
		logs []string
	)
	store.SetLogger(func(format string, args ...any) {
		mu.Lock()
		logs = append(logs, fmt.Sprintf(format, args...))
		mu.Unlock()
	})

	saved := gameStateV1{
		PlayerPos: vec2{X: 10, Y: 20},
		HP:        99,
		Inventory: []string{"key"},
	}
	if err := store.Save("overworld", 420, 7*time.Second, saved); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Simulate a rebuild where HP changed from int to float64 and a field was added.
	type gameStateV2 struct {
		PlayerPos vec2           `json:"player_pos"`
		HP        float64        `json:"hp"`
		Stamina   float64        `json:"stamina"`
		Inventory []string       `json:"inventory"`
		Flags     map[string]int `json:"flags"`
	}

	fresh := gameStateV2{HP: 100, Stamina: 50}
	res, err := store.Restore(&fresh)
	if err != nil {
		t.Fatalf("incompatible schema should not return fatal error, got: %v", err)
	}
	if res.Restored {
		t.Fatalf("expected Restored=false on schema mismatch, got true")
	}
	if !strings.Contains(strings.ToLower(res.SkipReason), "schema") {
		t.Fatalf("SkipReason = %q, want mention of schema mismatch", res.SkipReason)
	}
	// Fresh state must remain intact.
	if fresh.HP != 100 || fresh.Stamina != 50 {
		t.Fatalf("fresh state was mutated on skipped restore: %+v", fresh)
	}
	mu.Lock()
	logCount := len(logs)
	firstLog := ""
	if logCount > 0 {
		firstLog = logs[0]
	}
	mu.Unlock()
	if logCount == 0 || !strings.Contains(strings.ToLower(firstLog), "schema") {
		t.Fatalf("expected schema mismatch notice to be logged, got logs=%q", logs)
	}
}

func TestStoreCorruptSnapshotStartsFreshAndInvalidTargetErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := devstate.NewStore(path)
	store.SetLogger(func(string, ...any) {})

	var dst gameStateV1
	res, err := store.Restore(&dst)
	if err != nil {
		t.Fatalf("corrupt snapshot should start fresh without fatal error, got: %v", err)
	}
	if res.Restored || !strings.Contains(strings.ToLower(res.SkipReason), "corrupt") {
		t.Fatalf("unexpected restore result for corrupt snapshot: %+v", res)
	}

	if _, err := store.Restore(dst); err == nil {
		t.Fatal("expected error when passing non-pointer to Restore")
	}
}

type fakeScene struct {
	state gameStateV1
}

func (f *fakeScene) Snapshot() (any, error) { return f.state, nil }
func (f *fakeScene) Restore(s any) error {
	ptr, ok := s.(*gameStateV1)
	if !ok {
		return fmt.Errorf("unexpected type %T", s)
	}
	f.state = *ptr
	return nil
}

func TestHookAttachAndStorageStoreAcrossScopeLifecycle(t *testing.T) {
	var (
		shown *core.Signal[bool]
		scene = &fakeScene{
			state: gameStateV1{
				PlayerPos: vec2{X: 33, Y: 44},
				HP:        72,
				Inventory: []string{"shield"},
			},
		}
		store *devstate.Store
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.ProvideStorage("devstate-test", 1<<20)
		store = devstate.NewStorageStore(cx.Storage(), "dev.session")
		shown = core.NewSignal(cx, true)
		return core.Show{
			When: shown.Get,
			Then: func(child *core.Scope) core.Node {
				devstate.Attach(child, store, scene, devstate.HookOptions{
					SceneID:     func() string { return "boss_arena" },
					LoopTick:    func() uint64 { return 1200 },
					LoopElapsed: func() time.Duration { return 20 * time.Second },
				})
				return core.View{}
			},
		}
	})

	// Disposing the child scope triggers automatic save via Hook cleanup.
	app.Post(func() { shown.Set(false) })
	app.Settle()

	restoredScene := &fakeScene{}
	var (
		res devstate.RestoreResult
		err error
	)
	app.Post(func() {
		hook := devstate.Attach(nil, store, restoredScene, devstate.HookOptions{})
		var dst gameStateV1
		res, err = hook.RestoreInto(&dst)
	})
	app.Settle()

	if err != nil {
		t.Fatalf("RestoreInto failed: %v", err)
	}
	if !res.Restored {
		t.Fatalf("expected Restored=true from storage store, got %+v", res)
	}
	if res.SceneID != "boss_arena" || res.LoopTick != 1200 || res.LoopElapsed != 20*time.Second {
		t.Fatalf("unexpected restore result: %+v", res)
	}
	if !reflect.DeepEqual(restoredScene.state, scene.state) {
		t.Fatalf("restored scene state = %+v, want %+v", restoredScene.state, scene.state)
	}
}
