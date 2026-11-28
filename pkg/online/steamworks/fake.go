package steamworks

import (
	"encoding/binary"
	"fmt"
	"maps"
	"sync"
)

type fakeCallResult struct {
	expected int32
	data     []byte
	failed   bool
}

// FakeDriver is an in-process [Driver] for deterministic unit and integration
// tests without a Steamworks shared library or running Steam client. All
// methods are safe for concurrent use.
type FakeDriver struct {
	mu sync.Mutex

	// Configurable behavior before Open.
	RestartRequired bool
	InitResult      InitResult
	InitMessage     string
	InitErr         error
	Pack            int

	// Configurable environment/user state.
	AppIDValue   uint32
	SteamIDValue uint64
	Persona      string
	SteamDeck    bool
	OverlayOK    bool

	// Optional symbol resolver for tests that exercise ResolveSymbol.
	Symbols map[string]uintptr

	// Observed counters and records.
	restartCalls    int
	initCalls       int
	shutdownCalls   int
	runFrameCalls   int
	freedCallbacks  int
	overlayDialogs  []string
	overlayURLs     []string
	richPresence    map[string]string
	richClears      int
	nextAuthHandle  uint32
	authRequests    []string
	canceledTickets []uint32
	enteredText     string
	enteredTextOK   bool
	showKeyboard    int

	queue       []RawCallback
	apiResults  map[uint64]fakeCallResult
	inFlightCBs int
}

// NewFakeDriver returns a ready-to-use [FakeDriver] with sensible defaults
// (AppID 480, SteamID 76561198000000001, Persona "Player", OverlayOK true,
// CallbackPack 4).
func NewFakeDriver() *FakeDriver {
	return &FakeDriver{
		AppIDValue:   480,
		SteamIDValue: 76561198000000001,
		Persona:      "Player",
		OverlayOK:    true,
		Pack:         4,
		richPresence: make(map[string]string),
		apiResults:   make(map[uint64]fakeCallResult),
	}
}

func (f *FakeDriver) RestartAppIfNecessary(appID uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restartCalls++
	return f.RestartRequired
}

func (f *FakeDriver) Init() (InitResult, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initCalls++
	return f.InitResult, f.InitMessage, f.InitErr
}

func (f *FakeDriver) Shutdown() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalls++
	return nil
}

func (f *FakeDriver) RunFrame() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runFrameCalls++
}

func (f *FakeDriver) NextCallback() (RawCallback, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return RawCallback{}, false
	}
	cb := f.queue[0]
	f.queue = f.queue[1:]
	f.inFlightCBs++
	return cb, true
}

func (f *FakeDriver) FreeLastCallback() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.freedCallbacks++
	if f.inFlightCBs > 0 {
		f.inFlightCBs--
	}
}

func (f *FakeDriver) GetAPICallResult(call uint64, expectedCallback int32, size int32) ([]byte, bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	res, ok := f.apiResults[call]
	if !ok {
		return nil, true, false
	}
	delete(f.apiResults, call)
	if res.failed {
		return nil, true, true
	}
	if expectedCallback != 0 && res.expected != 0 && expectedCallback != res.expected {
		return nil, true, false
	}
	cp := make([]byte, len(res.data))
	copy(cp, res.data)
	return cp, false, true
}

func (f *FakeDriver) AppID() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.AppIDValue
}

func (f *FakeDriver) SteamID() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SteamIDValue
}

func (f *FakeDriver) PersonaName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Persona
}

func (f *FakeDriver) RunningOnSteamDeck() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SteamDeck
}

func (f *FakeDriver) OverlayEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.OverlayOK
}

func (f *FakeDriver) ActivateGameOverlay(dialog string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.OverlayOK {
		return false
	}
	f.overlayDialogs = append(f.overlayDialogs, dialog)
	return true
}

func (f *FakeDriver) ActivateGameOverlayToWebPage(url string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.OverlayOK {
		return false
	}
	f.overlayURLs = append(f.overlayURLs, url)
	return true
}

func (f *FakeDriver) SetRichPresence(key, value string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.richPresence == nil {
		f.richPresence = make(map[string]string)
	}
	if value == "" {
		delete(f.richPresence, key)
	} else {
		f.richPresence[key] = value
	}
	return true
}

func (f *FakeDriver) ClearRichPresence() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.richClears++
	clear(f.richPresence)
}

func (f *FakeDriver) GetAuthTicketForWebAPI(identity string) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authRequests = append(f.authRequests, identity)
	f.nextAuthHandle++
	return f.nextAuthHandle
}

func (f *FakeDriver) CancelAuthTicket(handle uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canceledTickets = append(f.canceledTickets, handle)
}

func (f *FakeDriver) ShowGamepadTextInput(desc, existing string, maxChars uint32, multiLine, password bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.showKeyboard++
	return true
}

func (f *FakeDriver) GetEnteredGamepadTextInput() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enteredText, f.enteredTextOK
}

func (f *FakeDriver) ResolveSymbol(names ...string) (uintptr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range names {
		if addr, ok := f.Symbols[name]; ok && addr != 0 {
			return addr, nil
		}
	}
	return 0, fmt.Errorf("%w: symbol %v not found in FakeDriver", ErrUnavailable, names)
}

func (f *FakeDriver) CallbackPack() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Pack > 0 {
		return f.Pack
	}
	return 4
}

// --- Test injection helpers ---

// EmitCallback queues a raw Steamworks callback for delivery on the next frame.
func (f *FakeDriver) EmitCallback(callbackID int32, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.queue = append(f.queue, RawCallback{
		CallbackID: callbackID,
		Data:       cp,
	})
}

// EmitOverlay queues a GameOverlayActivated_t callback.
func (f *FakeDriver) EmitOverlay(active, userInitiated bool, appID uint32) {
	var buf [12]byte
	if active {
		buf[0] = 1
	}
	if userInitiated {
		buf[1] = 1
	}
	binary.LittleEndian.PutUint32(buf[4:8], appID)
	f.EmitCallback(CallbackGameOverlayActivated, buf[:])
}

// EmitJoin queues a GameRichPresenceJoinRequested_t callback.
func (f *FakeDriver) EmitJoin(friendID uint64, connect string) {
	var buf [8 + 256]byte
	binary.LittleEndian.PutUint64(buf[0:8], friendID)
	copy(buf[8:8+255], connect)
	f.EmitCallback(CallbackGameRichPresenceJoinRequested, buf[:])
}

// EmitLobbyJoin queues a GameLobbyJoinRequested_t callback.
func (f *FakeDriver) EmitLobbyJoin(lobbyID, friendID uint64) {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], lobbyID)
	binary.LittleEndian.PutUint64(buf[8:16], friendID)
	f.EmitCallback(CallbackGameLobbyJoinRequested, buf[:])
}

// EmitShutdown queues a SteamShutdown_t callback.
func (f *FakeDriver) EmitShutdown() {
	f.EmitCallback(CallbackSteamShutdown, nil)
}

// EmitGamepadTextInput queues a GamepadTextInputDismissed_t callback and sets
// the text returned by GetEnteredGamepadTextInput.
func (f *FakeDriver) EmitGamepadTextInput(submitted bool, text string, appID uint32) {
	f.mu.Lock()
	f.enteredText = text
	f.enteredTextOK = submitted
	f.mu.Unlock()

	var buf [12]byte
	if submitted {
		buf[0] = 1
	}
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(text)))
	binary.LittleEndian.PutUint32(buf[8:12], appID)
	f.EmitCallback(CallbackGamepadTextInputDismissed, buf[:])
}

// EmitAuthTicket queues a GetTicketForWebApiResponse_t callback for handle.
// Pass eResult = 1 for k_EResultOK.
func (f *FakeDriver) EmitAuthTicket(handle uint32, eResult int32, ticket []byte) {
	buf := make([]byte, 12+len(ticket))
	binary.LittleEndian.PutUint32(buf[0:4], handle)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(eResult))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(len(ticket)))
	copy(buf[12:], ticket)
	f.EmitCallback(CallbackGetTicketForWebApiResponse, buf)
}

// CompleteAPICall queues a SteamAPICallCompleted_t callback for call with
// result payload data (or failed = true to simulate an I/O failure).
func (f *FakeDriver) CompleteAPICall(call uint64, callbackID int32, data []byte, failed bool) {
	f.mu.Lock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.apiResults[call] = fakeCallResult{
		expected: callbackID,
		data:     cp,
		failed:   failed,
	}
	var msg [16]byte
	binary.LittleEndian.PutUint64(msg[0:8], call)
	binary.LittleEndian.PutUint32(msg[8:12], uint32(callbackID))
	binary.LittleEndian.PutUint32(msg[12:16], uint32(len(cp)))
	f.queue = append(f.queue, RawCallback{
		CallbackID: CallbackAPICallCompleted,
		Data:       msg[:],
	})
	f.mu.Unlock()
}

// --- Test inspection helpers ---

func (f *FakeDriver) RestartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restartCalls
}

func (f *FakeDriver) InitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initCalls
}

func (f *FakeDriver) ShutdownCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdownCalls
}

func (f *FakeDriver) RunFrameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runFrameCalls
}

func (f *FakeDriver) FreedCallbackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.freedCallbacks
}

func (f *FakeDriver) InFlightCallbackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlightCBs
}

func (f *FakeDriver) RichPresence() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.richPresence)
}

func (f *FakeDriver) RichPresenceClearCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.richClears
}

func (f *FakeDriver) OverlayDialogs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.overlayDialogs))
	copy(out, f.overlayDialogs)
	return out
}

func (f *FakeDriver) OverlayURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.overlayURLs))
	copy(out, f.overlayURLs)
	return out
}

func (f *FakeDriver) AuthRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.authRequests))
	copy(out, f.authRequests)
	return out
}

func (f *FakeDriver) CanceledTickets() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uint32, len(f.canceledTickets))
	copy(out, f.canceledTickets)
	return out
}
