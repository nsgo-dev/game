package liveops

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// DefaultRemindersKey is the core.Storage key prefix under which Reminders persists the player's enabled setting, custom quiet hours and active reminder IDs when RemindersOptions.StorageKey is empty.
const DefaultRemindersKey = "liveops.reminders"

// DefaultReminderPrefix is the prefix Reminders prepends to reminder IDs passed to core.Scope.ShowNotification when RemindersOptions.Prefix is empty, keeping reminder notifications separate from the app's other notifications.
const DefaultReminderPrefix = "liveops.reminder:"

var (
	// ErrReminderNotFound is returned by Schedule and ScheduleWith when id is not declared in RemindersConfig.
	ErrReminderNotFound = errors.New("liveops: reminder not found")
	// ErrInvalidSchedule is returned by ScheduleWith when ScheduleOptions sets both Delay and At, sets a negative Delay, or schedules a non-repeating reminder in the past.
	ErrInvalidSchedule = errors.New("liveops: invalid reminder schedule")
)

// ReminderPress is the event reported to RemindersOptions.OnPress when the player presses a delivered reminder notification.
type ReminderPress struct {
	// ID is the reminder's ID from RemindersConfig (without RemindersOptions.Prefix).
	ID string
}

// ScheduleOptions overrides parts of a declared ReminderDef for a single ScheduleWith call.
type ScheduleOptions struct {
	// Delay, when positive, overrides the reminder's declared Delay or At with a relative delay from now. Mutually exclusive with At.
	Delay time.Duration
	// At, when non-zero, overrides the reminder's declared Delay or At with a specific wall-clock time. Mutually exclusive with Delay.
	At time.Time
	// Title, when non-empty, overrides the localized title for this scheduled reminder (for example when interpolating parameters with l10n).
	Title string
	// Body, when non-empty, overrides the localized body for this scheduled reminder.
	Body string
}

// RemindersOptions configures NewReminders.
type RemindersOptions struct {
	// Config holds the game's declared reminders and optional default quiet hours (loaded with ParseReminders or LoadRemindersFS).
	Config *RemindersConfig
	// Localize resolves a reminder's Title and Body localization keys into display text.
	// When nil, keys are used as written.
	Localize func(key string) string
	// Storage persists the player's enabled toggle, custom quiet hours and active reminder IDs across runs.
	// When zero, cx.Storage() is used.
	Storage core.Storage
	// StorageKey is the prefix for keys written to Storage; DefaultRemindersKey is used when empty.
	StorageKey string
	// QuietHours overrides Config.QuietHours as the default quiet-hours window when the player has not saved a custom window.
	QuietHours *QuietHours
	// DefaultDisabled makes reminders start disabled until the player enables them with SetEnabled(true).
	DefaultDisabled bool
	// Prefix is prepended to reminder IDs passed to core.Scope.ShowNotification; DefaultReminderPrefix is used when empty.
	Prefix string
	// OnPress runs when the player presses a delivered reminder notification.
	OnPress func(ReminderPress)
}

type scheduledReminder struct {
	at            time.Time
	repeat        ReminderRepeat
	titleOverride string
	bodyOverride  string
}

// Reminders schedules, cancels and reschedules local re-engagement notifications over NSGo's core.Scope.ShowNotification.
//
// Reminders never asks for notification permission at launch: it requests permission only when the game first calls Schedule (or ScheduleWith) or an explicit RequestPermission. It honors a persisted player toggle (Enabled/SetEnabled), local-time QuietHours, and each reminder's OnOpen rule (cancel, reschedule or keep) when the app comes to the front.
//
// Remote push notifications are not part of Reminders and are unavailable everywhere until NSGo ships remote push (nsgo-dev/nsgo#59, nsgo-dev/nsgo#1323).
//
// Reminders methods run on the scope's goroutine, as NSGo's callbacks do.
type Reminders struct {
	cx           *core.Scope
	storage      core.Storage
	storageKey   string
	prefix       string
	localize     func(string) string
	onPress      func(ReminderPress)
	defaultQuiet *QuietHours
	defs         map[string]ReminderDef
	defOrder     []string

	rev        *core.Signal[int]
	perm       *core.Signal[core.NotificationPermission]
	permKnown  bool
	permAsking bool
	permWait   []func(core.NotificationPermission)

	enabled    bool
	quietHours *QuietHours
	scheduled  map[string]scheduledReminder

	gen uint64
	seq map[string]uint64
}

// NewReminders creates a local reminder manager bound to cx.
// It never prompts for or queries notification permission at launch; it loads persisted player settings and reconciles any previously scheduled reminders with their OnOpen rules when the app starts in the foreground.
func NewReminders(cx *core.Scope, opts RemindersOptions) (*Reminders, error) {
	if cx == nil {
		return nil, errors.New("liveops: reminders need a core.Scope")
	}
	var cfg *RemindersConfig
	if opts.Config != nil {
		if err := ValidateRemindersConfig(opts.Config); err != nil {
			return nil, err
		}
		cfg = opts.Config.Clone()
	}
	if opts.QuietHours != nil {
		if err := opts.QuietHours.Validate(); err != nil {
			return nil, err
		}
	}

	storage := opts.Storage
	if storage == (core.Storage{}) {
		storage = cx.Storage()
	}
	storageKey := opts.StorageKey
	if storageKey == "" {
		storageKey = DefaultRemindersKey
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = DefaultReminderPrefix
	}

	var defaultQuiet *QuietHours
	if opts.QuietHours != nil {
		defaultQuiet = opts.QuietHours.Clone()
	} else if cfg != nil && cfg.QuietHours != nil {
		defaultQuiet = cfg.QuietHours.Clone()
	}

	defs := make(map[string]ReminderDef)
	var defOrder []string
	if cfg != nil {
		defs = make(map[string]ReminderDef, len(cfg.Reminders))
		defOrder = make([]string, 0, len(cfg.Reminders))
		for _, d := range cfg.Reminders {
			defs[d.ID] = d
			defOrder = append(defOrder, d.ID)
		}
	}

	r := &Reminders{
		cx:           cx,
		storage:      storage,
		storageKey:   storageKey,
		prefix:       prefix,
		localize:     opts.Localize,
		onPress:      opts.OnPress,
		defaultQuiet: defaultQuiet,
		defs:         defs,
		defOrder:     defOrder,
		rev:          core.NewSignal(cx, 0),
		perm:         core.NewSignal(cx, core.NotificationsNotAsked),
		enabled:      !opts.DefaultDisabled,
		quietHours:   defaultQuiet.Clone(),
		scheduled:    make(map[string]scheduledReminder),
		seq:          make(map[string]uint64),
	}

	r.loadState()

	cx.OnNotificationPress(func(notifID string) {
		if !strings.HasPrefix(notifID, r.prefix) {
			return
		}
		id := strings.TrimPrefix(notifID, r.prefix)
		if id == "" {
			return
		}
		if entry, ok := r.scheduled[id]; ok {
			def, hasDef := r.defs[id]
			if entry.repeat == ReminderRepeatNone && (!hasDef || def.EffectiveOnOpen() != ReminderOnOpenReschedule) {
				delete(r.scheduled, id)
				r.saveScheduled()
				r.bump()
			}
		}
		if r.onPress != nil {
			r.onPress(ReminderPress{ID: id})
		}
	})

	prevLifecycle := core.Untracked(cx, cx.Env().Lifecycle)
	if prevLifecycle == core.LifecycleActive {
		r.handleForeground()
	}

	cx.Effect(func() {
		lc := cx.Env().Lifecycle()
		becameActive := prevLifecycle != core.LifecycleActive && lc == core.LifecycleActive
		prevLifecycle = lc
		if becameActive {
			r.handleForeground()
		}
	})

	return r, nil
}

// CanScheduleNotifications reports whether this platform schedules local notifications (core.Env.CanScheduleNotifications).
// Read in a reactive computation, it follows environment changes so games can hide reminder settings where scheduled notifications do nothing.
func (r *Reminders) CanScheduleNotifications() bool {
	return r.cx.Env().CanScheduleNotifications()
}

// Permission returns the last notification permission status observed by RequestPermission or Schedule.
// It starts at core.NotificationsNotAsked without querying the OS at launch. Read in a reactive computation, it updates when permission changes.
func (r *Reminders) Permission() core.NotificationPermission {
	return r.perm.Get()
}

// RequestPermission explicitly asks the OS for notification permission through core.Scope.RequestNotificationPermission and answers done with the result.
// Concurrent calls share a single permission request. done may be nil.
func (r *Reminders) RequestPermission(done func(core.NotificationPermission)) {
	r.requestPermission(done)
}

func (r *Reminders) requestPermission(done func(core.NotificationPermission)) {
	if r.cx.Disposed() {
		if done != nil {
			done(core.NotificationsUnavailable)
		}
		return
	}
	if done != nil {
		r.permWait = append(r.permWait, done)
	}
	if r.permAsking {
		return
	}
	r.permAsking = true
	r.cx.RequestNotificationPermission(func(p core.NotificationPermission) {
		r.permAsking = false
		r.permKnown = true
		r.perm.Set(p)
		waiters := r.permWait
		r.permWait = nil
		for _, fn := range waiters {
			fn(p)
		}
	})
}

// Enabled reports whether the player's in-game reminder setting is turned on.
// Read in a reactive computation, it follows SetEnabled.
func (r *Reminders) Enabled() bool {
	r.rev.Get()
	return r.enabled
}

// SetEnabled persists the player's reminder setting.
// Disabling immediately cancels all scheduled reminders and prevents future Schedule calls from prompting or scheduling.
func (r *Reminders) SetEnabled(enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	err := r.storage.SetItem(r.enabledKey(), val)
	if err != nil {
		if !enabled {
			// Fail closed when disabling so reminders stop immediately even if storage is unavailable.
			r.enabled = false
			r.CancelAll()
		}
		return err
	}
	r.enabled = enabled
	if !enabled {
		r.CancelAll()
		return nil
	}
	r.bump()
	return nil
}

// QuietHours returns a copy of the active local-time quiet hours window, or nil when quiet hours are disabled.
// Read in a reactive computation, it follows SetQuietHours.
func (r *Reminders) QuietHours() *QuietHours {
	r.rev.Get()
	return r.quietHours.Clone()
}

// SetQuietHours updates and persists the player's quiet-hours window (nil disables quiet hours even when RemindersConfig defines defaults).
// Any currently scheduled reminder whose known fire time falls inside the new window is immediately deferred to the window's end without prompting for permission.
func (r *Reminders) SetQuietHours(qh *QuietHours) error {
	if qh != nil {
		if err := qh.Validate(); err != nil {
			return err
		}
	}
	raw := "off"
	if qh != nil {
		raw = qh.Start + "-" + qh.End
	}
	if err := r.storage.SetItem(r.quietKey(), raw); err != nil {
		return err
	}
	r.quietHours = qh.Clone()
	r.bump()

	if qh != nil && r.enabled && core.Untracked(r.cx, r.cx.Env().CanScheduleNotifications) {
		now := r.cx.WallTime()
		for _, id := range r.sortedScheduledIDs() {
			entry := r.scheduled[id]
			if entry.at.IsZero() {
				continue
			}
			if entry.repeat == ReminderRepeatNone && !entry.at.After(now) {
				continue
			}
			if !qh.Contains(entry.at) {
				continue
			}
			def, ok := r.defs[id]
			if !ok {
				continue
			}
			nextAt := qh.NextAllowed(entry.at)
			r.showWithoutPrompt(id, def, entry.titleOverride, entry.bodyOverride, nextAt)
		}
	}
	return nil
}

// Definition returns the declared ReminderDef for id.
func (r *Reminders) Definition(id string) (ReminderDef, bool) {
	d, ok := r.defs[id]
	return d, ok
}

// IsScheduled reports whether id is currently scheduled (and, for a one-shot reminder scheduled in this run, still in the future).
// Read in a reactive computation, it follows Schedule, Cancel, CancelAll and foreground reconciliation.
func (r *Reminders) IsScheduled(id string) bool {
	r.rev.Get()
	entry, ok := r.scheduled[id]
	if !ok {
		return false
	}
	return r.entryActive(entry, r.cx.WallTime())
}

// Scheduled returns the sorted IDs of all currently scheduled reminders.
// Read in a reactive computation, it follows Schedule, Cancel, CancelAll and foreground reconciliation.
func (r *Reminders) Scheduled() []string {
	r.rev.Get()
	now := r.cx.WallTime()
	out := make([]string, 0, len(r.scheduled))
	for id, entry := range r.scheduled {
		if r.entryActive(entry, now) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// NextFireTime returns the scheduled wall-clock fire time for id when scheduled during this run.
// Read in a reactive computation, it follows Schedule, Cancel, SetQuietHours and foreground reconciliation.
func (r *Reminders) NextFireTime(id string) (time.Time, bool) {
	r.rev.Get()
	entry, ok := r.scheduled[id]
	if !ok || entry.at.IsZero() || !r.entryActive(entry, r.cx.WallTime()) {
		return time.Time{}, false
	}
	return entry.at, true
}

func (r *Reminders) entryActive(entry scheduledReminder, now time.Time) bool {
	if entry.repeat == ReminderRepeatNone && !entry.at.IsZero() && !entry.at.After(now) {
		return false
	}
	return true
}

// Schedule schedules the declared reminder named id according to its YAML definition and the active QuietHours, answering done (when non-nil) with whether the platform accepted it.
//
// When reminders are disabled by the player or CanScheduleNotifications is false, Schedule answers false without asking for permission.
// Otherwise, if notification permission has not yet been granted, Schedule requests it first and schedules only when granted.
// Returns ErrReminderNotFound (without calling done) if id is not declared in RemindersConfig.
func (r *Reminders) Schedule(id string, done func(scheduled bool)) error {
	return r.ScheduleWith(id, ScheduleOptions{}, done)
}

// ScheduleWith schedules the declared reminder named id with per-call overrides in opts.
// Returns ErrReminderNotFound or ErrInvalidSchedule (without calling done) when the arguments are invalid.
func (r *Reminders) ScheduleWith(id string, opts ScheduleOptions, done func(scheduled bool)) error {
	def, ok := r.defs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrReminderNotFound, id)
	}
	if opts.Delay < 0 {
		return fmt.Errorf("%w: negative delay %s", ErrInvalidSchedule, opts.Delay)
	}
	if opts.Delay > 0 && !opts.At.IsZero() {
		return fmt.Errorf("%w: delay and at cannot both be set", ErrInvalidSchedule)
	}
	now := r.cx.WallTime()
	if !opts.At.IsZero() && def.Repeat == ReminderRepeatNone && !opts.At.After(now) {
		return fmt.Errorf("%w: fire time %s is not in the future", ErrInvalidSchedule, opts.At.Format(time.RFC3339))
	}

	answer := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}

	if r.cx.Disposed() || !r.enabled || !core.Untracked(r.cx, r.cx.Env().CanScheduleNotifications) {
		answer(false)
		return nil
	}

	r.seq[id]++
	myGen := r.gen
	mySeq := r.seq[id]

	proceed := func() {
		if r.cx.Disposed() || r.gen != myGen || r.seq[id] != mySeq || !r.enabled || !core.Untracked(r.cx, r.cx.Env().CanScheduleNotifications) {
			answer(false)
			return
		}
		at := r.computeFireTime(def, opts, r.cx.WallTime())
		r.dispatchShow(id, def, opts.Title, opts.Body, at, myGen, mySeq, answer)
	}

	if r.permKnown && r.perm.GetUntracked() == core.NotificationsGranted {
		proceed()
		return nil
	}

	r.requestPermission(func(p core.NotificationPermission) {
		if p != core.NotificationsGranted {
			answer(false)
			return
		}
		proceed()
	})
	return nil
}

// Cancel cancels the scheduled or shown notification for id and stops any in-flight Schedule call for id.
func (r *Reminders) Cancel(id string) {
	if id == "" {
		return
	}
	r.seq[id]++
	if !r.cx.Disposed() {
		r.cx.CloseNotification(r.notifID(id))
	}
	if _, ok := r.scheduled[id]; ok {
		delete(r.scheduled, id)
		r.saveScheduled()
	}
	r.bump()
}

// CancelAll cancels every scheduled or shown reminder notification tracked by Reminders or declared in RemindersConfig, and stops any in-flight Schedule calls.
// Unrelated notifications belonging to other parts of the app are left untouched.
func (r *Reminders) CancelAll() {
	r.gen++
	ids := make(map[string]struct{}, len(r.scheduled)+len(r.defOrder))
	for id := range r.scheduled {
		ids[id] = struct{}{}
	}
	for _, id := range r.defOrder {
		ids[id] = struct{}{}
	}
	all := make([]string, 0, len(ids))
	for id := range ids {
		all = append(all, id)
	}
	slices.Sort(all)
	if !r.cx.Disposed() {
		for _, id := range all {
			r.cx.CloseNotification(r.notifID(id))
		}
	}
	clear(r.scheduled)
	r.saveScheduled()
	r.bump()
}

// handleForeground reconciles active reminders when the app launches or returns to the foreground, without ever requesting notification permission.
func (r *Reminders) handleForeground() {
	if r.cx.Disposed() || len(r.scheduled) == 0 {
		return
	}
	now := r.cx.WallTime()
	canSchedule := core.Untracked(r.cx, r.cx.Env().CanScheduleNotifications)
	changed := false
	for _, id := range r.sortedScheduledIDs() {
		entry := r.scheduled[id]
		def, hasDef := r.defs[id]
		if !hasDef || !r.enabled {
			r.seq[id]++
			r.cx.CloseNotification(r.notifID(id))
			delete(r.scheduled, id)
			changed = true
			continue
		}
		switch def.EffectiveOnOpen() {
		case ReminderOnOpenCancel:
			r.seq[id]++
			r.cx.CloseNotification(r.notifID(id))
			delete(r.scheduled, id)
			changed = true
		case ReminderOnOpenReschedule:
			if !canSchedule {
				r.seq[id]++
				r.cx.CloseNotification(r.notifID(id))
				delete(r.scheduled, id)
				changed = true
				continue
			}
			at := r.computeFireTime(def, ScheduleOptions{}, now)
			r.showWithoutPrompt(id, def, entry.titleOverride, entry.bodyOverride, at)
		case ReminderOnOpenKeep:
			if !r.entryActive(entry, now) {
				delete(r.scheduled, id)
				changed = true
			}
		}
	}
	if changed {
		r.saveScheduled()
		r.bump()
	}
}

func (r *Reminders) showWithoutPrompt(id string, def ReminderDef, titleOverride, bodyOverride string, at time.Time) {
	r.seq[id]++
	myGen := r.gen
	mySeq := r.seq[id]
	r.dispatchShow(id, def, titleOverride, bodyOverride, at, myGen, mySeq, nil)
}

func (r *Reminders) dispatchShow(id string, def ReminderDef, titleOverride, bodyOverride string, at time.Time, myGen, mySeq uint64, done func(bool)) {
	title := titleOverride
	if title == "" {
		title = r.localizeKey(def.Title)
	}
	if title == "" {
		title = def.Title
	}
	body := bodyOverride
	if body == "" && def.Body != "" {
		body = r.localizeKey(def.Body)
	}
	notif := core.Notification{
		ID:     r.notifID(id),
		Title:  title,
		Body:   body,
		Silent: def.Silent,
		At:     at,
		Repeat: toCoreRepeat(def.Repeat),
	}
	err := r.cx.ShowNotification(notif, func(shown bool) {
		if r.cx.Disposed() || r.gen != myGen || r.seq[id] != mySeq || !r.enabled {
			if shown && !r.cx.Disposed() {
				r.cx.CloseNotification(notif.ID)
			}
			if done != nil {
				done(false)
			}
			return
		}
		if shown {
			r.scheduled[id] = scheduledReminder{
				at:            at,
				repeat:        def.Repeat,
				titleOverride: titleOverride,
				bodyOverride:  bodyOverride,
			}
			r.saveScheduled()
			r.bump()
			if done != nil {
				done(true)
			}
			return
		}
		if _, ok := r.scheduled[id]; ok {
			delete(r.scheduled, id)
			r.saveScheduled()
			r.bump()
		}
		if done != nil {
			done(false)
		}
	})
	if err != nil {
		if done != nil {
			done(false)
		}
	}
}

func (r *Reminders) computeFireTime(def ReminderDef, opts ScheduleOptions, now time.Time) time.Time {
	var at time.Time
	switch {
	case opts.Delay > 0:
		at = now.Add(opts.Delay)
	case !opts.At.IsZero():
		at = opts.At
	case def.Delay > 0:
		at = now.Add(def.Delay)
	default:
		hour, minute, _ := parseClockTime(def.At)
		weekday, hasWeekday := parseWeekday(def.Weekday)
		at = nextWallClock(now, hour, minute, hasWeekday, weekday)
	}
	if r.quietHours != nil {
		at = r.quietHours.NextAllowed(at)
	}
	return at
}

// nextWallClock returns the first local wall-clock instant strictly after now matching hour:minute (and weekday when hasWeekday is true).
func nextWallClock(now time.Time, hour, minute int, hasWeekday bool, weekday time.Weekday) time.Time {
	cand := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if hasWeekday {
		daysAhead := (int(weekday) - int(now.Weekday()) + 7) % 7
		cand = cand.AddDate(0, 0, daysAhead)
		if !cand.After(now) {
			cand = cand.AddDate(0, 0, 7)
		}
		return cand
	}
	if !cand.After(now) {
		cand = cand.AddDate(0, 0, 1)
	}
	return cand
}

func toCoreRepeat(rep ReminderRepeat) core.NotificationRepeat {
	switch rep {
	case ReminderRepeatDaily:
		return core.NotificationDaily
	case ReminderRepeatWeekly:
		return core.NotificationWeekly
	default:
		return core.NotificationOnce
	}
}

func (r *Reminders) localizeKey(key string) string {
	if key == "" {
		return ""
	}
	if r.localize != nil {
		return r.localize(key)
	}
	return key
}

func (r *Reminders) notifID(id string) string {
	return r.prefix + id
}

func (r *Reminders) enabledKey() string   { return r.storageKey + ".enabled" }
func (r *Reminders) quietKey() string     { return r.storageKey + ".quiet" }
func (r *Reminders) scheduledKey() string { return r.storageKey + ".scheduled" }

func (r *Reminders) loadState() {
	if raw, ok := r.storage.GetItem(r.enabledKey()); ok {
		switch raw {
		case "true":
			r.enabled = true
		case "false":
			r.enabled = false
		}
	}
	if raw, ok := r.storage.GetItem(r.quietKey()); ok {
		if raw == "off" {
			r.quietHours = nil
		} else if start, end, found := strings.Cut(raw, "-"); found {
			qh := QuietHours{Start: start, End: end}
			if qh.Validate() == nil {
				r.quietHours = &qh
			}
		}
	}
	if raw, ok := r.storage.GetItem(r.scheduledKey()); ok && raw != "" {
		for id := range strings.SplitSeq(raw, ",") {
			if id == "" {
				continue
			}
			var rep ReminderRepeat
			if def, exists := r.defs[id]; exists {
				rep = def.Repeat
			}
			r.scheduled[id] = scheduledReminder{repeat: rep}
		}
	}
}

func (r *Reminders) saveScheduled() {
	ids := r.sortedScheduledIDs()
	if len(ids) == 0 {
		r.storage.RemoveItem(r.scheduledKey())
		return
	}
	_ = r.storage.SetItem(r.scheduledKey(), strings.Join(ids, ","))
}

func (r *Reminders) sortedScheduledIDs() []string {
	ids := make([]string, 0, len(r.scheduled))
	for id := range r.scheduled {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (r *Reminders) bump() {
	r.rev.Set(r.rev.GetUntracked() + 1)
}
