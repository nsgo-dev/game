package liveops_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type fakeShareHost struct {
	mu           sync.Mutex
	maxBytes     int
	launch       liveops.LaunchInvite
	hasLaunch    bool
	launchReads  int
	shared       []liveops.PreparedCard
	shareErr     error
	shareShown   bool
	menuFn       func() (liveops.PreparedCard, error)
	menuCalls    int
	resumeFn     func(liveops.LaunchInvite)
	unsubscribed bool
}

func newFakeShareHost(maxBytes int) *fakeShareHost {
	return &fakeShareHost{
		maxBytes:   maxBytes,
		shareShown: true,
	}
}

func (f *fakeShareHost) MaxPayloadBytes() int { return f.maxBytes }

func (f *fakeShareHost) Share(card liveops.PreparedCard, done func(bool, error)) {
	f.mu.Lock()
	f.shared = append(f.shared, card)
	shown, err := f.shareShown, f.shareErr
	f.mu.Unlock()
	if done != nil {
		done(shown, err)
	}
}

func (f *fakeShareHost) SetMenuCard(fn func() (liveops.PreparedCard, error)) {
	f.mu.Lock()
	f.menuFn = fn
	f.menuCalls++
	f.mu.Unlock()
}

func (f *fakeShareHost) LaunchInvite() (liveops.LaunchInvite, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launchReads++
	if !f.hasLaunch {
		return liveops.LaunchInvite{}, false
	}
	f.hasLaunch = false
	return f.launch, true
}

func (f *fakeShareHost) OnInvite(fn func(liveops.LaunchInvite)) func() {
	f.mu.Lock()
	f.resumeFn = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.resumeFn = nil
		f.unsubscribed = true
		f.mu.Unlock()
	}
}

func (f *fakeShareHost) emitResume(inv liveops.LaunchInvite) {
	f.mu.Lock()
	fn := f.resumeFn
	f.mu.Unlock()
	if fn != nil {
		fn(inv)
	}
}

func (f *fakeShareHost) triggerMenu() (liveops.PreparedCard, error) {
	f.mu.Lock()
	fn := f.menuFn
	f.mu.Unlock()
	if fn == nil {
		return liveops.PreparedCard{}, errors.New("no menu card")
	}
	return fn()
}

type fakeMiniGameDriver struct {
	mu              sync.Mutex
	shared          []liveops.MiniGameShareCard
	shareErr        error
	menuEnabled     bool
	withShareTicket bool
	menuFn          func() liveops.MiniGameShareCard
	launch          liveops.MiniGameLaunchOptions
	hasLaunch       bool
	onShowFn        func(liveops.MiniGameLaunchOptions)
	stopped         bool
}

func (d *fakeMiniGameDriver) ShareAppMessage(card liveops.MiniGameShareCard, done func(bool, error)) {
	d.mu.Lock()
	d.shared = append(d.shared, card)
	err := d.shareErr
	d.mu.Unlock()
	if done != nil {
		done(err == nil, err)
	}
}

func (d *fakeMiniGameDriver) ShowShareMenu(enabled bool, withShareTicket bool) {
	d.mu.Lock()
	d.menuEnabled = enabled
	d.withShareTicket = withShareTicket
	d.mu.Unlock()
}

func (d *fakeMiniGameDriver) OnShareAppMessage(fn func() liveops.MiniGameShareCard) {
	d.mu.Lock()
	d.menuFn = fn
	d.mu.Unlock()
}

func (d *fakeMiniGameDriver) LaunchOptions() (liveops.MiniGameLaunchOptions, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.launch, d.hasLaunch
}

func (d *fakeMiniGameDriver) OnShow(fn func(liveops.MiniGameLaunchOptions)) func() {
	d.mu.Lock()
	d.onShowFn = fn
	d.mu.Unlock()
	return func() {
		d.mu.Lock()
		d.onShowFn = nil
		d.stopped = true
		d.mu.Unlock()
	}
}

func (d *fakeMiniGameDriver) emitShow(opts liveops.MiniGameLaunchOptions) {
	d.mu.Lock()
	fn := d.onShowFn
	d.mu.Unlock()
	if fn != nil {
		fn(opts)
	}
}

func TestInvitePayloadRoundTripAndDeterminism(t *testing.T) {
	key := []byte("super-secret-invite-hmac-key")
	in := map[string]string{
		"room":        "lobby-42",
		"from":        "player 1",
		"id":          "inv_001",
		"extra.param": "你好+world&1=2",
		"empty":       "",
	}

	raw, err := liveops.EncodeInvitePayload(in, liveops.InvitePayloadOptions{HMACKey: key})
	if err != nil {
		t.Fatalf("EncodeInvitePayload: %v", err)
	}

	decoded, err := liveops.DecodeInvitePayload(raw, liveops.InvitePayloadOptions{
		HMACKey:          key,
		RequireSignature: true,
	})
	if err != nil {
		t.Fatalf("DecodeInvitePayload: %v", err)
	}
	if !decoded.Verified {
		t.Fatal("expected Verified=true")
	}
	if decoded.ID != "inv_001" {
		t.Fatalf("ID = %q, want inv_001", decoded.ID)
	}
	if decoded.Signature == "" {
		t.Fatal("expected non-empty Signature")
	}
	if _, hasSig := decoded.Payload[liveops.InviteSigKey]; hasSig {
		t.Fatal("Signature key should not be exposed inside decoded Payload map")
	}
	if len(decoded.Payload) != len(in) {
		t.Fatalf("decoded len = %d, want %d", len(decoded.Payload), len(in))
	}
	for k, want := range in {
		if got := decoded.Payload[k]; got != want {
			t.Errorf("Payload[%q] = %q, want %q", k, got, want)
		}
	}

	reencoded, err := liveops.EncodeInvitePayload(decoded.Payload, liveops.InvitePayloadOptions{HMACKey: key})
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if reencoded != raw {
		t.Fatalf("re-encoded %q != original %q", reencoded, raw)
	}

	// Reordered parameters and %20 space encoding must still verify against the same HMAC.
	parts := strings.Split(raw, "&")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	reordered := strings.ReplaceAll(strings.Join(parts, "&"), "player+1", "player%201")
	decoded2, err := liveops.VerifyInvitePayload(reordered, key, 0)
	if err != nil {
		t.Fatalf("VerifyInvitePayload on reordered/%%20 query: %v", err)
	}
	if !decoded2.Verified || decoded2.Payload["from"] != "player 1" {
		t.Fatalf("unexpected decoded2: %+v", decoded2)
	}
}

func TestInvitePayloadValidationAndRejection(t *testing.T) {
	key := []byte("hmac-key")

	// Oversized payload is rejected at exact byte boundary.
	exact, err := liveops.EncodeInvitePayload(map[string]string{"a": "12345"}, liveops.InvitePayloadOptions{MaxBytes: 7})
	if err != nil || exact != "a=12345" {
		t.Fatalf("exact boundary failed: %q, %v", exact, err)
	}
	if _, err := liveops.EncodeInvitePayload(map[string]string{"a": "123456"}, liveops.InvitePayloadOptions{MaxBytes: 7}); !errors.Is(err, liveops.ErrInvitePayloadTooLarge) {
		t.Fatalf("expected ErrInvitePayloadTooLarge, got %v", err)
	}

	// Signature length counts toward MaxBytes.
	if _, err := liveops.SignInvitePayload(map[string]string{"a": "1"}, key, 40); !errors.Is(err, liveops.ErrInvitePayloadTooLarge) {
		t.Fatalf("expected signed payload to exceed 40 bytes, got %v", err)
	}

	// Malformed decode inputs are rejected safely.
	malformed := []struct {
		name string
		raw  string
		want error
	}{
		{"bad escape key", "%ZZ=1", liveops.ErrInvalidInvitePayload},
		{"bad escape val", "a=%2", liveops.ErrInvalidInvitePayload},
		{"missing equals", "room42", liveops.ErrInvalidInvitePayload},
		{"empty segment", "a=1&&b=2", liveops.ErrInvalidInvitePayload},
		{"trailing amp", "a=1&", liveops.ErrInvalidInvitePayload},
		{"leading question", "?a=1", liveops.ErrInvalidInvitePayload},
		{"empty key", "=1", liveops.ErrInvalidInvitePayload},
		{"duplicate key", "a=1&a=2", liveops.ErrInvalidInvitePayload},
		{"duplicate sig", "a=1&sig=abc&sig=def", liveops.ErrInvalidInvitePayload},
		{"invalid key char", "a/b=1", liveops.ErrInvalidInvitePayload},
		{"key too long", strings.Repeat("k", liveops.MaxInviteKeyBytes+1) + "=1", liveops.ErrInvalidInvitePayload},
		{"control char val", "a=hello%00world", liveops.ErrInvalidInvitePayload},
		{"newline val", "a=hello%0Aworld", liveops.ErrInvalidInvitePayload},
		{"invalid utf8 val", "a=%FF%FE", liveops.ErrInvalidInvitePayload},
		{"oversized raw", "a=" + strings.Repeat("x", 140), liveops.ErrInvitePayloadTooLarge},
	}
	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			_, err := liveops.DecodeInvitePayload(tc.raw, liveops.InvitePayloadOptions{MaxBytes: 128})
			if !errors.Is(err, tc.want) {
				t.Fatalf("DecodeInvitePayload(%q) err = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}

	// Too many keys rejected on both encode and decode.
	tooMany := map[string]string{}
	var b strings.Builder
	for i := range liveops.MaxInvitePayloadKeys + 1 {
		k := "k" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		tooMany[k] = "1"
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k + "=1")
	}
	if _, err := liveops.EncodeInvitePayload(tooMany, liveops.InvitePayloadOptions{MaxBytes: 4096}); !errors.Is(err, liveops.ErrInvalidInvitePayload) {
		t.Fatalf("encode too many keys: got %v, want ErrInvalidInvitePayload", err)
	}
	if _, err := liveops.DecodeInvitePayload(b.String(), liveops.InvitePayloadOptions{MaxBytes: 4096}); !errors.Is(err, liveops.ErrInvalidInvitePayload) {
		t.Fatalf("decode too many keys: got %v, want ErrInvalidInvitePayload", err)
	}

	// HMAC verification checks: tampered value, added key, wrong key, missing sig.
	signed, err := liveops.SignInvitePayload(map[string]string{"id": "inv1", "room": "7"}, key, 0)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}
	if _, err := liveops.VerifyInvitePayload(strings.Replace(signed, "room=7", "room=8", 1), key, 0); !errors.Is(err, liveops.ErrInvalidInviteSignature) {
		t.Fatalf("tampered value: got %v, want ErrInvalidInviteSignature", err)
	}
	if _, err := liveops.VerifyInvitePayload(signed+"&extra=1", key, 0); !errors.Is(err, liveops.ErrInvalidInviteSignature) {
		t.Fatalf("added key: got %v, want ErrInvalidInviteSignature", err)
	}
	if _, err := liveops.VerifyInvitePayload(signed, []byte("wrong-key"), 0); !errors.Is(err, liveops.ErrInvalidInviteSignature) {
		t.Fatalf("wrong key: got %v, want ErrInvalidInviteSignature", err)
	}
	if _, err := liveops.VerifyInvitePayload("room=7", key, 0); !errors.Is(err, liveops.ErrInvalidInviteSignature) {
		t.Fatalf("missing sig: got %v, want ErrInvalidInviteSignature", err)
	}
	if _, err := liveops.DecodeInvitePayload("room=7", liveops.InvitePayloadOptions{RequireSignature: true}); !errors.Is(err, liveops.ErrMissingHMACKey) {
		t.Fatalf("RequireSignature without key: got %v, want ErrMissingHMACKey", err)
	}
	// Unsigned allowed when RequireSignature is false, returning Verified=false.
	unsigned, err := liveops.DecodeInvitePayload("room=7", liveops.InvitePayloadOptions{HMACKey: key})
	if err != nil || unsigned.Verified || unsigned.Payload["room"] != "7" {
		t.Fatalf("unsigned with RequireSignature=false: decoded=%+v err=%v", unsigned, err)
	}
	// Tampered signature is still rejected even when RequireSignature is false.
	if _, err := liveops.DecodeInvitePayload(strings.Replace(signed, "room=7", "room=8", 1), liveops.InvitePayloadOptions{HMACKey: key}); !errors.Is(err, liveops.ErrInvalidInviteSignature) {
		t.Fatalf("tampered sig with RequireSignature=false: got %v, want ErrInvalidInviteSignature", err)
	}
}

func TestInvitesFakeHostRoundTripSizeHMACAndOnceDelivery(t *testing.T) {
	key := []byte("game-invite-secret")
	host := newFakeShareHost(120)

	launchQuery, err := liveops.SignInvitePayload(map[string]string{
		"id":      "inv-launch",
		"room":    "alpha",
		"unknown": "keep-me",
	}, key, host.MaxPayloadBytes())
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}
	host.launch = liveops.LaunchInvite{
		RawQuery:    launchQuery,
		Scene:       1044,
		ShareTicket: "ticket-group-1",
		Source:      liveops.InviteSourceLaunch,
	}
	host.hasLaunch = true

	var (
		received []liveops.Invite
		errs     []error
	)
	inv, err := liveops.NewInvites(nil, liveops.InvitesOptions{
		Host:             host,
		HMACKey:          key,
		RequireSignature: true,
		OnInvite:         func(i liveops.Invite) { received = append(received, i) },
		OnError:          func(e error) { errs = append(errs, e) },
	})
	if err != nil {
		t.Fatalf("NewInvites: %v", err)
	}
	defer inv.Close()

	// Cold-start invite is delivered once via Opened().
	opened, ok := inv.Opened()
	if !ok {
		t.Fatal("expected cold-start invite from Opened()")
	}
	if opened.ID != "inv-launch" || opened.Payload["room"] != "alpha" || opened.Payload["unknown"] != "keep-me" {
		t.Fatalf("unexpected opened invite: %+v", opened)
	}
	if !opened.Verified || opened.Scene != 1044 || opened.ShareTicket != "ticket-group-1" || opened.Source != liveops.InviteSourceLaunch {
		t.Fatalf("unexpected opened metadata: %+v", opened)
	}
	if _, again := inv.Opened(); again {
		t.Fatal("Opened() must deliver launch invite at most once")
	}

	// Oversized card is refused BEFORE ShareHost.Share is called.
	var shareShown bool
	var shareErr error
	inv.Share(liveops.Card{
		Title:    "Join my squad",
		InviteID: "inv-big",
		Payload:  map[string]string{"note": strings.Repeat("z", 120)},
	}, func(shown bool, err error) {
		shareShown, shareErr = shown, err
	})
	if shareShown || !errors.Is(shareErr, liveops.ErrInvitePayloadTooLarge) {
		t.Fatalf("oversized Share got (%v, %v), want (false, ErrInvitePayloadTooLarge)", shareShown, shareErr)
	}
	if len(host.shared) != 0 {
		t.Fatalf("oversized card reached ShareHost.Share (%d calls)", len(host.shared))
	}

	// Valid card is signed and passed to ShareHost.Share.
	inv.Share(liveops.Card{
		Title:    "Join my squad",
		ImageID:  "wx-reviewed-img-1",
		InviteID: "inv-2",
		Payload:  map[string]string{"party": "p9"},
	}, func(shown bool, err error) {
		shareShown, shareErr = shown, err
	})
	if !shareShown || shareErr != nil {
		t.Fatalf("valid Share got (%v, %v)", shareShown, shareErr)
	}
	if len(host.shared) != 1 {
		t.Fatalf("host.shared len = %d, want 1", len(host.shared))
	}
	sent := host.shared[0]
	if sent.Title != "Join my squad" || sent.ImageID != "wx-reviewed-img-1" {
		t.Fatalf("unexpected PreparedCard: %+v", sent)
	}

	// Simulate another player opening the game while already running (onShow) with the card we just shared.
	host.emitResume(liveops.LaunchInvite{
		RawQuery:    sent.Query,
		Scene:       1007,
		ShareTicket: "ticket-2",
		Source:      liveops.InviteSourceResume,
	})
	if len(received) != 1 || received[0].ID != "inv-2" || received[0].Payload["party"] != "p9" || !received[0].Verified {
		t.Fatalf("unexpected OnInvite delivery: %+v", received)
	}
	if inv.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, want 1", inv.PendingCount())
	}
	peeked, ok := inv.Incoming()
	if !ok || peeked.ID != "inv-2" {
		t.Fatalf("Incoming = (%+v, %v)", peeked, ok)
	}
	consumed, ok := inv.Consume()
	if !ok || consumed.ID != "inv-2" || consumed.Source != liveops.InviteSourceResume {
		t.Fatalf("Consume = (%+v, %v)", consumed, ok)
	}
	if _, again := inv.Consume(); again {
		t.Fatal("Consume() must deliver each resume invite once")
	}
	if inv.PendingCount() != 0 {
		t.Fatalf("PendingCount after Consume = %d, want 0", inv.PendingCount())
	}

	// Malformed / unsigned resume invite is rejected safely and sent to OnError without delivering.
	host.emitResume(liveops.LaunchInvite{RawQuery: "party=unverified"})
	host.emitResume(liveops.LaunchInvite{RawQuery: "party=%ZZ"})
	if len(received) != 1 || inv.PendingCount() != 0 {
		t.Fatalf("invalid invites must not be delivered: received=%d pending=%d", len(received), inv.PendingCount())
	}
	if len(errs) != 2 || !errors.Is(errs[0], liveops.ErrInvalidInviteSignature) || !errors.Is(errs[1], liveops.ErrInvalidInvitePayload) {
		t.Fatalf("unexpected OnError slice: %v", errs)
	}

	// SetMenuCard validates and signs cards dynamically, and reports oversized errors.
	inv.SetMenuCard(func() liveops.Card {
		return liveops.Card{Title: "Menu invite", InviteID: "menu-1", Payload: map[string]string{"room": "m1"}}
	})
	menuCard, err := host.triggerMenu()
	if err != nil {
		t.Fatalf("triggerMenu: %v", err)
	}
	verifiedMenu, err := liveops.VerifyInvitePayload(menuCard.Query, key, host.MaxPayloadBytes())
	if err != nil || verifiedMenu.ID != "menu-1" || verifiedMenu.Payload["room"] != "m1" {
		t.Fatalf("menu card payload did not verify: %+v, %v", verifiedMenu, err)
	}

	inv.SetMenuCard(func() liveops.Card {
		return liveops.Card{Title: "Too big", Payload: map[string]string{"blob": strings.Repeat("x", 200)}}
	})
	if _, err := host.triggerMenu(); !errors.Is(err, liveops.ErrInvitePayloadTooLarge) {
		t.Fatalf("oversized menu card err = %v, want ErrInvitePayloadTooLarge", err)
	}
	inv.SetMenuCard(nil)
	if host.menuFn != nil {
		t.Fatal("SetMenuCard(nil) did not clear host menu handler")
	}

	// Close unsubscribes from the host and stops future deliveries.
	inv.Close()
	if !host.unsubscribed {
		t.Fatal("Close did not unsubscribe from ShareHost")
	}
	host.emitResume(liveops.LaunchInvite{RawQuery: sent.Query})
	if len(received) != 1 {
		t.Fatalf("received invite after Close: %d", len(received))
	}
}

func TestInvitesServerMintedSignatureWithoutClientSecret(t *testing.T) {
	serverSecret := []byte("server-only-hmac-secret")
	serverQuery, err := liveops.SignInvitePayload(map[string]string{
		"id":   "grant-777",
		"room": "arena-9",
	}, serverSecret, 0)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}
	decodedOnServer, err := liveops.VerifyInvitePayload(serverQuery, serverSecret, 0)
	if err != nil {
		t.Fatalf("VerifyInvitePayload: %v", err)
	}

	host := newFakeShareHost(512)
	// Client has NO HMACKey; it shares the server-minted signature using Card.Signature.
	clientInvites, err := liveops.NewInvites(nil, liveops.InvitesOptions{Host: host})
	if err != nil {
		t.Fatalf("NewInvites: %v", err)
	}
	defer clientInvites.Close()

	var shareErr error
	clientInvites.Share(liveops.Card{
		Title:     "Server signed invite",
		InviteID:  "grant-777",
		Payload:   map[string]string{"room": "arena-9"},
		Signature: decodedOnServer.Signature,
	}, func(shown bool, err error) {
		shareErr = err
	})
	if shareErr != nil || len(host.shared) != 1 {
		t.Fatalf("Share failed: err=%v count=%d", shareErr, len(host.shared))
	}

	// Recipient receives the invite on a client without HMACKey and forwards RawQuery to the server.
	host.emitResume(liveops.LaunchInvite{RawQuery: host.shared[0].Query})
	got, ok := clientInvites.Consume()
	if !ok {
		t.Fatal("expected received invite")
	}
	if got.Verified {
		t.Fatal("client without HMACKey must not claim Verified=true")
	}
	if got.ID != "grant-777" || got.Signature != decodedOnServer.Signature {
		t.Fatalf("unexpected recipient invite: %+v", got)
	}
	// Server verifies recipient's RawQuery and extracts idempotency key.
	verified, err := liveops.VerifyInvitePayload(got.RawQuery, serverSecret, 0)
	if err != nil || !verified.Verified || verified.ID != "grant-777" || verified.Payload["room"] != "arena-9" {
		t.Fatalf("server VerifyInvitePayload failed: %+v, %v", verified, err)
	}
}

func TestInviteURLFormattingAndExtraction(t *testing.T) {
	q := "id=inv-1&room=42"

	// Query encoding.
	link, err := liveops.FormatInviteURL("mygame://invite", q, liveops.InviteEncodeQuery)
	if err != nil || link != "mygame://invite?id=inv-1&room=42" {
		t.Fatalf("FormatInviteURL query = (%q, %v)", link, err)
	}
	extracted, matched, err := liveops.ExtractInviteQuery("MYGAME://INVITE/?id=inv-1&room=42", liveops.InviteLinkOptions{
		BaseURL: "mygame://invite",
	})
	if err != nil || !matched || extracted != q {
		t.Fatalf("ExtractInviteQuery query = (%q, %v, %v)", extracted, matched, err)
	}

	// Unrelated scheme/host/path ignored.
	for _, unrelated := range []string{
		"https://example.com/invite?id=inv-1",
		"mygame://settings?id=inv-1",
		"mygame://invite/other?id=inv-1",
		"mygame://invite",
	} {
		if got, ok, err := liveops.ExtractInviteQuery(unrelated, liveops.InviteLinkOptions{BaseURL: "mygame://invite"}); err != nil || ok || got != "" {
			t.Fatalf("ExtractInviteQuery(%q) = (%q, %v, %v), want unmatched", unrelated, got, ok, err)
		}
	}

	// Fragment encoding.
	fragLink, err := liveops.FormatInviteURL("https://play.example.com/game", q, liveops.InviteEncodeFragment)
	if err != nil || fragLink != "https://play.example.com/game#id=inv-1&room=42" {
		t.Fatalf("FormatInviteURL fragment = (%q, %v)", fragLink, err)
	}
	extracted, matched, err = liveops.ExtractInviteQuery(fragLink, liveops.InviteLinkOptions{
		BaseURL:  "https://play.example.com/game",
		Encoding: liveops.InviteEncodeFragment,
	})
	if err != nil || !matched || extracted != q {
		t.Fatalf("ExtractInviteQuery fragment = (%q, %v, %v)", extracted, matched, err)
	}

	// AcceptFragment fallback with InviteEncodeQuery.
	extracted, matched, err = liveops.ExtractInviteQuery(fragLink, liveops.InviteLinkOptions{
		BaseURL:        "https://play.example.com/game",
		Encoding:       liveops.InviteEncodeQuery,
		AcceptFragment: true,
	})
	if err != nil || !matched || extracted != q {
		t.Fatalf("ExtractInviteQuery AcceptFragment = (%q, %v, %v)", extracted, matched, err)
	}

	// Conflicting base URLs refused.
	if _, err := liveops.FormatInviteURL("https://play.example.com/game?x=1", q, liveops.InviteEncodeQuery); !errors.Is(err, liveops.ErrInvalidInviteURL) {
		t.Fatalf("expected ErrInvalidInviteURL for base URL with query, got %v", err)
	}
	if _, err := liveops.FormatInviteURL("https://play.example.com/game#x=1", q, liveops.InviteEncodeFragment); !errors.Is(err, liveops.ErrInvalidInviteURL) {
		t.Fatalf("expected ErrInvalidInviteURL for base URL with fragment, got %v", err)
	}
}

func TestPlatformShareHostNativeMobileAndReactiveConsume(t *testing.T) {
	key := []byte("native-invite-key")
	var (
		inv         *liveops.Invites
		effectLog   []string
		shareResult bool
		shareErr    error
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		inv, err = liveops.NewInvites(cx, liveops.InvitesOptions{
			HMACKey:          key,
			RequireSignature: true,
			Platform: liveops.PlatformShareHostOptions{
				BaseURL: "mygame://invite",
			},
		})
		if err != nil {
			t.Fatalf("NewInvites: %v", err)
		}
		cx.Effect(func() {
			for {
				got, ok := inv.Consume()
				if !ok {
					break
				}
				effectLog = append(effectLog, got.ID+":"+got.Payload["room"])
			}
		})
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone}))

	// Share a card with a 5:4 PNG snapshot image.
	snapPNG, err := liveops.EncodeCardSnapshotPNG(makeTestImage(160, 90), 50)
	if err != nil {
		t.Fatalf("EncodeCardSnapshotPNG: %v", err)
	}
	inv.Share(liveops.Card{
		Title:    "Play with me!",
		Image:    core.ImageSource{Data: snapPNG},
		InviteID: "inv-ios-1",
		Payload:  map[string]string{"room": "r99"},
	}, func(shown bool, err error) {
		shareResult, shareErr = shown, err
	})
	app.Settle()

	if !shareResult || shareErr != nil {
		t.Fatalf("native Share = (%v, %v)", shareResult, shareErr)
	}
	if len(app.Shares) != 1 {
		t.Fatalf("app.Shares = %d, want 1", len(app.Shares))
	}
	recorded := app.Shares[0]
	if !strings.HasPrefix(recorded.Text, "Play with me!\nmygame://invite?") {
		t.Fatalf("unexpected share text: %q", recorded.Text)
	}
	if len(recorded.Files) != 1 || recorded.Files[0].MIME != "image/png" || len(recorded.Files[0].Data) == 0 {
		t.Fatalf("unexpected shared files: %+v", recorded.Files)
	}

	// Extract the shared link and deliver it to the running app via OpenLinks (warm re-open).
	_, sharedLink, _ := strings.Cut(recorded.Text, "\n")
	app.OpenLinks("https://unrelated.example/page?room=bad")
	app.OpenLinks(sharedLink)

	if len(effectLog) != 1 || effectLog[0] != "inv-ios-1:r99" {
		t.Fatalf("reactive Effect log = %v, want [inv-ios-1:r99]", effectLog)
	}
	if inv.PendingCount() != 0 {
		t.Fatalf("PendingCount after Effect consume = %d, want 0", inv.PendingCount())
	}
}

func TestPlatformShareHostWebColdStartQueryAndFragment(t *testing.T) {
	key := []byte("web-invite-key")
	q, err := liveops.SignInvitePayload(map[string]string{
		"id":   "web-1",
		"room": "browser-room",
	}, key, 0)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}

	t.Run("query auto-derived base URL", func(t *testing.T) {
		var inv *liveops.Invites
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			var err error
			inv, err = liveops.NewInvites(cx, liveops.InvitesOptions{
				HMACKey:          key,
				RequireSignature: true,
			})
			if err != nil {
				t.Fatalf("NewInvites web: %v", err)
			}
			return core.View{}
		}, nsgotest.Environment(core.Environment{
			OS:      core.OSWeb,
			PageURL: "https://play.example.com/arena?" + q,
		}))

		opened, ok := inv.Opened()
		if !ok {
			t.Fatal("expected cold-start web invite from PageURL")
		}
		if !opened.Verified || opened.ID != "web-1" || opened.Payload["room"] != "browser-room" || opened.Source != liveops.InviteSourceLaunch {
			t.Fatalf("unexpected web opened invite: %+v", opened)
		}
		if _, again := inv.Opened(); again {
			t.Fatal("web launch invite delivered twice")
		}

		// Sharing on the web uses the auto-derived BaseURL (origin + path without old query).
		inv.Share(liveops.Card{
			Title:    "Web invite",
			InviteID: "web-2",
			Payload:  map[string]string{"room": "next"},
		}, nil)
		app.Settle()

		if len(app.Shares) != 1 {
			t.Fatalf("app.Shares = %d, want 1", len(app.Shares))
		}
		if !strings.HasPrefix(app.Shares[0].Text, "Web invite\nhttps://play.example.com/arena?") {
			t.Fatalf("unexpected web share text: %q", app.Shares[0].Text)
		}
		if strings.Contains(app.Shares[0].Text, "browser-room") {
			t.Fatalf("old launch query leaked into new share link: %q", app.Shares[0].Text)
		}
	})

	t.Run("fragment encoding", func(t *testing.T) {
		var inv *liveops.Invites
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			var err error
			inv, err = liveops.NewInvites(cx, liveops.InvitesOptions{
				HMACKey:          key,
				RequireSignature: true,
				Platform: liveops.PlatformShareHostOptions{
					Encoding: liveops.InviteEncodeFragment,
				},
			})
			if err != nil {
				t.Fatalf("NewInvites web fragment: %v", err)
			}
			return core.View{}
		}, nsgotest.Environment(core.Environment{
			OS:      core.OSWeb,
			PageURL: "https://play.example.com/arena#" + q,
		}))

		opened, ok := inv.Opened()
		if !ok || !opened.Verified || opened.ID != "web-1" || opened.Payload["room"] != "browser-room" {
			t.Fatalf("unexpected web fragment launch invite: %+v, ok=%v", opened, ok)
		}
	})
}

func TestMiniGameShareHostAndUnavailableFallback(t *testing.T) {
	// Nil driver degrades safely with ErrShareUnavailable.
	nilHost := liveops.NewMiniGameShareHost(nil, liveops.MiniGameShareHostOptions{})
	nilInvites, err := liveops.NewInvites(nil, liveops.InvitesOptions{Host: nilHost})
	if err != nil {
		t.Fatalf("NewInvites nil mini-game driver: %v", err)
	}
	if _, ok := nilInvites.Opened(); ok {
		t.Fatal("nil driver must report no launch invite")
	}
	var gotErr error
	nilInvites.Share(liveops.Card{Title: "Hi", Payload: map[string]string{"a": "1"}}, func(shown bool, err error) {
		gotErr = err
	})
	if !errors.Is(gotErr, liveops.ErrShareUnavailable) {
		t.Fatalf("nil driver Share err = %v, want ErrShareUnavailable", gotErr)
	}
	nilInvites.Close()

	// Real MiniGameShareDriver flow: QueryMap launch, menu card + WithShareTicket, onShow resume, empty onShow ignored.
	key := []byte("wechat-minigame-secret")
	launchRaw, err := liveops.SignInvitePayload(map[string]string{"id": "wx-1", "room": "r1"}, key, 0)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}
	launchDecoded, err := liveops.VerifyInvitePayload(launchRaw, key, 0)
	if err != nil {
		t.Fatalf("VerifyInvitePayload: %v", err)
	}
	queryMap := map[string]string{
		"id":   "wx-1",
		"room": "r1",
		"sig":  launchDecoded.Signature,
	}

	drv := &fakeMiniGameDriver{
		hasLaunch: true,
		launch: liveops.MiniGameLaunchOptions{
			QueryMap:    queryMap,
			Scene:       1044,
			ShareTicket: "wx-group-ticket",
		},
	}
	mgHost := liveops.NewMiniGameShareHost(drv, liveops.MiniGameShareHostOptions{
		WithShareTicket:  true,
		MaxPayloadBytes:  256,
		FallbackMenuCard: liveops.MiniGameShareCard{Title: "Fallback"},
	})
	inv, err := liveops.NewInvites(nil, liveops.InvitesOptions{
		Host:             mgHost,
		HMACKey:          key,
		RequireSignature: true,
	})
	if err != nil {
		t.Fatalf("NewInvites mini game: %v", err)
	}
	defer inv.Close()

	opened, ok := inv.Opened()
	if !ok || !opened.Verified || opened.ID != "wx-1" || opened.Payload["room"] != "r1" || opened.Scene != 1044 || opened.ShareTicket != "wx-group-ticket" {
		t.Fatalf("unexpected mini-game launch invite: %+v, ok=%v", opened, ok)
	}

	// SetMenuCard enables wx.showShareMenu with withShareTicket=true.
	inv.SetMenuCard(func() liveops.Card {
		return liveops.Card{
			Title:    "Forward me",
			ImageID:  "reviewed-42",
			InviteID: "wx-menu",
			Payload:  map[string]string{"room": "lobby"},
		}
	})
	if !drv.menuEnabled || !drv.withShareTicket || drv.menuFn == nil {
		t.Fatalf("ShowShareMenu/OnShareAppMessage not wired: enabled=%v ticket=%v fn=%v", drv.menuEnabled, drv.withShareTicket, drv.menuFn != nil)
	}
	mc := drv.menuFn()
	if mc.Title != "Forward me" || mc.ImageURLID != "reviewed-42" {
		t.Fatalf("unexpected menu card: %+v", mc)
	}

	// Ordinary onShow without query is ignored; onShow with query delivers once.
	drv.emitShow(liveops.MiniGameLaunchOptions{Scene: 1001})
	if inv.PendingCount() != 0 {
		t.Fatalf("empty onShow must not enqueue an invite")
	}
	drv.emitShow(liveops.MiniGameLaunchOptions{
		Query:       mc.Query,
		Scene:       1008,
		ShareTicket: "wx-resume-ticket",
	})
	resumed, ok := inv.Consume()
	if !ok || !resumed.Verified || resumed.ID != "wx-menu" || resumed.Scene != 1008 || resumed.ShareTicket != "wx-resume-ticket" {
		t.Fatalf("unexpected mini-game resume invite: %+v, ok=%v", resumed, ok)
	}

	// Oversized menu card falls back cleanly to FallbackMenuCard.
	inv.SetMenuCard(func() liveops.Card {
		return liveops.Card{Title: "Too big", Payload: map[string]string{"x": strings.Repeat("a", 300)}}
	})
	if fb := drv.menuFn(); fb.Title != "Fallback" {
		t.Fatalf("expected FallbackMenuCard on oversized menu card, got %+v", fb)
	}

	// Clearing menu card disables wx.showShareMenu.
	inv.SetMenuCard(nil)
	if drv.menuEnabled || drv.menuFn != nil {
		t.Fatalf("SetMenuCard(nil) did not disable mini-game share menu")
	}
}

func TestCardSnapshotFiveByFourCropAndScale(t *testing.T) {
	// 16:9 landscape frame (1600x900) -> center-cropped to 1125x900 (5:4) -> scaled down to 500x400.
	landscape := makeCenterMarkedImage(1600, 900)
	out, err := liveops.CropCardSnapshot(landscape, 0)
	if err != nil {
		t.Fatalf("CropCardSnapshot landscape: %v", err)
	}
	if b := out.Bounds(); b.Dx() != 500 || b.Dy() != 400 {
		t.Fatalf("landscape bounds = %dx%d, want 500x400", b.Dx(), b.Dy())
	}
	// Center pixel must remain green (inside the centered 5:4 region), not red border.
	center := out.RGBAAt(250, 200)
	if center.G < 200 || center.R > 50 {
		t.Fatalf("landscape center pixel = %+v, expected green center", center)
	}

	// 9:16 portrait frame (720x1280) -> center-cropped to 720x576 (5:4) -> scaled down to 500x400.
	portrait := makeCenterMarkedImage(720, 1280)
	outP, err := liveops.CropCardSnapshot(portrait, 500)
	if err != nil {
		t.Fatalf("CropCardSnapshot portrait: %v", err)
	}
	if b := outP.Bounds(); b.Dx() != 500 || b.Dy() != 400 {
		t.Fatalf("portrait bounds = %dx%d, want 500x400", b.Dx(), b.Dy())
	}

	// Small 5:4 image is not upscaled.
	small := makeTestImage(250, 200)
	rawPNG, err := liveops.EncodeCardSnapshotPNG(small, 500)
	if err != nil {
		t.Fatalf("EncodeCardSnapshotPNG small: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(rawPNG))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 250 || b.Dy() != 200 {
		t.Fatalf("small bounds = %dx%d, want 250x200", b.Dx(), b.Dy())
	}

	// Nil image rejected.
	if _, err := liveops.CropCardSnapshot(nil, 500); err == nil {
		t.Fatal("expected error for nil image")
	}

	// CaptureCardSnapshot with nil scope returns error cleanly.
	var capErr error
	liveops.CaptureCardSnapshot(nil, nil, liveops.CardSnapshotOptions{}, func(_ core.ImageSource, err error) {
		capErr = err
	})
	if capErr == nil {
		t.Fatal("expected error for nil scope in CaptureCardSnapshot")
	}
}

func TestInvitesConcurrentAccessRaceSafe(t *testing.T) {
	key := []byte("race-test-key")
	host := newFakeShareHost(256)
	inv, err := liveops.NewInvites(nil, liveops.InvitesOptions{
		Host:             host,
		HMACKey:          key,
		RequireSignature: true,
	})
	if err != nil {
		t.Fatalf("NewInvites: %v", err)
	}
	defer inv.Close()

	q, err := liveops.SignInvitePayload(map[string]string{"id": "r1", "room": "ok"}, key, 256)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				inv.Share(liveops.Card{Title: "Race", InviteID: "id1", Payload: map[string]string{"k": "v"}}, nil)
				host.emitResume(liveops.LaunchInvite{RawQuery: q})
				_, _ = inv.Consume()
				_, _ = inv.Incoming()
				_ = inv.PendingCount()
			}
		}()
	}
	wg.Wait()
}

func TestPlatformShareHostColdStartOpenLinksNoDuplicate(t *testing.T) {
	key := []byte("cold-start-key")
	q, err := liveops.SignInvitePayload(map[string]string{"id": "cold-1", "room": "start"}, key, 0)
	if err != nil {
		t.Fatalf("SignInvitePayload: %v", err)
	}
	coldLink := "mygame://invite?" + q

	var (
		inv     *liveops.Invites
		resumed []liveops.Invite
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))

	// Deliver cold-start link BEFORE NewInvites registers (simulating launch-time deep link queued in NSGo).
	app.OpenLinks(coldLink)

	// Now mount a view that creates Invites (so cx.AppURL() already has coldLink AND NSGo's openQueue still holds it).
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var err error
		inv, err = liveops.NewInvites(cx, liveops.InvitesOptions{
			HMACKey:          key,
			RequireSignature: true,
			Platform: liveops.PlatformShareHostOptions{
				BaseURL: "mygame://invite",
			},
			OnInvite: func(i liveops.Invite) {
				resumed = append(resumed, i)
			},
		})
		if err != nil {
			t.Fatalf("NewInvites: %v", err)
		}
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))

	// Share with ImageSource.Pixels attaches encoded PNG bytes.
	inv.Share(liveops.Card{
		Title:    "Android invite",
		Image:    core.ImageSource{Pixels: makeTestImage(20, 16)},
		InviteID: "and-1",
		Payload:  map[string]string{"room": "r7"},
	}, nil)
	app2.Settle()
	if len(app2.Shares) != 1 || len(app2.Shares[0].Files) != 1 || app2.Shares[0].Files[0].MIME != "image/png" {
		t.Fatalf("unexpected Android pixel share: %+v", app2.Shares)
	}

	// Warm re-open on running Android app delivers once.
	app2.OpenLinks(coldLink)
	if len(resumed) != 1 || resumed[0].ID != "cold-1" || resumed[0].Source != liveops.InviteSourceResume {
		t.Fatalf("warm OpenLinks = %+v", resumed)
	}
	_ = app
}

func makeTestImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	return img
}

func makeCenterMarkedImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx0, cx1 := w/4, 3*w/4
	cy0, cy1 := h/4, 3*h/4
	for y := range h {
		for x := range w {
			if x >= cx0 && x < cx1 && y >= cy0 && y < cy1 {
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 240, B: 0, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{R: 240, G: 0, B: 0, A: 255})
			}
		}
	}
	return img
}
