package server_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/server"
)

func TestGuardRateLimitDisconnectsOnePeerOnly(t *testing.T) {
	g := server.NewGuard(server.Limits{PacketsPerSecond: 10, BurstPackets: 10}, server.LimitDisconnect)
	for i := 0; i < 10; i++ {
		if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 10)}); action != server.GuardAllow || err != nil {
			t.Fatalf("packet %d = %v, %v", i, action, err)
		}
	}
	if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 10)}); action != server.GuardDisconnect || err == nil {
		t.Fatalf("over-rate packet = %v, %v; want disconnect", action, err)
	}
	if action, err := g.Check(2, netplay.Packet{Payload: make([]byte, 10)}); action != server.GuardAllow || err != nil {
		t.Fatalf("other peer = %v, %v; want allowed", action, err)
	}
	if g.Stats(1).Dropped == 0 {
		t.Fatal("over-rate peer has no dropped count")
	}
}

func TestGuardRejectsOversizedBeforeBuffering(t *testing.T) {
	g := server.NewGuard(server.Limits{MaxMessageSize: 100}, server.LimitDisconnect)
	if action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 100)}); action != server.GuardAllow {
		t.Fatalf("100-byte packet = %v, %v", action, err)
	}
	action, err := g.Check(1, netplay.Packet{Payload: make([]byte, 101)})
	if action == server.GuardAllow || !errors.Is(err, server.ErrMessageTooLarge) {
		t.Fatalf("oversized packet = %v, %v", action, err)
	}
	stats := g.Stats(1)
	if stats.Bytes != 100 || stats.Dropped != 1 || stats.Violations != 1 {
		t.Fatalf("stats = %+v, want 100 bytes and one refused", stats)
	}
}

func TestGuardValidatorRejectsAndCountsPerPeer(t *testing.T) {
	g := server.NewGuard(server.Limits{}, server.LimitDisconnect)
	g.Validate = func(_ netplay.PeerID, p netplay.Packet) error {
		if string(p.Payload) == "bad" {
			return errors.New("speed hack")
		}
		return nil
	}
	if _, err := g.Check(1, netplay.Packet{Payload: []byte("bad")}); !errors.Is(err, server.ErrInvalidInput) {
		t.Fatalf("bad input = %v", err)
	}
	if action, err := g.Check(1, netplay.Packet{Payload: []byte("good")}); action != server.GuardAllow || err != nil {
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
	g := server.NewGuard(server.Limits{PacketsPerSecond: 1, BurstPackets: 1}, server.LimitThrottle)
	g.Now = func() time.Time { return now }
	if action, _ := g.Check(1, netplay.Packet{}); action != server.GuardAllow {
		t.Fatalf("first packet = %v", action)
	}
	if action, _ := g.Check(1, netplay.Packet{}); action != server.GuardThrottle {
		t.Fatalf("second packet = %v, want throttle", action)
	}
	now = now.Add(time.Second)
	if action, err := g.Check(1, netplay.Packet{}); action != server.GuardAllow || err != nil {
		t.Fatalf("packet after refill = %v, %v", action, err)
	}
}

func TestGuardPendingReliableLimit(t *testing.T) {
	g := server.NewGuard(server.Limits{MaxPendingReliable: 100}, server.LimitDisconnect)
	if err := g.ReserveReliable(1, 60); err != nil {
		t.Fatal(err)
	}
	if err := g.ReserveReliable(1, 60); !errors.Is(err, server.ErrPendingLimit) {
		t.Fatalf("second reserve = %v, want ErrPendingLimit", err)
	}
	g.ReleaseReliable(1, 60)
	if err := g.ReserveReliable(1, 60); err != nil {
		t.Fatal(err)
	}
}

func TestGuardRandomPacketsDoNotPanic(t *testing.T) {
	g := server.NewGuard(server.Limits{PacketsPerSecond: 100, BytesPerSecond: 1000, MaxMessageSize: 64}, server.LimitDrop)
	for i := 0; i < 5000; i++ {
		peer := netplay.PeerID(i%7 + 1)
		_, _ = g.Check(peer, netplay.Packet{Payload: make([]byte, (i*37)%200)})
	}
}
