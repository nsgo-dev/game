package screen

import "github.com/nsgo-dev/nsgo/core"

// SetFullScreenHook replaces the window's full-screen request with fn until restore is called. nsgotest records a full-screen request with neither its action nor its display ID, so tests observe the request here.
func SetFullScreenHook(fn func(cx *core.Scope, on bool, displayID string)) (restore func()) {
	old := setFullScreen
	setFullScreen = fn
	return func() { setFullScreen = old }
}
