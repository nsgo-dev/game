package save_test

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func near64(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
func near32(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestDefaultSettingsAndValidate(t *testing.T) {
	d := save.DefaultSettings()
	if d.Audio.Master != 1.0 || d.Audio.Muted {
		t.Fatalf("default audio = %+v, want master=1.0 muted=false", d.Audio)
	}
	for _, g := range []string{"music", "sfx", "ui"} {
		if d.Audio.Groups[g] != 1.0 {
			t.Fatalf("default audio group %q = %v, want 1.0", g, d.Audio.Groups[g])
		}
	}
	if !d.Display.VSync || !d.Display.AutoDropLowPower || d.Display.ScaleMode != "fit" || d.Display.FrameCap != 0 || d.Display.Quality != "auto" {
		t.Fatalf("default display = %+v", d.Display)
	}
	if d.Accessibility.CaptionScale != 1.0 || d.Accessibility.ScreenShake != 1.0 || d.Accessibility.GameSpeed != 1.0 || d.Accessibility.ColorBlindMode != "none" {
		t.Fatalf("default accessibility = %+v", d.Accessibility)
	}

	// Out-of-range, NaN, Inf, and whitespace values are cleanly clamped/normalized.
	s := save.Settings{
		Audio: save.AudioSettings{
			Master: 2.5,
			Groups: map[string]float64{
				"music": -0.4,
				"sfx":   math.NaN(),
				"ui":    math.Inf(1),
				"voice": math.Inf(-1),
			},
		},
		Display: save.DisplaySettings{
			DisplayIndex: -3,
			FrameCap:     -120,
			ScaleMode:    "   ",
			Quality:      " Ultra ",
		},
		Locale: "  zh-Hans  ",
		Accessibility: save.AccessibilitySettings{
			CaptionScale:   10.0,
			ColorBlindMode: "  ",
			ScreenShake:    -0.5,
			GameSpeed:      0.05,
		},
	}
	s.Validate()

	if s.Audio.Master != 1.0 {
		t.Fatalf("Master = %v, want 1.0", s.Audio.Master)
	}
	if s.Audio.Groups["music"] != 0.0 || s.Audio.Groups["sfx"] != 1.0 || s.Audio.Groups["ui"] != 1.0 || s.Audio.Groups["voice"] != 0.0 {
		t.Fatalf("Groups = %+v", s.Audio.Groups)
	}
	if s.Display.DisplayIndex != 0 || s.Display.FrameCap != 0 || s.Display.ScaleMode != "fit" || s.Display.Quality != "auto" {
		t.Fatalf("Display = %+v", s.Display)
	}
	s.Display.Quality = " HIGH "
	s.Validate()
	if s.Display.Quality != "high" {
		t.Fatalf("Quality = %q, want high", s.Display.Quality)
	}
	if s.Locale != "zh-Hans" {
		t.Fatalf("Locale = %q, want zh-Hans", s.Locale)
	}
	if s.Accessibility.CaptionScale != 3.0 || s.Accessibility.ColorBlindMode != "none" || s.Accessibility.ScreenShake != 0.0 || s.Accessibility.GameSpeed != 0.25 {
		t.Fatalf("Accessibility = %+v", s.Accessibility)
	}

	// Zero values for CaptionScale and GameSpeed default to 1.0; explicit 0 for Master and ScreenShake stay 0.
	z := save.Settings{
		Audio: save.AudioSettings{Master: 0},
		Accessibility: save.AccessibilitySettings{
			CaptionScale: 0,
			ScreenShake:  0,
			GameSpeed:    0,
		},
	}
	z.Validate()
	if z.Audio.Master != 0 || z.Accessibility.ScreenShake != 0 {
		t.Fatalf("explicit zeros were overwritten: master=%v shake=%v", z.Audio.Master, z.Accessibility.ScreenShake)
	}
	if z.Accessibility.CaptionScale != 1.0 || z.Accessibility.GameSpeed != 1.0 {
		t.Fatalf("zero scales = (%v, %v), want (1.0, 1.0)", z.Accessibility.CaptionScale, z.Accessibility.GameSpeed)
	}

	// Negative CaptionScale and oversized GameSpeed/ScreenShake clamp to bounds.
	hi := save.Settings{
		Audio: save.AudioSettings{Master: math.NaN()},
		Accessibility: save.AccessibilitySettings{
			CaptionScale: -2.0,
			ScreenShake:  5.0,
			GameSpeed:    9.0,
		},
	}
	hi.Validate()
	if hi.Audio.Master != 1.0 || hi.Accessibility.CaptionScale != 0.5 || hi.Accessibility.ScreenShake != 1.0 || hi.Accessibility.GameSpeed != 2.0 {
		t.Fatalf("clamped bounds = %+v", hi)
	}
}

func TestStartupLoadAndApplyAllTargets(t *testing.T) {
	run := func(t *testing.T, makeStore func(cx *core.Scope) *save.DefaultSettingsStore) {
		var mixer *sound.Mixer
		var store *save.DefaultSettingsStore

		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			mixer = sound.NewMixer(cx, "test-mixer")
			store = makeStore(cx)
			return core.View{}
		}, nsgotest.Environment(core.Environment{Audio: true}))

		// First run with absent settings returns and applies defaults cleanly.
		var defaultLocale string
		var defaultDisplay save.DisplaySettings
		gotDefault, err := store.LoadAndApply(save.SettingsTargets{
			Sound: mixer,
			ApplyDisplay: func(d save.DisplaySettings) error {
				defaultDisplay = d
				return nil
			},
			ApplyLocale: func(l string) error {
				defaultLocale = l
				return nil
			},
		})
		if err != nil {
			t.Fatalf("LoadAndApply absent: %v", err)
		}
		if gotDefault.Audio.Master != 1.0 || !defaultDisplay.VSync || defaultLocale != "" {
			t.Fatalf("absent defaults = %+v, display=%+v, locale=%q", gotDefault, defaultDisplay, defaultLocale)
		}
		if !near32(mixer.Master(), 1.0) || !near32(mixer.Volume("music"), 1.0) {
			t.Fatalf("mixer defaults = master:%v music:%v", mixer.Master(), mixer.Volume("music"))
		}

		// Save customized player settings (including Custom game data).
		want := save.DefaultSettings()
		want.Audio = save.AudioSettings{
			Master: 0.8,
			Groups: map[string]float64{
				"music": 0.45,
				"sfx":   0.75,
				"ui":    0.3,
			},
			Muted: false,
		}
		want.Bindings = map[string][]string{
			"jump":   {"Space", "GamepadA"},
			"attack": {"KeyJ", "MouseLeft"},
		}
		want.Display = save.DisplaySettings{
			Fullscreen:       true,
			DisplayID:        "display-2",
			DisplayIndex:     1,
			ScaleMode:        "integer",
			IntegerScale:     true,
			FrameCap:         120,
			VSync:            false,
			AutoDropLowPower: false,
			Quality:          "low",
		}
		want.Locale = "ja"
		want.Accessibility = save.AccessibilitySettings{
			Captions:       true,
			CaptionScale:   1.5,
			ColorBlindMode: "deuteranopia",
			ReduceMotion:   true,
			HighContrast:   true,
			ScreenShake:    0.25,
			GameSpeed:      0.75,
		}
		want.Custom = map[string]json.RawMessage{
			"tutorial_seen": json.RawMessage(`true`),
			"cursor_size":   json.RawMessage(`2`),
		}

		if err := store.Save(want); err != nil {
			t.Fatalf("Save: %v", err)
		}

		// Startup LoadAndApply wires Sound mixer AND all decoupled callbacks.
		var gotMaster float64
		gotGroups := map[string]float64{}
		var groupOrder []string
		var gotAudio save.AudioSettings
		var gotBindings map[string][]string
		var gotDisplay save.DisplaySettings
		var gotLocale string
		var gotAccess save.AccessibilitySettings

		loaded, err := store.LoadAndApply(save.SettingsTargets{
			Sound: mixer,
			SetMasterVolume: func(v float64) {
				gotMaster = v
			},
			SetGroupVolume: func(group string, v float64) {
				gotGroups[group] = v
				groupOrder = append(groupOrder, group)
			},
			ApplyAudio: func(a save.AudioSettings) error {
				gotAudio = a
				return nil
			},
			ApplyBindings: func(b map[string][]string) error {
				gotBindings = b
				return nil
			},
			ApplyDisplay: func(d save.DisplaySettings) error {
				gotDisplay = d
				return nil
			},
			ApplyLocale: func(l string) error {
				gotLocale = l
				return nil
			},
			ApplyAccessibility: func(a save.AccessibilitySettings) error {
				gotAccess = a
				return nil
			},
		})
		if err != nil {
			t.Fatalf("LoadAndApply: %v", err)
		}

		if !near32(mixer.Master(), 0.8) || !near32(mixer.Volume("music"), 0.45) || !near32(mixer.Volume("sfx"), 0.75) || !near32(mixer.Volume("ui"), 0.3) {
			t.Fatalf("mixer volumes = master:%v music:%v sfx:%v ui:%v", mixer.Master(), mixer.Volume("music"), mixer.Volume("sfx"), mixer.Volume("ui"))
		}
		if !near64(gotMaster, 0.8) || !maps.Equal(gotGroups, want.Audio.Groups) {
			t.Fatalf("audio callbacks = master:%v groups:%+v", gotMaster, gotGroups)
		}
		if !slices.Equal(groupOrder, []string{"music", "sfx", "ui"}) {
			t.Fatalf("group callback order = %v, want sorted [music sfx ui]", groupOrder)
		}
		if !near64(gotAudio.Master, 0.8) || gotAudio.Muted {
			t.Fatalf("ApplyAudio = %+v", gotAudio)
		}
		if !slices.Equal(gotBindings["jump"], []string{"Space", "GamepadA"}) || !slices.Equal(gotBindings["attack"], []string{"KeyJ", "MouseLeft"}) {
			t.Fatalf("ApplyBindings = %+v", gotBindings)
		}
		if gotDisplay != want.Display {
			t.Fatalf("ApplyDisplay = %+v, want %+v", gotDisplay, want.Display)
		}
		if gotLocale != "ja" {
			t.Fatalf("ApplyLocale = %q, want ja", gotLocale)
		}
		if gotAccess != want.Accessibility {
			t.Fatalf("ApplyAccessibility = %+v, want %+v", gotAccess, want.Accessibility)
		}
		if string(loaded.Custom["tutorial_seen"]) != "true" || string(loaded.Custom["cursor_size"]) != "2" {
			t.Fatalf("Custom = %+v", loaded.Custom)
		}

		// Muted silences mixer and SetMasterVolume while preserving stored Master level.
		want.Audio.Muted = true
		if err := store.Save(want); err != nil {
			t.Fatalf("Save muted: %v", err)
		}
		gotMuted, err := store.LoadAndApply(save.SettingsTargets{
			Sound: mixer,
			SetMasterVolume: func(v float64) {
				gotMaster = v
			},
		})
		if err != nil {
			t.Fatalf("LoadAndApply muted: %v", err)
		}
		if !near32(mixer.Master(), 0) || gotMaster != 0 {
			t.Fatalf("muted master = mixer:%v callback:%v, want 0", mixer.Master(), gotMaster)
		}
		if !near64(gotMuted.Audio.Master, 0.8) || !gotMuted.Audio.Muted {
			t.Fatalf("stored muted audio = %+v, want Master=0.8 Muted=true", gotMuted.Audio)
		}

		// Error in one target still invokes remaining targets and returns joined error + loaded settings.
		displayErr := errors.New("monitor disconnected")
		localeCalled := false
		loadedOnErr, err := store.LoadAndApply(save.SettingsTargets{
			ApplyDisplay: func(save.DisplaySettings) error { return displayErr },
			ApplyLocale: func(l string) error {
				localeCalled = true
				return nil
			},
		})
		if !errors.Is(err, displayErr) {
			t.Fatalf("LoadAndApply target err = %v, want displayErr", err)
		}
		if !localeCalled || loadedOnErr.Locale != "ja" {
			t.Fatalf("remaining target not called or settings dropped: localeCalled=%v locale=%q", localeCalled, loadedOnErr.Locale)
		}
	}

	t.Run("FileBackend", func(t *testing.T) {
		dir := t.TempDir()
		run(t, func(*core.Scope) *save.DefaultSettingsStore {
			return save.NewFileSettingsStore(dir)
		})
	})

	t.Run("StorageBackend", func(t *testing.T) {
		run(t, func(cx *core.Scope) *save.DefaultSettingsStore {
			return save.NewStorageSettingsStore(cx.Storage(), "game")
		})
	})
}

func TestSettingsCompletelyIsolatedFromSaveSlots(t *testing.T) {
	run := func(t *testing.T, makeStores func(t *testing.T) (*save.Store, *save.DefaultSettingsStore, func())) {
		slotStore, settingsStore, verifyLayout := makeStores(t)

		// Save settings and multiple save slots sharing the exact same backend / dir / storage prefix.
		wantSettings := save.DefaultSettings()
		wantSettings.Locale = "zh-Hans"
		wantSettings.Audio.Master = 0.65
		wantSettings.Bindings = map[string][]string{"pause": {"Escape"}}
		if err := settingsStore.Save(wantSettings); err != nil {
			t.Fatalf("Save settings: %v", err)
		}
		// Write settings a second time so .bak also exists alongside slots.
		wantSettings.Audio.Master = 0.55
		if err := settingsStore.Save(wantSettings); err != nil {
			t.Fatalf("Save settings overwrite: %v", err)
		}

		for _, slot := range []string{"slot1", "slot2", "auto"} {
			if err := slotStore.Save(slot, playerState{Name: slot, Level: 5}); err != nil {
				t.Fatalf("Save %q: %v", slot, err)
			}
		}

		// Settings must never appear in Store.Slots() or Store.Exists().
		slots, err := slotStore.Slots()
		if err != nil {
			t.Fatalf("Slots: %v", err)
		}
		if !slices.Equal(slots, []string{"auto", "slot1", "slot2"}) {
			t.Fatalf("Slots = %v, want [auto slot1 slot2] (settings must not appear)", slots)
		}
		if slotStore.Exists("settings") || slotStore.Exists("Settings") {
			t.Fatal("Store.Exists reported settings as a save slot")
		}

		// Trying to overwrite or delete "settings" through slot Store is rejected.
		if err := slotStore.Save("settings", playerState{Name: "Clobber"}); !errors.Is(err, save.ErrInvalidSlot) {
			t.Fatalf("Store.Save(settings) err = %v, want ErrInvalidSlot", err)
		}
		if err := slotStore.Delete("settings"); !errors.Is(err, save.ErrInvalidSlot) {
			t.Fatalf("Store.Delete(settings) err = %v, want ErrInvalidSlot", err)
		}

		// Verify dedicated key/filename layout outside slots.
		verifyLayout()

		// Deleting one slot leaves settings untouched.
		if err := slotStore.Delete("slot1"); err != nil {
			t.Fatalf("Delete slot1: %v", err)
		}
		got, err := settingsStore.Load()
		if err != nil || got.Locale != "zh-Hans" || !near64(got.Audio.Master, 0.55) {
			t.Fatalf("settings after Delete(slot1) = (%+v, %v)", got, err)
		}

		// DeleteAll removes every save slot and NEVER touches settings.
		if err := slotStore.DeleteAll(); err != nil {
			t.Fatalf("DeleteAll: %v", err)
		}
		slots, err = slotStore.Slots()
		if err != nil || len(slots) != 0 {
			t.Fatalf("Slots after DeleteAll = (%v, %v), want empty", slots, err)
		}
		if !settingsStore.Exists() {
			t.Fatal("settings disappeared after Store.DeleteAll")
		}
		got, err = settingsStore.Load()
		if err != nil || got.Locale != "zh-Hans" || !near64(got.Audio.Master, 0.55) || !slices.Equal(got.Bindings["pause"], []string{"Escape"}) {
			t.Fatalf("settings after DeleteAll = (%+v, %v)", got, err)
		}
	}

	t.Run("FileBackend", func(t *testing.T) {
		run(t, func(t *testing.T) (*save.Store, *save.DefaultSettingsStore, func()) {
			dir := t.TempDir()
			b := save.FileBackend(dir)
			return save.NewStore(b), save.NewDefaultSettingsStore(b), func() {
				if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
					t.Fatalf("expected settings.json in dir: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "settings.json.bak")); err != nil {
					t.Fatalf("expected settings.json.bak in dir: %v", err)
				}
			}
		})
	})

	t.Run("StorageBackend", func(t *testing.T) {
		run(t, func(t *testing.T) (*save.Store, *save.DefaultSettingsStore, func()) {
			app, _, _ := startStorageApp(t)
			b := save.StorageBackend(app.Storage(), "game")
			return save.NewStore(b), save.NewDefaultSettingsStore(b), func() {
				st := app.Storage()
				if _, ok := st.GetItem("game.settings"); !ok {
					t.Fatalf("expected key game.settings, got keys %v", st.Keys())
				}
				if _, ok := st.GetItem("game.settings.bak"); !ok {
					t.Fatalf("expected key game.settings.bak, got keys %v", st.Keys())
				}
				for _, k := range st.Keys() {
					if strings.HasPrefix(k, "game.slot.settings") || strings.HasPrefix(k, "game.bak.settings") {
						t.Fatalf("settings leaked into slot namespace key %q", k)
					}
				}
			}
		})
	})
}

func TestSettingsVersionMigrationV1ToV3(t *testing.T) {
	type settingsV1 struct {
		Volume    float64 `json:"volume"`
		MusicVol  float64 `json:"music_vol"`
		Effects   float64 `json:"effects_vol"`
		Language  string  `json:"language"`
		Subtitles bool    `json:"subtitles"`
		JumpKey   string  `json:"jump_key"`
	}
	type settingsV2 struct {
		Audio struct {
			Master float64            `json:"master"`
			Groups map[string]float64 `json:"groups"`
		} `json:"audio"`
		Locale   string              `json:"locale"`
		Captions bool                `json:"captions"`
		Bindings map[string][]string `json:"bindings"`
	}

	var steps []string
	m1to2 := func(raw json.RawMessage) (json.RawMessage, error) {
		steps = append(steps, "1->2")
		var old settingsV1
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		var next settingsV2
		next.Audio.Master = old.Volume
		next.Audio.Groups = map[string]float64{
			"music":   old.MusicVol,
			"effects": old.Effects,
		}
		next.Locale = old.Language
		next.Captions = old.Subtitles
		if old.JumpKey != "" {
			next.Bindings = map[string][]string{"jump": {old.JumpKey}}
		}
		return json.Marshal(next)
	}
	m2to3 := func(raw json.RawMessage) (json.RawMessage, error) {
		steps = append(steps, "2->3")
		var old settingsV2
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		out := save.DefaultSettings()
		out.Audio.Master = old.Audio.Master
		out.Audio.Groups = map[string]float64{
			"music": old.Audio.Groups["music"],
			"sfx":   old.Audio.Groups["effects"], // renamed "effects" -> "sfx"
			"ui":    1.0,                         // added group
		}
		out.Bindings = old.Bindings
		out.Locale = old.Locale
		out.Accessibility.Captions = old.Captions
		out.Accessibility.CaptionScale = 1.25
		return json.Marshal(out)
	}

	schema := save.SettingsSchema{
		Version: 3,
		Migrations: map[int]save.Migration{
			1: m1to2,
			2: m2to3,
		},
	}

	run := func(t *testing.T, makeBackend func() save.Backend) {
		b := makeBackend()

		// Persist a v1 settings document.
		v1Store := save.NewSettingsStore(b, func() settingsV1 { return settingsV1{} }, save.WithSettingsVersion(1))
		if err := v1Store.Save(settingsV1{
			Volume:    0.7,
			MusicVol:  0.4,
			Effects:   0.9,
			Language:  "en",
			Subtitles: true,
			JumpKey:   "Space",
		}); err != nil {
			t.Fatalf("Save v1: %v", err)
		}

		// Load through v3 DefaultSettingsStore using SettingsSchema.
		steps = nil
		v3Store := save.NewDefaultSettingsStore(b, save.WithSettingsSchema(schema))
		got, err := v3Store.Load()
		if err != nil {
			t.Fatalf("Load migrated v1->v3: %v", err)
		}
		if !slices.Equal(steps, []string{"1->2", "2->3"}) {
			t.Fatalf("migration order = %v, want [1->2 2->3]", steps)
		}
		if !near64(got.Audio.Master, 0.7) || !near64(got.Audio.Groups["music"], 0.4) || !near64(got.Audio.Groups["sfx"], 0.9) || !near64(got.Audio.Groups["ui"], 1.0) {
			t.Fatalf("migrated audio = %+v", got.Audio)
		}
		if got.Locale != "en" || !got.Accessibility.Captions || !near64(got.Accessibility.CaptionScale, 1.25) {
			t.Fatalf("migrated locale/accessibility = %q / %+v", got.Locale, got.Accessibility)
		}
		if !slices.Equal(got.Bindings["jump"], []string{"Space"}) {
			t.Fatalf("migrated bindings = %+v", got.Bindings)
		}
		// Defaults for fields untouched by v1 are preserved.
		if !got.Display.VSync || !near64(got.Accessibility.GameSpeed, 1.0) {
			t.Fatalf("defaults lost after migration: display=%+v access=%+v", got.Display, got.Accessibility)
		}

		// Re-saving at v3 runs zero migrations on next Load.
		steps = nil
		if err := v3Store.Save(got); err != nil {
			t.Fatalf("Save v3: %v", err)
		}
		again, err := v3Store.Load()
		if err != nil || len(steps) != 0 || !near64(again.Audio.Master, 0.7) {
			t.Fatalf("v3 reload = (%+v, %v), steps=%v", again, err, steps)
		}

		// Older v1 reader refuses v3 settings with ErrFutureVersion (no fallback to v1 backup).
		if _, err := v1Store.Load(); !errors.Is(err, save.ErrFutureVersion) {
			t.Fatalf("v1 load of v3 settings err = %v, want ErrFutureVersion", err)
		}
	}

	t.Run("FileBackend", func(t *testing.T) {
		dir := t.TempDir()
		run(t, func() save.Backend { return save.FileBackend(dir) })
	})

	t.Run("StorageBackend", func(t *testing.T) {
		app, _, _ := startStorageApp(t)
		run(t, func() save.Backend { return save.StorageBackend(app.Storage(), "settings-mig") })
	})
}

func TestSettingsCorruptPrimaryFallsBackToBackupAndReset(t *testing.T) {
	t.Run("FileBackend", func(t *testing.T) {
		dir := t.TempDir()
		store := save.NewFileSettingsStore(dir)

		// Missing settings file returns DefaultSettings cleanly.
		if store.Exists() {
			t.Fatal("Exists = true before Save")
		}
		def, err := store.Load()
		if err != nil {
			t.Fatalf("Load missing: %v", err)
		}
		if !near64(def.Audio.Master, 1.0) || !def.Display.VSync {
			t.Fatalf("missing returned %+v, want defaults", def)
		}

		// Write first good version, then second good version so .bak holds first.
		first := save.DefaultSettings()
		first.Locale = "fr"
		first.Audio.Master = 0.6
		if err := store.Save(first); err != nil {
			t.Fatalf("Save first: %v", err)
		}
		second := first
		second.Locale = "de"
		second.Audio.Master = 0.9
		if err := store.Save(second); err != nil {
			t.Fatalf("Save second: %v", err)
		}

		primary := filepath.Join(dir, "settings.json")

		// Corrupt primary with truncated JSON -> falls back to .bak (first).
		if err := os.WriteFile(primary, []byte(`{"version":1,"data":{"locale":"br`), 0o600); err != nil {
			t.Fatal(err)
		}
		recovered, err := store.Load()
		if err != nil {
			t.Fatalf("Load with corrupt primary: %v", err)
		}
		if recovered.Locale != "fr" || !near64(recovered.Audio.Master, 0.6) {
			t.Fatalf("recovered = %+v, want first (fr, 0.6)", recovered)
		}

		// Missing primary file -> also recovers from .bak.
		if err := os.Remove(primary); err != nil {
			t.Fatal(err)
		}
		recovered, err = store.Load()
		if err != nil || recovered.Locale != "fr" {
			t.Fatalf("Load with removed primary = (%+v, %v)", recovered, err)
		}

		// Both primary and backup corrupt -> returns ErrCorrupt.
		if err := os.WriteFile(primary, []byte(`not json`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json.bak"), []byte(`also bad`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); !errors.Is(err, save.ErrCorrupt) {
			t.Fatalf("Load with both corrupt err = %v, want ErrCorrupt", err)
		}

		// Reset restores DefaultSettings cleanly even after corruption.
		if err := store.Reset(); err != nil {
			t.Fatalf("Reset: %v", err)
		}
		afterReset, err := store.Load()
		if err != nil {
			t.Fatalf("Load after Reset: %v", err)
		}
		if afterReset.Locale != "" || !near64(afterReset.Audio.Master, 1.0) || !afterReset.Display.VSync {
			t.Fatalf("after Reset = %+v, want defaults", afterReset)
		}
	})

	t.Run("StorageBackend", func(t *testing.T) {
		app, _, _ := startStorageApp(t)
		store := save.NewStorageSettingsStore(app.Storage(), "prefs")

		first := save.DefaultSettings()
		first.Locale = "ko"
		first.Audio.Master = 0.5
		if err := store.Save(first); err != nil {
			t.Fatalf("Save first: %v", err)
		}
		second := first
		second.Locale = "es"
		if err := store.Save(second); err != nil {
			t.Fatalf("Save second: %v", err)
		}

		// Corrupt primary storage key directly.
		if err := app.Storage().SetItem("prefs.settings", `{"version":1,"data":BROKEN`); err != nil {
			t.Fatalf("corrupt primary: %v", err)
		}
		recovered, err := store.Load()
		if err != nil {
			t.Fatalf("Load with corrupt primary storage key: %v", err)
		}
		if recovered.Locale != "ko" || !near64(recovered.Audio.Master, 0.5) {
			t.Fatalf("recovered = %+v, want first (ko, 0.5)", recovered)
		}

		// Simulate app restart with persisted storage.
		if err := store.Save(second); err != nil {
			t.Fatalf("Save before restart: %v", err)
		}
		kept := map[string]string{}
		st := app.Storage()
		for _, k := range st.Keys() {
			kept[k], _ = st.GetItem(k)
		}
		var restartedStore *save.DefaultSettingsStore
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			restartedStore = save.NewStorageSettingsStore(cx.Storage(), "prefs")
			return core.View{}
		}, nsgotest.Storage(kept))

		afterRestart, err := restartedStore.Load()
		if err != nil || afterRestart.Locale != "es" {
			t.Fatalf("after restart = (%+v, %v), want Locale=es", afterRestart, err)
		}

		// Reset restores defaults.
		if err := restartedStore.Reset(); err != nil {
			t.Fatalf("Reset: %v", err)
		}
		resetVal, err := restartedStore.Load()
		if err != nil || resetVal.Locale != "" || !near64(resetVal.Audio.Master, 1.0) {
			t.Fatalf("after Reset = (%+v, %v), want defaults", resetVal, err)
		}
	})
}

type customGameSettings struct {
	save.Settings
	Difficulty string  `json:"difficulty"`
	FOV        float64 `json:"fov"`
}

func (c *customGameSettings) Validate() {
	c.Settings.Validate()
	if c.FOV < 60 {
		c.FOV = 60
	}
	if c.FOV > 120 {
		c.FOV = 120
	}
}

func TestGenericSettingsStoreWithEmbeddedSettingsAndValidator(t *testing.T) {
	dir := t.TempDir()
	defaults := func() customGameSettings {
		return customGameSettings{
			Settings:   save.DefaultSettings(),
			Difficulty: "normal",
			FOV:        90,
		}
	}
	store := save.NewSettingsStore(save.FileBackend(dir), defaults)

	// Missing returns custom defaults.
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load default custom: %v", err)
	}
	if got.Difficulty != "normal" || got.FOV != 90 || !got.Display.VSync {
		t.Fatalf("custom defaults = %+v", got)
	}

	// Save clamps via Validate() on both embedded Settings and custom fields.
	got.Difficulty = "hard"
	got.FOV = 180
	got.Audio.Master = 4.0
	if err := store.Save(got); err != nil {
		t.Fatalf("Save custom: %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load custom: %v", err)
	}
	if reloaded.Difficulty != "hard" || reloaded.FOV != 120 || reloaded.Audio.Master != 1.0 {
		t.Fatalf("reloaded custom = %+v, want hard/120/1.0", reloaded)
	}
}

func TestSaveDoesNotMutateCallerSettingsMaps(t *testing.T) {
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	s := save.DefaultSettings()
	s.Audio.Groups["music"] = 2.0 // out of range
	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if s.Audio.Groups["music"] != 2.0 {
		t.Fatalf("Save mutated caller's Audio.Groups map: got %v, want 2.0", s.Audio.Groups["music"])
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Audio.Groups["music"] != 1.0 {
		t.Fatalf("stored Audio.Groups[music] = %v, want clamped 1.0", loaded.Audio.Groups["music"])
	}
}

func TestSettingsPartialJSONPreservesDefaults(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "settings.json")

	// Partial JSON omitting audio.master, audio.groups, display.vsync, accessibility scales.
	if err := os.WriteFile(primary, []byte(`{"version":1,"data":{"audio":{"muted":true},"display":{"fullscreen":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := save.NewFileSettingsStore(dir)
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load partial JSON: %v", err)
	}
	if !got.Audio.Muted || !near64(got.Audio.Master, 1.0) || !near64(got.Audio.Groups["music"], 1.0) || !near64(got.Audio.Groups["sfx"], 1.0) || !near64(got.Audio.Groups["ui"], 1.0) {
		t.Fatalf("partial audio = %+v, want muted=true master=1.0 and default groups", got.Audio)
	}
	if !got.Display.Fullscreen || !got.Display.VSync || !got.Display.AutoDropLowPower || got.Display.ScaleMode != "fit" {
		t.Fatalf("partial display = %+v", got.Display)
	}
	if !near64(got.Accessibility.CaptionScale, 1.0) || !near64(got.Accessibility.ScreenShake, 1.0) || !near64(got.Accessibility.GameSpeed, 1.0) {
		t.Fatalf("partial accessibility = %+v", got.Accessibility)
	}

	// Explicit master=0 and explicit groups map override defaults cleanly.
	if err := os.WriteFile(primary, []byte(`{"version":1,"data":{"audio":{"master":0,"groups":{"music":0.25}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load()
	if err != nil {
		t.Fatalf("Load explicit zero master & groups: %v", err)
	}
	if got.Audio.Master != 0 {
		t.Fatalf("Master = %v, want 0", got.Audio.Master)
	}
	if len(got.Audio.Groups) != 1 || !near64(got.Audio.Groups["music"], 0.25) {
		t.Fatalf("Groups = %+v, want only music=0.25", got.Audio.Groups)
	}
}

func TestSettingsLoadFailureSkipsTargetsAndMissingMigration(t *testing.T) {
	dir := t.TempDir()
	b := save.FileBackend(dir)

	// Corrupt primary with no backup -> LoadAndApply fails and skips all targets.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{corrupt`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := save.NewDefaultSettingsStore(b)
	called := false
	_, err := store.LoadAndApply(save.SettingsTargets{
		SetMasterVolume: func(float64) { called = true },
		ApplyLocale:     func(string) error { called = true; return nil },
	})
	if !errors.Is(err, save.ErrCorrupt) {
		t.Fatalf("LoadAndApply corrupt err = %v, want ErrCorrupt", err)
	}
	if called {
		t.Fatal("targets were called despite Load failure")
	}

	// Missing migration step returns ErrMissingMigration without calling targets.
	if err := store.Save(save.DefaultSettings()); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	v3Missing := save.NewDefaultSettingsStore(b,
		save.WithSettingsVersion(3),
		save.WithSettingsMigration(1, func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }),
	)
	_, err = v3Missing.LoadAndApply(save.SettingsTargets{
		ApplyDisplay: func(save.DisplaySettings) error { called = true; return nil },
	})
	if !errors.Is(err, save.ErrMissingMigration) {
		t.Fatalf("LoadAndApply missing migration err = %v, want ErrMissingMigration", err)
	}
	if called {
		t.Fatal("targets were called despite ErrMissingMigration")
	}
}

func TestSettingsFailedWriteLeavesPreviousIntactAndDeleteIsolated(t *testing.T) {
	t.Run("InvalidCustomJSONAndDelete", func(t *testing.T) {
		dir := t.TempDir()
		b := save.FileBackend(dir)
		slotStore := save.NewStore(b)
		settingsStore := save.NewDefaultSettingsStore(b)

		if err := slotStore.Save("slot1", playerState{Name: "Hero", Level: 10}); err != nil {
			t.Fatalf("Save slot1: %v", err)
		}
		good := save.DefaultSettings()
		good.Locale = "en-US"
		good.Audio.Master = 0.7
		if err := settingsStore.Save(good); err != nil {
			t.Fatalf("Save good: %v", err)
		}

		// Invalid json.RawMessage in Custom fails marshal before touching disk.
		bad := good
		bad.Custom = map[string]json.RawMessage{"broken": json.RawMessage(`{invalid`)}
		if err := settingsStore.Save(bad); err == nil {
			t.Fatal("Save invalid Custom JSON succeeded, want error")
		}
		got, err := settingsStore.Load()
		if err != nil || got.Locale != "en-US" || !near64(got.Audio.Master, 0.7) {
			t.Fatalf("Load after failed Save = (%+v, %v), want original good settings", got, err)
		}

		// Deleting settings removes settings only and leaves save slots intact.
		if err := settingsStore.Delete(); err != nil {
			t.Fatalf("Delete settings: %v", err)
		}
		if settingsStore.Exists() {
			t.Fatal("settings still exist after Delete")
		}
		def, err := settingsStore.Load()
		if err != nil || def.Locale != "" || !near64(def.Audio.Master, 1.0) {
			t.Fatalf("Load after Delete = (%+v, %v), want defaults", def, err)
		}
		var ps playerState
		if err := slotStore.Load("slot1", &ps); err != nil || ps.Name != "Hero" {
			t.Fatalf("slot1 after settings Delete = (%+v, %v), want Hero", ps, err)
		}
	})

	t.Run("StorageFullLeavesPreviousIntact", func(t *testing.T) {
		var settingsStore *save.DefaultSettingsStore
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cx.ProvideStorage("tight-settings-quota", 600)
			settingsStore = save.NewStorageSettingsStore(cx.Storage(), "app")
			return core.View{}
		})

		good := save.DefaultSettings()
		good.Locale = "fr"
		if err := settingsStore.Save(good); err != nil {
			t.Fatalf("Save initial settings: %v", err)
		}

		huge := good
		huge.Custom = map[string]json.RawMessage{
			"blob": json.RawMessage(`"` + strings.Repeat("Z", 800) + `"`),
		}
		err := settingsStore.Save(huge)
		var fullErr *core.StorageFullError
		if !errors.As(err, &fullErr) {
			t.Fatalf("Save oversized settings err = %v, want *core.StorageFullError", err)
		}

		got, err := settingsStore.Load()
		if err != nil || got.Locale != "fr" {
			t.Fatalf("Load after StorageFullError = (%+v, %v), want Locale=fr", got, err)
		}
	})
}

type naiveMemBackend struct {
	primary map[string][]byte
	backup  map[string][]byte
}

func newNaiveMemBackend() *naiveMemBackend {
	return &naiveMemBackend{
		primary: make(map[string][]byte),
		backup:  make(map[string][]byte),
	}
}

func (m *naiveMemBackend) Read(slot string) ([]byte, error) {
	b, ok := m.primary[slot]
	if !ok {
		return nil, save.ErrNotFound
	}
	return slices.Clone(b), nil
}

func (m *naiveMemBackend) ReadBackup(slot string) ([]byte, error) {
	b, ok := m.backup[slot]
	if !ok {
		return nil, save.ErrNotFound
	}
	return slices.Clone(b), nil
}

func (m *naiveMemBackend) Write(slot string, data []byte) error {
	if prev, ok := m.primary[slot]; ok {
		m.backup[slot] = slices.Clone(prev)
	}
	m.primary[slot] = slices.Clone(data)
	return nil
}

func (m *naiveMemBackend) Delete(slot string) error {
	delete(m.primary, slot)
	delete(m.backup, slot)
	return nil
}

func (m *naiveMemBackend) Exists(slot string) bool {
	if _, ok := m.primary[slot]; ok {
		return true
	}
	_, ok := m.backup[slot]
	return ok
}

func (m *naiveMemBackend) Slots() ([]string, error) {
	seen := make(map[string]struct{})
	for k := range m.primary {
		seen[k] = struct{}{}
	}
	for k := range m.backup {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

func TestSettingsIsolatedOnEmptyPrefixAndCustomBackend(t *testing.T) {
	t.Run("EmptyPrefixStorageBackend", func(t *testing.T) {
		app, _, _ := startStorageApp(t)
		b := save.StorageBackend(app.Storage(), "")
		slotStore := save.NewStore(b)
		settingsStore := save.NewDefaultSettingsStore(b)

		s := save.DefaultSettings()
		s.Locale = "pt-BR"
		if err := settingsStore.Save(s); err != nil {
			t.Fatalf("Save settings: %v", err)
		}
		if err := slotStore.Save("slot_1", playerState{Name: "Ana"}); err != nil {
			t.Fatalf("Save slot_1: %v", err)
		}

		if _, ok := app.Storage().GetItem("settings"); !ok {
			t.Fatalf("expected key 'settings', got keys %v", app.Storage().Keys())
		}
		if err := slotStore.DeleteAll(); err != nil {
			t.Fatalf("DeleteAll: %v", err)
		}
		got, err := settingsStore.Load()
		if err != nil || got.Locale != "pt-BR" {
			t.Fatalf("settings after DeleteAll = (%+v, %v), want pt-BR", got, err)
		}
	})

	t.Run("CustomBackend", func(t *testing.T) {
		b := newNaiveMemBackend()
		slotStore := save.NewStore(b)
		settingsStore := save.NewDefaultSettingsStore(b)

		s := save.DefaultSettings()
		s.Locale = "it"
		if err := settingsStore.Save(s); err != nil {
			t.Fatalf("Save settings: %v", err)
		}
		if err := slotStore.Save("slot_1", playerState{Name: "Leo"}); err != nil {
			t.Fatalf("Save slot_1: %v", err)
		}

		slots, err := slotStore.Slots()
		if err != nil || !slices.Equal(slots, []string{"slot_1"}) {
			t.Fatalf("Store.Slots on custom backend = (%v, %v), want [slot_1]", slots, err)
		}
		if err := slotStore.DeleteAll(); err != nil {
			t.Fatalf("DeleteAll: %v", err)
		}
		got, err := settingsStore.Load()
		if err != nil || got.Locale != "it" {
			t.Fatalf("settings on custom backend after DeleteAll = (%+v, %v), want it", got, err)
		}
	})
}

func TestSettingsValidateTrimsDisplayID(t *testing.T) {
	s := save.DefaultSettings()
	s.Display.DisplayID = "  display-2 \n"
	s.Validate()
	if s.Display.DisplayID != "display-2" {
		t.Fatalf("DisplayID = %q, want %q", s.Display.DisplayID, "display-2")
	}
}

// recordingBackend wraps a Backend, records the names written through it and exposes the wrapped backend with Unwrap.
type recordingBackend struct {
	save.Backend
	written []string
}

func (b *recordingBackend) Write(slot string, data []byte) error {
	b.written = append(b.written, slot)
	return b.Backend.Write(slot, data)
}

func (b *recordingBackend) Unwrap() save.Backend { return b.Backend }

func TestSettingsStoreSeesThroughUnwrap(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	plain := save.NewStorageSettingsStore(app.Storage(), "game")
	before := save.DefaultSettings()
	before.Locale = "ja"
	if err := plain.Save(before); err != nil {
		t.Fatal(err)
	}

	// Settings saved before a wrapper was adopted are still found through it, under the same key.
	wrapped := &recordingBackend{Backend: save.StorageBackend(app.Storage(), "game")}
	settings := save.NewDefaultSettingsStore(wrapped)
	got, err := settings.Load()
	if err != nil || got.Locale != "ja" {
		t.Fatalf("Load through wrapper = %+v, %v", got, err)
	}
	got.Locale = "fr"
	if err := settings.Save(got); err != nil {
		t.Fatal(err)
	}
	if again, err := plain.Load(); err != nil || again.Locale != "fr" {
		t.Fatalf("Load without wrapper = %+v, %v", again, err)
	}
	if len(wrapped.written) != 0 {
		t.Fatalf("settings went through the wrapper: %q", wrapped.written)
	}
	if _, ok := app.Storage().GetItem("game.slot.settings"); ok {
		t.Fatal("settings stored as a save slot")
	}
}

func TestAccessibilityFieldsNormalizeMigrateAndPlatformFallback(t *testing.T) {
	// NormalizeColorBlindMode accepts both "none" and "normal" as canonical "none".
	normCases := []struct {
		in   string
		want string
	}{
		{"", "none"},
		{"  ", "none"},
		{"none", "none"},
		{"Normal", "none"},
		{"NORMAL", "none"},
		{"Protanopia", "protanopia"},
		{"deuteranopia", "deuteranopia"},
		{"TRITANOPIA", "tritanopia"},
		{"high-contrast", "high-contrast"},
		{"highcontrast", "high-contrast"},
		{"high_contrast", "high-contrast"},
		{"custom-mode", "custom-mode"},
	}
	for _, tc := range normCases {
		if got := save.NormalizeColorBlindMode(tc.in); got != tc.want {
			t.Errorf("NormalizeColorBlindMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Old settings.json with Custom["reduce_flashes"] and ColorBlindMode "normal" migrates on Load.
	dir := t.TempDir()
	primary := filepath.Join(dir, "settings.json")
	legacyJSON := `{"version":1,"data":{"accessibility":{"captions":true,"caption_scale":1.5,"color_blind_mode":"normal","reduce_motion":false,"high_contrast":false,"screen_shake":0.5,"game_speed":0.75},"custom":{"reduce_flashes":true,"resolution":"1280x720"}}}`
	if err := os.WriteFile(primary, []byte(legacyJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := save.NewFileSettingsStore(dir)
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load legacy settings: %v", err)
	}
	if !loaded.Accessibility.ReduceFlashes {
		t.Fatal("legacy custom.reduce_flashes did not migrate to Accessibility.ReduceFlashes")
	}
	if loaded.Accessibility.ColorBlindMode != "none" {
		t.Fatalf("ColorBlindMode = %q, want %q", loaded.Accessibility.ColorBlindMode, "none")
	}
	if _, ok := loaded.Custom["reduce_flashes"]; ok {
		t.Fatal("legacy custom.reduce_flashes was not removed after migration")
	}
	if string(loaded.Custom["resolution"]) != `"1280x720"` {
		t.Fatalf("unrelated custom.resolution = %q, want %q", string(loaded.Custom["resolution"]), `"1280x720"`)
	}

	// All new assist fields round-trip cleanly.
	loaded.Accessibility.SFXCaptions = true
	loaded.Accessibility.Invincibility = true
	loaded.Accessibility.AutoAim = true
	loaded.Accessibility.HoldToToggle = true
	loaded.Accessibility.SetReduceMotion(false)
	loaded.Accessibility.SetHighContrast(false)
	loaded.Accessibility.SetCaptionScale(1.0)
	if err := store.Save(loaded); err != nil {
		t.Fatalf("Save expanded accessibility: %v", err)
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Reload expanded accessibility: %v", err)
	}
	if reloaded.Accessibility != loaded.Accessibility {
		t.Fatalf("reloaded Accessibility = %+v, want %+v", reloaded.Accessibility, loaded.Accessibility)
	}

	// Platform fallback and explicit overrides via Effective* helpers.
	var env *core.Env
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		env = cx.Env()
		return core.View{}
	}, nsgotest.Environment(core.Environment{
		ReduceMotion:     true,
		IncreaseContrast: true,
		FontScale:        1.75,
	}))

	def := save.DefaultSettings().Accessibility
	if !def.EffectiveReduceMotion(env) {
		t.Fatal("unset ReduceMotion did not follow env.ReduceMotion=true")
	}
	if !def.EffectiveHighContrast(env) {
		t.Fatal("unset HighContrast did not follow env.IncreaseContrast=true")
	}
	if !near64(def.EffectiveCaptionScale(env), 1.75) {
		t.Fatalf("unset CaptionScale = %v, want 1.75 from env.FontScale", def.EffectiveCaptionScale(env))
	}

	// Explicit false / 1.0 overrides override the platform environment.
	if reloaded.Accessibility.EffectiveReduceMotion(env) {
		t.Fatal("explicit SetReduceMotion(false) did not override env.ReduceMotion=true")
	}
	if reloaded.Accessibility.EffectiveHighContrast(env) {
		t.Fatal("explicit SetHighContrast(false) did not override env.IncreaseContrast=true")
	}
	if !near64(reloaded.Accessibility.EffectiveCaptionScale(env), 1.0) {
		t.Fatalf("explicit SetCaptionScale(1.0) = %v, want 1.0 overriding env.FontScale", reloaded.Accessibility.EffectiveCaptionScale(env))
	}

	// Clearing overrides restores live platform tracking.
	reloaded.Accessibility.ClearReduceMotion()
	reloaded.Accessibility.ClearHighContrast()
	reloaded.Accessibility.ClearCaptionScale()
	if !reloaded.Accessibility.EffectiveReduceMotion(env) ||
		!reloaded.Accessibility.EffectiveHighContrast(env) ||
		!near64(reloaded.Accessibility.EffectiveCaptionScale(env), 1.75) {
		t.Fatalf("Clear* did not restore env fallback: rm=%v hc=%v scale=%v",
			reloaded.Accessibility.EffectiveReduceMotion(env),
			reloaded.Accessibility.EffectiveHighContrast(env),
			reloaded.Accessibility.EffectiveCaptionScale(env))
	}

	app.SetEnvironment(core.Environment{ReduceMotion: false, IncreaseContrast: false, FontScale: 0})
	if reloaded.Accessibility.EffectiveReduceMotion(env) ||
		reloaded.Accessibility.EffectiveHighContrast(env) ||
		!near64(reloaded.Accessibility.EffectiveCaptionScale(env), 1.0) {
		t.Fatalf("updated env fallback failed: rm=%v hc=%v scale=%v",
			reloaded.Accessibility.EffectiveReduceMotion(env),
			reloaded.Accessibility.EffectiveHighContrast(env),
			reloaded.Accessibility.EffectiveCaptionScale(env))
	}
}

func TestSettingsStoreSubscribe(t *testing.T) {
	store := save.NewFileSettingsStore(t.TempDir())
	var events []string
	unsub1 := store.Subscribe(func() {
		// Calling Load inside callback must not deadlock.
		s, err := store.Load()
		if err != nil {
			t.Errorf("Load inside Subscribe callback: %v", err)
		}
		events = append(events, "sub1:"+s.Locale)
	})
	unsub2 := store.Subscribe(func() {
		events = append(events, "sub2")
	})

	s := save.DefaultSettings()
	s.Locale = "en"
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(events, []string{"sub1:en", "sub2"}) {
		t.Fatalf("events after Save = %v, want [sub1:en sub2]", events)
	}

	unsub2()
	events = nil
	if err := store.Reset(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(events, []string{"sub1:"}) {
		t.Fatalf("events after Reset = %v, want [sub1:]", events)
	}

	events = nil
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(events, []string{"sub1:"}) {
		t.Fatalf("events after Delete = %v, want [sub1:]", events)
	}

	unsub1()
	events = nil
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events after unsubscribe = %v, want none", events)
	}
}

func TestAudioSettingsMonoAndBalancePersistValidateAndApply(t *testing.T) {
	// Defaults: Mono is nil (follow system) and Balance is 0.
	def := save.DefaultSettings()
	if def.Audio.Mono != nil || def.Audio.Balance != 0 {
		t.Fatalf("default audio mono/balance = (%v, %v), want (nil, 0)", def.Audio.Mono, def.Audio.Balance)
	}
	if !def.Audio.ResolveMono(true) || def.Audio.ResolveMono(false) {
		t.Fatal("ResolveMono with nil Mono did not follow systemMono")
	}

	// Validation clamps Balance into [-1, 1] and resets NaN to 0.
	for _, tc := range []struct {
		in   float64
		want float64
	}{
		{2.5, 1.0},
		{-3.0, -1.0},
		{math.Inf(1), 1.0},
		{math.Inf(-1), -1.0},
		{math.NaN(), 0},
		{-0.35, -0.35},
	} {
		s := save.DefaultSettings()
		s.Audio.Balance = tc.in
		s.Validate()
		if s.Audio.Balance != tc.want {
			t.Errorf("Validate Balance(%v) = %v, want %v", tc.in, s.Audio.Balance, tc.want)
		}
	}

	// JSON round-trip preserves nil, true, false, and Balance; caller pointer mutation does not alias.
	dir := t.TempDir()
	store := save.NewFileSettingsStore(dir)

	var mixer *sound.Mixer
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mixer = sound.NewMixer(cx, "test-mono-balance")
		return core.View{}
	}, nsgotest.Environment(core.Environment{Audio: true}))

	monoOn := true
	s := save.DefaultSettings()
	s.Audio.Mono = &monoOn
	s.Audio.Balance = 0.45
	if err := store.Save(s); err != nil {
		t.Fatalf("Save mono=true: %v", err)
	}
	monoOn = false // mutate caller's bool after Save
	loaded, err := store.LoadAndApply(save.SettingsTargets{Sound: mixer})
	if err != nil {
		t.Fatalf("LoadAndApply mono=true: %v", err)
	}
	if loaded.Audio.Mono == nil || !*loaded.Audio.Mono || !near64(loaded.Audio.Balance, 0.45) {
		t.Fatalf("loaded mono=true = %+v", loaded.Audio)
	}
	if !loaded.Audio.ResolveMono(false) {
		t.Fatal("ResolveMono with explicit true returned false")
	}
	if !mixer.Mono() || !near32(mixer.Balance(), 0.45) {
		t.Fatalf("mixer after Apply(mono=true, balance=0.45) = mono:%v balance:%v", mixer.Mono(), mixer.Balance())
	}

	// Explicit mono=false overrides systemMono=true and persists distinct from nil.
	monoOff := false
	s.Audio.Mono = &monoOff
	s.Audio.Balance = -0.6
	if err := store.Save(s); err != nil {
		t.Fatalf("Save mono=false: %v", err)
	}
	loaded, err = store.LoadAndApply(save.SettingsTargets{Sound: mixer})
	if err != nil {
		t.Fatalf("LoadAndApply mono=false: %v", err)
	}
	if loaded.Audio.Mono == nil || *loaded.Audio.Mono || !near64(loaded.Audio.Balance, -0.6) {
		t.Fatalf("loaded mono=false = %+v", loaded.Audio)
	}
	if loaded.Audio.ResolveMono(true) {
		t.Fatal("ResolveMono with explicit false returned true")
	}
	if mixer.Mono() || !near32(mixer.Balance(), -0.6) {
		t.Fatalf("mixer after Apply(mono=false, balance=-0.6) = mono:%v balance:%v", mixer.Mono(), mixer.Balance())
	}

	// Nil mono (follow system) clears explicit override on mixer; explicit JSON null also decodes to nil.
	mixer.SetMono(true)
	s.Audio.Mono = nil
	s.Audio.Balance = 0
	if err := store.Save(s); err != nil {
		t.Fatalf("Save mono=nil: %v", err)
	}
	loaded, err = store.LoadAndApply(save.SettingsTargets{Sound: mixer})
	if err != nil {
		t.Fatalf("LoadAndApply mono=nil: %v", err)
	}
	if loaded.Audio.Mono != nil || loaded.Audio.Balance != 0 {
		t.Fatalf("loaded mono=nil = %+v", loaded.Audio)
	}
	if mixer.Mono() || mixer.Balance() != 0 {
		t.Fatalf("mixer after Apply(mono=nil, balance=0) = mono:%v balance:%v", mixer.Mono(), mixer.Balance())
	}

	// Partial JSON with explicit null mono clears a non-nil default cleanly.
	var partial save.AudioSettings
	v := true
	partial.Mono = &v
	if err := json.Unmarshal([]byte(`{"mono":null,"balance":-0.25}`), &partial); err != nil {
		t.Fatalf("Unmarshal null mono: %v", err)
	}
	if partial.Mono != nil || !near64(partial.Balance, -0.25) {
		t.Fatalf("partial unmarshal = %+v, want mono=nil balance=-0.25", partial)
	}
}
