package liveops_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/liveops"
)

// printProvider stands in for an analytics SDK: it prints each event it receives.
type printProvider struct{}

func (printProvider) Send(e liveops.Event) error {
	fmt.Println("sent:", e.Name, e.Properties)
	return nil
}

func (printProvider) Flush() error { return nil }

// Analytics drops every event until the player grants consent, and never sends the ones it dropped.
func ExampleAnalytics() {
	a := liveops.NewMemoryAnalytics(printProvider{})
	fmt.Println(a.Consent(), a.Track("level_start", map[string]any{"level": 1}))
	if err := a.SetConsent(liveops.ConsentGranted); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(a.Track("level_start", map[string]any{"level": 2}))
	a.Flush()
	// Output:
	// unknown false
	// sent: level_start map[level:2]
	// true
}

// A CrashReporter keeps the last steps of the simulation and, when a guarded callback panics, reports them with the panic instead of crashing the game.
func ExampleCrashReporter() {
	c := liveops.NewCrashReporter(liveops.CrashOptions{
		HistorySize: 2,
		Metadata:    map[string]string{"version": "1.4.0"},
		Now:         func() time.Time { return time.Unix(0, 0) },
		Reporter: func(r liveops.CrashReport) {
			fmt.Println("crash:", r.Message, "at step", r.Step, r.Metadata["version"])
			for _, s := range r.LastSteps {
				fmt.Println(" ", s.Step, s.Summary)
			}
		},
	})
	for i := range 3 {
		c.RecordStep(time.Second/60, fmt.Sprint("wave ", i))
	}
	panicked := c.RunSafe(func() {
		var enemies []string
		_ = enemies[3]
	})
	fmt.Println("panicked:", panicked)
	// Output:
	// crash: runtime error: index out of range [3] with length 0 at step 3 1.4.0
	//   2 wave 1
	//   3 wave 2
	// panicked: true
}
