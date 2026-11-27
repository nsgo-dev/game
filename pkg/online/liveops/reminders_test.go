package liveops_test

import (
	"errors"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/online/liveops"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var schedEnv = nsgotest.Environment(core.Environment{ScheduledNotifications: true})

func sampleRemindersConfig(t *testing.T) *liveops.RemindersConfig {
	t.Helper()
	cfg, err := liveops.ParseReminders([]byte(`
version: 1
quietHours:
  start: "22:00"
  end: "08:00"
reminders:
  - id: energy_full
    title: reminder.energy.title
    body: reminder.energy.body
    delay: 2h
    onOpen: keep
  - id: daily_reward
    title: reminder.daily.title
    body: reminder.daily.body
    at: "09:00"
    repeat: daily
    onOpen: cancel
  - id: come_back
    title: reminder.comeback.title
    body: reminder.comeback.body
    delay: 4h
    onOpen: reschedule
  - id: weekend_event
    title: reminder.weekend.title
    body: reminder.weekend.body
    at: "18:00"
    weekday: friday
    repeat: weekly
    onOpen: keep
    silent: true
`))
	if err != nil {
		t.Fatalf("sampleRemindersConfig: %v", err)
	}
	return cfg
}

func testLocalizer(key string) string {
	switch key {
	case "reminder.energy.title":
		return "Energy Full"
	case "reminder.energy.body":
		return "Your energy has refilled!"
	case "reminder.daily.title":
		return "Daily Reward"
	case "reminder.daily.body":
		return "Claim your daily chest."
	case "reminder.comeback.title":
		return "Come Back"
	case "reminder.comeback.body":
		return "Your camp misses you."
	case "reminder.weekend.title":
		return "Weekend Event"
	case "reminder.weekend.body":
		return "The tournament has begun."
	default:
		return key
	}
}

func storageSnapshot(cx *core.Scope) map[string]string {
	st := cx.Storage()
	out := make(map[string]string)
	for _, k := range st.Keys() {
		out[k], _ = st.GetItem(k)
	}
	return out
}

func findPending(list []core.Notification, id string) (core.Notification, bool) {
	for _, n := range list {
		if n.ID == id {
			return n, true
		}
	}
	return core.Notification{}, false
}

func TestParseRemindersValidation(t *testing.T) {
	t.Run("LoadRemindersFS", func(t *testing.T) {
		raw, err := liveops.EncodeReminders(sampleRemindersConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		fsys := fstest.MapFS{"reminders.yaml": &fstest.MapFile{Data: raw}}
		cfg, err := liveops.LoadRemindersFS(fsys, "reminders.yaml")
		if err != nil {
			t.Fatalf("LoadRemindersFS: %v", err)
		}
		if len(cfg.Reminders) != 4 {
			t.Fatalf("len(Reminders) = %d, want 4", len(cfg.Reminders))
		}
		if _, err := liveops.LoadRemindersFS(fsys, "missing.yaml"); err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	badDocs := []struct {
		name string
		yaml string
	}{
		{"empty", "   \n"},
		{"trailing_doc", "version: 1\nreminders: []\n---\nversion: 1\n"},
		{"duplicate_key", "version: 1\nreminders: []\nreminders: []\n"},
		{"anchor", "version: 1\nquietHours: &q\n  start: \"22:00\"\n  end: \"08:00\"\n"},
		{"non_canonical_version", "version: 01\nreminders: []\n"},
		{"missing_version", "reminders: []\n"},
		{"future_version", "version: 2\nreminders: []\n"},
		{"unknown_field", "version: 1\nextra: true\n"},
		{"bad_quiet_start", "version: 1\nquietHours:\n  start: \"9:00\"\n  end: \"08:00\"\n"},
		{"bad_quiet_end", "version: 1\nquietHours:\n  start: \"22:00\"\n  end: \"24:00\"\n"},
		{"equal_quiet", "version: 1\nquietHours:\n  start: \"22:00\"\n  end: \"22:00\"\n"},
		{"empty_id", "version: 1\nreminders:\n  - id: \"\"\n    title: k\n    delay: 1h\n"},
		{"bad_id_char", "version: 1\nreminders:\n  - id: bad,id\n    title: k\n    delay: 1h\n"},
		{"dup_id", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: 1h\n  - id: a\n    title: k\n    delay: 2h\n"},
		{"missing_title", "version: 1\nreminders:\n  - id: a\n    delay: 1h\n"},
		{"control_char_title", "version: 1\nreminders:\n  - id: a\n    title: \"bad\\nkey\"\n    delay: 1h\n"},
		{"neither_delay_nor_at", "version: 1\nreminders:\n  - id: a\n    title: k\n"},
		{"both_delay_and_at", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: 1h\n    at: \"09:00\"\n"},
		{"negative_delay", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: -1h\n"},
		{"bad_at", "version: 1\nreminders:\n  - id: a\n    title: k\n    at: \"25:00\"\n"},
		{"bad_weekday", "version: 1\nreminders:\n  - id: a\n    title: k\n    at: \"09:00\"\n    weekday: funday\n"},
		{"weekday_without_at", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: 1h\n    weekday: monday\n"},
		{"weekday_with_daily", "version: 1\nreminders:\n  - id: a\n    title: k\n    at: \"09:00\"\n    weekday: monday\n    repeat: daily\n"},
		{"bad_repeat", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: 1h\n    repeat: hourly\n"},
		{"bad_on_open", "version: 1\nreminders:\n  - id: a\n    title: k\n    delay: 1h\n    onOpen: snooze\n"},
	}
	for _, tc := range badDocs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := liveops.ParseReminders([]byte(tc.yaml)); err == nil {
				t.Fatalf("ParseReminders accepted %s", tc.name)
			} else {
				var le *loaderr.Error
				if !errors.As(err, &le) {
					t.Errorf("error %v is not a *loaderr.Error", err)
				}
			}
		})
	}
}

func TestQuietHoursContainsAndNextAllowed(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*3600)
	overnight := liveops.QuietHours{Start: "22:00", End: "08:00"}

	cases := []struct {
		in     time.Time
		inside bool
		want   time.Time
	}{
		{time.Date(2026, 1, 1, 21, 59, 59, 0, tokyo), false, time.Date(2026, 1, 1, 21, 59, 59, 0, tokyo)},
		{time.Date(2026, 1, 1, 22, 0, 0, 0, tokyo), true, time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo)},
		{time.Date(2026, 1, 1, 23, 30, 0, 0, tokyo), true, time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo)},
		{time.Date(2026, 1, 2, 0, 0, 0, 0, tokyo), true, time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo)},
		{time.Date(2026, 1, 2, 7, 59, 59, 0, tokyo), true, time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo)},
		{time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo), false, time.Date(2026, 1, 2, 8, 0, 0, 0, tokyo)},
	}
	for _, tc := range cases {
		if got := overnight.Contains(tc.in); got != tc.inside {
			t.Errorf("overnight.Contains(%v) = %v, want %v", tc.in, got, tc.inside)
		}
		if got := overnight.NextAllowed(tc.in); !got.Equal(tc.want) || got.Location() != tc.want.Location() {
			t.Errorf("overnight.NextAllowed(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}

	daytime := liveops.QuietHours{Start: "13:00", End: "15:00"}
	if daytime.Contains(time.Date(2026, 1, 1, 12, 59, 0, 0, time.UTC)) {
		t.Error("12:59 should be outside daytime quiet hours")
	}
	if !daytime.Contains(time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)) {
		t.Error("13:00 should be inside daytime quiet hours")
	}
	if got, want := daytime.NextAllowed(time.Date(2026, 1, 1, 14, 15, 0, 0, time.UTC)), time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("daytime.NextAllowed = %v, want %v", got, want)
	}
	if daytime.Contains(time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)) {
		t.Error("15:00 should be outside daytime quiet hours")
	}
}

func TestRemindersNeverAskPermissionAtLaunch(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{
			Config:   cfg,
			Localize: testLocalizer,
		})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)))

	if app.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("launch prompted permission: app.NotificationPermission = %v, want NotAsked", app.NotificationPermission)
	}
	if r.Permission() != core.NotificationsNotAsked {
		t.Fatalf("r.Permission() = %v, want NotAsked", r.Permission())
	}
	if !r.CanScheduleNotifications() {
		t.Fatal("CanScheduleNotifications() = false, want true")
	}
	if !r.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
}

func TestRemindersRequestPermissionOnFirstScheduleAndFire(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	var pressed []liveops.ReminderPress

	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{
			Config:   cfg,
			Localize: testLocalizer,
			OnPress:  func(ev liveops.ReminderPress) { pressed = append(pressed, ev) },
		})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	var scheduled bool
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	if !scheduled {
		t.Fatal("Schedule answered false, want true")
	}
	if app.NotificationPermission != core.NotificationsGranted || r.Permission() != core.NotificationsGranted {
		t.Fatalf("permission = app:%v r:%v, want granted", app.NotificationPermission, r.Permission())
	}
	if !r.IsScheduled("energy_full") {
		t.Fatal("IsScheduled(energy_full) = false, want true")
	}
	n, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full")
	if !ok {
		t.Fatalf("pending notifications = %+v, missing energy_full", app.PendingNotifications)
	}
	if n.Title != "Energy Full" || n.Body != "Your energy has refilled!" {
		t.Errorf("localized text = %q / %q", n.Title, n.Body)
	}
	if want := start.Add(2 * time.Hour); !n.At.Equal(want) {
		t.Errorf("At = %v, want %v", n.At, want)
	}

	app.FastForward(2*time.Hour - time.Second)
	if len(app.Notifications) != 0 {
		t.Fatalf("fired early: %+v", app.Notifications)
	}
	app.FastForward(time.Second)
	if len(app.Notifications) != 1 || app.Notifications[0].ID != liveops.DefaultReminderPrefix+"energy_full" {
		t.Fatalf("notifications after 2h = %+v", app.Notifications)
	}
	if r.IsScheduled("energy_full") {
		t.Error("expired one-shot reminder should no longer report IsScheduled")
	}

	app.PressNotification(liveops.DefaultReminderPrefix + "energy_full")
	if !slices.Equal(pressed, []liveops.ReminderPress{{ID: "energy_full"}}) {
		t.Errorf("pressed = %+v, want [{ID:energy_full}]", pressed)
	}
}

func TestRemindersExplicitRequestPermissionAndDeniedRecovery(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)))

	app.NotificationAnswer = core.NotificationsDenied
	var gotPerm core.NotificationPermission
	r.RequestPermission(func(p core.NotificationPermission) { gotPerm = p })
	app.Settle()
	if gotPerm != core.NotificationsDenied || r.Permission() != core.NotificationsDenied {
		t.Fatalf("gotPerm=%v r.Permission=%v, want denied", gotPerm, r.Permission())
	}

	scheduled := true
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if scheduled || len(app.PendingNotifications) != 0 {
		t.Fatalf("denied permission scheduled reminder: ok=%v pending=%+v", scheduled, app.PendingNotifications)
	}

	// Player later enables notifications in OS settings.
	app.NotificationPermission = core.NotificationsGranted
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if !scheduled || len(app.PendingNotifications) != 1 || r.Permission() != core.NotificationsGranted {
		t.Fatalf("after OS grant: ok=%v pending=%+v perm=%v", scheduled, app.PendingNotifications, r.Permission())
	}
}

func TestRemindersWallClockDailyAndWeeklyRepeat(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	// 2026-01-01 is a Thursday.
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	var okDaily, okWeekly bool
	if err := r.Schedule("daily_reward", func(ok bool) { okDaily = ok }); err != nil {
		t.Fatal(err)
	}
	if err := r.Schedule("weekend_event", func(ok bool) { okWeekly = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	if !okDaily || !okWeekly {
		t.Fatalf("daily=%v weekly=%v, want true/true", okDaily, okWeekly)
	}
	daily, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"daily_reward")
	if !ok {
		t.Fatal("missing daily_reward")
	}
	// 09:00 already passed at 10:00, so next occurrence is tomorrow 09:00.
	if want := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC); !daily.At.Equal(want) || daily.Repeat != core.NotificationDaily {
		t.Errorf("daily = At:%v Repeat:%v, want %v Daily", daily.At, daily.Repeat, want)
	}
	weekly, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"weekend_event")
	if !ok {
		t.Fatal("missing weekend_event")
	}
	// Next Friday 18:00 is 2026-01-02 18:00.
	if want := time.Date(2026, 1, 2, 18, 0, 0, 0, time.UTC); !weekly.At.Equal(want) || weekly.Repeat != core.NotificationWeekly || !weekly.Silent {
		t.Errorf("weekly = At:%v Repeat:%v Silent:%v, want %v Weekly silent", weekly.At, weekly.Repeat, weekly.Silent, want)
	}

	// Fast-forward to Friday 09:00 -> daily fires and stays pending for Saturday 09:00.
	app.FastForward(23 * time.Hour)
	if len(app.Notifications) != 1 || app.Notifications[0].ID != liveops.DefaultReminderPrefix+"daily_reward" {
		t.Fatalf("after 23h notifications = %+v", app.Notifications)
	}
	dailyNext, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"daily_reward")
	if !ok || !dailyNext.At.Equal(time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("daily next = %+v, want 2026-01-03 09:00", dailyNext)
	}
}

func TestRemindersQuietHoursDeferralAndUpdate(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	// Start at 21:00 UTC -> 2h delay lands at 23:00 (inside 22:00-08:00 quiet hours) -> deferred to 08:00 next day.
	start := time.Date(2026, 1, 1, 21, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	var cx *core.Scope
	app := nsgotest.StartApp(t, func(s *core.Scope) {
		cx = s
		var err error
		r, err = liveops.NewReminders(s, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	if err := r.Schedule("energy_full", nil); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	n, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full")
	if !ok {
		t.Fatal("missing energy_full")
	}
	wantMorning := time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)
	if !n.At.Equal(wantMorning) {
		t.Fatalf("At = %v, want deferred %v", n.At, wantMorning)
	}

	// Player extends quiet hours to 22:00-09:30 -> already scheduled 08:00 reminder moves to 09:30.
	if err := r.SetQuietHours(&liveops.QuietHours{Start: "22:00", End: "09:30"}); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	n, ok = findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full")
	if !ok {
		t.Fatal("missing energy_full after SetQuietHours")
	}
	wantExtended := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
	if !n.At.Equal(wantExtended) {
		t.Fatalf("At after SetQuietHours = %v, want %v", n.At, wantExtended)
	}

	// Disabling quiet hours persists "off" across restart (does not revert to config default 22:00-08:00).
	if err := r.SetQuietHours(nil); err != nil {
		t.Fatal(err)
	}
	if r.QuietHours() != nil {
		t.Fatalf("QuietHours() = %+v, want nil", r.QuietHours())
	}
	disk := storageSnapshot(cx)
	var r2 *liveops.Reminders
	nsgotest.StartApp(t, func(s *core.Scope) {
		var err error
		r2, err = liveops.NewReminders(s, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.Storage(disk), nsgotest.WallTime(start))
	if r2.QuietHours() != nil {
		t.Fatalf("after restart QuietHours() = %+v, want nil", r2.QuietHours())
	}
}

func TestRemindersForegroundCancelRescheduleKeep(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	for _, id := range []string{"energy_full", "daily_reward", "come_back"} {
		if err := r.Schedule(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	app.Settle()

	if got := r.Scheduled(); !slices.Equal(got, []string{"come_back", "daily_reward", "energy_full"}) {
		t.Fatalf("Scheduled() = %v", got)
	}

	// App goes to background, 1 hour passes, then app returns to foreground.
	app.SetEnvironment(core.Environment{ScheduledNotifications: true, Lifecycle: core.LifecycleBackground})
	app.FastForward(time.Hour)
	app.SetEnvironment(core.Environment{ScheduledNotifications: true, Lifecycle: core.LifecycleActive})
	app.Settle()

	// daily_reward (onOpen: cancel) -> cancelled.
	if _, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"daily_reward"); ok {
		t.Error("daily_reward should be cancelled on foreground")
	}
	if r.IsScheduled("daily_reward") {
		t.Error("IsScheduled(daily_reward) = true, want false")
	}

	// come_back (onOpen: reschedule, delay 4h) -> rescheduled to 11:00 + 4h = 15:00.
	cb, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"come_back")
	if !ok {
		t.Fatal("come_back missing after foreground reschedule")
	}
	if want := start.Add(5 * time.Hour); !cb.At.Equal(want) {
		t.Errorf("come_back At = %v, want %v", cb.At, want)
	}

	// energy_full (onOpen: keep, delay 2h) -> stays at original 12:00.
	ef, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full")
	if !ok {
		t.Fatal("energy_full missing after foreground")
	}
	if want := start.Add(2 * time.Hour); !ef.At.Equal(want) {
		t.Errorf("energy_full At = %v, want %v", ef.At, want)
	}
}

func TestRemindersAcrossRestartReconcilesWithoutPrompting(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// Run 1: schedule all three reminders and persist state.
	var cx1 *core.Scope
	var r1 *liveops.Reminders
	app1 := nsgotest.StartApp(t, func(cx *core.Scope) {
		cx1 = cx
		var err error
		r1, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))
	for _, id := range []string{"energy_full", "daily_reward", "come_back"} {
		if err := r1.Schedule(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	app1.Settle()
	disk := storageSnapshot(cx1)

	// Run 2 (1 hour later, permission already granted by OS from Run 1):
	// launch reconciles without asking permission, rescheduling come_back and cancelling daily_reward.
	run2Time := start.Add(time.Hour)
	var r2 *liveops.Reminders
	app2 := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r2, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.Storage(disk), nsgotest.WallTime(run2Time))

	// Permission was NOT asked at launch (still NotAsked on a fresh nsgotest harness where we didn't pre-grant).
	if app2.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("Run 2 prompted at launch: %v", app2.NotificationPermission)
	}
	// daily_reward was cancelled, come_back failed silently because NotAsked in fresh harness, energy_full kept.
	if r2.IsScheduled("daily_reward") {
		t.Error("daily_reward should be cancelled on launch")
	}
	if !r2.IsScheduled("energy_full") {
		t.Error("energy_full (onOpen: keep) should stay tracked on launch")
	}

	// Run 3: simulate OS already having granted permission from Run 1 before NewReminders starts.
	var cx3 *core.Scope
	app3 := nsgotest.StartApp(t, func(cx *core.Scope) {
		cx3 = cx
	}, schedEnv, nsgotest.Storage(disk), nsgotest.WallTime(run2Time))
	app3.NotificationPermission = core.NotificationsGranted
	r3, err := liveops.NewReminders(cx3, liveops.RemindersOptions{
		Config:   cfg,
		Localize: testLocalizer,
	})
	if err != nil {
		t.Fatal(err)
	}
	app3.Settle()
	cb, ok := findPending(app3.PendingNotifications, liveops.DefaultReminderPrefix+"come_back")
	if !ok {
		t.Fatalf("Run 3 did not reschedule come_back on launch: pending=%+v", app3.PendingNotifications)
	}
	if want := run2Time.Add(4 * time.Hour); !cb.At.Equal(want) {
		t.Errorf("Run 3 come_back At = %v, want %v", cb.At, want)
	}
	if r3.IsScheduled("daily_reward") {
		t.Error("Run 3 should cancel daily_reward on launch")
	}
}

func TestRemindersPlayerSettingCancelAndCancelAll(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var cx *core.Scope
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(s *core.Scope) {
		cx = s
		var err error
		r, err = liveops.NewReminders(s, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	// Schedule an unrelated app notification alongside reminders.
	if err := r.Schedule("energy_full", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Schedule("daily_reward", nil); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if err := cx.ShowNotification(core.Notification{ID: "chat:42", Title: "Chat", At: start.Add(time.Hour)}, nil); err != nil {
		t.Fatal(err)
	}
	app.Settle()

	// Cancel one reminder leaves the other reminder and unrelated notification intact.
	r.Cancel("energy_full")
	app.Settle()
	if _, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full"); ok {
		t.Error("energy_full still pending after Cancel")
	}
	if _, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"daily_reward"); !ok {
		t.Error("daily_reward should stay pending")
	}
	if _, ok := findPending(app.PendingNotifications, "chat:42"); !ok {
		t.Error("unrelated notification should stay pending")
	}

	// Disabling reminders cancels remaining reminders without touching unrelated notifications.
	if err := r.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if r.Enabled() {
		t.Error("Enabled() = true after SetEnabled(false)")
	}
	if _, ok := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"daily_reward"); ok {
		t.Error("daily_reward still pending after SetEnabled(false)")
	}
	if _, ok := findPending(app.PendingNotifications, "chat:42"); !ok {
		t.Error("SetEnabled(false) clobbered unrelated notification")
	}

	// Scheduling while disabled answers false and stays disabled across restart.
	scheduled := true
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if scheduled {
		t.Error("Schedule while disabled answered true")
	}

	disk := storageSnapshot(cx)
	var r2 *liveops.Reminders
	app2 := nsgotest.StartApp(t, func(s *core.Scope) {
		var err error
		r2, err = liveops.NewReminders(s, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.Storage(disk), nsgotest.WallTime(start))
	if r2.Enabled() {
		t.Fatal("disabled setting did not persist across restart")
	}
	// Attempting to schedule while disabled must not prompt for OS permission.
	if err := r2.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app2.Settle()
	if scheduled || app2.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("disabled Schedule prompted or scheduled: ok=%v perm=%v", scheduled, app2.NotificationPermission)
	}
}

func TestRemindersCanScheduleNotificationsUnavailable(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	var r *liveops.Reminders
	var reactiveCan bool
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
		cx.Effect(func() {
			reactiveCan = r.CanScheduleNotifications()
		})
	}, nsgotest.WallTime(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)))

	if r.CanScheduleNotifications() || reactiveCan {
		t.Fatalf("CanScheduleNotifications = %v / reactive %v, want false", r.CanScheduleNotifications(), reactiveCan)
	}
	scheduled := true
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if scheduled || app.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("unavailable platform scheduled or prompted: ok=%v perm=%v", scheduled, app.NotificationPermission)
	}

	app.SetEnvironment(core.Environment{ScheduledNotifications: true})
	if !r.CanScheduleNotifications() || !reactiveCan {
		t.Fatalf("after env update CanScheduleNotifications = %v / reactive %v, want true", r.CanScheduleNotifications(), reactiveCan)
	}
}

func TestRemindersScheduleWithOverridesAndErrors(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	if err := r.Schedule("unknown", nil); !errors.Is(err, liveops.ErrReminderNotFound) {
		t.Fatalf("unknown id error = %v, want ErrReminderNotFound", err)
	}
	if err := r.ScheduleWith("energy_full", liveops.ScheduleOptions{Delay: -time.Minute}, nil); !errors.Is(err, liveops.ErrInvalidSchedule) {
		t.Fatalf("negative delay error = %v, want ErrInvalidSchedule", err)
	}
	if err := r.ScheduleWith("energy_full", liveops.ScheduleOptions{Delay: time.Hour, At: start.Add(time.Hour)}, nil); !errors.Is(err, liveops.ErrInvalidSchedule) {
		t.Fatalf("both delay and at error = %v, want ErrInvalidSchedule", err)
	}
	if err := r.ScheduleWith("energy_full", liveops.ScheduleOptions{At: start.Add(-time.Minute)}, nil); !errors.Is(err, liveops.ErrInvalidSchedule) {
		t.Fatalf("past At error = %v, want ErrInvalidSchedule", err)
	}

	var ok bool
	if err := r.ScheduleWith("energy_full", liveops.ScheduleOptions{
		Delay: 45 * time.Minute,
		Title: "5/5 Energy Ready",
		Body:  "Jump back into Cave Run!",
	}, func(s bool) { ok = s }); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if !ok {
		t.Fatal("ScheduleWith answered false")
	}
	n, found := findPending(app.PendingNotifications, liveops.DefaultReminderPrefix+"energy_full")
	if !found {
		t.Fatal("missing energy_full")
	}
	if n.Title != "5/5 Energy Ready" || n.Body != "Jump back into Cave Run!" || !n.At.Equal(start.Add(45*time.Minute)) {
		t.Errorf("got %+v", n)
	}
	fireAt, hasFire := r.NextFireTime("energy_full")
	if !hasFire || !fireAt.Equal(start.Add(45*time.Minute)) {
		t.Errorf("NextFireTime = %v (%v), want %v", fireAt, hasFire, start.Add(45*time.Minute))
	}
}

func TestRemindersCancelWhilePermissionPending(t *testing.T) {
	cfg := sampleRemindersConfig(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var r *liveops.Reminders
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		var err error
		r, err = liveops.NewReminders(cx, liveops.RemindersOptions{Config: cfg, Localize: testLocalizer})
		if err != nil {
			t.Fatal(err)
		}
	}, schedEnv, nsgotest.WallTime(start))

	scheduled := true
	if err := r.Schedule("energy_full", func(ok bool) { scheduled = ok }); err != nil {
		t.Fatal(err)
	}
	// Cancel before the permission prompt finishes settling.
	r.Cancel("energy_full")
	app.Settle()

	if scheduled {
		t.Error("cancelled in-flight Schedule answered true")
	}
	if len(app.PendingNotifications) != 0 {
		t.Errorf("cancelled in-flight Schedule left pending: %+v", app.PendingNotifications)
	}
}
