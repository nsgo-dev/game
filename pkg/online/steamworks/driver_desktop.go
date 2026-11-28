//go:build (darwin && !ios) || (linux && !android) || windows

package steamworks

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// callbackMsg mirrors Steamworks' CallbackMsg_t layout:
//
//	struct CallbackMsg_t {
//	    HSteamUser m_hSteamUser; // int32
//	    int        m_iCallback;  // int32
//	    uint8     *m_pubParam;   // pointer (8-byte aligned on 64-bit, 4-byte on 32-bit)
//	    int        m_cubParam;   // int32
//	};
type callbackMsg struct {
	steamUser int32
	callback  int32
	param     *byte
	paramSize int32
}

var (
	steamUtilsAccessors = []string{
		"SteamAPI_SteamUtils_v010",
		"SteamAPI_SteamUtils_v009",
		"SteamUtils",
	}
	steamUserAccessors = []string{
		"SteamAPI_SteamUser_v023",
		"SteamAPI_SteamUser_v022",
		"SteamAPI_SteamUser_v021",
		"SteamAPI_SteamUser_v020",
		"SteamAPI_SteamUser_v019",
		"SteamUser",
	}
	steamFriendsAccessors = []string{
		"SteamAPI_SteamFriends_v018",
		"SteamAPI_SteamFriends_v017",
		"SteamAPI_SteamFriends_v015",
		"SteamFriends",
	}
)

type desktopDriver struct {
	lib         *nativeLibrary
	initialized bool
	pipe        int32

	restartAppIfNecessary func(appID uint32) bool
	initFlat              func(outErrMsg *byte) int32
	initLegacy            func() bool
	shutdown              func()
	getHSteamPipe         func() int32

	manualDispatchInit             func()
	manualDispatchRunFrame         func(pipe int32)
	manualDispatchGetNextCallback  func(pipe int32, msg *callbackMsg) bool
	manualDispatchFreeLastCallback func(pipe int32)
	manualDispatchGetAPICallResult func(pipe int32, call uint64, callback *byte, size int32, expected int32, failed *bool) bool

	getUtils   func() uintptr
	getUser    func() uintptr
	getFriends func() uintptr

	getAppID                func(self uintptr) uint32
	isSteamDeck             func(self uintptr) bool
	isOverlayEnabled        func(self uintptr) bool
	showGamepadTextInput    func(self uintptr, inputMode, lineMode int32, desc *byte, charMax uint32, existing *byte) bool
	getEnteredTextLen       func(self uintptr) uint32
	getEnteredTextInput     func(self uintptr, pchText *byte, cchText uint32) bool
	getSteamID              func(self uintptr) uint64
	getAuthTicketForWebAPI  func(self uintptr, identity *byte) uint32
	cancelAuthTicket        func(self uintptr, handle uint32)
	getPersonaName          func(self uintptr) *byte
	setRichPresence         func(self uintptr, key, value *byte) bool
	clearRichPresence       func(self uintptr)
	activateOverlay         func(self uintptr, dialog *byte)
	activateOverlayToWebURL func(self uintptr, url *byte, mode int32)
}

func openPlatformDriver(opts Options) (Driver, error) {
	var (
		lib     *nativeLibrary
		lastErr error
	)
	for _, candidate := range libraryCandidates(opts.LibraryPath) {
		l, err := openNativeLibrary(candidate)
		if err == nil && l != nil {
			lib = l
			break
		}
		lastErr = err
	}
	if lib == nil {
		if lastErr != nil {
			return nil, fmt.Errorf("%w: could not load Steamworks library: %v", ErrUnavailable, lastErr)
		}
		return nil, ErrUnavailable
	}

	d := &desktopDriver{lib: lib}
	if err := d.bindCoreSymbols(); err != nil {
		_ = lib.close()
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return d, nil
}

func (d *desktopDriver) bindOptional(fnPtr any, names ...string) bool {
	for _, name := range names {
		if sym, err := d.lib.sym(name); err == nil && sym != 0 {
			purego.RegisterFunc(fnPtr, sym)
			return true
		}
	}
	return false
}

func (d *desktopDriver) bindRequired(fnPtr any, names ...string) error {
	if d.bindOptional(fnPtr, names...) {
		return nil
	}
	return fmt.Errorf("missing Steamworks symbol %v", names)
}

func (d *desktopDriver) bindCoreSymbols() error {
	d.bindOptional(&d.restartAppIfNecessary, "SteamAPI_RestartAppIfNecessary")
	hasFlat := d.bindOptional(&d.initFlat, "SteamAPI_InitFlat")
	hasLegacy := d.bindOptional(&d.initLegacy, "SteamAPI_Init")
	if !hasFlat && !hasLegacy {
		return fmt.Errorf("missing SteamAPI_InitFlat and SteamAPI_Init")
	}
	if err := d.bindRequired(&d.shutdown, "SteamAPI_Shutdown"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.getHSteamPipe, "SteamAPI_GetHSteamPipe"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.manualDispatchInit, "SteamAPI_ManualDispatch_Init"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.manualDispatchRunFrame, "SteamAPI_ManualDispatch_RunFrame"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.manualDispatchGetNextCallback, "SteamAPI_ManualDispatch_GetNextCallback"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.manualDispatchFreeLastCallback, "SteamAPI_ManualDispatch_FreeLastCallback"); err != nil {
		return err
	}
	if err := d.bindRequired(&d.manualDispatchGetAPICallResult, "SteamAPI_ManualDispatch_GetAPICallResult"); err != nil {
		return err
	}

	d.bindOptional(&d.getUtils, steamUtilsAccessors...)
	d.bindOptional(&d.getUser, steamUserAccessors...)
	d.bindOptional(&d.getFriends, steamFriendsAccessors...)

	d.bindOptional(&d.getAppID, "SteamAPI_ISteamUtils_GetAppID")
	d.bindOptional(&d.isSteamDeck, "SteamAPI_ISteamUtils_IsSteamRunningOnSteamDeck")
	d.bindOptional(&d.isOverlayEnabled, "SteamAPI_ISteamUtils_IsOverlayEnabled")
	d.bindOptional(&d.showGamepadTextInput, "SteamAPI_ISteamUtils_ShowGamepadTextInput")
	d.bindOptional(&d.getEnteredTextLen, "SteamAPI_ISteamUtils_GetEnteredGamepadTextLength")
	d.bindOptional(&d.getEnteredTextInput, "SteamAPI_ISteamUtils_GetEnteredGamepadTextInput")

	d.bindOptional(&d.getSteamID, "SteamAPI_ISteamUser_GetSteamID")
	d.bindOptional(&d.getAuthTicketForWebAPI, "SteamAPI_ISteamUser_GetAuthTicketForWebApi")
	d.bindOptional(&d.cancelAuthTicket, "SteamAPI_ISteamUser_CancelAuthTicket")

	d.bindOptional(&d.getPersonaName, "SteamAPI_ISteamFriends_GetPersonaName")
	d.bindOptional(&d.setRichPresence, "SteamAPI_ISteamFriends_SetRichPresence")
	d.bindOptional(&d.clearRichPresence, "SteamAPI_ISteamFriends_ClearRichPresence")
	d.bindOptional(&d.activateOverlay, "SteamAPI_ISteamFriends_ActivateGameOverlay")
	d.bindOptional(&d.activateOverlayToWebURL, "SteamAPI_ISteamFriends_ActivateGameOverlayToWebPage")
	return nil
}

func (d *desktopDriver) RestartAppIfNecessary(appID uint32) bool {
	if d.restartAppIfNecessary == nil || appID == 0 {
		return false
	}
	return d.restartAppIfNecessary(appID)
}

func (d *desktopDriver) Init() (InitResult, string, error) {
	if d.initFlat != nil {
		var errBuf [1024]byte
		res := InitResult(d.initFlat(&errBuf[0]))
		runtime.KeepAlive(&errBuf)
		if res != InitOK {
			return res, cString(errBuf[:]), nil
		}
	} else if d.initLegacy != nil {
		if !d.initLegacy() {
			return InitFailedGeneric, "SteamAPI_Init returned false", nil
		}
	} else {
		return InitFailedGeneric, "", ErrUnavailable
	}

	d.initialized = true
	d.manualDispatchInit()
	d.pipe = d.getHSteamPipe()
	if d.pipe == 0 {
		d.shutdown()
		d.initialized = false
		return InitFailedGeneric, "SteamAPI_GetHSteamPipe returned 0", nil
	}
	return InitOK, "", nil
}

func (d *desktopDriver) Shutdown() error {
	if d.initialized && d.shutdown != nil {
		d.shutdown()
		d.initialized = false
		d.pipe = 0
	}
	if d.lib != nil {
		l := d.lib
		d.lib = nil
		return l.close()
	}
	return nil
}

func (d *desktopDriver) RunFrame() {
	if !d.initialized || d.pipe == 0 {
		return
	}
	d.manualDispatchRunFrame(d.pipe)
}

func (d *desktopDriver) NextCallback() (RawCallback, bool) {
	if !d.initialized || d.pipe == 0 {
		return RawCallback{}, false
	}
	var msg callbackMsg
	if !d.manualDispatchGetNextCallback(d.pipe, &msg) {
		return RawCallback{}, false
	}
	var data []byte
	if msg.param != nil && msg.paramSize > 0 {
		src := unsafe.Slice(msg.param, int(msg.paramSize))
		data = make([]byte, len(src))
		copy(data, src)
	}
	runtime.KeepAlive(&msg)
	return RawCallback{
		SteamUser:  msg.steamUser,
		CallbackID: msg.callback,
		Data:       data,
	}, true
}

func (d *desktopDriver) FreeLastCallback() {
	if !d.initialized || d.pipe == 0 {
		return
	}
	d.manualDispatchFreeLastCallback(d.pipe)
}

func (d *desktopDriver) GetAPICallResult(call uint64, expectedCallback int32, size int32) ([]byte, bool, bool) {
	if !d.initialized || d.pipe == 0 || size <= 0 {
		return nil, true, false
	}
	buf := make([]byte, size)
	var failed bool
	ok := d.manualDispatchGetAPICallResult(d.pipe, call, &buf[0], size, expectedCallback, &failed)
	runtime.KeepAlive(buf)
	if !ok {
		return nil, failed, false
	}
	return buf, failed, true
}

func (d *desktopDriver) utilsPtr() uintptr {
	if d.getUtils == nil {
		return 0
	}
	return d.getUtils()
}

func (d *desktopDriver) userPtr() uintptr {
	if d.getUser == nil {
		return 0
	}
	return d.getUser()
}

func (d *desktopDriver) friendsPtr() uintptr {
	if d.getFriends == nil {
		return 0
	}
	return d.getFriends()
}

func (d *desktopDriver) AppID() uint32 {
	if d.getAppID == nil {
		return 0
	}
	u := d.utilsPtr()
	if u == 0 {
		return 0
	}
	return d.getAppID(u)
}

func (d *desktopDriver) SteamID() uint64 {
	if d.getSteamID == nil {
		return 0
	}
	u := d.userPtr()
	if u == 0 {
		return 0
	}
	return d.getSteamID(u)
}

func (d *desktopDriver) PersonaName() string {
	if d.getPersonaName == nil {
		return ""
	}
	f := d.friendsPtr()
	if f == 0 {
		return ""
	}
	return readCStringPtr(d.getPersonaName(f), 4096)
}

func (d *desktopDriver) RunningOnSteamDeck() bool {
	if d.isSteamDeck == nil {
		return false
	}
	u := d.utilsPtr()
	if u == 0 {
		return false
	}
	return d.isSteamDeck(u)
}

func (d *desktopDriver) OverlayEnabled() bool {
	if d.isOverlayEnabled == nil {
		return false
	}
	u := d.utilsPtr()
	if u == 0 {
		return false
	}
	return d.isOverlayEnabled(u)
}

func (d *desktopDriver) ActivateGameOverlay(dialog string) bool {
	if d.activateOverlay == nil {
		return false
	}
	f := d.friendsPtr()
	if f == 0 {
		return false
	}
	c := cBytes(dialog)
	d.activateOverlay(f, &c[0])
	runtime.KeepAlive(c)
	return true
}

func (d *desktopDriver) ActivateGameOverlayToWebPage(url string) bool {
	if d.activateOverlayToWebURL == nil {
		return false
	}
	f := d.friendsPtr()
	if f == 0 {
		return false
	}
	c := cBytes(url)
	// k_EActivateGameOverlayToWebPageMode_Default = 0
	d.activateOverlayToWebURL(f, &c[0], 0)
	runtime.KeepAlive(c)
	return true
}

func (d *desktopDriver) SetRichPresence(key, value string) bool {
	if d.setRichPresence == nil {
		return false
	}
	f := d.friendsPtr()
	if f == 0 {
		return false
	}
	ck := cBytes(key)
	cv := cBytes(value)
	ok := d.setRichPresence(f, &ck[0], &cv[0])
	runtime.KeepAlive(ck)
	runtime.KeepAlive(cv)
	return ok
}

func (d *desktopDriver) ClearRichPresence() {
	if d.clearRichPresence == nil {
		return
	}
	f := d.friendsPtr()
	if f == 0 {
		return
	}
	d.clearRichPresence(f)
}

func (d *desktopDriver) GetAuthTicketForWebAPI(identity string) uint32 {
	if d.getAuthTicketForWebAPI == nil {
		return 0
	}
	u := d.userPtr()
	if u == 0 {
		return 0
	}
	if identity == "" {
		return d.getAuthTicketForWebAPI(u, nil)
	}
	c := cBytes(identity)
	h := d.getAuthTicketForWebAPI(u, &c[0])
	runtime.KeepAlive(c)
	return h
}

func (d *desktopDriver) CancelAuthTicket(handle uint32) {
	if d.cancelAuthTicket == nil || handle == 0 {
		return
	}
	u := d.userPtr()
	if u == 0 {
		return
	}
	d.cancelAuthTicket(u, handle)
}

func (d *desktopDriver) ShowGamepadTextInput(desc, existing string, maxChars uint32, multiLine, password bool) bool {
	if d.showGamepadTextInput == nil {
		return false
	}
	u := d.utilsPtr()
	if u == 0 {
		return false
	}
	var inputMode, lineMode int32
	if password {
		inputMode = 1 // k_EGamepadTextInputModePassword
	}
	if multiLine {
		lineMode = 1 // k_EGamepadTextInputLineModeMultipleLines
	}
	cd := cBytes(desc)
	ce := cBytes(existing)
	ok := d.showGamepadTextInput(u, inputMode, lineMode, &cd[0], maxChars, &ce[0])
	runtime.KeepAlive(cd)
	runtime.KeepAlive(ce)
	return ok
}

func (d *desktopDriver) GetEnteredGamepadTextInput() (string, bool) {
	if d.getEnteredTextLen == nil || d.getEnteredTextInput == nil {
		return "", false
	}
	u := d.utilsPtr()
	if u == 0 {
		return "", false
	}
	n := d.getEnteredTextLen(u)
	if n == 0 {
		return "", true
	}
	buf := make([]byte, n)
	ok := d.getEnteredTextInput(u, &buf[0], n)
	runtime.KeepAlive(buf)
	if !ok {
		return "", false
	}
	return cString(buf), true
}

func (d *desktopDriver) ResolveSymbol(names ...string) (uintptr, error) {
	if d.lib == nil {
		return 0, ErrClosed
	}
	var lastErr error
	for _, name := range names {
		sym, err := d.lib.sym(name)
		if err == nil && sym != 0 {
			return sym, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
	}
	return 0, ErrUnavailable
}

func (d *desktopDriver) CallbackPack() int {
	return platformCallbackPack
}

func cBytes(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

func readCStringPtr(p *byte, maxLen int) string {
	if p == nil || maxLen <= 0 {
		return ""
	}
	buf := make([]byte, 0, 64)
	for i := range maxLen {
		b := *(*byte)(unsafe.Add(unsafe.Pointer(p), i))
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}
