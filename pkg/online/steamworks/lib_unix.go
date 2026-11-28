//go:build (darwin && !ios) || (linux && !android)

package steamworks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

// platformCallbackPack matches Steamworks VALVE_CALLBACK_PACK_SMALL (#pragma pack(4)) on macOS and Linux.
const platformCallbackPack = 4

type nativeLibrary struct {
	handle uintptr
}

func libraryCandidates(explicit string) []string {
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

func openNativeLibrary(path string) (*nativeLibrary, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, err
	}
	return &nativeLibrary{handle: handle}, nil
}

func (l *nativeLibrary) sym(name string) (uintptr, error) {
	if l == nil || l.handle == 0 {
		return 0, fmt.Errorf("steamworks library not loaded")
	}
	return purego.Dlsym(l.handle, name)
}

func (l *nativeLibrary) close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	h := l.handle
	l.handle = 0
	return purego.Dlclose(h)
}
