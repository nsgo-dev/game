package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nsgo-dev/game/pkg/editor/diaglog"
)

// ExportDiagnostics writes a redacted, bounded diagnostics archive beside the editor cache and returns its path.
func (e *Editor) ExportDiagnostics() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	logDir := filepath.Join(cache, "nsgo-game-editor", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	redactor := diaglog.Redactor{Home: homeDir(), Project: e.ProjectPath, Data: filepath.Join(cache, "nsgo-game-editor")}
	out := filepath.Join(logDir, fmt.Sprintf("nsgo-game-editor-diagnostics-%s.zip", time.Now().UTC().Format("20060102T150405Z")))
	m := diaglog.Manifest{Version: 1, OS: runtime.GOOS, Arch: runtime.GOARCH, Entries: []diaglog.Entry{}}
	return out, diaglog.Export(logDir, out, m, redactor)
}

var homeDir = func() string { h, _ := os.UserHomeDir(); return h }
