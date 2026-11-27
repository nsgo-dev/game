package a11y_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var desktopEnv = core.Environment{
	OS:    core.OSMacOS,
	Idiom: core.IdiomDesktop,
}

func dumpStorage(st core.Storage) map[string]string {
	out := make(map[string]string)
	for _, k := range st.Keys() {
		if v, ok := st.GetItem(k); ok {
			out[k] = v
		}
	}
	return out
}

func srgbDistance(a, b core.Color) float64 {
	ar, ag, ab, _ := a.Components()
	br, bg, bb, _ := b.Components()
	dr := (ar - br) * 255
	dg := (ag - bg) * 255
	db := (ab - bb) * 255
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// --- #214 Captions ---

func TestCaptionsAppearExpireOnGameLoopAndRenderWithLiveRegion(t *testing.T) {
	var track *a11y.CaptionTrack
	loop := &game.Loop{Rate: 60}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		track = a11y.NewCaptionTrack(cx)
		loop.Update = func(dt time.Duration) {
			track.Update(dt)
		}
		loop.Run(cx)
		return core.View{
			Children: []core.Node{
				a11y.CaptionsView(cx, track, a11y.CaptionStyle{}),
			},
		}
	})

	container := app.Find("id:a11y-captions")
	if container.Props.Role != core.RoleRegion || container.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("captions container props = %+v, want RoleRegion and LiveRegionPolite", container.Props)
	}

	var idDlg, idSFX, idImp uint64
	app.Post(func() {
		idDlg = track.ShowDialogue("Ada", "Watch the bridge!", 500*time.Millisecond)
		idSFX = track.ShowSFXAt("Door creaks", -0.8, 1*time.Second)
		idImp = track.ShowImportant("Boss incoming", 1500*time.Millisecond)
	})
	app.Settle()

	if got := track.Active(); len(got) != 3 {
		t.Fatalf("active captions = %d, want 3", len(got))
	}

	dlgView := app.Find(fmt.Sprintf("id:a11y-captions-item-%d", idDlg))
	if dlgView.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("dialogue caption LiveRegion = %v, want LiveRegionPolite", dlgView.Props.LiveRegion)
	}
	app.Find("text:Ada: Watch the bridge!")

	sfxView := app.Find(fmt.Sprintf("id:a11y-captions-item-%d", idSFX))
	if sfxView.Props.LiveRegion != core.LiveRegionPolite {
		t.Fatalf("sfx caption LiveRegion = %v, want LiveRegionPolite", sfxView.Props.LiveRegion)
	}
	app.Find("text:[<] [Door creaks]")

	impView := app.Find(fmt.Sprintf("id:a11y-captions-item-%d", idImp))
	if impView.Props.LiveRegion != core.LiveRegionAssertive || impView.Props.Role != core.RoleAlert {
		t.Fatalf("important caption props = %+v, want RoleAlert and LiveRegionAssertive", impView.Props)
	}
	app.Find("text:! Boss incoming")

	// Advance 600ms on the 60 Hz game.Loop: 500ms dialogue expires, others remain.
	app.Advance(600 * time.Millisecond)
	if got := track.Active(); len(got) != 2 {
		t.Fatalf("after 600ms: active captions = %d, want 2 (%+v)", len(got), got)
	}
	if app.Exists("text:Watch the bridge!") {
		t.Fatal("expired dialogue caption still rendered")
	}
	app.Find("text:[<] [Door creaks]")
	app.Find("text:! Boss incoming")

	// Advance another 500ms (total 1.1s): 1s SFX expires, 1.5s important remains.
	app.Advance(500 * time.Millisecond)
	if got := track.Active(); len(got) != 1 || got[0].ID != idImp {
		t.Fatalf("after 1.1s: active captions = %+v, want only important caption", got)
	}
	if app.Exists("text:Door creaks") {
		t.Fatal("expired SFX caption still rendered")
	}

	// Advance another 500ms (total 1.6s): all captions expire.
	app.Advance(500 * time.Millisecond)
	if got := track.Active(); len(got) != 0 {
		t.Fatalf("after 1.6s: active captions = %+v, want none", got)
	}
	if app.Exists("text:Boss incoming") {
		t.Fatal("expired important caption still rendered")
	}
}

func TestCaptionTrackMaxVisibleAndDirectionFormatting(t *testing.T) {
	var track *a11y.CaptionTrack
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		track = a11y.NewCaptionTrack(cx)
		return a11y.CaptionsView(cx, track, a11y.CaptionStyle{})
	})

	// Default MaxVisible is 4.
	app.Post(func() {
		for i := 1; i <= 6; i++ {
			track.ShowDialogue("NPC", fmt.Sprintf("Line %d", i), 2*time.Second)
		}
	})
	app.Settle()

	active := track.Active()
	if len(active) != 4 {
		t.Fatalf("len(Active) = %d, want 4", len(active))
	}
	if active[0].Text != "Line 3" || active[3].Text != "Line 6" {
		t.Fatalf("kept captions = %+v, want Line 3..Line 6", active)
	}

	// Stereo right cue formats with [>] and Clear removes all.
	app.Post(func() {
		track.Clear()
		track.Show(a11y.Caption{
			Text:      "Footsteps",
			Kind:      a11y.CaptionSFX,
			Direction: 0.9,
			Duration:  time.Second,
		})
	})
	app.Settle()
	app.Find("text:[Footsteps] [>]")

	app.Post(func() { track.Clear() })
	app.Settle()
	if len(track.Active()) != 0 || app.Exists("text:Footsteps") {
		t.Fatal("Clear did not remove captions")
	}
}

func TestCaptionsViewRespectsAssistFiltersAndScale(t *testing.T) {
	var track *a11y.CaptionTrack
	var assist *a11y.Assist
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		assist = a11y.NewAssist(cx, "test.captions")
		track = a11y.NewCaptionTrack(cx)
		return a11y.CaptionsView(cx, track, a11y.CaptionStyle{
			FontSize: 16,
			Assist:   assist,
		})
	})

	app.Post(func() {
		_ = assist.SetCaptionsEnabled(true)
		_ = assist.SetSFXCaptionsEnabled(false)
		_ = assist.SetTextScale(1.5)
		track.ShowDialogue("Hero", "Hello!", 2*time.Second)
		track.ShowSFX("Wind", 2*time.Second)
	})
	app.Settle()

	dlgText := app.Find("text:Hero: Hello!")
	if dlgText.TextStyle.Font.Size != 24 {
		t.Fatalf("scaled caption Font.Size = %v, want 24", dlgText.TextStyle.Font.Size)
	}
	if app.Exists("text:Wind") {
		t.Fatal("SFX caption shown while SFXCaptionsEnabled is false")
	}

	app.Post(func() { _ = assist.SetSFXCaptionsEnabled(true) })
	app.Settle()
	app.Find("text:[Wind]")
}

// --- #215 Color-blind Palettes ---

func TestPalettesMeetWCAGContrastAndStayDistinguishableUnderSimulation(t *testing.T) {
	modes := []a11y.ColorVision{
		a11y.VisionNormal,
		a11y.VisionProtanopia,
		a11y.VisionDeuteranopia,
		a11y.VisionTritanopia,
		a11y.VisionHighContrast,
	}

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			p := a11y.PaletteFor(mode)

			// Foreground vs Background must exceed WCAG AA (4.5) and AAA (7.0).
			fgRatio := a11y.ContrastRatio(p.Foreground, p.Background)
			if fgRatio < 7.0 {
				t.Errorf("%s: Foreground/Background contrast = %.2f, want >= 7.0", mode, fgRatio)
			}

			// Every semantic game role must meet WCAG AA (>= 4.5) against Background.
			roles := []struct {
				name  string
				color core.Color
			}{
				{"Player", p.Player},
				{"Ally", p.Ally},
				{"Enemy", p.Enemy},
				{"Hazard", p.Hazard},
				{"Pickup", p.Pickup},
				{"Neutral", p.Neutral},
				{"Highlight", p.Highlight},
				{"Accent", p.Accent},
			}
			for _, r := range roles {
				if cr := a11y.ContrastRatio(r.color, p.Background); cr < 4.5 {
					t.Errorf("%s: %s contrast vs Background = %.2f, want >= 4.5", mode, r.name, cr)
				}
			}

			// Distinct gameplay roles must remain clearly distinguishable under Simulate(c, mode).
			entities := []struct {
				name  string
				color core.Color
			}{
				{"Player", p.Player},
				{"Ally", p.Ally},
				{"Enemy", p.Enemy},
				{"Hazard", p.Hazard},
				{"Pickup", p.Pickup},
				{"Neutral", p.Neutral},
				{"Background", p.Background},
				{"Foreground", p.Foreground},
			}
			const minSimulatedDist = 42.0
			for i := 0; i < len(entities); i++ {
				for j := i + 1; j < len(entities); j++ {
					si := a11y.Simulate(entities[i].color, mode)
					sj := a11y.Simulate(entities[j].color, mode)
					dist := srgbDistance(si, sj)
					if dist < minSimulatedDist {
						t.Errorf("%s: simulated %s (%08x) and %s (%08x) distance = %.1f, want >= %.1f",
							mode, entities[i].name, uint32(si), entities[j].name, uint32(sj), dist, minSimulatedDist)
					}
				}
			}
		})
	}
}

func TestSimulateContrastRatioAndEnsureContrast(t *testing.T) {
	black := core.RGB(0x000000)
	white := core.RGB(0xFFFFFF)

	if r := a11y.ContrastRatio(white, black); math.Abs(r-21.0) > 1e-6 {
		t.Fatalf("ContrastRatio(white, black) = %.6f, want 21.0", r)
	}
	if r := a11y.ContrastRatio(black, black); math.Abs(r-1.0) > 1e-6 {
		t.Fatalf("ContrastRatio(black, black) = %.6f, want 1.0", r)
	}

	// Neutral grays stay invariant under all CVD simulation modes; alpha preserved.
	gray := core.RGBA(0x808080, 0.5)
	for _, mode := range []a11y.ColorVision{a11y.VisionNormal, a11y.VisionProtanopia, a11y.VisionDeuteranopia, a11y.VisionTritanopia, a11y.VisionHighContrast} {
		sim := a11y.Simulate(gray, mode)
		if sim&0xff != gray&0xff {
			t.Errorf("%s: alpha changed from %02x to %02x", mode, gray&0xff, sim&0xff)
		}
		if d := srgbDistance(sim, gray); d > 2.0 {
			t.Errorf("%s: neutral gray drifted by %.2f (%08x -> %08x)", mode, d, uint32(gray), uint32(sim))
		}
	}

	// Pure red and pure green collapse dramatically closer under protanopia/deuteranopia.
	red := core.RGB(0xCC2222)
	green := core.RGB(0x22AA22)
	origDist := srgbDistance(red, green)
	for _, mode := range []a11y.ColorVision{a11y.VisionProtanopia, a11y.VisionDeuteranopia} {
		simDist := srgbDistance(a11y.Simulate(red, mode), a11y.Simulate(green, mode))
		if simDist >= origDist*0.6 {
			t.Errorf("%s: expected red/green confusion, origDist=%.1f simDist=%.1f", mode, origDist, simDist)
		}
	}

	// EnsureContrast lifts low-contrast colors to meet the requested ratio across backgrounds.
	cases := []struct {
		fg, bg core.Color
		min    float64
	}{
		{core.RGB(0x333344), core.RGB(0x111318), 4.5},
		{core.RGB(0xBBBBCC), core.RGB(0xF5F5F5), 4.5},
		{core.RGB(0x777777), core.RGB(0x808080), 4.5},
		{core.RGB(0x446688), core.RGB(0x000000), 7.0},
	}
	for _, tc := range cases {
		adj := a11y.EnsureContrast(tc.fg, tc.bg, tc.min)
		if got := a11y.ContrastRatio(adj, tc.bg); got < tc.min {
			t.Errorf("EnsureContrast(%08x, %08x, %.1f) -> %08x contrast = %.2f", uint32(tc.fg), uint32(tc.bg), tc.min, uint32(adj), got)
		}
	}

	// Already-sufficient contrast is returned unchanged.
	if adj := a11y.EnsureContrast(white, black, 7.0); adj != white {
		t.Errorf("EnsureContrast altered already-compliant white: %08x", uint32(adj))
	}
}

// --- #216 Assist Options ---

func TestAssistPersistsAcrossRestartRespectsReduceMotionAndScalesDT(t *testing.T) {
	// 1. Respects cx.Env().ReduceMotion() when unset in storage.
	var a1 *a11y.Assist
	app1 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a1 = a11y.NewAssist(cx, "game.a11y")
		return core.View{}
	}, nsgotest.Environment(core.Environment{ReduceMotion: true}))

	if !a1.ReduceMotion() {
		t.Fatal("ReduceMotion = false, want true from cx.Env().ReduceMotion()")
	}
	if a1.GameSpeed() != 1.0 || a1.TextScale() != 1.0 || a1.ColorVision() != a11y.VisionNormal {
		t.Fatalf("unexpected defaults: speed=%v scale=%v vision=%v", a1.GameSpeed(), a1.TextScale(), a1.ColorVision())
	}

	// Live environment update propagates while unset.
	app1.SetEnvironment(core.Environment{ReduceMotion: false})
	if a1.ReduceMotion() {
		t.Fatal("ReduceMotion did not follow updated environment while unset")
	}

	// Clamping + ScaleDT accuracy (unified with save.AccessibilitySettings ranges).
	app1.Post(func() {
		_ = a1.SetGameSpeed(0.1) // clamps to 0.25
		_ = a1.SetTextScale(3.5) // clamps to 3.0
	})
	app1.Settle()
	if a1.GameSpeed() != 0.25 {
		t.Fatalf("clamped GameSpeed = %v, want 0.25", a1.GameSpeed())
	}
	if a1.TextScale() != a11y.MaxTextScale {
		t.Fatalf("clamped TextScale = %v, want %v", a1.TextScale(), a11y.MaxTextScale)
	}
	if got := a1.ScaleDT(16 * time.Millisecond); got != 4*time.Millisecond {
		t.Fatalf("ScaleDT(16ms) at 0.25 = %v, want 4ms", got)
	}

	app1.Post(func() {
		_ = a1.SetGameSpeed(5.0) // clamps to 2.0
		_ = a1.SetTextScale(0.1) // clamps to 0.5
	})
	app1.Settle()
	if a1.GameSpeed() != a11y.MaxGameSpeed {
		t.Fatalf("upper-clamped GameSpeed = %v, want %v", a1.GameSpeed(), a11y.MaxGameSpeed)
	}
	if a1.TextScale() != a11y.MinTextScale {
		t.Fatalf("lower-clamped TextScale = %v, want %v", a1.TextScale(), a11y.MinTextScale)
	}

	// Set every option to a distinct non-default value and verify ScaleDT on a real game.Loop.
	want := a11y.AssistOptions{
		GameSpeed:          0.5,
		ReduceMotion:       true,
		ReduceFlashes:      true,
		Invincibility:      true,
		AutoAim:            true,
		HoldToToggle:       true,
		HighContrast:       true,
		CaptionsEnabled:    true,
		SFXCaptionsEnabled: true,
		ColorVision:        a11y.VisionDeuteranopia,
		TextScale:          1.5,
	}
	var simElapsed time.Duration
	app1.Post(func() {
		if err := a1.Apply(want); err != nil {
			t.Errorf("Apply failed: %v", err)
		}
	})
	app1.Settle()

	if got := a1.Snapshot(); got != want {
		t.Fatalf("Snapshot() = %+v, want %+v", got, want)
	}

	// 2. Persist storage snapshot across an app restart with nsgotest.Storage.
	saved := dumpStorage(app1.Storage())
	if len(saved) == 0 {
		t.Fatal("expected persisted keys in storage")
	}

	var a2 *a11y.Assist
	loop := &game.Loop{Rate: 60}
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a2 = a11y.NewAssist(cx, "game.a11y")
		loop.Update = func(dt time.Duration) {
			simElapsed += a2.ScaleDT(dt)
		}
		loop.Run(cx)
		return core.View{}
	}, nsgotest.Storage(saved), nsgotest.Environment(core.Environment{ReduceMotion: false}))

	if got := a2.Snapshot(); got != want {
		t.Fatalf("after restart Snapshot() = %+v, want %+v", got, want)
	}

	// Advance 1 second of real time at 0.5x speed -> ~500ms of scaled simulation time.
	app2.Advance(time.Second)
	if math.Abs(float64(simElapsed-500*time.Millisecond)) > float64(2*time.Millisecond) {
		t.Fatalf("scaled simElapsed after 1s at 0.5x = %v, want ~500ms", simElapsed)
	}

	// Explicit ReduceMotion=false overrides OS ReduceMotion=true.
	app2.SetEnvironment(core.Environment{ReduceMotion: true})
	app2.Post(func() { _ = a2.SetReduceMotion(false) })
	app2.Settle()
	if a2.ReduceMotion() {
		t.Fatal("explicit SetReduceMotion(false) did not override env ReduceMotion=true")
	}

	// Reset restores OS environment fallback.
	app2.Post(func() { a2.Reset() })
	app2.Settle()
	if !a2.ReduceMotion() || a2.GameSpeed() != 1.0 || a2.Invincibility() {
		t.Fatalf("after Reset: ReduceMotion=%v GameSpeed=%v Invincibility=%v", a2.ReduceMotion(), a2.GameSpeed(), a2.Invincibility())
	}
}

// --- #217 Accessible Menu ---

func TestAccessibleMenuRolesKeyboardNavigationActivationAndAnnouncements(t *testing.T) {
	var resumed, closed int
	var invincibility *core.Signal[bool]
	var speedText *core.Signal[string]

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		invincibility = core.NewSignal(cx, false)
		speedText = core.NewSignal(cx, "100%")
		return a11y.AccessibleMenu(cx, a11y.MenuProps{
			TestID:      "pause-menu",
			Label:       "Pause Menu",
			Hint:        "Use arrow keys to navigate and Enter to activate",
			Description: "Game paused",
			OnClose:     func() { closed++ },
			Items: []a11y.MenuItem{
				{
					ID:          "resume",
					Label:       "Resume",
					Hint:        "Returns to gameplay",
					Description: "Continue from where you paused",
					OnSelect:    func() { resumed++ },
				},
				{
					ID:       "locked",
					Label:    "Multiplayer",
					Disabled: func() bool { return true },
					OnSelect: func() { t.Error("disabled item should not activate") },
				},
				{
					ID:       "invincibility",
					Label:    "Invincibility",
					Hint:     "Prevents taking damage",
					Checked:  invincibility.Get,
					OnSelect: func() { invincibility.Set(!invincibility.Get()) },
				},
				{
					ID:    "speed",
					Label: "Game Speed",
					Value: speedText.Get,
					OnSelect: func() {
						if speedText.Get() == "100%" {
							speedText.Set("75%")
						} else {
							speedText.Set("100%")
						}
					},
				},
			},
		})
	}, nsgotest.Environment(desktopEnv))

	// Menu container exposes RoleMenu, Label, Hint, Description, LiveRegion, and Focusable.
	menu := app.Find("id:pause-menu")
	if menu.Props.Role != core.RoleMenu ||
		menu.Props.Label != "Pause Menu" ||
		menu.Props.Hint != "Use arrow keys to navigate and Enter to activate" ||
		menu.Props.Description != "Game paused" ||
		menu.Props.LiveRegion != core.LiveRegionPolite ||
		!menu.Props.Focusable {
		t.Fatalf("menu props = %+v", menu.Props)
	}

	// Menu items expose proper roles, labels, hints, descriptions, and values.
	resumeItem := app.Find("id:pause-menu-resume")
	if resumeItem.Props.Role != core.RoleMenuItem ||
		resumeItem.Props.Label != "Resume" ||
		resumeItem.Props.Hint != "Returns to gameplay" ||
		resumeItem.Props.Description != "Continue from where you paused" ||
		!resumeItem.Props.Focusable {
		t.Fatalf("resume item props = %+v", resumeItem.Props)
	}

	invItem := app.Find("id:pause-menu-invincibility")
	if invItem.Props.Role != core.RoleMenuItemCheckbox || invItem.Props.Checked {
		t.Fatalf("invincibility item props = %+v, want RoleMenuItemCheckbox unchecked", invItem.Props)
	}

	speedItem := app.Find("id:pause-menu-speed")
	if speedItem.Props.Value.Text != "100%" {
		t.Fatalf("speed item AccessibilityValue = %+v, want Text=100%%", speedItem.Props.Value)
	}

	// Opening the menu announced itself.
	if len(app.Announced) == 0 || !strings.Contains(app.Announced[0], "Pause Menu") {
		t.Fatalf("initial announcements = %v, want Pause Menu open announcement", app.Announced)
	}

	// Focus menu, activate item 0 ("Resume") via Return.
	app.Focus("id:pause-menu")
	app.Key("return")
	if resumed != 1 {
		t.Fatalf("resumed = %d, want 1", resumed)
	}

	// Down skips disabled "Multiplayer" and lands on "Invincibility", announcing it.
	app.Key("down")
	lastAnn := app.Announced[len(app.Announced)-1]
	if !strings.Contains(lastAnn, "Invincibility") {
		t.Fatalf("after Down announcement = %q, want Invincibility (skipping disabled Multiplayer)", lastAnn)
	}

	// Space toggles "Invincibility" on, updates Checked, and announces checked state.
	app.Key("space")
	if !invincibility.Get() {
		t.Fatal("invincibility did not toggle on via Space")
	}
	if !app.Find("id:pause-menu-invincibility").Props.Checked {
		t.Fatal("invincibility view Props.Checked = false after toggle")
	}
	lastAnn = app.Announced[len(app.Announced)-1]
	if !strings.Contains(lastAnn, "Invincibility") || !strings.Contains(lastAnn, "checked") {
		t.Fatalf("toggle announcement = %q, want Invincibility + checked", lastAnn)
	}

	// Down moves to "Game Speed"; Enter updates reactive Value and announces "75%".
	app.Key("down")
	app.Key("enter")
	if got := app.Find("id:pause-menu-speed").Props.Value.Text; got != "75%" {
		t.Fatalf("after Enter speed AccessibilityValue = %q, want 75%%", got)
	}
	lastAnn = app.Announced[len(app.Announced)-1]
	if !strings.Contains(lastAnn, "75%") {
		t.Fatalf("speed announcement = %q, want 75%%", lastAnn)
	}

	// Up moves back to "Invincibility".
	app.Key("up")
	lastAnn = app.Announced[len(app.Announced)-1]
	if !strings.Contains(lastAnn, "Invincibility") {
		t.Fatalf("after Up announcement = %q, want Invincibility", lastAnn)
	}

	// Escape invokes OnClose and announces closure.
	app.Key("escape")
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	lastAnn = app.Announced[len(app.Announced)-1]
	if !strings.Contains(lastAnn, "closed") {
		t.Fatalf("escape announcement = %q, want closed", lastAnn)
	}
}

func TestAssistMigratesLegacyStorageKeysOnce(t *testing.T) {
	// ParseColorVision accepts both "normal" and "none".
	for _, alias := range []string{"normal", "none", "NONE"} {
		mode, ok := a11y.ParseColorVision(alias)
		if !ok || mode != a11y.VisionNormal {
			t.Fatalf("ParseColorVision(%q) = (%v, %v), want (VisionNormal, true)", alias, mode, ok)
		}
	}

	legacy := map[string]string{
		"a11y.assist.gameSpeed":          "0.75",
		"a11y.assist.reduceMotion":       "false",
		"a11y.assist.invincibility":      "true",
		"a11y.assist.autoAim":            "true",
		"a11y.assist.holdToToggle":       "true",
		"a11y.assist.highContrast":       "true",
		"a11y.assist.captionsEnabled":    "true",
		"a11y.assist.sfxCaptionsEnabled": "true",
		"a11y.assist.colorVision":        "tritanopia",
		"a11y.assist.textScale":          "1.75",
	}

	var store *save.DefaultSettingsStore
	var assist *a11y.Assist
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		// Pre-populate unrelated settings in the shared store to verify migration preserves them.
		store = save.NewStorageSettingsStore(cx.Storage(), "mygame")
		initial := save.DefaultSettings()
		initial.Locale = "ja"
		initial.Audio.Master = 0.6
		if err := store.Save(initial); err != nil {
			t.Fatalf("seed store: %v", err)
		}
		assist = a11y.NewAssistFromSettings(cx, store)
		return core.View{}
	}, nsgotest.Storage(legacy), nsgotest.Environment(core.Environment{
		ReduceMotion: true, // explicit legacy reduceMotion="false" must override this!
	}))

	// Every legacy key migrated into the shared store.
	if assist.GameSpeed() != 0.75 ||
		assist.ReduceMotion() != false ||
		!assist.Invincibility() ||
		!assist.AutoAim() ||
		!assist.HoldToToggle() ||
		!assist.HighContrast() ||
		!assist.CaptionsEnabled() ||
		!assist.SFXCaptionsEnabled() ||
		assist.ColorVision() != a11y.VisionTritanopia ||
		assist.TextScale() != 1.75 {
		t.Fatalf("migrated assist snapshot = %+v", assist.Snapshot())
	}

	// Unrelated settings in the shared store were preserved.
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if reloaded.Locale != "ja" || math.Abs(reloaded.Audio.Master-0.6) > 1e-6 {
		t.Fatalf("unrelated settings overwritten by migration: locale=%q master=%v", reloaded.Locale, reloaded.Audio.Master)
	}
	if !reloaded.Accessibility.ReduceMotionSet || reloaded.Accessibility.ReduceMotion {
		t.Fatalf("legacy reduceMotion=false override not preserved: %+v", reloaded.Accessibility)
	}

	// Legacy keys were deleted so migration runs only once.
	for k := range legacy {
		if _, exists := app.Storage().GetItem(k); exists {
			t.Fatalf("legacy key %q was not removed after migration", k)
		}
	}

	// Subsequent user edits are never overwritten on reload.
	app.Post(func() {
		if err := assist.SetGameSpeed(1.5); err != nil {
			t.Errorf("SetGameSpeed: %v", err)
		}
	})
	app.Settle()
	if assist.GameSpeed() != 1.5 {
		t.Fatalf("GameSpeed after edit = %v, want 1.5", assist.GameSpeed())
	}
}

func TestAssistFromSettingsReactsToStoreAndPreservesOtherSectionsOnReset(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	var assist *a11y.Assist
	var track *a11y.CaptionTrack
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		assist = a11y.NewAssistFromSettings(cx, store)
		track = a11y.NewCaptionTrack(cx)
		return a11y.CaptionsView(cx, track, a11y.CaptionStyle{
			FontSize: 16,
			Assist:   assist,
		})
	}, nsgotest.Environment(core.Environment{
		ReduceMotion:     true,
		IncreaseContrast: true,
		FontScale:        1.5,
	}))

	// Unset settings follow platform environment.
	if !assist.ReduceMotion() || !assist.HighContrast() || assist.TextScale() != 1.5 {
		t.Fatalf("platform fallback = (rm:%v hc:%v scale:%v), want (true, true, 1.5)",
			assist.ReduceMotion(), assist.HighContrast(), assist.TextScale())
	}

	// Saving through the shared file store notifies Assist and updates CaptionsView reactively.
	app.Post(func() {
		track.ShowDialogue("Guide", "Bridge ahead", 5*time.Second)
	})
	app.Settle()
	if app.Exists("text:Guide: Bridge ahead") {
		t.Fatal("caption visible before Captions enabled")
	}

	app.Post(func() {
		s := save.DefaultSettings()
		s.Locale = "zh-Hans"
		s.Audio.Master = 0.4
		s.Accessibility.Captions = true
		s.Accessibility.SetCaptionScale(2.0)
		s.Accessibility.ReduceFlashes = true
		s.Accessibility.ScreenShake = 0.25
		if err := store.Save(s); err != nil {
			t.Errorf("store.Save: %v", err)
		}
	})
	app.Settle()

	capText := app.Find("text:Guide: Bridge ahead")
	if assist.TextScale() != 2.0 {
		t.Fatalf("assist.TextScale() = %v, want 2.0", assist.TextScale())
	}
	// CaptionsView sets Style.FontSize = 16 * 2.0 = 32, and NSGo applies env.FontScale (1.5) -> 48.
	if capText.TextStyle.Font.Size != 48 {
		t.Fatalf("caption Font.Size = %v, want 48 (16 * 2.0 * env.FontScale 1.5)", capText.TextStyle.Font.Size)
	}
	if !assist.ReduceFlashes() || assist.ScreenShake() != 0.25 {
		t.Fatalf("ReduceFlashes=%v ScreenShake=%v, want true, 0.25", assist.ReduceFlashes(), assist.ScreenShake())
	}

	// Assist.Reset restores Accessibility defaults (and OS fallback) without erasing Locale/Audio.
	app.Post(func() { assist.Reset() })
	app.Settle()
	if assist.CaptionsEnabled() || assist.ReduceFlashes() || assist.TextScale() != 1.5 {
		t.Fatalf("after Reset: captions=%v flashes=%v scale=%v", assist.CaptionsEnabled(), assist.ReduceFlashes(), assist.TextScale())
	}
	afterReset, err := store.Load()
	if err != nil {
		t.Fatalf("store.Load after Reset: %v", err)
	}
	if afterReset.Locale != "zh-Hans" || math.Abs(afterReset.Audio.Master-0.4) > 1e-6 {
		t.Fatalf("Assist.Reset clobbered non-a11y settings: locale=%q master=%v", afterReset.Locale, afterReset.Audio.Master)
	}
}
