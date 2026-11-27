### Added

- `game/online/audit`: an append-only JSON Lines audit log for dedicated servers — one `<yyyy-mm-dd>.jsonl` file per UTC day rolled at midnight, a bounded queue that drops and counts instead of blocking, batched fsyncs and day-based retention (#1329).
