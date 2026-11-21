// Package debug holds development-only tooling and overlays for NSGo games: a frame-time graph, collision and sprite-bounds wireframes, a live scene inspector, an interactive dev console with typed tunables, named timing scopes with Chrome/Perfetto trace export, an asset memory tracker with budgets, and a GC/allocation monitor.
//
// In default builds every feature is compiled in and Enabled reports true. Building with -tags release swaps the implementation for empty no-ops where Enabled reports false and Overlay.View returns nil, so release binaries link none of the overlay UI, graph, console, profiler, memory tracker, GC monitor, or wireframe code.
package debug
