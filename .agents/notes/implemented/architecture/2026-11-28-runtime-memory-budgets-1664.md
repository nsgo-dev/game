# Runtime memory and asset budgets

`game.yaml` now carries `runtime.memory`, with per-platform Go soft limits and an optional GC target. `File.ApplyRuntime` applies the current target through `runtime/debug`.

`assets.Manager` enforces estimated decoded bytes at load completion, reports a typed budget error, and can release groups explicitly marked evictable when memory pressure is simulated.
