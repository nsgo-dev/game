package server_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/server"
)

func TestMatchmakerFillsRoomsByModeAndRegion(t *testing.T) {
	mm := server.NewMatchmaker(4)
	now := time.Unix(0, 0)
	mm.Now = func() time.Time { return now }
	for i := 1; i <= 6; i++ {
		if err := mm.Enqueue(server.Ticket{ID: uint64(i), Mode: "duel", Region: "eu", Party: []string{"p" + string(rune('0'+i))}}); err != nil {
			t.Fatal(err)
		}
	}
	matches := mm.Match()
	if len(matches) != 1 || len(matches[0].Players) != 4 {
		t.Fatalf("matches = %+v, want one room of 4", matches)
	}
	if mm.Len() != 2 {
		t.Fatalf("waiting = %d, want 2", mm.Len())
	}
}

func TestMatchmakerSkillWindowWidensWithWait(t *testing.T) {
	mm := server.NewMatchmaker(2)
	now := time.Unix(0, 0)
	mm.Now = func() time.Time { return now }
	mm.BaseWindow = 10
	mm.WindowGrowthPerSecond = 5
	first := now
	if err := mm.Enqueue(server.Ticket{ID: 1, Mode: "ranked", Region: "us", Rating: 0, Party: []string{"p"}, EnqueuedAt: first}); err != nil {
		t.Fatal(err)
	}
	if err := mm.Enqueue(server.Ticket{ID: 2, Mode: "ranked", Region: "us", Rating: 12, Party: []string{"p"}, EnqueuedAt: first.Add(900 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if matches := mm.Match(); len(matches) != 0 {
		t.Fatalf("matched across the 10-point window at 1s: %+v", matches)
	}
	now = now.Add(2 * time.Second) // 3s: the newest waiter's window is 25
	if matches := mm.Match(); len(matches) != 1 || len(matches[0].Players) != 2 {
		t.Fatalf("matches at 3s = %+v, want the pair", matches)
	}
}

func TestMatchmakerKeepsPartiesTogether(t *testing.T) {
	mm := server.NewMatchmaker(4)
	if err := mm.Enqueue(server.Ticket{ID: 1, Mode: "coop", Region: "eu", Party: []string{"a", "b", "c"}}); err != nil {
		t.Fatal(err)
	}
	if err := mm.Enqueue(server.Ticket{ID: 2, Mode: "coop", Region: "eu", Party: []string{"d"}}); err != nil {
		t.Fatal(err)
	}
	matches := mm.Match()
	if len(matches) != 1 || len(matches[0].Players) != 4 {
		t.Fatalf("matches = %+v, want the party of three plus the solo", matches)
	}
	if mm.Len() != 0 {
		t.Fatalf("waiting = %d, want 0", mm.Len())
	}
}

func TestMatchmakerCancelLeavesOthersAtThousand(t *testing.T) {
	mm := server.NewMatchmaker(4)
	for i := uint64(1); i <= 1000; i++ {
		if err := mm.Enqueue(server.Ticket{ID: i, Mode: "ffa", Region: "eu", Party: []string{"p" + string(rune(i%26+'a'))}}); err != nil {
			t.Fatal(err)
		}
	}
	for i := uint64(1); i <= 100; i++ {
		if !mm.Cancel(i) {
			t.Fatalf("cancel %d did not remove a waiting ticket", i)
		}
	}
	matches := mm.Match()
	seen := map[uint64]bool{}
	for _, m := range matches {
		for _, id := range m.TicketIDs {
			if id <= 100 {
				t.Fatalf("cancelled ticket %d was matched", id)
			}
			if seen[id] {
				t.Fatalf("ticket %d matched twice", id)
			}
			seen[id] = true
		}
		if len(m.Players) != 4 {
			t.Fatalf("room has %d players, want 4", len(m.Players))
		}
	}
	if mm.Len() != 0 {
		t.Fatalf("waiting = %d, want all 900 matched", mm.Len())
	}
}

func TestMatchmakerCustomRule(t *testing.T) {
	mm := server.NewMatchmaker(2)
	mm.Rule = func(tickets []*server.Ticket, _ int, _ func(*server.Ticket) float64) []server.Matchup {
		if len(tickets) < 2 {
			return nil
		}
		return []server.Matchup{{
			Mode: tickets[0].Mode, Region: tickets[0].Region,
			TicketIDs: []uint64{tickets[0].ID, tickets[1].ID},
			Players:   append(append([]string(nil), tickets[0].Party...), tickets[1].Party...),
		}}
	}
	for i := uint64(1); i <= 2; i++ {
		if err := mm.Enqueue(server.Ticket{ID: i, Mode: "custom", Region: "eu", Party: []string{"p"}}); err != nil {
			t.Fatal(err)
		}
	}
	if matches := mm.Match(); len(matches) != 1 || mm.Len() != 0 {
		t.Fatalf("custom rule result = %+v, waiting %d", matches, mm.Len())
	}
}
