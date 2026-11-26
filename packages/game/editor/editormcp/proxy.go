package editormcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Proxy is the MCP server go tool game mcp runs on standard input and output: it finds the editor that has the project open, through the project's endpoint, and forwards every tool call, resource read and subscription to it, and the editor's notifications back. It connects when a client first needs the editor and again after the editor closes and opens the project anew, so the agent's session outlives the editor's. While no editor has the project open, it lists the editor's built-in tools and answers each call with an error that says how to open the project.
type Proxy struct {
	root string
	sdk  *mcp.Server

	mu         sync.Mutex
	session    *mcp.ClientSession
	clientInfo *mcp.Implementation
	runCtx     context.Context
	// wg tracks background dial/refresh goroutines started while Run is active.
	wg sync.WaitGroup
	// tools are the tools listed to the client, as the editor last listed them, or the built-in ones before it has.
	tools map[string]*mcp.Tool
	// subs are the resource URIs the client subscribes to, which a new connection to the editor subscribes to again.
	subs map[string]bool
	// refreshing serializes mirroring the editor's tool list.
	refreshing sync.Mutex
}

// proxyStdioMarker identifies a connection forwarded from go tool game mcp over stdio so the editor shows "<Agent> over stdio".
const proxyStdioMarker = "nsgo-mcp-stdio"

// editorProtocol is the MCP version the proxy speaks to the editor. Under 2026-07-28 the SDK's ClientSession.Subscribe holds a subscriptions/listen request open until the subscription ends instead of returning, so the proxy, which subscribes for its client as it connects, keeps to the version before, where a subscription is a request that returns and updates arrive as notifications. Its own client may speak either.
const editorProtocol = "2025-11-25"

// NewProxy returns the proxy for the project at root, which ProjectRoot finds.
func NewProxy(root string) *Proxy {
	p := &Proxy{root: root, tools: map[string]*mcp.Tool{}, subs: map[string]bool{}}
	p.sdk = mcp.NewServer(&mcp.Implementation{Name: ServerName, Title: "NSGo Game Editor"}, &mcp.ServerOptions{
		Instructions:       instructions + "\n\nThis server forwards to the editor that has the project at " + root + " open; while none has, each tool says how to open it.",
		SubscribeHandler:   p.subscribe,
		UnsubscribeHandler: p.unsubscribe,
		Capabilities: &mcp.ServerCapabilities{
			Tools:     &mcp.ToolCapabilities{ListChanged: true},
			Resources: &mcp.ResourceCapabilities{Subscribe: true, ListChanged: true},
		},
	})
	p.sdk.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				if callReq, ok := req.(*mcp.CallToolRequest); ok && callReq != nil && callReq.Params != nil {
					name := callReq.Params.Name
					p.mu.Lock()
					_, listed := p.tools[name]
					p.mu.Unlock()
					if !listed {
						cs, err := p.editor(ctx)
						if err == nil {
							res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: callReq.Params.Arguments})
							if callErr == nil && res != nil {
								return res, nil
							}
							if callErr != nil && ctx.Err() == nil {
								p.drop(cs)
							}
						}
					}
				}
			}
			res, err := next(ctx, method, req)
			if err == nil && (method == "server/discover" || method == "initialize" || method == "notifications/initialized") {
				if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
					if params := ss.InitializeParams(); params != nil && params.ClientInfo != nil && params.ClientInfo.Name != "" {
						ci := *params.ClientInfo
						p.mu.Lock()
						p.clientInfo = &ci
						p.mu.Unlock()
					}
				}
				p.spawnBackground(func(bgCtx context.Context) {
					if cs, dialErr := p.editor(bgCtx); dialErr == nil {
						p.refresh(bgCtx, cs)
					}
				})
			}
			return res, err
		}
	})
	for _, t := range builtinTools() {
		if t.DefaultOff {
			continue
		}
		p.mirror(sdkTool(t))
	}
	for _, r := range resources {
		p.sdk.AddResource(r, p.readResource)
	}
	return p
}

// spawnBackground runs fn in a goroutine bound to the active Run context and tracked by p.wg
// so Proxy.Run never leaks goroutines after returning.
func (p *Proxy) spawnBackground(fn func(context.Context)) {
	p.mu.Lock()
	bgCtx := p.runCtx
	if bgCtx == nil || bgCtx.Err() != nil {
		p.mu.Unlock()
		return
	}
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		fn(bgCtx)
	}()
}

// Run serves one client on t until the client disconnects or ctx ends; then it closes its connection to the editor.
func (p *Proxy) Run(ctx context.Context, t mcp.Transport) error {
	runCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.runCtx = runCtx
	p.mu.Unlock()

	err := p.sdk.Run(runCtx, t)

	cancel()
	p.mu.Lock()
	p.runCtx = nil
	cs := p.session
	p.session = nil
	p.mu.Unlock()
	if cs != nil {
		_ = cs.Close()
	}
	p.wg.Wait()
	return err
}

func (p *Proxy) forwardImpl() *mcp.Implementation {
	impl := &mcp.Implementation{
		Name:    "go tool game mcp",
		Version: proxyStdioMarker,
	}
	if p.clientInfo == nil && p.sdk != nil {
		for ss := range p.sdk.Sessions() {
			if params := ss.InitializeParams(); params != nil && params.ClientInfo != nil && params.ClientInfo.Name != "" {
				ci := *params.ClientInfo
				p.clientInfo = &ci
				break
			}
		}
	}
	if p.clientInfo != nil && p.clientInfo.Name != "" {
		impl.Name = p.clientInfo.Name
		impl.Title = p.clientInfo.Title
	}
	return impl
}

// editor returns the session with the editor, connecting when there is none; its error says how to open the project when no editor has it open.
func (p *Proxy) editor(ctx context.Context) (*mcp.ClientSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil {
		return p.session, nil
	}
	conn, err := Dial(ctx, p.root)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(p.forwardImpl(), &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			// The handler runs on the session's reading goroutine, which the list request needs free.
			go p.refresh(context.Background(), p.current())
		},
		ResourceUpdatedHandler: func(ctx context.Context, req *mcp.ResourceUpdatedNotificationRequest) {
			_ = p.sdk.ResourceUpdated(ctx, req.Params)
		},
	})
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: conn, Writer: conn}, &mcp.ClientSessionOptions{ProtocolVersion: editorProtocol})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("editormcp: connect to the editor of %s: %w", p.root, err)
	}
	p.session = cs
	go func() {
		_ = cs.Wait()
		p.mu.Lock()
		if p.session == cs {
			p.session = nil
		}
		p.mu.Unlock()
	}()
	for _, uri := range slices.Sorted(maps.Keys(p.subs)) {
		_ = cs.Subscribe(ctx, &mcp.SubscribeParams{URI: uri})
	}
	// An editor may list more tools than the built-in ones, as other parts of it add theirs.
	go p.refresh(context.Background(), cs)
	return cs, nil
}

// current is the session with the editor, or nil.
func (p *Proxy) current() *mcp.ClientSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.session
}

// drop forgets cs after a call on it failed, so the next call connects again.
func (p *Proxy) drop(cs *mcp.ClientSession) {
	p.mu.Lock()
	if p.session == cs {
		p.session = nil
	}
	p.mu.Unlock()
	_ = cs.Close()
}

// refresh lists the editor's tools on cs and mirrors them: it adds those that are new or changed and removes those the editor no longer has, which tells the client the list changed.
func (p *Proxy) refresh(ctx context.Context, cs *mcp.ClientSession) {
	if cs == nil {
		return
	}
	p.refreshing.Lock()
	defer p.refreshing.Unlock()
	listed := map[string]*mcp.Tool{}
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return
		}
		listed[t.Name] = t
	}
	p.mu.Lock()
	var gone []string
	for name := range p.tools {
		if _, ok := listed[name]; !ok {
			gone = append(gone, name)
		}
	}
	p.mu.Unlock()
	if len(gone) > 0 {
		p.mu.Lock()
		for _, name := range gone {
			delete(p.tools, name)
		}
		p.mu.Unlock()
		p.sdk.RemoveTools(gone...)
	}
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		p.mirror(listed[name])
	}
}

// mirror lists t to the client, forwarding its calls to the editor, unless the same tool is listed already.
func (p *Proxy) mirror(t *mcp.Tool) {
	p.mu.Lock()
	prior, ok := p.tools[t.Name]
	if ok && sameTool(prior, t) {
		p.mu.Unlock()
		return
	}
	p.tools[t.Name] = t
	p.mu.Unlock()
	name := t.Name
	p.sdk.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cs, err := p.editor(ctx)
		if err != nil {
			r := &mcp.CallToolResult{}
			r.SetError(err)
			return r, nil
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: req.Params.Arguments})
		if err != nil {
			if ctx.Err() == nil {
				p.drop(cs)
			}
			r := &mcp.CallToolResult{}
			r.SetError(fmt.Errorf("editormcp: the editor of %s did not answer %s: %w; if it closed, open the project with `go tool game editor %s` and call the tool again", p.root, name, err, p.root))
			return r, nil
		}
		return res, nil
	})
}

// sameTool reports whether a and b list the same.
func sameTool(a, b *mcp.Tool) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// readResource forwards a resource read to the editor.
func (p *Proxy) readResource(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	cs, err := p.editor(ctx)
	if err != nil {
		return nil, err
	}
	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.Params.URI})
	if err != nil && ctx.Err() == nil {
		p.drop(cs)
	}
	return res, err
}

// subscribe records the client's subscription and forwards it to the editor when one is connected.
func (p *Proxy) subscribe(ctx context.Context, req *mcp.SubscribeRequest) error {
	p.mu.Lock()
	p.subs[req.Params.URI] = true
	cs := p.session
	p.mu.Unlock()
	if cs == nil {
		// Connecting subscribes to every recorded URI; without an editor the subscription holds and reaches the editor once one connects.
		_, _ = p.editor(ctx)
		return nil
	}
	return cs.Subscribe(ctx, &mcp.SubscribeParams{URI: req.Params.URI})
}

// unsubscribe forgets the client's subscription and forwards it to the editor when one is connected.
func (p *Proxy) unsubscribe(ctx context.Context, req *mcp.UnsubscribeRequest) error {
	p.mu.Lock()
	delete(p.subs, req.Params.URI)
	cs := p.session
	p.mu.Unlock()
	if cs == nil {
		return nil
	}
	return cs.Unsubscribe(ctx, &mcp.UnsubscribeParams{URI: req.Params.URI})
}
