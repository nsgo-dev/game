package steamworks

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/nsgo/core"
)

// Steamworks callback IDs from Valve's Steamworks SDK headers.
const (
	// CallbackGetTicketForWebApiResponse is GetTicketForWebApiResponse_t (k_iSteamUserCallbacks + 68).
	CallbackGetTicketForWebApiResponse int32 = 168
	// CallbackGameOverlayActivated is GameOverlayActivated_t (k_iSteamFriendsCallbacks + 31).
	CallbackGameOverlayActivated int32 = 331
	// CallbackGameLobbyJoinRequested is GameLobbyJoinRequested_t (k_iSteamFriendsCallbacks + 33).
	CallbackGameLobbyJoinRequested int32 = 333
	// CallbackGameRichPresenceJoinRequested is GameRichPresenceJoinRequested_t (k_iSteamFriendsCallbacks + 37).
	CallbackGameRichPresenceJoinRequested int32 = 337
	// CallbackAPICallCompleted is SteamAPICallCompleted_t (k_iSteamUtilsCallbacks + 3).
	CallbackAPICallCompleted int32 = 703
	// CallbackSteamShutdown is SteamShutdown_t (k_iSteamUtilsCallbacks + 4).
	CallbackSteamShutdown int32 = 704
	// CallbackGamepadTextInputDismissed is GamepadTextInputDismissed_t (k_iSteamUtilsCallbacks + 14).
	CallbackGamepadTextInputDismissed int32 = 714
	// CallbackItemInstalled is ItemInstalled_t (k_iSteamUGCCallbacks + 5).
	CallbackItemInstalled int32 = 3405
)

const (
	steamResultOK          int32 = 1
	maxCachedAPICallResult       = 64
)

var (
	// ErrUnavailable is returned when Steamworks is not available on the current
	// platform, the shared library cannot be loaded, or Steam initialization fails.
	ErrUnavailable = errors.New("steamworks: unavailable")
	// ErrRestartRequired is returned by [Open] when Options.RestartIfNecessary is
	// true and SteamAPI_RestartAppIfNecessary reports that Steam is relaunching
	// the game; the caller should exit immediately.
	ErrRestartRequired = errors.New("steamworks: restart through Steam required")
	// ErrClosed is returned by operations on a [Client] after its owning scope has
	// been disposed and SteamAPI_Shutdown has run.
	ErrClosed = errors.New("steamworks: client closed")
	// ErrAppIDMismatch is returned by [Open] when a Steamworks client is already
	// open in this process with a different non-zero AppID.
	ErrAppIDMismatch = errors.New("steamworks: already initialized with a different AppID")
	// ErrDriverConflict is returned by [Open] when a Steamworks client is already
	// open in this process with a different explicit Driver.
	ErrDriverConflict = errors.New("steamworks: already initialized with a different driver")
	// ErrAuthTicket is returned when Steamworks rejects an auth ticket request.
	ErrAuthTicket = errors.New("steamworks: auth ticket request failed")
)

// InitResult matches Steamworks' ESteamAPIInitResult returned by SteamAPI_InitFlat.
type InitResult int32

const (
	// InitOK indicates SteamAPI_InitFlat succeeded.
	InitOK InitResult = 0
	// InitFailedGeneric indicates an unspecified Steamworks initialization failure.
	InitFailedGeneric InitResult = 1
	// InitNoSteamClient indicates the Steam client is not running or cannot be reached.
	InitNoSteamClient InitResult = 2
	// InitVersionMismatch indicates a Steamworks SDK/client version mismatch.
	InitVersionMismatch InitResult = 3
)

// String returns the Steamworks name for r.
func (r InitResult) String() string {
	switch r {
	case InitOK:
		return "OK"
	case InitFailedGeneric:
		return "FailedGeneric"
	case InitNoSteamClient:
		return "NoSteamClient"
	case InitVersionMismatch:
		return "VersionMismatch"
	default:
		return fmt.Sprintf("InitResult(%d)", int32(r))
	}
}

// InitError reports a SteamAPI_InitFlat failure, preserving both the ESteamAPIInitResult
// code and the human-readable SteamErrMsg text returned by Steamworks.
// It wraps [ErrUnavailable] so callers that degrade gracefully can check
// errors.Is(err, steamworks.ErrUnavailable).
type InitError struct {
	Result  InitResult
	Message string
}

func (e *InitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("steamworks: init failed (%s): %s", e.Result, e.Message)
	}
	return fmt.Sprintf("steamworks: init failed (%s)", e.Result)
}

func (e *InitError) Unwrap() error { return ErrUnavailable }

// Options configures [Open].
type Options struct {
	// AppID is the game's Steam application ID. When RestartIfNecessary is true
	// and AppID is non-zero, Open calls SteamAPI_RestartAppIfNecessary(AppID)
	// before initializing Steamworks.
	AppID uint32
	// RestartIfNecessary calls SteamAPI_RestartAppIfNecessary(AppID) before
	// SteamAPI_InitFlat when AppID is non-zero. If Steam requests a relaunch,
	// Open returns [ErrRestartRequired] without initializing Steamworks.
	RestartIfNecessary bool
	// LibraryPath overrides the shared library path loaded on desktop targets
	// (libsteam_api.dylib, libsteam_api.so, or steam_api64.dll). When empty,
	// standard locations beside the executable, Contents/Frameworks on macOS,
	// lib64 on Linux, and the system loader path are tried.
	LibraryPath string
	// Args overrides the command-line arguments scanned for +connect and
	// +connect_lobby launch joins. When nil, os.Args[1:] is scanned; pass an
	// empty non-nil slice []string{} to disable launch-argument scanning.
	Args []string
	// Driver replaces dynamic purego library loading with a custom or fake
	// [Driver] (such as [NewFakeDriver] in tests).
	Driver Driver
}

// RawCallback is one callback payload dequeued by [Driver.NextCallback].
type RawCallback struct {
	SteamUser  int32
	CallbackID int32
	Data       []byte
}

// Driver abstracts the low-level Steamworks shared library so [Client] can be
// tested deterministically without Steam installed.
type Driver interface {
	RestartAppIfNecessary(appID uint32) bool
	Init() (InitResult, string, error)
	Shutdown() error

	RunFrame()
	NextCallback() (RawCallback, bool)
	FreeLastCallback()
	GetAPICallResult(call uint64, expectedCallback int32, size int32) (data []byte, failed bool, ok bool)

	AppID() uint32
	SteamID() uint64
	PersonaName() string
	RunningOnSteamDeck() bool
	OverlayEnabled() bool
	ActivateGameOverlay(dialog string) bool
	ActivateGameOverlayToWebPage(url string) bool

	SetRichPresence(key, value string) bool
	ClearRichPresence()

	GetAuthTicketForWebAPI(identity string) uint32
	CancelAuthTicket(handle uint32)

	ShowGamepadTextInput(desc, existing string, maxChars uint32, multiLine, password bool) bool
	GetEnteredGamepadTextInput() (string, bool)

	ResolveSymbol(names ...string) (uintptr, error)
	CallbackPack() int
}

// OverlayEvent is delivered when the Steam Overlay opens or closes
// (GameOverlayActivated_t).
type OverlayEvent struct {
	Active        bool
	UserInitiated bool
	AppID         uint32
}

// JoinEvent is delivered when a friend invite or Join Game action triggers
// GameRichPresenceJoinRequested_t, or when the game was launched with +connect.
type JoinEvent struct {
	FriendID   uint64
	Connect    string
	FromLaunch bool
}

// LobbyJoinEvent is delivered when a friend lobby invite triggers
// GameLobbyJoinRequested_t, or when the game was launched with +connect_lobby.
type LobbyJoinEvent struct {
	LobbyID    uint64
	FriendID   uint64
	FromLaunch bool
}

// ShutdownEvent is delivered when the Steam client notifies the game that it is
// shutting down (SteamShutdown_t).
type ShutdownEvent struct{}

// GamepadTextInputEvent is delivered when the Steam Big Picture / Steam Deck
// on-screen keyboard closes (GamepadTextInputDismissed_t).
type GamepadTextInputEvent struct {
	Submitted bool
	Text      string
	AppID     uint32
}

// AuthTicket holds a Steam Web API session ticket returned by
// [Client.AuthTicketForWebAPI] for server-side verification with
// ISteamUserAuth/AuthenticateUserTicket.
type AuthTicket struct {
	Handle uint32
	Bytes  []byte
}

// Hex returns the uppercase hexadecimal encoding expected by Steam's
// ISteamUserAuth/AuthenticateUserTicket Web API endpoint.
func (t AuthTicket) Hex() string {
	return strings.ToUpper(hex.EncodeToString(t.Bytes))
}

type apiCallOutcome struct {
	callbackID int32
	data       []byte
	err        error
}

type authTicketOutcome struct {
	ticket AuthTicket
	err    error
}

type scopeAttachment struct {
	id uint64
	cx *core.Scope
}

type callbackSub[T any] struct {
	id uint64
	fn func(T)
}

type rawCallbackSub struct {
	id         uint64
	callbackID int32
	fn         func([]byte)
}

var (
	processMu    sync.Mutex
	activeClient *Client
	nextAttachID uint64
	nextSubID    uint64
)

// Client is the process-wide Steamworks client owned by one or more NSGo
// scopes. Obtain it via [Open] or read the currently active client via
// [Current].
//
// Lock ordering: processMu -> c.drvMu -> c.mu. User event handlers are never
// called while holding any lock.
type Client struct {
	drvMu sync.Mutex
	drv   Driver

	mu                 sync.Mutex
	closed             bool
	appID              uint32
	launchConnect      string
	launchJoinPending  bool
	launchLobbyID      uint64
	launchLobbyPending bool
	overlayActive      bool

	scopes      []scopeAttachment
	pumpScopeID uint64
	cancelPump  func()

	overlaySubs []callbackSub[OverlayEvent]
	joinSubs    []callbackSub[JoinEvent]
	lobbySubs   []callbackSub[LobbyJoinEvent]
	shutSubs    []callbackSub[ShutdownEvent]
	textSubs    []callbackSub[GamepadTextInputEvent]
	rawSubs     []rawCallbackSub

	callWaiters    map[uint64]chan apiCallOutcome
	completedCalls map[uint64]apiCallOutcome
	completedOrder []uint64

	authWaiters map[uint32]chan authTicketOutcome
}

// Current returns the currently open process-wide Steamworks client, or nil if
// no scope holds an open client.
func Current() *Client {
	processMu.Lock()
	defer processMu.Unlock()
	return activeClient
}

// Open initializes or attaches to the process-wide Steamworks client for cx.
//
// Only one SteamAPI_InitFlat runs per process: if a client is already open,
// Open attaches cx to the existing client (refusing a conflicting non-zero
// AppID or explicit Driver) and reference-counts shutdown. Manual dispatch runs
// once per display frame on the engine goroutine until the last attached scope
// is disposed, at which point SteamAPI_Shutdown runs.
func Open(cx *core.Scope, opts Options) (*Client, error) {
	if cx == nil {
		return nil, errors.New("steamworks: nil scope")
	}

	args := opts.Args
	if args == nil && len(os.Args) > 1 {
		args = os.Args[1:]
	}
	launchConnect, _ := ParseConnectArg(args)
	launchLobby, _ := ParseConnectLobbyArg(args)

	processMu.Lock()
	if activeClient != nil {
		c := activeClient
		c.mu.Lock()
		if !c.closed {
			if opts.AppID != 0 && c.appID != 0 && opts.AppID != c.appID {
				c.mu.Unlock()
				processMu.Unlock()
				return nil, fmt.Errorf("%w: active=%d requested=%d", ErrAppIDMismatch, c.appID, opts.AppID)
			}
			if opts.Driver != nil && opts.Driver != c.drv {
				c.mu.Unlock()
				processMu.Unlock()
				return nil, ErrDriverConflict
			}
			if opts.AppID != 0 && c.appID == 0 {
				c.appID = opts.AppID
			}
			attachID := c.attachScopeLocked(cx)
			c.mu.Unlock()
			processMu.Unlock()
			cx.Cleanup(func() {
				c.detachScope(attachID)
			})
			return c, nil
		}
		c.mu.Unlock()
	}

	drv := opts.Driver
	if drv == nil {
		var err error
		drv, err = openPlatformDriver(opts)
		if err != nil {
			processMu.Unlock()
			return nil, err
		}
	}

	if opts.RestartIfNecessary && opts.AppID != 0 {
		if drv.RestartAppIfNecessary(opts.AppID) {
			if opts.Driver == nil {
				_ = drv.Shutdown()
			}
			processMu.Unlock()
			return nil, ErrRestartRequired
		}
	}

	res, msg, initErr := drv.Init()
	if initErr != nil {
		if opts.Driver == nil {
			_ = drv.Shutdown()
		}
		processMu.Unlock()
		return nil, initErr
	}
	if res != InitOK {
		if opts.Driver == nil {
			_ = drv.Shutdown()
		}
		processMu.Unlock()
		return nil, &InitError{Result: res, Message: msg}
	}

	appID := opts.AppID
	if detected := drv.AppID(); detected != 0 {
		appID = detected
	}

	c := &Client{
		drv:                drv,
		appID:              appID,
		launchConnect:      launchConnect,
		launchJoinPending:  launchConnect != "",
		launchLobbyID:      launchLobby,
		launchLobbyPending: launchLobby != 0,
		callWaiters:        make(map[uint64]chan apiCallOutcome),
		completedCalls:     make(map[uint64]apiCallOutcome),
		authWaiters:        make(map[uint32]chan authTicketOutcome),
	}

	c.mu.Lock()
	attachID := c.attachScopeLocked(cx)
	c.startPumpLocked(attachID, cx)
	c.mu.Unlock()

	activeClient = c
	processMu.Unlock()

	cx.Cleanup(func() {
		c.detachScope(attachID)
	})
	return c, nil
}

func (c *Client) attachScopeLocked(cx *core.Scope) uint64 {
	nextAttachID++
	id := nextAttachID
	c.scopes = append(c.scopes, scopeAttachment{id: id, cx: cx})
	return id
}

func (c *Client) startPumpLocked(attachID uint64, cx *core.Scope) {
	if c.cancelPump != nil {
		c.cancelPump()
		c.cancelPump = nil
	}
	c.pumpScopeID = attachID
	var frame func(time.Time)
	frame = func(time.Time) {
		c.mu.Lock()
		if c.closed || c.pumpScopeID != attachID {
			c.mu.Unlock()
			return
		}
		c.cancelPump = cx.RequestAnimationFrame(frame)
		c.mu.Unlock()
		c.Pump()
	}
	cx.Run(func() {
		c.cancelPump = cx.RequestAnimationFrame(frame)
	})
}

func (c *Client) detachScope(attachID uint64) {
	processMu.Lock()
	c.drvMu.Lock()
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		c.drvMu.Unlock()
		processMu.Unlock()
		return
	}

	for i, s := range c.scopes {
		if s.id == attachID {
			c.scopes = append(c.scopes[:i], c.scopes[i+1:]...)
			break
		}
	}

	if len(c.scopes) > 0 {
		// Hand off the single per-frame pump if the disposed scope owned it.
		if c.pumpScopeID == attachID {
			next := c.scopes[0]
			c.startPumpLocked(next.id, next.cx)
		}
		c.mu.Unlock()
		c.drvMu.Unlock()
		processMu.Unlock()
		return
	}

	// Last owning scope disposed: stop the pump, fail pending waiters, and shut down.
	c.closed = true
	if c.cancelPump != nil {
		c.cancelPump()
		c.cancelPump = nil
	}
	c.pumpScopeID = 0
	for call, ch := range c.callWaiters {
		ch <- apiCallOutcome{err: ErrClosed}
		delete(c.callWaiters, call)
	}
	for handle, ch := range c.authWaiters {
		ch <- authTicketOutcome{err: ErrClosed}
		delete(c.authWaiters, handle)
	}
	clear(c.completedCalls)
	c.completedOrder = nil
	c.overlaySubs = nil
	c.joinSubs = nil
	c.lobbySubs = nil
	c.shutSubs = nil
	c.textSubs = nil
	c.rawSubs = nil

	if activeClient == c {
		activeClient = nil
	}
	c.mu.Unlock()

	_ = c.drv.Shutdown()
	c.drvMu.Unlock()
	processMu.Unlock()
}

// Closed reports whether the client's owning scopes have all been disposed.
func (c *Client) Closed() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// AppID returns the Steam application ID reported by Steamworks (or
// Options.AppID when Steamworks reports 0).
func (c *Client) AppID() uint32 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appID
}

// SteamID returns the signed-in user's 64-bit SteamID, or 0 if closed.
func (c *Client) SteamID() uint64 {
	if c == nil {
		return 0
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return 0
	}
	return c.drv.SteamID()
}

// PersonaName returns the signed-in user's Steam persona name, or "" if closed.
func (c *Client) PersonaName() string {
	if c == nil {
		return ""
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return ""
	}
	return c.drv.PersonaName()
}

// RunningOnSteamDeck reports whether Steamworks reports the game is running on
// a Steam Deck (ISteamUtils::IsSteamRunningOnSteamDeck).
func (c *Client) RunningOnSteamDeck() bool {
	if c == nil {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.RunningOnSteamDeck()
}

// OverlayActive reports whether the Steam Overlay is currently open as last
// reported by GameOverlayActivated_t.
func (c *Client) OverlayActive() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.overlayActive
}

// OverlayEnabled reports whether the Steam Overlay is enabled for this session
// (ISteamUtils::IsOverlayEnabled).
func (c *Client) OverlayEnabled() bool {
	if c == nil {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.OverlayEnabled()
}

// OpenOverlay activates the Steam Overlay to the specified dialog (for example
// "friends", "community", "players", "settings", "officialgamegroup", "stats",
// or "achievements", or "" for the default overlay).
func (c *Client) OpenOverlay(dialog string) bool {
	if c == nil {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.ActivateGameOverlay(dialog)
}

// OpenOverlayURL activates the Steam Overlay web browser directly to url.
func (c *Client) OpenOverlayURL(url string) bool {
	if c == nil || url == "" {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.ActivateGameOverlayToWebPage(url)
}

// LaunchConnect returns the +connect launch argument value captured when the
// client opened, or "" if none was present.
func (c *Client) LaunchConnect() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launchConnect
}

// SetRichPresence calls ISteamFriends::SetRichPresence on the shared client.
func (c *Client) SetRichPresence(key, value string) bool {
	if c == nil {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.SetRichPresence(key, value)
}

// ClearRichPresence calls ISteamFriends::ClearRichPresence on the shared client.
func (c *Client) ClearRichPresence() {
	if c == nil {
		return
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return
	}
	c.drv.ClearRichPresence()
}

// ShowGamepadTextInput opens the Steam Big Picture / Steam Deck on-screen
// keyboard. When the player submits or cancels, [OnGamepadTextInput] fires.
func (c *Client) ShowGamepadTextInput(desc, existing string, maxChars uint32, multiLine, password bool) bool {
	if c == nil {
		return false
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return false
	}
	return c.drv.ShowGamepadTextInput(desc, existing, maxChars, multiLine, password)
}

// AuthTicketForWebAPI requests a Steam session ticket for server-side
// verification (ISteamUserAuth/AuthenticateUserTicket) and waits for
// GetTicketForWebApiResponse_t. Pass the same identity string your backend
// verifies (empty string is allowed by Steamworks).
func (c *Client) AuthTicketForWebAPI(ctx context.Context, identity string) (AuthTicket, error) {
	if c == nil {
		return AuthTicket{}, ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return AuthTicket{}, err
	}

	c.drvMu.Lock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.drvMu.Unlock()
		return AuthTicket{}, ErrClosed
	}
	c.mu.Unlock()

	handle := c.drv.GetAuthTicketForWebAPI(identity)
	if handle == 0 {
		c.drvMu.Unlock()
		return AuthTicket{}, fmt.Errorf("%w: GetAuthTicketForWebApi returned invalid handle", ErrAuthTicket)
	}

	ch := make(chan authTicketOutcome, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.drvMu.Unlock()
		return AuthTicket{}, ErrClosed
	}
	c.authWaiters[handle] = ch
	c.mu.Unlock()
	c.drvMu.Unlock()

	select {
	case out := <-ch:
		return out.ticket, out.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.authWaiters, handle)
		c.mu.Unlock()
		c.CancelAuthTicket(handle)
		return AuthTicket{}, ctx.Err()
	}
}

// CancelAuthTicket cancels a previously issued auth ticket handle.
func (c *Client) CancelAuthTicket(handle uint32) {
	if c == nil || handle == 0 {
		return
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return
	}
	c.drv.CancelAuthTicket(handle)
}

// ResolveSymbol resolves the first available symbol name from the shared
// Steamworks library handle loaded by this client. Subsystem packages (such as
// modding.SteamWorkshop) use this instead of opening libsteam_api a second time.
func (c *Client) ResolveSymbol(names ...string) (uintptr, error) {
	if c == nil {
		return 0, ErrUnavailable
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if c.Closed() {
		return 0, ErrClosed
	}
	return c.drv.ResolveSymbol(names...)
}

// CallbackPack returns the struct packing alignment used by Steamworks callback
// structures on this platform (4 on macOS/Linux, 8 on Windows).
func (c *Client) CallbackPack() int {
	if c == nil {
		return 4
	}
	c.drvMu.Lock()
	defer c.drvMu.Unlock()
	if p := c.drv.CallbackPack(); p > 0 {
		return p
	}
	return 4
}

// AwaitCall waits for a SteamAPICall_t asynchronous result to arrive via the
// shared client's manual-dispatch callback pump, returning the raw callback
// payload bytes.
func (c *Client) AwaitCall(ctx context.Context, call uint64, expectedCallback int32) ([]byte, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if call == 0 {
		return nil, errors.New("steamworks: invalid SteamAPICall_t handle")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if done, ok := c.completedCalls[call]; ok {
		delete(c.completedCalls, call)
		c.mu.Unlock()
		if done.err != nil {
			return nil, done.err
		}
		if expectedCallback != 0 && done.callbackID != 0 && done.callbackID != expectedCallback {
			return nil, fmt.Errorf("steamworks: API call %d returned callback %d, want %d", call, done.callbackID, expectedCallback)
		}
		return done.data, nil
	}
	ch := make(chan apiCallOutcome, 1)
	c.callWaiters[call] = ch
	c.mu.Unlock()

	select {
	case out := <-ch:
		if out.err != nil {
			return nil, out.err
		}
		if expectedCallback != 0 && out.callbackID != 0 && out.callbackID != expectedCallback {
			return nil, fmt.Errorf("steamworks: API call %d returned callback %d, want %d", call, out.callbackID, expectedCallback)
		}
		return out.data, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.callWaiters, call)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// OnOverlay registers fn to run on the engine goroutine whenever the Steam
// Overlay opens or closes. The returned function unregisters fn.
func (c *Client) OnOverlay(fn func(OverlayEvent)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.overlaySubs = append(c.overlaySubs, callbackSub[OverlayEvent]{id: id, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.overlaySubs {
			if s.id == id {
				c.overlaySubs = append(c.overlaySubs[:i], c.overlaySubs[i+1:]...)
				return
			}
		}
	}
}

// OnJoin registers fn to run on the engine goroutine when a friend join is
// requested (GameRichPresenceJoinRequested_t) or when the game was launched
// with +connect. A pending +connect launch join is delivered on the next pumped
// frame once at least one handler is registered.
func (c *Client) OnJoin(fn func(JoinEvent)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.joinSubs = append(c.joinSubs, callbackSub[JoinEvent]{id: id, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.joinSubs {
			if s.id == id {
				c.joinSubs = append(c.joinSubs[:i], c.joinSubs[i+1:]...)
				return
			}
		}
	}
}

// OnLobbyJoin registers fn to run on the engine goroutine when a lobby invite
// is accepted (GameLobbyJoinRequested_t) or when the game was launched with
// +connect_lobby.
func (c *Client) OnLobbyJoin(fn func(LobbyJoinEvent)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.lobbySubs = append(c.lobbySubs, callbackSub[LobbyJoinEvent]{id: id, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.lobbySubs {
			if s.id == id {
				c.lobbySubs = append(c.lobbySubs[:i], c.lobbySubs[i+1:]...)
				return
			}
		}
	}
}

// OnShutdown registers fn to run on the engine goroutine when Steam broadcasts
// SteamShutdown_t.
func (c *Client) OnShutdown(fn func(ShutdownEvent)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.shutSubs = append(c.shutSubs, callbackSub[ShutdownEvent]{id: id, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.shutSubs {
			if s.id == id {
				c.shutSubs = append(c.shutSubs[:i], c.shutSubs[i+1:]...)
				return
			}
		}
	}
}

// OnGamepadTextInput registers fn to run on the engine goroutine when the Steam
// Big Picture / Steam Deck on-screen keyboard closes.
func (c *Client) OnGamepadTextInput(fn func(GamepadTextInputEvent)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.textSubs = append(c.textSubs, callbackSub[GamepadTextInputEvent]{id: id, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.textSubs {
			if s.id == id {
				c.textSubs = append(c.textSubs[:i], c.textSubs[i+1:]...)
				return
			}
		}
	}
}

// OnCallback registers fn to run on the engine goroutine whenever Steamworks
// broadcasts a callback with the given callbackID. Subsystem packages (such as
// Workshop for CallbackItemInstalled) use this to receive broadcast callbacks
// from the shared manual-dispatch pump.
func (c *Client) OnCallback(callbackID int32, fn func(data []byte)) (stop func()) {
	if c == nil || fn == nil {
		return func() {}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return func() {}
	}
	nextSubID++
	id := nextSubID
	c.rawSubs = append(c.rawSubs, rawCallbackSub{id: id, callbackID: callbackID, fn: fn})
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, s := range c.rawSubs {
			if s.id == id {
				c.rawSubs = append(c.rawSubs[:i], c.rawSubs[i+1:]...)
				return
			}
		}
	}
}

// PauseOnOverlay pauses loop while the Steam Overlay is open and resumes it
// when the overlay closes, preserving any pause that was already active before
// the overlay opened (such as an open pause menu). Calling the returned stop
// function unregisters the listener and restores the loop if this helper had
// paused it.
func (c *Client) PauseOnOverlay(loop *game.Loop) (stop func()) {
	if c == nil || loop == nil {
		return func() {}
	}
	var (
		mu         sync.Mutex
		pausedByUs bool
		stopped    bool
	)
	apply := func(active bool) {
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		if active {
			if !pausedByUs && !loop.Paused {
				pausedByUs = true
				mu.Unlock()
				loop.SetPaused(true)
				return
			}
			mu.Unlock()
			return
		}
		if pausedByUs {
			pausedByUs = false
			mu.Unlock()
			loop.SetPaused(false)
			return
		}
		mu.Unlock()
	}

	if c.OverlayActive() {
		apply(true)
	}
	unsub := c.OnOverlay(func(ev OverlayEvent) {
		apply(ev.Active)
	})
	return func() {
		unsub()
		mu.Lock()
		if stopped {
			mu.Unlock()
			return
		}
		stopped = true
		restore := pausedByUs
		pausedByUs = false
		mu.Unlock()
		if restore {
			loop.SetPaused(false)
		}
	}
}

type drainedEvent struct {
	callbackID int32
	rawData    []byte
	overlay    *OverlayEvent
	join       *JoinEvent
	lobbyJoin  *LobbyJoinEvent
	shutdown   bool
	textInput  *GamepadTextInputEvent
}

// Pump runs one manual-dispatch frame: calls SteamAPI_ManualDispatch_RunFrame,
// drains all queued callbacks, resolves completed SteamAPICall_t and Web API
// auth tickets, and delivers typed events to registered listeners.
//
// [Open] runs Pump automatically once per display frame on the engine
// goroutine; callers normally do not need to invoke it directly.
func (c *Client) Pump() {
	if c == nil {
		return
	}

	c.drvMu.Lock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.drvMu.Unlock()
		return
	}
	c.mu.Unlock()

	c.drv.RunFrame()

	var events []drainedEvent
	type resolvedCall struct {
		call    uint64
		outcome apiCallOutcome
	}
	var calls []resolvedCall
	type resolvedAuth struct {
		handle  uint32
		outcome authTicketOutcome
	}
	var auths []resolvedAuth

	for {
		cb, ok := c.drv.NextCallback()
		if !ok {
			break
		}
		if cb.CallbackID == CallbackAPICallCompleted {
			callID, expectedCB, paramSize, valid := decodeAPICallCompleted(cb.Data)
			if valid {
				data, failed, got := c.drv.GetAPICallResult(callID, expectedCB, int32(paramSize))
				c.drv.FreeLastCallback()
				var out apiCallOutcome
				out.callbackID = expectedCB
				if !got || failed {
					out.err = fmt.Errorf("steamworks: API call %d (callback %d) failed", callID, expectedCB)
				} else {
					out.data = data
				}
				calls = append(calls, resolvedCall{call: callID, outcome: out})
			} else {
				c.drv.FreeLastCallback()
			}
			continue
		}

		c.drv.FreeLastCallback()

		switch cb.CallbackID {
		case CallbackGameOverlayActivated:
			ev := decodeOverlayActivated(cb.Data)
			events = append(events, drainedEvent{
				callbackID: cb.CallbackID,
				rawData:    cb.Data,
				overlay:    &ev,
			})
		case CallbackGameRichPresenceJoinRequested:
			if ev, ok := decodeRichPresenceJoin(cb.Data); ok {
				events = append(events, drainedEvent{
					callbackID: cb.CallbackID,
					rawData:    cb.Data,
					join:       &ev,
				})
			}
		case CallbackGameLobbyJoinRequested:
			if ev, ok := decodeLobbyJoin(cb.Data); ok {
				events = append(events, drainedEvent{
					callbackID: cb.CallbackID,
					rawData:    cb.Data,
					lobbyJoin:  &ev,
				})
			}
		case CallbackSteamShutdown:
			events = append(events, drainedEvent{
				callbackID: cb.CallbackID,
				rawData:    cb.Data,
				shutdown:   true,
			})
		case CallbackGamepadTextInputDismissed:
			ev := decodeGamepadTextInputDismissed(cb.Data)
			if ev.Submitted {
				if text, ok := c.drv.GetEnteredGamepadTextInput(); ok {
					ev.Text = text
				}
			}
			events = append(events, drainedEvent{
				callbackID: cb.CallbackID,
				rawData:    cb.Data,
				textInput:  &ev,
			})
		case CallbackGetTicketForWebApiResponse:
			if handle, res, ticketBytes, ok := decodeTicketForWebAPI(cb.Data); ok {
				var out authTicketOutcome
				if res != steamResultOK {
					out.err = fmt.Errorf("%w (EResult=%d)", ErrAuthTicket, res)
				} else {
					out.ticket = AuthTicket{Handle: handle, Bytes: ticketBytes}
				}
				auths = append(auths, resolvedAuth{handle: handle, outcome: out})
			}
			events = append(events, drainedEvent{
				callbackID: cb.CallbackID,
				rawData:    cb.Data,
			})
		default:
			events = append(events, drainedEvent{
				callbackID: cb.CallbackID,
				rawData:    cb.Data,
			})
		}
	}
	c.drvMu.Unlock()

	// Deliver call completions, auth tickets, update overlay state, and check pending launch joins under c.mu.
	var (
		launchJoin  *JoinEvent
		launchLobby *LobbyJoinEvent
	)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	for _, rc := range calls {
		if ch, ok := c.callWaiters[rc.call]; ok {
			delete(c.callWaiters, rc.call)
			ch <- rc.outcome
		} else {
			if _, exists := c.completedCalls[rc.call]; !exists {
				if len(c.completedOrder) >= maxCachedAPICallResult {
					oldest := c.completedOrder[0]
					c.completedOrder = c.completedOrder[1:]
					delete(c.completedCalls, oldest)
				}
				c.completedOrder = append(c.completedOrder, rc.call)
			}
			c.completedCalls[rc.call] = rc.outcome
		}
	}
	for _, ra := range auths {
		if ch, ok := c.authWaiters[ra.handle]; ok {
			delete(c.authWaiters, ra.handle)
			ch <- ra.outcome
		}
	}
	if c.launchJoinPending && len(c.joinSubs) > 0 {
		c.launchJoinPending = false
		ev := JoinEvent{Connect: c.launchConnect, FromLaunch: true}
		launchJoin = &ev
	}
	if c.launchLobbyPending && len(c.lobbySubs) > 0 {
		c.launchLobbyPending = false
		ev := LobbyJoinEvent{LobbyID: c.launchLobbyID, FromLaunch: true}
		launchLobby = &ev
	}
	c.mu.Unlock()

	if launchJoin != nil {
		for _, sub := range c.snapshotJoinSubs() {
			sub.fn(*launchJoin)
		}
	}
	if launchLobby != nil {
		for _, sub := range c.snapshotLobbySubs() {
			sub.fn(*launchLobby)
		}
	}

	for _, ev := range events {
		if ev.overlay != nil {
			c.mu.Lock()
			c.overlayActive = ev.overlay.Active
			c.mu.Unlock()
			for _, sub := range c.snapshotOverlaySubs() {
				sub.fn(*ev.overlay)
			}
		}
		if ev.join != nil {
			for _, sub := range c.snapshotJoinSubs() {
				sub.fn(*ev.join)
			}
		}
		if ev.lobbyJoin != nil {
			for _, sub := range c.snapshotLobbySubs() {
				sub.fn(*ev.lobbyJoin)
			}
		}
		if ev.shutdown {
			for _, sub := range c.snapshotShutdownSubs() {
				sub.fn(ShutdownEvent{})
			}
		}
		if ev.textInput != nil {
			for _, sub := range c.snapshotTextSubs() {
				sub.fn(*ev.textInput)
			}
		}
		for _, sub := range c.snapshotRawSubs(ev.callbackID) {
			cp := make([]byte, len(ev.rawData))
			copy(cp, ev.rawData)
			sub.fn(cp)
		}
	}
}

func (c *Client) snapshotOverlaySubs() []callbackSub[OverlayEvent] {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]callbackSub[OverlayEvent], len(c.overlaySubs))
	copy(out, c.overlaySubs)
	return out
}

func (c *Client) snapshotJoinSubs() []callbackSub[JoinEvent] {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]callbackSub[JoinEvent], len(c.joinSubs))
	copy(out, c.joinSubs)
	return out
}

func (c *Client) snapshotLobbySubs() []callbackSub[LobbyJoinEvent] {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]callbackSub[LobbyJoinEvent], len(c.lobbySubs))
	copy(out, c.lobbySubs)
	return out
}

func (c *Client) snapshotShutdownSubs() []callbackSub[ShutdownEvent] {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]callbackSub[ShutdownEvent], len(c.shutSubs))
	copy(out, c.shutSubs)
	return out
}

func (c *Client) snapshotTextSubs() []callbackSub[GamepadTextInputEvent] {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]callbackSub[GamepadTextInputEvent], len(c.textSubs))
	copy(out, c.textSubs)
	return out
}

func (c *Client) snapshotRawSubs(callbackID int32) []rawCallbackSub {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []rawCallbackSub
	for _, s := range c.rawSubs {
		if s.callbackID == callbackID {
			out = append(out, s)
		}
	}
	return out
}

// ParseConnectArg extracts the +connect launch argument value passed by Steam
// when the game is started from a friend's "Join Game" action. It recognizes
// `["+connect", "<value>"]`, `["+connect=<value>"]`, and `["+connect <value>"]`.
func ParseConnectArg(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "+connect":
			if i+1 < len(args) {
				v := strings.TrimSpace(args[i+1])
				if v != "" {
					return v, true
				}
			}
		case strings.HasPrefix(arg, "+connect="):
			v := strings.TrimSpace(strings.TrimPrefix(arg, "+connect="))
			if v != "" {
				return v, true
			}
		case strings.HasPrefix(arg, "+connect "):
			v := strings.TrimSpace(strings.TrimPrefix(arg, "+connect "))
			if v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// ParseConnectLobbyArg extracts the 64-bit Steam lobby ID passed by Steam via
// +connect_lobby <lobbyID> or +connect_lobby=<lobbyID>.
func ParseConnectLobbyArg(args []string) (uint64, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var raw string
		switch {
		case arg == "+connect_lobby":
			if i+1 < len(args) {
				raw = strings.TrimSpace(args[i+1])
			}
		case strings.HasPrefix(arg, "+connect_lobby="):
			raw = strings.TrimSpace(strings.TrimPrefix(arg, "+connect_lobby="))
		case strings.HasPrefix(arg, "+connect_lobby "):
			raw = strings.TrimSpace(strings.TrimPrefix(arg, "+connect_lobby "))
		}
		if raw != "" {
			if id, err := strconv.ParseUint(raw, 10, 64); err == nil && id != 0 {
				return id, true
			}
		}
	}
	return 0, false
}

func decodeAPICallCompleted(data []byte) (call uint64, callbackID int32, paramSize uint32, ok bool) {
	if len(data) < 16 {
		return 0, 0, 0, false
	}
	call = binary.LittleEndian.Uint64(data[0:8])
	callbackID = int32(binary.LittleEndian.Uint32(data[8:12]))
	paramSize = binary.LittleEndian.Uint32(data[12:16])
	return call, callbackID, paramSize, true
}

func decodeOverlayActivated(data []byte) OverlayEvent {
	var ev OverlayEvent
	if len(data) >= 1 {
		ev.Active = data[0] != 0
	}
	if len(data) >= 2 {
		ev.UserInitiated = data[1] != 0
	}
	if len(data) >= 8 {
		ev.AppID = binary.LittleEndian.Uint32(data[4:8])
	}
	return ev
}

func decodeRichPresenceJoin(data []byte) (JoinEvent, bool) {
	if len(data) < 9 {
		return JoinEvent{}, false
	}
	friendID := binary.LittleEndian.Uint64(data[0:8])
	end := min(len(data), 8+256)
	connect := cString(data[8:end])
	return JoinEvent{FriendID: friendID, Connect: connect}, true
}

func decodeLobbyJoin(data []byte) (LobbyJoinEvent, bool) {
	if len(data) < 16 {
		return LobbyJoinEvent{}, false
	}
	lobbyID := binary.LittleEndian.Uint64(data[0:8])
	friendID := binary.LittleEndian.Uint64(data[8:16])
	return LobbyJoinEvent{LobbyID: lobbyID, FriendID: friendID}, true
}

func decodeGamepadTextInputDismissed(data []byte) GamepadTextInputEvent {
	var ev GamepadTextInputEvent
	if len(data) >= 1 {
		ev.Submitted = data[0] != 0
	}
	if len(data) >= 12 {
		ev.AppID = binary.LittleEndian.Uint32(data[8:12])
	}
	return ev
}

func decodeTicketForWebAPI(data []byte) (handle uint32, eResult int32, ticket []byte, ok bool) {
	if len(data) < 12 {
		return 0, 0, nil, false
	}
	handle = binary.LittleEndian.Uint32(data[0:4])
	eResult = int32(binary.LittleEndian.Uint32(data[4:8]))
	size := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	if size < 0 || 12+size > len(data) {
		return handle, eResult, nil, true
	}
	out := make([]byte, size)
	copy(out, data[12:12+size])
	return handle, eResult, out, true
}

func cString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx >= 0 {
		return string(b[:idx])
	}
	return string(b)
}
