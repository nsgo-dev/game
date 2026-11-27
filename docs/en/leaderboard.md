# leaderboard

```go
import "github.com/nsgo-dev/game/pkg/online/leaderboard"
```

`leaderboard` submits scores to and shows the platform's own leaderboards: Game Center on iOS, iPadOS, tvOS and macOS, Play Games Services on Android, and friend rankings on WeChat and Douyin mini games through host user cloud storage and the open data context. It is a thin layer over NSGo's [`gameservices`](https://github.com/nsgo-dev/nsgo/tree/main/gameservices) package and mini-game host APIs that lets a game name its boards once in a YAML file instead of passing platform IDs around its code.

## Declaring boards

A catalog maps each board's name in the game to its entry in every platform service it is registered with:

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: com.example.game.high_score
    playGames: CgkIxxxxxxxxxxEAIQAQ
    miniGame:
      key: best
      order: desc
  - name: fastest-run
    gameCenter: com.example.game.fastest_run
    miniGame:
      key: fastest
      order: asc
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `leaderboards` | The boards, in the order the game lists them |
| `name` | The game's own key for the board; unique, no control characters or surrounding whitespace |
| `gameCenter` | The leaderboard ID registered in App Store Connect; no two boards share one |
| `playGames` | The leaderboard ID the Google Play Console generated; no two boards share one |
| `miniGame` | Friend ranking config for WeChat and Douyin mini games: `key` (host user cloud storage key, unique across boards, at most 128 bytes) and optional `order` (`desc` or `asc`; omitted defaults to `desc`) |

A board needs at least one platform entry (`gameCenter`, `playGames` or `miniGame`). Game Center and Play Games IDs are at most 100 ASCII letters, digits, `.`, `_` or `-`, and their sort order, score format and reset period are set in App Store Connect and the Play Console. A catalog may declare at most 128 `miniGame` keys (`MaxMiniGameKeys`) of at most 128 bytes each (`MaxMiniGameKeyLength`), matching WeChat's user cloud storage limits.

`Parse` refuses unknown fields, duplicate keys, other versions and a second YAML document. `Catalog.Encode` writes the fields in schema order and leaves out absent entries, so a file `Encode` wrote parses and encodes back byte for byte.

## Submitting and showing

```go
//go:embed leaderboards.yaml
var leaderboardsYAML []byte

catalog, err := leaderboard.Parse(leaderboardsYAML)
boards, err := leaderboard.New(cx, catalog)

boards.SignIn(nil) // at launch; the platform may show its own sign-in

// When a run ends:
boards.Submit("high-score", int64(score), func(err error) {
	if err != nil {
		status.Set("Score not submitted")
	}
})

// From a button:
if err := boards.Show("high-score"); err != nil { ... }
```

- `SignIn` signs the local player in and keeps the result in a signal: `Player` and `SignedIn` update views that read them.
- `Submit` sends a score for the signed-in player. On Game Center and Play Games the platform keeps each player's best by the board's own sort order; on mini games (`ProviderMiniGame`), `Service` writes through host user cloud storage (`setUserCloudStorage`) only when the new score improves on the player's previous best for the board's `order` (caching the best locally in `cx.Storage()` across restarts), so a game can submit every finished run.
- `Show` opens the platform's leaderboard UI. On mini games, `Show` presents the open data context friend ranking over the full game view; `ShowIn(name, bounds)` places it in a specific `core.Bounds` rectangle, and `Hide()` removes it.
- `Scores` loads ranked rows for a game that draws its own board. `Query` chooses `Global` or `Friends`, `AllTime`, `Week` or `Today`, and a limit (25 by default, at most 100). On `ProviderMiniGame`, `Scores` (including `Friends`) always answers `ErrUnsupported` because friend cloud storage is isolated inside the open data context and never returned to the game.

Every callback runs on the engine goroutine after the call that started it has returned, so it can set signals directly.

## Mini games (WeChat and Douyin)

Inside a mini program host (`cx.Env().CanSignInMiniProgram()`), `ProviderFor(cx.Env())` selects `ProviderMiniGame` even when the device OS is iOS or Android, so a WeChat or Douyin mini game never reports Game Center or Play Games.

- On `js/wasm` builds, `New` automatically bridges `nsgoMP.api`, `wx` or `tt` when `setUserCloudStorage` is present. `Submit` encodes each score in WeChat's `{"wxgame":{"score":...,"update_time":...}}` format (`EncodeMiniGameScore` / `DecodeMiniGameScore`) within the 1 KiB KV pair limit (`MaxMiniGameValueBytes`), and `Show` / `ShowIn` / `Hide` forward layout and key messages to `getOpenDataContext().postMessage` (or NSGo's open data context bridge when present).
- Because a mini program's friend data cannot be read from the main game scope, use `Show` / `ShowIn` and `Hide` to display the host's open data context ranking rather than `Scores`.

## Platforms without a game service

`ProviderFor(env)` and `Service.Provider()` name the service the game runs against (`ProviderGameCenter`, `ProviderPlayGames`, `ProviderMiniGame` or `ProviderNone`). When `env.Simulated()` is true without a mini program host (such as a web build run with `--nsgo-os ios`), `ProviderFor(env)` returns `ProviderNone` instead of Game Center or Play Games. `Available` reports whether that service can be used here, and `Supports(name)` whether a board can be submitted to and shown; both read the environment reactively, so a view can hide its leaderboard button with them.

| Error | When |
| --- | --- |
| `ErrUnknownLeaderboard` | The name is not in the catalog. Reported on every platform, so a misspelt name shows up on a desktop too |
| `ErrUnsupported` | No game service here (Windows, Linux, the web, watchOS, simulated OS builds without a mini-game host), or `Scores` on `ProviderMiniGame` |
| `ErrNoPlatformID` | The board has no ID or `miniGame` entry for this platform's service |
| `ErrUnauthenticated` | Nobody is signed in; `SignedIn` turns false |
| `ErrCancelled` | The player dismissed the sign-in |
| `ErrUnavailable` | The service could not be reached or is not enabled |

`Submit`, `Show`, `ShowIn` and `Scores` check `ErrUnknownLeaderboard`, `ErrUnsupported` and `ErrNoPlatformID` before reaching the platform.

A game that needs one board across platforms, or a board on platforms without a game service, runs the [self-hosted leaderboard service](leaderboard-selfhost.md) instead.

## Testing

nsgotest gives every test app its own fake game service:

```go
app := nsgotest.Start(t, root, nsgotest.Environment(core.Environment{CanUseGameServices: true, OS: core.OSIOS}))
app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", DisplayName: "Ada"})
boards.SignIn(nil)
app.Settle()

boards.Submit("high-score", 4200, nil)
app.Settle()
app.GameServicesSubmittedScores()   // [{LeaderboardID: "com.example.game.high_score", Score: 4200, ...}]
app.GameServicesShownLeaderboards() // IDs passed to Show
```

Set `OS` to `core.OSAndroid` to test the `playGames` IDs, and use `SeedLeaderboardScores`, `SetGameServicesFriends` and `SetGameServicesError` for `Scores` and failure paths.

To test mini-game friend rankings in `nsgotest`, start the app with `core.Environment{OS: core.OSIOS, Simulated: true, MiniProgramSignIn: true}` and pass a `MiniGameHost` test double through `leaderboard.WithMiniGameHost(host)` to assert best-score writes, `Show` / `ShowIn` bounds, `Hide`, and `ErrUnsupported` on `Scores`.

## Example

[Starburst](https://github.com/nsgo-dev/game/tree/main/examples/starburst) declares a `high-score` board in `leaderboards.yaml`, submits the score when a run ends, and shows a LEADERBOARD button on the game-over screen once the player is signed in, or a SIGN IN button that signs the player in and submits the run while signed out.
