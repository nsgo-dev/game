package gameserver_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/gameserver"
	"github.com/nsgo-dev/game/pkg/online/netplay"
)

func TestRoomsKeepPacketsSeparate(t *testing.T) {
	rs := gameserver.NewRooms()
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
	if err := one.Deliver(2, netplay.Packet{Payload: []byte("cross")}); !errors.Is(err, gameserver.ErrNotInRoom) {
		t.Fatalf("cross-room delivery = %v, want ErrNotInRoom", err)
	}
}

func TestRoomLateJoinGetsFullSnapshot(t *testing.T) {
	rs := gameserver.NewRooms()
	rs.Snapshot = func(r *gameserver.Room) []byte { return []byte("full-state") }
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
	rs := gameserver.NewRooms()
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
	rs := gameserver.NewRooms()
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
	if !errors.Is(err, gameserver.ErrRoomFull) {
		t.Fatalf("third join = %v, want ErrRoomFull", err)
	}
	if _, err := rs.JoinCode("WRONG", 4); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("wrong code join = %v, want ErrRoomNotFound", err)
	}
}

func TestEmptyRoomClosesAfterTimeout(t *testing.T) {
	rs := gameserver.NewRooms()
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
	if _, err := rs.Join(room.ID(), 2); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("join after close = %v, want ErrRoomNotFound", err)
	}
}

func TestNeverJoinedRoomClosesAfterTimeout(t *testing.T) {
	rs := gameserver.NewRooms()
	now := time.Unix(100, 0)
	rs.Now = func() time.Time { return now }
	rs.EmptyTimeout = time.Second
	rs.CodeGenerator = func() string { return "IDLE42" }

	pub, err := rs.Create(4, false)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := rs.Create(4, true)
	if err != nil {
		t.Fatal(err)
	}
	if closed := rs.Tick(); len(closed) != 0 {
		t.Fatalf("closed immediately: %v", closed)
	}
	now = now.Add(999 * time.Millisecond)
	if closed := rs.Tick(); len(closed) != 0 {
		t.Fatalf("closed before timeout: %v", closed)
	}
	if got := rs.List(); len(got) != 2 {
		t.Fatalf("listed rooms before timeout = %d, want 2", len(got))
	}

	now = now.Add(time.Millisecond)
	closed := rs.Tick()
	slices.Sort(closed)
	want := []uint64{pub.ID(), priv.ID()}
	if !slices.Equal(closed, want) {
		t.Fatalf("closed = %v, want %v", closed, want)
	}
	if got := rs.List(); len(got) != 0 {
		t.Fatalf("listed rooms after close = %v, want empty", got)
	}
	if _, err := rs.Join(pub.ID(), 1); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("Join(pub) after close = %v, want ErrRoomNotFound", err)
	}
	if _, err := rs.JoinCode("IDLE42", 1); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("JoinCode after close = %v, want ErrRoomNotFound", err)
	}
	// Closing released the private code so a new room can allocate it.
	recreated, err := rs.Create(4, true)
	if err != nil {
		t.Fatalf("recreate private room with released code: %v", err)
	}
	if recreated.Code() != "IDLE42" {
		t.Fatalf("recreated code = %q, want IDLE42", recreated.Code())
	}
}

func TestNeverJoinedRoomClosesImmediatelyWithZeroTimeout(t *testing.T) {
	rs := gameserver.NewRooms()
	rs.CodeGenerator = func() string { return "ZERO01" }

	room, err := rs.Create(4, true)
	if err != nil {
		t.Fatal(err)
	}
	closed := rs.Tick()
	if !slices.Equal(closed, []uint64{room.ID()}) {
		t.Fatalf("closed = %v, want [%d]", closed, room.ID())
	}
	if got := rs.List(); len(got) != 0 {
		t.Fatalf("listed rooms = %v, want empty", got)
	}
	if _, err := rs.JoinCode("ZERO01", 1); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("JoinCode after close = %v, want ErrRoomNotFound", err)
	}
}

func TestJoinedRoomCancelsIdleCloseAndReemptyingClosesAfterTimeout(t *testing.T) {
	rs := gameserver.NewRooms()
	now := time.Unix(0, 0)
	rs.Now = func() time.Time { return now }
	rs.EmptyTimeout = time.Second

	room, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(800 * time.Millisecond)
	if _, err := rs.Join(room.ID(), 1); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if closed := rs.Tick(); len(closed) != 0 {
		t.Fatalf("occupied room closed: %v", closed)
	}
	if !rs.Leave(room.ID(), 1) {
		t.Fatal("Leave(1) = false, want true")
	}
	now = now.Add(500 * time.Millisecond)
	if closed := rs.Tick(); len(closed) != 0 {
		t.Fatalf("closed before post-leave timeout: %v", closed)
	}
	// A non-member Leave must not reset the empty timer.
	now = now.Add(300 * time.Millisecond)
	if rs.Leave(room.ID(), 99) {
		t.Fatal("Leave(non-member) = true, want false")
	}
	now = now.Add(200 * time.Millisecond)
	if closed := rs.Tick(); !slices.Equal(closed, []uint64{room.ID()}) {
		t.Fatalf("closed = %v, want [%d]", closed, room.ID())
	}
}

func TestEndedEmptyRoomClosesAfterTimeout(t *testing.T) {
	rs := gameserver.NewRooms()
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
	if err := room.Start(); err != nil {
		t.Fatal(err)
	}
	room.End()
	if !rs.Leave(room.ID(), 1) {
		t.Fatal("Leave(1) = false, want true")
	}
	now = now.Add(time.Second)
	if closed := rs.Tick(); !slices.Equal(closed, []uint64{room.ID()}) {
		t.Fatalf("closed = %v, want [%d]", closed, room.ID())
	}
	if _, err := rs.Join(room.ID(), 2); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("Join after close = %v, want ErrRoomNotFound", err)
	}
	if err := room.Start(); !errors.Is(err, gameserver.ErrRoomNotFound) {
		t.Fatalf("Start after close = %v, want ErrRoomNotFound", err)
	}
}

func TestConcurrentJoinAndTickNeverStrandsPeer(t *testing.T) {
	rs := gameserver.NewRooms()
	for round := range 200 {
		room, err := rs.Create(4, true)
		if err != nil {
			t.Fatalf("round %d Create: %v", round, err)
		}
		code := room.Code()
		id := room.ID()

		start := make(chan struct{})
		var (
			errByID   error
			errByCode error
			closedIDs []uint64
			wg        sync.WaitGroup
		)
		wg.Add(4)
		go func() {
			defer wg.Done()
			<-start
			_, errByID = rs.Join(id, 1)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, errByCode = rs.JoinCode(code, 2)
		}()
		go func() {
			defer wg.Done()
			<-start
			closedIDs = rs.Tick()
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = rs.List()
		}()
		close(start)
		wg.Wait()

		tickClosed := slices.Contains(closedIDs, id)
		if tickClosed {
			if !errors.Is(errByID, gameserver.ErrRoomNotFound) {
				t.Fatalf("round %d: room closed by Tick, but Join returned %v", round, errByID)
			}
			if !errors.Is(errByCode, gameserver.ErrRoomNotFound) {
				t.Fatalf("round %d: room closed by Tick, but JoinCode returned %v", round, errByCode)
			}
			if members := room.Members(); len(members) != 0 {
				t.Fatalf("round %d: closed room still has members %v", round, members)
			}
			if _, err := rs.JoinCode(code, 3); !errors.Is(err, gameserver.ErrRoomNotFound) {
				t.Fatalf("round %d: JoinCode after close = %v, want ErrRoomNotFound", round, err)
			}
			continue
		}

		if errByID != nil && errByCode != nil {
			t.Fatalf("round %d: Tick did not close room, yet both joins failed (%v, %v)", round, errByID, errByCode)
		}
		if errByID == nil && !rs.Leave(id, 1) {
			t.Fatalf("round %d: peer 1 admitted by Join was stranded (Leave returned false)", round)
		}
		if errByCode == nil && !rs.Leave(id, 2) {
			t.Fatalf("round %d: peer 2 admitted by JoinCode was stranded (Leave returned false)", round)
		}
		if cleanup := rs.Tick(); !slices.Contains(cleanup, id) {
			t.Fatalf("round %d: emptied room %d did not close on follow-up Tick (%v)", round, id, cleanup)
		}
	}
}
