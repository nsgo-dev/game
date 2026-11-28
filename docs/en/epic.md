# Epic Online Services

`game/online/eos` defines the native boundary for Epic Online Services. Construct `eos.Platform` with product, sandbox and deployment IDs plus an injected driver. The driver's `Login` receives the Epic launcher's exchange code, and `Tick` runs once per game-loop step. Use `UnlockAchievement`, `ProgressAchievement`, `SubmitScore`, `QueryLeaderboard` and `SetPresence` from the engine goroutine. Desktop adapters return `eos.ErrUnavailable` when the EOS shared library is missing. Tests can inject a fake driver without loading native code.
