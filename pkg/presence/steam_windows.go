//go:build windows

package presence

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/ebitengine/purego"
)

type windowsSteamDriver struct {
	dll               *syscall.DLL
	didInit           bool
	getFriends        func() uintptr
	setRichPresence   func(instance uintptr, key, value string) bool
	clearRichPresence func(instance uintptr)
	shutdown          func()
}

func (d *windowsSteamDriver) SetRichPresence(key, value string) bool {
	if d.getFriends == nil || d.setRichPresence == nil {
		return false
	}
	friends := d.getFriends()
	if friends == 0 {
		return false
	}
	return d.setRichPresence(friends, key, value)
}

func (d *windowsSteamDriver) ClearRichPresence() {
	if d.getFriends == nil || d.clearRichPresence == nil {
		return
	}
	friends := d.getFriends()
	if friends == 0 {
		return
	}
	d.clearRichPresence(friends)
}

func (d *windowsSteamDriver) Close() error {
	if d.didInit && d.shutdown != nil {
		d.shutdown()
		d.didInit = false
	}
	if d.dll != nil {
		dll := d.dll
		d.dll = nil
		return dll.Release()
	}
	return nil
}

func steamLibraryCandidates(explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	dllName := "steam_api64.dll"
	if runtime.GOARCH == "386" {
		dllName = "steam_api.dll"
	}
	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), dllName))
	}
	out = append(out, dllName)
	return out
}

func openSteamDriver(cfg SteamConfig) (SteamDriver, error) {
	var dll *syscall.DLL
	for _, candidate := range steamLibraryCandidates(cfg.LibraryPath) {
		loaded, err := syscall.LoadDLL(candidate)
		if err == nil && loaded != nil {
			dll = loaded
			break
		}
	}
	if dll == nil {
		return nil, ErrUnavailable
	}

	var friendsProc *syscall.Proc
	for _, name := range steamFriendsSymbols {
		p, err := dll.FindProc(name)
		if err == nil && p != nil {
			friendsProc = p
			break
		}
	}
	if friendsProc == nil {
		_ = dll.Release()
		return nil, fmt.Errorf("%w: steam library missing ISteamFriends accessor", ErrUnavailable)
	}

	setProc, err := dll.FindProc("SteamAPI_ISteamFriends_SetRichPresence")
	if err != nil || setProc == nil {
		_ = dll.Release()
		return nil, fmt.Errorf("%w: steam library missing SteamAPI_ISteamFriends_SetRichPresence", ErrUnavailable)
	}
	clearProc, err := dll.FindProc("SteamAPI_ISteamFriends_ClearRichPresence")
	if err != nil || clearProc == nil {
		_ = dll.Release()
		return nil, fmt.Errorf("%w: steam library missing SteamAPI_ISteamFriends_ClearRichPresence", ErrUnavailable)
	}

	drv := &windowsSteamDriver{dll: dll}
	purego.RegisterFunc(&drv.getFriends, friendsProc.Addr())
	purego.RegisterFunc(&drv.setRichPresence, setProc.Addr())
	purego.RegisterFunc(&drv.clearRichPresence, clearProc.Addr())

	if drv.getFriends() == 0 {
		if cfg.AutoInit {
			if initProc, initErr := dll.FindProc("SteamAPI_Init"); initErr == nil && initProc != nil {
				var initFn func() bool
				purego.RegisterFunc(&initFn, initProc.Addr())
				if initFn() {
					drv.didInit = true
					if shutProc, shutErr := dll.FindProc("SteamAPI_Shutdown"); shutErr == nil && shutProc != nil {
						purego.RegisterFunc(&drv.shutdown, shutProc.Addr())
					}
				}
			}
		}
		if drv.getFriends() == 0 {
			_ = drv.Close()
			return nil, fmt.Errorf("%w: steam client not initialized", ErrUnavailable)
		}
	}

	return drv, nil
}
