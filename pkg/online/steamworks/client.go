// Package steamworks owns the process-wide Steam lifecycle shared by online
// features. Native bindings can provide a Driver without exposing Steam's C ABI
// to presence, Workshop or game code.
package steamworks

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var ErrUnavailable = errors.New("steamworks unavailable")

type Driver interface {
	Initialize(appID uint32) error
	Shutdown()
	RunCallbacks()
}
type EventKind uint8

const (
	EventOverlay EventKind = iota
	EventJoinRequested
	EventShutdown
)

type Event struct {
	Kind    EventKind
	Active  bool
	Connect string
}
type Options struct {
	AppID       uint32
	Driver      Driver
	ConnectArgs []string
}
type Client struct {
	driver  Driver
	mu      sync.Mutex
	closed  bool
	subs    map[EventKind][]func(Event)
	pending []Event
	appID   uint32
}

func Open(opts Options) (*Client, error) {
	if opts.Driver == nil {
		return nil, ErrUnavailable
	}
	if opts.AppID == 0 {
		return nil, fmt.Errorf("steamworks: app ID must be positive")
	}
	if err := opts.Driver.Initialize(opts.AppID); err != nil {
		return nil, fmt.Errorf("steamworks: initialize: %w", err)
	}
	c := &Client{driver: opts.Driver, appID: opts.AppID, subs: make(map[EventKind][]func(Event))}
	if connect := ConnectArgument(opts.ConnectArgs); connect != "" {
		c.pending = append(c.pending, Event{Kind: EventJoinRequested, Connect: connect})
	}
	return c, nil
}

func (c *Client) AppID() uint32 {
	if c == nil {
		return 0
	}
	return c.appID
}
func (c *Client) On(kind EventKind, fn func(Event)) {
	if c == nil || fn == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.subs[kind] = append(c.subs[kind], fn)
	pending := c.pending[:0]
	var replay []Event
	for _, ev := range c.pending {
		if ev.Kind == kind {
			replay = append(replay, ev)
		} else {
			pending = append(pending, ev)
		}
	}
	c.pending = pending
	c.mu.Unlock()
	for _, ev := range replay {
		fn(ev)
	}
}
func (c *Client) Emit(ev Event) {
	if c == nil {
		return
	}
	c.mu.Lock()
	fns := append([]func(Event){}, c.subs[ev.Kind]...)
	c.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}
func (c *Client) Pump() {
	if c == nil {
		return
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		c.driver.RunCallbacks()
	}
}
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	d := c.driver
	c.mu.Unlock()
	d.Shutdown()
	return nil
}

func ConnectArgument(args []string) string {
	for i, a := range args {
		if a == "+connect" && i+1 < len(args) && validConnect(args[i+1]) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "+connect=") && validConnect(strings.TrimPrefix(a, "+connect=")) {
			return strings.TrimPrefix(a, "+connect=")
		}
	}
	return ""
}
func validConnect(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}
