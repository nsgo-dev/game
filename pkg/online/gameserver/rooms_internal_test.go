package gameserver

import (
	"errors"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

func TestJoinAfterTickClosedReturnsNotFound(t *testing.T) {
	t.Run("waiting room", func(t *testing.T) {
		rs := NewRooms()
		room, err := rs.Create(2, true)
		if err != nil {
			t.Fatal(err)
		}
		if closed := rs.Tick(); !slices.Equal(closed, []uint64{room.ID()}) {
			t.Fatalf("closed = %v, want [%d]", closed, room.ID())
		}
		if _, err := rs.join(room, netplay.PeerID(1)); !errors.Is(err, ErrRoomNotFound) {
			t.Fatalf("join on closed room = %v, want ErrRoomNotFound", err)
		}
		if got := room.Members(); len(got) != 0 {
			t.Fatalf("closed room members = %v, want empty", got)
		}
		if rs.Leave(room.ID(), 1) {
			t.Fatal("Leave on closed room = true, want false")
		}
	})

	t.Run("ended room closed by tick", func(t *testing.T) {
		rs := NewRooms()
		room, err := rs.Create(2, false)
		if err != nil {
			t.Fatal(err)
		}
		room.End()
		if closed := rs.Tick(); !slices.Equal(closed, []uint64{room.ID()}) {
			t.Fatalf("closed = %v, want [%d]", closed, room.ID())
		}
		if _, err := rs.join(room, netplay.PeerID(1)); !errors.Is(err, ErrRoomNotFound) {
			t.Fatalf("join on ended+closed room = %v, want ErrRoomNotFound", err)
		}
	})
}

func TestJoinInterleavedWithTickCloseReturnsNotFound(t *testing.T) {
	cases := []struct {
		name string
		join func(rs *Rooms, r *Room, peer netplay.PeerID) (Join, error)
	}{
		{
			name: "Join",
			join: func(rs *Rooms, r *Room, peer netplay.PeerID) (Join, error) {
				return rs.Join(r.ID(), peer)
			},
		},
		{
			name: "JoinCode",
			join: func(rs *Rooms, r *Room, peer netplay.PeerID) (Join, error) {
				return rs.JoinCode(r.Code(), peer)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs := NewRooms()
			rs.CodeGenerator = func() string { return "RACE99" }

			entered := make(chan struct{})
			proceed := make(chan struct{})
			rs.beforeJoin = func(*Room) {
				close(entered)
				<-proceed
			}

			room, err := rs.Create(2, true)
			if err != nil {
				t.Fatal(err)
			}

			joinErrCh := make(chan error, 1)
			go func() {
				_, err := tc.join(rs, room, 7)
				joinErrCh <- err
			}()

			// Wait until Join/JoinCode has looked up room under rs.mu and released rs.mu.
			<-entered
			closed := rs.Tick()
			if !slices.Equal(closed, []uint64{room.ID()}) {
				t.Fatalf("Tick closed = %v, want [%d]", closed, room.ID())
			}
			close(proceed)

			if err := <-joinErrCh; !errors.Is(err, ErrRoomNotFound) {
				t.Fatalf("%s after concurrent Tick close = %v, want ErrRoomNotFound", tc.name, err)
			}
			if got := room.Members(); len(got) != 0 {
				t.Fatalf("closed room members = %v, want empty", got)
			}
			if got := rs.List(); len(got) != 0 {
				t.Fatalf("List after close = %v, want empty", got)
			}
		})
	}
}
