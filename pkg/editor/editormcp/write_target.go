package editormcp

import (
	"context"
	"errors"

	"github.com/nsgo-dev/game/pkg/editor"
)

var errWriteNeedsExplicitTarget = errors.New("write target is not pinned; pass id (selection is context, never a write target)")

func resolveWriteID(ctx context.Context, c *Call, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	var id string
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		turn, ok := ed.LatestAgentTurn()
		if !ok || len(turn.Context.SelectionIDs) == 0 {
			return errWriteNeedsExplicitTarget
		}
		id = turn.Context.SelectionIDs[0]
		return nil
	})
	return id, err
}

func resolveWriteIDs(ctx context.Context, c *Call, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return append([]string(nil), explicit...), nil
	}
	var ids []string
	err := c.Editor(ctx, func(ed *editor.Editor) error {
		turn, ok := ed.LatestAgentTurn()
		if !ok || len(turn.Context.SelectionIDs) == 0 {
			return errWriteNeedsExplicitTarget
		}
		ids = append([]string(nil), turn.Context.SelectionIDs...)
		return nil
	})
	return ids, err
}
