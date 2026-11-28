package editormcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/nsgo-dev/game/pkg/editor"
)

// maxWriteRequests is how many completed requestId results the server keeps for replay; the oldest are forgotten first.
const maxWriteRequests = 256

// writeRequest is one requestId a write tool has seen: the fingerprint of its arguments and, once it succeeded, its result.
type writeRequest struct {
	fingerprint string
	result      *Result
}

// sceneVersion is a short content hash of the open scene as the editor would save it, which scene_read and selection_get report and the write tools' baseVersion compares. Selection, camera and other editor state outside the scene document do not change it.
func sceneVersion(ed *editor.Editor) (string, error) {
	data, err := sceneYAML(ed)
	if err != nil {
		return "", err
	}
	return contentVersion(data), nil
}

// contentVersion is the short hash of a scene's saved YAML that sceneVersion reports.
func contentVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// checkBaseVersion refuses a write whose baseVersion differs from the open scene's current version. It runs on the editor's goroutine right before the write applies, so nothing can change the scene between the check and the edit.
func checkBaseVersion(ed *editor.Editor, base *string, file string) error {
	if base == nil {
		return nil
	}
	current, err := sceneVersion(ed)
	if err != nil {
		return err
	}
	if current == *base {
		return nil
	}
	return &EditError{
		File:    file,
		Problem: fmt.Sprintf("changed since you read it: baseVersion %q is not the scene's current version %q", *base, current),
		Fix:     "read it again with scene_read or selection_get and retry with the version it reports",
	}
}

// beginWriteRequest looks up requestID for tool. With no ID it returns nothing to replay and no-op finish. A completed call with the same ID and identical arguments returns its stored result to replay; the same ID with different arguments, or one still running, is refused. Otherwise the ID is reserved, and finish must be called with the call's result (stored for replay) or nil (the call failed, which releases the ID).
func (s *Server) beginWriteRequest(tool, requestID string, args any) (replay *Result, finish func(*Result), err error) {
	if requestID == "" {
		return nil, func(*Result) {}, nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", tool, err)
	}
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	key := tool + "\x00" + requestID
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.writeRequests[key]; ok {
		switch {
		case prev.fingerprint != fingerprint:
			return nil, nil, fmt.Errorf("%s: requestId %q was already used for a call with different arguments; use a new requestId for a new write", tool, requestID)
		case prev.result == nil:
			return nil, nil, fmt.Errorf("%s: a call with requestId %q is still running; retry once it finishes", tool, requestID)
		}
		out := *prev.result
		return &out, nil, nil
	}
	req := &writeRequest{fingerprint: fingerprint}
	s.writeRequests[key] = req
	return nil, func(res *Result) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if res == nil {
			delete(s.writeRequests, key)
			return
		}
		stored := *res
		req.result = &stored
		s.writeRequestOrder = append(s.writeRequestOrder, key)
		for len(s.writeRequestOrder) > maxWriteRequests {
			delete(s.writeRequests, s.writeRequestOrder[0])
			s.writeRequestOrder = s.writeRequestOrder[1:]
		}
	}, nil
}
