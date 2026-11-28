package gameserver

import (
	"context"
	"errors"
	"github.com/nsgo-dev/game/pkg/online/netplay"
	"reflect"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu         sync.Mutex
	attempts   int
	joined     []netplay.PeerID
	left       []LeaveReason
	terminated time.Duration
	signal     []byte
}

func (h *testHandler) JoinAttempt(_ netplay.PeerID, account string, meta []byte) (bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attempts++
	if account == "banned" {
		return false, "banned"
	}
	h.signal = append([]byte(account), meta...)
	return true, ""
}
func (h *testHandler) Joined(p netplay.PeerID) {
	h.mu.Lock()
	h.joined = append(h.joined, p)
	h.mu.Unlock()
}
func (h *testHandler) Left(_ netplay.PeerID, r LeaveReason) {
	h.mu.Lock()
	h.left = append(h.left, r)
	h.mu.Unlock()
}
func (h *testHandler) Signal(data []byte) ([]byte, error) { return append([]byte("ok:"), data...), nil }
func (h *testHandler) Terminate(d time.Duration)          { h.terminated = d }

func TestMatchHandlerApprovalLifecycleSignalAndLabel(t *testing.T) {
	rs := NewRooms()
	room, err := rs.Create(2, false)
	if err != nil {
		t.Fatal(err)
	}
	h := &testHandler{}
	room.SetHandler(h)
	room.SetLabel(map[string]string{"mode": "duel"})
	if _, err := rs.JoinWithMeta(room.ID(), 9, "banned", []byte("x")); err == nil || !errors.Is(err, errors.New("gameserver: join rejected: banned")) && err.Error() != "gameserver: join rejected: banned" {
		t.Fatalf("reject = %v", err)
	}
	if _, err := rs.JoinWithMeta(room.ID(), 1, "ok", []byte("meta")); err != nil {
		t.Fatal(err)
	}
	if got := room.Members(); !reflect.DeepEqual(got, []netplay.PeerID{1}) {
		t.Fatalf("members = %v", got)
	}
	if got := rs.List()[0].Label; got["mode"] != "duel" {
		t.Fatalf("label = %v", got)
	}
	reply, err := room.Signal(context.Background(), []byte("ping"))
	if err != nil || string(reply) != "ok:ping" {
		t.Fatalf("signal = %q, %v", reply, err)
	}
	if !rs.LeaveWithReason(room.ID(), 1, LeaveDropped) {
		t.Fatal("leave failed")
	}
	if len(h.left) != 1 || h.left[0] != LeaveDropped {
		t.Fatalf("left = %v", h.left)
	}
	room.Terminate(2 * time.Second)
	if h.terminated != 2*time.Second {
		t.Fatalf("terminate = %v", h.terminated)
	}
}
