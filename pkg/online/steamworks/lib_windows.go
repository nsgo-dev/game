//go:build windows

package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// platformCallbackPack matches Steamworks VALVE_CALLBACK_PACK_LARGE (#pragma pack(8)) on Windows.
const platformCallbackPack = 8

type nativeLibrary struct {
	dll *syscall.DLL
}

func libraryCandidates(explicit string) []string {
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

func openNativeLibrary(path string) (*nativeLibrary, error) {
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, err
	}
	return &nativeLibrary{dll: dll}, nil
}

func (l *nativeLibrary) sym(name string) (uintptr, error) {
	if l == nil || l.dll == nil {
		return 0, fmt.Errorf("steamworks library not loaded")
	}
	proc, err := l.dll.FindProc(name)
	if err != nil {
		return 0, err
	}
	return proc.Addr(), nil
}

func (l *nativeLibrary) close() error {
	if l == nil || l.dll == nil {
		return nil
	}
	d := l.dll
	l.dll = nil
	return d.Release()
}
