package editor

import (
	"math"
	"testing"
)

func TestFormatGainDBNonFiniteAndRange(t *testing.T) {
	cases := []struct {
		in   float32
		want string
	}{
		{float32(math.NaN()), "−∞ dB"},
		{float32(math.Inf(-1)), "−∞ dB"},
		{float32(math.Inf(1)), "+∞ dB"},
		{-1, "−∞ dB"},
		{0, "−∞ dB"},
		{1, "0.0 dB"},
	}
	for _, tc := range cases {
		if got := formatGainDB(tc.in); got != tc.want {
			t.Errorf("formatGainDB(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMatchSettingsPageQueryShortcutsAndLocales(t *testing.T) {
	cases := []struct {
		q          string
		hasProject bool
		want       SettingsPage
		ok         bool
	}{
		{"hotkey", true, SettingsPageShortcuts, true},
		{"keymap", true, SettingsPageShortcuts, true},
		{"shortcut", true, SettingsPageShortcuts, true},
		{"快捷键", true, SettingsPageShortcuts, true},
		{"快捷键", false, SettingsPageShortcuts, true},
		{"input", true, SettingsPageInput, true},
		{"音频", true, SettingsPageAudio, true},
		{"导出", true, SettingsPageExport, true},
		{"项目", true, SettingsPageProject, true},
		{"语言", false, SettingsPageGeneral, true},
		{"", true, "", false},
	}
	for _, tc := range cases {
		got, ok := matchSettingsPageQuery(tc.q, tc.hasProject)
		if ok != tc.ok || got != tc.want {
			t.Errorf("matchSettingsPageQuery(%q, %v) = (%q, %v), want (%q, %v)", tc.q, tc.hasProject, got, ok, tc.want, tc.ok)
		}
	}
}

func TestOpenSettingsPageStopsAudioAuditionOnLeave(t *testing.T) {
	ed := &Editor{
		settingsOpen: true,
		settingsPage: SettingsPageAudio,
	}
	ed.audioMixer.auditioning = true
	ed.OpenSettingsPage(SettingsPageProject)
	if ed.audioMixer.auditioning {
		t.Fatal("leaving SettingsPageAudio did not stop audio audition")
	}
}
