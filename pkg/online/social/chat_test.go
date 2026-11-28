package social

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestChatLifecycle(t *testing.T) {
	c := NewChat()
	m, e := c.Send(ChannelID(DMChannel, "b", "a"), "a", json.RawMessage(`{"x":1}`))
	if e != nil || len(c.History(m.Channel, 1)) != 1 || !c.Edit(m.Channel, m.ID, "a", nil) || !c.Delete(m.Channel, m.ID, "a") {
		t.Fatal("chat lifecycle failed")
	}
}

func TestChatMembershipHistoryAndRetention(t *testing.T) {
	c := NewChat()
	now := time.Unix(100, 0)
	c.Now = func() time.Time { return now }
	c.Retention = time.Second
	c.Join("room:x", "b")
	c.Join("room:x", "a")
	if got := c.Members("room:x"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("members = %v", got)
	}
	m1, _ := c.Send("room:x", "a", json.RawMessage(`1`))
	now = now.Add(500 * time.Millisecond)
	_, _ = c.Send("room:x", "b", json.RawMessage(`2`))
	if got := c.HistoryBefore("room:x", m1.ID, 10); len(got) != 0 {
		t.Fatalf("before first = %v", got)
	}
	now = now.Add(time.Second)
	if c.Sweep(now) != 2 || len(c.History("room:x", 10)) != 0 {
		t.Fatal("retention sweep failed")
	}
}
