package modding

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/tsx"
)

// GameModule is the module specifier mods import to reach the sandboxed game API:
//
//	import { onStep, onEvent, emit, spawn, getState, setState, log } from "game";
const GameModule = "game"

var (
	// ErrPermission is returned when a mod calls an API it lacks the capability for.
	ErrPermission = errors.New("modding: permission denied")
	// ErrNotFound is returned when a mod ID is not known to the manager.
	ErrNotFound = errors.New("modding: mod not found")
	// ErrDuplicateMod is returned when two discovered mods share the same ID.
	ErrDuplicateMod = errors.New("modding: duplicate mod id")
	// ErrMissingDependency is returned when a mod declares a dependency that was not discovered.
	ErrMissingDependency = errors.New("modding: missing dependency")
	// ErrVersionMismatch is returned when a dependency or game version fails a version constraint.
	ErrVersionMismatch = errors.New("modding: incompatible version")
	// ErrCyclicDependency is returned when mod dependencies form a cycle.
	ErrCyclicDependency = errors.New("modding: cyclic dependency")
	// ErrInvalidManifest is returned when a mod.yaml manifest fails parsing or validation.
	ErrInvalidManifest = errors.New("modding: invalid manifest")
	// ErrModDisabled is returned when attempting to load a disabled mod.
	ErrModDisabled = errors.New("modding: mod is disabled")
	// ErrModFailed is returned when interacting with a mod in StateFailed.
	ErrModFailed = errors.New("modding: mod failed")
	// ErrWebUnisolated is returned when a mod would load on the web, where its capabilities cannot be enforced, without ManagerOptions.WebUnisolated.
	ErrWebUnisolated = errors.New("modding: the web cannot isolate mods from the page; set ManagerOptions.WebUnisolated to run them with the page's authority")
	// ErrUnknownCapability is returned when a mod manifest requests a host capability the manager does not define.
	ErrUnknownCapability = errors.New("modding: unknown host capability")
)

// PermissionError reports a blocked capability check. It unwraps to both [ErrPermission]
// and a [*core.Error] with [core.CodeAccessDenied], so TSX catches receive error.code,
// error.permission, and error.resource.
type PermissionError struct {
	ModID          string
	Capability     Capability
	HostCapability string
	Action         string
}

func (e *PermissionError) capName() string {
	if e.HostCapability != "" {
		return e.HostCapability
	}
	return e.Capability.String()
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("modding: mod %q lacks capability %q for %s", e.ModID, e.capName(), e.Action)
}

// Unwrap exposes ErrPermission and a structured core.Error with CodeAccessDenied.
func (e *PermissionError) Unwrap() []error {
	return []error{
		ErrPermission,
		&core.Error{
			Code:       core.CodeAccessDenied,
			Message:    e.Error(),
			Permission: e.capName(),
			Resource:   e.Action,
		},
	}
}

// SpawnRequest describes an entity spawn requested by a mod.
type SpawnRequest struct {
	ModID  string
	Prefab string
	X, Y   float64
	Props  map[string]any
}

// Event describes an event emitted by a mod or by the host game.
type Event struct {
	// ModID is the emitting mod's ID, or empty when emitted by the host.
	ModID   string
	Name    string
	Payload any
}

// SoundRequest describes a sound playback requested by a mod.
type SoundRequest struct {
	ModID  string
	Name   string
	Volume float64
	Rate   float64
	Loop   bool
}

// LogEntry is one captured log line emitted by a mod via game.log.
type LogEntry struct {
	ModID   string
	Message string
	At      time.Time
}

// Panel is a TSX UI panel or HUD widget mounted by a mod via game.mountPanel.
type Panel struct {
	ModID string
	ID    string
	Slot  string
	Node  core.Node
}

// GameBridge connects sandboxed mods to the host game.
// All callbacks are optional: when nil, [Manager] supplies safe built-in defaults
// for scoped state, entity ID generation, event dispatch, and UI panels.
type GameBridge struct {
	// OnSpawn handles entity spawn requests from mods with CapSpawnEntity.
	// Returning an entity ID passes it back to the mod; returning an error throws in JS.
	OnSpawn func(req SpawnRequest) (string, error)
	// OnEmit is notified whenever a mod with CapEmitEvent emits an event.
	// Events are also dispatched to subscribed mods in load order.
	OnEmit func(ev Event)
	// OnGetState reads a scoped state key for mods with CapReadState.
	// When nil, reads from the manager's built-in state map.
	OnGetState func(modID, key string) (any, bool)
	// OnSetState writes a scoped state key for mods with CapWriteState or CapStorage.
	// When nil, writes to the manager's built-in state map.
	OnSetState func(modID, key string, val any) error
	// OnPlaySound handles audio playback requests from mods with CapAudio.
	OnPlaySound func(req SoundRequest) error
	// OnLog is notified whenever a mod logs a message, in addition to the per-mod log buffer.
	OnLog func(entry LogEntry)
	// OnPanelsChange is notified whenever a mod mounts or unmounts a UI panel.
	OnPanelsChange func()
}

type stepHook struct {
	id     uint64
	fn     func(dt float64)
	active bool
}

type eventHook struct {
	id     uint64
	name   string
	fn     func(payload any)
	active bool
}

type mountedPanel struct {
	id     string
	slot   string
	render func() core.Node
	node   core.Node
}

// ModAPI is the capability-gated game API bound to a single loaded mod instance.
// TSX/JS mods access it by importing functions from the "game" module; Go code may
// also inspect or invoke it via [Manager.API].
type ModAPI struct {
	manifest ModManifest
	caps     Capability
	hostMu   sync.RWMutex
	hostCaps []string
	manager  *Manager
	scope    *core.Scope // mod's parent scope where sessionKey is provided
	vmScope  *core.Scope // inner tsx runtime scope captured on first module call
	closed   bool

	nextHookID uint64
	stepHooks  []*stepHook
	eventHooks map[string][]*eventHook
	panels     []*mountedPanel
	logs       []LogEntry
}

// sessionKey binds the active *ModAPI to a mod's core.Scope hierarchy.
var sessionKey = core.NewKey[*ModAPI]("game.modding.session", nil)

func init() {
	tsx.RegisterFunctions(GameModule, map[string]any{
		"registerHook":  jsRegisterHook,
		"onStep":        jsOnStep,
		"onEvent":       jsOnEvent,
		"emit":          jsEmit,
		"spawn":         jsSpawn,
		"getState":      jsGetState,
		"setState":      jsSetState,
		"playSound":     jsPlaySound,
		"log":           jsLog,
		"mountPanel":    jsMountPanel,
		"unmountPanel":  jsUnmountPanel,
		"storageGet":    jsStorageGet,
		"storageSet":    jsStorageSet,
		"storageRemove": jsStorageRemove,
		"modInfo":       jsModInfo,
	})
}

// Caller returns the [*ModAPI] of the mod whose code is running in cx, for host module functions.
func Caller(cx *core.Scope) (*ModAPI, error) {
	return currentAPI(cx)
}

func currentAPI(cx *core.Scope) (*ModAPI, error) {
	if cx == nil {
		return nil, errors.New("modding: game API called outside a mod scope")
	}
	api := sessionKey.Use(cx)
	if api == nil {
		return nil, errors.New("modding: game API called outside a managed mod")
	}
	if api.closed {
		return nil, fmt.Errorf("modding: mod %q has been unloaded", api.manifest.ID)
	}
	if api.vmScope == nil {
		api.vmScope = cx
	}
	return api, nil
}

// Manifest returns the mod's manifest.
func (a *ModAPI) Manifest() ModManifest { return a.manifest }

// Capabilities returns the effective built-in capabilities granted to this mod.
func (a *ModAPI) Capabilities() Capability { return a.caps }

// HostCapabilities returns a copy of the effective host capability names granted to this mod, sorted in byte order.
func (a *ModAPI) HostCapabilities() []string {
	a.hostMu.RLock()
	defer a.hostMu.RUnlock()
	return slices.Clone(a.hostCaps)
}

// HasHostCapability reports whether this mod is currently granted the named host capability.
func (a *ModAPI) HasHostCapability(capability string) bool {
	a.hostMu.RLock()
	defer a.hostMu.RUnlock()
	return slices.Contains(a.hostCaps, capability)
}

func (a *ModAPI) hasAnyHostCapability(capabilities []string) bool {
	a.hostMu.RLock()
	defer a.hostMu.RUnlock()
	for _, cap := range capabilities {
		if slices.Contains(a.hostCaps, cap) {
			return true
		}
	}
	return false
}

func (a *ModAPI) setHostCapabilities(caps []string) {
	a.hostMu.Lock()
	a.hostCaps = slices.Clone(caps)
	a.hostMu.Unlock()
}

// RequireHost verifies that this mod is active and currently holds the named host capability.
// When denied, it returns a [*PermissionError] wrapping [ErrPermission] and a [*core.Error] with [core.CodeAccessDenied].
func (a *ModAPI) RequireHost(capability, action string) error {
	if a.closed {
		return fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if !a.HasHostCapability(capability) {
		return &PermissionError{
			ModID:          a.manifest.ID,
			HostCapability: capability,
			Action:         action,
		}
	}
	return nil
}

// Logs returns a copy of the log entries captured for this mod.
func (a *ModAPI) Logs() []LogEntry {
	return slices.Clone(a.logs)
}

func (a *ModAPI) require(req Capability, action string) error {
	if a.closed {
		return fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if !a.caps.Has(req) {
		return &PermissionError{ModID: a.manifest.ID, Capability: req, Action: action}
	}
	return nil
}

func (a *ModAPI) requireAny(allowed, primary Capability, action string) error {
	if a.closed {
		return fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if a.caps&allowed == 0 {
		return &PermissionError{ModID: a.manifest.ID, Capability: primary, Action: action}
	}
	return nil
}

// RegisterHook subscribes fn to event. Event "step" subscribes to fixed simulation steps
// (receiving dt in seconds); any other name subscribes to emitted events of that name.
func (a *ModAPI) RegisterHook(event string, fn func(any)) (func(), error) {
	if a.closed {
		return nil, fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if strings.TrimSpace(event) == "" {
		return nil, errors.New("modding: registerHook requires a non-empty event name")
	}
	if fn == nil {
		return nil, errors.New("modding: registerHook requires a callback function")
	}
	if event == "step" {
		return a.OnStep(func(dt float64) { fn(dt) })
	}
	return a.OnEvent(event, fn)
}

// OnStep registers a callback invoked on each [Manager.Step] tick with dt in seconds.
func (a *ModAPI) OnStep(fn func(dt float64)) (func(), error) {
	if a.closed {
		return nil, fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if fn == nil {
		return nil, errors.New("modding: onStep requires a callback function")
	}
	a.nextHookID++
	h := &stepHook{id: a.nextHookID, fn: fn, active: true}
	a.stepHooks = append(a.stepHooks, h)
	return func() {
		if !h.active || a.closed {
			h.active = false
			return
		}
		h.active = false
		a.stepHooks = slices.DeleteFunc(a.stepHooks, func(x *stepHook) bool { return x.id == h.id })
	}, nil
}

// OnEvent registers a callback invoked whenever an event named name is emitted.
func (a *ModAPI) OnEvent(name string, fn func(payload any)) (func(), error) {
	if a.closed {
		return nil, fmt.Errorf("modding: mod %q has been unloaded", a.manifest.ID)
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("modding: onEvent requires a non-empty event name")
	}
	if fn == nil {
		return nil, errors.New("modding: onEvent requires a callback function")
	}
	if a.eventHooks == nil {
		a.eventHooks = map[string][]*eventHook{}
	}
	a.nextHookID++
	h := &eventHook{id: a.nextHookID, name: name, fn: fn, active: true}
	a.eventHooks[name] = append(a.eventHooks[name], h)
	return func() {
		// After the mod unloads its hooks are gone (and eventHooks is nil), so unsubscribing then does nothing (#546).
		if !h.active || a.closed {
			h.active = false
			return
		}
		h.active = false
		a.eventHooks[name] = slices.DeleteFunc(a.eventHooks[name], func(x *eventHook) bool { return x.id == h.id })
		if len(a.eventHooks[name]) == 0 {
			delete(a.eventHooks, name)
		}
	}, nil
}

// Emit emits an event to the host bridge and all loaded mods subscribed to name.
// Requires [CapEmitEvent].
func (a *ModAPI) Emit(name string, payload any) error {
	if err := a.require(CapEmitEvent, "emit"); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("modding: emit requires a non-empty event name")
	}
	ev := Event{ModID: a.manifest.ID, Name: name, Payload: payload}
	return a.manager.dispatchEvent(ev)
}

// Spawn requests the host game to spawn an entity of prefab at (x, y) with optional props.
// Requires [CapSpawnEntity].
func (a *ModAPI) Spawn(prefab string, x, y float64, props map[string]any) (string, error) {
	if err := a.require(CapSpawnEntity, "spawn"); err != nil {
		return "", err
	}
	if strings.TrimSpace(prefab) == "" {
		return "", errors.New("modding: spawn requires a non-empty prefab name")
	}
	req := SpawnRequest{
		ModID:  a.manifest.ID,
		Prefab: prefab,
		X:      x,
		Y:      y,
		Props:  props,
	}
	if a.manager.opts.Bridge.OnSpawn != nil {
		return a.manager.opts.Bridge.OnSpawn(req)
	}
	a.manager.spawnSeq++
	return fmt.Sprintf("%s:%s:%d", a.manifest.ID, prefab, a.manager.spawnSeq), nil
}

// GetState reads a key from scoped game state. Requires [CapReadState].
func (a *ModAPI) GetState(key string) (any, bool, error) {
	if err := a.require(CapReadState, "getState"); err != nil {
		return nil, false, err
	}
	if a.manager.opts.Bridge.OnGetState != nil {
		v, ok := a.manager.opts.Bridge.OnGetState(a.manifest.ID, key)
		return v, ok, nil
	}
	v, ok := a.manager.state[key]
	return v, ok, nil
}

// SetState writes a key to scoped game state. Requires [CapWriteState] or [CapStorage].
func (a *ModAPI) SetState(key string, val any) error {
	if err := a.requireAny(CapWriteState|CapStorage, CapWriteState, "setState"); err != nil {
		return err
	}
	if a.manager.opts.Bridge.OnSetState != nil {
		return a.manager.opts.Bridge.OnSetState(a.manifest.ID, key, val)
	}
	a.manager.state[key] = val
	return nil
}

// PlaySound requests the host game to play a named sound. Requires [CapAudio].
func (a *ModAPI) PlaySound(name string, opts map[string]any) error {
	if err := a.require(CapAudio, "playSound"); err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("modding: playSound requires a non-empty sound name")
	}
	req := SoundRequest{
		ModID:  a.manifest.ID,
		Name:   name,
		Volume: 1,
		Rate:   1,
	}
	if opts != nil {
		if v, ok := asFloat64(opts["volume"]); ok {
			req.Volume = v
		}
		if v, ok := asFloat64(opts["rate"]); ok {
			req.Rate = v
		}
		if v, ok := opts["loop"].(bool); ok {
			req.Loop = v
		}
	}
	if a.manager.opts.Bridge.OnPlaySound != nil {
		return a.manager.opts.Bridge.OnPlaySound(req)
	}
	return nil
}

// Log appends a message to the mod's captured log buffer and notifies Bridge.OnLog.
func (a *ModAPI) Log(msg string) {
	if a.closed {
		return
	}
	entry := LogEntry{
		ModID:   a.manifest.ID,
		Message: msg,
		At:      a.manager.cx.Now(),
	}
	maxLogs := a.manager.maxLogs()
	if len(a.logs) >= maxLogs {
		copy(a.logs, a.logs[1:])
		a.logs[len(a.logs)-1] = entry
	} else {
		a.logs = append(a.logs, entry)
	}
	if a.manager.opts.Bridge.OnLog != nil {
		a.manager.opts.Bridge.OnLog(entry)
	}
}

// MountPanel mounts or replaces a TSX UI panel or HUD widget for this mod. Requires [CapUI].
func (a *ModAPI) MountPanel(id string, render func() core.Node, slot string) error {
	if err := a.require(CapUI, "mountPanel"); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("modding: mountPanel requires a non-empty panel id")
	}
	if render == nil {
		return errors.New("modding: mountPanel requires a TSX element or render function")
	}
	for _, p := range a.panels {
		if p.id == id {
			p.slot = slot
			p.render = render
			p.node = nil
			a.manager.notifyPanelsChanged()
			return nil
		}
	}
	a.panels = append(a.panels, &mountedPanel{
		id:     id,
		slot:   slot,
		render: render,
	})
	a.manager.notifyPanelsChanged()
	return nil
}

// UnmountPanel removes a previously mounted TSX UI panel. Requires [CapUI].
func (a *ModAPI) UnmountPanel(id string) error {
	if err := a.require(CapUI, "unmountPanel"); err != nil {
		return err
	}
	before := len(a.panels)
	a.panels = slices.DeleteFunc(a.panels, func(p *mountedPanel) bool { return p.id == id })
	if len(a.panels) != before {
		a.manager.notifyPanelsChanged()
	}
	return nil
}

// StorageGet reads a string value from this mod's isolated persistent storage. Requires [CapStorage].
func (a *ModAPI) StorageGet(key string) (string, bool, error) {
	if err := a.require(CapStorage, "storageGet"); err != nil {
		return "", false, err
	}
	v, ok := a.manager.modStorageGet(a.manifest.ID, key)
	return v, ok, nil
}

// StorageSet writes a string value to this mod's isolated persistent storage. Requires [CapStorage].
func (a *ModAPI) StorageSet(key, val string) error {
	if err := a.require(CapStorage, "storageSet"); err != nil {
		return err
	}
	return a.manager.modStorageSet(a.manifest.ID, key, val)
}

// StorageRemove removes a key from this mod's isolated persistent storage. Requires [CapStorage].
func (a *ModAPI) StorageRemove(key string) error {
	if err := a.require(CapStorage, "storageRemove"); err != nil {
		return err
	}
	a.manager.modStorageRemove(a.manifest.ID, key)
	return nil
}

func (a *ModAPI) cleanup() bool {
	a.closed = true
	for _, h := range a.stepHooks {
		h.active = false
	}
	for _, hooks := range a.eventHooks {
		for _, h := range hooks {
			h.active = false
		}
	}
	a.stepHooks = nil
	a.eventHooks = nil
	hadPanels := len(a.panels) > 0
	a.panels = nil
	return hadPanels
}

// TSX module entrypoints registered under "game".

func jsRegisterHook(cx *core.Scope, event string, fn func(any)) (func(), error) {
	api, err := currentAPI(cx)
	if err != nil {
		return nil, err
	}
	return api.RegisterHook(event, fn)
}

func jsOnStep(cx *core.Scope, fn func(float64)) (func(), error) {
	api, err := currentAPI(cx)
	if err != nil {
		return nil, err
	}
	return api.OnStep(fn)
}

func jsOnEvent(cx *core.Scope, name string, fn func(any)) (func(), error) {
	api, err := currentAPI(cx)
	if err != nil {
		return nil, err
	}
	return api.OnEvent(name, fn)
}

func jsEmit(cx *core.Scope, name string, payload any) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.Emit(name, payload)
}

func jsSpawn(cx *core.Scope, prefab string, x, y float64, props map[string]any) (string, error) {
	api, err := currentAPI(cx)
	if err != nil {
		return "", err
	}
	return api.Spawn(prefab, x, y, props)
}

func jsGetState(cx *core.Scope, key string) (any, error) {
	api, err := currentAPI(cx)
	if err != nil {
		return nil, err
	}
	v, ok, err := api.GetState(key)
	if err != nil || !ok {
		return nil, err
	}
	return v, nil
}

func jsSetState(cx *core.Scope, key string, val any) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.SetState(key, val)
}

func jsPlaySound(cx *core.Scope, name string, opts map[string]any) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.PlaySound(name, opts)
}

func jsLog(cx *core.Scope, args ...any) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		if arg == nil {
			parts[i] = "null"
		} else {
			parts[i] = fmt.Sprint(arg)
		}
	}
	api.Log(strings.Join(parts, " "))
	return nil
}

func jsMountPanel(cx *core.Scope, id string, render func() core.Node, slot string) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.MountPanel(id, render, slot)
}

func jsUnmountPanel(cx *core.Scope, id string) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.UnmountPanel(id)
}

func jsStorageGet(cx *core.Scope, key string) (string, bool, error) {
	api, err := currentAPI(cx)
	if err != nil {
		return "", false, err
	}
	return api.StorageGet(key)
}

func jsStorageSet(cx *core.Scope, key, val string) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.StorageSet(key, val)
}

func jsStorageRemove(cx *core.Scope, key string) error {
	api, err := currentAPI(cx)
	if err != nil {
		return err
	}
	return api.StorageRemove(key)
}

func jsModInfo(cx *core.Scope) (map[string]any, error) {
	api, err := currentAPI(cx)
	if err != nil {
		return nil, err
	}
	caps := append(api.caps.Names(), api.HostCapabilities()...)
	return map[string]any{
		"id":           api.manifest.ID,
		"name":         api.manifest.Name,
		"version":      api.manifest.Version,
		"author":       api.manifest.Author,
		"description":  api.manifest.Description,
		"capabilities": caps,
	}, nil
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
	default:
		return 0, false
	}
}

// GameAPIDTS is the TypeScript declaration file (game.d.ts) for the sandboxed "game" module.
// Mod authors can include it in their tsconfig.json to get full TypeScript type checking.
const GameAPIDTS = `// TypeScript declarations for NSGo Game Modding API ("game" module).

declare module "game" {
  export type CapabilityName =
    | "readState"
    | "writeState"
    | "spawnEntity"
    | "emitEvent"
    | "ui"
    | "audio"
    | "storage";

  export type Unsubscribe = () => void;

  export interface ModInfo {
    readonly id: string;
    readonly name: string;
    readonly version: string;
    readonly author: string;
    readonly description: string;
    readonly capabilities: (CapabilityName | (string & {}))[];
  }

  export interface SoundOptions {
    volume?: number;
    rate?: number;
    loop?: boolean;
  }

  export type PanelContent = unknown | (() => unknown);

  /**
   * Registers a hook for a named game event.
   * Use "step" to subscribe to fixed-rate simulation ticks (payload is dt in seconds).
   */
  export function registerHook(event: string, fn: (payload: any) => void): Unsubscribe;

  /**
   * Registers a callback invoked on every fixed-rate simulation step.
   * @param fn Callback receiving the step duration dt in seconds.
   */
  export function onStep(fn: (dt: number) => void): Unsubscribe;

  /**
   * Subscribes to a named game event emitted by the host or another mod.
   */
  export function onEvent<T = any>(name: string, fn: (payload: T) => void): Unsubscribe;

  /**
   * Emits a named event to the host and subscribed mods.
   * Requires the "emitEvent" capability.
   */
  export function emit(name: string, payload?: any): void;

  /**
   * Spawns a game entity from a prefab at world coordinates (x, y).
   * Returns the spawned entity ID.
   * Requires the "spawnEntity" capability.
   */
  export function spawn(
    prefab: string,
    x: number,
    y: number,
    props?: Record<string, any>
  ): string;

  /**
   * Reads a value from scoped game state, or null if absent.
   * Requires the "readState" capability.
   */
  export function getState<T = any>(key: string): T | null;

  /**
   * Writes a value to scoped game state.
   * Requires the "writeState" or "storage" capability.
   */
  export function setState(key: string, value: any): void;

  /**
   * Requests playback of a named sound asset.
   * Requires the "audio" capability.
   */
  export function playSound(name: string, opts?: SoundOptions): void;

  /**
   * Appends a message to this mod's captured log buffer.
   */
  export function log(...args: any[]): void;

  /**
   * Mounts or updates a TSX UI panel or HUD widget in an optional named slot.
   * Requires the "ui" capability.
   */
  export function mountPanel(id: string, render: PanelContent, slot?: string): void;

  /**
   * Unmounts a previously mounted TSX UI panel by ID.
   * Requires the "ui" capability.
   */
  export function unmountPanel(id: string): void;

  /**
   * Reads a string from this mod's isolated persistent storage, or null if absent.
   * Requires the "storage" capability.
   */
  export function storageGet(key: string): string | null;

  /**
   * Writes a string to this mod's isolated persistent storage.
   * Requires the "storage" capability.
   */
  export function storageSet(key: string, value: string): void;

  /**
   * Removes a key from this mod's isolated persistent storage.
   * Requires the "storage" capability.
   */
  export function storageRemove(key: string): void;

  /**
   * Returns metadata and effective capabilities for the calling mod.
   */
  export function modInfo(): ModInfo;
}
`

// WriteGameAPIDTS writes [GameAPIDTS] to dst (creating parent directories as needed)
// so mod projects can reference game.d.ts for TypeScript type checking.
func WriteGameAPIDTS(dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(GameAPIDTS), 0o644)
}
