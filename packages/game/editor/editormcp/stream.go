package editormcp

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
)

// The resources of the editor's server, which a client reads and subscribes to for the change stream and shared project context.
const (
	// SceneURI is the scene open in the editor as YAML, with every edit not yet saved.
	SceneURI = "editor://scene"
	// SelectionURI is what the person has selected, as selection_get reports it, in JSON.
	SelectionURI = "editor://selection"
	// AgentTurnURI is the latest agent turn submitted from the ⌘L in-place composer or Agent panel,
	// including selection object IDs, open scene YAML, skipPlan flag, and the captured viewport PNG.
	AgentTurnURI = "editor://agent/turn"
	// BriefURI is the open project's brief.yaml as canonical YAML (empty when absent).
	BriefURI = "editor://brief"
	// RulesURI is the open project's AGENTS.md as plain Markdown (empty when absent).
	RulesURI = "editor://rules"
	// ContextURI is the combined JSON context payload (brief.yaml, AGENTS.md, gaps, playtest targets, and pending reviews) the agent reads before every request.
	ContextURI = "editor://context"
)

// The keys of a resource notice's _meta.
const (
	// MetaBy says who made the change a notice reports: "agent" when it came from a tool call, "person" when it came from the editor's own controls.
	MetaBy = "nsgo.dev/by"
	// MetaRevision is the editor's revision (editor.Editor.Revision) after the change.
	MetaRevision = "nsgo.dev/revision"
	// MetaTurnID names the agent turn carried by an editor://agent/turn notification.
	MetaTurnID = "nsgo.dev/turnId"
	// MetaSkipPlan reports whether the notified agent turn is a small in-place ⌘L request that skips the plan.
	MetaSkipPlan = "nsgo.dev/skipPlan"
)

// resources are the server's resources, which the proxy of go tool game mcp lists too.
var resources = []*mcp.Resource{
	{URI: SceneURI, Name: "scene", Title: "Open scene", Description: "The scene open in the editor as YAML, with every edit not yet saved. Subscribe to hear of each change.", MIMEType: "application/yaml"},
	{URI: SelectionURI, Name: "selection", Title: "Selection", Description: "What the person has selected in the editor, as selection_get reports it. Subscribe to hear of each change.", MIMEType: "application/json"},
	{URI: AgentTurnURI, Name: "agent-turn", Title: "Latest agent turn", Description: "The latest request sent from the editor's ⌘L in-place composer or Agent panel, with selection object IDs, open scene, skipPlan flag, and viewport PNG image. Subscribe to hear of each request.", MIMEType: "application/json"},
	{URI: BriefURI, Name: "brief", Title: "Game brief", Description: "The open project's brief.yaml (pitch, genre, platforms, art direction, feel and difficulty, and playtest targets) that the agent reads before every request.", MIMEType: "application/yaml"},
	{URI: RulesURI, Name: "rules", Title: "Project rules", Description: "The open project's AGENTS.md plain Markdown instructions shared by Claude Code, Codex, and the built-in editor agent.", MIMEType: "text/markdown"},
	{URI: ContextURI, Name: "context", Title: "Project agent context", Description: "Combined JSON context containing brief.yaml, AGENTS.md, missing brief sections, playtest targets, and pending document reviews.", MIMEType: "application/json"},
}

// changeStream is what the editor showed when the stream last looked, to tell a change from a notification that changed nothing.
type changeStream struct {
	scene       []byte
	selection   []byte
	turnSeq     uint64
	brief       []byte
	rules       []byte
	context     []byte
	docsRev     uint64
	docsProject string
	docsLang    string
}

// reset records what ed shows now.
func (c *changeStream) reset(ed *editor.Editor) {
	c.scene, _ = sceneYAML(ed)
	c.selection = selectionJSON(ed)
	c.turnSeq = ed.AgentTurnSeq()
	c.brief = []byte(briefResourceText(ed))
	c.rules = []byte(ed.ProjectRules())
	c.context = contextJSON(ed)
	c.docsRev = ed.AgentDocsRevision()
	c.docsProject = ed.ProjectPath
	c.docsLang = ed.ResolvedLanguage()
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
	s.sdk.AddResource(resources[2], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var (
			data []byte
			png  []byte
		)
		err := s.Editor(ctx, func(ed *editor.Editor) error {
			data, png, _, _ = agentTurnResourceJSON(ed)
			return nil
		})
		if err != nil {
			return nil, err
		}
		contents := []*mcp.ResourceContents{
			{URI: AgentTurnURI, MIMEType: "application/json", Text: string(data)},
		}
		if len(png) > 0 {
			contents = append(contents, &mcp.ResourceContents{
				URI:      AgentTurnURI + "/viewport.png",
				MIMEType: "image/png",
				Blob:     png,
			})
		}
		return &mcp.ReadResourceResult{Contents: contents}, nil
	})
	s.sdk.AddResource(resources[3], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var text string
		err := s.Editor(ctx, func(ed *editor.Editor) error {
			text = briefResourceText(ed)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: BriefURI, MIMEType: "application/yaml", Text: text}}}, nil
	})
	s.sdk.AddResource(resources[4], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var text string
		err := s.Editor(ctx, func(ed *editor.Editor) error {
			text = ed.ProjectRules()
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: RulesURI, MIMEType: "text/markdown", Text: text}}}, nil
	})
	s.sdk.AddResource(resources[5], func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var data []byte
		err := s.Editor(ctx, func(ed *editor.Editor) error {
			data = contextJSON(ed)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: ContextURI, MIMEType: "application/json", Text: string(data)}}}, nil
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

// subscribed reports whether a session still connected subscribes to the scene, selection, or agent turns.
func (s *Server) subscribed() (scene, selection, agentTurn bool) {
	scene, selection, agentTurn, _, _, _ = s.subscribedAll()
	return scene, selection, agentTurn
}

// subscribedAll reports which resources any still-connected session subscribes to,
// dropping subscriptions of sessions that ended without unsubscribing.
func (s *Server) subscribedAll() (scene, selection, agentTurn, brief, rules, ctxRes bool) {
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
		agentTurn = agentTurn || uris[AgentTurnURI]
		brief = brief || uris[BriefURI]
		rules = rules || uris[RulesURI]
		ctxRes = ctxRes || uris[ContextURI]
	}
	return scene, selection, agentTurn, brief, rules, ctxRes
}

// changed runs on the editor's goroutine after every change to the editor's state: it follows the project to its endpoint and queues a notice for each subscribed resource whose content changed, saying whether the agent or the person changed it.
func (s *Server) changed() {
	if s.opened && s.ed.ProjectPath != s.projectPath {
		s.follow()
	}
	wantScene, wantSelection, wantTurn, wantBrief, wantRules, wantContext := s.subscribedAll()
	if !wantScene && !wantSelection && !wantTurn && !wantBrief && !wantRules && !wantContext {
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
	if wantTurn {
		if seq := s.ed.AgentTurnSeq(); seq != s.stream.turnSeq {
			s.stream.turnSeq = seq
			turnMeta := mcp.Meta{MetaBy: by, MetaRevision: s.ed.Revision()}
			if turn, ok := s.ed.LatestAgentTurn(); ok {
				turnMeta[MetaTurnID] = turn.ID
				turnMeta[MetaSkipPlan] = turn.SkipPlan
			}
			s.queue(AgentTurnURI, turnMeta)
		}
	}
	if wantBrief || wantRules || wantContext {
		docsRev := s.ed.AgentDocsRevision()
		docsProj := s.ed.ProjectPath
		docsLang := s.ed.ResolvedLanguage()
		if docsRev != s.stream.docsRev || docsProj != s.stream.docsProject || docsLang != s.stream.docsLang {
			s.stream.docsRev = docsRev
			s.stream.docsProject = docsProj
			s.stream.docsLang = docsLang
			if wantBrief {
				if data := []byte(briefResourceText(s.ed)); !bytes.Equal(data, s.stream.brief) {
					s.stream.brief = data
					s.queue(BriefURI, meta)
				}
			}
			if wantRules {
				if data := []byte(s.ed.ProjectRules()); !bytes.Equal(data, s.stream.rules) {
					s.stream.rules = data
					s.queue(RulesURI, meta)
				}
			}
			if wantContext {
				if data := contextJSON(s.ed); !bytes.Equal(data, s.stream.context) {
					s.stream.context = data
					s.queue(ContextURI, meta)
				}
			}
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
