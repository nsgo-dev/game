package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrRemoteNotConnected is returned by remote inspection and live-control operations when the editor has no active MCP connection to a running game.
var ErrRemoteNotConnected = errors.New("editor: not connected to a running game")

// ErrRemoteUnsupported is returned by ConnectRemoteCommand on non-desktop targets where launching `go tool nsgo mcp` is unavailable.
var ErrRemoteUnsupported = errors.New("editor: remote MCP command transport is only supported on desktop")

const (
	defaultRemoteRefreshInterval = 250 * time.Millisecond
	remoteCallTimeout            = 10 * time.Second
	maxRemoteConsoleEntries      = 200
)

// SceneTreeTab selects whether the left scene tree panel displays the open scene document or the live remote game tree.
type SceneTreeTab int

const (
	// SceneTreeLocal shows the editable scene document layers and objects.
	SceneTreeLocal SceneTreeTab = iota
	// SceneTreeRemote shows the running game's live scenes, layers, and entities over MCP.
	SceneTreeRemote
)

// String returns "Scene" or "Remote".
func (t SceneTreeTab) String() string {
	switch t {
	case SceneTreeRemote:
		return "Remote"
	default:
		return "Scene"
	}
}

// RemoteLiveChange records one live component field edit made through game_component_set during a remote session.
// Stopping or disconnecting keeps the list until the author keeps the changes in the scene or discards them.
type RemoteLiveChange struct {
	EntityRef  string
	EntityName string
	ObjectID   string
	Component  string
	Field      string
	From       any
	To         any
}

// RemoteConsoleEntry records one command executed against the running game's debug console via game_console_exec.
type RemoteConsoleEntry struct {
	Time    time.Time
	Command string
	OK      bool
	Output  string
	Lines   []string
	Error   string
}

// RemoteTakenEntity records an object present in the open scene that was alive in the running game and is no longer alive (for example a collected pickup or destroyed enemy).
type RemoteTakenEntity struct {
	ObjectID    string
	Name        string
	Layer       string
	TakenAtStep uint64
}

// RemoteSnapshot is a point-in-time snapshot of the connected game's live scenes, layers, entities, selected entity inspection, loop state, tunables, and taken scene objects.
type RemoteSnapshot struct {
	Connected    bool
	Connecting   bool
	Error        string
	Scenes       gamemcp.ScenesListResult
	ActiveScene  string
	Layers       []gamemcp.LayerInfo
	Entities     []gamemcp.EntitySummary
	SelectedRef  string
	Inspected    *gamemcp.EntityInspectResult
	Loop         *gamemcp.LoopStatusResult
	FPS          float64
	HasFPS       bool
	Tunables     []gamemcp.TunableResult
	HasConsole   bool
	Taken        []RemoteTakenEntity
	RefreshCount uint64
}

// NewRemoteBridgeServer wraps a live game *mcp.Bridge as a *sdkmcp.Server so the editor (or a test) can connect over an in-memory or pipe MCP transport with the exact same JSON tool wire format as `go tool nsgo mcp`.
// When run is non-nil (for example a game's core.Scope.Post), each tool handler runs on that goroutine and waits for completion.
func NewRemoteBridgeServer(bridge *gamemcp.Bridge, run func(func())) *sdkmcp.Server {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "nsgo-game-bridge",
		Version: "1.0.0",
	}, nil)
	if bridge == nil {
		return srv
	}
	for _, tool := range bridge.Tools() {
		t := tool
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		srv.AddTool(&sdkmcp.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			var rawArgs json.RawMessage
			if req != nil && req.Params != nil {
				rawArgs = req.Params.Arguments
			}
			var val any
			var err error
			if run != nil {
				done := make(chan struct{})
				run(func() {
					defer close(done)
					val, err = t.Handler(ctx, rawArgs)
				})
				select {
				case <-done:
				case <-ctx.Done():
					r := &sdkmcp.CallToolResult{}
					r.SetError(ctx.Err())
					return r, nil
				}
			} else {
				val, err = t.Handler(ctx, rawArgs)
			}
			if err != nil {
				r := &sdkmcp.CallToolResult{}
				r.SetError(err)
				return r, nil
			}
			encoded, err := json.Marshal(val)
			if err != nil {
				r := &sdkmcp.CallToolResult{}
				r.SetError(err)
				return r, nil
			}
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(encoded)}},
			}, nil
		})
	}
	return srv
}

type remoteChangeKey struct {
	entityRef string
	component string
	field     string
}

type remoteState struct {
	mu              sync.Mutex
	refreshMu       sync.Mutex
	scope           *core.Scope
	tab             SceneTreeTab
	session         *sdkmcp.ClientSession
	sessionCtx      context.Context
	cancelTransport context.CancelFunc
	cancelPoll      context.CancelFunc
	pollDone        chan struct{}
	actionCh        chan func()
	actionDone      chan struct{}
	cancelDial      context.CancelFunc
	dialDone        chan struct{}
	snap            RemoteSnapshot
	snapPtr         *RemoteSnapshot // immutable snapshot shared with UI memos
	seq             uint64          // monotonic counter bumped whenever remote UI state changes
	docObjectIDs    map[string]struct{}
	seenObjects     map[string]RemoteTakenEntity
	takenByID       map[string]RemoteTakenEntity
	liveChanges     []RemoteLiveChange
	consoleLog      []RemoteConsoleEntry
	selectedStep    int
}

func (r *remoteState) setScope(cx *core.Scope) {
	r.mu.Lock()
	r.scope = cx
	r.mu.Unlock()
}

func (e *Editor) publishSnapLocked() {
	cp := cloneRemoteSnapshot(e.remote.snap)
	e.remote.snapPtr = &cp
	e.remote.seq++
}

func (e *Editor) remoteSeq() uint64 {
	if e == nil {
		return 0
	}
	e.remote.mu.Lock()
	s := e.remote.seq
	e.remote.mu.Unlock()
	return s
}

// remoteSnapshotShared returns the current immutable RemoteSnapshot pointer without deep-cloning.
// Callers must never mutate the returned snapshot or its slices/maps.
func (e *Editor) remoteSnapshotShared() *RemoteSnapshot {
	if e == nil {
		return &RemoteSnapshot{}
	}
	e.remote.mu.Lock()
	p := e.remote.snapPtr
	if p == nil {
		cp := cloneRemoteSnapshot(e.remote.snap)
		e.remote.snapPtr = &cp
		p = &cp
	}
	e.remote.mu.Unlock()
	return p
}

func (e *Editor) hasRemoteLiveChanges() bool {
	if e == nil {
		return false
	}
	e.remote.mu.Lock()
	has := len(e.remote.liveChanges) > 0
	e.remote.mu.Unlock()
	return has
}

func (e *Editor) remoteLiveChangesByKey() map[remoteChangeKey]RemoteLiveChange {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	if len(e.remote.liveChanges) == 0 {
		return nil
	}
	out := make(map[remoteChangeKey]RemoteLiveChange, len(e.remote.liveChanges))
	for _, c := range e.remote.liveChanges {
		c.From = cloneAny(c.From)
		c.To = cloneAny(c.To)
		out[remoteChangeKey{c.EntityRef, c.Component, c.Field}] = c
	}
	return out
}

func collectDocObjectIDs(doc *sceneformat.SceneDoc) map[string]struct{} {
	if doc == nil {
		return nil
	}
	ids := make(map[string]struct{})
	var walk func(objs []sceneformat.ObjectDoc)
	walk = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			if objs[i].ID != "" {
				ids[objs[i].ID] = struct{}{}
			}
			if len(objs[i].Children) > 0 {
				walk(objs[i].Children)
			}
		}
	}
	for i := range doc.Layers {
		walk(doc.Layers[i].Objects)
	}
	return ids
}

func stringSetEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// syncRemoteDocObjectIDs snapshots e.Doc's object IDs under e.remote.mu on the UI/caller goroutine
// so background polling (refreshRemote) never reads e.Doc concurrently.
func (e *Editor) syncRemoteDocObjectIDs() {
	if e == nil {
		return
	}
	ids := collectDocObjectIDs(e.Doc)
	e.remote.mu.Lock()
	changed := !stringSetEqual(e.remote.docObjectIDs, ids)
	e.remote.docObjectIDs = ids
	if changed {
		e.remote.seq++
	}
	e.remote.mu.Unlock()
}

func (e *Editor) remoteDocObjectIDs() map[string]struct{} {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	ids := e.remote.docObjectIDs
	e.remote.mu.Unlock()
	return ids
}

// SceneTreeTab returns whether the scene tree panel is showing the local scene document or the live remote tree.
func (e *Editor) SceneTreeTab() SceneTreeTab {
	if e == nil {
		return SceneTreeLocal
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	return e.remote.tab
}

// SetSceneTreeTab switches the scene tree panel between SceneTreeLocal and SceneTreeRemote.
func (e *Editor) SetSceneTreeTab(tab SceneTreeTab) {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	changed := e.remote.tab != tab
	e.remote.tab = tab
	if changed {
		e.remote.seq++
	}
	e.remote.mu.Unlock()
	if changed {
		e.notify()
	}
}

// RemoteConnected reports whether the editor currently has an active MCP client session to a running game.
func (e *Editor) RemoteConnected() bool {
	if e == nil {
		return false
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	return e.remote.snap.Connected && e.remote.session != nil
}

// RemoteConnecting reports whether the editor is currently waiting to establish a remote MCP connection to a running game.
func (e *Editor) RemoteConnecting() bool {
	if e == nil {
		return false
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	return e.remote.snap.Connecting
}

// RemoteSnapshot returns a deep-enough copy of the latest remote game snapshot for UI rendering and inspection.
func (e *Editor) RemoteSnapshot() RemoteSnapshot {
	if e == nil {
		return RemoteSnapshot{}
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	return cloneRemoteSnapshot(e.remote.snap)
}

func cloneRemoteSnapshot(s RemoteSnapshot) RemoteSnapshot {
	out := s
	if len(s.Scenes.Scenes) > 0 {
		out.Scenes.Scenes = slices.Clone(s.Scenes.Scenes)
	}
	if len(s.Layers) > 0 {
		out.Layers = slices.Clone(s.Layers)
	}
	if len(s.Entities) > 0 {
		out.Entities = make([]gamemcp.EntitySummary, len(s.Entities))
		for i, ent := range s.Entities {
			ent.Tags = slices.Clone(ent.Tags)
			ent.Components = slices.Clone(ent.Components)
			out.Entities[i] = ent
		}
	}
	if s.Inspected != nil {
		cp := *s.Inspected
		cp.Tags = slices.Clone(s.Inspected.Tags)
		cp.Children = slices.Clone(s.Inspected.Children)
		if s.Inspected.Components != nil {
			cp.Components = make(map[string]json.RawMessage, len(s.Inspected.Components))
			for k, v := range s.Inspected.Components {
				cp.Components[k] = slices.Clone(v)
			}
		}
		out.Inspected = &cp
	}
	if s.Loop != nil {
		cp := *s.Loop
		out.Loop = &cp
	}
	if len(s.Tunables) > 0 {
		out.Tunables = slices.Clone(s.Tunables)
	}
	if len(s.Taken) > 0 {
		out.Taken = slices.Clone(s.Taken)
	}
	return out
}

// SelectedRemoteEntity returns the stable entity ref currently selected in the Remote tab, or "".
func (e *Editor) SelectedRemoteEntity() string {
	if e == nil {
		return ""
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	return e.remote.snap.SelectedRef
}

// RemoteLiveChanges returns a copy of all pending live component edits made through the remote inspector.
func (e *Editor) RemoteLiveChanges() []RemoteLiveChange {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	if len(e.remote.liveChanges) == 0 {
		return nil
	}
	out := make([]RemoteLiveChange, len(e.remote.liveChanges))
	for i, c := range e.remote.liveChanges {
		c.From = cloneAny(c.From)
		c.To = cloneAny(c.To)
		out[i] = c
	}
	return out
}

// RemoteConsoleHistory returns the history of commands run through RemoteExecConsole.
func (e *Editor) RemoteConsoleHistory() []RemoteConsoleEntry {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	if len(e.remote.consoleLog) == 0 {
		return nil
	}
	out := make([]RemoteConsoleEntry, len(e.remote.consoleLog))
	for i, entry := range e.remote.consoleLog {
		entry.Lines = slices.Clone(entry.Lines)
		out[i] = entry
	}
	return out
}

// ClearRemoteConsole clears the remote console command output history.
func (e *Editor) ClearRemoteConsole() {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	had := len(e.remote.consoleLog) > 0
	e.remote.consoleLog = nil
	if had {
		e.remote.seq++
	}
	e.remote.mu.Unlock()
	if had {
		e.postRemoteNotify()
	}
}

// ConnectRemote connects the editor as an MCP client over transport, performs an initial refresh of scenes, layers, entities, loop status, and tunables, switches the scene tree to the Remote tab, and starts background refreshing while connected.
func (e *Editor) ConnectRemote(ctx context.Context, transport sdkmcp.Transport) error {
	if e == nil {
		return ErrRemoteNotConnected
	}
	if transport == nil {
		return errors.New("editor: nil MCP transport")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.syncRemoteDocObjectIDs()
	interval := e.RemoteRefreshInterval
	e.stopRemoteDial()
	_ = e.DisconnectRemote()

	e.remote.mu.Lock()
	e.remote.snap.Connecting = true
	e.remote.snap.Error = ""
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "nsgo-game-editor",
		Version: "1.0.0",
	}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		e.remote.mu.Lock()
		e.remote.snap.Connecting = false
		e.remote.snap.Connected = false
		e.remote.snap.Error = err.Error()
		e.publishSnapLocked()
		e.remote.mu.Unlock()
		e.postRemoteNotify()
		return fmt.Errorf("editor: connect remote MCP: %w", err)
	}

	pollCtx, cancelPoll := context.WithCancel(context.Background())
	pollDone := make(chan struct{})
	actionCh := make(chan func(), 64)
	actionDone := make(chan struct{})

	e.remote.mu.Lock()
	e.remote.session = cs
	e.remote.sessionCtx = pollCtx
	e.remote.cancelPoll = cancelPoll
	e.remote.pollDone = pollDone
	e.remote.actionCh = actionCh
	e.remote.actionDone = actionDone
	e.remote.tab = SceneTreeRemote
	e.remote.snap = RemoteSnapshot{
		Connected:  true,
		Connecting: false,
	}
	e.remote.seenObjects = make(map[string]RemoteTakenEntity)
	e.remote.takenByID = make(map[string]RemoteTakenEntity)
	if e.remote.selectedStep <= 0 {
		e.remote.selectedStep = 1
	}
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	// Perform initial snapshot synchronously so callers have live state immediately.
	_ = e.refreshRemoteForSession(pollCtx, cs, false, true)

	go e.remoteActionLoop(pollCtx, actionCh, actionDone)
	go e.remotePollLoop(pollCtx, cs, interval, pollDone)

	e.postRemoteStatus("Connected to running game")
	return nil
}

// ConnectRemoteCommand connects the editor to a running game via `go tool nsgo mcp` in dir (defaulting to the open project's directory when dir is empty).
func (e *Editor) ConnectRemoteCommand(ctx context.Context, dir string, extraArgs ...string) error {
	if e == nil {
		return ErrRemoteNotConnected
	}
	if dir == "" && e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	if !canConnectRemoteCommand {
		return ErrRemoteUnsupported
	}
	transport, err := newRemoteCommandTransport(dir, extraArgs...)
	if err != nil {
		return err
	}
	return e.ConnectRemote(ctx, transport)
}

// startRemoteCommandConnect dials `go tool nsgo mcp` asynchronously for the Remote tab's Connect button.
// It snapshots e.ProjectPath, e.RemoteRefreshInterval, and e.Doc object IDs on the UI goroutine before
// spawning a tracked dial goroutine that DisconnectRemote / Close cancels and joins cleanly.
func (e *Editor) startRemoteCommandConnect() {
	if e == nil {
		return
	}
	e.syncRemoteDocObjectIDs()
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
	}
	interval := e.RemoteRefreshInterval
	e.stopRemoteDial()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	e.remote.mu.Lock()
	e.remote.cancelDial = cancel
	e.remote.dialDone = done
	e.remote.tab = SceneTreeRemote
	e.remote.snap.Connecting = true
	e.remote.snap.Error = ""
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.notify()

	go func() {
		defer close(done)
		defer cancel()

		if !canConnectRemoteCommand {
			e.remote.mu.Lock()
			e.remote.snap.Connecting = false
			e.remote.snap.Error = ErrRemoteUnsupported.Error()
			e.publishSnapLocked()
			e.remote.mu.Unlock()
			e.postRemoteStatusBG("Remote connect failed: " + ErrRemoteUnsupported.Error())
			return
		}
		transport, err := newRemoteCommandTransport(dir)
		if err != nil {
			e.remote.mu.Lock()
			e.remote.snap.Connecting = false
			e.remote.snap.Error = err.Error()
			e.publishSnapLocked()
			e.remote.mu.Unlock()
			e.postRemoteStatusBG("Remote connect failed: " + err.Error())
			return
		}
		connCtx, connCancel := context.WithTimeout(ctx, 15*time.Second)
		err = e.connectRemoteFromDial(connCtx, transport, nil, interval)
		connCancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.remote.mu.Lock()
			e.remote.snap.Connecting = false
			e.remote.snap.Error = err.Error()
			e.publishSnapLocked()
			e.remote.mu.Unlock()
			e.postRemoteStatusBG("Remote connect failed: " + err.Error())
		}
	}()
}

// DisconnectRemote closes any active remote MCP client session and stops background polling.
// Any unkept RemoteLiveChanges are preserved so the author can still keep them in the scene or discard them after stopping.
func (e *Editor) DisconnectRemote() error {
	return e.disconnectRemote(false)
}

func (e *Editor) disconnectRemote(fromBG bool) error {
	if e == nil {
		return nil
	}
	e.stopRemoteDial()

	e.remote.mu.Lock()
	cs := e.remote.session
	cancelTransport := e.remote.cancelTransport
	cancel := e.remote.cancelPoll
	pollDone := e.remote.pollDone
	actionDone := e.remote.actionDone
	wasConnected := e.remote.snap.Connected || e.remote.snap.Connecting
	e.remote.session = nil
	e.remote.sessionCtx = nil
	e.remote.cancelTransport = nil
	e.remote.cancelPoll = nil
	e.remote.pollDone = nil
	e.remote.actionCh = nil
	e.remote.actionDone = nil
	e.remote.snap.Connected = false
	e.remote.snap.Connecting = false
	e.remote.snap.Error = ""
	e.remote.snap.Scenes = gamemcp.ScenesListResult{}
	e.remote.snap.ActiveScene = ""
	e.remote.snap.Layers = nil
	e.remote.snap.Entities = nil
	e.remote.snap.SelectedRef = ""
	e.remote.snap.Inspected = nil
	e.remote.snap.Loop = nil
	e.remote.snap.FPS = 0
	e.remote.snap.HasFPS = false
	e.remote.snap.Tunables = nil
	e.remote.snap.HasConsole = false
	e.remote.snap.Taken = nil
	e.remote.seenObjects = nil
	e.remote.takenByID = nil
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cancelTransport != nil {
		cancelTransport()
	}
	var closeErr error
	if cs != nil {
		closeErr = cs.Close()
	}
	if pollDone != nil {
		<-pollDone
	}
	if actionDone != nil {
		<-actionDone
	}
	if wasConnected {
		if fromBG {
			e.postRemoteNotifyBG()
		} else {
			e.postRemoteNotify()
		}
	}
	return closeErr
}

func (e *Editor) stopRemoteDial() {
	e.remote.mu.Lock()
	cancel := e.remote.cancelDial
	done := e.remote.dialDone
	e.remote.cancelDial = nil
	e.remote.dialDone = nil
	if e.remote.snap.Connecting {
		e.remote.snap.Connecting = false
		e.publishSnapLocked()
	}
	e.remote.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// startRemoteAutoConnect dials the running game's MCP endpoint in the background after RunGameWith launches with -automation.
func (e *Editor) startRemoteAutoConnect(spec RunGameSpec, runDone <-chan struct{}) {
	e.syncRemoteDocObjectIDs()
	transportFn := e.RemoteTransport
	interval := e.RemoteRefreshInterval

	e.stopRemoteDial()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	e.remote.mu.Lock()
	e.remote.cancelDial = cancel
	e.remote.dialDone = done
	e.remote.tab = SceneTreeRemote
	e.remote.snap.Connecting = true
	e.remote.snap.Error = ""
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteNotify()

	go func() {
		defer close(done)
		defer cancel()

		backoff := 50 * time.Millisecond
		for {
			select {
			case <-ctx.Done():
				return
			case <-runDone:
				e.remote.mu.Lock()
				e.remote.snap.Connecting = false
				e.publishSnapLocked()
				e.remote.mu.Unlock()
				e.postRemoteNotifyBG()
				return
			default:
			}

			transportCtx, cancelTransport := context.WithCancel(context.Background())
			var transport sdkmcp.Transport
			var err error
			if transportFn != nil {
				transport, err = transportFn(transportCtx, spec)
			} else if canConnectRemoteCommand {
				transport, err = newRemoteCommandTransport(spec.Dir)
			} else {
				err = ErrRemoteUnsupported
			}
			if err == nil && transport != nil {
				connCtx, connCancel := context.WithTimeout(ctx, 15*time.Second)
				err = e.connectRemoteFromDial(connCtx, transport, cancelTransport, interval)
				connCancel()
				if err == nil {
					return
				}
			}
			cancelTransport()

			if errors.Is(err, ErrRemoteUnsupported) {
				e.remote.mu.Lock()
				e.remote.snap.Connecting = false
				e.remote.snap.Error = err.Error()
				e.publishSnapLocked()
				e.remote.mu.Unlock()
				e.postRemoteNotifyBG()
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-runDone:
				e.remote.mu.Lock()
				e.remote.snap.Connecting = false
				e.publishSnapLocked()
				e.remote.mu.Unlock()
				e.postRemoteNotifyBG()
				return
			case <-time.After(backoff):
				if backoff < 500*time.Millisecond {
					backoff *= 2
				}
			}
		}
	}()
}

// connectRemoteFromDial establishes a session without calling stopRemoteDial (which would wait on itself).
func (e *Editor) connectRemoteFromDial(ctx context.Context, transport sdkmcp.Transport, cancelTransport context.CancelFunc, interval time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "nsgo-game-editor",
		Version: "1.0.0",
	}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err
	}
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	pollDone := make(chan struct{})
	actionCh := make(chan func(), 64)
	actionDone := make(chan struct{})

	e.remote.mu.Lock()
	if err := ctx.Err(); err != nil {
		e.remote.mu.Unlock()
		cancelPoll()
		_ = cs.Close()
		return err
	}
	oldCS := e.remote.session
	oldCancel := e.remote.cancelPoll
	oldTransportCancel := e.remote.cancelTransport
	oldPollDone := e.remote.pollDone
	oldActionDone := e.remote.actionDone
	e.remote.session = cs
	e.remote.sessionCtx = pollCtx
	e.remote.cancelTransport = cancelTransport
	e.remote.cancelPoll = cancelPoll
	e.remote.pollDone = pollDone
	e.remote.actionCh = actionCh
	e.remote.actionDone = actionDone
	e.remote.tab = SceneTreeRemote
	e.remote.snap = RemoteSnapshot{
		Connected:  true,
		Connecting: false,
	}
	e.remote.seenObjects = make(map[string]RemoteTakenEntity)
	e.remote.takenByID = make(map[string]RemoteTakenEntity)
	if e.remote.selectedStep <= 0 {
		e.remote.selectedStep = 1
	}
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldTransportCancel != nil {
		oldTransportCancel()
	}
	if oldCS != nil {
		_ = oldCS.Close()
	}
	if oldPollDone != nil {
		<-oldPollDone
	}
	if oldActionDone != nil {
		<-oldActionDone
	}

	_ = e.refreshRemoteForSession(pollCtx, cs, true, true)
	go e.remoteActionLoop(pollCtx, actionCh, actionDone)
	go e.remotePollLoop(pollCtx, cs, interval, pollDone)
	e.postRemoteStatusBG("Connected to running game")
	return nil
}

func (e *Editor) remoteActionLoop(ctx context.Context, ch <-chan func(), done chan struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-ch:
			if ctx.Err() != nil {
				return
			}
			fn()
		}
	}
}

func (e *Editor) dispatchRemoteAction(cx *core.Scope, fn func() error) {
	if e == nil || fn == nil {
		return
	}
	e.remote.mu.Lock()
	ch := e.remote.actionCh
	ctx := e.remote.sessionCtx
	scope := e.remote.scope
	e.remote.mu.Unlock()
	if scope == nil {
		scope = cx
	}
	if ch == nil || ctx == nil {
		// Disconnected (no RPC will be made); run directly on caller goroutine.
		e.report(fn())
		return
	}
	run := func() {
		err := fn()
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrRemoteNotConnected) {
			if scope != nil {
				scope.Post(func() { e.report(err) })
			}
		}
	}
	select {
	case ch <- run:
	case <-ctx.Done():
	default:
		if scope != nil {
			scope.Post(func() {
				e.Status = "Remote action queue busy"
				e.notify()
			})
		}
	}
}

func (e *Editor) remotePollLoop(ctx context.Context, cs *sdkmcp.ClientSession, interval time.Duration, done chan struct{}) {
	defer close(done)

	if interval <= 0 {
		interval = defaultRemoteRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sessionClosed := make(chan struct{})
	go func() {
		_ = cs.Wait()
		close(sessionClosed)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sessionClosed:
			var cancel context.CancelFunc
			e.remote.mu.Lock()
			if e.remote.session == cs {
				cancel = e.remote.cancelPoll
				e.remote.session = nil
				e.remote.sessionCtx = nil
				e.remote.actionCh = nil
				e.remote.snap.Connected = false
				e.remote.snap.Connecting = false
				e.remote.snap.Loop = nil
				e.publishSnapLocked()
			}
			e.remote.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			e.postRemoteNotifyBG()
			return
		case <-ticker.C:
			// Avoid redundant polling traffic while paused; explicit Step/Edit/Refresh calls refresh immediately.
			e.remote.mu.Lock()
			paused := e.remote.snap.Loop != nil && e.remote.snap.Loop.Paused
			same := e.remote.session == cs
			e.remote.mu.Unlock()
			if !same {
				return
			}
			if paused {
				continue
			}
			_ = e.refreshRemoteForSession(ctx, cs, true, false)
		}
	}
}

func (e *Editor) remoteSession() (*sdkmcp.ClientSession, context.Context, error) {
	if e == nil {
		return nil, nil, ErrRemoteNotConnected
	}
	e.remote.mu.Lock()
	cs := e.remote.session
	ctx := e.remote.sessionCtx
	connected := e.remote.snap.Connected
	e.remote.mu.Unlock()
	if cs == nil || !connected {
		return nil, nil, ErrRemoteNotConnected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return cs, ctx, nil
}

func callRemoteToolJSON(ctx context.Context, cs *sdkmcp.ClientSession, name string, args any, dst any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, remoteCallTimeout)
	defer cancel()

	if args == nil {
		args = map[string]any{}
	}
	res, err := cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return fmt.Errorf("remote %s: %w", name, err)
	}
	text := extractMCPText(res)
	if res.IsError {
		if text == "" {
			text = "tool returned error"
		}
		return fmt.Errorf("remote %s: %s", name, text)
	}
	if dst == nil {
		return nil
	}
	if text == "" && res.StructuredContent != nil {
		encoded, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return fmt.Errorf("remote %s: marshal structured result: %w", name, err)
		}
		text = string(encoded)
	}
	if err := json.Unmarshal([]byte(text), dst); err != nil {
		return fmt.Errorf("remote %s: decode result: %w", name, err)
	}
	return nil
}

func extractMCPText(res *sdkmcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// RefreshRemote queries the connected running game for its latest scenes, layers, live entities, selected entity components, loop state, and tunables.
func (e *Editor) RefreshRemote() error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	return e.refreshRemoteForSession(ctx, cs, false, true)
}

func (e *Editor) refreshRemoteForSession(ctx context.Context, cs *sdkmcp.ClientSession, fromBG, forceNotify bool) error {
	if cs == nil {
		return ErrRemoteNotConnected
	}

	e.remote.refreshMu.Lock()
	defer e.remote.refreshMu.Unlock()

	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	activeScene := e.remote.snap.ActiveScene
	selectedRef := e.remote.snap.SelectedRef
	e.remote.mu.Unlock()

	var scenes gamemcp.ScenesListResult
	scenesErr := callRemoteToolJSON(ctx, cs, gamemcp.ToolScenesList, nil, &scenes)

	if activeScene == "" || !remoteHasScene(scenes, activeScene) {
		activeScene = scenes.Current
		if activeScene == "" && len(scenes.Scenes) > 0 {
			activeScene = scenes.Scenes[0].Name
		}
	}

	var layersRes gamemcp.LayersListResult
	layerArgs := map[string]any{"include_hidden": true}
	if activeScene != "" {
		layerArgs["scene"] = activeScene
	}
	_ = callRemoteToolJSON(ctx, cs, gamemcp.ToolLayersList, layerArgs, &layersRes)

	var entitiesRes gamemcp.EntitiesListResult
	entitiesErr := callRemoteToolJSON(ctx, cs, gamemcp.ToolEntitiesList, nil, &entitiesRes)

	// Verify selected entity is still alive; keep selection if alive, else clear.
	if selectedRef != "" && !remoteHasEntity(entitiesRes.Entities, selectedRef) {
		selectedRef = ""
	}
	var inspected *gamemcp.EntityInspectResult
	if selectedRef != "" {
		var ins gamemcp.EntityInspectResult
		if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolEntityInspect, map[string]any{"ref": selectedRef}, &ins); err == nil {
			inspected = &ins
		}
	}

	var loopRes gamemcp.LoopStatusResult
	var loopPtr *gamemcp.LoopStatusResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLoopStatus, nil, &loopRes); err == nil {
		loopPtr = &loopRes
	}

	var tunablesRes gamemcp.TunablesListResult
	hasConsole := false
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolTunablesList, nil, &tunablesRes); err == nil {
		hasConsole = true
	}

	var fps float64
	hasFPS := false
	var perfRes gamemcp.PerfReadoutResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolPerfReadout, nil, &perfRes); err == nil && perfRes.FPS > 0 {
		fps = perfRes.FPS
		hasFPS = true
	}

	// Track scene objects that were alive and later disappeared ("taken" entities).
	liveNames := make(map[string]bool, len(entitiesRes.Entities))
	for _, ent := range entitiesRes.Entities {
		if ent.Name != "" {
			liveNames[ent.Name] = true
		}
	}
	var stepCount uint64
	if loopPtr != nil {
		stepCount = loopPtr.Steps
	}

	var firstErr error
	if scenesErr != nil {
		firstErr = scenesErr
	} else if entitiesErr != nil {
		firstErr = entitiesErr
	}

	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	if e.remote.seenObjects == nil {
		e.remote.seenObjects = make(map[string]RemoteTakenEntity)
	}
	if e.remote.takenByID == nil {
		e.remote.takenByID = make(map[string]RemoteTakenEntity)
	}
	// Read snapshotted docObjectIDs under e.remote.mu — never touch e.Doc from background polling.
	docIDs := e.remote.docObjectIDs
	for _, ent := range entitiesRes.Entities {
		if ent.Name != "" {
			if _, ok := docIDs[ent.Name]; ok {
				e.remote.seenObjects[ent.Name] = RemoteTakenEntity{
					ObjectID: ent.Name,
					Name:     ent.Name,
					Layer:    ent.Layer,
				}
				delete(e.remote.takenByID, ent.Name)
			}
		}
	}
	for id, seen := range e.remote.seenObjects {
		if !liveNames[id] {
			if _, already := e.remote.takenByID[id]; !already {
				seen.TakenAtStep = stepCount
				e.remote.takenByID[id] = seen
			}
		}
	}
	var taken []RemoteTakenEntity
	for _, t := range e.remote.takenByID {
		taken = append(taken, t)
	}
	slices.SortFunc(taken, func(a, b RemoteTakenEntity) int {
		return strings.Compare(a.ObjectID, b.ObjectID)
	})

	next := RemoteSnapshot{
		Connected:    true,
		Connecting:   false,
		Scenes:       scenes,
		ActiveScene:  activeScene,
		Layers:       layersRes.Layers,
		Entities:     entitiesRes.Entities,
		SelectedRef:  selectedRef,
		Inspected:    inspected,
		Loop:         loopPtr,
		FPS:          fps,
		HasFPS:       hasFPS,
		Tunables:     tunablesRes.Tunables,
		HasConsole:   hasConsole,
		Taken:        taken,
		RefreshCount: e.remote.snap.RefreshCount + 1,
	}
	if firstErr != nil {
		next.Error = firstErr.Error()
	}

	changed := !remoteSnapshotsEqual(e.remote.snap, next)
	e.remote.snap = next
	if forceNotify || changed {
		e.publishSnapLocked()
	}
	e.remote.mu.Unlock()

	if forceNotify || changed {
		if fromBG {
			e.postRemoteNotifyBG()
		} else {
			e.postRemoteNotify()
		}
	}
	return firstErr
}

func remoteSnapshotsEqual(a, b RemoteSnapshot) bool {
	if a.Connected != b.Connected ||
		a.Connecting != b.Connecting ||
		a.Error != b.Error ||
		a.ActiveScene != b.ActiveScene ||
		a.SelectedRef != b.SelectedRef ||
		a.FPS != b.FPS ||
		a.HasFPS != b.HasFPS ||
		a.HasConsole != b.HasConsole ||
		a.Scenes.Current != b.Scenes.Current ||
		!slices.Equal(a.Scenes.Scenes, b.Scenes.Scenes) ||
		!slices.Equal(a.Layers, b.Layers) ||
		!slices.Equal(a.Taken, b.Taken) ||
		len(a.Entities) != len(b.Entities) {
		return false
	}
	if (a.Loop == nil) != (b.Loop == nil) {
		return false
	}
	if a.Loop != nil && *a.Loop != *b.Loop {
		return false
	}
	for i := range a.Entities {
		ea, eb := a.Entities[i], b.Entities[i]
		if ea.Ref != eb.Ref || ea.Name != eb.Name || ea.Layer != eb.Layer || ea.Parent != eb.Parent ||
			!slices.Equal(ea.Tags, eb.Tags) || !slices.Equal(ea.Components, eb.Components) {
			return false
		}
	}
	if (a.Inspected == nil) != (b.Inspected == nil) {
		return false
	}
	if a.Inspected != nil {
		ia, ib := a.Inspected, b.Inspected
		if ia.Ref != ib.Ref || ia.Name != ib.Name || ia.Layer != ib.Layer || ia.Parent != ib.Parent ||
			!slices.Equal(ia.Tags, ib.Tags) || !slices.Equal(ia.Children, ib.Children) ||
			len(ia.Components) != len(ib.Components) {
			return false
		}
		for k, va := range ia.Components {
			vb, ok := ib.Components[k]
			if !ok || !bytes.Equal(va, vb) {
				return false
			}
		}
	}
	if len(a.Tunables) != len(b.Tunables) {
		return false
	}
	if len(a.Tunables) > 0 && !reflect.DeepEqual(a.Tunables, b.Tunables) {
		return false
	}
	return true
}

func remoteHasScene(res gamemcp.ScenesListResult, name string) bool {
	for _, sc := range res.Scenes {
		if sc.Name == name {
			return true
		}
	}
	return false
}

func remoteHasEntity(list []gamemcp.EntitySummary, ref string) bool {
	for _, ent := range list {
		if ent.Ref == ref {
			return true
		}
	}
	return false
}

func (e *Editor) docHasObjectID(id string) bool {
	if e == nil || e.Doc == nil || id == "" {
		return false
	}
	obj, _, _ := e.FindObject(id)
	return obj != nil
}

// SelectRemoteScene chooses which remote scene's layers are shown in the Remote tab and refreshes layers immediately.
func (e *Editor) SelectRemoteScene(name string) error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	var layersRes gamemcp.LayersListResult
	args := map[string]any{"include_hidden": true}
	if name != "" {
		args["scene"] = name
	}
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLayersList, args, &layersRes); err != nil {
		return err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	e.remote.snap.ActiveScene = name
	e.remote.snap.Layers = layersRes.Layers
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteNotify()
	return nil
}

// SelectRemoteEntity selects a live entity by ref in the Remote tab and inspects its live components via game_entity_inspect.
// Passing an empty ref clears the remote selection.
func (e *Editor) SelectRemoteEntity(ref string) error {
	if e == nil {
		return ErrRemoteNotConnected
	}
	if ref == "" {
		e.remote.mu.Lock()
		e.remote.snap.SelectedRef = ""
		e.remote.snap.Inspected = nil
		e.publishSnapLocked()
		e.remote.mu.Unlock()
		e.postRemoteNotify()
		return nil
	}
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	var ins gamemcp.EntityInspectResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolEntityInspect, map[string]any{"ref": ref}, &ins); err != nil {
		return err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	e.remote.snap.SelectedRef = ref
	e.remote.snap.Inspected = &ins
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteNotify()
	return nil
}

// SetRemoteComponentField patches one field of component on the live entity ref via game_component_set, records or updates the edit in RemoteLiveChanges, and updates the cached inspection snapshot.
func (e *Editor) SetRemoteComponentField(ref, component, field string, value any) error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	if ref == "" || component == "" || field == "" {
		return errors.New("editor: remote component set requires ref, component, and field")
	}

	// Ensure we know the current value (and entity name / object_ref) before editing so LiveChanges captures the baseline From value.
	ins := e.inspectForChangeBaseline(ctx, cs, ref)
	oldVal := extractComponentFieldValue(ins, component, field)
	entityName, objectID := remoteEntityIdentity(ins, ref)

	var res gamemcp.ComponentSetResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolComponentSet, map[string]any{
		"ref":       ref,
		"component": component,
		"value":     map[string]any{field: value},
	}, &res); err != nil {
		return err
	}

	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	if e.remote.snap.Inspected != nil && e.remote.snap.Inspected.Ref == ref {
		if e.remote.snap.Inspected.Components == nil {
			e.remote.snap.Inspected.Components = make(map[string]json.RawMessage)
		}
		if len(res.Value) > 0 && !bytes.Equal(bytes.TrimSpace(res.Value), []byte("null")) {
			e.remote.snap.Inspected.Components[component] = slices.Clone(res.Value)
		}
	}
	// Read back normalized value from the server response if available.
	appliedVal := value
	if len(res.Value) > 0 {
		var m map[string]any
		if json.Unmarshal(res.Value, &m) == nil {
			if v, ok := m[field]; ok {
				appliedVal = v
			}
		}
	}
	e.recordLiveChangeLocked(RemoteLiveChange{
		EntityRef:  ref,
		EntityName: entityName,
		ObjectID:   objectID,
		Component:  component,
		Field:      field,
		From:       oldVal,
		To:         appliedVal,
	})
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	e.postRemoteStatus(fmt.Sprintf("Live set %s.%s", component, field))
	return nil
}

// SetRemoteComponentValue sets or replaces a scalar or structured component value on the live entity ref via game_component_set.
func (e *Editor) SetRemoteComponentValue(ref, component string, value any, replace bool) error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	if ref == "" || component == "" {
		return errors.New("editor: remote component set requires ref and component")
	}

	ins := e.inspectForChangeBaseline(ctx, cs, ref)
	var oldVal any
	if ins != nil {
		if raw, ok := ins.Components[component]; ok {
			_ = json.Unmarshal(raw, &oldVal)
		}
	}
	entityName, objectID := remoteEntityIdentity(ins, ref)

	var res gamemcp.ComponentSetResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolComponentSet, map[string]any{
		"ref":       ref,
		"component": component,
		"value":     value,
		"replace":   replace,
	}, &res); err != nil {
		return err
	}

	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	if e.remote.snap.Inspected != nil && e.remote.snap.Inspected.Ref == ref {
		if e.remote.snap.Inspected.Components == nil {
			e.remote.snap.Inspected.Components = make(map[string]json.RawMessage)
		}
		if len(res.Value) > 0 && !bytes.Equal(bytes.TrimSpace(res.Value), []byte("null")) {
			e.remote.snap.Inspected.Components[component] = slices.Clone(res.Value)
		}
	}
	appliedVal := value
	if len(res.Value) > 0 {
		var decoded any
		if json.Unmarshal(res.Value, &decoded) == nil {
			appliedVal = decoded
		}
	}
	e.recordLiveChangeLocked(RemoteLiveChange{
		EntityRef:  ref,
		EntityName: entityName,
		ObjectID:   objectID,
		Component:  component,
		Field:      "",
		From:       oldVal,
		To:         appliedVal,
	})
	e.publishSnapLocked()
	e.remote.mu.Unlock()

	e.postRemoteStatus(fmt.Sprintf("Live set %s", component))
	return nil
}

func (e *Editor) inspectForChangeBaseline(ctx context.Context, cs *sdkmcp.ClientSession, ref string) *gamemcp.EntityInspectResult {
	e.remote.mu.Lock()
	if e.remote.session == cs && e.remote.snap.Inspected != nil && e.remote.snap.Inspected.Ref == ref {
		cp := *e.remote.snap.Inspected
		if e.remote.snap.Inspected.Components != nil {
			cp.Components = make(map[string]json.RawMessage, len(e.remote.snap.Inspected.Components))
			for k, v := range e.remote.snap.Inspected.Components {
				cp.Components[k] = slices.Clone(v)
			}
		}
		e.remote.mu.Unlock()
		return &cp
	}
	e.remote.mu.Unlock()
	var ins gamemcp.EntityInspectResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolEntityInspect, map[string]any{"ref": ref}, &ins); err == nil {
		return &ins
	}
	return nil
}

func extractComponentFieldValue(ins *gamemcp.EntityInspectResult, component, field string) any {
	if ins == nil {
		return nil
	}
	raw, ok := ins.Components[component]
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m[field]
}

func remoteEntityIdentity(ins *gamemcp.EntityInspectResult, ref string) (entityName, objectID string) {
	entityName = ref
	if ins == nil {
		return entityName, ""
	}
	if ins.Name != "" {
		entityName = ins.Name
	}
	if raw, ok := ins.Components["object_ref"]; ok {
		var objRef sceneformat.ObjectRef
		if json.Unmarshal(raw, &objRef) == nil && objRef.ID != "" {
			objectID = objRef.ID
		}
	}
	return entityName, objectID
}

func (e *Editor) recordLiveChangeLocked(change RemoteLiveChange) {
	for i := range e.remote.liveChanges {
		c := &e.remote.liveChanges[i]
		if c.EntityRef == change.EntityRef && c.Component == change.Component && c.Field == change.Field {
			if remoteValuesEqual(c.From, change.To) {
				e.remote.liveChanges = slices.Delete(e.remote.liveChanges, i, i+1)
				return
			}
			c.To = cloneAny(change.To)
			if c.ObjectID == "" {
				c.ObjectID = change.ObjectID
			}
			if c.EntityName == "" || c.EntityName == c.EntityRef {
				c.EntityName = change.EntityName
			}
			return
		}
	}
	if remoteValuesEqual(change.From, change.To) {
		return
	}
	change.From = cloneAny(change.From)
	change.To = cloneAny(change.To)
	e.remote.liveChanges = append(e.remote.liveChanges, change)
}

func remoteValuesEqual(a, b any) bool {
	fa, oka := asFloat64(a)
	fb, okb := asFloat64(b)
	if oka && okb {
		return math.Abs(fa-fb) <= 1e-9
	}
	return reflect.DeepEqual(a, b)
}

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// RevertRemoteLiveChange puts back the baseline scene value for live change index (sending game_component_set if still connected) and removes it from RemoteLiveChanges.
func (e *Editor) RevertRemoteLiveChange(index int) error {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	if index < 0 || index >= len(e.remote.liveChanges) {
		e.remote.mu.Unlock()
		return fmt.Errorf("editor: live change index %d out of range", index)
	}
	change := e.remote.liveChanges[index]
	cs := e.remote.session
	ctx := e.remote.sessionCtx
	connected := e.remote.snap.Connected
	e.remote.mu.Unlock()

	return e.revertRemoteLiveChange(change, cs, ctx, connected)
}

func (e *Editor) revertRemoteLiveChangeByKey(ref, component, field string) error {
	if e == nil {
		return nil
	}
	e.remote.mu.Lock()
	idx := -1
	for i, c := range e.remote.liveChanges {
		if c.EntityRef == ref && c.Component == component && c.Field == field {
			idx = i
			break
		}
	}
	if idx < 0 {
		e.remote.mu.Unlock()
		return nil
	}
	change := e.remote.liveChanges[idx]
	cs := e.remote.session
	ctx := e.remote.sessionCtx
	connected := e.remote.snap.Connected
	e.remote.mu.Unlock()

	return e.revertRemoteLiveChange(change, cs, ctx, connected)
}

func (e *Editor) revertRemoteLiveChange(change RemoteLiveChange, cs *sdkmcp.ClientSession, ctx context.Context, connected bool) error {
	if connected && cs != nil {
		var payload any = change.From
		if change.Field != "" {
			payload = map[string]any{change.Field: change.From}
		}
		var res gamemcp.ComponentSetResult
		if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolComponentSet, map[string]any{
			"ref":       change.EntityRef,
			"component": change.Component,
			"value":     payload,
		}, &res); err != nil {
			return err
		}
		e.remote.mu.Lock()
		if e.remote.session == cs && e.remote.snap.Inspected != nil && e.remote.snap.Inspected.Ref == change.EntityRef && len(res.Value) > 0 {
			if e.remote.snap.Inspected.Components == nil {
				e.remote.snap.Inspected.Components = make(map[string]json.RawMessage)
			}
			e.remote.snap.Inspected.Components[change.Component] = slices.Clone(res.Value)
		}
		e.remote.mu.Unlock()
	}

	e.remote.mu.Lock()
	for i := range e.remote.liveChanges {
		c := e.remote.liveChanges[i]
		if c.EntityRef == change.EntityRef && c.Component == change.Component && c.Field == change.Field {
			e.remote.liveChanges = slices.Delete(e.remote.liveChanges, i, i+1)
			break
		}
	}
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteStatus("Reverted live change")
	return nil
}

// DiscardRemoteLiveChanges reverts every pending live change in the running game (when connected) and clears the RemoteLiveChanges list.
func (e *Editor) DiscardRemoteLiveChanges() error {
	if e == nil {
		return nil
	}
	changes := e.RemoteLiveChanges()
	if len(changes) == 0 {
		return nil
	}
	cs, ctx, _ := e.remoteSession()
	var firstErr error
	if cs != nil {
		for _, c := range changes {
			var payload any = c.From
			if c.Field != "" {
				payload = map[string]any{c.Field: c.From}
			}
			var res gamemcp.ComponentSetResult
			if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolComponentSet, map[string]any{
				"ref":       c.EntityRef,
				"component": c.Component,
				"value":     payload,
			}, &res); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if errors.Is(err, context.Canceled) {
					break
				}
				continue
			}
			e.remote.mu.Lock()
			if e.remote.session == cs && e.remote.snap.Inspected != nil && e.remote.snap.Inspected.Ref == c.EntityRef && len(res.Value) > 0 {
				if e.remote.snap.Inspected.Components == nil {
					e.remote.snap.Inspected.Components = make(map[string]json.RawMessage)
				}
				e.remote.snap.Inspected.Components[c.Component] = slices.Clone(res.Value)
			}
			e.remote.mu.Unlock()
		}
	}
	e.remote.mu.Lock()
	e.remote.liveChanges = nil
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteStatus("Discarded live changes")
	return firstErr
}

// KeepRemoteLiveChangesInScene writes every live change whose entity maps to an object in the open scene (via sceneformat.ObjectRef or matching object ID) into e.Doc as a single undoable History group.
// One Undo restores every kept change. Applied changes are removed from RemoteLiveChanges; changes on runtime-spawned entities with no scene object remain untouched.
func (e *Editor) KeepRemoteLiveChangesInScene() (int, error) {
	if e == nil || e.Doc == nil {
		return 0, errors.New("editor: no scene open")
	}
	if e.playing {
		return 0, ErrPlaying
	}
	changes := e.RemoteLiveChanges()
	if len(changes) == 0 {
		return 0, nil
	}

	type resolvedChange struct {
		objectID string
		change   RemoteLiveChange
	}
	var applicable []resolvedChange
	for _, c := range changes {
		objID := c.ObjectID
		if objID == "" && c.EntityName != "" {
			if obj, _, _ := e.FindObject(c.EntityName); obj != nil {
				objID = obj.ID
			}
		}
		if objID == "" {
			continue
		}
		if obj, _, _ := e.FindObject(objID); obj == nil {
			continue
		}
		if c.Field == "" {
			continue
		}
		applicable = append(applicable, resolvedChange{objectID: objID, change: c})
	}
	if len(applicable) == 0 {
		return 0, errors.New("editor: none of the live changes map to an object in the open scene")
	}

	label := "Keep live changes"
	if len(applicable) == 1 {
		c := applicable[0].change
		label = fmt.Sprintf("Keep %s.%s", c.Component, c.Field)
	}
	if err := e.History.Group(label, func() error {
		for _, rc := range applicable {
			if err := e.applyRemoteChangeToScene(rc.objectID, rc.change); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}

	appliedByKey := make(map[remoteChangeKey]any, len(applicable))
	for _, rc := range applicable {
		appliedByKey[remoteChangeKey{rc.change.EntityRef, rc.change.Component, rc.change.Field}] = rc.change.To
	}
	e.remote.mu.Lock()
	var remaining []RemoteLiveChange
	for _, c := range e.remote.liveChanges {
		k := remoteChangeKey{c.EntityRef, c.Component, c.Field}
		if appliedTo, ok := appliedByKey[k]; ok {
			if remoteValuesEqual(c.To, appliedTo) {
				continue
			}
			// A newer live edit updated this same key while keeping; keep the remaining delta.
			c.From = cloneAny(appliedTo)
		}
		remaining = append(remaining, c)
	}
	e.remote.liveChanges = remaining
	e.remote.seq++
	e.remote.mu.Unlock()

	e.Status = fmt.Sprintf("Kept %d live change(s) in scene", len(applicable))
	e.notify()
	return len(applicable), nil
}

func (e *Editor) applyRemoteChangeToScene(objectID string, c RemoteLiveChange) error {
	if c.Component == "transform2d" {
		obj, _, _ := e.FindObject(objectID)
		if obj == nil {
			return fmt.Errorf("editor: object %q not found", objectID)
		}
		f, ok := asFloat64(c.To)
		if !ok {
			return fmt.Errorf("editor: transform2d.%s requires a number", c.Field)
		}
		switch c.Field {
		case "x":
			return e.MoveObject(objectID, float32(f), obj.Y)
		case "y":
			return e.MoveObject(objectID, obj.X, float32(f))
		case "rotation":
			return e.RotateObject(objectID, float32(f))
		case "scale_x":
			return e.ScaleObject(objectID, float32(f), obj.EffectiveScaleY())
		case "scale_y":
			return e.ScaleObject(objectID, obj.EffectiveScaleX(), float32(f))
		}
	}

	val := c.To
	if t, ok := e.lookupComponentType(c.Component); ok {
		for _, field := range t.Fields {
			if field.Name == c.Field {
				if field.Kind == sceneformat.FieldInt {
					if f, ok := asFloat64(val); ok {
						val = int(math.Round(f))
					}
				}
				break
			}
		}
	} else if f, ok := val.(float64); ok && math.Trunc(f) == f {
		// Preserve integer representation when the scene already stored an int for this field.
		if obj, _, _ := e.FindObject(objectID); obj != nil {
			if cm, ok := obj.Components[c.Component].(map[string]any); ok {
				if _, isInt := cm[c.Field].(int); isInt {
					val = int(f)
				}
			}
		}
	}
	return e.SetComponentField(objectID, c.Component, c.Field, val)
}

// RemotePause pauses the running game's fixed-step loop via game_loop_pause.
func (e *Editor) RemotePause() error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	var res gamemcp.LoopStatusResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLoopPause, nil, &res); err != nil {
		return err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	cp := res
	e.remote.snap.Loop = &cp
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	_ = e.refreshRemoteForSession(ctx, cs, false, true)
	e.postRemoteStatus(fmt.Sprintf("Paused at step %d", res.Steps))
	return nil
}

// RemoteResume resumes the running game's fixed-step loop via game_loop_resume.
func (e *Editor) RemoteResume() error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	var res gamemcp.LoopStatusResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLoopResume, nil, &res); err != nil {
		return err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	cp := res
	e.remote.snap.Loop = &cp
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteStatus("Resumed running game")
	return nil
}

// RemoteStep synchronously advances the running game by count fixed updates via game_loop_step, then refreshes the remote tree and selected entity inspection.
func (e *Editor) RemoteStep(count int) (gamemcp.LoopStepResult, error) {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return gamemcp.LoopStepResult{}, err
	}
	if count < 1 {
		count = 1
	}
	if count > gamemcp.MaxLoopSteps {
		count = gamemcp.MaxLoopSteps
	}
	var res gamemcp.LoopStepResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLoopStep, map[string]any{"count": count}, &res); err != nil {
		return gamemcp.LoopStepResult{}, err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return gamemcp.LoopStepResult{}, ErrRemoteNotConnected
	}
	if e.remote.snap.Loop != nil {
		e.remote.snap.Loop.Steps = res.TotalSteps
		e.remote.snap.Loop.GameTimeSeconds = res.GameTimeSeconds
		e.remote.snap.Loop.Paused = res.Paused
	}
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	_ = e.refreshRemoteForSession(ctx, cs, false, true)
	e.postRemoteStatus(fmt.Sprintf("Stepped %d update(s) (step %d)", res.StepsAdvanced, res.TotalSteps))
	return res, nil
}

// RemoteSetTimeScale sets the running game's simulation time scale via game_loop_time_scale.
func (e *Editor) RemoteSetTimeScale(scale float64) error {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return err
	}
	var res gamemcp.LoopStatusResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolLoopTimeScale, map[string]any{"time_scale": scale}, &res); err != nil {
		return err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return ErrRemoteNotConnected
	}
	cp := res
	e.remote.snap.Loop = &cp
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteStatus(fmt.Sprintf("Time scale: %g×", res.TimeScale))
	return nil
}

// RemoteSetTunable updates a registered debug console tunable on the running game via game_tunable_set.
func (e *Editor) RemoteSetTunable(name string, value any, clamp bool) (gamemcp.TunableSetResult, error) {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return gamemcp.TunableSetResult{}, err
	}
	var res gamemcp.TunableSetResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolTunableSet, map[string]any{
		"name":  name,
		"value": value,
		"clamp": clamp,
	}, &res); err != nil {
		return gamemcp.TunableSetResult{}, err
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return gamemcp.TunableSetResult{}, ErrRemoteNotConnected
	}
	for i := range e.remote.snap.Tunables {
		if e.remote.snap.Tunables[i].Name == name {
			e.remote.snap.Tunables[i].Value = res.Value
			break
		}
	}
	e.publishSnapLocked()
	e.remote.mu.Unlock()
	e.postRemoteStatus(fmt.Sprintf("Tunable %s = %v", name, res.Value))
	return res, nil
}

// RemoteExecConsole executes a command line on the running game's debug console via game_console_exec, appends the result to RemoteConsoleHistory, and refreshes remote state.
func (e *Editor) RemoteExecConsole(command string) (gamemcp.ConsoleExecResult, error) {
	cs, ctx, err := e.remoteSession()
	if err != nil {
		return gamemcp.ConsoleExecResult{}, err
	}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return gamemcp.ConsoleExecResult{}, errors.New("editor: console command must not be empty")
	}
	var res gamemcp.ConsoleExecResult
	if err := callRemoteToolJSON(ctx, cs, gamemcp.ToolConsoleExec, map[string]any{
		"command": cmd,
	}, &res); err != nil {
		return gamemcp.ConsoleExecResult{}, err
	}

	entry := RemoteConsoleEntry{
		Time:    time.Now(),
		Command: res.Command,
		OK:      res.OK,
		Output:  res.Output,
		Lines:   slices.Clone(res.Lines),
		Error:   res.Error,
	}
	e.remote.mu.Lock()
	if e.remote.session != cs {
		e.remote.mu.Unlock()
		return gamemcp.ConsoleExecResult{}, ErrRemoteNotConnected
	}
	e.remote.consoleLog = append(e.remote.consoleLog, entry)
	if len(e.remote.consoleLog) > maxRemoteConsoleEntries {
		e.remote.consoleLog = e.remote.consoleLog[len(e.remote.consoleLog)-maxRemoteConsoleEntries:]
	}
	e.remote.seq++
	e.remote.mu.Unlock()

	_ = e.refreshRemoteForSession(ctx, cs, false, true)
	e.postRemoteNotify()
	return res, nil
}

func (e *Editor) remoteStepCount() int {
	e.remote.mu.Lock()
	defer e.remote.mu.Unlock()
	if e.remote.selectedStep <= 0 {
		return 1
	}
	return e.remote.selectedStep
}

func (e *Editor) setRemoteStepCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > gamemcp.MaxLoopSteps {
		n = gamemcp.MaxLoopSteps
	}
	e.remote.mu.Lock()
	changed := e.remote.selectedStep != n
	e.remote.selectedStep = n
	if changed {
		e.remote.seq++
	}
	e.remote.mu.Unlock()
	if changed {
		e.notify()
	}
}

func (e *Editor) postRemoteNotify() {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	scope := e.remote.scope
	e.remote.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
		return
	}
	e.notify()
}

func (e *Editor) postRemoteNotifyBG() {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	scope := e.remote.scope
	e.remote.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
	}
}

func (e *Editor) postRemoteStatus(status string) {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	scope := e.remote.scope
	e.remote.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.Status = status
			e.notify()
		})
		return
	}
	e.Status = status
	e.notify()
}

func (e *Editor) postRemoteStatusBG(status string) {
	if e == nil {
		return
	}
	e.remote.mu.Lock()
	scope := e.remote.scope
	e.remote.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.Status = status
			e.notify()
		})
	}
}
