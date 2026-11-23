package leaderboard_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/leaderboard"
	"github.com/nsgo-dev/nsgo/core"
)

const exampleCatalog = `version: 1
leaderboards:
  - name: high-score
    gameCenter: com.example.game.high_score
    playGames: CgkIxxxxxxxxxxEAIQAQ
  - name: daily
    playGames: CgkIxxxxxxxxxxEAIQAw
`

// A Catalog maps each board's name in the game to its ID in every platform game service; Encode writes a parsed catalog back byte for byte.
func ExampleParse() {
	c, err := leaderboard.Parse([]byte(exampleCatalog))
	if err != nil {
		fmt.Println(err)
		return
	}
	b, _ := c.Board("high-score")
	fmt.Println(b.ID(leaderboard.ProviderGameCenter), b.ID(leaderboard.ProviderPlayGames))
	daily, _ := c.Board("daily")
	fmt.Printf("%q\n", daily.ID(leaderboard.ProviderGameCenter))
	out, _ := c.Encode()
	fmt.Println(string(out) == exampleCatalog)
	_, err = leaderboard.Parse([]byte(exampleCatalog + "sort: descending\n"))
	fmt.Println(err != nil)
	// Output:
	// com.example.game.high_score CgkIxxxxxxxxxxEAIQAQ
	// ""
	// true
	// true
}

// A Service names boards by their catalog names; a misspelt name is refused on every platform before the platform is reached.
func ExampleService() {
	root := core.NewRoot(core.RootOptions{})
	defer root.Dispose()
	c, _ := leaderboard.Parse([]byte(exampleCatalog))
	boards, err := leaderboard.New(root, c)
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = boards.ID("high-scores")
	fmt.Println(err)
	fmt.Println(boards.Show("high-scores"))
	fmt.Println(boards.SignedIn(), len(boards.Catalog().Leaderboards))
	// Output:
	// leaderboard: unknown leaderboard "high-scores"
	// leaderboard: unknown leaderboard "high-scores"
	// false 2
}

// ProviderFor names the game service a platform's leaderboards go through.
func ExampleProviderFor() {
	for _, os := range []core.OS{core.OSIOS, core.OSMacOS, core.OSAndroid, core.OSWindows} {
		fmt.Println(leaderboard.ProviderFor(os))
	}
	// Output:
	// gameCenter
	// gameCenter
	// playGames
	// none
}
