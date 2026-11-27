package modding

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/tsx"
)

// HostCapability defines a host-owned capability that mods can request in mod.yaml
// and that gates host-registered TSX modules or individual host module functions.
type HostCapability struct {
	// Name is the canonical capability name in "<namespace>.<name>" form,
	// using dot-separated lower camel case segments (such as "inventory.read").
	Name string
	// Doc describes what the capability permits, for display in player permission prompts.
	Doc string
}

// ModModule defines a host-registered TSX module importable by mods that hold its capability.
type ModModule struct {
	// Name is the JS/TS import specifier, such as "mygame/inventory".
	// It must not be "game" or use NSGo's reserved "nsgo" prefix.
	Name string
	// Funcs maps exported JS function names to Go functions, as [tsx.RegisterFunctions] takes them.
	// Functions receive the calling mod's *core.Scope as their first parameter (or after context.Context for async functions) and can look up the calling mod with [Caller].
	Funcs map[string]any
	// Capability is the declared host capability required to import this module, such as "inventory.read".
	// Either Capability or Capabilities must be set.
	Capability string
	// Capabilities is an optional list of declared host capabilities for modules whose exports span multiple permissions.
	// When set (with Capability empty), a mod may import the module when it holds at least one listed capability, and individual functions enforce their specific permission via [ModAPI.RequireHost].
	Capabilities []string
	// Doc optionally describes this module for generated TypeScript declarations and host UIs.
	Doc string
}

var (
	contextType = reflect.TypeFor[context.Context]()
	scopeType   = reflect.TypeFor[*core.Scope]()
	errorType   = reflect.TypeFor[error]()
)

type boundHostFunc struct {
	fn       reflect.Value
	scopeIdx int
	hasScope bool
}

type boundHostModule struct {
	name         string
	capability   string
	capabilities []string
	doc          string
	funcs        map[string]boundHostFunc
	trampTypes   map[string]reflect.Type
	scopeIdxs    map[string]int
}

func (hm *boundHostModule) allowedBy(effHostCaps []string) bool {
	if hm.capability != "" {
		return slices.Contains(effHostCaps, hm.capability)
	}
	for _, cap := range hm.capabilities {
		if slices.Contains(effHostCaps, cap) {
			return true
		}
	}
	return false
}

var (
	hostRegistryMu sync.Mutex
	hostRegistry   = map[string]map[string]reflect.Type{}
)

// RegisterModule pre-registers a host [ModModule]'s function signatures with [tsx.RegisterFunctions]
// before any TSX runtime starts (for example, from a package init function).
//
// Subsequent [NewManager] calls in the same process—including after TSX runtimes have started or across multiple game sessions—can pass a [ModModule] with the same Name and matching function signatures to bind per-manager implementations. Panics if mod is invalid or already registered.
func RegisterModule(mod ModModule) {
	bound, err := bindHostModuleFuncs(mod)
	if err != nil {
		panic(err)
	}
	hostRegistryMu.Lock()
	defer hostRegistryMu.Unlock()
	if _, exists := hostRegistry[bound.name]; exists {
		panic(fmt.Sprintf("modding: duplicate host module registration %q", bound.name))
	}
	registerTrampolinesLocked(bound)
}

func registerTrampolinesLocked(bound *boundHostModule) {
	trampolines := make(map[string]any, len(bound.trampTypes))
	sigCopy := make(map[string]reflect.Type, len(bound.trampTypes))
	for fnName, trampType := range bound.trampTypes {
		trampolines[fnName] = buildHostTrampoline(bound.name, fnName, trampType, bound.scopeIdxs[fnName])
		sigCopy[fnName] = trampType
	}
	tsx.RegisterFunctions(bound.name, trampolines)
	hostRegistry[bound.name] = sigCopy
}

func ensureHostModuleRegistered(bound *boundHostModule) error {
	hostRegistryMu.Lock()
	defer hostRegistryMu.Unlock()
	if existing, ok := hostRegistry[bound.name]; ok {
		if len(existing) != len(bound.trampTypes) {
			return fmt.Errorf("modding: host module %q already registered with different exports", bound.name)
		}
		for fnName, trampType := range bound.trampTypes {
			if existing[fnName] != trampType {
				return fmt.Errorf("modding: host module %q export %q signature differs from previous registration", bound.name, fnName)
			}
		}
		return nil
	}
	registerTrampolinesLocked(bound)
	return nil
}

func buildHostTrampoline(modName, fnName string, trampType reflect.Type, scopeIdx int) any {
	returnsErr := trampType.NumOut() > 0 && trampType.Out(trampType.NumOut()-1) == errorType
	fail := func(err error) []reflect.Value {
		if !returnsErr {
			panic(err)
		}
		out := make([]reflect.Value, trampType.NumOut())
		for i := range out {
			out[i] = reflect.Zero(trampType.Out(i))
		}
		out[len(out)-1] = reflect.ValueOf(err)
		return out
	}

	tramp := reflect.MakeFunc(trampType, func(args []reflect.Value) []reflect.Value {
		cx, _ := args[scopeIdx].Interface().(*core.Scope)
		api, err := Caller(cx)
		if err != nil {
			return fail(err)
		}
		hm, ok := api.manager.hostModules[modName]
		if !ok {
			return fail(fmt.Errorf("modding: host module %q is not registered on this manager", modName))
		}
		action := modName + "." + fnName
		if hm.capability != "" {
			if err := api.RequireHost(hm.capability, action); err != nil {
				return fail(err)
			}
		} else if len(hm.capabilities) > 0 {
			if !api.hasAnyHostCapability(hm.capabilities) {
				return fail(&PermissionError{
					ModID:          api.manifest.ID,
					HostCapability: hm.capabilities[0],
					Action:         action,
				})
			}
		}
		bound, ok := hm.funcs[fnName]
		if !ok {
			return fail(fmt.Errorf("modding: host module %q has no bound export %q", modName, fnName))
		}
		callArgs := args
		if !bound.hasScope {
			callArgs = make([]reflect.Value, 0, len(args)-1)
			callArgs = append(callArgs, args[:scopeIdx]...)
			callArgs = append(callArgs, args[scopeIdx+1:]...)
		}
		if bound.fn.Type().IsVariadic() {
			return bound.fn.CallSlice(callArgs)
		}
		return bound.fn.Call(callArgs)
	})
	return tramp.Interface()
}

func validateModuleName(name string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("modding: invalid host module name %q", name)
	}
	if name == GameModule {
		return fmt.Errorf("modding: host module name %q collides with built-in %q module", name, GameModule)
	}
	if name == "nsgo" || strings.HasPrefix(name, "nsgo/") || strings.HasPrefix(name, "nsgo:") {
		return fmt.Errorf("modding: host module name %q uses reserved nsgo prefix", name)
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return fmt.Errorf("modding: invalid host module name %q", name)
	}
	for _, r := range name {
		if r <= 0x20 || r == 0x7f {
			return fmt.Errorf("modding: invalid host module name %q", name)
		}
	}
	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("modding: invalid host module name %q", name)
		}
	}
	return nil
}

func bindHostModuleFuncs(mod ModModule) (*boundHostModule, error) {
	if err := validateModuleName(mod.Name); err != nil {
		return nil, err
	}
	if len(mod.Funcs) == 0 {
		return nil, fmt.Errorf("modding: host module %q must define at least one function", mod.Name)
	}
	funcs := make(map[string]boundHostFunc, len(mod.Funcs))
	trampTypes := make(map[string]reflect.Type, len(mod.Funcs))
	scopeIdxs := make(map[string]int, len(mod.Funcs))
	for fnName, fn := range mod.Funcs {
		if fnName == "" || strings.TrimSpace(fnName) != fnName {
			return nil, fmt.Errorf("modding: host module %q has invalid export name %q", mod.Name, fnName)
		}
		ft := reflect.TypeOf(fn)
		if ft == nil || ft.Kind() != reflect.Func {
			return nil, fmt.Errorf("modding: host module %q export %q is not a function", mod.Name, fnName)
		}
		async := ft.NumIn() > 0 && ft.In(0) == contextType
		scopeIdx := 0
		if async {
			scopeIdx = 1
		}
		hasScope := ft.NumIn() > scopeIdx && ft.In(scopeIdx) == scopeType
		trampType := ft
		if !hasScope {
			in := make([]reflect.Type, 0, ft.NumIn()+1)
			if async {
				in = append(in, ft.In(0))
			}
			in = append(in, scopeType)
			for i := scopeIdx; i < ft.NumIn(); i++ {
				in = append(in, ft.In(i))
			}
			out := make([]reflect.Type, ft.NumOut())
			for i := range out {
				out[i] = ft.Out(i)
			}
			trampType = reflect.FuncOf(in, out, ft.IsVariadic())
		}
		funcs[fnName] = boundHostFunc{
			fn:       reflect.ValueOf(fn),
			scopeIdx: scopeIdx,
			hasScope: hasScope,
		}
		trampTypes[fnName] = trampType
		scopeIdxs[fnName] = scopeIdx
	}
	return &boundHostModule{
		name:         mod.Name,
		capability:   mod.Capability,
		capabilities: slices.Clone(mod.Capabilities),
		doc:          mod.Doc,
		funcs:        funcs,
		trampTypes:   trampTypes,
		scopeIdxs:    scopeIdxs,
	}, nil
}

func validateHostConfig(opts ManagerOptions) (
	declaredCaps map[string]HostCapability,
	orderedCaps []HostCapability,
	modules map[string]*boundHostModule,
	moduleOrder []string,
	err error,
) {
	declaredCaps = make(map[string]HostCapability, len(opts.Capabilities))
	orderedCaps = make([]HostCapability, 0, len(opts.Capabilities))
	for _, hc := range opts.Capabilities {
		if !ValidHostCapability(hc.Name) {
			return nil, nil, nil, nil, fmt.Errorf("modding: invalid host capability name %q", hc.Name)
		}
		if _, dup := declaredCaps[hc.Name]; dup {
			return nil, nil, nil, nil, fmt.Errorf("modding: duplicate host capability %q", hc.Name)
		}
		declaredCaps[hc.Name] = hc
		orderedCaps = append(orderedCaps, hc)
	}

	modules = make(map[string]*boundHostModule, len(opts.Modules))
	moduleOrder = make([]string, 0, len(opts.Modules))
	for _, mod := range opts.Modules {
		bound, bindErr := bindHostModuleFuncs(mod)
		if bindErr != nil {
			return nil, nil, nil, nil, bindErr
		}
		if _, dup := modules[bound.name]; dup {
			return nil, nil, nil, nil, fmt.Errorf("modding: duplicate host module %q", bound.name)
		}
		switch {
		case mod.Capability != "" && len(mod.Capabilities) > 0:
			return nil, nil, nil, nil, fmt.Errorf("modding: host module %q must set either Capability or Capabilities, not both", bound.name)
		case mod.Capability != "":
			if _, ok := declaredCaps[mod.Capability]; !ok {
				return nil, nil, nil, nil, fmt.Errorf("modding: host module %q requires undeclared capability %q", bound.name, mod.Capability)
			}
		case len(mod.Capabilities) > 0:
			seen := make(map[string]struct{}, len(mod.Capabilities))
			for _, c := range mod.Capabilities {
				if _, ok := declaredCaps[c]; !ok {
					return nil, nil, nil, nil, fmt.Errorf("modding: host module %q requires undeclared capability %q", bound.name, c)
				}
				if _, dup := seen[c]; dup {
					return nil, nil, nil, nil, fmt.Errorf("modding: host module %q lists capability %q twice", bound.name, c)
				}
				seen[c] = struct{}{}
			}
		default:
			return nil, nil, nil, nil, fmt.Errorf("modding: host module %q must declare a required capability", bound.name)
		}
		modules[bound.name] = bound
		moduleOrder = append(moduleOrder, bound.name)
	}
	return declaredCaps, orderedCaps, modules, moduleOrder, nil
}

// WriteModAPIDTS writes game.d.ts plus one TypeScript declaration file per host module in opts.Modules
// under dir, using [GameAPIDTS] for "game" and [tsx.Declarations] for each host module.
// Parent directories are created as needed.
func WriteModAPIDTS(dir string, opts ManagerOptions) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("modding: WriteModAPIDTS requires a non-empty directory")
	}
	_, _, modules, moduleOrder, err := validateHostConfig(opts)
	if err != nil {
		return err
	}
	for _, name := range moduleOrder {
		if err := ensureHostModuleRegistered(modules[name]); err != nil {
			return err
		}
	}
	if err := WriteGameAPIDTS(filepath.Join(dir, "game.d.ts")); err != nil {
		return err
	}
	if len(moduleOrder) == 0 {
		return nil
	}
	decls := tsx.Declarations()
	for _, name := range moduleOrder {
		key := "types/" + name + ".d.ts"
		content, ok := decls[key]
		if !ok {
			return fmt.Errorf("modding: tsx.Declarations missing %q", key)
		}
		hm := modules[name]
		var header strings.Builder
		fmt.Fprintf(&header, "// TypeScript declarations for host mod module %q.\n", name)
		if hm.doc != "" {
			for line := range strings.SplitSeq(strings.TrimSpace(hm.doc), "\n") {
				fmt.Fprintf(&header, "// %s\n", line)
			}
		}
		if hm.capability != "" {
			fmt.Fprintf(&header, "// Requires capability: %s\n", hm.capability)
		} else if len(hm.capabilities) > 0 {
			fmt.Fprintf(&header, "// Requires one of capabilities: %s\n", strings.Join(hm.capabilities, ", "))
		}
		dst := filepath.Join(dir, filepath.FromSlash(name)+".d.ts")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(header.String()+strings.TrimLeft(content, "\n")), 0o644); err != nil {
			return err
		}
	}
	return nil
}
