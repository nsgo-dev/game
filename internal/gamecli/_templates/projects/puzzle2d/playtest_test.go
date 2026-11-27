package main

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

// sparkPuzzleState adapts Spark Slide's Board and slideMove to playtest.PuzzleState and playtest.PuzzleScorer.
type sparkPuzzleState struct {
	board *Board
}

func clonePuzzleBoard(b *Board) *Board {
	cp := *b
	cp.lit = slices.Clone(b.lit)
	return &cp
}

func (s sparkPuzzleState) Key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d,%d:", s.board.Spark.X, s.board.Spark.Y)
	for _, lit := range s.board.lit {
		if lit {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	if s.board.Fallen {
		b.WriteString(":F")
	}
	return b.String()
}

func (s sparkPuzzleState) Solved() bool { return s.board.Status() == Won }

func (s sparkPuzzleState) Dead() bool { return s.board.Status() == Lost }

func (s sparkPuzzleState) ProgressScore() float64 {
	return float64(s.board.LitCount) / float64(max(s.board.Lamps(), 1))
}

func (s sparkPuzzleState) Moves() []playtest.PuzzleMove {
	if s.board.Status() != Playing {
		return nil
	}
	out := make([]playtest.PuzzleMove, 0, 4)
	for _, d := range []Dir{DirUp, DirRight, DirDown, DirLeft} {
		if len(s.board.Path(d)) > 0 {
			out = append(out, playtest.PuzzleMove(d.String()))
		}
	}
	return out
}

func (s sparkPuzzleState) Apply(mv playtest.PuzzleMove) (playtest.PuzzleState, bool) {
	var d Dir
	switch mv {
	case "up":
		d = DirUp
	case "down":
		d = DirDown
	case "left":
		d = DirLeft
	case "right":
		d = DirRight
	default:
		return nil, false
	}
	if len(s.board.Path(d)) == 0 {
		return nil, false
	}
	next := clonePuzzleBoard(s.board)
	(&slideMove{Dir: d}).Apply(next)
	return sparkPuzzleState{board: next}, true
}

func puzzlePlaytestLevels() []playtest.PuzzleLevel {
	levels := make([]playtest.PuzzleLevel, len(Levels.Levels))
	for i := range Levels.Levels {
		l := &Levels.Levels[i]
		levels[i] = playtest.PuzzleLevel{
			ID:            l.ID,
			Name:          l.Name,
			Par:           l.Stars.Three,
			IntendedMoves: l.Stars.Three,
			Start:         sparkPuzzleState{board: NewBoard(l)},
		}
	}
	return levels
}

func loadPuzzlePlaytestBot(t testing.TB) playtest.BotConfig {
	t.Helper()
	cfgPath := os.Getenv(playtest.EnvConfig)
	if cfgPath == "" {
		cfgPath = playtest.FileName
	}
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("load %s: %v", cfgPath, err)
	}
	return cfg.Bot
}

// TestPlaytest runs playtest.yaml's puzzle-solver bot against every level in levels.yaml.
func TestPlaytest(t *testing.T) {
	botCfg := loadPuzzlePlaytestBot(t)
	makeSim := playtest.NewPuzzlePackSim(puzzlePlaytestLevels(), botCfg)
	playtest.RunTest(t, playtest.FileName, makeSim)
}

// TestBotReportCanonicalAndDeterministic checks that playtest.yaml is canonical and repeated runs on the same seeds write byte-identical reports.
func TestBotReportCanonicalAndDeterministic(t *testing.T) {
	raw, err := os.ReadFile(playtest.FileName)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := playtest.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := playtest.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("playtest.yaml is not canonical;\n%s", encoded)
	}
	cfg.Runs.Count = 4
	makeSim := playtest.NewPuzzlePackSim(puzzlePlaytestLevels(), cfg.Bot)
	r1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	y1, err := playtest.EncodeReport(r1.Report)
	if err != nil {
		t.Fatal(err)
	}
	y2, err := playtest.EncodeReport(r2.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}
	if !r1.Report.Passed {
		t.Fatalf("playtest goals missed:\n%s", r1.Report.Summary())
	}
}
