package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestAgentThreadFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.AgentThreadDoc]{
		Doc:    "editor#thread",
		Decode: editor.ParseAgentThread,
		Encode: editor.EncodeAgentThread,
		Files:  []string{"testdata/default.thread.yaml"},
	})
}
