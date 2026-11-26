//go:build ios || android || js

package editor

import (
	"context"
	"errors"
)

func defaultCLIAgentRunner() AgentRunner {
	return AgentRunnerFunc(func(context.Context, AgentRequest, func(AgentEvent)) error {
		return errors.New("editor: running a local agent CLI is only supported on desktop")
	})
}
