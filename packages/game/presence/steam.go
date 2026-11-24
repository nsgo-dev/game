package presence

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Steam rich presence limits from Steamworks' isteamfriends.h.
// Steamworks defines k_cchMaxRichPresenceKeyLength = 64 and
// k_cchMaxRichPresenceValueLength = 256 including the trailing NUL byte.
const (
	// SteamMaxKeys is the maximum number of simultaneous rich presence keys
	// Steamworks allows for the local user (k_cchMaxRichPresenceKeys).
	SteamMaxKeys = 30
	// SteamMaxKeyLength is the maximum UTF-8 byte length of a Steam rich
	// presence key (excluding the NUL terminator).
	SteamMaxKeyLength = 63
	// SteamMaxValueLength is the maximum UTF-8 byte length of a Steam rich
	// presence value (excluding the NUL terminator).
	SteamMaxValueLength = 255
)

var (
	// ErrSteamKeyInvalid reports a Steam rich presence key that is empty,
	// contains a NUL byte, or exceeds SteamMaxKeyLength bytes.
	ErrSteamKeyInvalid = errors.New("presence: invalid steam rich presence key")

	// ErrSteamTooManyKeys reports an Activity that maps to more than
	// SteamMaxKeys Steam rich presence keys.
	ErrSteamTooManyKeys = errors.New("presence: too many steam rich presence keys")
)

// steamFriendsSymbols lists the flat C API entry points that return the
// ISteamFriends singleton pointer across Steamworks SDK versions, newest first.
var steamFriendsSymbols = []string{
	"SteamAPI_SteamFriends_v018",
	"SteamAPI_SteamFriends_v017",
	"SteamAPI_SteamFriends_v015",
	"SteamFriends",
}

// SteamDriver is the low-level binding to Steamworks ISteamFriends rich
// presence functions.
//
// By default, [NewSteam] dynamically loads the Steamworks shared library via
// purego on desktop platforms (macOS, Linux, and Windows) and returns
// [ErrUnavailable] on mobile/web targets or when the library or Steam client is
// absent. Tests and custom integrations can inject a SteamDriver through
// [SteamConfig.Driver].
type SteamDriver interface {
	// SetRichPresence corresponds to ISteamFriends::SetRichPresence(key, value).
	// Passing an empty value deletes the key.
	SetRichPresence(key, value string) bool
	// ClearRichPresence corresponds to ISteamFriends::ClearRichPresence().
	ClearRichPresence()
	// Close releases any dynamic library handles owned by the driver.
	Close() error
}

// SteamConfig configures a [Steam] rich presence provider.
type SteamConfig struct {
	// LibraryPath overrides the shared library path loaded on desktop targets
	// (for example a custom path to libsteam_api.dylib, libsteam_api.so, or
	// steam_api64.dll). When empty, standard library names beside the game
	// executable and on the system loader path are tried.
	LibraryPath string

	// DisplayToken sets the default "steam_display" localization token (for
	// example "#Status_InLevel") when Activity.Custom["steam_display"] is not
	// set and the activity produces at least one rich presence key.
	DisplayToken string

	// AutoInit, when true, calls SteamAPI_Init if ISteamFriends is not yet
	// initialized when opening the Steamworks library, and calls
	// SteamAPI_Shutdown on Close if Steam initialized it. Leave false when the
	// game initializes Steamworks elsewhere.
	AutoInit bool

	// Driver overrides dynamic purego loading with a custom or mock
	// SteamDriver.
	Driver SteamDriver
}

// Steam publishes rich presence updates through Steamworks'
// ISteamFriends::SetRichPresence and ClearRichPresence flat C API using purego
// (no cgo or hand-written C).
//
// Steam maps an [Activity] into Steam rich presence keys:
//   - "status": derived from Activity.Summary() (Status, Details, Mode - Level, Level, or Mode)
//   - "level": Activity.Level when non-empty (usable by Steam localization tokens as %level%)
//   - "mode": Activity.Mode when non-empty (usable by Steam localization tokens as %mode%)
//   - "steam_player_group": Activity.PartyID when non-empty
//   - "steam_player_group_size": strconv.Itoa(Activity.PartySize) when PartySize > 0
//   - "connect": Activity.JoinSecret when non-empty
//   - "steam_display": SteamConfig.DisplayToken when configured and presence is non-empty
//   - Activity.Custom entries override or add keys; setting a key to "" omits it.
//
// Removed keys from a previous Set call are deleted via SetRichPresence(key, "")
// without clearing unchanged keys, avoiding friend-list flicker.
//
// All methods on Steam are safe for concurrent use.
type Steam struct {
	mu       sync.Mutex
	cfg      SteamConfig
	driver   SteamDriver
	lastKeys map[string]string
	closed   bool
}

// NewSteam constructs a Steam rich presence provider. If cfg.Driver is nil,
// Steam loads the Steamworks shared library lazily on the first Set or Clear
// call so games can construct Steam unconditionally on every target.
func NewSteam(cfg SteamConfig) *Steam {
	return &Steam{
		cfg:    cfg,
		driver: cfg.Driver,
	}
}

// Name returns "steam".
func (s *Steam) Name() string { return "steam" }

// Set publishes activity to Steam's ISteamFriends rich presence table.
func (s *Steam) Set(ctx context.Context, activity Activity) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	nextKeys, err := buildSteamKeys(activity, s.cfg.DisplayToken)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}
	drv, err := s.ensureDriverLocked()
	if err != nil {
		return err
	}

	// Delete keys that were set previously but are absent from nextKeys, in
	// deterministic sorted order.
	var removed []string
	for k := range s.lastKeys {
		if _, ok := nextKeys[k]; !ok {
			removed = append(removed, k)
		}
	}
	slices.Sort(removed)
	for _, k := range removed {
		if !drv.SetRichPresence(k, "") {
			return fmt.Errorf("steam SetRichPresence(%q, \"\") rejected", k)
		}
		delete(s.lastKeys, k)
	}

	// Set new or changed keys in deterministic sorted order.
	var keys []string
	for k := range nextKeys {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	if s.lastKeys == nil {
		s.lastKeys = make(map[string]string, len(nextKeys))
	}
	for _, k := range keys {
		v := nextKeys[k]
		if prev, ok := s.lastKeys[k]; ok && prev == v {
			continue
		}
		if !drv.SetRichPresence(k, v) {
			return fmt.Errorf("steam SetRichPresence(%q) rejected", k)
		}
		s.lastKeys[k] = v
	}

	return nil
}

// Clear clears all Steam rich presence keys for the local user.
func (s *Steam) Clear(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}
	drv, err := s.ensureDriverLocked()
	if err != nil {
		return err
	}
	drv.ClearRichPresence()
	clear(s.lastKeys)
	return nil
}

// Close releases the underlying SteamDriver (if opened) and marks the provider
// closed.
func (s *Steam) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	drv := s.driver
	s.driver = nil
	s.lastKeys = nil
	s.mu.Unlock()

	if drv == nil {
		return nil
	}
	return drv.Close()
}

func (s *Steam) ensureDriverLocked() (SteamDriver, error) {
	if s.driver != nil {
		return s.driver, nil
	}
	drv, err := openSteamDriver(s.cfg)
	if err != nil {
		return nil, err
	}
	s.driver = drv
	return drv, nil
}

func buildSteamKeys(a Activity, defaultDisplayToken string) (map[string]string, error) {
	keys := make(map[string]string)
	set := func(k, v string) {
		v = stripNUL(v)
		if v == "" {
			return
		}
		keys[k] = truncateUTF8(v, SteamMaxValueLength)
	}

	if summary := a.Summary(); summary != "" {
		set("status", summary)
	}
	if a.Level != "" {
		set("level", a.Level)
	}
	if a.Mode != "" {
		set("mode", a.Mode)
	}
	if a.PartyID != "" {
		set("steam_player_group", a.PartyID)
	}
	if a.PartySize > 0 {
		set("steam_player_group_size", strconv.Itoa(a.PartySize))
	}
	if a.JoinSecret != "" {
		set("connect", a.JoinSecret)
	}

	// Apply custom overrides in sorted order for deterministic validation errors.
	if len(a.Custom) > 0 {
		customKeys := make([]string, 0, len(a.Custom))
		for k := range a.Custom {
			customKeys = append(customKeys, k)
		}
		slices.Sort(customKeys)
		for _, k := range customKeys {
			if err := validateSteamKey(k); err != nil {
				return nil, err
			}
			v := stripNUL(a.Custom[k])
			if v == "" {
				delete(keys, k)
				continue
			}
			keys[k] = truncateUTF8(v, SteamMaxValueLength)
		}
	}

	if defaultDisplayToken != "" && len(keys) > 0 {
		if _, explicitlySet := a.Custom["steam_display"]; !explicitlySet {
			if _, exists := keys["steam_display"]; !exists {
				set("steam_display", defaultDisplayToken)
			}
		}
	}

	for k := range keys {
		if err := validateSteamKey(k); err != nil {
			return nil, err
		}
	}
	if len(keys) > SteamMaxKeys {
		return nil, fmt.Errorf("%w: %d exceeds limit %d", ErrSteamTooManyKeys, len(keys), SteamMaxKeys)
	}
	return keys, nil
}

func validateSteamKey(k string) error {
	if k == "" || strings.ContainsRune(k, 0) {
		return fmt.Errorf("%w: %q", ErrSteamKeyInvalid, k)
	}
	if len(k) > SteamMaxKeyLength {
		return fmt.Errorf("%w: %q length %d exceeds %d bytes", ErrSteamKeyInvalid, k, len(k), SteamMaxKeyLength)
	}
	return nil
}
