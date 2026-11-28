package gameserver_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
	"github.com/nsgo-dev/game/pkg/online/netplay"
)

func TestGuardRateLimitDisconnectsOnePeerOnly(t *testing.T) {
	g := gameserver.NewGuard(gameserver.Limits{PacketsPerSecond: 10, BurstPackets: 10}, gameserver.LimitDisconnect)
	for i := 0; i < 10; i++ {
		if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 10)}); action != gameserver.GuardAllow || err != nil {
			t.Fatalf("packet %d = %v, %v", i, action, err)
		}
	}
	if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 10)}); action != gameserver.GuardDisconnect || err == nil {
		t.Fatalf("over-rate packet = %v, %v; want disconnect", action, err)
	}
	if action, err := g.Check(2, netplay.Packet{Payload: make([]byte, 10)}); action != gameserver.GuardAllow || err != nil {
		t.Fatalf("other peer = %v, %v; want allowed", action, err)
	}
	if g.Stats(1).Dropped == 0 {
		t.Fatal("over-rate peer has no dropped count")
	}
}

func TestGuardRejectsOversizedBeforeBuffering(t *testing.T) {
	g := gameserver.NewGuard(gameserver.Limits{MaxMessageSize: 100}, gameserver.LimitDisconnect)
	if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 100)}); action != gameserver.GuardAllow {
		t.Fatalf("100-byte packet = %v, %v", action, err)
	}
	action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 101)})
	if action == gameserver.GuardAllow || !errors.Is(err, gameserver.ErrMessageTooLarge) {
		t.Fatalf("oversized packet = %v, %v", action, err)
	}
	stats := g.Stats(1)
	if stats.Bytes != 100 || stats.Dropped != 1 || stats.Violations != 1 {
		t.Fatalf("stats = %+v, want 100 bytes and one refused", stats)
	}
}

func TestGuardValidatorRejectsAndCountsPerPeer(t *testing.T) {
	g := gameserver.NewGuard(gameserver.Limits{}, gameserver.LimitDisconnect)
	g.Validate = func(_ netplay.PeerID, p netplay.Packet) error {
		if string(p.Payload) == "bad" {
			return errors.New("speed hack")
		}
		return nil
	}
	if _, err := g.Check(1, netplay.Packet{Payload: []byte("bad")}); !errors.Is(err, gameserver.ErrInvalidInput) {
		t.Fatalf("bad input = %v", err)
	}
	if action, err := g.Check(1, netplay.Packet{Payload: []byte("good")}); action != gameserver.GuardAllow || err != nil {
		t.Fatalf("good input = %v, %v", action, err)
	}
	if g.Stats(1).Violations != 1 {
		t.Fatalf("violations = %d, want 1", g.Stats(1).Violations)
	}
	if g.Stats(2).Violations != 0 {
		t.Fatal("violation leaked to another peer")
	}
}

func TestGuardThrottleRefills(t *testing.T) {
	now := time.Unix(0, 0)
	g := gameserver.NewGuard(gameserver.Limits{PacketsPerSecond: 1, BurstPackets: 1}, gameserver.LimitThrottle)
	g.Now = func() time.Time { return now }
	if action, _ := g.Check(1, netplay.Packet{}); action != gameserver.GuardAllow {
		t.Fatalf("first packet = %v", action)
	}
	if action, _ := g.Check(1, netplay.Packet{}); action != gameserver.GuardThrottle {
		t.Fatalf("second packet = %v, want throttle", action)
	}
	now = now.Add(time.Second)
	if action, err := g.Check(1, netplay.Packet{}); action != gameserver.GuardAllow || err != nil {
		t.Fatalf("packet after refill = %v, %v", action, err)
	}
}

func TestGuardPendingReliableLimit(t *testing.T) {
	g := gameserver.NewGuard(gameserver.Limits{MaxPendingReliable: 100}, gameserver.LimitDisconnect)
	if err := g.ReserveReliable(1, 60); err != nil {
		t.Fatal(err)
	}
	if err := g.ReserveReliable(1, 60); !errors.Is(err, gameserver.ErrPendingLimit) {
		t.Fatalf("second reserve = %v, want ErrPendingLimit", err)
	}
	g.ReleaseReliable(1, 60)
	if err := g.ReserveReliable(1, 60); err != nil {
		t.Fatal(err)
	}
}

func TestGuardRandomPacketsDoNotPanic(t *testing.T) {
	g := gameserver.NewGuard(gameserver.Limits{PacketsPerSecond: 100, BytesPerSecond: 1000, MaxMessageSize: 64}, gameserver.LimitDrop)
	for i := 0; i < 5000; i++ {
		peer := netplay.PeerID(i%7 + 1)
		_, _ = g.Check(peer, netplay.Packet{Payload: make([]byte, (i*37)%200)})
	}
}
