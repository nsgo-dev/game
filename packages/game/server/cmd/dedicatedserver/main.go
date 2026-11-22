// Command dedicatedserver runs one fixed-step match in a plain Go process.
// Build it with `go build ./server/cmd/dedicatedserver`; it needs no NSGo app,
// window or cgo.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/nsgo-dev/game/packages/game/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var ticks uint64
	match := &server.Match{
		Name: "example",
		Rate: 60,
		Update: func(dt time.Duration) {
			ticks++
			_ = dt
		},
		OnStop: func() { log.Printf("match stopped after %d ticks", ticks) },
	}
	r := &server.Runner{
		Matches: []*server.Match{match},
		OnOverrun: func(m *server.Match, lag time.Duration, missed int) {
			log.Printf("match %q overran its tick by %v (%d missed)", m.Name, lag, missed)
		},
	}
	if err := r.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
