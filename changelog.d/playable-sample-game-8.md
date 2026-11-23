### Added

- `examples/caverun`: plays with held keyboard keys and game controllers through `input.Actions` (`WatchKeys`, `WatchGamepads`, an analog left stick with a dead zone, Start to start or pause, Back to restart) beside touch and the mouse, replacing the key-down timers that approximated held keys (#8).
- `examples/caverun`: a 1000-firefly stress swarm (`F`, North, or the pause menu) drawn with one `DrawAtlas` and moved without allocating, a HUD sprite count, `BenchmarkLevel1000Sprites`, and headless tests that play the level to the end with the HUD buttons, the keyboard, a game controller and the swarm on (#8).
- `examples/caverun`: a README with a screenshot that a test keeps current, the controls, how to start a game with the package, and the performance measurements so far (#8).
