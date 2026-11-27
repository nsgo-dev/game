package presence_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/presence"
)

type steamCall struct {
	Key   string
	Value string
}

type mockSteamDriver struct {
	mu       sync.Mutex
	calls    []steamCall
	state    map[string]string
	clears   int
	closed   int
	rejectOn string
}

func newMockSteamDriver() *mockSteamDriver {
	return &mockSteamDriver{
		state: make(map[string]string),
	}
}

func (m *mockSteamDriver) SetRichPresence(key, value string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rejectOn != "" && key == m.rejectOn {
		return false
	}
	m.calls = append(m.calls, steamCall{Key: key, Value: value})
	if value == "" {
		delete(m.state, key)
	} else {
		m.state[key] = value
	}
	return true
}

func (m *mockSteamDriver) ClearRichPresence() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clears++
	clear(m.state)
}

func (m *mockSteamDriver) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return nil
}

func (m *mockSteamDriver) snapshot() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.state)
}

func (m *mockSteamDriver) recordedCalls() []steamCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]steamCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func TestSteamSetLevelModePartyAndClear(t *testing.T) {
	drv := newMockSteamDriver()
	s := presence.NewSteam(presence.SteamConfig{Driver: drv})
	ctx := context.Background()

	if err := s.Set(ctx, presence.Activity{
		Mode:       "Campaign",
		Level:      "Forest 1-2",
		PartyID:    "party-9",
		PartySize:  2,
		PartyMax:   4,
		JoinSecret: "+connect 192.0.2.10:27015",
		Custom: map[string]string{
			"steam_display": "#Status_InLevel",
			"difficulty":    "hard",
		},
	}); err != nil {
		t.Fatalf("Set initial: %v", err)
	}

	want := map[string]string{
		"connect":                 "+connect 192.0.2.10:27015",
		"difficulty":              "hard",
		"level":                   "Forest 1-2",
		"mode":                    "Campaign",
		"status":                  "Campaign - Forest 1-2",
		"steam_display":           "#Status_InLevel",
		"steam_player_group":      "party-9",
		"steam_player_group_size": "2",
	}
	if got := drv.snapshot(); !maps.Equal(got, want) {
		t.Fatalf("initial steam state = %v, want %v", got, want)
	}

	beforeCount := len(drv.recordedCalls())

	// Changing level should only emit the changed keys ("level" and "status").
	if err := s.Set(ctx, presence.Activity{
		Mode:       "Campaign",
		Level:      "Cavern 2-1",
		PartyID:    "party-9",
		PartySize:  2,
		PartyMax:   4,
		JoinSecret: "+connect 192.0.2.10:27015",
		Custom: map[string]string{
			"steam_display": "#Status_InLevel",
			"difficulty":    "hard",
		},
	}); err != nil {
		t.Fatalf("Set next level: %v", err)
	}

	delta := drv.recordedCalls()[beforeCount:]
	wantDelta := []steamCall{
		{Key: "level", Value: "Cavern 2-1"},
		{Key: "status", Value: "Campaign - Cavern 2-1"},
	}
	if len(delta) != len(wantDelta) || delta[0] != wantDelta[0] || delta[1] != wantDelta[1] {
		t.Fatalf("level change delta = %+v, want %+v", delta, wantDelta)
	}

	// Leaving party deletes the old steam_player_group* keys explicitly.
	beforeLeave := len(drv.recordedCalls())
	if err := s.Set(ctx, presence.Activity{
		Mode:  "Campaign",
		Level: "Cavern 2-1",
	}); err != nil {
		t.Fatalf("Set solo: %v", err)
	}
	leaveState := drv.snapshot()
	wantSolo := map[string]string{
		"level":  "Cavern 2-1",
		"mode":   "Campaign",
		"status": "Campaign - Cavern 2-1",
	}
	if !maps.Equal(leaveState, wantSolo) {
		t.Fatalf("solo steam state = %v, want %v", leaveState, wantSolo)
	}
	leaveDelta := drv.recordedCalls()[beforeLeave:]
	if len(leaveDelta) != 5 {
		t.Fatalf("expected 5 key deletions when leaving party/custom, got %+v", leaveDelta)
	}
	for _, c := range leaveDelta {
		if c.Value != "" {
			t.Fatalf("expected empty value deletion, got %+v", c)
		}
	}

	// Clear calls ClearRichPresence and resets internal key cache.
	if err := s.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if drv.clears != 1 || len(drv.snapshot()) != 0 {
		t.Fatalf("after Clear: clears=%d state=%v", drv.clears, drv.snapshot())
	}

	// Close shuts down driver and rejects subsequent calls.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if drv.closed != 1 {
		t.Fatalf("driver closed count = %d, want 1", drv.closed)
	}
	if err := s.Set(ctx, presence.Activity{Level: "Closed"}); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Set after Close = %v, want ErrClosed", err)
	}
	if err := s.Clear(ctx); !errors.Is(err, presence.ErrClosed) {
		t.Fatalf("Clear after Close = %v, want ErrClosed", err)
	}
}

func TestSteamDefaultDisplayTokenAndCustomOverrides(t *testing.T) {
	drv := newMockSteamDriver()
	s := presence.NewSteam(presence.SteamConfig{
		DisplayToken: "#Status_Default",
		Driver:       drv,
	})
	defer s.Close()

	ctx := context.Background()
	if err := s.Set(ctx, presence.Activity{Level: "Tutorial"}); err != nil {
		t.Fatal(err)
	}
	if got := drv.snapshot()["steam_display"]; got != "#Status_Default" {
		t.Fatalf("default steam_display = %q, want #Status_Default", got)
	}

	// Explicit Custom["steam_display"] overrides the default.
	if err := s.Set(ctx, presence.Activity{
		Level:  "Boss Room",
		Custom: map[string]string{"steam_display": "#Status_Boss"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := drv.snapshot()["steam_display"]; got != "#Status_Boss" {
		t.Fatalf("overridden steam_display = %q, want #Status_Boss", got)
	}

	// Explicit empty Custom["steam_display"] suppresses the default.
	if err := s.Set(ctx, presence.Activity{
		Level:  "Credits",
		Custom: map[string]string{"steam_display": ""},
	}); err != nil {
		t.Fatal(err)
	}
	if _, exists := drv.snapshot()["steam_display"]; exists {
		t.Fatalf("expected steam_display to be omitted when explicitly cleared")
	}
}

func TestSteamLimitsAndValidation(t *testing.T) {
	drv := newMockSteamDriver()
	s := presence.NewSteam(presence.SteamConfig{Driver: drv})
	defer s.Close()

	ctx := context.Background()

	// Empty key rejected.
	if err := s.Set(ctx, presence.Activity{
		Custom: map[string]string{"": "v"},
	}); !errors.Is(err, presence.ErrSteamKeyInvalid) {
		t.Fatalf("empty key: got %v, want ErrSteamKeyInvalid", err)
	}

	// NUL byte in key rejected.
	if err := s.Set(ctx, presence.Activity{
		Custom: map[string]string{"bad\x00key": "v"},
	}); !errors.Is(err, presence.ErrSteamKeyInvalid) {
		t.Fatalf("NUL key: got %v, want ErrSteamKeyInvalid", err)
	}

	// Overlong key (> 63 bytes) rejected.
	if err := s.Set(ctx, presence.Activity{
		Custom: map[string]string{strings.Repeat("k", presence.SteamMaxKeyLength+1): "v"},
	}); !errors.Is(err, presence.ErrSteamKeyInvalid) {
		t.Fatalf("overlong key: got %v, want ErrSteamKeyInvalid", err)
	}

	// More than SteamMaxKeys (30) rejected.
	tooMany := make(map[string]string, presence.SteamMaxKeys+1)
	for i := range presence.SteamMaxKeys + 1 {
		tooMany[fmt.Sprintf("k%02d", i)] = "v"
	}
	if err := s.Set(ctx, presence.Activity{Custom: tooMany}); !errors.Is(err, presence.ErrSteamTooManyKeys) {
		t.Fatalf("too many keys: got %v, want ErrSteamTooManyKeys", err)
	}

	// Overlong multi-byte UTF-8 value is truncated cleanly on rune boundary and NUL bytes stripped.
	longVal := "A\x00" + strings.Repeat("星", 100) // 300+ bytes
	if err := s.Set(ctx, presence.Activity{Status: longVal}); err != nil {
		t.Fatalf("overlong value Set: %v", err)
	}
	gotStatus := drv.snapshot()["status"]
	if len(gotStatus) > presence.SteamMaxValueLength || !utf8.ValidString(gotStatus) || strings.ContainsRune(gotStatus, 0) {
		t.Fatalf("sanitized value invalid: len=%d valid=%v", len(gotStatus), utf8.ValidString(gotStatus))
	}

	// Driver rejection surfaces as error.
	drv.rejectOn = "level"
	if err := s.Set(ctx, presence.Activity{Level: "Rejected Level"}); err == nil {
		t.Fatalf("expected error when SteamDriver rejects SetRichPresence")
	}
}

func TestSteamUnavailableWithoutLibrary(t *testing.T) {
	s := presence.NewSteam(presence.SteamConfig{
		LibraryPath: "/nonexistent/path/to/libsteam_api_missing.so",
	})
	defer s.Close()

	if err := s.Set(context.Background(), presence.Activity{Level: "1-1"}); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("missing library Set: got %v, want ErrUnavailable", err)
	}
	if err := s.Clear(context.Background()); !errors.Is(err, presence.ErrUnavailable) {
		t.Fatalf("missing library Clear: got %v, want ErrUnavailable", err)
	}
}
