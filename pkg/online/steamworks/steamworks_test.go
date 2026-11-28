package steamworks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/nsgo/core"
)

// testRoot is a root scope whose display frames the test runs by hand, so callbacks are pumped only inside frame().
type testRoot struct {
	root   *core.Scope
	wanted bool
}

func newTestRoot(t *testing.T) *testRoot {
	t.Helper()
	r := &testRoot{}
	r.root = core.NewRoot(core.RootOptions{RequestFrame: func() { r.wanted = true }})
	t.Cleanup(func() {
		if !r.root.Disposed() {
			r.root.Dispose()
		}
	})
	return r
}

func (r *testRoot) frame() {
	if r.wanted {
		r.wanted = false
		r.root.RunFrame()
	}
}

func openFake(t *testing.T, cx *core.Scope, drv *FakeDriver, args ...string) *Client {
	t.Helper()
	if args == nil {
		args = []string{}
	}
	c, err := Open(cx, Options{AppID: 480, Driver: drv, Args: args})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestOpenInitializesOncePerProcessAndShutsDownOnLastDispose(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	a := r.root.Child()
	b := r.root.Child()

	ca := openFake(t, a, drv)
	cb := openFake(t, b, drv)
	if ca != cb {
		t.Fatal("second Open returned a different client")
	}
	if got := drv.InitCount(); got != 1 {
		t.Fatalf("Init calls = %d, want 1", got)
	}
	if Current() != ca {
		t.Fatal("Current does not return the open client")
	}

	a.Dispose()
	if ca.Closed() || drv.ShutdownCount() != 0 {
		t.Fatalf("client closed after first of two scopes disposed (shutdowns=%d)", drv.ShutdownCount())
	}
	// The pump moves to the remaining scope.
	before := drv.RunFrameCount()
	r.frame()
	if drv.RunFrameCount() != before+1 {
		t.Fatalf("RunFrame calls after handoff = %d, want %d", drv.RunFrameCount(), before+1)
	}

	b.Dispose()
	if !ca.Closed() {
		t.Fatal("client still open after last scope disposed")
	}
	if got := drv.ShutdownCount(); got != 1 {
		t.Fatalf("Shutdown calls = %d, want 1", got)
	}
	if Current() != nil {
		t.Fatal("Current is not nil after shutdown")
	}
	r.frame()
	if drv.RunFrameCount() != before+1 {
		t.Fatal("client pumped after shutdown")
	}
}

func TestOpenRefusesConflictingAppIDAndDriver(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	openFake(t, r.root.Child(), drv)

	if _, err := Open(r.root.Child(), Options{AppID: 481, Args: []string{}}); !errors.Is(err, ErrAppIDMismatch) {
		t.Fatalf("Open with another AppID: err = %v, want ErrAppIDMismatch", err)
	}
	if _, err := Open(r.root.Child(), Options{Driver: NewFakeDriver(), Args: []string{}}); !errors.Is(err, ErrDriverConflict) {
		t.Fatalf("Open with another driver: err = %v, want ErrDriverConflict", err)
	}
	if got := drv.InitCount(); got != 1 {
		t.Fatalf("Init calls = %d, want 1", got)
	}
}

func TestOpenReportsInitFailureAndRestart(t *testing.T) {
	r := newTestRoot(t)

	failing := NewFakeDriver()
	failing.InitResult = InitNoSteamClient
	failing.InitMessage = "Steam is not running"
	_, err := Open(r.root.Child(), Options{Driver: failing, Args: []string{}})
	var initErr *InitError
	if !errors.As(err, &initErr) || initErr.Result != InitNoSteamClient || initErr.Message != "Steam is not running" {
		t.Fatalf("Open with failing init: err = %v, want InitError NoSteamClient", err)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("InitError does not match ErrUnavailable")
	}
	if Current() != nil {
		t.Fatal("failed Open left an active client")
	}

	restart := NewFakeDriver()
	restart.RestartRequired = true
	if _, err := Open(r.root.Child(), Options{AppID: 480, RestartIfNecessary: true, Driver: restart, Args: []string{}}); !errors.Is(err, ErrRestartRequired) {
		t.Fatalf("Open with restart: err = %v, want ErrRestartRequired", err)
	}
	if restart.InitCount() != 0 {
		t.Fatal("Open initialized Steamworks after a restart was requested")
	}
}

func TestEventsDeliveredInsideTheDisplayFrame(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv)

	inFrame := false
	var overlays []OverlayEvent
	var joins []JoinEvent
	var lobbies []LobbyJoinEvent
	var texts []GamepadTextInputEvent
	shutdowns := 0
	check := func() {
		if !inFrame {
			t.Error("event delivered outside the display frame")
		}
	}
	c.OnOverlay(func(ev OverlayEvent) { check(); overlays = append(overlays, ev) })
	c.OnJoin(func(ev JoinEvent) { check(); joins = append(joins, ev) })
	c.OnLobbyJoin(func(ev LobbyJoinEvent) { check(); lobbies = append(lobbies, ev) })
	c.OnGamepadTextInput(func(ev GamepadTextInputEvent) { check(); texts = append(texts, ev) })
	c.OnShutdown(func(ShutdownEvent) { check(); shutdowns++ })

	drv.EmitOverlay(true, true, 480)
	drv.EmitJoin(7, "+connect 10.0.0.1:27015")
	drv.EmitLobbyJoin(99, 8)
	drv.EmitGamepadTextInput(true, "hello", 480)
	drv.EmitShutdown()
	if len(overlays)+len(joins)+len(lobbies)+len(texts)+shutdowns != 0 {
		t.Fatal("events delivered before the frame ran")
	}

	inFrame = true
	r.frame()
	inFrame = false

	if len(overlays) != 1 || !overlays[0].Active || !overlays[0].UserInitiated || overlays[0].AppID != 480 {
		t.Fatalf("overlay events = %+v", overlays)
	}
	if !c.OverlayActive() {
		t.Fatal("OverlayActive false after overlay opened")
	}
	if len(joins) != 1 || joins[0].FriendID != 7 || joins[0].Connect != "+connect 10.0.0.1:27015" || joins[0].FromLaunch {
		t.Fatalf("join events = %+v", joins)
	}
	if len(lobbies) != 1 || lobbies[0].LobbyID != 99 || lobbies[0].FriendID != 8 {
		t.Fatalf("lobby events = %+v", lobbies)
	}
	if len(texts) != 1 || !texts[0].Submitted || texts[0].Text != "hello" {
		t.Fatalf("text input events = %+v", texts)
	}
	if shutdowns != 1 {
		t.Fatalf("shutdown events = %d, want 1", shutdowns)
	}
	if got := drv.InFlightCallbackCount(); got != 0 {
		t.Fatalf("callbacks not freed: %d in flight", got)
	}
}

func TestLaunchConnectDeliveredOnceToFirstHandlers(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv, "-windowed", "+connect", "lobby-42", "+connect_lobby", "109775241021923456")

	if got := c.LaunchConnect(); got != "lobby-42" {
		t.Fatalf("LaunchConnect = %q, want lobby-42", got)
	}
	// Nothing is lost while no handler is registered.
	r.frame()

	var joins []JoinEvent
	var lobbies []LobbyJoinEvent
	c.OnJoin(func(ev JoinEvent) { joins = append(joins, ev) })
	c.OnLobbyJoin(func(ev LobbyJoinEvent) { lobbies = append(lobbies, ev) })
	r.frame()
	r.frame()

	if len(joins) != 1 || joins[0].Connect != "lobby-42" || !joins[0].FromLaunch {
		t.Fatalf("launch joins = %+v, want one FromLaunch lobby-42", joins)
	}
	if len(lobbies) != 1 || lobbies[0].LobbyID != 109775241021923456 || !lobbies[0].FromLaunch {
		t.Fatalf("launch lobby joins = %+v", lobbies)
	}
}

func TestParseConnectArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"+connect", "1.2.3.4:27015"}, "1.2.3.4:27015", true},
		{[]string{"+connect=abc"}, "abc", true},
		{[]string{"+connect abc"}, "abc", true},
		{[]string{"+connect"}, "", false},
		{[]string{"+connect_lobby", "5"}, "", false},
		{nil, "", false},
	} {
		got, ok := ParseConnectArg(tc.args)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseConnectArg(%q) = %q, %v; want %q, %v", tc.args, got, ok, tc.want, tc.ok)
		}
	}
	if id, ok := ParseConnectLobbyArg([]string{"+connect_lobby=12"}); !ok || id != 12 {
		t.Errorf("ParseConnectLobbyArg = %d, %v; want 12, true", id, ok)
	}
	if _, ok := ParseConnectLobbyArg([]string{"+connect_lobby", "x"}); ok {
		t.Error("ParseConnectLobbyArg accepted a non-numeric lobby ID")
	}
}

func TestAwaitCallResolvesBeforeAndAfterCompletion(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv)
	ctx := context.Background()

	// Completed before anyone waits: the result is cached for the next AwaitCall.
	drv.CompleteAPICall(1, 3411, []byte{1, 2, 3}, false)
	r.frame()
	data, err := c.AwaitCall(ctx, 1, 3411)
	if err != nil || string(data) != "\x01\x02\x03" {
		t.Fatalf("AwaitCall(cached) = %v, %v", data, err)
	}

	// Waiting first: the frame that drains the completion wakes the waiter.
	done := make(chan error, 1)
	go func() {
		_, err := c.AwaitCall(ctx, 2, 3411)
		done <- err
	}()
	waitForWaiter(t, c, 2)
	drv.CompleteAPICall(2, 3411, nil, true)
	r.frame()
	if err := <-done; err == nil {
		t.Fatal("AwaitCall of a failed call returned nil error")
	}

	// A wrong callback ID is reported, not decoded as the expected struct.
	drv.CompleteAPICall(3, 3412, []byte{9}, false)
	r.frame()
	if _, err := c.AwaitCall(ctx, 3, 3411); err == nil {
		t.Fatal("AwaitCall accepted a result for another callback")
	}

	// Closing the client fails outstanding waiters with ErrClosed.
	go func() {
		_, err := c.AwaitCall(ctx, 4, 3411)
		done <- err
	}()
	waitForWaiter(t, c, 4)
	r.root.Dispose()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("AwaitCall after dispose: err = %v, want ErrClosed", err)
	}
}

func waitForWaiter(t *testing.T, c *Client, call uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		_, ok := c.callWaiters[call]
		c.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no waiter registered for call %d", call)
}

func TestAuthTicketForWebAPI(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv)

	type result struct {
		ticket AuthTicket
		err    error
	}
	done := make(chan result, 1)
	go func() {
		ticket, err := c.AuthTicketForWebAPI(context.Background(), "verifier")
		done <- result{ticket, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.authWaiters)
		c.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("auth ticket waiter not registered")
		}
		time.Sleep(time.Millisecond)
	}
	drv.EmitAuthTicket(1, 1, []byte{0xab, 0x01})
	r.frame()
	got := <-done
	if got.err != nil || got.ticket.Handle != 1 || got.ticket.Hex() != "AB01" {
		t.Fatalf("AuthTicketForWebAPI = %+v, %v", got.ticket, got.err)
	}
	if reqs := drv.AuthRequests(); len(reqs) != 1 || reqs[0] != "verifier" {
		t.Fatalf("auth requests = %q", reqs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, err := c.AuthTicketForWebAPI(ctx, "")
		done <- result{err: err}
	}()
	for {
		c.mu.Lock()
		n := len(c.authWaiters)
		c.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if got := <-done; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("canceled AuthTicketForWebAPI: err = %v", got.err)
	}
	if canceled := drv.CanceledTickets(); len(canceled) != 1 || canceled[0] != 2 {
		t.Fatalf("canceled tickets = %v, want [2]", canceled)
	}
}

func TestPauseOnOverlayKeepsAnExistingPause(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv)
	loop := &game.Loop{}
	stop := c.PauseOnOverlay(loop)

	drv.EmitOverlay(true, true, 480)
	r.frame()
	if !loop.Paused {
		t.Fatal("loop not paused while the overlay is open")
	}
	drv.EmitOverlay(false, true, 480)
	r.frame()
	if loop.Paused {
		t.Fatal("loop still paused after the overlay closed")
	}

	// A pause menu that was already open stays open after the overlay closes.
	loop.SetPaused(true)
	drv.EmitOverlay(true, true, 480)
	r.frame()
	drv.EmitOverlay(false, true, 480)
	r.frame()
	if !loop.Paused {
		t.Fatal("overlay close resumed a loop the game had paused")
	}

	loop.SetPaused(false)
	drv.EmitOverlay(true, true, 480)
	r.frame()
	stop()
	if loop.Paused {
		t.Fatal("stop did not restore the loop it paused")
	}
}

func TestRawCallbacksAndRichPresence(t *testing.T) {
	r := newTestRoot(t)
	drv := NewFakeDriver()
	c := openFake(t, r.root, drv)

	var got [][]byte
	stop := c.OnCallback(CallbackItemInstalled, func(data []byte) { got = append(got, data) })
	drv.EmitCallback(CallbackItemInstalled, []byte{1, 2})
	drv.EmitCallback(CallbackItemInstalled+1, []byte{3})
	r.frame()
	stop()
	drv.EmitCallback(CallbackItemInstalled, []byte{4})
	r.frame()
	if len(got) != 1 || string(got[0]) != "\x01\x02" {
		t.Fatalf("raw callbacks = %v", got)
	}

	if !c.SetRichPresence("status", "In menus") {
		t.Fatal("SetRichPresence returned false")
	}
	if rp := drv.RichPresence(); rp["status"] != "In menus" {
		t.Fatalf("rich presence = %v", rp)
	}
	c.ClearRichPresence()
	if len(drv.RichPresence()) != 0 {
		t.Fatal("ClearRichPresence left keys")
	}
	if c.SteamID() != 76561198000000001 || c.PersonaName() != "Player" || c.AppID() != 480 {
		t.Fatalf("identity = %d %q %d", c.SteamID(), c.PersonaName(), c.AppID())
	}
}
