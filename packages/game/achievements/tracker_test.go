package achievements_test

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nsgo-dev/game/packages/game/achievements"
	"github.com/nsgo-dev/nsgo/gameservices"
)

func testSet(t testing.TB) *achievements.Set {
	t.Helper()
	set, err := achievements.Parse([]byte(`version: 1
achievements:
  - id: first-match
    platforms:
      gameCenter: gc.first
      playGames: pg.first
  - id: gem-collector
    steps: 4
    platforms:
      gameCenter: gc.gems
      playGames: pg.gems
  - id: apple-only
    platforms:
      gameCenter: gc.apple
`))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func newTracker(t testing.TB) *achievements.Tracker {
	t.Helper()
	tr, err := achievements.NewTracker(testSet(t))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// call is one ReportAchievement call held until the test answers it.
type call struct {
	id      string
	percent float64
	done    func(error)
}

// queueReporter holds every report so tests decide when and how the service answers.
type queueReporter struct{ calls []call }

func (q *queueReporter) ReportAchievement(id string, percent float64, done func(error)) {
	q.calls = append(q.calls, call{id, percent, done})
}

// answer answers the oldest held report with err.
func (q *queueReporter) answer(t *testing.T, err error) call {
	t.Helper()
	if len(q.calls) == 0 {
		t.Fatal("no report to answer")
	}
	c := q.calls[0]
	q.calls = q.calls[1:]
	c.done(err)
	return c
}

func (q *queueReporter) ids() []string {
	var ids []string
	for _, c := range q.calls {
		ids = append(ids, fmt.Sprintf("%s@%g", c.id, c.percent))
	}
	return ids
}

// mapStore is an in-memory Store.
type mapStore struct {
	items map[string]string
	err   error
}

func (m *mapStore) GetItem(key string) (string, bool) {
	v, ok := m.items[key]
	return v, ok
}

func (m *mapStore) SetItem(key, value string) error {
	if m.err != nil {
		return m.err
	}
	if m.items == nil {
		m.items = map[string]string{}
	}
	m.items[key] = value
	return nil
}

func TestNewTrackerRejectsInvalidSet(t *testing.T) {
	if _, err := achievements.NewTracker(&achievements.Set{Version: 3}); err == nil {
		t.Fatal("NewTracker accepted an invalid set")
	}
}

func TestProgressIsMonotonicAndCapped(t *testing.T) {
	tr := newTracker(t)
	var unlocked []string
	tr.OnUnlock(func(d achievements.Definition) { unlocked = append(unlocked, d.ID) })

	mustOK(t, tr.Add("gem-collector", 1))
	mustOK(t, tr.SetSteps("gem-collector", 3))
	mustOK(t, tr.SetSteps("gem-collector", 2)) // lower count is ignored
	if got := tr.Steps("gem-collector"); got != 3 {
		t.Fatalf("steps = %d, want 3", got)
	}
	if got := tr.Percent("gem-collector"); got != 75 {
		t.Fatalf("percent = %v, want 75", got)
	}
	if tr.Unlocked("gem-collector") || len(unlocked) != 0 {
		t.Fatal("unlocked before reaching the step count")
	}
	mustOK(t, tr.Add("gem-collector", 10)) // saturates at 4
	mustOK(t, tr.Add("gem-collector", 1))
	mustOK(t, tr.Unlock("gem-collector"))
	if tr.Steps("gem-collector") != 4 || !tr.Unlocked("gem-collector") || tr.Percent("gem-collector") != 100 {
		t.Fatalf("after saturating: steps=%d", tr.Steps("gem-collector"))
	}
	mustOK(t, tr.Unlock("first-match"))
	mustOK(t, tr.Unlock("first-match"))
	if !slices.Equal(unlocked, []string{"gem-collector", "first-match"}) {
		t.Fatalf("OnUnlock calls = %v", unlocked)
	}
	mustOK(t, tr.Add("first-match", 0))
}

func TestAddSaturatesAtMaxSteps(t *testing.T) {
	set := &achievements.Set{Version: 1, Achievements: []achievements.Definition{{ID: "huge", Steps: achievements.MaxSteps}}}
	tr, err := achievements.NewTracker(set)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, tr.Add("huge", achievements.MaxSteps-1))
	mustOK(t, tr.Add("huge", achievements.MaxSteps))
	if !tr.Unlocked("huge") || tr.Steps("huge") != achievements.MaxSteps {
		t.Fatalf("steps = %d", tr.Steps("huge"))
	}
}

func TestUnknownIDsAndBadInput(t *testing.T) {
	tr := newTracker(t)
	for name, err := range map[string]error{
		"Unlock":   tr.Unlock("nope"),
		"SetSteps": tr.SetSteps("nope", 1),
		"Add":      tr.Add("nope", 1),
		"Add 0":    tr.Add("nope", 0),
	} {
		if !errors.Is(err, achievements.ErrUnknownAchievement) {
			t.Errorf("%s: err = %v, want ErrUnknownAchievement", name, err)
		}
	}
	if tr.SetSteps("gem-collector", -1) == nil || tr.Add("gem-collector", -1) == nil {
		t.Fatal("negative steps accepted")
	}
	if tr.Steps("nope") != 0 || tr.Percent("nope") != 0 || tr.Unlocked("nope") {
		t.Fatal("unknown ID reports progress")
	}
	if err := tr.Connect("steam", &queueReporter{}); err == nil {
		t.Fatal("Connect accepted an unknown service")
	}
	if err := tr.Connect(achievements.GameCenter, nil); err == nil {
		t.Fatal("Connect accepted a nil Reporter")
	}
}

func TestDefinitionsAreCopies(t *testing.T) {
	set := testSet(t)
	tr, err := achievements.NewTracker(set)
	if err != nil {
		t.Fatal(err)
	}
	set.Achievements[0].ID = "changed"
	defs := tr.Definitions()
	defs[1].Steps = 1
	if d, ok := tr.Lookup("first-match"); !ok || d.Platforms.GameCenter != "gc.first" {
		t.Fatal("Tracker shares the caller's Set")
	}
	if d, _ := tr.Lookup("gem-collector"); d.TotalSteps() != 4 {
		t.Fatal("Definitions returned the Tracker's own slice")
	}
}

func TestReportsOnlyWhatTheServiceHasNotAcknowledged(t *testing.T) {
	tr := newTracker(t)
	mustOK(t, tr.Unlock("first-match"))
	mustOK(t, tr.Add("gem-collector", 1))
	mustOK(t, tr.Unlock("apple-only"))
	if got := tr.Pending(achievements.PlayGames); !slices.Equal(got, []string{"first-match", "gem-collector"}) {
		t.Fatalf("pending on Play Games = %v", got)
	}

	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.PlayGames, q))
	if tr.Service() != achievements.PlayGames {
		t.Fatalf("service = %q", tr.Service())
	}
	// apple-only has no Play Games ID, so it is not reported there.
	if got := q.ids(); !slices.Equal(got, []string{"pg.first@100", "pg.gems@25"}) {
		t.Fatalf("reports = %v", got)
	}

	// Progress made while a report is in flight is held back until the answer.
	mustOK(t, tr.Add("gem-collector", 1))
	tr.Flush()
	if len(q.calls) != 2 {
		t.Fatalf("reported again while in flight: %v", q.ids())
	}
	q.answer(t, nil)
	q.answer(t, nil)
	if got := q.ids(); !slices.Equal(got, []string{"pg.gems@50"}) {
		t.Fatalf("follow-up reports = %v", got)
	}
	q.answer(t, nil)
	if got := tr.Pending(achievements.PlayGames); len(got) != 0 {
		t.Fatalf("still pending: %v", got)
	}
	tr.Flush()
	if len(q.calls) != 0 {
		t.Fatalf("Flush re-sent acknowledged progress: %v", q.ids())
	}
	// Game Center has acknowledged nothing yet.
	if got := tr.Pending(achievements.GameCenter); !slices.Equal(got, []string{"first-match", "gem-collector", "apple-only"}) {
		t.Fatalf("pending on Game Center = %v", got)
	}
}

func TestFailedReportsStayPendingAndAreRetried(t *testing.T) {
	tr := newTracker(t)
	var failures []string
	tr.OnError(func(d achievements.Definition, err error) { failures = append(failures, d.ID+": "+err.Error()) })
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))

	mustOK(t, tr.Unlock("first-match"))
	q.answer(t, gameservices.ErrUnavailable)
	if len(failures) != 1 || !slices.Equal(tr.Pending(achievements.GameCenter), []string{"first-match"}) {
		t.Fatalf("failures=%v pending=%v", failures, tr.Pending(achievements.GameCenter))
	}
	if len(q.calls) != 0 {
		t.Fatal("a failed report was retried immediately")
	}
	tr.Flush()
	if got := q.ids(); !slices.Equal(got, []string{"gc.first@100"}) {
		t.Fatalf("retry = %v", got)
	}
	q.answer(t, nil)
	if len(tr.Pending(achievements.GameCenter)) != 0 {
		t.Fatal("retry did not clear pending")
	}
}

func TestUnsupportedAnswerDisconnects(t *testing.T) {
	tr := newTracker(t)
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))
	mustOK(t, tr.Unlock("first-match"))
	q.answer(t, fmt.Errorf("wrapped: %w", gameservices.ErrUnsupported))
	if tr.Service() != "" {
		t.Fatal("still connected after ErrUnsupported")
	}
	mustOK(t, tr.Add("gem-collector", 1))
	if len(q.calls) != 0 {
		t.Fatal("reported after disconnecting")
	}
}

func TestAnswersFromAnOldConnection(t *testing.T) {
	tr := newTracker(t)
	old := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, old))
	mustOK(t, tr.Unlock("first-match"))

	// Reconnecting re-sends what the old connection never confirmed.
	fresh := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, fresh))
	if got := fresh.ids(); !slices.Equal(got, []string{"gc.first@100"}) {
		t.Fatalf("fresh reports = %v", got)
	}
	// The old answer still counts as an acknowledgement but does not disturb the new connection.
	old.answer(t, nil)
	if len(tr.Pending(achievements.GameCenter)) != 0 {
		t.Fatal("old acknowledgement was dropped")
	}
	old.calls = nil
	fresh.answer(t, nil)
	if len(fresh.calls) != 0 || len(old.calls) != 0 {
		t.Fatal("duplicate follow-up report")
	}

	// An old ErrUnsupported does not disconnect the new connection.
	mustOK(t, tr.Add("gem-collector", 1))
	tr.Disconnect()
	if tr.Service() != "" {
		t.Fatal("Disconnect kept the service")
	}
	newest := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, newest))
	fresh.answer(t, gameservices.ErrUnsupported)
	if tr.Service() != achievements.GameCenter {
		t.Fatal("a stale ErrUnsupported disconnected the new connection")
	}
}

func TestSynchronousReporterCanReenter(t *testing.T) {
	tr := newTracker(t)
	var reported []string
	var r achievements.ReporterFunc = func(id string, percent float64, done func(error)) {
		reported = append(reported, fmt.Sprintf("%s@%g", id, percent))
		_ = tr.Steps("gem-collector") // the lock is not held during the call
		done(nil)
	}
	mustOK(t, tr.Connect(achievements.GameCenter, r))
	mustOK(t, tr.Add("gem-collector", 2))
	mustOK(t, tr.Add("gem-collector", 2))
	if !slices.Equal(reported, []string{"gc.gems@50", "gc.gems@100"}) {
		t.Fatalf("reported = %v", reported)
	}
}

func TestRestoreAndProgress(t *testing.T) {
	tr := newTracker(t)
	p := &achievements.Progress{Version: 1, Achievements: []achievements.Record{
		{ID: "gem-collector", Steps: 9, Reported: map[achievements.Service]int{achievements.GameCenter: 2, achievements.PlayGames: 9}},
		{ID: "removed", Steps: 1},
	}}
	var unlocked int
	tr.OnUnlock(func(achievements.Definition) { unlocked++ })
	mustOK(t, tr.Restore(p))
	if unlocked != 0 {
		t.Fatal("Restore called OnUnlock")
	}
	got := tr.Progress()
	want := &achievements.Progress{Version: 1, Achievements: []achievements.Record{
		{ID: "gem-collector", Steps: 4, Reported: map[achievements.Service]int{achievements.GameCenter: 2, achievements.PlayGames: 4}},
	}}
	if !equalProgress(got, want) {
		t.Fatalf("Progress = %+v, want %+v", got, want)
	}
	if err := tr.Restore(&achievements.Progress{Version: 9}); err == nil {
		t.Fatal("Restore accepted an invalid document")
	}
	// Restore with a connection reports what the service lacks.
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))
	if got := q.ids(); !slices.Equal(got, []string{"gc.gems@100"}) {
		t.Fatalf("reports after restore = %v", got)
	}
}

func TestPersistLoadsAndSavesEveryChange(t *testing.T) {
	store := &mapStore{}
	tr := newTracker(t)
	mustOK(t, tr.Persist(store, "ach"))
	if _, ok := store.items["ach"]; ok {
		t.Fatal("Persist wrote without a change")
	}
	mustOK(t, tr.Unlock("first-match"))
	mustOK(t, tr.Add("gem-collector", 3))
	want := "version: 1\nachievements:\n  - id: first-match\n    steps: 1\n  - id: gem-collector\n    steps: 3\n"
	if store.items["ach"] != want {
		t.Fatalf("stored:\n%s\nwant:\n%s", store.items["ach"], want)
	}

	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.PlayGames, q))
	q.answer(t, nil)
	if want := "version: 1\nachievements:\n  - id: first-match\n    steps: 1\n    reported:\n      playGames: 1\n  - id: gem-collector\n    steps: 3\n"; store.items["ach"] != want {
		t.Fatalf("stored after acknowledgement:\n%s\nwant:\n%s", store.items["ach"], want)
	}

	// A new session restores it and only reports what Play Games lacks.
	next := newTracker(t)
	mustOK(t, next.Persist(store, "ach"))
	if next.Steps("gem-collector") != 3 || !next.Unlocked("first-match") {
		t.Fatal("Persist did not restore progress")
	}
	q2 := &queueReporter{}
	mustOK(t, next.Connect(achievements.PlayGames, q2))
	if got := q2.ids(); !slices.Equal(got, []string{"pg.gems@75"}) {
		t.Fatalf("reports after restart = %v", got)
	}
}

func TestPersistRefusesCorruptProgress(t *testing.T) {
	store := &mapStore{items: map[string]string{"ach": "version: 1\nachievements: 7\n"}}
	tr := newTracker(t)
	if err := tr.Persist(store, "ach"); err == nil {
		t.Fatal("Persist accepted corrupt progress")
	}
	mustOK(t, tr.Unlock("first-match"))
	if store.items["ach"] != "version: 1\nachievements: 7\n" {
		t.Fatal("a refused Persist still overwrote the store")
	}
	if tr.Persist(nil, "x") == nil || tr.Persist(store, "") == nil {
		t.Fatal("Persist accepted a nil store or empty key")
	}
}

func TestSaveFailuresAreReported(t *testing.T) {
	store := &mapStore{}
	tr := newTracker(t)
	mustOK(t, tr.Persist(store, "ach"))
	store.err = errors.New("disk full")
	err := tr.Unlock("first-match")
	if !errors.Is(err, achievements.ErrSave) || !errors.Is(err, store.err) {
		t.Fatalf("err = %v", err)
	}
	if !tr.Unlocked("first-match") {
		t.Fatal("a save failure lost the progress")
	}
	var saveErrs int
	tr.OnError(func(_ achievements.Definition, err error) {
		if errors.Is(err, achievements.ErrSave) {
			saveErrs++
		}
	})
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))
	q.answer(t, nil)
	if saveErrs != 1 {
		t.Fatalf("save errors after acknowledgement = %d", saveErrs)
	}
	store.err = nil
	mustOK(t, tr.Add("gem-collector", 1))
	if _, ok := store.items["ach"]; !ok {
		t.Fatal("the next successful write did not save")
	}
}

func TestRestoreWhileAReportIsInFlight(t *testing.T) {
	store := &mapStore{}
	tr := newTracker(t)
	var errs []error
	tr.OnError(func(_ achievements.Definition, err error) { errs = append(errs, err) })
	mustOK(t, tr.Persist(store, "ach"))
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))
	mustOK(t, tr.SetSteps("gem-collector", 4))

	// Restoring lower progress while gc.gems@100 awaits its answer reports the restored progress afresh.
	mustOK(t, tr.Restore(&achievements.Progress{Version: 1, Achievements: []achievements.Record{{ID: "gem-collector", Steps: 1}}}))
	if got := q.ids(); !slices.Equal(got, []string{"gc.gems@100", "gc.gems@25"}) {
		t.Fatalf("reports = %v", got)
	}
	// The answer to the report sent before the restore counts for no more than the restored steps.
	q.answer(t, nil)
	q.answer(t, nil)
	if len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	want := &achievements.Progress{Version: 1, Achievements: []achievements.Record{
		{ID: "gem-collector", Steps: 1, Reported: map[achievements.Service]int{achievements.GameCenter: 1}},
	}}
	if got := tr.Progress(); !equalProgress(got, want) {
		t.Fatalf("Progress = %+v, want %+v", got, want)
	}
	if _, err := achievements.EncodeProgress(tr.Progress()); err != nil {
		t.Fatal(err)
	}
	mustOK(t, tr.Add("gem-collector", 1))
	if got := q.ids(); !slices.Equal(got, []string{"gc.gems@50"}) {
		t.Fatalf("reports after restore = %v", got)
	}
	if want := "version: 1\nachievements:\n  - id: gem-collector\n    steps: 2\n    reported:\n      gameCenter: 1\n"; store.items["ach"] != want {
		t.Fatalf("stored:\n%s\nwant:\n%s", store.items["ach"], want)
	}
}

func TestPersistWhileAReportIsInFlight(t *testing.T) {
	store := &mapStore{items: map[string]string{"ach": "version: 1\n"}}
	tr := newTracker(t)
	q := &queueReporter{}
	mustOK(t, tr.Connect(achievements.GameCenter, q))
	mustOK(t, tr.Unlock("first-match"))
	// Loading the stored progress drops first-match, and the old answer must not leave it acknowledged beyond its steps.
	mustOK(t, tr.Persist(store, "ach"))
	q.answer(t, nil)
	if got := tr.Progress(); len(got.Achievements) != 0 {
		t.Fatalf("Progress = %+v", got)
	}
	if _, err := achievements.EncodeProgress(tr.Progress()); err != nil {
		t.Fatal(err)
	}
}

// asyncReporter answers every report from a goroutine of its own, as a platform service may.
type asyncReporter struct {
	wg  *sync.WaitGroup
	err error
}

func (a asyncReporter) ReportAchievement(_ string, _ float64, done func(error)) {
	a.wg.Go(func() { done(a.err) })
}

func TestConcurrentUse(t *testing.T) {
	store := &mapStore{}
	tr := newTracker(t)
	var unlocks atomic.Int32
	tr.OnUnlock(func(achievements.Definition) { unlocks.Add(1) })
	tr.OnError(func(achievements.Definition, error) {})
	mustOK(t, tr.Persist(store, "ach"))

	services := []achievements.Service{achievements.GameCenter, achievements.PlayGames}
	var answers, workers sync.WaitGroup
	for g := range 8 {
		workers.Go(func() {
			r := asyncReporter{wg: &answers}
			if g%4 == 0 {
				r.err = errors.New("offline")
			}
			for i := range 20 {
				switch i % 5 {
				case 0:
					errorAsync(t, tr.Connect(services[g%2], r))
				case 1:
					errorAsync(t, tr.Add("gem-collector", 1))
				case 2:
					errorAsync(t, tr.SetSteps("gem-collector", i/5))
					tr.Flush()
				case 3:
					errorAsync(t, tr.Unlock("first-match"))
					_ = tr.Pending(achievements.GameCenter)
					_ = tr.Progress()
				case 4:
					if g%3 == 0 {
						tr.Disconnect()
					}
					errorAsync(t, tr.Add("apple-only", 0))
				}
			}
		})
	}
	workers.Wait()
	answers.Wait()

	if !tr.Unlocked("first-match") || !tr.Unlocked("gem-collector") || tr.Unlocked("apple-only") {
		t.Fatalf("progress = %+v", tr.Progress())
	}
	if n := unlocks.Load(); n != 2 {
		t.Fatalf("OnUnlock calls = %d, want 2", n)
	}
	if _, err := achievements.EncodeProgress(tr.Progress()); err != nil {
		t.Fatal(err)
	}
	// Once the answers are in, a reliable connection acknowledges everything.
	mustOK(t, tr.Connect(achievements.GameCenter, asyncReporter{wg: &answers}))
	answers.Wait()
	if got := tr.Pending(achievements.GameCenter); len(got) != 0 {
		t.Fatalf("pending = %v", got)
	}
}

// errorAsync reports err from a goroutine other than the test's, where t.Fatal must not be called.
func errorAsync(t *testing.T, err error) {
	if err != nil {
		t.Error(err)
	}
}

func BenchmarkTrackerAdd(b *testing.B) {
	set := &achievements.Set{Version: 1, Achievements: []achievements.Definition{{ID: "steps", Steps: achievements.MaxSteps}}}
	tr, err := achievements.NewTracker(set)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = tr.Add("steps", 1)
	}
}

func BenchmarkTrackerAddReported(b *testing.B) {
	set := &achievements.Set{Version: 1, Achievements: []achievements.Definition{{ID: "steps", Steps: achievements.MaxSteps, Platforms: achievements.Platforms{GameCenter: "gc.steps"}}}}
	tr, err := achievements.NewTracker(set)
	if err != nil {
		b.Fatal(err)
	}
	_ = tr.Connect(achievements.GameCenter, achievements.ReporterFunc(func(_ string, _ float64, done func(error)) { done(nil) }))
	b.ReportAllocs()
	for b.Loop() {
		_ = tr.Add("steps", 1)
	}
}

func BenchmarkEncodeProgress(b *testing.B) {
	p := &achievements.Progress{Version: 1}
	for i := range 64 {
		p.Achievements = append(p.Achievements, achievements.Record{ID: fmt.Sprintf("a%02d", i), Steps: i + 1, Reported: map[achievements.Service]int{achievements.GameCenter: i}})
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := achievements.EncodeProgress(p); err != nil {
			b.Fatal(err)
		}
	}
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func equalProgress(a, b *achievements.Progress) bool {
	if a.Version != b.Version || len(a.Achievements) != len(b.Achievements) {
		return false
	}
	for i := range a.Achievements {
		x, y := a.Achievements[i], b.Achievements[i]
		if x.ID != y.ID || x.Steps != y.Steps || len(x.Reported) != len(y.Reported) {
			return false
		}
		for s, n := range x.Reported {
			if y.Reported[s] != n {
				return false
			}
		}
	}
	return true
}
