//go:build js && wasm

package webmcpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

const (
	playModeHeaderName   = "X-NSGo-Play-Mode"
	bridgeInfoHeaderName = "X-NSGo-MCP-Bridge"
	defaultBridgeInfo    = "/__nsgo_mcp/info"
	defaultBridgeScript  = "/__nsgo_mcp/bridge.js"
	bridgeScriptPrefix   = "/__nsgo_mcp/"
	callTimeout          = 30 * time.Second
)

type registration struct {
	id        uint64
	cfg       Config
	done      chan struct{}
	closeOnce sync.Once
}

func (r *registration) close() {
	r.closeOnce.Do(func() {
		close(r.done)
	})
}

var (
	regMu          sync.Mutex
	nextRegID      uint64
	registrations  []*registration
	installed      bool
	installedObj   js.Value
	callToolFunc   js.Func
	listToolsFunc  js.Func
	probeStartedMu sync.Mutex
	probeStarted   bool
)

// Register exposes cfg's MCP tools on window.__nsgoMCP and triggers Play-mode
// LAN MCP bridge discovery when served with X-NSGo-Play-Mode: debug.
// Calling the returned unregister function detaches this registration cleanly.
func Register(cfg Config) (unregister func()) {
	if cfg.CallTool == nil {
		return func() {}
	}
	regMu.Lock()
	nextRegID++
	reg := &registration{
		id:   nextRegID,
		cfg:  cfg,
		done: make(chan struct{}),
	}
	registrations = append(registrations, reg)
	syncWindowObjectLocked()
	regMu.Unlock()

	ensurePlayModeBridgeProbe()

	var once sync.Once
	return func() {
		once.Do(func() {
			reg.close()
			regMu.Lock()
			for i, r := range registrations {
				if r.id == reg.id {
					registrations = append(registrations[:i], registrations[i+1:]...)
					break
				}
			}
			syncWindowObjectLocked()
			regMu.Unlock()
		})
	}
}

func activeRegistrationLocked() *registration {
	var best *registration
	for _, r := range registrations {
		if best == nil || r.cfg.Priority >= best.cfg.Priority {
			best = r
		}
	}
	return best
}

func activeRegistration() *registration {
	regMu.Lock()
	defer regMu.Unlock()
	return activeRegistrationLocked()
}

func syncWindowObjectLocked() {
	global := js.Global()
	if !global.Truthy() {
		return
	}
	best := activeRegistrationLocked()
	if best == nil {
		if installed {
			if cur := global.Get("__nsgoMCP"); cur.Equal(installedObj) {
				global.Delete("__nsgoMCP")
			}
			callToolFunc.Release()
			listToolsFunc.Release()
			installed = false
			installedObj = js.Undefined()
		}
		return
	}
	if !installed {
		obj := global.Get("Object").New()
		callToolFunc = js.FuncOf(handleJSCallTool)
		listToolsFunc = js.FuncOf(handleJSListTools)
		obj.Set("version", 1)
		obj.Set("callTool", callToolFunc)
		obj.Set("listTools", listToolsFunc)
		installedObj = obj
		installed = true
		global.Set("__nsgoMCP", obj)
	}
	installedObj.Set("source", best.cfg.Source)
}

func handleJSListTools(_ js.Value, _ []js.Value) any {
	reg := activeRegistration()
	if reg == nil || reg.cfg.ListTools == nil {
		return js.Global().Get("Array").New()
	}
	tools := reg.cfg.ListTools()
	raw, err := json.Marshal(tools)
	if err != nil {
		return js.Global().Get("Array").New()
	}
	return js.Global().Get("JSON").Call("parse", string(raw))
}

func handleJSCallTool(_ js.Value, args []js.Value) any {
	toolName := ""
	if len(args) > 0 && args[0].Type() == js.TypeString {
		toolName = strings.TrimSpace(args[0].String())
	}
	argsJSON := "{}"
	if len(args) > 1 && !args[1].IsUndefined() && !args[1].IsNull() {
		str := js.Global().Get("JSON").Call("stringify", args[1])
		if str.Type() == js.TypeString && strings.TrimSpace(str.String()) != "" {
			argsJSON = str.String()
		}
	}

	executor := js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
		resolve := pArgs[0]
		reject := pArgs[1]
		go func() {
			raw, err := dispatchToolCall(toolName, json.RawMessage(argsJSON))
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			if len(raw) == 0 {
				resolve.Invoke(js.Null())
				return
			}
			parsed := js.Global().Get("JSON").Call("parse", string(raw))
			resolve.Invoke(parsed)
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	// Per ECMAScript Promise constructor semantics, executor runs synchronously before New returns.
	executor.Release()
	return promise
}

func dispatchToolCall(toolName string, args json.RawMessage) (json.RawMessage, error) {
	if toolName == "" {
		return nil, errors.New("mcp: missing tool name")
	}
	reg := activeRegistration()
	if reg == nil {
		return nil, errors.New("mcp: no game runtime is registered on window.__nsgoMCP")
	}
	if toolName == "tools/list" {
		var tools []ToolInfo
		if reg.cfg.ListTools != nil {
			tools = reg.cfg.ListTools()
		}
		return json.Marshal(map[string]any{"tools": tools})
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	runSafe := func() (raw json.RawMessage, err error) {
		defer func() {
			if r := recover(); r != nil {
				raw = nil
				err = fmt.Errorf("mcp: tool %s panicked: %v", toolName, r)
			}
		}()
		return reg.cfg.CallTool(ctx, toolName, args)
	}

	if reg.cfg.Post == nil {
		return runSafe()
	}

	type outcome struct {
		raw json.RawMessage
		err error
	}
	ch := make(chan outcome, 1)
	reg.cfg.Post(func() {
		raw, err := runSafe()
		ch <- outcome{raw: raw, err: err}
	})

	select {
	case out := <-ch:
		return out.raw, out.err
	case <-reg.done:
		return nil, errors.New("mcp: game runtime detached before tool completed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func ensurePlayModeBridgeProbe() {
	probeStartedMu.Lock()
	if probeStarted {
		probeStartedMu.Unlock()
		return
	}
	probeStarted = true
	probeStartedMu.Unlock()

	global := js.Global()
	if !global.Truthy() {
		return
	}
	if global.Get("__nsgoMCPBridgeProbed").Truthy() || global.Get("__nsgoMCPBridge").Truthy() {
		return
	}
	loc := global.Get("location")
	fetchFn := global.Get("fetch")
	doc := global.Get("document")
	if !loc.Truthy() || fetchFn.Type() != js.TypeFunction || !doc.Truthy() {
		return
	}
	proto := ""
	if p := loc.Get("protocol"); p.Type() == js.TypeString {
		proto = p.String()
	}
	if proto != "http:" && proto != "https:" {
		return
	}
	href := ""
	if h := loc.Get("href"); h.Type() == js.TypeString {
		href = h.String()
	}
	if href == "" {
		return
	}
	global.Set("__nsgoMCPBridgeProbed", true)
	go probePlayModeBridge(href)
}

func probePlayModeBridge(href string) {
	global := js.Global()
	fetchFn := global.Get("fetch")
	if fetchFn.Type() != js.TypeFunction {
		return
	}

	headOpts := global.Get("Object").New()
	headOpts.Set("method", "HEAD")
	headOpts.Set("cache", "no-store")
	headRes, err := awaitPromise(fetchFn.Invoke(href, headOpts))
	if err != nil || !headRes.Truthy() || !headRes.Get("ok").Bool() {
		return
	}
	headers := headRes.Get("headers")
	if !headers.Truthy() {
		return
	}
	playMode := headers.Call("get", playModeHeaderName)
	if playMode.Type() != js.TypeString || !strings.EqualFold(strings.TrimSpace(playMode.String()), "debug") {
		return
	}

	infoPath := defaultBridgeInfo
	if hdr := headers.Call("get", bridgeInfoHeaderName); hdr.Type() == js.TypeString {
		if s := strings.TrimSpace(hdr.String()); s != "" {
			infoPath = s
		}
	}
	if !IsSameOriginPath(infoPath) {
		return
	}

	infoOpts := global.Get("Object").New()
	infoOpts.Set("cache", "no-store")
	infoRes, err := awaitPromise(fetchFn.Invoke(infoPath, infoOpts))
	if err != nil || !infoRes.Truthy() || !infoRes.Get("ok").Bool() {
		return
	}
	info, err := awaitPromise(infoRes.Call("json"))
	if err != nil || info.Type() != js.TypeObject || info.IsNull() {
		return
	}
	if !info.Get("playMode").Bool() {
		return
	}

	scriptPath := defaultBridgeScript
	if s := info.Get("bridgeScript"); s.Type() == js.TypeString {
		if trimmed := strings.TrimSpace(s.String()); trimmed != "" {
			scriptPath = trimmed
		}
	}
	if !IsSameOriginPath(scriptPath) || !strings.HasPrefix(scriptPath, bridgeScriptPrefix) {
		return
	}

	if global.Get("__nsgoMCPBridge").Truthy() {
		return
	}
	doc := global.Get("document")
	if !doc.Truthy() {
		return
	}
	if existing := doc.Call("querySelector", `script[data-nsgo-mcp-bridge="1"]`); !existing.IsNull() && !existing.IsUndefined() {
		return
	}
	script := doc.Call("createElement", "script")
	script.Set("src", scriptPath)
	script.Set("async", true)
	script.Call("setAttribute", "data-nsgo-mcp-bridge", "1")
	parent := doc.Get("head")
	if !parent.Truthy() {
		parent = doc.Get("documentElement")
	}
	if !parent.Truthy() {
		parent = doc.Get("body")
	}
	if parent.Truthy() {
		parent.Call("appendChild", script)
	}
}

func awaitPromise(p js.Value) (js.Value, error) {
	if !p.Truthy() || p.Get("then").Type() != js.TypeFunction {
		return p, nil
	}
	type outcome struct {
		val js.Value
		err error
	}
	ch := make(chan outcome, 1)
	onResolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var v js.Value
		if len(args) > 0 {
			v = args[0]
		}
		select {
		case ch <- outcome{val: v}:
		default:
		}
		return nil
	})
	onReject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "promise rejected"
		if len(args) > 0 && !args[0].IsUndefined() && !args[0].IsNull() {
			if args[0].Type() == js.TypeObject && args[0].Get("message").Type() == js.TypeString {
				msg = args[0].Get("message").String()
			} else {
				msg = args[0].String()
			}
		}
		select {
		case ch <- outcome{err: errors.New(msg)}:
		default:
		}
		return nil
	})
	defer onResolve.Release()
	defer onReject.Release()

	p.Call("then", onResolve, onReject)
	res := <-ch
	return res.val, res.err
}
