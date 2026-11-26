package editor_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/internal/formattest"
)

func TestAgentThreadYAMLFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*editor.AgentThread]{
		Doc:    "editor#agent-thread",
		Decode: editor.ParseAgentThreadYAML,
		Encode: editor.EncodeAgentThreadYAML,
		Files:  []string{"testdata/agent_thread.yaml"},
	})
}
