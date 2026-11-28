//go:build (darwin && !ios) || (linux && !android) || windows

package modding

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/nsgo-dev/game/pkg/online/steamworks"
)

// Steamworks SDK constants (ISteamUGC / EResult / callback IDs).
const (
	steamResultOK       int32 = 1
	steamResultFileNone int32 = 9

	steamWorkshopFileTypeCommunity uint32 = 0
	steamUGCQueryRankedByVote      uint32 = 0
	steamUGCMatchingItems          uint32 = 0

	steamItemStateInstalled       uint32 = 4
	steamItemStateNeedsUpdate     uint32 = 8
	steamItemStateDownloading     uint32 = 16
	steamItemStateDownloadPending uint32 = 32

	steamCallbackUGCQueryCompleted      int32 = 3401
	steamCallbackCreateItemResult       int32 = 3403
	steamCallbackSubmitItemUpdateResult int32 = 3404
	steamCallbackSubscribeItemResult    int32 = 1313
	steamCallbackUnsubscribeItemResult  int32 = 1315

	steamUGCDetailsBufSize = 16384
	defaultSteamPoll       = 10 * time.Millisecond
)

type steamParamStringArray struct {
	strings **byte
	count   int32
}

// SteamWorkshop implements [WorkshopBackend] against the local Steamworks shared library
// (libsteam_api.dylib on macOS, libsteam_api.so on Linux, steam_api64.dll on Windows) using
// purego and the Steamworks flat C API without CGo.
type SteamWorkshop struct {
	mu           sync.Mutex
	client       *steamworks.Client
	lib          *steamLibrary
	ownsInit     bool
	closed       bool
	pollInterval time.Duration

	ugc   uintptr
	utils uintptr

	apiInit         func() bool
	apiShutdown     func()
	apiRunCallbacks func()

	isAPICallCompleted func(self uintptr, call uint64, failed *bool) bool
	getAPICallResult   func(self uintptr, call uint64, callback *byte, size int32, expected int32, failed *bool) bool

	createItem          func(self uintptr, consumerAppID uint32, fileType uint32) uint64
	startItemUpdate     func(self uintptr, consumerAppID uint32, fileID uint64) uint64
	setItemTitle        func(self uintptr, handle uint64, title *byte) bool
	setItemDescription  func(self uintptr, handle uint64, desc *byte) bool
	setItemVisibility   func(self uintptr, handle uint64, visibility uint32) bool
	setItemTags         func(self uintptr, handle uint64, tags *steamParamStringArray, allowAdminTags bool) bool
	setItemContent      func(self uintptr, handle uint64, contentDir *byte) bool
	setItemPreview      func(self uintptr, handle uint64, previewFile *byte) bool
	submitItemUpdate    func(self uintptr, handle uint64, changeNote *byte) uint64
	subscribeItem       func(self uintptr, fileID uint64) uint64
	unsubscribeItem     func(self uintptr, fileID uint64) uint64
	getNumSubscribed    func(self uintptr) uint32
	getSubscribedItems  func(self uintptr, ids *uint64, maxEntries uint32) uint32
	getItemState        func(self uintptr, fileID uint64) uint32
	getItemInstallInfo  func(self uintptr, fileID uint64, sizeOnDisk *uint64, folder *byte, folderSize uint32, timestamp *uint32) bool
	downloadItem        func(self uintptr, fileID uint64, highPriority bool) bool
	createQueryAllPage  func(self uintptr, queryType uint32, matchingType uint32, creatorAppID uint32, consumerAppID uint32, page uint32) uint64
	createQueryDetails  func(self uintptr, ids *uint64, numIDs uint32) uint64
	setSearchText       func(self uintptr, handle uint64, text *byte) bool
	addRequiredTag      func(self uintptr, handle uint64, tag *byte) bool
	sendQueryUGCRequest func(self uintptr, handle uint64) uint64
	getQueryUGCResult   func(self uintptr, handle uint64, index uint32, details *byte) bool
	releaseQueryRequest func(self uintptr, handle uint64) bool
}

// NewSteamWorkshop attaches to opts.Client (recommended) or loads the Steamworks
// shared library via purego, initializes Steamworks (unless opts.SkipInit is set),
// and binds the flat ISteamUGC APIs. When opts.Client is set, SteamWorkshop does
// not open the library again and never calls SteamAPI_Init, SteamAPI_Shutdown,
// or SteamAPI_RunCallbacks.
// When the Steamworks library or running Steam client is absent, it returns an
// error matching [ErrWorkshopUnavailable].
func NewSteamWorkshop(opts SteamWorkshopOptions) (*SteamWorkshop, error) {
	poll := opts.PollInterval
	if poll <= 0 {
		poll = defaultSteamPoll
	}

	if opts.Client != nil {
		if opts.Client.Closed() {
			return nil, fmt.Errorf("%w: shared steamworks client is closed", ErrWorkshopUnavailable)
		}
		w := &SteamWorkshop{
			client:       opts.Client,
			pollInterval: poll,
		}
		resolve := func(names ...string) (uintptr, error) {
			return opts.Client.ResolveSymbol(names...)
		}
		if err := w.bindUGCSymbols(resolve); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrWorkshopUnavailable, err)
		}
		ugcAccessor, err := resolve(
			"SteamAPI_SteamUGC_v021",
			"SteamAPI_SteamUGC_v020",
			"SteamAPI_SteamUGC_v018",
			"SteamAPI_SteamUGC_v017",
			"SteamAPI_SteamUGC_v016",
			"SteamAPI_SteamUGC_v015",
			"SteamAPI_SteamUGC_v014",
			"SteamUGC",
		)
		if err != nil {
			return nil, fmt.Errorf("%w: ISteamUGC accessor not found: %v", ErrWorkshopUnavailable, err)
		}
		var getUGC func() uintptr
		purego.RegisterFunc(&getUGC, ugcAccessor)
		w.ugc = getUGC()
		if w.ugc == 0 {
			return nil, fmt.Errorf("%w: Steamworks returned nil ISteamUGC interface", ErrWorkshopUnavailable)
		}
		return w, nil
	}

	candidates := defaultSteamLibraryNames()
	if opts.LibraryPath != "" {
		candidates = []string{opts.LibraryPath}
	}

	var (
		lib     *steamLibrary
		openErr error
	)
	for _, candidate := range candidates {
		lib, openErr = openSteamLibrary(candidate)
		if openErr == nil {
			break
		}
	}
	if lib == nil {
		return nil, fmt.Errorf("%w: could not load Steamworks library: %v", ErrWorkshopUnavailable, openErr)
	}

	w := &SteamWorkshop{
		lib:          lib,
		pollInterval: poll,
	}

	if err := w.bindLegacySymbols(); err != nil {
		_ = lib.close()
		return nil, fmt.Errorf("%w: %v", ErrWorkshopUnavailable, err)
	}

	if !opts.SkipInit {
		if !w.apiInit() {
			_ = lib.close()
			return nil, fmt.Errorf("%w: SteamAPI_Init returned false (is Steam running with a valid steam_appid.txt?)", ErrWorkshopUnavailable)
		}
		w.ownsInit = true
	}

	ugcAccessor, err := w.resolveFirstSymbol(
		"SteamAPI_SteamUGC_v021",
		"SteamAPI_SteamUGC_v020",
		"SteamAPI_SteamUGC_v018",
		"SteamAPI_SteamUGC_v017",
		"SteamAPI_SteamUGC_v016",
		"SteamAPI_SteamUGC_v015",
		"SteamAPI_SteamUGC_v014",
		"SteamUGC",
	)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("%w: ISteamUGC accessor not found: %v", ErrWorkshopUnavailable, err)
	}
	utilsAccessor, err := w.resolveFirstSymbol(
		"SteamAPI_SteamUtils_v010",
		"SteamAPI_SteamUtils_v009",
		"SteamUtils",
	)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("%w: ISteamUtils accessor not found: %v", ErrWorkshopUnavailable, err)
	}

	var getUGC, getUtils func() uintptr
	purego.RegisterFunc(&getUGC, ugcAccessor)
	purego.RegisterFunc(&getUtils, utilsAccessor)

	w.ugc = getUGC()
	w.utils = getUtils()
	if w.ugc == 0 || w.utils == 0 {
		_ = w.Close()
		return nil, fmt.Errorf("%w: Steamworks returned nil ISteamUGC or ISteamUtils interface", ErrWorkshopUnavailable)
	}

	return w, nil
}

func (w *SteamWorkshop) bindUGCSymbols(resolve func(names ...string) (uintptr, error)) error {
	required := []struct {
		ptr  any
		name string
	}{
		{&w.createItem, "SteamAPI_ISteamUGC_CreateItem"},
		{&w.startItemUpdate, "SteamAPI_ISteamUGC_StartItemUpdate"},
		{&w.setItemTitle, "SteamAPI_ISteamUGC_SetItemTitle"},
		{&w.setItemDescription, "SteamAPI_ISteamUGC_SetItemDescription"},
		{&w.setItemVisibility, "SteamAPI_ISteamUGC_SetItemVisibility"},
		{&w.setItemTags, "SteamAPI_ISteamUGC_SetItemTags"},
		{&w.setItemContent, "SteamAPI_ISteamUGC_SetItemContent"},
		{&w.setItemPreview, "SteamAPI_ISteamUGC_SetItemPreview"},
		{&w.submitItemUpdate, "SteamAPI_ISteamUGC_SubmitItemUpdate"},
		{&w.subscribeItem, "SteamAPI_ISteamUGC_SubscribeItem"},
		{&w.unsubscribeItem, "SteamAPI_ISteamUGC_UnsubscribeItem"},
		{&w.getNumSubscribed, "SteamAPI_ISteamUGC_GetNumSubscribedItems"},
		{&w.getSubscribedItems, "SteamAPI_ISteamUGC_GetSubscribedItems"},
		{&w.getItemState, "SteamAPI_ISteamUGC_GetItemState"},
		{&w.getItemInstallInfo, "SteamAPI_ISteamUGC_GetItemInstallInfo"},
		{&w.downloadItem, "SteamAPI_ISteamUGC_DownloadItem"},
		{&w.createQueryAllPage, "SteamAPI_ISteamUGC_CreateQueryAllUGCRequestPage"},
		{&w.createQueryDetails, "SteamAPI_ISteamUGC_CreateQueryUGCDetailsRequest"},
		{&w.setSearchText, "SteamAPI_ISteamUGC_SetSearchText"},
		{&w.addRequiredTag, "SteamAPI_ISteamUGC_AddRequiredTag"},
		{&w.sendQueryUGCRequest, "SteamAPI_ISteamUGC_SendQueryUGCRequest"},
		{&w.getQueryUGCResult, "SteamAPI_ISteamUGC_GetQueryUGCResult"},
		{&w.releaseQueryRequest, "SteamAPI_ISteamUGC_ReleaseQueryUGCRequest"},
	}
	for _, r := range required {
		sym, err := resolve(r.name)
		if err != nil {
			return fmt.Errorf("missing symbol %s: %w", r.name, err)
		}
		purego.RegisterFunc(r.ptr, sym)
	}
	return nil
}

func (w *SteamWorkshop) bindLegacySymbols() error {
	resolve := func(names ...string) (uintptr, error) {
		return w.resolveFirstSymbol(names...)
	}
	legacy := []struct {
		ptr  any
		name string
	}{
		{&w.apiInit, "SteamAPI_Init"},
		{&w.apiShutdown, "SteamAPI_Shutdown"},
		{&w.apiRunCallbacks, "SteamAPI_RunCallbacks"},
		{&w.isAPICallCompleted, "SteamAPI_ISteamUtils_IsAPICallCompleted"},
		{&w.getAPICallResult, "SteamAPI_ISteamUtils_GetAPICallResult"},
	}
	for _, r := range legacy {
		sym, err := resolve(r.name)
		if err != nil {
			return fmt.Errorf("missing symbol %s: %w", r.name, err)
		}
		purego.RegisterFunc(r.ptr, sym)
	}
	return w.bindUGCSymbols(resolve)
}

func (w *SteamWorkshop) resolveFirstSymbol(names ...string) (uintptr, error) {
	var lastErr error
	for _, name := range names {
		sym, err := w.lib.sym(name)
		if err == nil {
			return sym, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

// Close releases Workshop resources. When attached to a shared
// [*steamworks.Client], Close does not shut down Steamworks or unload the
// shared library. In legacy standalone mode it shuts down Steamworks (if
// initialized by [NewSteamWorkshop]) and unloads the library.
func (w *SteamWorkshop) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.client != nil {
		w.client = nil
		w.ugc = 0
		w.utils = 0
		return nil
	}
	if w.ownsInit && w.apiShutdown != nil {
		w.apiShutdown()
		w.ownsInit = false
	}
	w.ugc = 0
	w.utils = 0
	if w.lib != nil {
		return w.lib.close()
	}
	return nil
}

func (w *SteamWorkshop) checkOpenLocked() error {
	if w == nil || w.closed || w.ugc == 0 {
		return ErrWorkshopUnavailable
	}
	if w.client != nil {
		if w.client.Closed() {
			return ErrWorkshopUnavailable
		}
		return nil
	}
	if w.utils == 0 {
		return ErrWorkshopUnavailable
	}
	return nil
}

func (w *SteamWorkshop) callbackPack() int {
	if w.client != nil {
		return w.client.CallbackPack()
	}
	return steamCallbackPack
}

func (w *SteamWorkshop) awaitAPICallLocked(ctx context.Context, call uint64, expectedCallback int32, buf []byte) error {
	if call == 0 {
		return errors.New("modding: steamworks returned invalid SteamAPICall_t handle")
	}
	if client := w.client; client != nil {
		// Release w.mu while awaiting the shared client's manual-dispatch pump
		// so Close or concurrent calls do not block behind network I/O.
		w.mu.Unlock()
		data, err := client.AwaitCall(ctx, call, expectedCallback)
		w.mu.Lock()
		if err != nil {
			if errors.Is(err, steamworks.ErrClosed) {
				return ErrWorkshopUnavailable
			}
			return err
		}
		if err := w.checkOpenLocked(); err != nil {
			return err
		}
		clear(buf)
		copy(buf, data)
		return nil
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		w.apiRunCallbacks()
		var failed bool
		if w.isAPICallCompleted(w.utils, call, &failed) {
			if failed {
				return fmt.Errorf("modding: steamworks API call %d failed", call)
			}
			var ioFailed bool
			ok := w.getAPICallResult(w.utils, call, &buf[0], int32(len(buf)), expectedCallback, &ioFailed)
			if !ok || ioFailed {
				return fmt.Errorf("modding: steamworks GetAPICallResult failed for callback %d", expectedCallback)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// CreateItem creates a new empty Steam Workshop community item under appID.
func (w *SteamWorkshop) CreateItem(ctx context.Context, appID uint32) (PublishedFileID, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return 0, err
	}

	call := w.createItem(w.ugc, appID, steamWorkshopFileTypeCommunity)
	var buf [32]byte
	if err := w.awaitAPICallLocked(ctx, call, steamCallbackCreateItemResult, buf[:]); err != nil {
		return 0, err
	}
	eResult, id, _ := decodeCreateItemResult(buf[:], w.callbackPack())
	if eResult != steamResultOK {
		return 0, fmt.Errorf("modding: steamworks CreateItem failed (EResult=%d)", eResult)
	}
	return id, nil
}

// SubmitUpdate uploads updated metadata, preview image, and content directory for req.ID.
func (w *SteamWorkshop) SubmitUpdate(ctx context.Context, req PublishRequest) (PublishResult, error) {
	if err := ctx.Err(); err != nil {
		return PublishResult{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return PublishResult{}, err
	}

	handle := w.startItemUpdate(w.ugc, req.AppID, uint64(req.ID))
	if handle == 0 {
		return PublishResult{}, errors.New("modding: steamworks StartItemUpdate failed")
	}

	if req.Title != "" {
		c := cBytes(req.Title)
		if !w.setItemTitle(w.ugc, handle, &c[0]) {
			return PublishResult{}, errors.New("modding: steamworks SetItemTitle failed")
		}
		runtime.KeepAlive(c)
	}
	if req.Description != "" {
		c := cBytes(req.Description)
		if !w.setItemDescription(w.ugc, handle, &c[0]) {
			return PublishResult{}, errors.New("modding: steamworks SetItemDescription failed")
		}
		runtime.KeepAlive(c)
	}
	if req.Visibility != "" {
		if !w.setItemVisibility(w.ugc, handle, req.Visibility.steamValue()) {
			return PublishResult{}, errors.New("modding: steamworks SetItemVisibility failed")
		}
	}
	if len(req.Tags) > 0 {
		cstrs := make([][]byte, len(req.Tags))
		ptrs := make([]*byte, len(req.Tags))
		for i, tag := range req.Tags {
			cstrs[i] = cBytes(tag)
			ptrs[i] = &cstrs[i][0]
		}
		arr := steamParamStringArray{
			strings: &ptrs[0],
			count:   int32(len(ptrs)),
		}
		ok := w.setItemTags(w.ugc, handle, &arr, false)
		runtime.KeepAlive(cstrs)
		runtime.KeepAlive(ptrs)
		if !ok {
			return PublishResult{}, errors.New("modding: steamworks SetItemTags failed")
		}
	}
	if req.ContentDir != "" {
		c := cBytes(req.ContentDir)
		if !w.setItemContent(w.ugc, handle, &c[0]) {
			return PublishResult{}, errors.New("modding: steamworks SetItemContent failed")
		}
		runtime.KeepAlive(c)
	}
	if req.PreviewFile != "" {
		c := cBytes(req.PreviewFile)
		if !w.setItemPreview(w.ugc, handle, &c[0]) {
			return PublishResult{}, errors.New("modding: steamworks SetItemPreview failed")
		}
		runtime.KeepAlive(c)
	}

	changeNote := cBytes(req.ChangeNote)
	call := w.submitItemUpdate(w.ugc, handle, &changeNote[0])
	runtime.KeepAlive(changeNote)

	var buf [32]byte
	if err := w.awaitAPICallLocked(ctx, call, steamCallbackSubmitItemUpdateResult, buf[:]); err != nil {
		return PublishResult{}, err
	}
	eResult, needsLegal, id := decodeSubmitItemUpdateResult(buf[:], w.callbackPack())
	if eResult != steamResultOK {
		return PublishResult{}, fmt.Errorf("modding: steamworks SubmitItemUpdate failed (EResult=%d)", eResult)
	}
	if id == 0 {
		id = req.ID
	}
	res := PublishResult{
		ID:                  id,
		NeedsLegalAgreement: needsLegal,
	}
	if needsLegal {
		res.LegalAgreementURL = WorkshopLegalAgreementURL
	}
	return res, nil
}

// Subscribe subscribes the local Steam user to Workshop item id.
func (w *SteamWorkshop) Subscribe(ctx context.Context, id PublishedFileID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return err
	}

	call := w.subscribeItem(w.ugc, uint64(id))
	var buf [32]byte
	if err := w.awaitAPICallLocked(ctx, call, steamCallbackSubscribeItemResult, buf[:]); err != nil {
		return err
	}
	eResult, _ := decodeSubscribeResult(buf[:], w.callbackPack())
	if eResult != steamResultOK {
		if eResult == steamResultFileNone {
			return fmt.Errorf("%w: %s", ErrWorkshopNotFound, id)
		}
		return fmt.Errorf("modding: steamworks SubscribeItem failed (EResult=%d)", eResult)
	}
	return nil
}

// Unsubscribe unsubscribes the local Steam user from Workshop item id.
func (w *SteamWorkshop) Unsubscribe(ctx context.Context, id PublishedFileID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return err
	}

	call := w.unsubscribeItem(w.ugc, uint64(id))
	var buf [32]byte
	if err := w.awaitAPICallLocked(ctx, call, steamCallbackUnsubscribeItemResult, buf[:]); err != nil {
		return err
	}
	eResult, _ := decodeSubscribeResult(buf[:], w.callbackPack())
	if eResult != steamResultOK {
		return fmt.Errorf("modding: steamworks UnsubscribeItem failed (EResult=%d)", eResult)
	}
	return nil
}

// SubscribedItems returns all Workshop item IDs the local Steam user is subscribed to.
func (w *SteamWorkshop) SubscribedItems(ctx context.Context) ([]PublishedFileID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return nil, err
	}

	count := w.getNumSubscribed(w.ugc)
	if count == 0 {
		return nil, nil
	}
	raw := make([]uint64, count)
	got := w.getSubscribedItems(w.ugc, &raw[0], count)
	out := make([]PublishedFileID, got)
	for i := uint32(0); i < got; i++ {
		out[i] = PublishedFileID(raw[i])
	}
	return out, nil
}

// ItemInstallInfo returns the local install folder, size, and update timestamp for id.
// If the item is not installed or needs an update, it returns [ErrWorkshopNotInstalled].
func (w *SteamWorkshop) ItemInstallInfo(ctx context.Context, id PublishedFileID) (InstallInfo, error) {
	if err := ctx.Err(); err != nil {
		return InstallInfo{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return InstallInfo{}, err
	}

	state := w.getItemState(w.ugc, uint64(id))
	if state&steamItemStateInstalled == 0 || state&steamItemStateNeedsUpdate != 0 {
		return InstallInfo{}, fmt.Errorf("%w: %s", ErrWorkshopNotInstalled, id)
	}

	var (
		size   uint64
		ts     uint32
		folder [4096]byte
	)
	if !w.getItemInstallInfo(w.ugc, uint64(id), &size, &folder[0], uint32(len(folder)), &ts) {
		return InstallInfo{}, fmt.Errorf("%w: %s", ErrWorkshopNotInstalled, id)
	}
	return InstallInfo{
		ID:         id,
		SizeOnDisk: size,
		Folder:     cString(folder[:]),
		Timestamp:  time.Unix(int64(ts), 0).UTC(),
	}, nil
}

// DownloadItem starts downloading or updating Workshop item id and waits until Steam reports
// it is installed and up to date (or ctx is canceled).
func (w *SteamWorkshop) DownloadItem(ctx context.Context, id PublishedFileID, highPriority bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	if err := w.checkOpenLocked(); err != nil {
		w.mu.Unlock()
		return err
	}

	if client := w.client; client != nil {
		pack := client.CallbackPack()
		installedCh := make(chan struct{}, 1)
		unsub := client.OnCallback(steamworks.CallbackItemInstalled, func(data []byte) {
			if _, fileID, ok := decodeItemInstalled(data, pack); ok && fileID == id {
				select {
				case installedCh <- struct{}{}:
				default:
				}
			}
		})
		defer unsub()

		if !w.downloadItem(w.ugc, uint64(id), highPriority) {
			w.mu.Unlock()
			return fmt.Errorf("modding: steamworks DownloadItem(%s) failed", id)
		}

		poll := w.pollInterval
		if poll <= 0 {
			poll = defaultSteamPoll
		}
		ticker := time.NewTicker(poll)
		defer ticker.Stop()

		for {
			if err := w.checkOpenLocked(); err != nil {
				w.mu.Unlock()
				return err
			}
			state := w.getItemState(w.ugc, uint64(id))
			pending := state&(steamItemStateDownloading|steamItemStateDownloadPending|steamItemStateNeedsUpdate) != 0
			if state&steamItemStateInstalled != 0 && !pending {
				w.mu.Unlock()
				return nil
			}
			w.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-installedCh:
			case <-ticker.C:
			}
			w.mu.Lock()
		}
	}

	defer w.mu.Unlock()
	if !w.downloadItem(w.ugc, uint64(id), highPriority) {
		return fmt.Errorf("modding: steamworks DownloadItem(%s) failed", id)
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		w.apiRunCallbacks()
		state := w.getItemState(w.ugc, uint64(id))
		pending := state&(steamItemStateDownloading|steamItemStateDownloadPending|steamItemStateNeedsUpdate) != 0
		if state&steamItemStateInstalled != 0 && !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// QueryItems queries Steam Workshop for items matching query.
func (w *SteamWorkshop) QueryItems(ctx context.Context, query WorkshopQuery) ([]WorkshopItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.checkOpenLocked(); err != nil {
		return nil, err
	}

	var handle uint64
	if len(query.IDs) > 0 {
		rawIDs := make([]uint64, len(query.IDs))
		for i, id := range query.IDs {
			rawIDs[i] = uint64(id)
		}
		handle = w.createQueryDetails(w.ugc, &rawIDs[0], uint32(len(rawIDs)))
		runtime.KeepAlive(rawIDs)
	} else {
		page := query.Page
		if page == 0 {
			page = 1
		}
		handle = w.createQueryAllPage(w.ugc, steamUGCQueryRankedByVote, steamUGCMatchingItems, query.AppID, query.AppID, page)
		if handle != 0 {
			if query.SearchText != "" {
				c := cBytes(query.SearchText)
				w.setSearchText(w.ugc, handle, &c[0])
				runtime.KeepAlive(c)
			}
			for _, tag := range query.Tags {
				c := cBytes(tag)
				w.addRequiredTag(w.ugc, handle, &c[0])
				runtime.KeepAlive(c)
			}
		}
	}
	if handle == 0 {
		return nil, errors.New("modding: steamworks failed to create UGC query")
	}
	defer func() {
		if !w.closed && w.ugc != 0 && w.releaseQueryRequest != nil {
			w.releaseQueryRequest(w.ugc, handle)
		}
	}()

	call := w.sendQueryUGCRequest(w.ugc, handle)
	var completedBuf [32]byte
	if err := w.awaitAPICallLocked(ctx, call, steamCallbackUGCQueryCompleted, completedBuf[:]); err != nil {
		return nil, err
	}
	eResult, numReturned := decodeQueryCompleted(completedBuf[:])
	if eResult != steamResultOK {
		return nil, fmt.Errorf("modding: steamworks SendQueryUGCRequest failed (EResult=%d)", eResult)
	}

	pack := w.callbackPack()
	detailsBuf := make([]byte, steamUGCDetailsBufSize)
	items := make([]WorkshopItem, 0, numReturned)
	for i := uint32(0); i < numReturned; i++ {
		clear(detailsBuf)
		if !w.getQueryUGCResult(w.ugc, handle, i, &detailsBuf[0]) {
			continue
		}
		item, ok := decodeUGCDetails(detailsBuf, pack)
		if ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func cBytes(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

func cString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx >= 0 {
		return string(b[:idx])
	}
	return string(b)
}

func alignOffset(offset, align int) int {
	if align <= 1 {
		return offset
	}
	rem := offset % align
	if rem == 0 {
		return offset
	}
	return offset + (align - rem)
}

// decodeCreateItemResult decodes Steamworks CreateItemResult_t under pack (4 on Unix, 8 on Windows).
func decodeCreateItemResult(buf []byte, pack int) (eResult int32, id PublishedFileID, needsLegal bool) {
	eResult = int32(binary.LittleEndian.Uint32(buf[0:4]))
	idOff := alignOffset(4, min(8, pack))
	id = PublishedFileID(binary.LittleEndian.Uint64(buf[idOff : idOff+8]))
	needsLegal = buf[idOff+8] != 0
	return eResult, id, needsLegal
}

// decodeSubmitItemUpdateResult decodes Steamworks SubmitItemUpdateResult_t under pack.
func decodeSubmitItemUpdateResult(buf []byte, pack int) (eResult int32, needsLegal bool, id PublishedFileID) {
	eResult = int32(binary.LittleEndian.Uint32(buf[0:4]))
	needsLegal = buf[4] != 0
	idOff := alignOffset(5, min(8, pack))
	id = PublishedFileID(binary.LittleEndian.Uint64(buf[idOff : idOff+8]))
	return eResult, needsLegal, id
}

// decodeSubscribeResult decodes RemoteStorageSubscribePublishedFileResult_t /
// RemoteStorageUnsubscribePublishedFileResult_t under pack.
func decodeSubscribeResult(buf []byte, pack int) (eResult int32, id PublishedFileID) {
	eResult = int32(binary.LittleEndian.Uint32(buf[0:4]))
	idOff := alignOffset(4, min(8, pack))
	id = PublishedFileID(binary.LittleEndian.Uint64(buf[idOff : idOff+8]))
	return eResult, id
}

// decodeItemInstalled decodes Steamworks ItemInstalled_t under pack (4 on Unix, 8 on Windows).
func decodeItemInstalled(buf []byte, pack int) (appID uint32, id PublishedFileID, ok bool) {
	idOff := alignOffset(4, min(8, pack))
	if len(buf) < idOff+8 {
		return 0, 0, false
	}
	appID = binary.LittleEndian.Uint32(buf[0:4])
	id = PublishedFileID(binary.LittleEndian.Uint64(buf[idOff : idOff+8]))
	return appID, id, true
}

// decodeQueryCompleted decodes SteamUGCQueryCompleted_t.
func decodeQueryCompleted(buf []byte) (eResult int32, numReturned uint32) {
	// Offset 0: UGCQueryHandle_t (uint64)
	// Offset 8: EResult (int32)
	// Offset 12: uint32 m_unNumResultsReturned
	eResult = int32(binary.LittleEndian.Uint32(buf[8:12]))
	numReturned = binary.LittleEndian.Uint32(buf[12:16])
	return eResult, numReturned
}

// decodeUGCDetails decodes the fixed-layout prefix of Steamworks SteamUGCDetails_t under pack.
func decodeUGCDetails(buf []byte, pack int) (WorkshopItem, bool) {
	id := PublishedFileID(binary.LittleEndian.Uint64(buf[0:8]))
	eResult := int32(binary.LittleEndian.Uint32(buf[8:12]))
	if id == 0 || eResult != steamResultOK {
		return WorkshopItem{}, false
	}
	consumerAppID := binary.LittleEndian.Uint32(buf[20:24])
	title := cString(buf[24 : 24+129])
	desc := cString(buf[153 : 153+8000])

	off := alignOffset(153+8000, min(8, pack))
	ownerID := binary.LittleEndian.Uint64(buf[off : off+8])
	off += 8
	off += 4 // m_rtimeCreated
	updatedUnix := binary.LittleEndian.Uint32(buf[off : off+4])
	off += 4
	off += 4 // m_rtimeAddedToUserList
	vis := visibilityFromSteam(int32(binary.LittleEndian.Uint32(buf[off : off+4])))
	off += 4
	off += 3 // m_bBanned, m_bAcceptedForUse, m_bTagsTruncated
	rawTags := cString(buf[off : off+1025])
	var tags []string
	if rawTags != "" {
		for _, part := range strings.Split(rawTags, ",") {
			if t := strings.TrimSpace(part); t != "" {
				tags = append(tags, t)
			}
		}
	}

	return WorkshopItem{
		ID:          id,
		AppID:       consumerAppID,
		Title:       title,
		Description: desc,
		Tags:        tags,
		Visibility:  vis,
		OwnerID:     ownerID,
		UpdatedAt:   time.Unix(int64(updatedUnix), 0).UTC(),
	}, true
}
