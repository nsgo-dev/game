//go:build !js || !wasm

package leaderboard

func defaultMiniGameHost() MiniGameHost { return nil }
