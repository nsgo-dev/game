package liveops

import (
	"errors"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultConsentKey is the core.Storage key used by NewAnalytics when key is empty.
const DefaultConsentKey = "liveops.analytics.consent"

// ErrInvalidConsent is returned by Analytics.SetConsent when given an unknown ConsentState value.
var ErrInvalidConsent = errors.New("liveops: invalid consent state")

// ConsentState represents the player's analytics consent choice.
type ConsentState int

const (
	// ConsentUnknown is the default before the player chooses. No events are sent or buffered.
	ConsentUnknown ConsentState = iota
	// ConsentDenied records that the player declined or revoked analytics. No events are sent or buffered.
	ConsentDenied
	// ConsentGranted records explicit opt-in consent. Only in this state may events be dispatched to the provider.
	ConsentGranted
)

// String returns "unknown", "denied" or "granted".
func (s ConsentState) String() string {
	switch s {
	case ConsentDenied:
		return "denied"
	case ConsentGranted:
		return "granted"
	default:
		return "unknown"
	}
}

func parseConsent(raw string) ConsentState {
	switch raw {
	case "granted":
		return ConsentGranted
	case "denied":
		return ConsentDenied
	default:
		// Any unrecognized or missing value fails closed so tracking never starts without explicit opt-in.
		return ConsentUnknown
	}
}

func validConsent(s ConsentState) bool {
	return s == ConsentUnknown || s == ConsentDenied || s == ConsentGranted
}

// Event is one analytics event delivered to an AnalyticsProvider after explicit consent.
type Event struct {
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties,omitempty"`
	Time       time.Time      `json:"time"`
}

// AnalyticsProvider receives analytics events after explicit opt-in consent.
// Provider implementations live outside the core game module so no third-party analytics SDK is linked unless chosen by the app.
type AnalyticsProvider interface {
	Send(Event) error
	Flush() error
}

// ConsentRevoker is optionally implemented by providers that buffer events. Purge is called when consent is revoked so already queued events cannot be sent later.
type ConsentRevoker interface {
	Purge() error
}

// Analytics gates event tracking on explicit player consent and optionally persists the consent choice in core.Storage.
//
// Events tracked while Consent is ConsentUnknown or ConsentDenied are dropped immediately — they are never sent and never buffered for later replay.
type Analytics struct {
	mu       sync.RWMutex
	store    core.Storage
	hasStore bool
	key      string
	consent  ConsentState
	provider AnalyticsProvider
	now      func() time.Time
}

// NewAnalytics returns an Analytics gate backed by store under key (DefaultConsentKey when empty) and dispatching consented events to provider.
// It loads any previously persisted consent choice from store immediately, failing closed to ConsentUnknown when absent or unrecognized.
func NewAnalytics(store core.Storage, key string, provider AnalyticsProvider) *Analytics {
	if key == "" {
		key = DefaultConsentKey
	}
	initial := ConsentUnknown
	if raw, ok := store.GetItem(key); ok {
		initial = parseConsent(raw)
	}
	return &Analytics{
		store:    store,
		hasStore: true,
		key:      key,
		consent:  initial,
		provider: provider,
	}
}

// NewMemoryAnalytics returns an Analytics gate that keeps its consent state in memory only, starting in ConsentUnknown.
func NewMemoryAnalytics(provider AnalyticsProvider) *Analytics {
	return &Analytics{
		consent:  ConsentUnknown,
		provider: provider,
	}
}

// SetClock sets the clock function used for Event.Time; time.Now is used when nil.
func (a *Analytics) SetClock(now func() time.Time) {
	a.mu.Lock()
	a.now = now
	a.mu.Unlock()
}

// SetProvider replaces the AnalyticsProvider that receives consented events.
func (a *Analytics) SetProvider(provider AnalyticsProvider) {
	a.mu.Lock()
	a.provider = provider
	a.mu.Unlock()
}

// Consent returns the current ConsentState.
// When backed by core.Storage, reading Consent inside a reactive computation subscribes it to storage changes.
func (a *Analytics) Consent() ConsentState { return a.consentState() }

// consentState resolves consent from storage when one backs the instance, so
// Track and Flush see a revocation or grant made through another instance or
// by clearing storage, not a stale in-memory cache.
func (a *Analytics) consentState() ConsentState {
	a.mu.RLock()
	hasStore := a.hasStore
	store := a.store
	key := a.key
	cached := a.consent
	a.mu.RUnlock()
	if !hasStore {
		return cached
	}
	if raw, ok := store.GetItem(key); ok {
		return parseConsent(raw)
	}
	return ConsentUnknown
}

// SetConsent updates the consent choice and persists it when backed by core.Storage.
// Revoking consent (ConsentDenied or ConsentUnknown) immediately stops all subsequent Track calls.
func (a *Analytics) SetConsent(state ConsentState) error {
	if !validConsent(state) {
		return ErrInvalidConsent
	}
	a.mu.Lock()
	previous := a.consentStateLocked()
	if a.hasStore {
		if state == ConsentUnknown {
			a.store.RemoveItem(a.key)
		} else if err := a.store.SetItem(a.key, state.String()); err != nil {
			// If persisting a revocation fails (for example storage is full), clear any stored grant so tracking stops immediately.
			if state == ConsentDenied {
				a.store.RemoveItem(a.key)
				a.consent = ConsentDenied
			}
			a.mu.Unlock()
			return err
		}
	}
	a.consent = state
	provider := a.provider
	a.mu.Unlock()
	if state != ConsentGranted && previous == ConsentGranted {
		if revoker, ok := provider.(ConsentRevoker); ok && revoker != nil {
			return revoker.Purge()
		}
	}
	return nil
}

func (a *Analytics) consentStateLocked() ConsentState {
	if a.hasStore {
		if raw, ok := a.store.GetItem(a.key); ok {
			return parseConsent(raw)
		}
		return ConsentUnknown
	}
	return a.consent
}

// Track dispatches an event to the configured provider only when Consent is ConsentGranted.
// When consent is ConsentUnknown or ConsentDenied, or when no provider is configured, Track drops the event immediately without buffering and returns false.
// When consent is granted, props is defensively copied before dispatch and Track returns whether provider.Send succeeded.
func (a *Analytics) Track(name string, props map[string]any) bool {
	consent := a.consentState()
	a.mu.RLock()
	provider := a.provider
	nowFn := a.now
	a.mu.RUnlock()

	if consent != ConsentGranted || provider == nil {
		return false
	}

	var ts time.Time
	if nowFn != nil {
		ts = nowFn()
	} else {
		ts = time.Now()
	}

	ev := Event{
		Name:       name,
		Properties: cloneMap(props),
		Time:       ts,
	}
	return provider.Send(ev) == nil
}

// Flush flushes the configured provider when Consent is ConsentGranted.
// When consent is not granted or no provider is set, Flush is a no-op returning nil.
func (a *Analytics) Flush() error {
	consent := a.consentState()
	a.mu.RLock()
	provider := a.provider
	a.mu.RUnlock()

	if consent != ConsentGranted || provider == nil {
		return nil
	}
	return provider.Flush()
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = cloneValue(v)
	}
	return dst
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		if len(x) == 0 {
			return nil
		}
		out := make([]any, len(x))
		for i, elem := range x {
			out[i] = cloneValue(elem)
		}
		return out
	default:
		return v
	}
}
