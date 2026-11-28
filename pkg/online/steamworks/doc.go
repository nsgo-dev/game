// Package steamworks provides one process-wide Steamworks client for desktop
// targets (macOS, Linux, and Windows) loaded dynamically through purego without
// cgo or hand-written C. On non-desktop targets (web, iOS, Android, and
// experimental targets) [Open] returns [ErrUnavailable] unless a custom
// [Driver] is supplied in [Options].
//
// Call [Open] once on the game's root [core.Scope] to load the Steamworks
// shared library, optionally invoke SteamAPI_RestartAppIfNecessary, initialize
// Steamworks with SteamAPI_InitFlat, enable SteamAPI_ManualDispatch, and pump
// callbacks once per display frame on the engine goroutine. Disposing the last
// scope that opened the client calls SteamAPI_Shutdown.
//
// Subsystem integrations such as [github.com/nsgo-dev/game/pkg/online/presence]
// and [github.com/nsgo-dev/game/pkg/modding] attach to the shared [*Client] so
// they never initialize, pump, or shut down Steamworks independently.
package steamworks
