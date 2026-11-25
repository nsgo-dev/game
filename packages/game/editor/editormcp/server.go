package editormcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/playtest"
)

// ServerName is the name the editor's server gives clients when they connect.
const ServerName = "nsgo-game-editor"

// instructions is what the server tells clients when they connect.
const instructions = `Reads and edits the game project open in the NSGo Game Editor, as the person using the editor does: through the same editor commands as its inspector, scene tree and viewport, so every edit is one undo step the person can see and take back.

Start with selection_get for what the person has selected and the scene open in the editor, and scene_read for any scene, prefab, data or project file, with line numbers. Change the open scene with scene_edit: add, move, set and remove objects, components and properties, all of one call kept as one undo step or, when the editor refuses any of them, none at all. Edits stay unsaved in the editor until the person saves. viewport_image is a PNG of the viewport, and game_check runs the checks of the Problems panel over the project's files on disk.

Resources editor://scene and editor://selection hold the open scene and the selection; subscribe to them to hear of each change, whose _meta says whether the person or the agent made it.`

// Action is what a tool does to the project, which the editor's permission rules decide on: reading changes nothing, editing changes files the person can review and undo, and running starts work such as a check or a playtest.
type Action string

// The actions a tool takes.
const (
	// ActionRead reads the project or the editor's state and changes nothing.
	ActionRead Action = "read"
	// ActionEdit changes the open project through editor commands.
	ActionEdit Action = "edit"
	// ActionRun runs work over the project, such as game check or a playtest, and changes no file.
	ActionRun Action = "run"
)

// Tool is one tool of the editor's server: the five this package adds (scene_read, scene_edit, selection_get, viewport_image and game_check) and those other parts of the editor add with Server.AddTool, so that every editor action with a button or a shortcut has a tool.
type Tool struct {
	// Name is the tool's MCP name, such as scene_read: letters, digits, '_', '-' and '.', unique on the server.
	Name string
	// Title is a short human-readable name, such as "Read a file".
	Title string
	// Description says what the tool does, for the agent.
	Description string
	// InputSchema is the JSON Schema of the tool's arguments, an object schema; nil takes any object.
	InputSchema json.RawMessage
	// Action is what the tool does to the project.
	Action Action
	// Handler runs the tool on a goroutine of the server, never the editor's: it reaches the editor through Call.Editor, which runs a function on the editor's goroutine, so it may wait for work the editor does elsewhere, such as a project check, without stopping the editor.
	Handler func(ctx context.Context, c *Call) (*Result, error)
}

// Call is one call of a tool: its arguments and the server it runs on.
type Call struct {
	// Args is the call's JSON object of arguments.
	Args json.RawMessage
	s    *Server
	// onEditor reports that the call runs on the editor's goroutine already, as an automation tool's handler does.
	onEditor bool
}

// Editor runs fn on the editor's goroutine and returns its error; see Server.Editor.
func (c *Call) Editor(ctx context.Context, fn func(ed *editor.Editor) error) error {
	if c.onEditor {
		return c.s.asAgent(fn)
	}
	return c.s.Editor(ctx, fn)
}

// Decode decodes the call's arguments into v, refusing a field v does not have, so a misspelled argument is an error the agent reads rather than one silently dropped.
func (c *Call) Decode(v any) error {
	return decodeArgs(c.Args, v)
}

// Result is what a tool returns: text for the agent, an optional JSON value with the same facts for programs, and an optional PNG.
type Result struct {
	// Text is what the agent reads.
	Text string
	// Structured is a JSON-encodable value with what Text says, or nil.
	Structured any
	// PNG is an image the tool returns, such as viewport_image's, or nil.
	PNG []byte
}

// Options configure a Server.
type Options struct {
	// Run hands fn to the editor's goroutine and returns at once, as core.Scope.Post does for an editor shown in a window; the server waits for fn to run. When nil, the server runs fn on the calling goroutine, one at a time, for an editor no UI drives, as in a test or a script.
	Run func(fn func())
	// RunPlaytest optionally overrides how game_playtest executes a playtest request, for tests or in-process runners. When nil, game_playtest runs `go tool nsgo test` in the project root.
	RunPlaytest func(ctx context.Context, req PlaytestRunRequest) (*playtest.Report, error)
}

// Server is the editor's MCP server. It serves the project open in an Editor on the project's local endpoint (Open), or on any MCP transport (Connect), with a registry of tools that the editor's parts join (AddTool), and a stream of changes to the open scene and the selection as resource notifications.
type Server struct {
	ed          *editor.Editor
	run         func(func())
	runPlaytest func(ctx context.Context, req PlaytestRunRequest) (*playtest.Report, error)
	sdk         *mcp.Server

	// inline serializes the calls run inline when Options.Run is nil.
	inline sync.Mutex

	mu              sync.Mutex
	tools           map[string]Tool
	nextReviewID    int
	playtestReviews []storedPlaytestReview
	// subs holds the resource URIs each session subscribes to. A session that ends without unsubscribing is dropped the next time changed looks (subscribed), so its subscriptions stop costing the editor anything.
	subs map[*mcp.ServerSession]map[string]bool
	// listener and root are the endpoint Open serves on and the project root it belongs to.
	listener net.Listener
	root     string
	openErr  error
	conns    map[net.Conn]struct{}
	closed   bool
	done     chan struct{}
	// wg tracks the goroutines serving connections and sending notifications.
	wg sync.WaitGroup

	// The rest belong to the editor's goroutine.
	agent  int
	unsub  func()
	opened bool
	// projectPath is the editor's ProjectPath when follow last looked, so a change elsewhere does not search for the project again.
	projectPath string
	stream      changeStream

	// pending holds the resource notices not sent yet, by URI, and wake tells the sender there are some.
	pendingMu sync.Mutex
	pending   map[string]mcp.Meta
	wake      chan struct{}
}

// New returns a server for ed with the built-in tools. Call it on the editor's goroutine, which is the one Options.Run hands functions to.
func New(ed *editor.Editor, opts Options) *Server {
	s := &Server{
		ed:          ed,
		run:         opts.Run,
		runPlaytest: opts.RunPlaytest,
		tools:       map[string]Tool{},
		subs:        map[*mcp.ServerSession]map[string]bool{},
		conns:       map[net.Conn]struct{}{},
		done:        make(chan struct{}),
		pending:     map[string]mcp.Meta{},
		wake:        make(chan struct{}, 1),
	}
	s.sdk = mcp.NewServer(&mcp.Implementation{Name: ServerName, Title: "NSGo Game Editor"}, &mcp.ServerOptions{
		Instructions:       instructions,
		SubscribeHandler:   s.subscribe,
		UnsubscribeHandler: s.unsubscribe,
		Capabilities: &mcp.ServerCapabilities{
			Tools:     &mcp.ToolCapabilities{ListChanged: true},
			Resources: &mcp.ResourceCapabilities{Subscribe: true, ListChanged: true},
		},
	})
	for _, t := range builtinTools() {
		if err := s.AddTool(t); err != nil {
			panic(err)
		}
	}
	s.addResources()
	s.stream.reset(ed)
	s.unsub = ed.Subscribe(s.changed)
	s.wg.Add(1)
	go s.sendNotices()
	return s
}

// AddTool adds t to the server, which tells connected clients the tool list changed. It refuses a tool without a name or a handler, with an invalid input schema, or with a name already on the server.
func (s *Server) AddTool(t Tool) error {
	if t.Name == "" || !validToolName(t.Name) {
		return fmt.Errorf("editormcp: tool name %q must be 1 to 128 letters, digits, '_', '-' or '.'", t.Name)
	}
	if t.Handler == nil {
		return fmt.Errorf("editormcp: tool %s needs a handler", t.Name)
	}
	schema := t.InputSchema
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(schema, &obj); err != nil || obj.Type != "object" {
		return fmt.Errorf("editormcp: tool %s: input schema must be a JSON Schema object with type \"object\"", t.Name)
	}
	t.InputSchema = slices.Clone(schema)
	s.mu.Lock()
	if _, dup := s.tools[t.Name]; dup {
		s.mu.Unlock()
		return fmt.Errorf("editormcp: tool %s is already on the server", t.Name)
	}
	s.tools[t.Name] = t
	s.mu.Unlock()
	s.sdk.AddTool(sdkTool(t), s.handler(t))
	return nil
}

// RemoveTool removes the tool named name, if the server has it, which tells connected clients the tool list changed.
func (s *Server) RemoveTool(name string) {
	s.mu.Lock()
	_, ok := s.tools[name]
	delete(s.tools, name)
	s.mu.Unlock()
	if ok {
		s.sdk.RemoveTools(name)
	}
}

// Tools returns the server's tools sorted by name.
func (s *Server) Tools() []Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Tool, 0, len(s.tools))
	for _, name := range slices.Sorted(maps.Keys(s.tools)) {
		out = append(out, s.tools[name])
	}
	return out
}

// Editor runs fn on the editor's goroutine with the editor and returns fn's error, waiting until fn has run. Edits fn makes through the editor's methods count as the agent's in the change stream. When ctx ends before fn starts, fn does not run and Editor returns ctx's error; once fn has started, Editor waits for it. A panic in fn comes back as an error.
func (s *Server) Editor(ctx context.Context, fn func(ed *editor.Editor) error) error {
	if s.run == nil {
		s.inline.Lock()
		defer s.inline.Unlock()
		return s.asAgent(fn)
	}
	const (
		pending int32 = iota
		started
		abandoned
	)
	var state atomic.Int32
	done := make(chan error, 1)
	s.run(func() {
		if !state.CompareAndSwap(pending, started) {
			return
		}
		done <- s.asAgent(fn)
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if state.CompareAndSwap(pending, abandoned) {
			return ctx.Err()
		}
		return <-done
	case <-s.done:
		if state.CompareAndSwap(pending, abandoned) {
			return errors.New("editormcp: the editor's server is closed")
		}
		return <-done
	}
}

// asAgent runs fn as the agent's, recovering a panic as an error.
func (s *Server) asAgent(fn func(ed *editor.Editor) error) (err error) {
	s.agent++
	defer func() {
		s.agent--
		if failure := recover(); failure != nil {
			err = fmt.Errorf("editormcp: %v", failure)
		}
	}()
	return fn(s.ed)
}

// Connect serves one MCP session on t until the client disconnects or the server closes, as Open does for each client of the endpoint and as a test or an in-process agent does with mcp.NewInMemoryTransports.
func (s *Server) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	return s.sdk.Connect(ctx, t, nil)
}

// Open starts serving on the endpoint of the project open in the editor (Endpoint) and moves to the new project's endpoint whenever the editor opens another, until Close. Call it on the editor's goroutine. Without a project it serves nothing until one opens. It returns the error opening the endpoint, such as ErrInUse when another editor has the same project open, which Status reports too.
func (s *Server) Open() error {
	s.opened = true
	s.follow()
	_, err := s.Status()
	return err
}

// Status returns the endpoint the server listens on, empty when it serves none, and why it does not when opening it failed.
func (s *Server) Status() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return "", s.openErr
	}
	return s.listener.Addr().String(), nil
}

// follow serves on the open project's endpoint, closing the one of a project the editor no longer has open and the connections its clients made.
func (s *Server) follow() {
	if !s.opened {
		return
	}
	s.projectPath = s.ed.ProjectPath
	root := ""
	if s.ed.ProjectPath != "" {
		if r, err := ProjectRoot(s.ed.ProjectPath); err == nil {
			root = r
		}
	}
	s.mu.Lock()
	if s.closed || (root == s.root && (s.listener != nil || s.openErr != nil)) {
		s.mu.Unlock()
		return
	}
	old := s.listener
	s.listener, s.root, s.openErr = nil, root, nil
	// Every client of the endpoint came for the project the editor no longer has open, and must not edit the next one.
	stale := slices.Collect(maps.Keys(s.conns))
	s.mu.Unlock()
	if old != nil {
		closeListener(old)
	}
	for _, c := range stale {
		_ = c.Close()
	}
	if root == "" {
		return
	}
	l, err := Listen(root)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if l != nil {
			closeListener(l)
		}
		return
	}
	if err != nil {
		s.openErr = err
		return
	}
	s.listener = l
	s.wg.Add(1)
	go s.accept(l)
}

// closeListener stops l and removes its socket file.
func closeListener(l net.Listener) {
	path := l.Addr().String()
	_ = l.Close()
	if filepath.IsAbs(path) {
		_ = os.Remove(path)
	}
}

// accept serves each client of l until l closes.
func (s *Server) accept(l net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.conns[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serveConn(conn)
	}
}

// serveConn serves one MCP session on conn.
func (s *Server) serveConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	ss, err := s.sdk.Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		return
	}
	_ = ss.Wait()
}

// Close stops serving: it closes the endpoint and every session, removes the socket file and stops following the editor. Call it on the editor's goroutine; the editor stays as it is.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	l := s.listener
	s.listener = nil
	conns := slices.Collect(maps.Keys(s.conns))
	close(s.done)
	s.mu.Unlock()
	if s.unsub != nil {
		s.unsub()
		s.unsub = nil
	}
	if l != nil {
		closeListener(l)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	for ss := range s.sdk.Sessions() {
		_ = ss.Close()
	}
	s.wg.Wait()
	return nil
}

// handler adapts t to the SDK's tool handler: a tool's error is a tool result the agent reads, never a protocol error.
func (s *Server) handler(t Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := s.call(ctx, t, req.Params.Arguments, false)
		return sdkResult(res, err), nil
	}
}

// call runs t with args, recovering a panic as an error; onEditor says the caller is on the editor's goroutine.
func (s *Server) call(ctx context.Context, t Tool, args json.RawMessage, onEditor bool) (res *Result, err error) {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	defer func() {
		if failure := recover(); failure != nil {
			res, err = nil, fmt.Errorf("editormcp: tool %s failed: %v", t.Name, failure)
		}
	}()
	return t.Handler(ctx, &Call{Args: args, s: s, onEditor: onEditor})
}

// sdkTool is t as the SDK lists it.
func sdkTool(t Tool) *mcp.Tool {
	readOnly := t.Action == ActionRead
	destructive := false
	return &mcp.Tool{
		Name:        t.Name,
		Title:       t.Title,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Annotations: &mcp.ToolAnnotations{Title: t.Title, ReadOnlyHint: readOnly, DestructiveHint: &destructive, IdempotentHint: readOnly},
		Meta:        mcp.Meta{"nsgo.dev/action": string(t.Action)},
	}
}

// sdkResult is a tool's result or error as the SDK returns it.
func sdkResult(res *Result, err error) *mcp.CallToolResult {
	if err != nil {
		r := &mcp.CallToolResult{}
		if d, ok := errors.AsType[*EditError](err); ok {
			r.StructuredContent = map[string]any{"error": d}
		}
		r.SetError(err)
		return r
	}
	if res == nil {
		res = &Result{}
	}
	r := &mcp.CallToolResult{Content: []mcp.Content{}}
	if res.Text != "" {
		r.Content = append(r.Content, &mcp.TextContent{Text: res.Text})
	}
	if res.PNG != nil {
		r.Content = append(r.Content, &mcp.ImageContent{Data: res.PNG, MIMEType: "image/png"})
	}
	if res.Structured != nil {
		r.StructuredContent = res.Structured
	}
	return r
}

// validToolName reports whether name is a tool name MCP allows.
func validToolName(name string) bool {
	if len(name) > 128 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// decodeArgs decodes the JSON object args into v, refusing unknown fields and trailing data.
func decodeArgs(args json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("arguments: %v", err)
	}
	if dec.More() {
		return errors.New("arguments: more than one JSON value")
	}
	return nil
}
