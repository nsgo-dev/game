package social

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

func stringID(n uint64) string {
	const d = "0123456789abcdefghijklmnopqrstuvwxyz"
	s := ""
	for n > 0 {
		s = string(d[n%36]) + s
		n /= 36
	}
	return s
}

type ChannelKind string

const (
	RoomChannel  ChannelKind = "room"
	GroupChannel ChannelKind = "group"
	DMChannel    ChannelKind = "dm"
)

type Message struct {
	ID, Channel, Sender string
	Content             json.RawMessage
	Created, Updated    time.Time
	Deleted             bool
}
type Chat struct {
	messages   map[string][]Message
	next       uint64
	BeforeSend func(Message) (Message, error)
	MaxBytes   int
	Retention  time.Duration
	Now        func() time.Time
	mu         sync.RWMutex
	members    map[string]map[string]struct{}
}

func NewChat() *Chat {
	return &Chat{messages: map[string][]Message{}, members: map[string]map[string]struct{}{}, MaxBytes: 4096}
}
func (c *Chat) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func (c *Chat) Join(ch, user string) {
	if c == nil || ch == "" || user == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.members[ch] == nil {
		c.members[ch] = map[string]struct{}{}
	}
	c.members[ch][user] = struct{}{}
}
func (c *Chat) Leave(ch, user string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.members[ch], user)
}
func (c *Chat) Members(ch string) []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.members[ch]))
	for u := range c.members[ch] {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
func (c *Chat) Sweep(now time.Time) int {
	if c == nil || c.Retention <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for ch, xs := range c.messages {
		keep := xs[:0]
		for _, m := range xs {
			if now.Sub(m.Created) >= c.Retention {
				n++
				continue
			}
			keep = append(keep, m)
		}
		c.messages[ch] = keep
	}
	return n
}
func ChannelID(k ChannelKind, a, b string) string {
	if k == DMChannel && b < a {
		a, b = b, a
	}
	if b == "" {
		return string(k) + ":" + a
	}
	return string(k) + ":" + a + ":" + b
}
func (c *Chat) Send(ch, s string, v json.RawMessage) (Message, error) {
	if c == nil {
		return Message{}, errors.New("social: nil chat")
	}
	if len(v) > c.MaxBytes {
		return Message{}, errors.New("social: message too large")
	}
	c.mu.Lock()
	c.next++
	m := Message{ID: stringID(c.next), Channel: ch, Sender: s, Content: append(json.RawMessage(nil), v...), Created: c.now()}
	m.Updated = m.Created
	hook := c.BeforeSend
	c.mu.Unlock()
	if hook != nil {
		var e error
		m, e = hook(m)
		if e != nil {
			return Message{}, e
		}
	}
	c.mu.Lock()
	c.messages[ch] = append(c.messages[ch], m)
	c.mu.Unlock()
	return m, nil
}
func (c *Chat) History(ch string, limit int) []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	xs := c.messages[ch]
	if limit <= 0 {
		limit = 50
	}
	if len(xs) > limit {
		xs = xs[len(xs)-limit:]
	}
	return append([]Message(nil), xs...)
}
func (c *Chat) HistoryBefore(ch, before string, limit int) []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	xs := c.messages[ch]
	end := len(xs)
	if before != "" {
		for i, m := range xs {
			if m.ID == before {
				end = i
				break
			}
		}
	}
	if limit <= 0 {
		limit = 50
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return append([]Message(nil), xs[start:end]...)
}
func (c *Chat) Edit(ch, id, s string, v json.RawMessage) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.messages[ch] {
		m := &c.messages[ch][i]
		if m.ID == id && m.Sender == s && !m.Deleted {
			m.Content = append(json.RawMessage(nil), v...)
			m.Updated = c.now()
			return true
		}
	}
	return false
}
func (c *Chat) Delete(ch, id, s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.messages[ch] {
		m := &c.messages[ch][i]
		if m.ID == id && m.Sender == s && !m.Deleted {
			m.Deleted = true
			m.Updated = c.now()
			return true
		}
	}
	return false
}
