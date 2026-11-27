package presence

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	// ErrUnavailable reports that a rich presence platform or local daemon is
	// not running, its shared library is absent, or the current target platform
	// does not support the provider.
	ErrUnavailable = errors.New("presence: provider unavailable")

	// ErrClosed reports that a Provider or Manager has already been closed.
	ErrClosed = errors.New("presence: closed")

	// ErrInvalidConfig reports that a provider configuration is missing a
	// required field or contains an invalid setting.
	ErrInvalidConfig = errors.New("presence: invalid configuration")
)

// DefaultMinInterval is the recommended minimum interval between presence
// updates when throttling rapid state transitions for desktop clients such as
// Discord (which rate-limits SET_ACTIVITY bursts).
const DefaultMinInterval = 4 * time.Second

// Activity describes what the player is currently doing in the game.
//
// All fields are optional. Providers translate the populated fields into their
// native representation: Discord's SET_ACTIVITY payload and Steam's
// ISteamFriends rich presence key-value table.
type Activity struct {
	// Level names the current level, stage, map, or area (for example "Forest 1-2").
	Level string
	// Mode names the current game mode (for example "Campaign", "Co-op", or "Ranked").
	Mode string
	// Details is the primary summary line shown in presence UIs. When empty,
	// providers derive it from Mode and Level.
	Details string
	// Status is the secondary state line shown in presence UIs (Discord's
	// "state" field and Steam's "status" key). When empty, providers derive a
	// readable string from Level and Mode.
	Status string

	// PartyID identifies the player's current party or lobby. Maps to Discord's
	// party.id and Steam's "steam_player_group" key.
	PartyID string
	// PartySize is the current number of players in the party (0 if solo).
	// Maps to Discord's party.size[0] and Steam's "steam_player_group_size" key.
	PartySize int
	// PartyMax is the maximum capacity of the party (0 if unspecified).
	// Maps to Discord's party.size[1].
	PartyMax int

	// StartTime marks when the current activity began (shown as elapsed time on
	// Discord).
	StartTime time.Time
	// EndTime marks when the current activity ends (shown as remaining time on
	// Discord).
	EndTime time.Time

	// LargeImage is the Discord artwork asset key or URL for the large icon.
	LargeImage string
	// LargeText is the hover tooltip for LargeImage.
	LargeText string
	// SmallImage is the Discord artwork asset key or URL for the small badge icon.
	SmallImage string
	// SmallText is the hover tooltip for SmallImage.
	SmallText string

	// JoinSecret is an opaque session join token. Maps to Discord's
	// secrets.join and Steam's "connect" key when Custom["connect"] is unset.
	JoinSecret string

	// Custom holds additional provider-specific key-value pairs, primarily for
	// Steam rich presence localization tokens such as "steam_display",
	// "connect", "steam_player_group", "steam_player_group_size", or custom
	// token parameters.
	Custom map[string]string
}

// Clone returns a copy of a with an independent Custom map so later mutations
// do not alias stored state.
func (a Activity) Clone() Activity {
	if a.Custom != nil {
		a.Custom = maps.Clone(a.Custom)
	}
	return a
}

// Equal reports whether a and b represent the same rich presence state.
func (a Activity) Equal(b Activity) bool {
	return a.Level == b.Level &&
		a.Mode == b.Mode &&
		a.Details == b.Details &&
		a.Status == b.Status &&
		a.PartyID == b.PartyID &&
		a.PartySize == b.PartySize &&
		a.PartyMax == b.PartyMax &&
		a.StartTime.Equal(b.StartTime) &&
		a.EndTime.Equal(b.EndTime) &&
		a.LargeImage == b.LargeImage &&
		a.LargeText == b.LargeText &&
		a.SmallImage == b.SmallImage &&
		a.SmallText == b.SmallText &&
		a.JoinSecret == b.JoinSecret &&
		maps.Equal(a.Custom, b.Custom)
}

// Summary returns a single human-readable status string derived from a, using
// Status if set, otherwise Details, otherwise combining Mode and Level.
func (a Activity) Summary() string {
	switch {
	case a.Status != "":
		return a.Status
	case a.Details != "":
		return a.Details
	case a.Mode != "" && a.Level != "":
		return a.Mode + " - " + a.Level
	case a.Level != "":
		return a.Level
	default:
		return a.Mode
	}
}

// discordLines resolves the (details, state) lines for Discord's activity
// payload from explicit fields and Mode/Level fallbacks.
func (a Activity) discordLines() (details, state string) {
	details = a.Details
	state = a.Status
	if details == "" && a.Mode != "" {
		details = a.Mode
	}
	if state == "" && a.Level != "" {
		state = a.Level
	} else if details == "" && a.Level != "" {
		details = a.Level
	}
	return details, state
}

// Provider publishes rich presence state to a single platform (such as Steam or
// Discord).
type Provider interface {
	// Set publishes activity to the platform.
	Set(ctx context.Context, activity Activity) error
	// Clear removes any active rich presence from the platform.
	Clear(ctx context.Context) error
	// Close releases platform resources held by the provider.
	Close() error
}

type namedProvider interface {
	Name() string
}

func providerName(p Provider, idx int) string {
	if np, ok := p.(namedProvider); ok && np.Name() != "" {
		return np.Name()
	}
	return fmt.Sprintf("provider[%d]", idx)
}

// ManagerConfig configures a [Manager].
type ManagerConfig struct {
	// MinInterval enforces a minimum duration between consecutive provider
	// pushes. When zero, every distinct update is pushed immediately. When
	// positive, updates arriving within MinInterval of the last push are
	// coalesced and kept pending until Poll is called after MinInterval elapses
	// or Flush is called.
	MinInterval time.Duration

	// Now returns the current time for MinInterval throttling. When nil,
	// time.Now is used.
	Now func() time.Time

	// OnError, if non-nil, is called whenever an individual provider returns an
	// error during Update, SetLevel, Clear, Poll, or Flush (including
	// ErrUnavailable). It is invoked without holding the Manager's internal
	// lock.
	OnError func(provider string, err error)
}

// Manager fans rich presence updates out to one or more [Provider] backends
// (for example Steam and Discord simultaneously), deduplicates unchanged
// activities, optionally throttles bursts via [ManagerConfig.MinInterval], and
// degrades gracefully when a platform daemon is unavailable.
//
// All methods on Manager are safe for concurrent use.
type Manager struct {
	mu          sync.Mutex
	providers   []Provider
	minInterval time.Duration
	now         func() time.Time
	onError     func(provider string, err error)

	closed bool

	// Desired state tracks the latest activity requested by the caller.
	desired   Activity
	hasActive bool

	// Sent state tracks what was last pushed to providers.
	sent    Activity
	hasSent bool

	// Pending state holds a coalesced update deferred by MinInterval.
	pending    Activity
	hasPending bool

	lastPush    time.Time
	hasLastPush bool
}

// NewManager constructs a Manager that fans out updates to providers. Nil
// entries in providers are ignored.
func NewManager(cfg ManagerConfig, providers ...Provider) *Manager {
	filtered := make([]Provider, 0, len(providers))
	for _, p := range providers {
		if p != nil {
			filtered = append(filtered, p)
		}
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	minInterval := cfg.MinInterval
	if minInterval < 0 {
		minInterval = 0
	}
	return &Manager{
		providers:   filtered,
		minInterval: minInterval,
		now:         nowFn,
		onError:     cfg.OnError,
	}
}

// Update publishes activity to all configured providers.
//
// If activity equals the last applied state (and no earlier update is pending),
// Update is a no-op and returns nil. When [ManagerConfig.MinInterval] is
// positive and has not yet elapsed since the previous push, Update coalesces
// activity as pending and returns nil; call [Manager.Poll] once the interval
// elapses or [Manager.Flush] to push immediately.
//
// If at least one provider succeeds and the remaining providers return
// [ErrUnavailable], Update returns nil so a missing optional client (such as
// Discord not running) does not fail the update. If all providers return
// [ErrUnavailable], Update returns [ErrUnavailable]. Any non-unavailable
// provider errors are joined and returned while still updating all other
// providers.
func (m *Manager) Update(ctx context.Context, activity Activity) error {
	if ctx == nil {
		ctx = context.Background()
	}
	act := activity.Clone()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.hasActive && !m.hasPending && m.hasSent && m.sent.Equal(act) {
		m.mu.Unlock()
		return nil
	}
	m.desired = act
	m.hasActive = true

	if m.minInterval > 0 && m.hasLastPush && m.now().Sub(m.lastPush) < m.minInterval {
		if m.hasSent && m.sent.Equal(act) {
			m.hasPending = false
			m.pending = Activity{}
		} else {
			m.pending = act
			m.hasPending = true
		}
		m.mu.Unlock()
		return nil
	}

	m.hasPending = false
	m.pending = Activity{}
	providers := m.providers
	onError := m.onError
	m.mu.Unlock()

	err := m.fanoutSet(ctx, providers, onError, act)

	m.mu.Lock()
	if !m.closed {
		m.lastPush = m.now()
		m.hasLastPush = true
		if err == nil || errors.Is(err, ErrUnavailable) {
			m.sent = act
			m.hasSent = true
		}
	}
	m.mu.Unlock()

	return err
}

// SetLevel updates the Level field of the current activity while preserving its
// other fields (such as Mode, party, assets, timestamps, and Custom keys), then
// pushes the update to all configured providers.
func (m *Manager) SetLevel(ctx context.Context, level string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	next := m.desired.Clone()
	m.mu.Unlock()

	next.Level = level
	return m.Update(ctx, next)
}

// SetMode updates the Mode field of the current activity while preserving its
// other fields, then pushes the update to all configured providers.
func (m *Manager) SetMode(ctx context.Context, mode string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	next := m.desired.Clone()
	m.mu.Unlock()

	next.Mode = mode
	return m.Update(ctx, next)
}

// SetParty updates the PartyID, PartySize, and PartyMax fields of the current
// activity while preserving its other fields, then pushes the update to all
// configured providers.
func (m *Manager) SetParty(ctx context.Context, id string, size, max int) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	next := m.desired.Clone()
	m.mu.Unlock()

	next.PartyID = id
	next.PartySize = size
	next.PartyMax = max
	return m.Update(ctx, next)
}

// Clear removes active rich presence on all configured providers and cancels
// any pending throttled update.
func (m *Manager) Clear(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	wasEmpty := !m.hasActive && !m.hasSent && !m.hasPending
	m.desired = Activity{}
	m.hasActive = false
	m.pending = Activity{}
	m.hasPending = false
	providers := m.providers
	onError := m.onError
	m.mu.Unlock()

	if wasEmpty {
		return nil
	}

	err := m.fanoutClear(ctx, providers, onError)

	m.mu.Lock()
	if !m.closed {
		m.sent = Activity{}
		m.hasSent = false
		m.lastPush = m.now()
		m.hasLastPush = true
	}
	m.mu.Unlock()

	return err
}

// Poll pushes a coalesced pending update if [ManagerConfig.MinInterval] has
// elapsed since the previous push. Games using MinInterval can call Poll from
// their update loop or periodic tick.
func (m *Manager) Poll(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if !m.hasPending {
		m.mu.Unlock()
		return nil
	}
	if m.minInterval > 0 && m.hasLastPush && m.now().Sub(m.lastPush) < m.minInterval {
		m.mu.Unlock()
		return nil
	}
	act := m.pending.Clone()
	m.pending = Activity{}
	m.hasPending = false
	providers := m.providers
	onError := m.onError
	m.mu.Unlock()

	err := m.fanoutSet(ctx, providers, onError, act)

	m.mu.Lock()
	if !m.closed {
		m.lastPush = m.now()
		m.hasLastPush = true
		if err == nil || errors.Is(err, ErrUnavailable) {
			m.sent = act
			m.hasSent = true
		}
	}
	m.mu.Unlock()

	return err
}

// Flush immediately pushes any pending coalesced update regardless of
// [ManagerConfig.MinInterval].
func (m *Manager) Flush(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if !m.hasPending {
		m.mu.Unlock()
		return nil
	}
	act := m.pending.Clone()
	m.pending = Activity{}
	m.hasPending = false
	providers := m.providers
	onError := m.onError
	m.mu.Unlock()

	err := m.fanoutSet(ctx, providers, onError, act)

	m.mu.Lock()
	if !m.closed {
		m.lastPush = m.now()
		m.hasLastPush = true
		if err == nil || errors.Is(err, ErrUnavailable) {
			m.sent = act
			m.hasSent = true
		}
	}
	m.mu.Unlock()

	return err
}

// Activity returns a copy of the latest desired activity and whether rich
// presence is currently active.
func (m *Manager) Activity() (Activity, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasActive {
		return Activity{}, false
	}
	return m.desired.Clone(), true
}

// Pending reports whether a rate-limited update is currently waiting to be
// pushed by [Manager.Poll] or [Manager.Flush].
func (m *Manager) Pending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasPending
}

// Close closes every configured provider and marks the Manager as closed.
// Subsequent calls to Close are no-ops and return nil.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.hasPending = false
	m.pending = Activity{}
	providers := m.providers
	m.mu.Unlock()

	var errs []error
	for i, p := range providers {
		if err := p.Close(); err != nil && !errors.Is(err, ErrClosed) {
			errs = append(errs, fmt.Errorf("presence: %s: %w", providerName(p, i), err))
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) fanoutSet(ctx context.Context, providers []Provider, onError func(string, error), act Activity) error {
	if len(providers) == 0 {
		return ErrUnavailable
	}
	return fanout(providers, onError, func(p Provider) error {
		return p.Set(ctx, act.Clone())
	})
}

func (m *Manager) fanoutClear(ctx context.Context, providers []Provider, onError func(string, error)) error {
	if len(providers) == 0 {
		return ErrUnavailable
	}
	return fanout(providers, onError, func(p Provider) error {
		return p.Clear(ctx)
	})
}

func fanout(providers []Provider, onError func(string, error), call func(Provider) error) error {
	var (
		succeeded   int
		unavailable int
		realErrs    []error
	)
	for i, p := range providers {
		err := call(p)
		if err == nil {
			succeeded++
			continue
		}
		name := providerName(p, i)
		if onError != nil {
			onError(name, err)
		}
		if errors.Is(err, ErrUnavailable) {
			unavailable++
			continue
		}
		realErrs = append(realErrs, fmt.Errorf("presence: %s: %w", name, err))
	}
	if len(realErrs) > 0 {
		return errors.Join(realErrs...)
	}
	if succeeded == 0 && unavailable > 0 {
		return ErrUnavailable
	}
	return nil
}

// truncateUTF8 trims s to at most maxBytes bytes without splitting a multi-byte
// UTF-8 rune.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	n := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		n = i
	}
	// If the rune starting at n fits within maxBytes, include it.
	if n < len(s) {
		_, size := utf8.DecodeRuneInString(s[n:])
		if n+size <= maxBytes {
			n += size
		}
	}
	return s[:n]
}

// stripNUL removes embedded NUL bytes that C-string APIs would truncate on.
func stripNUL(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}
