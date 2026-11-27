package playtest_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/tooling/playtest"
)

// Run a deterministic headless playtest with platformer-runner, collect positioned events and timers, and evaluate goals.
func ExampleRun() {
	cfg := &playtest.Config{
		Version: playtest.CurrentVersion,
		Scope:   playtest.Scope{Project: "demo", Scene: "level_01"},
		Runs:    playtest.Runs{Count: 2, SeedStart: 1, Rate: 60, MaxDuration: 5 * time.Second},
		Bot: playtest.BotConfig{
			Profile:     playtest.ProfilePlatformerRunner,
			ReactionMin: 120 * time.Millisecond,
			ReactionMax: 220 * time.Millisecond,
		},
		Metrics: []playtest.Metric{
			{
				Name:    "finished",
				Label:   "Finished",
				Measure: playtest.Measure{Kind: playtest.MeasureOutcome, Key: playtest.OutcomeFinished, Aggregate: playtest.AggCount},
				Goal:    &playtest.Goal{Source: "brief.yaml", Min: playtest.Float64(2)},
			},
		},
	}

	res, err := playtest.Run(cfg, func(_ uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		x := 0.0
		return playtest.Sim{
			Observe: func() any {
				return playtest.PlatformerObservation{PlayerX: x, ExitX: 120, Grounded: true}
			},
			Step: func(dt time.Duration) {
				src.Step()
				x += src.Axis("move_x") * 120 * dt.Seconds()
				if x >= 120 {
					metrics.Finish()
				}
			},
		}, nil
	}, playtest.RunOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Printf("passed=%v runs=%d finished=%.0f\n", res.Report.Passed, res.Report.SeedCount, res.Report.Metrics[0].Value)
	// Output:
	// passed=true runs=2 finished=2
}
