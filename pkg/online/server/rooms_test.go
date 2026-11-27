package server_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/server"
)

func TestRoomsKeepPacketsSeparate(t *testing.T) {
	rs := server.NewRooms()
	one, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	two, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(one.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(two.ID(), 2); err != nil {
		t.Fatal(err)
	}
	if err := one.Deliver(1, netplay.Packet{Payload: []byte("one")}); err != nil {
		t.Fatal(err)
	}
	if err := two.Deliver(2, netplay.Packet{Payload: []byte("two")}); err != nil {
		t.Fatal(err)
	}
	if p, ok := one.Recv(1); !ok || string(p.Payload) != "one" {
		t.Fatalf("room one recv = %q, %v", p.Payload, ok)
	}
	if p, ok := two.Recv(2); !ok || string(p.Payload) != "two" {
		t.Fatalf("room two recv = %q, %v", p.Payload, ok)
	}
	if _, ok := one.Recv(2); ok {
		t.Fatal("room one saw room two's peer")
	}
	if err := one.Deliver(2, netplay.Packet{Payload: []byte("cross")}); !errors.Is(err, server.ErrNotInRoom) {
		t.Fatalf("cross-room delivery = %v, want ErrNotInRoom", err)
	}
}

func TestRoomLateJoinGetsFullSnapshot(t *testing.T) {
	rs := server.NewRooms()
	rs.Snapshot = func(r *server.Room) []byte { return []byte("full-state") }
	room, err := rs.Create(4, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(room.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	room.Advance()
	room.Advance()
	join, err := rs.Join(room.ID(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if join.Epoch != 1 || join.Tick != 2 {
		t.Fatalf("late join epoch/tick = %d/%d, want 1/2", join.Epoch, join.Tick)
	}
	if string(join.Snapshot) != "full-state" {
		t.Fatalf("late join snapshot = %q", join.Snapshot)
	}
	// A delta delivered after the snapshot reaches the joiner separately.
	if err := room.Deliver(1, netplay.Packet{Payload: []byte("delta")}); err != nil {
		t.Fatal(err)
	}
	if p, ok := room.Recv(2); ok && string(p.Payload) == "delta" {
		t.Fatal("delta was attributed to the late joiner")
	}
	if p, ok := room.Recv(1); !ok || string(p.Payload) != "delta" {
		t.Fatal("delta did not reach its sender's inbox")
	}
}

func TestRoomNewMatchResetsEpochAndTick(t *testing.T) {
	rs := server.NewRooms()
	room, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(room.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		room.Advance()
	}
	if room.Epoch() != 1 || room.Tick() != 5 {
		t.Fatalf("first match epoch/tick = %d/%d, want 1/5", room.Epoch(), room.Tick())
	}
	room.End()
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	if room.Epoch() != 2 || room.Tick() != 0 {
		t.Fatalf("next match epoch/tick = %d/%d, want 2/0", room.Epoch(), room.Tick())
	}
}

func TestPrivateRoomCodeAndCapacity(t *testing.T) {
	rs := server.NewRooms()
	rs.CodeGenerator = func() string { return "FRIEND" }
	room, err := rs.Create(2, true)
	if err != nil {
		t.Fatal(err)
	}
	if room.Code() != "FRIEND" {
		t.Fatalf("code = %q", room.Code())
	}
	if _, err := rs.JoinCode("FRIEND", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.JoinCode("FRIEND", 2); err != nil {
		t.Fatal(err)
	}
	_, err = rs.JoinCode("FRIEND", 3)
	if !errors.Is(err, server.ErrRoomFull) {
		t.Fatalf("third join = %v, want ErrRoomFull", err)
	}
	if _, err := rs.JoinCode("WRONG", 4); !errors.Is(err, server.ErrRoomNotFound) {
		t.Fatalf("wrong code join = %v, want ErrRoomNotFound", err)
	}
}

func TestEmptyRoomClosesAfterTimeout(t *testing.T) {
	rs := server.NewRooms()
	now := time.Unix(0, 0)
	rs.Now = func() time.Time { return now }
	rs.EmptyTimeout = time.Second
	room, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(room.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if !rs.Leave(room.ID(), 1) {
		t.Fatal("leave did not remove the member")
	}
	if closed := rs.Tick(); len(closed) != 0 {
		t.Fatalf("closed before timeout: %v", closed)
	}
	now = now.Add(2 * time.Second)
	if closed := rs.Tick(); len(closed) != 1 || closed[0] != room.ID() {
		t.Fatalf("closed = %v, want room %d", closed, room.ID())
	}
	if _, err := rs.Join(room.ID(), 2); !errors.Is(err, server.ErrRoomNotFound) {
		t.Fatalf("join after close = %v, want ErrRoomNotFound", err)
	}
}
