package liveops

import (
	"testing"

	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestSyncStateExamplesOnFormatsPage loads each example of the sync state's entry on the File Formats page as CloudSyncer loads a slot's sync state, since the reader is unexported.
func TestSyncStateExamplesOnFormatsPage(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	s := &CloudSyncer{state: app.Storage(), prefix: DefaultSyncStateKey}
	for _, ex := range formattest.Examples(t, "online/liveops#sync", "json") {
		if err := app.Storage().SetItem(s.stateKey("main"), string(ex.Data)); err != nil {
			t.Fatal(err)
		}
		st, err := s.loadState("main")
		if err != nil {
			t.Errorf("%s:%d: the example does not load: %v", ex.Page, ex.Line, err)
			continue
		}
		if st.Version != syncStateVersion || st.Hash == "" {
			t.Errorf("%s:%d: the example loads as %+v, want a synced slot of version %d", ex.Page, ex.Line, st, syncStateVersion)
		}
	}
}
