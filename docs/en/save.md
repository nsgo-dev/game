# save

`game/save` stores game save slots as versioned JSON envelopes in `core.Storage` or on disk (such as `cx.DataDir()`), with sequential schema migrations and atomic backup recovery.

```go
import "github.com/nsgo-dev/game/pkg/save"
```

## Choosing a store

Pick a store based on your targets:

- **`NewStorageStore(cx.Storage(), prefix, opts...)`**: stores each slot in `core.Storage` under `<prefix>.slot.<slot>` and `<prefix>.bak.<slot>`. Works on every target, including the web.
- **`NewFileStore(cx.DataDir(), opts...)`**: stores each slot as `<slot>.json` and `<slot>.json.bak` in a directory. Ideal for larger saves on desktop and mobile (on the web, `cx.DataDir()` is empty, so fall back to `NewStorageStore`).
- **`NewStore(backend, opts...)`**: accepts any custom `Backend` implementation (such as cloud sync).

```go
type Progress struct {
	Level int    `json:"level"`
	Coins int    `json:"coins"`
	Name  string `json:"name"`
}

store := save.NewStorageStore(cx.Storage(), "mygame", save.WithVersion(1))

if err := store.Save("slot1", Progress{Level: 3, Coins: 120, Name: "Hero"}); err != nil {
	return err
}

var p Progress
if err := store.Load("slot1", &p); errors.Is(err, save.ErrNotFound) {
	p = Progress{Level: 1}
} else if err != nil {
	return err
}
```

Inspect and manage slots with `store.Exists(slot)`, `store.Slots()` (sorted slice of slot names), and `store.Delete(slot)` (removes the backup and then the primary, so an interrupted delete never leaves a backup that `Load` would resurrect; deleting an absent slot returns `nil`). `save.ValidateSlot(name)` checks a slot name from outside the game, such as a cloud listing, against the same rules before it reaches a `Backend`. A `Backend` that wraps another, such as `liveops.CloudSyncer.Backend()`, can expose it with an `Unwrap() Backend` method; a settings store built on the wrapper then keeps settings exactly where it would on the wrapped backend.

## Versioned envelopes and migrations

Every slot is written inside a `{"version": N, "data": ...}` envelope. When your save schema evolves, bump `WithVersion(N)` and register one-step `Migration` functions (`v -> v+1`) with `WithMigration`, `WithMigrations`, or `RegisterMigration`:

```go
store := save.NewFileStore(cx.DataDir(),
	save.WithVersion(2),
	save.WithMigration(1, func(raw json.RawMessage) (json.RawMessage, error) {
		var v1 struct {
			Level int `json:"level"`
			Gold  int `json:"gold"`
		}
		if err := json.Unmarshal(raw, &v1); err != nil {
			return nil, err
		}
		return json.Marshal(Progress{
			Level: v1.Level,
			Coins: v1.Gold, // renamed field
			Name:  "Adventurer",
		})
	}),
)
```

When `Load` reads an older version, it upgrades the JSON one version at a time (`1 -> 2 -> 3 ...`) before unmarshaling into `dst`. `Load` does not write the migrated save back, so migrations run again on every `Load` until the slot is saved: each one must be a pure function of its input that gives the same result every time.

An envelope with any field besides `version` and `data`, or with either field given twice, is `ErrCorrupt`. `save.EnvelopeVersion(payload)` reads a payload's schema version without decoding its body (including envelopes written by a newer build that carry extra fields).

When syncing slots across devices with `liveops.CloudSyncer`, pass the store's `Version` as `CloudSyncOptions.SaveVersion`. Before writing a cloud copy locally, the syncer checks `save.EnvelopeVersion`; if the cloud copy is newer, it keeps the local slot intact and reports `liveops.OutcomeNeedsUpdate` with `LocalVersion` and `CloudVersion` so the game can ask the player to update instead of writing a future-version envelope that `Load` would refuse with `ErrFutureVersion` and risking a fresh save overwriting the cloud copy. See [cloudsave](cloudsave.md#mixed-client-versions).

Sentinel errors to check with `errors.Is`:

| Error | Meaning |
| --- | --- |
| `ErrNotFound` | No primary or backup save exists for that slot. |
| `ErrInvalidSlot` | Slot name is empty or unsafe for keys/file paths. |
| `ErrFutureVersion` | Save was written by a newer schema version than `Store.Version`. |
| `ErrMissingMigration` | Store lacks a migration step needed to reach `Store.Version`. |
| `ErrCorrupt` | Stored payload is not a valid save envelope. |

## Atomic writes and backup recovery

`Save` marshals the payload completely before touching the backend, so marshaling errors leave existing saves untouched. Before replacing a slot's primary copy, both built-in backends preserve the previous valid primary as a backup:

- **`FileBackend`**: writes to a temporary file in the same directory, syncs it to disk, backs up the existing `<slot>.json` to `<slot>.json.bak`, atomically renames the temp file into place, and syncs the directory; a failed directory sync is returned as a write error, except on file systems that cannot sync a directory at all (`EINVAL` or `ENOTSUP`, as some network and FUSE mounts report), where the sync is skipped. The first write through a backend removes `.save-*.tmp` files an interrupted write left more than an hour ago.
- **`StorageBackend`**: writes the backup key before updating the primary key, so a `*core.StorageFullError` leaves the last good save intact.

On `Load`, if the primary copy is missing, unreadable, or corrupt, `Store` automatically falls back to the backup copy. It rewrites the primary from the backup only when the primary is missing or is not a JSON object at all (empty, truncated or garbage). A primary that failed to read for another reason, such as a permission or I/O error, or that is a JSON object `Load` cannot use, is left in place, since it may be newer than the backup. If the primary carries a version newer than `Store.Version`, even with envelope fields this build does not know, `Load` returns `ErrFutureVersion` without silently downgrading to an older backup.

## Load errors and checks

An error about a copy's contents is a `*loaderr.Error` (see [Errors](errors.md)) that names the copy, the line in the stored JSON, the field, such as `data.items[1]`, and a fix. A `FileBackend` copy is named by its file, such as `saves/slot1.json` or `saves/slot1.json.bak` for the backup, and a `StorageBackend` copy by its storage key; a custom `Backend`'s copy by its slot, `slot1 (backup)` for the backup. The sentinels above still match it with `errors.Is`.

`LoadChecked` is `Load` with a check of the decoded value. A copy the check refuses is treated as corrupt, so the backup is tried, decoded into the value `dst` held before the call rather than over what the refused copy left; when every copy fails, `dst` keeps that value; when the check returns a `*loaderr.Error` about a field of the value, the error gives that field's line in the stored copy, under `data`. `entities.LoadWorld` checks a world snapshot this way:

```go
var snap Progress
err := store.LoadChecked("slot1", &snap, func() error {
	if snap.Level < 1 {
		return loaderr.Errorf("level", "start levels at 1", "level %d is below 1", snap.Level)
	}
	return nil
})
```

## Player settings (`SettingsStore` & `AccessibilitySettings`)

`NewDefaultSettingsStore` (`NewFileSettingsStore` / `NewStorageSettingsStore`) persists `Settings` (`Audio`, `Bindings`, `Display`, `Controls`, `Camera`, `Locale`, `Accessibility`, `Custom`) outside save slots with the same versioned envelope and backup recovery. Call `store.Subscribe(fn)` to react whenever `Save`, `Reset`, or `Delete` succeeds (used by `a11y.NewAssistFromSettings` to keep captions, menus, and effects synchronized with `gameui.SettingsMenu`).

`Controls` stores per-device look sensitivity (multipliers in [`MinLookSensitivity`, `MaxLookSensitivity`], default 1), axis inversion, stick dead zone ([0, `MaxStickDeadZone`], default 0.15, 0 turns it off), trigger threshold (default 0.5, for games that read analog triggers themselves), and vibration strength ([0, 1], default 1). `Camera` stores the vertical field of view in degrees ([`MinFieldOfView`, `MaxFieldOfView`], default 90), camera bob and sway strength ([0, 1], default 1), and motion blur. Both sections validate on load, a settings file written before they existed loads their defaults, and they are dispatched through `SettingsTargets.ApplyControls` and `SettingsTargets.ApplyCamera`.

`AccessibilitySettings` is the single model for accessibility and gameplay-assist preferences:

- **Ranges & canonical names**: `CaptionScale` clamps to `[MinCaptionScale, MaxCaptionScale]` (`0.5`–`3.0`), `GameSpeed` to `[MinGameSpeed, MaxGameSpeed]` (`0.25`–`2.0`), and `ScreenShake` to `[0, 1]`. `NormalizeColorBlindMode` accepts both `"none"` and `"normal"` (normalizing both to `"none"`), `"protanopia"`, `"deuteranopia"`, `"tritanopia"`, and `"high-contrast"` (including `"highcontrast"` / `"high_contrast"`).
- **Assist fields & migration**: holds `Captions`, `SFXCaptions`, `Caption` (`CaptionSettings`), `ReduceFlashes`, `Invincibility`, `AutoAim`, and `HoldToToggle` as typed fields; loading an older file with `Custom["reduce_flashes"]` migrates it into `Accessibility.ReduceFlashes` automatically.
- **Caption styling & per-kind toggles (`CaptionSettings`)**: configures `Background` (`black`, `gray`, `blue`, `white`, or `#rrggbb`), `BackgroundOpacity` (`0`–`1`, default `0.9`; use `SetBackgroundOpacity(0)` for an explicit transparent backdrop), `TextColor` (`white`, `yellow`, `cyan`, `green`, `black`, or `#rrggbb`), `SpeakerColors` (default `true`), `Dialogue` (default `true`), `Chatter`, `Sounds` (synchronized with `SFXCaptions` via `SetSoundCaptions` / `EffectiveSoundCaptions`), and `Size` (`small`, `medium`, `large`, `xlarge`).
- **Follow-platform defaults**: `EffectiveCaptions(env)`, `EffectiveReduceMotion(env)`, `EffectiveHighContrast(env)`, and `EffectiveCaptionScale(env)` fall back to the platform environment while unset; call `SetCaptions`, `SetReduceMotion`, `SetHighContrast`, or `SetCaptionScale` to record an explicit override (including `false` or `1.0`), and `Clear*` to follow the platform again.

## Whole-game save registry (`save.Game`)

Instead of maintaining one hand-written struct and a manual snapshot/restore pair for every subsystem, register each saveable subsystem once on a `save.Game` and save or load the whole game in one call:

```go
saver := save.NewGame()
saver.Register("bag", 1, bag.SavePart())
saver.Register("gear", 1, gear.SavePart())
saver.Register("quests", 1, quests.SavePart())
saver.Register("dialogue", 1, dialogueRunner.SavePart())
saver.Register("persistence", 1, persistence)
saver.Register("player", 1, save.ValuePart(&player))

if err := saver.Save(store, "slot1"); err != nil {
	return err
}
if err := saver.Load(store, "slot1"); err != nil {
	return err
}
```

- **Slot layout**: `Game.Save` writes `{"parts": {"<name>": {"version": N, "data": ...}}}` inside the `Store` envelope, with part keys in deterministic order.
- **Built-in `SavePart()` adapters**: `inventory.Container`, `inventory.Equipment`, `quest.Log`, `stats.Sheet`, `ability.Caster`, `flow.Machine[S]`, `dialogue.Runner`, and `entities.World` each expose `SavePart() save.Part`. `save.Persistence` implements `save.Part`, `save.StagedPart`, and `save.Resetter` directly.
- **Custom parts**: wrap a pointer with `save.ValuePart(&state)`, a save/load pair with `save.FuncPart(saveFn, loadFn, resetFn...)`, or a two-phase validate-then-apply pair with `save.StagedFuncPart(saveFn, prepareFn, resetFn...)`.
- **All-or-nothing loads**: `Game.Load` runs through `Store.LoadChecked`, validates and migrates every part before mutating live state, and applies all parts in registration order. Every built-in adapter implements `save.StagedPart` (`PrepareLoad`), so a failure never touches any subsystem; plain `save.Part` implementations are snapshotted before commit and rolled back on failure. If the primary copy fails part validation, `Store.LoadChecked` automatically tries the backup copy.
- **Per-part versions, migrations, and newly added parts**: bump an individual part's version and pass `save.WithPartMigration(from, fn)` (or `saver.RegisterMigration(name, from, fn)`). When an older slot omits a part added in a newer build, `Game.Load` initializes it from `save.WithPartDefault(v)` if given, calls `ResetState()` if the part implements `save.Resetter` (as all built-in adapters do), or leaves it untouched. A part with a newer version than registered returns `ErrFutureVersion`, and a missing step returns `ErrMissingMigration`, both without downgrading to an older backup.
- **Migrating legacy flat slots & load-menu summaries**: use `save.EncodeParts` inside a `Store`-level `save.Migration` to upgrade a legacy flat struct into a `Game` parts slot, and `saver.PeekPart(store, slot, "player", &summary)` to read one part (with migrations applied) without mutating the running game.

## Persistent per-scene object state (`save.Persistence`)

`save.Persistence` tracks per-scene object state keyed by cleaned scene path and object ID—destroyed objects (`MarkDestroyed` / `IsDestroyed`), moved 2D/3D positions and rotations (`SetPosition` / `SetPosition3D` / `SetRotation`), component field overrides (`SetComponentField` / `ComponentField`), RPG Maker-style boolean self switches (`SetSwitch` / `Switch`), and per-object variables (`SetVar` / `Var`).

Register it on `save.Game` alongside your other parts, and connect it to `sceneformat` when entering and leaving scenes:

```go
// On entering a scene:
spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{
	Components:  reg,
	Persistence: persistence,
	ScenePath:   "scenes/cave.scene.yaml",
})

// Before leaving the scene:
sceneformat.CapturePersistence(doc, world, spawned, persistence, "scenes/cave.scene.yaml")
```

Pass `CapturePersistence` the scene as parsed from its file: it compares each object against the file's position and rotation, records the ones that differ, and clears an earlier override once an object is back where the file places it.

`sceneformat.ApplyPersistence` and `sceneformat.BuildWithPersistence` apply the same overrides to a cloned `SceneDoc` (omitting destroyed objects and pruning connections that reference them) without mutating the original parsed scene document. Integer values inside `Components` and `Vars` round-trip through JSON as Go `int` so `sceneformat.FieldInt` validation continues to pass after reload.

## Round-trip testing (`pkg/save/savetest`)

Use `savetest.RoundTrip` in tests to catch forgotten fields or broken restore paths early:

```go
import "github.com/nsgo-dev/game/pkg/save/savetest"

func TestSaveRoundTrip(t *testing.T) {
	g := newTestGame(t)
	// ... play the game so every subsystem moves away from defaults ...
	savetest.RoundTrip(t, g.saver, func() *save.Game {
		return newTestGame(t).saver
	})
}
```

Passing a fresh factory saves `g` to a temporary store, loads it into the fresh `*save.Game`, verifies both register the same parts and versions, and compares every part's JSON before and after the load. Calling `savetest.RoundTrip(t, g)` without a factory resets any `save.Resetter` parts on `g` before reloading back into `g`.
