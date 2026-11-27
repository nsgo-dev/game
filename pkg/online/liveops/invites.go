package liveops

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/core"
)

const maxPendingInvites = 64

// InviteSource reports whether an invite reached the game at cold launch or while already running.
type InviteSource uint8

const (
	// InviteSourceLaunch indicates the game was cold-started from the invite.
	InviteSourceLaunch InviteSource = iota + 1
	// InviteSourceResume indicates the invite arrived while the game was already running (for example via OnOpenLinks, web URL change or mini-game onShow).
	InviteSourceResume
)

// String returns "launch", "resume" or "unknown".
func (s InviteSource) String() string {
	switch s {
	case InviteSourceLaunch:
		return "launch"
	case InviteSourceResume:
		return "resume"
	default:
		return "unknown"
	}
}

// Card describes a share card or invite passed to Invites.Share or returned by a menu-share callback.
type Card struct {
	// Title is the short headline shown on the share card or prepended to the native/web share message.
	Title string
	// Image is the card's image (for example a 5:4 snapshot from CaptureCardSnapshot or an embedded PNG/JPEG).
	Image core.ImageSource
	// ImageID is an optional platform-reviewed image identifier (such as WeChat mini game's imageUrlId).
	ImageID string
	// InviteID is an optional unique identifier for this invite, encoded under InviteIDKey ("id").
	// Servers use Invite.ID as the idempotency key for invite reward grants.
	InviteID string
	// Payload holds key-value parameters carried to whoever opens the game from the card (for example room or party IDs).
	// Unknown keys are preserved on decode.
	Payload map[string]string
	// Signature is an optional pre-computed base64url raw HMAC-SHA256 signature minted by a server.
	// When InvitesOptions.HMACKey is configured, Invites computes the signature itself and refuses a non-empty Card.Signature.
	Signature string
	// Anchor is the optional UI element anchoring a popover share sheet on macOS and iPadOS.
	Anchor *core.Ref
}

// PreparedCard is a validated, payload-encoded card ready for a ShareHost to present.
type PreparedCard struct {
	// Title is the validated card title.
	Title string
	// Image is the card's image source.
	Image core.ImageSource
	// ImageID is the optional platform-reviewed image identifier.
	ImageID string
	// Query is the canonical encoded "key=value&..." payload (including id and sig when present), guaranteed to fit within the host's byte limit.
	Query string
	// Anchor is the optional popover anchor for native share sheets.
	Anchor *core.Ref
}

// LaunchInvite is the raw invite event delivered by a ShareHost at launch or on resume.
type LaunchInvite struct {
	// RawQuery is the encoded "key=value&..." invite payload extracted by the host.
	RawQuery string
	// RawURL is the full link that opened the game on native/web hosts (empty on mini games).
	RawURL string
	// Scene is the platform launch scene code (such as WeChat's launch/onShow scene value; 0 when unused).
	Scene int
	// ShareTicket is the opaque group-share ticket provided by mini-game hosts when opened from a group chat (empty otherwise).
	ShareTicket string
	// Source identifies whether this invite arrived on cold launch or while running.
	Source InviteSource
}

// Invite is a decoded, validated invite received when a player opens or resumes the game from a share card or invite link.
type Invite struct {
	// ID is the invite identifier from InviteIDKey ("id"), empty when not present.
	// Pass ID to the game server as the idempotency key for invite reward grants.
	ID string
	// Payload holds all decoded payload fields except InviteSigKey ("sig"), preserving unknown keys.
	Payload map[string]string
	// RawQuery is the raw "key=value&..." payload received from the host, suitable for forwarding to a game server for VerifyInvitePayload.
	RawQuery string
	// RawURL is the full URL that opened the game on native/web hosts (empty on mini games).
	RawURL string
	// Signature is the raw signature from InviteSigKey ("sig"), empty when unsigned.
	Signature string
	// Verified is true only when InvitesOptions.HMACKey was configured and Signature matched Payload under HMAC-SHA256.
	Verified bool
	// Scene is the platform launch scene code (0 when unused).
	Scene int
	// ShareTicket is the opaque group-share ticket supplied by mini-game hosts (empty otherwise).
	ShareTicket string
	// Source reports whether the invite arrived on cold launch (InviteSourceLaunch) or while running (InviteSourceResume).
	Source InviteSource
}

// Get returns the value of key from Invite.Payload.
func (inv Invite) Get(key string) (string, bool) {
	if len(inv.Payload) == 0 {
		return "", false
	}
	v, ok := inv.Payload[key]
	return v, ok
}

// Clone returns a copy of inv with an independent Payload map.
func (inv Invite) Clone() Invite {
	cp := inv
	if inv.Payload != nil {
		cp.Payload = maps.Clone(inv.Payload)
	}
	return cp
}

// ShareHost is the small platform boundary underneath Invites.
// Built-in implementations are PlatformShareHost (mobile, desktop and web over NSGo's core APIs) and MiniGameShareHost (mini-game bridge ready for nsgo-dev/nsgo#2672); tests supply a fake ShareHost.
type ShareHost interface {
	// MaxPayloadBytes returns the maximum allowed byte length of PreparedCard.Query.
	// Returning <= 0 uses DefaultMaxInvitePayloadBytes (1024).
	MaxPayloadBytes() int

	// Share presents the platform share card or system share sheet for card and invokes done when complete.
	Share(card PreparedCard, done func(shown bool, err error))

	// SetMenuCard registers a callback supplying the card for the host's built-in share menu (such as a mini game's Forward item).
	// Passing nil disables/clears the menu card. Hosts without a system share menu treat this as a no-op.
	SetMenuCard(fn func() (PreparedCard, error))

	// LaunchInvite returns the invite that cold-started the game, if any.
	LaunchInvite() (LaunchInvite, bool)

	// OnInvite subscribes fn to invites that arrive while the game is running (warm re-open / onShow) and returns an unsubscribe function.
	OnInvite(fn func(LaunchInvite)) (stop func())
}

// InvitesOptions configures NewInvites.
type InvitesOptions struct {
	// Host is the ShareHost used to send cards and receive launch/resume invites.
	// When nil, NewInvites creates a PlatformShareHost on cx using Platform.
	Host ShareHost
	// Platform configures the default PlatformShareHost when Host is nil.
	Platform PlatformShareHostOptions
	// HMACKey, when non-empty, signs outgoing cards with HMAC-SHA256 under InviteSigKey ("sig")
	// and verifies incoming invites, setting Invite.Verified.
	HMACKey []byte
	// RequireSignature rejects incoming invites that lack a valid HMAC-SHA256 signature under HMACKey.
	// Setting RequireSignature with an empty HMACKey causes NewInvites to return ErrMissingHMACKey.
	RequireSignature bool
	// MaxPayloadBytes caps encoded invite payloads in bytes.
	// When <= 0, the host's MaxPayloadBytes() (or DefaultMaxInvitePayloadBytes) applies.
	// When larger than the host's limit, the smaller host limit still wins.
	MaxPayloadBytes int
	// OnInvite, when non-nil, runs once for each invite that arrives while the game is running.
	OnInvite func(Invite)
	// OnError, when non-nil, receives errors from malformed, oversized or unverified incoming invites or menu-card preparation failures.
	OnError func(error)
}

type inviteListener struct {
	id uint64
	fn func(Invite)
}

// Invites sends payload-carrying share cards and invites over a ShareHost and reads incoming invites back on cold launch and warm resume.
//
// Outgoing payloads are canonically encoded as "key=value&...", bounded by the host's payload limit and refused before sharing if invalid or oversized.
// Incoming invites are delivered at most once: Opened consumes the cold-start invite once, and Consume / OnInvite deliver each warm resume invite once.
// When created with a non-nil core.Scope, Incoming, Consume and PendingCount integrate with NSGo's reactive signal graph.
type Invites struct {
	cx               *core.Scope
	host             ShareHost
	hmacKey          []byte
	requireSignature bool
	maxPayloadBytes  int
	onError          func(error)

	// arrived ticks only when a new resume invite arrives so Effects calling Consume do not re-run on pop.
	arrived *core.Signal[int]
	// pending ticks whenever the pending resume queue length changes so UI badges reading PendingCount/Incoming stay current.
	pending *core.Signal[int]

	mu           sync.Mutex
	closed       bool
	stopHost     func()
	opened       Invite
	hasOpened    bool
	openedTaken  bool
	incoming     []Invite
	listeners    []inviteListener
	nextListener uint64
}

// NewInvites creates an Invites manager backed by opts.Host (or PlatformShareHost on cx when opts.Host is nil).
// It reads any cold-start launch invite immediately and subscribes to warm resume invites while the scope (or manager, until Close) lives.
func NewInvites(cx *core.Scope, opts InvitesOptions) (*Invites, error) {
	if opts.RequireSignature && len(opts.HMACKey) == 0 {
		return nil, ErrMissingHMACKey
	}
	if opts.MaxPayloadBytes < 0 {
		return nil, fmt.Errorf("%w: negative MaxPayloadBytes %d", ErrInvalidInvitePayload, opts.MaxPayloadBytes)
	}

	host := opts.Host
	if host == nil {
		if cx == nil {
			return nil, errors.New("liveops: invites need either a core.Scope or InvitesOptions.Host")
		}
		var err error
		host, err = NewPlatformShareHost(cx, opts.Platform)
		if err != nil {
			return nil, err
		}
	}

	limit := effectivePayloadLimit(host.MaxPayloadBytes())
	if opts.MaxPayloadBytes > 0 && opts.MaxPayloadBytes < limit {
		limit = opts.MaxPayloadBytes
	}

	inv := &Invites{
		cx:               cx,
		host:             host,
		hmacKey:          bytes.Clone(opts.HMACKey),
		requireSignature: opts.RequireSignature,
		maxPayloadBytes:  limit,
		onError:          opts.OnError,
	}
	if cx != nil {
		inv.arrived = core.NewSignal(cx, 0)
		inv.pending = core.NewSignal(cx, 0)
	}
	if opts.OnInvite != nil {
		inv.nextListener++
		inv.listeners = append(inv.listeners, inviteListener{id: inv.nextListener, fn: opts.OnInvite})
	}

	if raw, ok := host.LaunchInvite(); ok {
		raw.Source = InviteSourceLaunch
		decoded, err := inv.decodeRaw(raw)
		if err != nil {
			inv.reportError(err)
		} else {
			inv.opened = decoded
			inv.hasOpened = true
		}
	}

	inv.stopHost = host.OnInvite(inv.handleIncoming)
	if cx != nil {
		cx.Cleanup(inv.Close)
	}
	return inv, nil
}

// MaxPayloadBytes returns the effective byte limit enforced on encoded invite payloads.
func (i *Invites) MaxPayloadBytes() int {
	if i == nil {
		return DefaultMaxInvitePayloadBytes
	}
	return i.maxPayloadBytes
}

// PrepareCard validates card, encodes its payload as a canonical "key=value&..." string, signs it when HMACKey is set, and enforces MaxPayloadBytes.
func (i *Invites) PrepareCard(card Card) (PreparedCard, error) {
	if i == nil {
		return PreparedCard{}, errors.New("liveops: nil Invites")
	}
	if !utf8.ValidString(card.Title) || hasControlChars(card.Title) {
		return PreparedCard{}, fmt.Errorf("%w: title contains invalid UTF-8 or control characters", ErrInvalidCard)
	}
	if card.ImageID != "" && (!utf8.ValidString(card.ImageID) || hasControlChars(card.ImageID) || strings.ContainsAny(card.ImageID, " \t\r\n")) {
		return PreparedCard{}, fmt.Errorf("%w: invalid ImageID", ErrInvalidCard)
	}
	if card.InviteID != "" {
		if err := validateInviteValue(card.InviteID); err != nil {
			return PreparedCard{}, fmt.Errorf("%w: invalid InviteID: %v", ErrInvalidCard, err)
		}
	}

	payload := make(map[string]string, len(card.Payload)+1)
	maps.Copy(payload, card.Payload)
	if card.InviteID != "" {
		if existing, ok := payload[InviteIDKey]; ok && existing != "" && existing != card.InviteID {
			return PreparedCard{}, fmt.Errorf("%w: Card.InviteID %q conflicts with Payload[%q]=%q", ErrInvalidCard, card.InviteID, InviteIDKey, existing)
		}
		payload[InviteIDKey] = card.InviteID
	}
	if _, hasSig := payload[InviteSigKey]; hasSig {
		return PreparedCard{}, fmt.Errorf("%w: use Card.Signature or InvitesOptions.HMACKey instead of Payload[%q]", ErrInvalidCard, InviteSigKey)
	}

	query, err := encodeInvitePayloadWithSig(payload, card.Signature, InvitePayloadOptions{
		MaxBytes: i.maxPayloadBytes,
		HMACKey:  i.hmacKey,
	})
	if err != nil {
		return PreparedCard{}, err
	}

	return PreparedCard{
		Title:   card.Title,
		Image:   card.Image,
		ImageID: card.ImageID,
		Query:   query,
		Anchor:  card.Anchor,
	}, nil
}

// Share validates, encodes and optionally signs card, then asks the ShareHost to present it.
// Oversized or malformed cards are refused before calling the host and reported immediately to done(false, err).
func (i *Invites) Share(card Card, done func(shown bool, err error)) {
	finish := func(shown bool, err error) {
		if done != nil {
			done(shown, err)
		}
	}
	if i == nil || i.host == nil {
		finish(false, ErrShareUnavailable)
		return
	}
	prepared, err := i.PrepareCard(card)
	if err != nil {
		finish(false, err)
		return
	}
	i.host.Share(prepared, finish)
}

// SetMenuCard sets the callback supplying the card when the player uses the host's built-in share menu (such as a mini game's Forward button).
// Passing nil clears and disables the menu card. Each returned Card is validated, size-checked and signed before reaching the host.
func (i *Invites) SetMenuCard(fn func() Card) {
	if i == nil || i.host == nil {
		return
	}
	if fn == nil {
		i.host.SetMenuCard(nil)
		return
	}
	i.host.SetMenuCard(func() (PreparedCard, error) {
		prepared, err := i.PrepareCard(fn())
		if err != nil {
			i.reportError(err)
			return PreparedCard{}, err
		}
		return prepared, nil
	})
}

// Opened returns the invite that cold-started the game and consumes it so subsequent calls return (Invite{}, false).
func (i *Invites) Opened() (Invite, bool) {
	if i == nil {
		return Invite{}, false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.hasOpened || i.openedTaken {
		return Invite{}, false
	}
	i.openedTaken = true
	return i.opened.Clone(), true
}

// Incoming reactively peeks at the oldest unconsumed invite that arrived while the game was running, without consuming it.
func (i *Invites) Incoming() (Invite, bool) {
	if i == nil {
		return Invite{}, false
	}
	if i.pending != nil {
		_ = i.pending.Get()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.incoming) == 0 {
		return Invite{}, false
	}
	return i.incoming[0].Clone(), true
}

// Consume reactively pops and returns the oldest unconsumed invite that arrived while the game was running, delivering each resume invite at most once.
// Calling Consume inside a core.Effect re-runs the effect whenever a new resume invite arrives.
func (i *Invites) Consume() (Invite, bool) {
	if i == nil {
		return Invite{}, false
	}
	if i.arrived != nil {
		_ = i.arrived.Get()
	}
	i.mu.Lock()
	if len(i.incoming) == 0 {
		i.mu.Unlock()
		return Invite{}, false
	}
	next := i.incoming[0].Clone()
	i.incoming = i.incoming[1:]
	i.mu.Unlock()

	i.bumpPending()
	return next, true
}

// PendingCount reactively returns the number of unconsumed invites that arrived while the game was running.
func (i *Invites) PendingCount() int {
	if i == nil {
		return 0
	}
	if i.pending != nil {
		_ = i.pending.Get()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.incoming)
}

// OnInvite subscribes fn to invites that arrive while the game is running (warm re-open / onShow) and returns a stop function to unsubscribe.
func (i *Invites) OnInvite(fn func(Invite)) (stop func()) {
	if i == nil || fn == nil {
		return func() {}
	}
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return func() {}
	}
	i.nextListener++
	id := i.nextListener
	i.listeners = append(i.listeners, inviteListener{id: id, fn: fn})
	i.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			i.mu.Lock()
			i.listeners = slices.DeleteFunc(i.listeners, func(l inviteListener) bool { return l.id == id })
			i.mu.Unlock()
		})
	}
}

// Close unsubscribes from the ShareHost and stops further resume invite delivery. Safe to call more than once.
func (i *Invites) Close() {
	if i == nil {
		return
	}
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return
	}
	i.closed = true
	stop := i.stopHost
	i.stopHost = nil
	i.listeners = nil
	i.mu.Unlock()

	if stop != nil {
		stop()
	}
}

func (i *Invites) handleIncoming(raw LaunchInvite) {
	if raw.Source == 0 {
		raw.Source = InviteSourceResume
	}
	decoded, err := i.decodeRaw(raw)
	if err != nil {
		i.reportError(err)
		return
	}

	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return
	}
	if len(i.incoming) >= maxPendingInvites {
		i.incoming = slices.Delete(i.incoming, 0, len(i.incoming)-maxPendingInvites+1)
	}
	i.incoming = append(i.incoming, decoded)
	listeners := slices.Clone(i.listeners)
	i.mu.Unlock()

	i.bumpArrived()
	i.bumpPending()

	for _, l := range listeners {
		l.fn(decoded.Clone())
	}
}

func (i *Invites) decodeRaw(raw LaunchInvite) (Invite, error) {
	decoded, err := DecodeInvitePayload(raw.RawQuery, InvitePayloadOptions{
		MaxBytes:         i.maxPayloadBytes,
		HMACKey:          i.hmacKey,
		RequireSignature: i.requireSignature,
	})
	if err != nil {
		return Invite{}, err
	}
	return Invite{
		ID:          decoded.ID,
		Payload:     decoded.Payload,
		RawQuery:    raw.RawQuery,
		RawURL:      raw.RawURL,
		Signature:   decoded.Signature,
		Verified:    decoded.Verified,
		Scene:       raw.Scene,
		ShareTicket: raw.ShareTicket,
		Source:      raw.Source,
	}, nil
}

func (i *Invites) reportError(err error) {
	if err != nil && i.onError != nil {
		i.onError(err)
	}
}

func (i *Invites) bumpArrived() {
	if i.arrived != nil {
		i.arrived.Update(func(n int) int { return n + 1 })
	}
}

func (i *Invites) bumpPending() {
	if i.pending != nil {
		i.pending.Update(func(n int) int { return n + 1 })
	}
}
