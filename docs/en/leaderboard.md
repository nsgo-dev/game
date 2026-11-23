# leaderboard

```go
import "github.com/nsgo-dev/game/packages/game/leaderboard"
```

`leaderboard` submits scores to and shows the platform's own leaderboards: Game Center on iOS, iPadOS, tvOS and macOS, and Play Games Services on Android. It is a thin layer over NSGo's [`gameservices`](https://github.com/nsgo-dev/nsgo/tree/main/gameservices) package that lets a game name its boards once in a YAML file instead of passing platform IDs around its code.

## Declaring boards

A catalog maps each board's name in the game to its ID in every game service it is registered with:

```yaml
version: 1
leaderboards:
  - name: high-score
    gameCenter: com.example.game.high_score
    playGames: CgkIxxxxxxxxxxEAIQAQ
  - name: fastest-run
    gameCenter: com.example.game.fastest_run
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `leaderboards` | The boards, in the order the game lists them |
| `name` | The game's own key for the board; unique, no control characters or surrounding whitespace |
| `gameCenter` | The leaderboard ID registered in App Store Connect; no two boards share one |
| `playGames` | The leaderboard ID the Google Play Console generated; no two boards share one |

A board needs at least one ID, and an ID is at most 100 ASCII letters, digits, `.`, `_` or `-`. Sort order, score format and reset period are set in App Store Connect and the Play Console, not in the catalog.

`Parse` refuses unknown fields, duplicate keys, other versions and a second YAML document. `Catalog.Encode` writes the fields in schema order and leaves out absent IDs, so a file `Encode` wrote parses and encodes back byte for byte.

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
- `Submit` sends a score for the signed-in player. The platform keeps each player's best by the board's own sort order, so a game can submit every finished run.
- `Show` opens the platform's leaderboard UI.
- `Scores` loads ranked rows for a game that draws its own board. `Query` chooses `Global` or `Friends`, `AllTime`, `Week` or `Today`, and a limit (25 by default, at most 100).

Every callback runs on the engine goroutine after the call that started it has returned, so it can set signals directly.

## Platforms without a game service

`Provider` names the service the game runs against (`ProviderGameCenter`, `ProviderPlayGames` or `ProviderNone`). `Available` reports whether that service can be used here, and `Supports(name)` whether a board can be submitted to and shown; both read the environment reactively, so a view can hide its leaderboard button with them.

| Error | When |
| --- | --- |
| `ErrUnknownLeaderboard` | The name is not in the catalog. Reported on every platform, so a misspelt name shows up on a desktop too |
| `ErrUnsupported` | No game service here: Windows, Linux, the web, watchOS and mini programs |
| `ErrNoPlatformID` | The board has no ID for this platform's service |
| `ErrUnauthenticated` | Nobody is signed in; `SignedIn` turns false |
| `ErrCancelled` | The player dismissed the sign-in |
| `ErrUnavailable` | The service could not be reached or is not enabled |

`Submit`, `Show` and `Scores` check the first three before reaching the platform.

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

## Example

[Starburst](https://github.com/nsgo-dev/game/tree/main/examples/starburst) declares a `high-score` board in `leaderboards.yaml`, submits the score when a run ends, and shows a LEADERBOARD button on the game-over screen once the player is signed in, or a SIGN IN button that signs the player in and submits the run while signed out.
