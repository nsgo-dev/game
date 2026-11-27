// Package leaderboard submits scores to and shows the platform's leaderboards: Game Center on Apple platforms and Play Games Services on Android through NSGo's gameservices package, and friend rankings on WeChat and Douyin mini games through host user cloud storage and the open data context.
//
// A game declares its boards once in a versioned YAML Catalog that maps each board's name in the game to its entry in every platform service, and its code then names boards rather than platform IDs:
//
//	version: 1
//	leaderboards:
//	  - name: high-score
//	    gameCenter: com.example.game.high_score
//	    playGames: CgkIxxxxxxxxxxEAIQAQ
//	    miniGame:
//	      key: best
//	      order: desc
//
//	catalog, err := leaderboard.Parse(data)
//	boards, err := leaderboard.New(cx, catalog)
//	boards.SignIn(nil)
//	boards.Submit("high-score", score, func(err error) { ... })
//	boards.Show("high-score")
//
// Calls complete through callbacks on the engine goroutine. On platforms without a game service (Windows, Linux, the web, watchOS, or simulated OS builds such as --nsgo-os ios without a mini-game host) they report ErrUnsupported, and Available and Supports let a view hide its leaderboard button there. A misspelt board name reports ErrUnknownLeaderboard on every platform.
//
// On WeChat and Douyin mini games (ProviderMiniGame), Submit writes to the host's user cloud storage only when the score improves on the player's previous best for the board's Order, Show and ShowIn present the open data context friend ranking over the game view, Hide removes it, and Scores (including Friends) reports ErrUnsupported because friend data stays inside the isolated open data context.
//
// Tests run against nsgotest's per-app fake game service (start with nsgotest.Environment(core.Environment{CanUseGameServices: true, OS: core.OSIOS}), sign in with App.SetGameServicesPlayer, and read App.GameServicesSubmittedScores and App.GameServicesShownLeaderboards) or against a MiniGameHost supplied via WithMiniGameHost.
package leaderboard
