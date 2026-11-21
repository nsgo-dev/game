package game

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAFrameBeforeTheResumptionTakesNoTimeOff(t *testing.T) {
	var r *loopRun
	updates := 0
	l := &Loop{Update: func(time.Duration) { updates++ }}
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		r = &loopRun{loop: l, cx: cx}
		r.resume()
		return core.View{}
	})
	r.frame(r.last.Add(-5 * time.Millisecond))
	if r.acc < 0 {
		t.Fatalf("acc %v after a frame stamped before the resumption, want at least 0", r.acc)
	}
	r.frame(r.last.Add(l.step()))
	if updates != 1 {
		t.Fatalf("%d updates after a whole step, want 1", updates)
	}
}
