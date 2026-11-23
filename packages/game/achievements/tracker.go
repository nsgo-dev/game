package achievements

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

var (
	// ErrUnknownAchievement reports an ID that the Tracker's Set does not declare.
	ErrUnknownAchievement = errors.New("achievements: unknown achievement")
	// ErrSave reports that progress was recorded in memory but could not be written to the Store given to Persist; the next successful write saves it.
	ErrSave = errors.New("achievements: saving progress failed")
)

// Store is the key-value storage Persist keeps progress in; core.Storage satisfies it.
type Store interface {
	GetItem(key string) (string, bool)
	SetItem(key, value string) error
}

// Reporter sends achievement progress to one platform service. ReportAchievement receives the achievement's ID on that service and its completion in percent (0 to 100, where 100 unlocks it) and must call done exactly once, from any goroutine, with nil once the service has accepted the update.
type Reporter interface {
	ReportAchievement(id string, percent float64, done func(error))
}

// ReporterFunc adapts a function to the Reporter interface.
type ReporterFunc func(id string, percent float64, done func(error))

// ReportAchievement calls f(id, percent, done).
func (f ReporterFunc) ReportAchievement(id string, percent float64, done func(error)) {
	f(id, percent, done)
}

// Tracker records a player's progress toward the achievements of a Set and reports it to a connected platform service. Its methods are safe for concurrent use; callbacks run without the Tracker's lock held, so they may call back into it.
type Tracker struct {
	mu sync.Mutex
	// set and index never change after NewTracker, so they are read without mu.
	set     *Set
	index   map[string]int
	entries []entry

	onUnlock func(Definition)
	onError  func(Definition, error)

	store Store
	key   string

	service  Service
	reporter Reporter
	// gen changes on every Connect, Disconnect, Restore and Persist that loads progress, so a report answered after the connection or the progress changed does not touch the new state's in-flight bookkeeping.
	gen uint64
	// conn changes on every Connect and Disconnect only, so SignIn can tell whether the connection it made is still the current one.
	conn uint64
}

type entry struct {
	steps    int
	reported map[Service]int
	// inflight is the step count of the report awaiting an answer on the current connection, 0 when none. While a report is in flight, further progress is held back and sent as one follow-up report once the answer arrives, so rapid increments do not flood the service.
	inflight int
}

// NewTracker returns a Tracker for a copy of set, with no progress and no service connected. It returns the error Validate reports for an invalid set.
func NewTracker(set *Set) (*Tracker, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	t := &Tracker{
		set:     set.Clone(),
		index:   make(map[string]int, len(set.Achievements)),
		entries: make([]entry, len(set.Achievements)),
	}
	for i, d := range t.set.Achievements {
		t.index[d.ID] = i
	}
	return t, nil
}

// Definitions returns a copy of the tracked achievements in their declared order.
func (t *Tracker) Definitions() []Definition {
	return t.set.Clone().Achievements
}

// Lookup returns the definition whose ID is id.
func (t *Tracker) Lookup(id string) (Definition, bool) {
	i, ok := t.index[id]
	if !ok {
		return Definition{}, false
	}
	return t.set.Achievements[i], true
}

// OnUnlock sets fn to be called once each time an achievement reaches its step count through SetSteps, Add or Unlock; nil clears it. Restore never calls it.
func (t *Tracker) OnUnlock(fn func(Definition)) {
	t.mu.Lock()
	t.onUnlock = fn
	t.mu.Unlock()
}

// OnError sets fn to be called with a failed report's achievement and error, or with a save failure wrapped in ErrSave; nil clears it. Failed reports stay pending and are retried by the next change, Flush or Connect.
func (t *Tracker) OnError(fn func(Definition, error)) {
	t.mu.Lock()
	t.onError = fn
	t.mu.Unlock()
}

// Steps returns the steps made toward achievement id, 0 for an unknown ID.
func (t *Tracker) Steps(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i, ok := t.index[id]; ok {
		return t.entries[i].steps
	}
	return 0
}

// Percent returns achievement id's completion from 0 to 100, 0 for an unknown ID.
func (t *Tracker) Percent(id string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i, ok := t.index[id]; ok {
		return t.percentLocked(i, t.entries[i].steps)
	}
	return 0
}

// Unlocked reports whether achievement id has reached its step count.
func (t *Tracker) Unlocked(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i, ok := t.index[id]; ok {
		return t.entries[i].steps >= t.set.Achievements[i].TotalSteps()
	}
	return false
}

// Unlock completes achievement id. Unlocking an achievement that is already complete does nothing.
func (t *Tracker) Unlock(id string) error {
	d, ok := t.Lookup(id)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownAchievement, id)
	}
	return t.SetSteps(id, d.TotalSteps())
}

// SetSteps raises achievement id's progress to steps, capped at its step count. Progress never goes back, so a count at or below the current one does nothing. The new progress is saved when Persist set a Store, and reported when a service is connected; a save failure is returned wrapped in ErrSave after the progress is recorded.
func (t *Tracker) SetSteps(id string, steps int) error {
	if steps < 0 {
		return fmt.Errorf("achievements: %q: negative steps %d", id, steps)
	}
	return t.advance(id, func(int) int { return steps })
}

// Add adds n steps to achievement id's progress, saturating at its step count. It returns an error for a negative n and does nothing for 0; otherwise it saves and reports as SetSteps does.
func (t *Tracker) Add(id string, n int) error {
	if n < 0 {
		return fmt.Errorf("achievements: %q: negative step increment %d", id, n)
	}
	if n == 0 {
		if _, ok := t.index[id]; !ok {
			return fmt.Errorf("%w %q", ErrUnknownAchievement, id)
		}
		return nil
	}
	return t.advance(id, func(cur int) int {
		if cur > MaxSteps-n {
			return MaxSteps
		}
		return cur + n
	})
}

func (t *Tracker) advance(id string, next func(cur int) int) error {
	i, ok := t.index[id]
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownAchievement, id)
	}
	t.mu.Lock()
	d := t.set.Achievements[i]
	total := d.TotalSteps()
	e := &t.entries[i]
	steps := min(next(e.steps), total)
	if steps <= e.steps {
		t.mu.Unlock()
		return nil
	}
	unlocked := e.steps < total && steps >= total
	e.steps = steps
	saveErr := t.saveLocked()
	onUnlock := t.onUnlock
	r := t.prepareLocked(i)
	t.mu.Unlock()

	if unlocked && onUnlock != nil {
		onUnlock(d)
	}
	r.send(t)
	return saveErr
}

// Progress returns the current progress as a document EncodeProgress can write: one record per achievement with steps made or acknowledged, sorted by ID.
func (t *Tracker) Progress() *Progress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.progressLocked()
}

func (t *Tracker) progressLocked() *Progress {
	p := &Progress{Version: CurrentVersion}
	for i, d := range t.set.Achievements {
		e := t.entries[i]
		if e.steps == 0 && len(e.reported) == 0 {
			continue
		}
		r := Record{ID: d.ID, Steps: e.steps}
		if len(e.reported) > 0 {
			r.Reported = make(map[Service]int, len(e.reported))
			for s, n := range e.reported {
				r.Reported[s] = n
			}
		}
		p.Achievements = append(p.Achievements, r)
	}
	sortRecords(p.Achievements)
	return p
}

// Restore replaces the Tracker's progress with p. Records for achievements the Set no longer declares are dropped, and steps beyond an achievement's step count are capped. It does not call OnUnlock or save, and reports whatever is pending when a service is connected.
func (t *Tracker) Restore(p *Progress) error {
	if err := ValidateProgress(p); err != nil {
		return err
	}
	t.mu.Lock()
	t.restoreLocked(p)
	reports := t.prepareAllLocked()
	t.mu.Unlock()
	for _, r := range reports {
		r.send(t)
	}
	return nil
}

func (t *Tracker) restoreLocked(p *Progress) {
	// Answers to reports sent before the restore must not clear the in-flight marks of reports sent after it.
	t.gen++
	for i := range t.entries {
		t.entries[i] = entry{}
	}
	for _, r := range p.Achievements {
		i, ok := t.index[r.ID]
		if !ok {
			continue
		}
		total := t.set.Achievements[i].TotalSteps()
		e := &t.entries[i]
		e.steps = min(r.Steps, total)
		for s, n := range r.Reported {
			if n = min(n, e.steps); n > 0 {
				if e.reported == nil {
					e.reported = make(map[Service]int, len(r.Reported))
				}
				e.reported[s] = n
			}
		}
	}
}

// Persist loads the progress stored under key in store, if any, and then writes the progress back there as YAML after every change. When the stored progress cannot be parsed, Persist returns the error and leaves the Tracker and store untouched, so the caller decides whether to discard it.
func (t *Tracker) Persist(store Store, key string) error {
	if store == nil || key == "" {
		return errors.New("achievements: Persist needs a store and a key")
	}
	var p *Progress
	if raw, ok := store.GetItem(key); ok {
		var err error
		if p, err = ParseProgress([]byte(raw)); err != nil {
			return fmt.Errorf("achievements: stored progress %q: %w", key, err)
		}
	}
	t.mu.Lock()
	if p != nil {
		t.restoreLocked(p)
	}
	t.store, t.key = store, key
	reports := t.prepareAllLocked()
	t.mu.Unlock()
	for _, r := range reports {
		r.send(t)
	}
	return nil
}

// saveLocked writes the progress to the Persist store, if any.
func (t *Tracker) saveLocked() error {
	if t.store == nil {
		return nil
	}
	data, err := EncodeProgress(t.progressLocked())
	if err == nil {
		err = t.store.SetItem(t.key, string(data))
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSave, err)
	}
	return nil
}

// Connect makes r the Reporter for service and reports every achievement whose progress service has not acknowledged. It replaces any earlier connection.
func (t *Tracker) Connect(service Service, r Reporter) error {
	_, err := t.connect(service, r)
	return err
}

// connect is Connect returning the new connection's number for disconnectIf.
func (t *Tracker) connect(service Service, r Reporter) (uint64, error) {
	if !service.Valid() {
		return 0, fmt.Errorf("achievements: unknown service %q", service)
	}
	if r == nil {
		return 0, errors.New("achievements: Connect needs a Reporter")
	}
	t.mu.Lock()
	t.gen++
	t.conn++
	conn := t.conn
	t.service, t.reporter = service, r
	for i := range t.entries {
		t.entries[i].inflight = 0
	}
	reports := t.prepareAllLocked()
	t.mu.Unlock()
	for _, rep := range reports {
		rep.send(t)
	}
	return conn, nil
}

// Disconnect detaches the connected service. Progress keeps being recorded, and saved when Persist set a Store, and is reported on the next Connect.
func (t *Tracker) Disconnect() {
	t.mu.Lock()
	t.disconnectLocked()
	t.mu.Unlock()
}

// disconnectIf disconnects the service only while conn, from connect, is still the current connection.
func (t *Tracker) disconnectIf(conn uint64) {
	t.mu.Lock()
	if t.conn == conn {
		t.disconnectLocked()
	}
	t.mu.Unlock()
}

func (t *Tracker) disconnectLocked() {
	t.gen++
	t.conn++
	t.service, t.reporter = "", nil
	for i := range t.entries {
		t.entries[i].inflight = 0
	}
}

// Service returns the connected service, "" when none.
func (t *Tracker) Service() Service {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.service
}

// Flush reports every achievement whose progress the connected service has not acknowledged and that is not already being reported. It does nothing without a connected service.
func (t *Tracker) Flush() {
	t.mu.Lock()
	reports := t.prepareAllLocked()
	t.mu.Unlock()
	for _, r := range reports {
		r.send(t)
	}
}

// Pending returns, in declared order, the IDs of achievements with an ID on service whose progress service has not acknowledged.
func (t *Tracker) Pending(service Service) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ids []string
	for i, d := range t.set.Achievements {
		if d.Platforms.ID(service) != "" && t.entries[i].steps > t.entries[i].reported[service] {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

// report is one ReportAchievement call prepared under the lock and sent after it is released.
type report struct {
	index    int
	gen      uint64
	service  Service
	reporter Reporter
	id       string
	steps    int
	percent  float64
}

func (r report) send(t *Tracker) {
	if r.reporter == nil {
		return
	}
	r.reporter.ReportAchievement(r.id, r.percent, func(err error) { t.answered(r, err) })
}

// prepareLocked returns the report achievement i needs on the current connection, or a zero report when none is due.
func (t *Tracker) prepareLocked(i int) report {
	if t.reporter == nil {
		return report{}
	}
	d := t.set.Achievements[i]
	pid := d.Platforms.ID(t.service)
	e := &t.entries[i]
	if pid == "" || e.steps <= e.reported[t.service] || e.inflight != 0 {
		return report{}
	}
	e.inflight = e.steps
	return report{
		index:    i,
		gen:      t.gen,
		service:  t.service,
		reporter: t.reporter,
		id:       pid,
		steps:    e.steps,
		percent:  t.percentLocked(i, e.steps),
	}
}

func (t *Tracker) prepareAllLocked() []report {
	if t.reporter == nil {
		return nil
	}
	var reports []report
	for i := range t.entries {
		if r := t.prepareLocked(i); r.reporter != nil {
			reports = append(reports, r)
		}
	}
	return reports
}

// answered records the service's answer to r and, after a success, reports progress made while r was in flight.
func (t *Tracker) answered(r report, err error) {
	t.mu.Lock()
	d := t.set.Achievements[r.index]
	e := &t.entries[r.index]
	current := r.gen == t.gen
	if current {
		e.inflight = 0
	}
	var saveErr error
	var next report
	switch {
	case err == nil:
		// A Restore since r was sent may have lowered the steps, and an acknowledgement never counts for more than the steps made.
		if acked := min(r.steps, e.steps); acked > e.reported[r.service] {
			if e.reported == nil {
				e.reported = map[Service]int{}
			}
			e.reported[r.service] = acked
			saveErr = t.saveLocked()
		}
		if current {
			next = t.prepareLocked(r.index)
		}
	case current && isUnsupported(err):
		t.disconnectLocked()
	}
	onError := t.onError
	t.mu.Unlock()

	if onError != nil {
		if err != nil {
			onError(d, err)
		}
		if saveErr != nil {
			onError(d, saveErr)
		}
	}
	next.send(t)
}

func (t *Tracker) percentLocked(i, steps int) float64 {
	total := t.set.Achievements[i].TotalSteps()
	if steps >= total {
		return 100
	}
	return float64(steps) * 100 / float64(total)
}

func sortRecords(rs []Record) {
	slices.SortFunc(rs, func(a, b Record) int { return strings.Compare(a.ID, b.ID) })
}
