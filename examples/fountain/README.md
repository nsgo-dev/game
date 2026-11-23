# Fountain

**Fountain** is a particle stress scene built with NSGo and the 2D game layer (`game`, `particles`, `sprite`). Four seeded `particles.Emitter` fountains are topped up to 5000 live particles after every fixed step of a `game.Loop`, each particle rotating, scaling and fading, all drawn through the emitters' `sprite.Batch` from a procedural atlas built in memory.

It exists to measure how many particles a device draws at its display rate with NSGo's performance monitor.

## Controls

- `Up` / `Down`: raise or lower the particle count by 1000, between 1000 and 20000.
- `R`: restart the scene from its seed.

## Run, test and benchmark

From `examples/fountain/`:

```sh
# Run with the performance monitor; read the FPS line at the top right while the window is visible
go tool nsgo dev -fps

# Headless tests: the count holds every step, the scene replays for a seed, every particle is drawn
go tool nsgo test ./...

# One fixed step plus one draw of the 5000-particle scene
go tool nsgo test -run '^$' -bench Scene5000 .

# Cross-compile check
go tool nsgo check -target macos,windows,linux,android,web
```
