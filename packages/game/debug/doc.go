// Package debug holds development-only overlays for NSGo games: a frame-time graph, collision and sprite-bounds wireframes, and a live scene inspector, toggled with a key.
//
// In default builds every feature is compiled in and Enabled reports true. Building with -tags release swaps the implementation for empty no-ops where Enabled reports false and Overlay.View returns nil, so release binaries link none of the overlay UI, graph or wireframe code.
package debug
