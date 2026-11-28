package gameserver_test

import (
	"context"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
	"github.com/nsgo-dev/game/pkg/online/netplay"
)

// A Matchmaker groups waiting tickets of the same mode and region into rooms of players with close ratings; the accepted rating gap widens the longer a ticket waits.
func ExampleMatchmaker() {
	now := time.Unix(0, 0)
	mm := gameserver.NewMatchmaker(2)
	mm.BaseWindow, mm.WindowGrowthPerSecond, mm.MaxWindow = 50, 10, 500
	mm.Now = func() time.Time { return now }

	mm.Enqueue(gameserver.Ticket{ID: 1, Mode: "duel", Region: "eu", Rating: 1500, Party: []string{"ada"}})
	mm.Enqueue(gameserver.Ticket{ID: 2, Mode: "duel", Region: "eu", Rating: 1620, Party: []string{"bo"}})
	fmt.Println(len(mm.Match()), mm.Len())

	now = now.Add(10 * time.Second)
	for _, m := range mm.Match() {
		fmt.Println(m.Mode, m.Region, m.Players)
	}
	fmt.Println(mm.Len())
	// Output:
	// 0 2
	// duel eu [ada bo]
	// 0
}

// Rooms hosts matches: players join by ID or by a private room's code, and Start begins a match epoch that late joiners catch up to.
func ExampleRooms() {
	rooms := gameserver.NewRooms()
	rooms.CodeGenerator = func() string { return "KX7Q" }
	room, err := rooms.Create(4, true)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(room.Code(), room.State())
	rooms.JoinCode("KX7Q", 1)
	rooms.Join(room.ID(), 2)
	room.Start()
	room.Advance()
	j, err := rooms.Join(room.ID(), 3)
	// Members lists peers in join order.
	fmt.Println(err, room.Members(), room.State(), j.Epoch, j.Tick)
	// Output:
	// KX7Q waiting
	// <nil> [1 2 3] running 1 1
}

// A Guard limits how much each peer may send: packets over the size limit are refused, and bursts beyond the rate are throttled.
func ExampleGuard() {
	now := time.Unix(0, 0)
	g := gameserver.NewGuard(gameserver.Limits{PacketsPerSecond: 2, BurstPackets: 2, MaxMessageSize: 64}, gameserver.LimitThrottle)
	g.Now = func() time.Time { return now }

	names := map[gameserver.GuardAction]string{gameserver.GuardAllow: "allow", gameserver.GuardThrottle: "throttle", gameserver.GuardDisconnect: "disconnect"}
	small := netplay.Packet{Payload: make([]byte, 10)}
	for range 3 {
		action, err := g.Check(7, small)
		fmt.Println(names[action], err)
	}
	_, err := g.Check(8, netplay.Packet{Payload: make([]byte, 100)})
	fmt.Println(err)
	// Output:
	// allow <nil>
	// allow <nil>
	// throttle gameserver: packet rate limited
	// gameserver: message too large
}

// A Runner steps each match at its own fixed rate until its context is canceled, then calls the match's OnStop. Here the match ends itself after three ticks by canceling the context.
func ExampleRunner() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := 0
	r := &gameserver.Runner{Matches: []*gameserver.Match{{
		Name: "arena-1",
		Rate: 1000,
		Update: func(dt time.Duration) {
			ticks++
			fmt.Println("tick", ticks, dt)
			if ticks == 3 {
				cancel()
			}
		},
		OnStop: func() { fmt.Println("stopped after", ticks, "ticks") },
	}}}
	fmt.Println(r.Run(ctx))
	// Output:
	// tick 1 1ms
	// tick 2 1ms
	// tick 3 1ms
	// stopped after 3 ticks
	// <nil>
}
