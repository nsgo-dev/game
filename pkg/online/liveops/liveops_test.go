package liveops_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

type gameSnapshot struct {
	Level int `json:"level"`
	HP    int `json:"hp"`
}

func TestPanicInLoopUpdateProducesCrashReport(t *testing.T) {
	var reports []liveops.CrashReport
	var cr *liveops.CrashReporter
	var stop func()
	ticks := 0
	state := gameSnapshot{Level: 3, HP: 42}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cr = liveops.NewCrashReporter(liveops.CrashOptions{
			HistorySize: 16,
			Reporter: func(r liveops.CrashReport) {
				reports = append(reports, r)
				if stop != nil {
					stop()
				}
			},
			CaptureState: func() any { return state },
			Metadata: map[string]string{
				"version": "1.2.3",
				"scene":   "dungeon",
			},
			Now: cx.Now,
		})
		l := cr.WrapLoop(&game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				ticks++
				state.HP -= 5
				cr.RecordStep(dt, fmt.Sprintf("tick-%d", ticks))
				if ticks == 5 {
					panic("enemy health NaN")
				}
			},
		})
		stop = l.Run(cx)
		return core.View{}
	})

	app.Advance(200 * time.Millisecond)

	if len(reports) != 1 {
		t.Fatalf("%d reports, want 1", len(reports))
	}
	rep := reports[0]
	if rep.Panic != "enemy health NaN" || rep.Message != "enemy health NaN" {
		t.Fatalf("panic=%v message=%q, want %q", rep.Panic, rep.Message, "enemy health NaN")
	}
	if !strings.Contains(rep.Stack, "TestPanicInLoopUpdateProducesCrashReport") {
		t.Fatalf("stack trace missing panicking test frame:\n%s", rep.Stack)
	}
	if rep.Timestamp.IsZero() {
		t.Fatal("timestamp is zero")
	}
	if rep.Step != 5 {
		t.Fatalf("step %d, want 5", rep.Step)
	}
	if len(rep.LastSteps) != 5 {
		t.Fatalf("%d last steps, want 5", len(rep.LastSteps))
	}
	wantDT := time.Second / 60
	for i, sr := range rep.LastSteps {
		wantStep := uint64(i + 1)
		wantSummary := fmt.Sprintf("tick-%d", i+1)
		if sr.Step != wantStep || sr.Summary != wantSummary || sr.DT != wantDT {
			t.Fatalf("step[%d] = %+v, want step=%d summary=%q dt=%v", i, sr, wantStep, wantSummary, wantDT)
		}
	}
	gotState, ok := rep.State.(gameSnapshot)
	if !ok || gotState != (gameSnapshot{Level: 3, HP: 17}) {
		t.Fatalf("state %#v, want {Level:3 HP:17}", rep.State)
	}
	if rep.Metadata["version"] != "1.2.3" || rep.Metadata["scene"] != "dungeon" {
		t.Fatalf("metadata %v, want version=1.2.3 scene=dungeon", rep.Metadata)
	}

	// Mutating reporter metadata after capture must not affect already delivered reports.
	cr.SetMetadata("scene", "title")
	cr.DeleteMetadata("version")
	if rep.Metadata["scene"] != "dungeon" || rep.Metadata["version"] != "1.2.3" {
		t.Fatalf("report metadata mutated after delivery: %v", rep.Metadata)
	}
}

func TestGuardLoopCatchesDrawAndAutoRecordedUpdatePanics(t *testing.T) {
	t.Run("draw panic", func(t *testing.T) {
		var reports []liveops.CrashReport
		var stop func()
		draws := 0

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cr := liveops.NewCrashReporter(liveops.CrashOptions{
				Reporter: func(r liveops.CrashReport) {
					reports = append(reports, r)
					if stop != nil {
						stop()
					}
				},
				CaptureState: func() any { return map[string]any{"draws": draws} },
				Now:          cx.Now,
			})
			l := &game.Loop{
				Rate:   60,
				Update: func(time.Duration) {},
				Draw: func(float64) {
					draws++
					if draws == 3 {
						panic("draw boom")
					}
				},
			}
			cr.GuardLoop(l)
			stop = l.Run(cx)
			return core.View{}
		})

		app.Advance(200 * time.Millisecond)

		if len(reports) != 1 {
			t.Fatalf("%d reports, want 1", len(reports))
		}
		rep := reports[0]
		if rep.Message != "draw boom" {
			t.Fatalf("message %q, want %q", rep.Message, "draw boom")
		}
		if len(rep.LastSteps) != 3 {
			t.Fatalf("%d last steps, want 3 auto-recorded update steps", len(rep.LastSteps))
		}
		for i, sr := range rep.LastSteps {
			if sr.Step != uint64(i+1) || sr.Summary != "update" {
				t.Fatalf("step[%d] = %+v, want step=%d summary=update", i, sr, i+1)
			}
		}
		if rep.Stack == "" {
			t.Fatal("empty stack trace")
		}
	})

	t.Run("update panic without manual RecordStep", func(t *testing.T) {
		var reports []liveops.CrashReport
		var stop func()
		updates := 0

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cr := liveops.NewCrashReporter(liveops.CrashOptions{
				Reporter: func(r liveops.CrashReport) {
					reports = append(reports, r)
					if stop != nil {
						stop()
					}
				},
			})
			l := &game.Loop{
				Rate: 60,
				Update: func(time.Duration) {
					updates++
					if updates == 4 {
						panic("auto step panic")
					}
				},
			}
			cr.GuardLoop(l)
			stop = l.Run(cx)
			return core.View{}
		})

		app.Advance(200 * time.Millisecond)

		if len(reports) != 1 {
			t.Fatalf("%d reports, want 1", len(reports))
		}
		if reports[0].Step != 4 || len(reports[0].LastSteps) != 4 {
			t.Fatalf("step=%d len(last_steps)=%d, want 4 and 4", reports[0].Step, len(reports[0].LastSteps))
		}
	})
}

func TestCrashRingBufferWraparoundAndZeroAllocs(t *testing.T) {
	cr := liveops.NewCrashReporter(liveops.CrashOptions{HistorySize: 5})
	for i := 1; i <= 12; i++ {
		got := cr.RecordStep(16*time.Millisecond, fmt.Sprintf("s%d", i))
		if got != uint64(i) {
			t.Fatalf("step %d, want %d", got, i)
		}
	}
	steps := cr.LastSteps()
	if len(steps) != 5 {
		t.Fatalf("%d steps, want 5", len(steps))
	}
	for i, sr := range steps {
		want := uint64(8 + i)
		if sr.Step != want || sr.Summary != fmt.Sprintf("s%d", want) {
			t.Fatalf("steps[%d] = %+v, want step %d", i, sr, want)
		}
	}

	// Default history size keeps the last DefaultHistorySize steps chronologically.
	def := liveops.NewCrashReporter(liveops.CrashOptions{})
	for i := 1; i <= liveops.DefaultHistorySize+30; i++ {
		def.RecordStep(time.Millisecond, "tick")
	}
	defSteps := def.LastSteps()
	if len(defSteps) != liveops.DefaultHistorySize {
		t.Fatalf("default history len %d, want %d", len(defSteps), liveops.DefaultHistorySize)
	}
	if defSteps[0].Step != 31 || defSteps[len(defSteps)-1].Step != uint64(liveops.DefaultHistorySize+30) {
		t.Fatalf("default history range [%d..%d], want [31..%d]", defSteps[0].Step, defSteps[len(defSteps)-1].Step, liveops.DefaultHistorySize+30)
	}

	// Steady-state RecordStep must not allocate.
	allocs := testing.AllocsPerRun(100, func() {
		def.RecordStep(16*time.Millisecond, "steady")
	})
	if allocs != 0 {
		t.Fatalf("RecordStep allocated %v times per op, want 0", allocs)
	}

	// Reset clears buffered history while preserving monotonic step count.
	before := def.Step()
	def.Reset()
	if len(def.LastSteps()) != 0 {
		t.Fatalf("expected empty LastSteps after Reset, got %d", len(def.LastSteps()))
	}
	if next := def.RecordStep(time.Millisecond, "after-reset"); next != before+1 {
		t.Fatalf("step after Reset = %d, want %d", next, before+1)
	}
}

func TestCrashReportJSONRoundTrip(t *testing.T) {
	fixedTime := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		panicVal  any
		wantPanic string
	}{
		{"string", "division by zero", "division by zero"},
		{"error", errors.New("invalid entity id"), "invalid entity id"},
		{"non-json func", func() {}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured liveops.CrashReport
			cr := liveops.NewCrashReporter(liveops.CrashOptions{
				HistorySize:  4,
				Reporter:     func(r liveops.CrashReport) { captured = r },
				CaptureState: func() any { return map[string]any{"score": 99} },
				Metadata:     map[string]string{"os": "macos"},
				Now:          func() time.Time { return fixedTime },
			})
			cr.RecordStepData(16*time.Millisecond, "ai", map[string]any{"entities": 4})

			if !cr.RunSafe(func() { panic(tc.panicVal) }) {
				t.Fatal("RunSafe reported no panic")
			}

			raw, err := json.Marshal(captured)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}
			var decoded liveops.CrashReport
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}
			if !decoded.Timestamp.Equal(fixedTime) {
				t.Fatalf("timestamp %v, want %v", decoded.Timestamp, fixedTime)
			}
			if decoded.Message != captured.Message {
				t.Fatalf("message %q, want %q", decoded.Message, captured.Message)
			}
			if tc.wantPanic != "" && decoded.Panic != tc.wantPanic {
				t.Fatalf("decoded panic %#v, want %q", decoded.Panic, tc.wantPanic)
			}
			if decoded.Panic == nil {
				t.Fatal("decoded panic is nil")
			}
			if decoded.Step != 1 || len(decoded.LastSteps) != 1 || decoded.LastSteps[0].Summary != "ai" {
				t.Fatalf("decoded steps mismatch: step=%d steps=%+v", decoded.Step, decoded.LastSteps)
			}
			if decoded.Metadata["os"] != "macos" {
				t.Fatalf("decoded metadata %v, want os=macos", decoded.Metadata)
			}
			if decoded.Stack == "" {
				t.Fatal("decoded stack is empty")
			}
		})
	}
}

func TestCrashReporterRethrow(t *testing.T) {
	var reported []liveops.CrashReport
	cr := liveops.NewCrashReporter(liveops.CrashOptions{
		Reporter: func(r liveops.CrashReport) { reported = append(reported, r) },
		Rethrow:  true,
	})
	cr.RecordStep(16*time.Millisecond, "before-rethrow")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		cr.RunSafe(func() { panic("fatal crash") })
	}()

	if recovered != "fatal crash" {
		t.Fatalf("recovered %v, want %q", recovered, "fatal crash")
	}
	if len(reported) != 1 || reported[0].Message != "fatal crash" {
		t.Fatalf("reported %+v, want 1 report with fatal crash", reported)
	}

	// Verify Rethrow also works through WrapLoop.
	wrapped := cr.WrapLoop(&game.Loop{
		Update: func(time.Duration) { panic("loop fatal") },
	})
	recovered = nil
	func() {
		defer func() { recovered = recover() }()
		wrapped.Update(16 * time.Millisecond)
	}()
	if recovered != "loop fatal" {
		t.Fatalf("wrapped loop recovered %v, want %q", recovered, "loop fatal")
	}
	if len(reported) != 2 || reported[1].Message != "loop fatal" {
		t.Fatalf("reported %+v, want 2 reports", reported)
	}
}

func TestCrashReporterDefendsAgainstPanickingCaptureStateAndNilInputs(t *testing.T) {
	var got liveops.CrashReport
	cr := liveops.NewCrashReporter(liveops.CrashOptions{
		Reporter:     func(r liveops.CrashReport) { got = r },
		CaptureState: func() any { panic("state corrupted") },
	})
	if cr.WrapLoop(nil) != nil {
		t.Fatal("WrapLoop(nil) returned non-nil")
	}
	cr.GuardLoop(nil)
	if cr.RunSafe(nil) {
		t.Fatal("RunSafe(nil) returned true")
	}
	if !cr.RunSafe(func() { panic("primary boom") }) {
		t.Fatal("RunSafe did not catch panic")
	}
	if got.Message != "primary boom" {
		t.Fatalf("message %q, want primary boom", got.Message)
	}
	if s, ok := got.State.(string); !ok || !strings.Contains(s, "state corrupted") {
		t.Fatalf("state %#v, want fallback capture panic message", got.State)
	}
}

type fakeAnalyticsProvider struct {
	events  []liveops.Event
	flushes int
	sendErr error
}

func (p *fakeAnalyticsProvider) Send(ev liveops.Event) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.events = append(p.events, ev)
	return nil
}

func (p *fakeAnalyticsProvider) Flush() error {
	p.flushes++
	return nil
}

func TestAnalyticsStrictlyRequiresExplicitConsent(t *testing.T) {
	prov := &fakeAnalyticsProvider{}
	var an *liveops.Analytics

	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an = liveops.NewAnalytics(cx.Storage(), "analytics.consent", prov)
		an.SetClock(cx.Now)
		return core.View{}
	})

	if an.Consent() != liveops.ConsentUnknown {
		t.Fatalf("initial consent %v, want ConsentUnknown", an.Consent())
	}
	if an.Track("before_choice", map[string]any{"step": 1}) {
		t.Fatal("Track returned true under ConsentUnknown")
	}
	if err := an.Flush(); err != nil {
		t.Fatalf("Flush under ConsentUnknown: %v", err)
	}
	if len(prov.events) != 0 || prov.flushes != 0 {
		t.Fatalf("provider touched before consent: %d events, %d flushes", len(prov.events), prov.flushes)
	}

	if err := an.SetConsent(liveops.ConsentDenied); err != nil {
		t.Fatalf("SetConsent(Denied): %v", err)
	}
	if an.Consent() != liveops.ConsentDenied {
		t.Fatalf("consent %v, want ConsentDenied", an.Consent())
	}
	if an.Track("while_denied", map[string]any{"step": 2}) {
		t.Fatal("Track returned true under ConsentDenied")
	}
	if err := an.Flush(); err != nil {
		t.Fatalf("Flush under ConsentDenied: %v", err)
	}
	if len(prov.events) != 0 || prov.flushes != 0 {
		t.Fatalf("provider touched while denied: %d events, %d flushes", len(prov.events), prov.flushes)
	}

	// Granting consent must NOT retroactively send any earlier events.
	if err := an.SetConsent(liveops.ConsentGranted); err != nil {
		t.Fatalf("SetConsent(Granted): %v", err)
	}
	if len(prov.events) != 0 {
		t.Fatalf("%d buffered events leaked on grant, want 0", len(prov.events))
	}

	// Track with defensive copy of props (including nested map).
	nested := map[string]any{"weapon": "sword"}
	props := map[string]any{"level": 1, "loadout": nested}
	if !an.Track("level_start", props) {
		t.Fatal("Track returned false under ConsentGranted")
	}
	props["level"] = 999
	nested["weapon"] = "mutated"

	if len(prov.events) != 1 {
		t.Fatalf("%d events, want 1", len(prov.events))
	}
	ev := prov.events[0]
	if ev.Name != "level_start" || ev.Time.IsZero() {
		t.Fatalf("event %+v, want level_start with non-zero time", ev)
	}
	if ev.Properties["level"] != 1 {
		t.Fatalf("event properties mutated by caller: %v", ev.Properties)
	}
	gotLoadout, ok := ev.Properties["loadout"].(map[string]any)
	if !ok || gotLoadout["weapon"] != "sword" {
		t.Fatalf("nested properties mutated by caller: %v", ev.Properties)
	}

	if err := an.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if prov.flushes != 1 {
		t.Fatalf("%d flushes, want 1", prov.flushes)
	}

	// Revoking consent stops future events immediately.
	if err := an.SetConsent(liveops.ConsentDenied); err != nil {
		t.Fatalf("revoke consent: %v", err)
	}
	if an.Track("after_revoke", nil) {
		t.Fatal("Track returned true after revocation")
	}
	if err := an.Flush(); err != nil {
		t.Fatalf("Flush after revoke: %v", err)
	}
	if len(prov.events) != 1 || prov.flushes != 1 {
		t.Fatalf("provider received calls after revoke: %d events, %d flushes", len(prov.events), prov.flushes)
	}
}

func TestAnalyticsConsentPersistsAcrossRestart(t *testing.T) {
	snapshotStorage := func(app *nsgotest.App) map[string]string {
		kept := map[string]string{}
		st := app.Storage()
		for _, k := range st.Keys() {
			kept[k], _ = st.GetItem(k)
		}
		return kept
	}

	// Launch 1: grant consent.
	prov1 := &fakeAnalyticsProvider{}
	var an1 *liveops.Analytics
	app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an1 = liveops.NewAnalytics(cx.Storage(), "game.consent", prov1)
		return core.View{}
	})
	if err := an1.SetConsent(liveops.ConsentGranted); err != nil {
		t.Fatalf("SetConsent(Granted): %v", err)
	}
	keptGranted := snapshotStorage(app1)

	// Launch 2: consent remains granted, events flow, then player revokes consent.
	prov2 := &fakeAnalyticsProvider{}
	var an2 *liveops.Analytics
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an2 = liveops.NewAnalytics(cx.Storage(), "game.consent", prov2)
		return core.View{}
	}, nsgotest.Storage(keptGranted))
	if an2.Consent() != liveops.ConsentGranted {
		t.Fatalf("after restart consent=%v, want ConsentGranted", an2.Consent())
	}
	if !an2.Track("session_resume", nil) || len(prov2.events) != 1 {
		t.Fatalf("expected consented event after restart, got %d events", len(prov2.events))
	}
	if err := an2.SetConsent(liveops.ConsentDenied); err != nil {
		t.Fatalf("SetConsent(Denied): %v", err)
	}
	keptDenied := snapshotStorage(app2)

	// Launch 3: denial persists across restart and blocks all events.
	prov3 := &fakeAnalyticsProvider{}
	var an3 *liveops.Analytics
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an3 = liveops.NewAnalytics(cx.Storage(), "game.consent", prov3)
		return core.View{}
	}, nsgotest.Storage(keptDenied))
	if an3.Consent() != liveops.ConsentDenied {
		t.Fatalf("after restart consent=%v, want ConsentDenied", an3.Consent())
	}
	if an3.Track("blocked_after_restart", nil) || len(prov3.events) != 0 {
		t.Fatalf("event sent after persisted denial: %d events", len(prov3.events))
	}

	// Launch 4: unrecognized persisted value fails closed to ConsentUnknown.
	prov4 := &fakeAnalyticsProvider{}
	var an4 *liveops.Analytics
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an4 = liveops.NewAnalytics(cx.Storage(), "game.consent", prov4)
		return core.View{}
	}, nsgotest.Storage(map[string]string{"game.consent": "unexpected-value"}))
	if an4.Consent() != liveops.ConsentUnknown {
		t.Fatalf("corrupt storage consent=%v, want ConsentUnknown", an4.Consent())
	}
	if an4.Track("blocked_corrupt", nil) || len(prov4.events) != 0 {
		t.Fatal("event sent under corrupt consent value")
	}
}

func TestAnalyticsNilProviderErrorsAndMemoryMode(t *testing.T) {
	// Nil provider is safe even when consent is granted.
	nilAn := liveops.NewMemoryAnalytics(nil)
	if err := nilAn.SetConsent(liveops.ConsentGranted); err != nil {
		t.Fatalf("SetConsent: %v", err)
	}
	if nilAn.Track("noop", map[string]any{"k": "v"}) {
		t.Fatal("Track with nil provider returned true")
	}
	if err := nilAn.Flush(); err != nil {
		t.Fatalf("Flush with nil provider: %v", err)
	}

	// Invalid consent state is rejected without changing current state.
	if err := nilAn.SetConsent(liveops.ConsentState(99)); !errors.Is(err, liveops.ErrInvalidConsent) {
		t.Fatalf("invalid consent err=%v, want ErrInvalidConsent", err)
	}
	if nilAn.Consent() != liveops.ConsentGranted {
		t.Fatalf("consent changed after invalid SetConsent: %v", nilAn.Consent())
	}

	// Provider send errors return false from Track.
	failProv := &fakeAnalyticsProvider{sendErr: errors.New("network down")}
	nilAn.SetProvider(failProv)
	if nilAn.Track("will_fail", nil) {
		t.Fatal("Track returned true despite provider.Send error")
	}

	// Resetting to ConsentUnknown removes stored key and stops tracking.
	prov := &fakeAnalyticsProvider{}
	var stored *liveops.Analytics
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stored = liveops.NewAnalytics(cx.Storage(), "", prov)
		return core.View{}
	})
	_ = stored.SetConsent(liveops.ConsentGranted)
	if !stored.Track("ok", nil) || len(prov.events) != 1 {
		t.Fatal("expected event under granted consent")
	}
	if err := stored.SetConsent(liveops.ConsentUnknown); err != nil {
		t.Fatalf("SetConsent(Unknown): %v", err)
	}
	if _, exists := app.Storage().GetItem(liveops.DefaultConsentKey); exists {
		t.Fatal("default consent key still present after reset to ConsentUnknown")
	}
	if stored.Track("blocked", nil) || len(prov.events) != 1 {
		t.Fatal("event sent after reset to ConsentUnknown")
	}
}

func TestAnalyticsTracksPersistedConsentOnEveryEvent(t *testing.T) {
	prov := &fakeAnalyticsProvider{}
	var an *liveops.Analytics
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		an = liveops.NewAnalytics(cx.Storage(), "game.consent", prov)
		return core.View{}
	})
	if err := an.SetConsent(liveops.ConsentGranted); err != nil {
		t.Fatalf("SetConsent(Granted): %v", err)
	}
	if !an.Track("before_revocation", nil) || len(prov.events) != 1 {
		t.Fatalf("expected one consented event, got %d", len(prov.events))
	}

	// Revoking consent outside this instance, as a settings screen backed by the
	// same storage would, must stop the next Track call.
	app.Storage().RemoveItem("game.consent")
	if an.Consent() != liveops.ConsentUnknown {
		t.Fatalf("Consent after external clear = %v, want ConsentUnknown", an.Consent())
	}
	if an.Track("after_revocation", nil) {
		t.Fatal("Track returned true after consent was revoked externally")
	}
	if err := an.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if prov.flushes != 0 {
		t.Fatalf("Flush reached the provider after revocation: %d", prov.flushes)
	}
}
