//go:build (darwin && !ios) || (linux && !android)

package modding

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// steamCallbackPack matches Steamworks VALVE_CALLBACK_PACK_SMALL (#pragma pack(4)) on macOS and Linux.
const steamCallbackPack = 4

type steamLibrary struct {
	handle uintptr
}

func defaultSteamLibraryNames() []string {
	if runtime.GOOS == "darwin" {
		return []string{"libsteam_api.dylib", "./libsteam_api.dylib"}
	}
	return []string{"libsteam_api.so", "./libsteam_api.so"}
}

func openSteamLibrary(path string) (*steamLibrary, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	return &steamLibrary{handle: handle}, nil
}

func (l *steamLibrary) sym(name string) (uintptr, error) {
	if l == nil || l.handle == 0 {
		return 0, fmt.Errorf("steam library not loaded")
	}
	return purego.Dlsym(l.handle, name)
}

func (l *steamLibrary) close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	h := l.handle
	l.handle = 0
	return purego.Dlclose(h)
}
