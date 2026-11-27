package modding

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestWebTargetRequiresWebUnisolated(t *testing.T) {
	saved := pageAuthorityTarget
	pageAuthorityTarget = true
	t.Cleanup(func() { pageAuthorityTarget = saved })

	fsys := fstest.MapFS{
		"hello/mod.yaml": {Data: []byte("manifestVersion: 1\nid: hello\nname: Hello\nversion: 1.0.0\nentry: mod.js\n")},
		"hello/mod.js":   {Data: []byte(`export {};`)},
	}
	for _, optIn := range []bool{false, true} {
		var loadErr error
		var state ModState
		nsgotest.Start(t, func(cx *core.Scope) core.Node {
			mgr := NewManager(cx, ManagerOptions{WebUnisolated: optIn})
			if _, err := mgr.DiscoverFS(fsys); err != nil {
				t.Fatalf("DiscoverFS: %v", err)
			}
			loadErr = mgr.Load("hello")
			info, _ := mgr.Info("hello")
			state = info.State
			return core.View{}
		})
		if optIn {
			if loadErr != nil || state != StateLoaded {
				t.Fatalf("with WebUnisolated: Load = %v, state %v", loadErr, state)
			}
			continue
		}
		if !errors.Is(loadErr, ErrWebUnisolated) || state != StateFailed {
			t.Fatalf("without WebUnisolated: Load = %v, state %v; want ErrWebUnisolated and failed", loadErr, state)
		}
	}
}
