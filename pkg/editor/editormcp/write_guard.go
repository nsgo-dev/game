package editormcp

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/editor"
)

// beginWriteGuard checks the caller's optimistic base revision and reserves a
// request ID. The returned rollback releases the ID when the edit fails before
// it is committed. All checks happen on the editor goroutine, so a selection
// change cannot slip between validation and mutation.
func (c *Call) beginWriteGuard(ed *editor.Editor, tool, requestID string, baseRevision *uint64, file string) (func(), error) {
	if baseRevision != nil && ed.Revision() != *baseRevision {
		return nil, &EditError{
			File:    file,
			Problem: fmt.Sprintf("base revision %d is stale; the editor is at revision %d", *baseRevision, ed.Revision()),
			Fix:     "reread the scene or selection and retry with its current revision",
		}
	}
	requestID = tool + ":" + requestID
	if requestID == tool+":" {
		return func() {}, nil
	}
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	if _, exists := c.s.writeRequestIDs[requestID]; exists {
		return nil, fmt.Errorf("%s: requestId %q was already applied; use a new requestId", tool, requestID[len(tool)+1:])
	}
	c.s.writeRequestIDs[requestID] = struct{}{}
	return func() {
		c.s.mu.Lock()
		delete(c.s.writeRequestIDs, requestID)
		c.s.mu.Unlock()
	}, nil
}
