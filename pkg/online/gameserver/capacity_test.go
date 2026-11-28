package gameserver

import (
	"errors"
	"testing"
)

func TestRoomsCapacityRefusesNewRoomsAndMembers(t *testing.T) {
	rs := NewRooms()
	rs.SetCapacity(Capacity{MaxRooms: 1, MaxMembers: 1}, nil)
	r, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Create(2, false); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("second create = %v", err)
	}
	if _, err := rs.Join(r.ID(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(r.ID(), 2); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("second join = %v", err)
	}
	if !rs.Leave(r.ID(), 1) {
		t.Fatal("leave failed")
	}
	if _, err := rs.Join(r.ID(), 2); err != nil {
		t.Fatal(err)
	}
}

func TestRoomsBusyCallbackRefusesOnlyNewAdmissions(t *testing.T) {
	rs := NewRooms()
	busy := true
	rs.SetCapacity(Capacity{}, func() bool { return busy })
	if _, err := rs.Create(2, false); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("create = %v", err)
	}
	busy = false
	r, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(r.ID(), 1); err != nil {
		t.Fatal(err)
	}
	busy = true
	if _, err := rs.Join(r.ID(), 2); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("join = %v", err)
	}
}
