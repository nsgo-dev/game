package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"time"
)

func Root(cx *core.Scope) core.Node {
	return core.Text{Content: "Co-op room: create or join with a room code"}
}
func fixedTickCount(d time.Duration, rate int) int { return int(d.Seconds() * float64(rate)) }
