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

An envelope with any field besides `version` and `data`, or with either field given twice, is `ErrCorrupt`.

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
