package gameserver

import (
	"errors"
	"testing"
	"time"
)

func TestRoomReservationsBlockAndExpire(t *testing.T) {
	rs := NewRooms()
	now := time.Now()
	rs.Now = func() time.Time { return now }
	r, _ := rs.Create(1, false)
	res, err := rs.Reserve(r.ID(), "a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Join(r.ID(), 1); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("join = %v", err)
	}
	now = now.Add(2 * time.Second)
	if _, err := rs.Join(r.ID(), 1); err != nil {
		t.Fatal(err)
	}
	_ = res
}
