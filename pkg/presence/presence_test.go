package presence_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/presence"
)

type stubProvider struct {
	mu       sync.Mutex
	name     string
	sets     []presence.Activity
	clears   int
	closed   int
	setErr   error
	clearErr error
}

func (p *stubProvider) Name() string { return p.name }

func (p *stubProvider) Set(_ context.Context, act presence.Activity) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.setErr != nil {
		return p.setErr
	}
	p.sets = append(p.sets, act.Clone())
	return nil
}

func (p *stubProvider) Clear(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clearErr != nil {
		return p.clearErr
	}
	p.clears++
	return nil
}

func (p *stubProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed++
	return nil
}

func TestManagerChangingLevelUpdatesSteamAndDiscord(t *testing.T) {
	steamDrv := newMockSteamDriver()
	steamProvider := presence.NewSteam(presence.SteamConfig{
		DisplayToken: "#Status_Playing",
		Driver:       steamDrv,
	})

	discordSrv := &mockDiscordServer{}
	discordProvider, err := presence.NewDiscord(presence.DiscordConfig{
		ClientID: "9876543210",
		PID:      1337,
		Dial:     discordSrv.dial,
	})
	if err != nil {
		t.Fatal(err)
	}

	mgr := presence.NewManager(presence.ManagerConfig{}, steamProvider, discordProvider)
	ctx := context.Background()

	// Initial level + mode + party.
	if err := mgr.Update(ctx, presence.Activity{
		Mode:       "Co-op",
		Level:      "Forest 1-2",
		PartyID:    "party-77",
		PartySize:  2,
		PartyMax:   4,
		LargeImage: "world1",
	}); err != nil {
		t.Fatalf("Update initial: %v", err)
	}

	// Verify Steam received initial level.
	steamState := steamDrv.snapshot()
	if steamState["status"] != "Co-op - Forest 1-2" || steamState["level"] != "Forest 1-2" ||
		steamState["steam_player_group"] != "party-77" || steamState["steam_player_group_size"] != "2" ||
		steamState["steam_display"] != "#Status_Playing" {
		t.Fatalf("initial Steam presence = %v", steamState)
	}

	// Change level through Manager.SetLevel: must update BOTH Steam and Discord.
	if err := mgr.SetLevel(ctx, "Volcano 3-4"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}

	steamState = steamDrv.snapshot()
	if steamState["status"] != "Co-op - Volcano 3-4" || steamState["level"] != "Volcano 3-4" ||
		steamState["mode"] != "Co-op" || steamState["steam_player_group"] != "party-77" {
		t.Fatalf("updated Steam presence after SetLevel = %v", steamState)
	}

	// Change party size through Manager.SetParty.
	if err := mgr.SetParty(ctx, "party-77", 3, 4); err != nil {
		t.Fatalf("SetParty: %v", err)
	}
	if steamDrv.snapshot()["steam_player_group_size"] != "3" {
		t.Fatalf("updated Steam party size = %q, want 3", steamDrv.snapshot()["steam_player_group_size"])
	}

	// Clear both providers.
	if err := mgr.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if steamDrv.clears != 1 || len(steamDrv.snapshot()) != 0 {
		t.Fatalf("Steam after Clear: clears=%d state=%v", steamDrv.clears, steamDrv.snapshot())
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	discordSrv.wait()

	// Inspect every Discord wire frame recorded: handshake, set1 (Forest 1-2), set2 (Volcano 3-4), set3 (party 3/4), clear (null), close.
	frames := discordSrv.recordedFrames()
	if len(frames) != 6 {
		t.Fatalf("expected 6 Discord IPC frames, got %d", len(frames))
	}

	type activityFrame struct {
		Args struct {
			Activity *struct {
				Details string `json:"details"`
				State   string `json:"state"`
				Party   *struct {
					ID   string `json:"id"`
					Size []int  `json:"size"`
				} `json:"party"`
			} `json:"activity"`
		} `json:"args"`
	}

	var f1, f2, f3, fClear activityFrame
	if err := json.Unmarshal(frames[1].Payload, &f1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[2].Payload, &f2); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[3].Payload, &f3); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[4].Payload, &fClear); err != nil {
		t.Fatal(err)
	}

	if f1.Args.Activity.Details != "Co-op" || f1.Args.Activity.State != "Forest 1-2" {
		t.Fatalf("Discord frame 1 = %+v", f1.Args.Activity)
	}
	if f2.Args.Activity.Details != "Co-op" || f2.Args.Activity.State != "Volcano 3-4" {
		t.Fatalf("Discord frame 2 after SetLevel = %+v", f2.Args.Activity)
	}
	if f2.Args.Activity.Party == nil || f2.Args.Activity.Party.ID != "party-77" || f2.Args.Activity.Party.Size[0] != 2 {
		t.Fatalf("Discord frame 2 lost party = %+v", f2.Args.Activity.Party)
	}
	if f3.Args.Activity.Party == nil || f3.Args.Activity.Party.Size[0] != 3 || f3.Args.Activity.Party.Size[1] != 4 {
		t.Fatalf("Discord frame 3 party = %+v, want [3 4]", f3.Args.Activity.Party)
	}
	if fClear.Args.Activity != nil {
		t.Fatalf("Discord clear frame expected nil activity, got %+v", fClear.Args.Activity)
	}
}

func TestManagerDeduplication(t *testing.T) {
	p := &stubProvider{name: "stub"}
	mgr := presence.NewManager(presence.ManagerConfig{}, p)
	defer mgr.Close()

	ctx := context.Background()
	custom := map[string]string{"steam_display": "#InLevel"}
	act := presence.Activity{Mode: "Story", Level: "1-1", Custom: custom}

	if err := mgr.Update(ctx, act); err != nil {
		t.Fatal(err)
	}
	// Mutating caller's map must not corrupt Manager's stored copy.
	custom["steam_display"] = "#Mutated"

	// Identical update is deduplicated.
	if err := mgr.Update(ctx, presence.Activity{
		Mode:   "Story",
		Level:  "1-1",
		Custom: map[string]string{"steam_display": "#InLevel"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SetLevel(ctx, "1-1"); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 1 {
		t.Fatalf("expected 1 Set call after duplicate updates, got %d", len(p.sets))
	}

	// Clearing once works; clearing again is deduplicated.
	if err := mgr.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if p.clears != 1 {
		t.Fatalf("expected 1 Clear call, got %d", p.clears)
	}
}

func TestManagerRateLimitingAndCoalescing(t *testing.T) {
	now := time.Unix(1000, 0)
	p := &stubProvider{name: "stub"}
	mgr := presence.NewManager(presence.ManagerConfig{
		MinInterval: 4 * time.Second,
		Now:         func() time.Time { return now },
	}, p)
	defer mgr.Close()

	ctx := context.Background()

	// t=0: first update goes through immediately.
	if err := mgr.SetLevel(ctx, "1-1"); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 1 || p.sets[0].Level != "1-1" || mgr.Pending() {
		t.Fatalf("t=0: sets=%v pending=%v", p.sets, mgr.Pending())
	}

	// t=1s, 2s: rapid level transitions are coalesced without hitting provider.
	now = now.Add(time.Second)
	if err := mgr.SetLevel(ctx, "1-2"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if err := mgr.SetLevel(ctx, "1-3"); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 1 || !mgr.Pending() {
		t.Fatalf("t=2s: sets=%d pending=%v", len(p.sets), mgr.Pending())
	}

	// Activity() reflects latest desired level even while pending.
	cur, active := mgr.Activity()
	if !active || cur.Level != "1-3" {
		t.Fatalf("Activity() = (%+v, %v), want Level 1-3", cur, active)
	}

	// t=3s: Poll still within MinInterval -> no push.
	now = now.Add(time.Second)
	if err := mgr.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 1 || !mgr.Pending() {
		t.Fatalf("t=3s: expected still pending, sets=%d", len(p.sets))
	}

	// t=4s: Poll flushes the coalesced "1-3" ("1-2" skipped).
	now = now.Add(time.Second)
	if err := mgr.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 2 || p.sets[1].Level != "1-3" || mgr.Pending() {
		t.Fatalf("t=4s: sets=%+v pending=%v", p.sets, mgr.Pending())
	}

	// t=5s: another update queued, then Flush forces it immediately.
	now = now.Add(time.Second)
	if err := mgr.SetLevel(ctx, "1-4"); err != nil {
		t.Fatal(err)
	}
	if !mgr.Pending() {
		t.Fatal("expected pending before Flush")
	}
	if err := mgr.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.sets) != 3 || p.sets[2].Level != "1-4" || mgr.Pending() {
		t.Fatalf("after Flush: sets=%+v pending=%v", p.sets, mgr.Pending())
	}

	// Returning to the already-sent activity within MinInterval clears pending.
	now = now.Add(time.Second)
	_ = mgr.SetLevel(ctx, "1-5")
	if !mgr.Pending() {
		t.Fatal("expected pending for 1-5")
	}
	_ = mgr.SetLevel(ctx, "1-4")
	if mgr.Pending() {
		t.Fatal("expected pending cleared when reverting to already-sent state")
	}
}

func TestManagerPartialFailureAndGracefulDegradation(t *testing.T) {
	ctx := context.Background()

	// 1. One provider unavailable, one succeeds -> returns nil and invokes OnError.
	steam := &stubProvider{name: "steam"}
	discord := &stubProvider{name: "discord", setErr: presence.ErrUnavailable}
	var errLog []string
	mgr := presence.NewManager(presence.ManagerConfig{
		OnError: func(provider string, err error) {
			errLog = append(errLog, provider+":"+err.Error())
		},
	}, steam, discord)

	if err := mgr.SetLevel(ctx, "Forest 1-1"); err != nil {
		t.Fatalf("partial unavailable should degrade gracefully, got %v", err)
	}
	if len(steam.sets) != 1 || steam.sets[0].Level != "Forest 1-1" {
		t.Fatalf("healthy provider did not receive update: %+v", steam.sets)
	}
	if len(errLog) != 1 {
		t.Fatalf("expected 1 OnError callback, got %v", errLog)
	}
	_ = mgr.Close()

	// 2. All providers unavailable -> returns ErrUnavailable.
	allUnavail := presence.NewManager(
		presence.ManagerConfig{},
		&stubProvider{name: "steam", setErr: presence.ErrUnavailable},
		&stubProvider{name: "discord", setErr: presence.ErrUnavailable},
	)
	if err := allUnavail.SetLevel(ctx, "Forest 1-1"); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("all unavailable: got %v, want ErrUnavailable", err)
	}
	_ = allUnavail.Close()

	// 3. Zero providers -> returns ErrUnavailable.
	empty := presence.NewManager(presence.ManagerConfig{})
	if err := empty.SetLevel(ctx, "Forest 1-1"); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("zero providers: got %v, want ErrUnavailable", err)
	}
	_ = empty.Close()

	// 4. Real (non-unavailable) error does not stop other providers and is returned.
	boom := errors.New("boom")
	okProv := &stubProvider{name: "discord"}
	failProv := &stubProvider{name: "steam", setErr: boom}
	partial := presence.NewManager(presence.ManagerConfig{}, failProv, okProv)
	if err := partial.SetLevel(ctx, "Stage 1"); !errors.Is(err, boom) {
		t.Fatalf("real error: got %v, want boom", err)
	}
	if len(okProv.sets) != 1 {
		t.Fatalf("healthy provider should still update when another fails")
	}

	// Clearing the error allows retrying the exact same activity (not swallowed by dedupe).
	failProv.setErr = nil
	if err := partial.SetLevel(ctx, "Stage 1"); err != nil {
		t.Fatalf("retry after error: %v", err)
	}
	if len(failProv.sets) != 1 {
		t.Fatalf("expected failed provider to receive retry")
	}

	// Closed manager rejects all operations with ErrClosed.
	_ = partial.Close()
	if err := partial.SetLevel(ctx, "Stage 2"); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("SetLevel after Close = %v, want ErrClosed", err)
	}
	if err := partial.SetMode(ctx, "Ranked"); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("SetMode after Close = %v, want ErrClosed", err)
	}
	if err := partial.SetParty(ctx, "p", 1, 2); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("SetParty after Close = %v, want ErrClosed", err)
	}
	if err := partial.Clear(ctx); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Clear after Close = %v, want ErrClosed", err)
	}
	if err := partial.Poll(ctx); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Poll after Close = %v, want ErrClosed", err)
	}
	if err := partial.Flush(ctx); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Flush after Close = %v, want ErrClosed", err)
	}
}
