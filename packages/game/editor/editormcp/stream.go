package editormcp

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// The resources of the editor's server, which a client reads and subscribes to for the change stream.
const (
	// SceneURI is the scene open in the editor as YAML, with every edit not yet saved.
	SceneURI = "editor://scene"
	// SelectionURI is what the person has selected, as selection_get reports it, in JSON.
	SelectionURI = "editor://selection"
)

// The keys of a resource notice's _meta.
const (
	// MetaBy says who made the change a notice reports: "agent" when it came from a tool call, "person" when it came from the editor's own controls.
	MetaBy = "nsgo.dev/by"
	// MetaRevision is the editor's revision (editor.Editor.Revision) after the change.
	MetaRevision = "nsgo.dev/revision"
)

// resources are the server's resources, which the proxy of go tool game mcp lists too.
var resources = []*mcp.Resource{
	{URI: SceneURI, Name: "scene", Title: "Open scene", Description: "The scene open in the editor as YAML, with every edit not yet saved. Subscribe to hear of each change.", MIMEType: "application/yaml"},
	{URI: SelectionURI, Name: "selection", Title: "Selection", Description: "What the person has selected in the editor, as selection_get reports it. Subscribe to hear of each change.", MIMEType: "application/json"},
}

// changeStream is what the editor showed when the stream last looked, to tell a change from a notification that changed nothing.
type changeStream struct {
	scene     []byte
	selection []byte
}

// reset records what ed shows now.
func (c *changeStream) reset(ed *editor.Editor) {
	c.scene, _ = sceneYAML(ed)
	c.selection = selectionJSON(ed)
}

// addResources adds the resources and their read handlers to the SDK server.
func (s *Server) addResources() {
	s.sdk.AddResource(resources[0], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var data []byte
		err := s.Editor(ctx, func(ed *editor.Editor) (err error) {
			data, err = sceneYAML(ed)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: SceneURI, MIMEType: "application/yaml", Text: string(data)}}}, nil
	})
	s.sdk.AddResource(resources[1], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var data []byte
		err := s.Editor(ctx, func(ed *editor.Editor) error {
			data = selectionJSON(ed)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: SelectionURI, MIMEType: "application/json", Text: string(data)}}}, nil
	})
}

// subscribe records a session's subscription to a resource.
func (s *Server) subscribe(_ context.Context, req *mcp.SubscribeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[req.Session] == nil {
		s.subs[req.Session] = map[string]bool{}
	}
	s.subs[req.Session][req.Params.URI] = true
	return nil
}

// unsubscribe forgets a session's subscription to a resource.
func (s *Server) unsubscribe(_ context.Context, req *mcp.UnsubscribeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs[req.Session], req.Params.URI)
	if len(s.subs[req.Session]) == 0 {
		delete(s.subs, req.Session)
	}
	return nil
}

// subscribed reports whether a session still connected subscribes to the scene and to the selection, first dropping the subscriptions of sessions that ended without unsubscribing.
func (s *Server) subscribed() (scene, selection bool) {
	live := map[*mcp.ServerSession]bool{}
	for ss := range s.sdk.Sessions() {
		live[ss] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ss, uris := range s.subs {
		if !live[ss] {
			delete(s.subs, ss)
			continue
		}
		scene = scene || uris[SceneURI]
		selection = selection || uris[SelectionURI]
	}
	return scene, selection
}

// changed runs on the editor's goroutine after every change to the editor's state: it follows the project to its endpoint and queues a notice for each subscribed resource whose content changed, saying whether the agent or the person changed it.
func (s *Server) changed() {
	if s.opened && s.ed.ProjectPath != s.projectPath {
		s.follow()
	}
	wantScene, wantSelection := s.subscribed()
	if !wantScene && !wantSelection {
		return
	}
	by := "person"
	if s.agent > 0 {
		by = "agent"
	}
	meta := mcp.Meta{MetaBy: by, MetaRevision: s.ed.Revision()}
	if wantScene {
		if data, err := sceneYAML(s.ed); err == nil && !bytes.Equal(data, s.stream.scene) {
			s.stream.scene = data
			s.queue(SceneURI, meta)
		}
	}
	if wantSelection {
		if data := selectionJSON(s.ed); !bytes.Equal(data, s.stream.selection) {
			s.stream.selection = data
			s.queue(SelectionURI, meta)
		}
	}
}

// queue records a notice for uri, replacing one not sent yet, so a burst of edits sends one notice with the latest revision.
func (s *Server) queue(uri string, meta mcp.Meta) {
	s.pendingMu.Lock()
	s.pending[uri] = meta
	s.pendingMu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// sendNotices sends the queued notices off the editor's goroutine, as the SDK writes them to every subscribed session, until the server closes.
func (s *Server) sendNotices() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		}
		s.pendingMu.Lock()
		batch := s.pending
		s.pending = map[string]mcp.Meta{}
		s.pendingMu.Unlock()
		for _, r := range resources {
			if meta, ok := batch[r.URI]; ok {
				_ = s.sdk.ResourceUpdated(context.Background(), &mcp.ResourceUpdatedNotificationParams{URI: r.URI, Meta: meta})
			}
		}
	}
}

// selectionJSON is the selection as selection_get reports it, encoded for the change stream.
func selectionJSON(ed *editor.Editor) []byte {
	data, err := json.Marshal(readSelection(ed))
	if err != nil {
		return nil
	}
	return data
}
