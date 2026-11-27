package liveops_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var reviewEnv = nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone, CanRequestReview: true})

func startReviewApp(t *testing.T, opts liveops.ReviewPromptOptions, appOpts ...nsgotest.Option) (*nsgotest.App, *core.Scope, *liveops.ReviewPrompt) {
	t.Helper()
	var scope *core.Scope
	var rp *liveops.ReviewPrompt
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		o := opts
		if o.Now == nil {
			o.Now = cx.WallTime
		}
		rp = liveops.NewReviewPrompt(cx.Storage(), o)
		return core.View{}
	}, appOpts...)
	return app, scope, rp
}

func askReview(app *nsgotest.App, cx *core.Scope, rp *liveops.ReviewPrompt) bool {
	var asked bool
	app.Post(func() {
		asked = rp.MaybeAsk(cx)
	})
	app.Settle()
	return asked
}

func TestReviewPromptNoPromptInFirstSession(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion: "1.0.0",
	}, reviewEnv, nsgotest.WallTime(day0))

	// Even after advancing past MinDays and recording many positive moments, session 1 never prompts.
	app.FastForward(10 * 24 * time.Hour)
	for range 10 {
		rp.Moment(liveops.MomentPositive)
	}
	if rp.Eligible() {
		t.Fatal("Eligible() = true in first session, want false")
	}
	if askReview(app, cx, rp) {
		t.Fatal("MaybeAsk returned true in first session, want false")
	}
	if app.ReviewRequests != 0 {
		t.Fatalf("ReviewRequests = %d, want 0", app.ReviewRequests)
	}
}

func TestReviewPromptThresholdsAcrossSessions(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	opts := liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        3,
		MinDays:            3,
		MinPositiveMoments: 3,
		Cooldown:           30 * 24 * time.Hour,
	}

	// Session 1 (day 0): 2 positive moments.
	app1, cx1, rp1 := startReviewApp(t, opts, reviewEnv, nsgotest.WallTime(day0))
	rp1.Moment(liveops.MomentPositive)
	rp1.Moment(liveops.MomentPositive)
	if askReview(app1, cx1, rp1) {
		t.Fatal("session 1 should not prompt")
	}
	snap1 := storageSnapshot(cx1)

	// Session 2 (day 1): 1 more positive moment (total 3), still below MinSessions and MinDays.
	app2, cx2, rp2 := startReviewApp(t, opts, reviewEnv, nsgotest.WallTime(day0.Add(24*time.Hour)), nsgotest.Storage(snap1))
	rp2.Moment(liveops.MomentPositive)
	if askReview(app2, cx2, rp2) {
		t.Fatal("session 2 should not prompt")
	}
	snap2 := storageSnapshot(cx2)

	// Session 3 (day 2): MinSessions (3) and MinPositiveMoments (3) met, but only 2 days elapsed (< MinDays=3).
	app3, cx3, rp3 := startReviewApp(t, opts, reviewEnv, nsgotest.WallTime(day0.Add(2*24*time.Hour)), nsgotest.Storage(snap2))
	if askReview(app3, cx3, rp3) {
		t.Fatal("should not prompt before MinDays elapses")
	}

	// Advance 24h so 3 full days have elapsed since first launch.
	app3.FastForward(24 * time.Hour)
	if !rp3.Eligible() {
		t.Fatal("Eligible() = false once all thresholds are met, want true")
	}
	if !askReview(app3, cx3, rp3) {
		t.Fatal("MaybeAsk returned false once all thresholds were met, want true")
	}
	if app1.ReviewRequests != 0 || app2.ReviewRequests != 0 || app3.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests = [%d, %d, %d], want [0, 0, 1]", app1.ReviewRequests, app2.ReviewRequests, app3.ReviewRequests)
	}

	// Never prompts twice in the same version.
	if askReview(app3, cx3, rp3) {
		t.Fatal("second MaybeAsk in same version returned true, want false")
	}
	if app3.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests after second call = %d, want 1", app3.ReviewRequests)
	}
}

func TestReviewPromptNegativeMomentSuppressesSession(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	opts := liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 2,
	}

	// Session 1: build up positive moments.
	_, cx1, rp1 := startReviewApp(t, opts, reviewEnv, nsgotest.WallTime(day0))
	rp1.Moment(liveops.MomentPositive)
	rp1.Moment(liveops.MomentPositive)
	snap := storageSnapshot(cx1)

	// Session 2 (day 2): all thresholds met, then a negative moment occurs.
	app2, cx2, rp2 := startReviewApp(t, opts, reviewEnv, nsgotest.WallTime(day0.Add(48*time.Hour)), nsgotest.Storage(snap))
	if !rp2.Eligible() {
		t.Fatal("expected session 2 to start eligible")
	}
	rp2.Moment(liveops.MomentNegative)
	if rp2.Eligible() {
		t.Fatal("Eligible() = true after MomentNegative, want false")
	}
	if askReview(app2, cx2, rp2) {
		t.Fatal("MaybeAsk returned true after MomentNegative, want false")
	}
	// A later positive moment in the same session still stays suppressed.
	rp2.Moment(liveops.MomentPositive)
	if askReview(app2, cx2, rp2) {
		t.Fatal("MaybeAsk returned true in a session that already had MomentNegative, want false")
	}
	if app2.ReviewRequests != 0 {
		t.Fatalf("ReviewRequests = %d, want 0", app2.ReviewRequests)
	}

	// Starting a clean new session via RecordSession clears negative suppression.
	rp2.RecordSession()
	rp2.Moment(liveops.MomentPositive)
	if !askReview(app2, cx2, rp2) {
		t.Fatal("MaybeAsk returned false in clean session after RecordSession, want true")
	}
	if app2.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests = %d, want 1", app2.ReviewRequests)
	}
}

func TestReviewPromptOncePerVersionCooldownAndVersionReset(t *testing.T) {
	day0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	baseOpts := liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 2,
		Cooldown:           60 * 24 * time.Hour,
	}

	// Session 1 on 1.0.0.
	_, cx1, rp1 := startReviewApp(t, baseOpts, reviewEnv, nsgotest.WallTime(day0))
	rp1.Moment(liveops.MomentPositive)
	rp1.Moment(liveops.MomentPositive)
	snap := storageSnapshot(cx1)

	// Session 2 on 1.0.0 (day 2): prompts once.
	app2, cx2, rp2 := startReviewApp(t, baseOpts, reviewEnv, nsgotest.WallTime(day0.Add(2*24*time.Hour)), nsgotest.Storage(snap))
	if !askReview(app2, cx2, rp2) {
		t.Fatal("expected prompt in v1.0.0")
	}
	if app2.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests = %d, want 1", app2.ReviewRequests)
	}
	// Even after advancing past cooldown, still never asks twice for v1.0.0.
	app2.FastForward(90 * 24 * time.Hour)
	rp2.Moment(liveops.MomentPositive)
	if askReview(app2, cx2, rp2) {
		t.Fatal("prompted twice for v1.0.0 after cooldown")
	}
	snapAfterV1 := storageSnapshot(cx2)

	// Upgrade to v1.1.0 inside the 60-day cooldown (day 20, only 18 days since day 2 prompt):
	// positiveMoments reset on version change, and cooldown blocks prompting even after earning new positive moments.
	v11Opts := baseOpts
	v11Opts.AppVersion = "1.1.0"
	app3, cx3, rp3 := startReviewApp(t, v11Opts, reviewEnv, nsgotest.WallTime(day0.Add(20*24*time.Hour)), nsgotest.Storage(snapAfterV1))
	rp3.Moment(liveops.MomentPositive)
	rp3.Moment(liveops.MomentPositive)
	if askReview(app3, cx3, rp3) {
		t.Fatal("prompted for v1.1.0 inside cooldown")
	}
	if app3.ReviewRequests != 0 {
		t.Fatalf("ReviewRequests inside cooldown = %d, want 0", app3.ReviewRequests)
	}

	// Advance to day 61 (59 days since day 2 prompt): still inside 60-day cooldown.
	app3.FastForward(41 * 24 * time.Hour)
	if askReview(app3, cx3, rp3) {
		t.Fatal("prompted 1 day before cooldown expired")
	}

	// Advance 1 more day (60 days since day 2 prompt): cooldown met, new version prompts once!
	app3.FastForward(24 * time.Hour)
	if !askReview(app3, cx3, rp3) {
		t.Fatal("expected prompt in v1.1.0 once cooldown expired")
	}
	if app3.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests for v1.1.0 = %d, want 1", app3.ReviewRequests)
	}
	if askReview(app3, cx3, rp3) {
		t.Fatal("prompted twice for v1.1.0")
	}
	snapAfterV11 := storageSnapshot(cx3)

	// Upgrade to v1.2.0 well past cooldown: must earn fresh positive moments in v1.2.0 before prompting.
	v12Opts := baseOpts
	v12Opts.AppVersion = "1.2.0"
	app4, cx4, rp4 := startReviewApp(t, v12Opts, reviewEnv, nsgotest.WallTime(day0.Add(200*24*time.Hour)), nsgotest.Storage(snapAfterV11))
	if askReview(app4, cx4, rp4) {
		t.Fatal("prompted on v1.2.0 before earning fresh positive moments in the new version")
	}
	rp4.Moment(liveops.MomentPositive)
	if askReview(app4, cx4, rp4) {
		t.Fatal("prompted on v1.2.0 with only 1 of 2 required positive moments")
	}
	rp4.Moment(liveops.MomentPositive)
	if !askReview(app4, cx4, rp4) {
		t.Fatal("expected prompt on v1.2.0 after meeting fresh positive moments")
	}
	if app4.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests for v1.2.0 = %d, want 1", app4.ReviewRequests)
	}
}

func TestReviewPromptUnsupportedPlatformIsNoOp(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	opts := liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 1,
	}

	for _, tc := range []struct {
		name string
		env  core.Environment
	}{
		{"windows", core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop}},
		{"linux", core.Environment{OS: core.OSLinux, Idiom: core.IdiomDesktop}},
		{"web", core.Environment{OS: core.OSWeb, Idiom: core.IdiomDesktop}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, cx, rp := startReviewApp(t, opts, nsgotest.Environment(tc.env), nsgotest.WallTime(day0))
			rp.RecordSession()
			rp.Moment(liveops.MomentPositive)
			app.FastForward(48 * time.Hour)

			if !rp.Eligible() {
				t.Fatal("policy should be eligible independent of platform")
			}
			if askReview(app, cx, rp) {
				t.Fatal("MaybeAsk returned true on unsupported platform, want false")
			}
			if app.ReviewRequests != 0 {
				t.Fatalf("ReviewRequests = %d, want 0", app.ReviewRequests)
			}

			// Because MaybeAsk was a no-op, the version was not marked prompted:
			// if the environment later supports review requests, it can still ask.
			app.SetEnvironment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone, CanRequestReview: true})
			if !askReview(app, cx, rp) {
				t.Fatal("MaybeAsk should still be able to ask once CanRequestReview becomes true")
			}
			if app.ReviewRequests != 1 {
				t.Fatalf("ReviewRequests = %d, want 1", app.ReviewRequests)
			}
		})
	}
}

func TestReviewPromptSuppressAndUnknownMoment(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	suppressed := true
	app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 1,
		Suppress:           func() bool { return suppressed },
	}, reviewEnv, nsgotest.WallTime(day0))

	rp.RecordSession()
	// Unknown moments are ignored and never count as positive.
	rp.Moment(liveops.ReviewMoment(0))
	rp.Moment(liveops.ReviewMoment(99))
	app.FastForward(48 * time.Hour)
	suppressed = false
	if askReview(app, cx, rp) {
		t.Fatal("unknown moment counted as positive")
	}

	rp.Moment(liveops.MomentPositive)
	suppressed = true
	if rp.Eligible() {
		t.Fatal("Eligible() = true while suppressed, want false")
	}
	if askReview(app, cx, rp) {
		t.Fatal("MaybeAsk returned true while suppressed, want false")
	}
	if app.ReviewRequests != 0 {
		t.Fatalf("ReviewRequests while suppressed = %d, want 0", app.ReviewRequests)
	}

	suppressed = false
	if !askReview(app, cx, rp) {
		t.Fatal("MaybeAsk returned false after suppression cleared, want true")
	}
	if app.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests = %d, want 1", app.ReviewRequests)
	}
}

func TestReviewPromptCorruptStateFailsClosed(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"garbage", "{not json"},
		{"null", "null"},
		{"array", "[]"},
		{"empty_object", "{}"},
		{"version_zero", `{"version":0,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5}`},
		{"future_version", `{"version":2,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5}`},
		{"unknown_field", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5,"extra":true}`},
		{"trailing_data", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5} {}`},
		{"negative_sessions", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":-1,"positiveMoments":5}`},
		{"negative_moments", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":-2}`},
		{"missing_first_launch", `{"version":1,"appVersion":"1.0.0","sessions":5,"positiveMoments":5}`},
		{"bad_first_launch", `{"version":1,"appVersion":"1.0.0","firstLaunch":"yesterday","sessions":5,"positiveMoments":5}`},
		{"bad_last_prompted_at", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5,"lastPromptedAt":"nope"}`},
		{"prompted_version_without_time", `{"version":1,"appVersion":"1.0.0","firstLaunch":"2026-01-01T00:00:00Z","sessions":5,"positiveMoments":5,"lastPromptedVersion":"0.9.0"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
				AppVersion:         "1.0.0",
				MinSessions:        2,
				MinDays:            1,
				MinPositiveMoments: 1,
			}, reviewEnv, nsgotest.WallTime(day0), nsgotest.Storage(map[string]string{
				liveops.DefaultReviewPromptKey: tc.raw,
			}))

			rp.RecordSession()
			rp.Moment(liveops.MomentPositive)
			app.FastForward(10 * 24 * time.Hour)

			if rp.Eligible() {
				t.Fatalf("Eligible() = true for corrupt state %q, want false", tc.raw)
			}
			if askReview(app, cx, rp) {
				t.Fatalf("MaybeAsk returned true for corrupt state %q, want false", tc.raw)
			}
			if app.ReviewRequests != 0 {
				t.Fatalf("ReviewRequests = %d, want 0", app.ReviewRequests)
			}
			// Corrupt or future-version payload must not be silently overwritten.
			got, ok := cx.Storage().GetItem(liveops.DefaultReviewPromptKey)
			if !ok || got != tc.raw {
				t.Fatalf("stored payload changed to (%q, %v), want original (%q, true)", got, ok, tc.raw)
			}
		})
	}
}

func TestReviewPromptExternalCorruptionAndZeroStorageFailClosed(t *testing.T) {
	day0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 1,
	}, reviewEnv, nsgotest.WallTime(day0))

	rp.RecordSession()
	rp.Moment(liveops.MomentPositive)
	app.FastForward(48 * time.Hour)
	if !rp.Eligible() {
		t.Fatal("expected eligible before external corruption")
	}

	// External corruption after initialization fails closed without overwriting.
	const bad = `{"version":99}`
	if err := cx.Storage().SetItem(liveops.DefaultReviewPromptKey, bad); err != nil {
		t.Fatal(err)
	}
	if rp.Eligible() {
		t.Fatal("Eligible() = true after external corruption, want false")
	}
	if askReview(app, cx, rp) {
		t.Fatal("MaybeAsk returned true after external corruption, want false")
	}
	if app.ReviewRequests != 0 {
		t.Fatalf("ReviewRequests = %d, want 0", app.ReviewRequests)
	}
	if got, _ := cx.Storage().GetItem(liveops.DefaultReviewPromptKey); got != bad {
		t.Fatalf("corrupt storage was overwritten with %q", got)
	}

	// Zero core.Storage and nil receivers fail closed without panicking.
	zero := liveops.NewReviewPrompt(core.Storage{}, liveops.ReviewPromptOptions{AppVersion: "1.0.0"})
	zero.RecordSession()
	zero.Moment(liveops.MomentPositive)
	if zero.Eligible() || zero.MaybeAsk(cx) {
		t.Fatal("zero storage ReviewPrompt should fail closed")
	}
	var nilPrompt *liveops.ReviewPrompt
	nilPrompt.RecordSession()
	nilPrompt.Moment(liveops.MomentPositive)
	if nilPrompt.Eligible() || nilPrompt.MaybeAsk(cx) || rp.MaybeAsk(nil) {
		t.Fatal("nil receiver or nil scope should return false")
	}
}

func TestReviewPromptClockSkewBackwardsFailsClosed(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion:         "1.0.0",
		MinSessions:        2,
		MinDays:            2,
		MinPositiveMoments: 1,
		Now:                func() time.Time { return now },
	}, reviewEnv)

	rp.RecordSession()
	rp.Moment(liveops.MomentPositive)

	// Clock jumps backwards before FirstLaunch: must fail closed.
	now = now.Add(-48 * time.Hour)
	if askReview(app, cx, rp) {
		t.Fatal("prompted when clock moved backwards before first launch")
	}

	// Advance forward past MinDays: prompts once.
	now = time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if !askReview(app, cx, rp) {
		t.Fatal("expected prompt once clock moved past MinDays")
	}
	snap := storageSnapshot(cx)

	// Upgrade to 1.1.0 with clock moved earlier than LastPromptedAt: cooldown fails closed.
	now = time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	app2, cx2, rp2 := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion:         "1.1.0",
		MinSessions:        2,
		MinDays:            2,
		MinPositiveMoments: 1,
		Cooldown:           24 * time.Hour,
		Now:                func() time.Time { return now },
	}, reviewEnv, nsgotest.Storage(snap))
	rp2.Moment(liveops.MomentPositive)
	if askReview(app2, cx2, rp2) {
		t.Fatal("prompted on v1.1.0 when clock moved backwards before LastPromptedAt")
	}
	if app.ReviewRequests != 1 {
		t.Fatalf("ReviewRequests = %d, want 1", app.ReviewRequests)
	}
}

func TestReviewPromptPersistedJSONFormatAndMomentStrings(t *testing.T) {
	if got := liveops.MomentPositive.String(); got != "positive" {
		t.Fatalf("MomentPositive.String() = %q, want positive", got)
	}
	if got := liveops.MomentNegative.String(); got != "negative" {
		t.Fatalf("MomentNegative.String() = %q, want negative", got)
	}
	if got := liveops.ReviewMoment(0).String(); got != "unknown" {
		t.Fatalf("ReviewMoment(0).String() = %q, want unknown", got)
	}

	day0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	app, cx, rp := startReviewApp(t, liveops.ReviewPromptOptions{
		AppVersion:         "1.2.3",
		StorageKey:         "custom.review",
		MinSessions:        2,
		MinDays:            1,
		MinPositiveMoments: 1,
	}, reviewEnv, nsgotest.WallTime(day0))

	rp.RecordSession()
	rp.Moment(liveops.MomentPositive)
	app.FastForward(24 * time.Hour)
	if !askReview(app, cx, rp) {
		t.Fatal("expected prompt")
	}

	raw, ok := cx.Storage().GetItem("custom.review")
	if !ok {
		t.Fatal("state not persisted under custom StorageKey")
	}
	want := `{"version":1,"appVersion":"1.2.3","firstLaunch":"2026-03-01T10:00:00Z","sessions":2,"positiveMoments":1,"lastPromptedAt":"2026-03-02T10:00:00Z","lastPromptedVersion":"1.2.3"}`
	if raw != want {
		t.Fatalf("persisted JSON =\n%s\nwant:\n%s", raw, want)
	}
	// Verify valid JSON with deterministic key order.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.HasPrefix(raw, `{"version":1,`) {
		t.Fatalf("JSON does not start with version field: %s", raw)
	}
}
