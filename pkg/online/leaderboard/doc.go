// Package leaderboard submits scores to and shows the platform's leaderboards, Game Center on Apple platforms and Play Games Services on Android, through NSGo's gameservices package.
//
// A game declares its boards once in a versioned YAML Catalog that maps each board's name in the game to its ID in every game service, and its code then names boards rather than platform IDs:
//
//	version: 1
//	leaderboards:
//	  - name: high-score
//	    gameCenter: com.example.game.high_score
//	    playGames: CgkIxxxxxxxxxxEAIQAQ
//
//	catalog, err := leaderboard.Parse(data)
//	boards, err := leaderboard.New(cx, catalog)
//	boards.SignIn(nil)
//	boards.Submit("high-score", score, func(err error) { ... })
//	boards.Show("high-score")
//
// Calls complete through callbacks on the engine goroutine. On platforms without a game service (Windows, Linux, the web, watchOS and mini programs) they report ErrUnsupported, and Available and Supports let a view hide its leaderboard button there. A misspelt board name reports ErrUnknownLeaderboard on every platform.
//
// Tests run against nsgotest's per-app fake game service: start the app with nsgotest.Environment(core.Environment{CanUseGameServices: true, OS: core.OSIOS}), sign a player in with App.SetGameServicesPlayer, and read App.GameServicesSubmittedScores and App.GameServicesShownLeaderboards.
package leaderboard
