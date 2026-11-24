//go:build (darwin && !ios) || (linux && !android)

package presence

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

type puregoSteamDriver struct {
	handle            uintptr
	didInit           bool
	getFriends        func() uintptr
	setRichPresence   func(instance uintptr, key, value string) bool
	clearRichPresence func(instance uintptr)
	shutdown          func()
}

func (d *puregoSteamDriver) SetRichPresence(key, value string) bool {
	if d.getFriends == nil || d.setRichPresence == nil {
		return false
	}
	friends := d.getFriends()
	if friends == 0 {
		return false
	}
	return d.setRichPresence(friends, key, value)
}

func (d *puregoSteamDriver) ClearRichPresence() {
	if d.getFriends == nil || d.clearRichPresence == nil {
		return
	}
	friends := d.getFriends()
	if friends == 0 {
		return
	}
	d.clearRichPresence(friends)
}

func (d *puregoSteamDriver) Close() error {
	if d.didInit && d.shutdown != nil {
		d.shutdown()
		d.didInit = false
	}
	if d.handle != 0 {
		h := d.handle
		d.handle = 0
		return purego.Dlclose(h)
	}
	return nil
}

func steamLibraryCandidates(explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	var out []string
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	switch runtime.GOOS {
	case "darwin":
		if exeDir != "" {
			out = append(out,
				filepath.Join(exeDir, "libsteam_api.dylib"),
				filepath.Join(exeDir, "..", "Frameworks", "libsteam_api.dylib"),
			)
		}
		out = append(out, "libsteam_api.dylib")
	default: // linux
		if exeDir != "" {
			out = append(out,
				filepath.Join(exeDir, "libsteam_api.so"),
				filepath.Join(exeDir, "lib64", "libsteam_api.so"),
			)
		}
		out = append(out, "libsteam_api.so")
	}
	return out
}

func openSteamDriver(cfg SteamConfig) (SteamDriver, error) {
	var (
		handle uintptr
		err    error
	)
	for _, candidate := range steamLibraryCandidates(cfg.LibraryPath) {
		handle, err = purego.Dlopen(candidate, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil && handle != 0 {
			break
		}
	}
	if handle == 0 {
		return nil, ErrUnavailable
	}

	closeHandle := func() {
		_ = purego.Dlclose(handle)
	}

	var friendsSym uintptr
	for _, name := range steamFriendsSymbols {
		sym, symErr := purego.Dlsym(handle, name)
		if symErr == nil && sym != 0 {
			friendsSym = sym
			break
		}
	}
	if friendsSym == 0 {
		closeHandle()
		return nil, fmt.Errorf("%w: steam library missing ISteamFriends accessor", ErrUnavailable)
	}

	setSym, err := purego.Dlsym(handle, "SteamAPI_ISteamFriends_SetRichPresence")
	if err != nil || setSym == 0 {
		closeHandle()
		return nil, fmt.Errorf("%w: steam library missing SteamAPI_ISteamFriends_SetRichPresence", ErrUnavailable)
	}
	clearSym, err := purego.Dlsym(handle, "SteamAPI_ISteamFriends_ClearRichPresence")
	if err != nil || clearSym == 0 {
		closeHandle()
		return nil, fmt.Errorf("%w: steam library missing SteamAPI_ISteamFriends_ClearRichPresence", ErrUnavailable)
	}

	drv := &puregoSteamDriver{handle: handle}
	purego.RegisterFunc(&drv.getFriends, friendsSym)
	purego.RegisterFunc(&drv.setRichPresence, setSym)
	purego.RegisterFunc(&drv.clearRichPresence, clearSym)

	if drv.getFriends() == 0 {
		if cfg.AutoInit {
			if initSym, initErr := purego.Dlsym(handle, "SteamAPI_Init"); initErr == nil && initSym != 0 {
				var initFn func() bool
				purego.RegisterFunc(&initFn, initSym)
				if initFn() {
					drv.didInit = true
					if shutSym, shutErr := purego.Dlsym(handle, "SteamAPI_Shutdown"); shutErr == nil && shutSym != 0 {
						purego.RegisterFunc(&drv.shutdown, shutSym)
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
