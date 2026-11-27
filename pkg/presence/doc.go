// Package presence reports what the player is doing (level, game mode, party
// size, timestamps, artwork, and custom localization tokens) to Steam and Discord
// friend lists through a single provider-neutral API.
//
// # Architecture
//
// A game builds an [Activity] describing the player's current state and passes
// it to a [Manager] configured with one or more [Provider] implementations:
//
//   - [Discord] speaks Discord's local IPC v1 protocol directly over Unix domain
//     sockets on macOS and Linux or named pipes on Windows, in pure Go with no
//     cgo.
//   - [Steam] calls Steamworks' flat C rich presence API
//     (ISteamFriends::SetRichPresence and ClearRichPresence) by dynamically
//     loading the Steamworks shared library through purego on desktop targets,
//     with no hand-written C.
//
// On platforms without desktop IPC or Steamworks (web, iOS, Android) or when
// Discord or Steam is not running, the providers return [ErrUnavailable]. A
// [Manager] fans updates out to every configured provider, deduplicates
// unchanged states, optionally throttles rapid updates via [ManagerConfig.MinInterval],
// and degrades gracefully when one provider is unavailable while another
// succeeds.
//
// # Changing levels
//
// Games typically set their mode, party, and artwork once with [Manager.Update]
// and update the current level as the player progresses with [Manager.SetLevel]:
//
//	mgr := presence.NewManager(
//		presence.ManagerConfig{},
//		presence.NewSteam(presence.SteamConfig{}),
//		discordProvider,
//	)
//	defer mgr.Close()
//
//	_ = mgr.Update(ctx, presence.Activity{
//		Mode:      "Campaign",
//		Level:     "Forest 1-2",
//		PartyID:   "squad-42",
//		PartySize: 2,
//		PartyMax:  4,
//	})
//	_ = mgr.SetLevel(ctx, "Cavern 2-1")
//
// Both [Discord] and [Steam] accept injectable transports ([DiscordConfig.Dial]
// and [SteamConfig.Driver]) so tests can verify the exact wire frames and
// Steamworks key-value mutations deterministically without running Discord or
// Steam.
package presence
