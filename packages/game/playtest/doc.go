// Package playtest defines playtest.yaml (bots, seeded headless runs, metrics and goals), a metrics collection API for games, a deterministic headless playtest runner with replay recordings and YAML reports, and built-in bot profiles starting with platformer-runner.
//
// A game reports positioned events (such as deaths and pickups), counters, timers and perf readouts through a *Collector during each seeded run. Run drives the game headless for every configured seed through a replay.Recorder so any run can be watched or replayed as a deterministic regression test, evaluates every configured metric against its goal (from brief.yaml, game.yaml or playtest.yaml), and produces a deterministic YAML Report with heat-map position data.
package playtest
