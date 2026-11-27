package main

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/leaderboard"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const highScoreGameCenterID = "dev.nsgo.game.starburst.high_score"

func startWithEnv(t *testing.T, env core.Environment) (*nsgotest.App, *GameController) {
	t.Helper()
	var gc *GameController
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		gc, node = NewGameView(cx, 1337)
		return node
	}, nsgotest.Size(960, 540), nsgotest.Environment(env))
	return app, gc
}

// endRun ends the current run with score, as losing the last hit point does, and lets the next frame notice it.
func endRun(app *nsgotest.App, gc *GameController, score int) {
	gc.Arena.Score = score
	gc.Arena.Player.HP = 0
	gc.Arena.GameOver = true
	app.Advance(50 * time.Millisecond)
	app.Settle()
}

func TestLeaderboardCatalogIsValid(t *testing.T) {
	c, err := leaderboard.Parse(leaderboardsYAML)
	if err != nil {
		t.Fatalf("leaderboards.yaml: %v", err)
	}
	b, ok := c.Board(highScoreBoard)
	if !ok || b.GameCenter != highScoreGameCenterID {
		t.Fatalf("high-score board = %+v, %v", b, ok)
	}
	out, err := c.Encode()
	if err != nil || string(out) != string(leaderboardsYAML) {
		t.Fatalf("leaderboards.yaml is not in canonical form (%v):\n%s", err, out)
	}
}

func TestGameOverSubmitsScoreAndShowsBoard(t *testing.T) {
	app, gc := startWithEnv(t, core.Environment{CanUseGameServices: true, OS: core.OSIOS})
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p-ada", DisplayName: "Ada"})
	gc.Boards.SignIn(nil)
	app.Settle()
	if !gc.Boards.SignedIn() {
		t.Fatal("player not signed in")
	}

	if app.Exists("btn-leaderboard") {
		t.Fatal("leaderboard button shown during play")
	}
	endRun(app, gc, 12345)

	want := []core.GameScoreEntry{{LeaderboardID: highScoreGameCenterID, PlayerID: "p-ada", DisplayName: "Ada", Score: 12345}}
	if got := app.GameServicesSubmittedScores(); !slices.Equal(got, want) {
		t.Fatalf("submitted = %+v, want %+v", got, want)
	}
	if got := app.Find("overlay-board-status").Text; got != boardStatusSubmitted {
		t.Fatalf("overlay-board-status = %q", got)
	}

	// Later frames of the same game-over screen do not submit again.
	app.Advance(200 * time.Millisecond)
	if n := len(app.GameServicesSubmittedScores()); n != 1 {
		t.Fatalf("submitted %d times for one run", n)
	}

	app.Press("btn-leaderboard")
	if shown := app.GameServicesShownLeaderboards(); !slices.Equal(shown, []string{highScoreGameCenterID}) {
		t.Fatalf("shown = %v", shown)
	}
	board := app.GameServicesScores(highScoreGameCenterID)
	if len(board) != 1 || board[0].Score != 12345 || board[0].Rank != 1 {
		t.Fatalf("board = %+v", board)
	}

	app.Press("btn-overlay-action")
	app.Settle()
	if app.Exists("overlay-board-status") || app.Exists("btn-leaderboard") {
		t.Fatal("leaderboard status or button left over after PLAY AGAIN")
	}
}

func TestGameOverWhileSignedOutAsksToSignIn(t *testing.T) {
	app, gc := startWithEnv(t, core.Environment{CanUseGameServices: true, OS: core.OSIOS})
	endRun(app, gc, 500)
	if got := app.Find("overlay-board-status").Text; got != boardStatusSignedOut {
		t.Fatalf("overlay-board-status = %q", got)
	}
	if app.Exists("btn-leaderboard") {
		t.Fatal("leaderboard button shown while signed out")
	}
	if n := len(app.GameServicesSubmittedScores()); n != 0 {
		t.Fatalf("submitted %d scores while signed out", n)
	}

	// SIGN IN signs the player in and submits the run that just ended.
	app.SetGameServicesPlayer(leaderboard.Player{ID: "p-ada", DisplayName: "Ada"})
	app.Press("btn-sign-in")
	app.Settle()
	want := []core.GameScoreEntry{{LeaderboardID: highScoreGameCenterID, PlayerID: "p-ada", DisplayName: "Ada", Score: 500}}
	if got := app.GameServicesSubmittedScores(); !slices.Equal(got, want) {
		t.Fatalf("submitted = %+v, want %+v", got, want)
	}
	if got := app.Find("overlay-board-status").Text; got != boardStatusSubmitted {
		t.Fatalf("overlay-board-status after sign-in = %q", got)
	}
	if app.Exists("btn-sign-in") || !app.Exists("btn-leaderboard") {
		t.Fatal("expected LEADERBOARD in place of SIGN IN once signed in")
	}
}

func TestNewRunIgnoresPreviousRunsReply(t *testing.T) {
	app, gc := startWithEnv(t, core.Environment{CanUseGameServices: true, OS: core.OSIOS})

	// End a run while signed out and leave the game-over screen through the stress toggle before the platform's reply is delivered.
	gc.Arena.Score = 500
	gc.Arena.Player.HP = 0
	gc.Arena.GameOver = true
	gc.syncUI()
	gc.ToggleStress()
	gc.ToggleStress()
	app.Settle()
	if gc.Arena.GameOver {
		t.Fatal("stress toggle did not start a new run")
	}
	if got := gc.boardStatus.GetUntracked(); got != "" {
		t.Fatalf("previous run's reply labelled the new run: %q", got)
	}

	app.SetGameServicesPlayer(leaderboard.Player{ID: "p-ada", DisplayName: "Ada"})
	gc.Boards.SignIn(nil)
	app.Settle()
	endRun(app, gc, 700)
	if got := app.Find("overlay-board-status").Text; got != boardStatusSubmitted {
		t.Fatalf("overlay-board-status = %q", got)
	}
	want := []core.GameScoreEntry{{LeaderboardID: highScoreGameCenterID, PlayerID: "p-ada", DisplayName: "Ada", Score: 700}}
	if got := app.GameServicesSubmittedScores(); !slices.Equal(got, want) {
		t.Fatalf("submitted = %+v, want %+v", got, want)
	}
}

func TestNoLeaderboardWithoutGameService(t *testing.T) {
	for name, env := range map[string]core.Environment{
		"linux":   {OS: core.OSLinux},
		"android": {CanUseGameServices: true, OS: core.OSAndroid}, // no playGames ID declared
	} {
		t.Run(name, func(t *testing.T) {
			app, gc := startWithEnv(t, env)
			app.SetGameServicesPlayer(leaderboard.Player{ID: "p1", Authenticated: true})
			gc.Boards.SignIn(nil)
			app.Settle()
			endRun(app, gc, 900)
			if !app.Exists("overlay-status") {
				t.Fatal("game-over overlay missing")
			}
			if app.Exists("overlay-board-status") || app.Exists("btn-leaderboard") || app.Exists("btn-sign-in") {
				t.Fatal("leaderboard UI shown without a game service ID")
			}
			if n := len(app.GameServicesSubmittedScores()); n != 0 {
				t.Fatalf("submitted %d scores", n)
			}
		})
	}
}
