package realtime

import "testing"

func TestPersistentOfflineAndOnlineDelivery(t *testing.T) {
	s := NewStore()
	s.SetOnline("a", false)
	n, err := s.Notify(Notification{To: "a", Code: "gift", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.History("a", 0, 10)) != 1 {
		t.Fatal("missing")
	}
	s.SetOnline("a", true)
	ch, stop := s.Subscribe("a", 1)
	defer stop()
	_, _ = s.Notify(Notification{To: "a", Code: "ping"})
	select {
	case got := <-ch:
		if got.Code != "ping" {
			t.Fatal(got.Code)
		}
	default:
		t.Fatal("not delivered")
	}
	_ = n
}
