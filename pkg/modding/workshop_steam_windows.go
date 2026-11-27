//go:build windows

package modding

import (
	"fmt"
	"syscall"
)

// steamCallbackPack matches Steamworks VALVE_CALLBACK_PACK_LARGE (#pragma pack(8)) on Windows.
const steamCallbackPack = 8

type steamLibrary struct {
	dll *syscall.DLL
}

func defaultSteamLibraryNames() []string {
	if ^uintptr(0)>>63 == 1 {
		return []string{"steam_api64.dll", "steam_api.dll"}
	}
	return []string{"steam_api.dll", "steam_api64.dll"}
}

func openSteamLibrary(path string) (*steamLibrary, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	return &steamLibrary{dll: dll}, nil
}

func (l *steamLibrary) sym(name string) (uintptr, error) {
	if l == nil || l.dll == nil {
		return 0, fmt.Errorf("steam library not loaded")
	}
	proc, err := l.dll.FindProc(name)
	if err != nil {
		return 0, err
	}
	return proc.Addr(), nil
}

func (l *steamLibrary) close() error {
	if l == nil || l.dll == nil {
		return nil
	}
	d := l.dll
	l.dll = nil
	return d.Release()
}
