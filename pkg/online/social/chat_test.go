package social

import (
	"encoding/json"
	"testing"
)

func TestChatLifecycle(t *testing.T) {
	c := NewChat()
	m, e := c.Send(ChannelID(DMChannel, "b", "a"), "a", json.RawMessage(`{"x":1}`))
	if e != nil || len(c.History(m.Channel, 1)) != 1 || !c.Edit(m.Channel, m.ID, "a", nil) || !c.Delete(m.Channel, m.ID, "a") {
		t.Fatal("chat lifecycle failed")
	}
}
