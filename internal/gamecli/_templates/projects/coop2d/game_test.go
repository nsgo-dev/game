package main

import (
	"github.com/nsgo-dev/nsgo/nsgotest"
	"testing"
	"time"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }
func TestRoomJoinLeaveRejoinAndRevive(t *testing.T) {
	cfg := CoopConfig{Version: 1, TickRate: 20, MaxPlayers: 4, RejoinGrace: 30 * time.Second, ReviveTicks: 2, RespawnTicks: 3}
	s, _ := NewServer(cfg)
	r, err := s.Join("ABC123", "a")
	if err != nil || r == nil {
		t.Fatal(err)
	}
	if _, err = s.Join("ABC123", "b"); err != nil {
		t.Fatal(err)
	}
	s.Leave("ABC123", "b")
	if _, ok := s.Rejoin("ABC123", "b"); !ok {
		t.Fatal("rejoin failed")
	}
	r.Players["a"].Downed = true
	s.Tick("ABC123")
	s.Tick("ABC123")
	if r.Players["a"].Downed {
		t.Fatal("teammate did not revive player")
	}
}
func TestSoloRespawn(t *testing.T) {
	cfg := CoopConfig{Version: 1, TickRate: 20, MaxPlayers: 4, RejoinGrace: time.Second, ReviveTicks: 2, RespawnTicks: 2}
	s, _ := NewServer(cfg)
	r, _ := s.Join("ROOM", "a")
	s.Leave("ROOM", "a")
	s.Tick("ROOM")
	s.Tick("ROOM")
	if !r.Players["a"].Connected {
		t.Fatal("solo player did not respawn")
	}
}
