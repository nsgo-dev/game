package server_test

import (
	"slices"
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

func TestMatchmakerUnfilledAnchorPreservesCandidates(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)

	type wantMatch struct {
		mode, region string
		tickets      []uint64
		players      []string
	}

	cases := []struct {
		name        string
		roomSize    int
		baseWindow  float64
		growth      float64
		nowOffset   time.Duration
		tickets     []server.Ticket
		wantMatches []wantMatch
		wantWaiting []uint64
	}{
		{
			name:       "unfilled anchor drops neighbor in rating chain",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"c"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 170, Party: []string{"d"}, EnqueuedAt: base.Add(3 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"b", "c", "d"}},
			},
			wantWaiting: []uint64{1},
		},
		{
			name:       "unfilled solo anchor preserves party of two for later pair",
			roomSize:   4,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b1", "b2"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"c1", "c2"}, EnqueuedAt: base.Add(2 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3}, players: []string{"b1", "b2", "c1", "c2"}},
			},
			wantWaiting: []uint64{1},
		},
		{
			name:       "unfilled anchor preserves multiple candidates (party of two plus solo)",
			roomSize:   5,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b1", "b2"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 95, Party: []string{"c"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"d1", "d2"}, EnqueuedAt: base.Add(3 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"b1", "b2", "c", "d1", "d2"}},
			},
			wantWaiting: []uint64{1},
		},
		{
			name:       "equal rating party packing preserves solo skipped past larger party",
			roomSize:   4,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "coop", Region: "us", Rating: 1000, Party: []string{"a1", "a2"}, EnqueuedAt: base},
				{ID: 2, Mode: "coop", Region: "us", Rating: 1000, Party: []string{"b1", "b2", "b3"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "coop", Region: "us", Rating: 1000, Party: []string{"c"}, EnqueuedAt: base.Add(2 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "coop", region: "us", tickets: []uint64{2, 3}, players: []string{"b1", "b2", "b3", "c"}},
			},
			wantWaiting: []uint64{1},
		},
		{
			name:       "anchor party larger than room is skipped without blocking later rooms",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 100, Party: []string{"x1", "x2", "x3", "x4"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"c"}, EnqueuedAt: base.Add(3 * time.Second)},
				{ID: 5, Mode: "ranked", Region: "eu", Rating: 170, Party: []string{"d"}, EnqueuedAt: base.Add(4 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{3, 4, 5}, players: []string{"b", "c", "d"}},
			},
			wantWaiting: []uint64{1, 2},
		},
		{
			name:       "full-room party behind unfilled anchor fills cleanly",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 50, Party: []string{"b1", "b2", "b3"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 80, Party: []string{"c"}, EnqueuedAt: base.Add(2 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2}, players: []string{"b1", "b2", "b3"}},
			},
			wantWaiting: []uint64{1, 3},
		},
		{
			name:       "multiple fillable rooms formed behind separate unfilled anchors",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"c"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 170, Party: []string{"d"}, EnqueuedAt: base.Add(3 * time.Second)},
				{ID: 5, Mode: "ranked", Region: "eu", Rating: 1000, Party: []string{"e"}, EnqueuedAt: base.Add(4 * time.Second)},
				{ID: 6, Mode: "ranked", Region: "eu", Rating: 1090, Party: []string{"f"}, EnqueuedAt: base.Add(5 * time.Second)},
				{ID: 7, Mode: "ranked", Region: "eu", Rating: 1180, Party: []string{"g"}, EnqueuedAt: base.Add(6 * time.Second)},
				{ID: 8, Mode: "ranked", Region: "eu", Rating: 1170, Party: []string{"h"}, EnqueuedAt: base.Add(7 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"b", "c", "d"}},
				{mode: "ranked", region: "eu", tickets: []uint64{6, 7, 8}, players: []string{"f", "g", "h"}},
			},
			wantWaiting: []uint64{1, 5},
		},
		{
			name:       "mixed modes and regions each form fillable rooms independently",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"eu-a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"eu-b"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"eu-c"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 170, Party: []string{"eu-d"}, EnqueuedAt: base.Add(3 * time.Second)},
				{ID: 5, Mode: "ranked", Region: "us", Rating: 0, Party: []string{"us-a"}, EnqueuedAt: base},
				{ID: 6, Mode: "ranked", Region: "us", Rating: 90, Party: []string{"us-b"}, EnqueuedAt: base.Add(time.Second)},
				{ID: 7, Mode: "ranked", Region: "us", Rating: 180, Party: []string{"us-c"}, EnqueuedAt: base.Add(2 * time.Second)},
				{ID: 8, Mode: "ranked", Region: "us", Rating: 170, Party: []string{"us-d"}, EnqueuedAt: base.Add(3 * time.Second)},
				{ID: 9, Mode: "casual", Region: "eu", Rating: 0, Party: []string{"cas-a"}, EnqueuedAt: base},
				{ID: 10, Mode: "casual", Region: "eu", Rating: 90, Party: []string{"cas-b"}, EnqueuedAt: base.Add(time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"eu-b", "eu-c", "eu-d"}},
				{mode: "ranked", region: "us", tickets: []uint64{6, 7, 8}, players: []string{"us-b", "us-c", "us-d"}},
			},
			wantWaiting: []uint64{1, 5, 9, 10},
		},
		{
			name:       "deterministic oldest-first and ID tie-breaks after unfilled anchor",
			roomSize:   3,
			baseWindow: 100,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"b"}, EnqueuedAt: base},
				{ID: 5, Mode: "ranked", Region: "eu", Rating: 150, Party: []string{"e"}, EnqueuedAt: base},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 180, Party: []string{"c"}, EnqueuedAt: base},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 170, Party: []string{"d"}, EnqueuedAt: base},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"b", "c", "d"}},
			},
			wantWaiting: []uint64{1, 5},
		},
		{
			name:       "asymmetric widened anchor window does not starve newer fillable room",
			roomSize:   3,
			baseWindow: 50,
			growth:     10,
			nowOffset:  10 * time.Second,
			tickets: []server.Ticket{
				{ID: 1, Mode: "ranked", Region: "eu", Rating: 0, Party: []string{"a"}, EnqueuedAt: base},
				{ID: 2, Mode: "ranked", Region: "eu", Rating: 50, Party: []string{"b"}, EnqueuedAt: base.Add(10 * time.Second)},
				{ID: 3, Mode: "ranked", Region: "eu", Rating: 90, Party: []string{"c"}, EnqueuedAt: base.Add(10 * time.Second)},
				{ID: 4, Mode: "ranked", Region: "eu", Rating: 80, Party: []string{"d"}, EnqueuedAt: base.Add(10 * time.Second)},
			},
			wantMatches: []wantMatch{
				{mode: "ranked", region: "eu", tickets: []uint64{2, 3, 4}, players: []string{"b", "c", "d"}},
			},
			wantWaiting: []uint64{1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mm := server.NewMatchmaker(tc.roomSize)
			mm.BaseWindow = tc.baseWindow
			mm.WindowGrowthPerSecond = tc.growth
			now := base.Add(tc.nowOffset)
			mm.Now = func() time.Time { return now }

			for _, tk := range tc.tickets {
				if err := mm.Enqueue(tk); err != nil {
					t.Fatalf("Enqueue(%d): %v", tk.ID, err)
				}
			}

			matches := mm.Match()
			if len(matches) != len(tc.wantMatches) {
				t.Fatalf("Match() returned %d rooms (%+v), want %d (%+v)", len(matches), matches, len(tc.wantMatches), tc.wantMatches)
			}
			for i, want := range tc.wantMatches {
				got := matches[i]
				if got.Mode != want.mode || got.Region != want.region {
					t.Errorf("match[%d] mode/region = %s/%s, want %s/%s", i, got.Mode, got.Region, want.mode, want.region)
				}
				if !slices.Equal(got.TicketIDs, want.tickets) {
					t.Errorf("match[%d] tickets = %v, want %v", i, got.TicketIDs, want.tickets)
				}
				if !slices.Equal(got.Players, want.players) {
					t.Errorf("match[%d] players = %v, want %v", i, got.Players, want.players)
				}
			}

			waiting := mm.Waiting()
			gotWaiting := make([]uint64, len(waiting))
			for i, w := range waiting {
				gotWaiting[i] = w.ID
			}
			if !slices.Equal(gotWaiting, tc.wantWaiting) {
				t.Errorf("waiting ticket IDs = %v, want %v", gotWaiting, tc.wantWaiting)
			}

			// A second Match() at the same timestamp must not form spurious rooms or drop waiting tickets.
			if again := mm.Match(); len(again) != 0 {
				t.Fatalf("second Match() = %+v, want none", again)
			}
			if mm.Len() != len(tc.wantWaiting) {
				t.Fatalf("waiting count after second Match() = %d, want %d", mm.Len(), len(tc.wantWaiting))
			}
		})
	}
}
