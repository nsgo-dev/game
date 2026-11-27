package editor

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/internal/gotoolchain"
)

// GoDownloadURL is the official Go download page opened from the Start Screen notice.
const GoDownloadURL = gotoolchain.DownloadURL

// DefaultMinGoVersion is the default minimum Go version required by the editor's project templates.
const DefaultMinGoVersion = gotoolchain.DefaultMinVersion

// ErrGoToolchainUnavailable is returned when an editor action that needs `go` (such as Create or Play)
// is invoked while no compatible Go toolchain is installed.
var ErrGoToolchainUnavailable = errors.New("editor: Go toolchain unavailable")

// GoToolchainStatus reports whether a usable Go toolchain is available for creating projects,
// running games, building assets, and running checks.
type GoToolchainStatus struct {
	// Found reports whether a `go` executable was located.
	Found bool
	// Binary is the resolved path to the `go` executable, or empty when not found.
	Binary string
	// Version is the Go version reported by `go version` without the leading "go" prefix (for example "1.27.4").
	Version string
	// MinVersion is the minimum Go version required by the project templates (for example "1.27.3").
	MinVersion string
	// TooOld reports whether Go was found but is older than MinVersion.
	TooOld bool
	// Err holds any error encountered while checking `go version`.
	Err string
}

// OK reports whether a Go binary was found and satisfies MinVersion.
func (s GoToolchainStatus) OK() bool {
	return s.Found && !s.TooOld && s.Err == ""
}

type goToolchainState struct {
	mu         sync.Mutex
	minVersion string
	checked    bool
	status     GoToolchainStatus
}

// MinGoVersion returns the minimum Go version checked by the editor (defaults to DefaultMinGoVersion).
func (e *Editor) MinGoVersion() string {
	if e == nil {
		return DefaultMinGoVersion
	}
	e.goToolchain.mu.Lock()
	v := e.goToolchain.minVersion
	e.goToolchain.mu.Unlock()
	if strings.TrimSpace(v) == "" {
		return DefaultMinGoVersion
	}
	return v
}

// SetMinGoVersion sets the minimum Go version checked against the found Go toolchain
// (for example, the highest `go` line required by the embedded project templates).
func (e *Editor) SetMinGoVersion(v string) {
	if e == nil {
		return
	}
	norm := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "go"))
	if norm == "" {
		norm = DefaultMinGoVersion
	}
	e.goToolchain.mu.Lock()
	changed := e.goToolchain.minVersion != norm
	e.goToolchain.minVersion = norm
	if changed {
		e.goToolchain.checked = false
	}
	e.goToolchain.mu.Unlock()
	if changed {
		e.notify()
	}
}

func goToolchainStatusFrom(st gotoolchain.Status) GoToolchainStatus {
	return GoToolchainStatus{
		Found:      st.Found,
		Binary:     st.Binary,
		Version:    st.Version,
		MinVersion: st.MinVersion,
		TooOld:     st.TooOld,
		Err:        st.Err,
	}
}

func (e *Editor) probeGoToolchain(refresh bool) GoToolchainStatus {
	minVer := e.MinGoVersion()
	if e != nil && e.GoToolchainProbe != nil {
		st := e.GoToolchainProbe(minVer)
		if strings.TrimSpace(st.MinVersion) == "" {
			st.MinVersion = minVer
		}
		if st.Found && st.Err == "" && st.Version != "" && !st.TooOld {
			if gotoolchain.CompareVersion(st.Version, st.MinVersion) < 0 {
				st.TooOld = true
			}
		}
		return st
	}
	if !canRunGameDesktop {
		return GoToolchainStatus{
			Found:      true,
			MinVersion: minVer,
		}
	}
	if refresh {
		return goToolchainStatusFrom(gotoolchain.RefreshMin(minVer))
	}
	return goToolchainStatusFrom(gotoolchain.ResolveMin(minVer))
}

// GoToolchainStatus returns the cached Go toolchain status, probing lazily on first call.
func (e *Editor) GoToolchainStatus() GoToolchainStatus {
	if e == nil {
		return GoToolchainStatus{MinVersion: DefaultMinGoVersion}
	}
	e.goToolchain.mu.Lock()
	if e.goToolchain.checked {
		st := e.goToolchain.status
		e.goToolchain.mu.Unlock()
		return st
	}
	e.goToolchain.mu.Unlock()

	st := e.probeGoToolchain(false)
	e.goToolchain.mu.Lock()
	e.goToolchain.status = st
	e.goToolchain.checked = true
	e.goToolchain.mu.Unlock()
	return st
}

// GoToolchainOK reports whether a compatible Go toolchain is available.
func (e *Editor) GoToolchainOK() bool {
	return e.GoToolchainStatus().OK()
}

// RetryGoToolchainCheck invalidates the cached Go toolchain lookup, re-probes PATH, standard
// install directories, and the login shell PATH, updates the UI, and returns the fresh status.
func (e *Editor) RetryGoToolchainCheck() GoToolchainStatus {
	if e == nil {
		return GoToolchainStatus{MinVersion: DefaultMinGoVersion}
	}
	st := e.probeGoToolchain(true)
	e.goToolchain.mu.Lock()
	e.goToolchain.status = st
	e.goToolchain.checked = true
	e.goToolchain.mu.Unlock()
	e.notify()
	return st
}

// OpenGoDownloadPage opens https://go.dev/dl/ in the host browser (via Editor.OpenBrowserURL when set).
func (e *Editor) OpenGoDownloadPage() error {
	opener := defaultOpenBrowserURL
	if e != nil && e.OpenBrowserURL != nil {
		opener = e.OpenBrowserURL
	}
	return opener(GoDownloadURL)
}

func (e *Editor) goToolchainError() error {
	st := e.GoToolchainStatus()
	if st.OK() {
		return nil
	}
	minVer := st.MinVersion
	if minVer == "" {
		minVer = DefaultMinGoVersion
	}
	if st.TooOld && st.Version != "" {
		return fmt.Errorf("%w: NSGo Game Editor needs Go %s or later (found Go %s)", ErrGoToolchainUnavailable, minVer, st.Version)
	}
	if st.Err != "" {
		return fmt.Errorf("%w: NSGo Game Editor needs Go %s or later (%s)", ErrGoToolchainUnavailable, minVer, st.Err)
	}
	return fmt.Errorf("%w: NSGo Game Editor needs Go %s or later", ErrGoToolchainUnavailable, minVer)
}
