package liveops

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultReviewPromptKey is the core.Storage key under which ReviewPrompt persists its versioned JSON state when ReviewPromptOptions.StorageKey is empty.
	DefaultReviewPromptKey = "liveops.review_prompt"

	// ReviewPromptStateVersion is the schema version of the JSON state persisted by ReviewPrompt.
	ReviewPromptStateVersion = 1

	// DefaultReviewMinSessions is the default minimum number of sessions before MaybeAsk may request a review.
	DefaultReviewMinSessions = 3

	// DefaultReviewMinDays is the default minimum number of days since first launch before MaybeAsk may request a review.
	DefaultReviewMinDays = 3

	// DefaultReviewMinPositiveMoments is the default minimum number of positive moments in the current app version before MaybeAsk may request a review.
	DefaultReviewMinPositiveMoments = 3

	// DefaultReviewCooldown is the default minimum duration between review requests across app versions.
	// 122 days ensures at most three prompts in any rolling 365-day window, staying within Apple's StoreKit limit.
	DefaultReviewCooldown = 122 * 24 * time.Hour
)

// ReviewMoment classifies an in-game outcome reported to ReviewPrompt.Moment.
type ReviewMoment int

const (
	// MomentPositive records a rewarding event such as clearing a level or setting a personal record.
	MomentPositive ReviewMoment = iota + 1
	// MomentNegative records a frustrating event such as losing a level, failing a purchase or hitting an error.
	// Recording a negative moment suppresses review prompts for the rest of the current session.
	MomentNegative
)

// String returns "positive", "negative" or "unknown".
func (m ReviewMoment) String() string {
	switch m {
	case MomentPositive:
		return "positive"
	case MomentNegative:
		return "negative"
	default:
		return "unknown"
	}
}

// ReviewPromptOptions configures NewReviewPrompt.
type ReviewPromptOptions struct {
	// AppVersion is the current release version (for example project.File.AppVersion).
	// ReviewPrompt asks at most once per AppVersion and resets per-version positive moments when AppVersion changes so a new release earns fresh engagement before asking again.
	AppVersion string

	// StorageKey is the core.Storage key holding the versioned JSON state.
	// DefaultReviewPromptKey is used when empty.
	StorageKey string

	// MinSessions is the minimum number of sessions before MaybeAsk may prompt.
	// Values <= 0 use DefaultReviewMinSessions (3). Stores advise against prompting in the first session.
	MinSessions int

	// MinDays is the minimum number of whole 24-hour days since first launch before MaybeAsk may prompt.
	// Values <= 0 use DefaultReviewMinDays (3).
	MinDays int

	// MinPositiveMoments is the minimum number of positive moments recorded in the current AppVersion before MaybeAsk may prompt.
	// Values <= 0 use DefaultReviewMinPositiveMoments (3).
	MinPositiveMoments int

	// Cooldown is the minimum time between store review requests across all app versions.
	// Values <= 0 use DefaultReviewCooldown (122 days).
	Cooldown time.Duration

	// Now returns the current wall-clock time; time.Now is used when nil.
	// Pass cx.WallTime in NSGo apps so nsgotest.WallTime and app.FastForward control the clock.
	Now func() time.Time

	// Suppress, when non-nil and returning true, prevents MaybeAsk from requesting a review
	// (for example while a purchase sheet, ad flow or modal dialog is open).
	Suppress func() bool
}

type reviewPromptState struct {
	Version             int    `json:"version"`
	AppVersion          string `json:"appVersion"`
	FirstLaunch         string `json:"firstLaunch"`
	Sessions            int    `json:"sessions"`
	PositiveMoments     int    `json:"positiveMoments"`
	LastPromptedAt      string `json:"lastPromptedAt,omitempty"`
	LastPromptedVersion string `json:"lastPromptedVersion,omitempty"`
}

type parsedReviewState struct {
	raw            reviewPromptState
	firstLaunch    time.Time
	lastPromptedAt time.Time
}

// ReviewPrompt decides when to call core.Scope.RequestReview using persisted session, elapsed-day, positive-moment, cooldown and once-per-version rules.
//
// State is stored in core.Storage as strict versioned JSON text and fails closed on any corrupt, unrecognised or future-version payload without overwriting it.
// On platforms where core.Env.CanRequestReview is false, MaybeAsk is a no-op returning false so a game can call it unconditionally.
type ReviewPrompt struct {
	mu                 sync.Mutex
	store              core.Storage
	hasStore           bool
	key                string
	appVersion         string
	minSessions        int
	minDays            int
	minPositiveMoments int
	cooldown           time.Duration
	now                func() time.Time
	suppress           func() bool

	corrupt             bool
	negativeThisSession bool
}

// NewReviewPrompt creates a review prompt policy backed by store and records the start of a new session.
// If store is the zero core.Storage, or if the persisted JSON state is corrupt or unrecognised, the policy fails closed and MaybeAsk never prompts.
func NewReviewPrompt(store core.Storage, opts ReviewPromptOptions) *ReviewPrompt {
	key := opts.StorageKey
	if key == "" {
		key = DefaultReviewPromptKey
	}
	minSessions := opts.MinSessions
	if minSessions <= 0 {
		minSessions = DefaultReviewMinSessions
	}
	minDays := opts.MinDays
	if minDays <= 0 {
		minDays = DefaultReviewMinDays
	}
	minPositive := opts.MinPositiveMoments
	if minPositive <= 0 {
		minPositive = DefaultReviewMinPositiveMoments
	}
	cooldown := opts.Cooldown
	if cooldown <= 0 {
		cooldown = DefaultReviewCooldown
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	r := &ReviewPrompt{
		store:              store,
		hasStore:           store != (core.Storage{}),
		key:                key,
		appVersion:         opts.AppVersion,
		minSessions:        minSessions,
		minDays:            minDays,
		minPositiveMoments: minPositive,
		cooldown:           cooldown,
		now:                nowFn,
		suppress:           opts.Suppress,
	}
	if !r.hasStore {
		r.corrupt = true
		return r
	}

	st, exists, ok := r.loadStateLocked()
	if !ok {
		r.corrupt = true
		return r
	}
	now := r.now().UTC()
	if !exists {
		st = parsedReviewState{
			raw: reviewPromptState{
				Version:     ReviewPromptStateVersion,
				AppVersion:  r.appVersion,
				FirstLaunch: formatReviewTime(now),
				Sessions:    1,
			},
			firstLaunch: now,
		}
	} else {
		if st.raw.AppVersion != r.appVersion {
			st.raw.AppVersion = r.appVersion
			st.raw.PositiveMoments = 0
		}
		st.raw.Sessions++
	}
	_ = r.saveStateLocked(st.raw)
	return r
}

// Moment records a positive or negative gameplay moment.
// MomentPositive increments the current version's positive-moment counter in storage.
// MomentNegative suppresses prompts for the remainder of the current session.
// Unrecognised moment values are ignored.
func (r *ReviewPrompt) Moment(kind ReviewMoment) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.corrupt || !r.hasStore {
		return
	}
	switch kind {
	case MomentNegative:
		r.negativeThisSession = true
	case MomentPositive:
		st, exists, ok := r.loadStateLocked()
		if !ok {
			r.corrupt = true
			return
		}
		if !exists {
			return
		}
		st.raw.PositiveMoments++
		_ = r.saveStateLocked(st.raw)
	}
}

// RecordSession starts a new session on an existing ReviewPrompt (for example when a mobile app returns to the foreground after a long pause), clearing any session-scoped negative moment suppression and incrementing the persisted session count.
func (r *ReviewPrompt) RecordSession() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.corrupt || !r.hasStore {
		return
	}
	st, exists, ok := r.loadStateLocked()
	if !ok {
		r.corrupt = true
		return
	}
	r.negativeThisSession = false
	if !exists {
		now := r.now().UTC()
		st = parsedReviewState{
			raw: reviewPromptState{
				Version:     ReviewPromptStateVersion,
				AppVersion:  r.appVersion,
				FirstLaunch: formatReviewTime(now),
				Sessions:    1,
			},
			firstLaunch: now,
		}
	} else {
		if st.raw.AppVersion != r.appVersion {
			st.raw.AppVersion = r.appVersion
			st.raw.PositiveMoments = 0
		}
		st.raw.Sessions++
	}
	_ = r.saveStateLocked(st.raw)
}

// Eligible reports whether all policy rules currently allow asking for a review (independent of whether the platform supports core.Scope.RequestReview).
// It returns false on corrupt state, after a negative moment in the current session, while suppressed, or before thresholds and cooldown are satisfied.
func (r *ReviewPrompt) Eligible() bool {
	if r == nil {
		return false
	}
	if r.isSuppressed() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.corrupt || !r.hasStore || r.negativeThisSession {
		return false
	}
	st, exists, ok := r.loadStateLocked()
	if !ok {
		r.corrupt = true
		return false
	}
	if !exists {
		return false
	}
	return r.policyAllowsLocked(st, r.now().UTC())
}

// MaybeAsk requests an in-app store rating through cx.RequestReview when the platform supports it (cx.Env().CanRequestReview()) and all policy rules allow it, returning whether it asked.
//
// On platforms without a store prompt, MaybeAsk is a no-op returning false without modifying state.
// When asking, ReviewPrompt persists the prompt timestamp and version before calling cx.RequestReview so a storage failure fails closed and never causes repeated prompts.
func (r *ReviewPrompt) MaybeAsk(cx *core.Scope) bool {
	if r == nil || cx == nil {
		return false
	}
	if !core.Untracked(cx, cx.Env().CanRequestReview) {
		return false
	}
	if r.isSuppressed() {
		return false
	}

	r.mu.Lock()
	if r.corrupt || !r.hasStore || r.negativeThisSession {
		r.mu.Unlock()
		return false
	}
	st, exists, ok := r.loadStateLocked()
	if !ok {
		r.corrupt = true
		r.mu.Unlock()
		return false
	}
	if !exists {
		r.mu.Unlock()
		return false
	}
	now := r.now().UTC()
	if !r.policyAllowsLocked(st, now) {
		r.mu.Unlock()
		return false
	}
	st.raw.LastPromptedAt = formatReviewTime(now)
	st.raw.LastPromptedVersion = r.appVersion
	if err := r.saveStateLocked(st.raw); err != nil {
		r.mu.Unlock()
		return false
	}
	r.mu.Unlock()

	cx.RequestReview()
	return true
}

func (r *ReviewPrompt) isSuppressed() bool {
	r.mu.Lock()
	fn := r.suppress
	r.mu.Unlock()
	return fn != nil && fn()
}

func (r *ReviewPrompt) policyAllowsLocked(st parsedReviewState, now time.Time) bool {
	// Once per app version.
	if !st.lastPromptedAt.IsZero() && st.raw.LastPromptedVersion == r.appVersion {
		return false
	}
	// Cooldown across versions (fails closed if clock moved backwards).
	if !st.lastPromptedAt.IsZero() && now.Sub(st.lastPromptedAt) < r.cooldown {
		return false
	}
	if st.raw.Sessions < r.minSessions {
		return false
	}
	minAge := time.Duration(r.minDays) * 24 * time.Hour
	if now.Sub(st.firstLaunch) < minAge {
		return false
	}
	if st.raw.PositiveMoments < r.minPositiveMoments {
		return false
	}
	return true
}

func (r *ReviewPrompt) loadStateLocked() (st parsedReviewState, exists bool, ok bool) {
	raw, found := r.store.GetItem(r.key)
	if !found {
		return parsedReviewState{}, false, true
	}
	parsed, err := decodeReviewState(raw)
	if err != nil {
		return parsedReviewState{}, true, false
	}
	return parsed, true, true
}

func (r *ReviewPrompt) saveStateLocked(st reviewPromptState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return r.store.SetItem(r.key, string(data))
}

func formatReviewTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func decodeReviewState(raw string) (parsedReviewState, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var st reviewPromptState
	if err := dec.Decode(&st); err != nil {
		return parsedReviewState{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	if st.Version != ReviewPromptStateVersion {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	if st.Sessions < 0 || st.PositiveMoments < 0 {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	if st.FirstLaunch == "" {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	firstLaunch, err := time.Parse(time.RFC3339Nano, st.FirstLaunch)
	if err != nil {
		return parsedReviewState{}, err
	}
	var lastPromptedAt time.Time
	if st.LastPromptedAt != "" {
		lastPromptedAt, err = time.Parse(time.RFC3339Nano, st.LastPromptedAt)
		if err != nil {
			return parsedReviewState{}, err
		}
	} else if st.LastPromptedVersion != "" {
		return parsedReviewState{}, io.ErrUnexpectedEOF
	}
	return parsedReviewState{
		raw:            st,
		firstLaunch:    firstLaunch.UTC(),
		lastPromptedAt: lastPromptedAt.UTC(),
	}, nil
}
