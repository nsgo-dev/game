package main

import (
	_ "embed"
	"errors"

	"github.com/nsgo-dev/game/pkg/leaderboard"
	"github.com/nsgo-dev/nsgo/core"
)

// leaderboardsYAML declares Starburst's leaderboards and their platform IDs.
//
//go:embed leaderboards.yaml
var leaderboardsYAML []byte

// highScoreBoard is the catalog name of the board each finished run's score goes to.
const highScoreBoard = "high-score"

// Statuses shown under the game-over score once the platform answers a submission.
const (
	boardStatusSubmitted = "SCORE SUBMITTED"
	boardStatusSignedOut = "SIGN IN TO RANK SCORES"
	boardStatusFailed    = "SCORE NOT SUBMITTED"
)

// newLeaderboards builds the leaderboard service from the embedded catalog and signs the player in where the platform has a game service.
func newLeaderboards(cx *core.Scope) *leaderboard.Service {
	catalog, err := leaderboard.Parse(leaderboardsYAML)
	if err != nil {
		panic(err)
	}
	boards, err := leaderboard.New(cx, catalog)
	if err != nil {
		panic(err)
	}
	if boards.Available() {
		boards.SignIn(nil)
	}
	return boards
}

// submitScore sends a finished run's score to the high-score board and records the outcome for the game-over overlay. Platforms without a game service, or without an ID for the board, show nothing.
func (gc *GameController) submitScore(score int) {
	run := gc.runID
	gc.Boards.Submit(highScoreBoard, int64(score), func(err error) {
		if run != gc.runID {
			return
		}
		switch {
		case err == nil:
			gc.boardStatus.Set(boardStatusSubmitted)
		case errors.Is(err, leaderboard.ErrUnsupported), errors.Is(err, leaderboard.ErrNoPlatformID):
		case errors.Is(err, leaderboard.ErrUnauthenticated):
			gc.boardStatus.Set(boardStatusSignedOut)
		default:
			gc.boardStatus.Set(boardStatusFailed)
		}
	})
}

// SignInAndSubmit signs the player in from the game-over screen and, once signed in, submits the run that just ended. A sign-in answered after the next run started submits nothing.
func (gc *GameController) SignInAndSubmit() {
	run := gc.runID
	score := gc.lastHUDState.Score
	gc.Boards.SignIn(func(_ leaderboard.Player, err error) {
		if err != nil || run != gc.runID {
			return
		}
		gc.submitScore(score)
	})
}

// ShowLeaderboard opens the platform's high-score board.
func (gc *GameController) ShowLeaderboard() {
	_ = gc.Boards.Show(highScoreBoard)
}
