package gameplugin

import (
	"errors"
	"fmt"
	"maps"
	rtdebug "runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/gameplugin/manifest"
	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

// APIVersion is the runtime plugin API contract version supported by this engine build.
// It is bumped only on breaking changes to the plugin contract. Every [Plugin] must set API to APIVersion.
const APIVersion = 1

var (
	// ErrNilHost reports a nil *Host passed to Install or a Host method.
	ErrNilHost = errors.New("gameplugin: nil host")
	// ErrNilWorld reports a nil *entities.World passed to Host.OnWorld.
	ErrNilWorld = errors.New("gameplugin: nil world")
	// ErrInvalidPlugin reports a Plugin with an invalid name, module path, or malformed contribution.
	ErrInvalidPlugin = errors.New("gameplugin: invalid plugin")
	// ErrAPIMismatch reports a Plugin whose API does not equal APIVersion.
	ErrAPIMismatch = errors.New("gameplugin: unsupported API version")
	// ErrDuplicatePlugin reports installing a plugin whose Name is already installed on the Host or repeated in the same Install call.
	ErrDuplicatePlugin = errors.New("gameplugin: duplicate plugin")
	// ErrConflict reports a contribution whose name, pattern, or effect ID collides with an existing entry or another plugin in the same Install call.
	ErrConflict = errors.New("gameplugin: contribution conflict")
)

// Stage identifies one of the four standard scheduler stages that a plugin system can attach to without knowing the game's own system names.
//
// [Install] adds anchor systems for the four stages once per [entities.Scheduler] in the order [StageInput] -> [StageUpdate] -> [StagePhysics] -> [StageLate]. A [System] with Stage set is constrained with entities.After(stage.Start()) and entities.Before(stage.End()).
type Stage string

const (
	// StageInput runs first on each fixed step, for reading input and translating player intent.
	StageInput Stage = "input"
	// StageUpdate runs after StageInput, for general gameplay systems.
	StageUpdate Stage = "update"
	// StagePhysics runs after StageUpdate, for physics, movement, and collision resolution.
	StagePhysics Stage = "physics"
	// StageLate runs last, after StagePhysics, for camera follow, transform synchronization, and cleanup.
	StageLate Stage = "late"
)

var standardStages = []Stage{StageInput, StageUpdate, StagePhysics, StageLate}

// Stages returns the four standard scheduler stages in execution order.
func Stages() []Stage {
	return slices.Clone(standardStages)
}

// Valid reports whether s is one of the four standard stages.
func (s Stage) Valid() bool {
	switch s {
	case StageInput, StageUpdate, StagePhysics, StageLate:
		return true
	default:
		return false
	}
}

// String returns the stage's name.
func (s Stage) String() string {
	return string(s)
}

// Start returns the scheduler anchor system name marking the start of s, or empty if s is not a valid stage.
func (s Stage) Start() string {
	if !s.Valid() {
		return ""
	}
	return "plugin.stage." + string(s) + ".start"
}

// End returns the scheduler anchor system name marking the end of s, or empty if s is not a valid stage.
func (s Stage) End() string {
	if !s.Valid() {
		return ""
	}
	return "plugin.stage." + string(s) + ".end"
}

func isReservedStageAnchor(name string) bool {
	for _, st := range standardStages {
		if name == st.Start() || name == st.End() {
			return true
		}
	}
	return false
}

func noopSystem(*entities.World, time.Duration) {}

// EnsureStages registers the standard stage anchor systems ([StageInput], [StageUpdate], [StagePhysics], [StageLate]) on s if they are not already present. It is safe to call repeatedly on the same scheduler.
func EnsureStages(s *entities.Scheduler) error {
	if s == nil {
		return errors.New("gameplugin: nil scheduler")
	}
	var prevEnd string
	for _, st := range standardStages {
		start := st.Start()
		end := st.End()
		if !s.HasSystem(start) {
			var opts []entities.SystemOption
			if prevEnd != "" {
				opts = append(opts, entities.After(prevEnd))
			}
			if err := s.AddSystem(start, noopSystem, opts...); err != nil {
				return fmt.Errorf("gameplugin: register stage anchor %q: %w", start, err)
			}
		}
		if !s.HasSystem(end) {
			if err := s.AddSystem(end, noopSystem, entities.After(start)); err != nil {
				return fmt.Errorf("gameplugin: register stage anchor %q: %w", end, err)
			}
		}
		prevEnd = end
	}
	return nil
}

// System describes one ECS update system a [Plugin] registers on [Host.Scheduler].
type System struct {
	// Name uniquely identifies the system within the scheduler.
	Name string
	// Stage optionally places the system inside one of the four standard stages ([StageInput], [StageUpdate], [StagePhysics], [StageLate]). Leave empty to constrain only via Before and After.
	Stage Stage
	// Before lists system names this system must run before.
	Before []string
	// After lists system names this system must run after.
	After []string
	// Fn is the update function run on each Scheduler.Step.
	Fn func(w *entities.World, dt time.Duration)
}

// ConsoleCommand describes one developer console command a [Plugin] registers on [Host.Console].
type ConsoleCommand struct {
	// Name is the command token typed in the console (non-empty, without spaces or control characters).
	Name string
	// Help is the one-line usage description shown by the console's help command.
	Help string
	// Fn executes the command with its tokenized arguments and returns output text.
	Fn func(args []string) (string, error)
}

// Plugin describes a runtime Go plugin's identity and declarative contributions.
//
// There is deliberately no Version field: a plugin's version is its Go module version resolved by the game project's go.mod and read by [Install] from runtime/debug.ReadBuildInfo.
type Plugin struct {
	// Name uniquely identifies the plugin using the same rule as plugin.yaml's name (1 to 64 lower-case ASCII letters, digits and '-', starting with a letter or digit).
	Name string
	// Module is the plugin's Go module path, matching go.mod and plugin.yaml when set. Install reads its version from runtime/debug.ReadBuildInfo.
	Module string
	// API must equal [APIVersion].
	API int
	// Components declares scene component types (including optional Spawn factories) merged into Host.Components in slice order.
	Components []sceneformat.ComponentType
	// Systems declares ECS scheduler systems added to Host.Scheduler in slice order.
	Systems []System
	// Codecs registers snapshot codecs on every entities.World passed to Host.OnWorld.
	Codecs func(*entities.World) error
	// PixelFX declares procedural pixel effects registered in the process-global pixelfx registry in sorted ID order, once per process per owning plugin.
	PixelFX map[string]*pixelfx.Effect
	// RowTypes maps data-table file patterns to typed Go row schemas registered on Host.DataTables in sorted pattern order.
	RowTypes map[string]*datatable.RowType
	// Console declares developer console commands registered on Host.Console in slice order.
	Console []ConsoleCommand
	// Install is an optional escape hatch called last after all declarative contributions have been validated and registered.
	Install func(*Host) error
}

// Installed describes a plugin that has been installed on a [Host].
type Installed struct {
	// Name is the plugin's unique short identifier.
	Name string
	// Module is the plugin's Go module path, if declared.
	Module string
	// Version is the Go module version from runtime/debug.ReadBuildInfo, "(devel)" for a local build or replace directive without a version, or empty when Module is empty or absent from build info.
	Version string
}

type codecEntry struct {
	plugin string
	fn     func(*entities.World) error
}

// Host holds the runtime registries that [Install] wires plugins into. Any nil registry field skips that kind of contribution.
type Host struct {
	// Components receives plugin scene component types (including ComponentType.Spawn) when non-nil.
	Components *sceneformat.ComponentRegistry
	// Scheduler receives standard stage anchors and plugin systems when non-nil.
	Scheduler *entities.Scheduler
	// DataTables receives plugin data-table row types when non-nil.
	DataTables *datatable.Registry
	// Console receives plugin console commands when non-nil.
	Console *gamedebug.Console

	installed    []Installed
	installedSet map[string]struct{}
	codecs       []codecEntry
}

// Plugins returns the plugins installed on h in installation order.
func (h *Host) Plugins() []Installed {
	if h == nil || len(h.installed) == 0 {
		return nil
	}
	return slices.Clone(h.installed)
}

// Has reports whether a plugin named name has been installed on h.
func (h *Host) Has(name string) bool {
	if h == nil || name == "" {
		return false
	}
	_, ok := h.installedSet[name]
	return ok
}

// OnWorld runs the [Plugin.Codecs] callback of every installed plugin on w, in installation order.
func (h *Host) OnWorld(w *entities.World) error {
	if h == nil {
		return ErrNilHost
	}
	if w == nil {
		return ErrNilWorld
	}
	for _, entry := range h.codecs {
		if err := entry.fn(w); err != nil {
			return fmt.Errorf("plugin %q: register codecs: %w", entry.plugin, err)
		}
	}
	return nil
}

// ModuleVersion returns the resolved Go module version of modulePath from runtime/debug.ReadBuildInfo, "(devel)" for a local build or replace directive without a version, or empty when modulePath is empty or not present in build info.
func ModuleVersion(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	bi, ok := rtdebug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return moduleVersionFromBuildInfo(bi, modulePath)
}

func moduleVersionFromBuildInfo(bi *rtdebug.BuildInfo, modulePath string) string {
	if bi == nil || modulePath == "" {
		return ""
	}
	if bi.Main.Path == modulePath {
		return resolveModuleVersion(&bi.Main)
	}
	for _, dep := range bi.Deps {
		if dep != nil && dep.Path == modulePath {
			return resolveModuleVersion(dep)
		}
	}
	return ""
}

func resolveModuleVersion(m *rtdebug.Module) string {
	if m == nil {
		return ""
	}
	if m.Replace != nil {
		if m.Replace.Version != "" {
			return m.Replace.Version
		}
		return "(devel)"
	}
	return m.Version
}

type pluginOwner struct {
	name   string
	module string
}

var (
	pixelfxMu     sync.Mutex
	pixelfxOwners = map[string]pluginOwner{}
)

type pixelFXItem struct {
	id         string
	effect     *pixelfx.Effect
	owner      pluginOwner
	pluginName string
	skip       bool
}

// Install validates every plugin in ps against h and the process-global pixelfx registry before registering anything, then installs their contributions in deterministic order (plugins in argument order, slice fields in declaration order, maps by sorted key) and runs each plugin's Install escape hatch last.
func Install(h *Host, ps ...Plugin) error {
	if h == nil {
		return ErrNilHost
	}
	if len(ps) == 0 {
		return nil
	}

	batchPlugins := make(map[string]struct{}, len(ps))
	tempComps := sceneformat.NewComponentRegistry()
	compOwners := make(map[string]string)
	batchSystems := make(map[string]string)
	tempDT := datatable.NewRegistry()
	dtOwners := make(map[string]string)
	batchConsole := make(map[string]string)
	batchPixelFX := make(map[string]string)
	var pixelFXPlan []pixelFXItem

	var existingConsole map[string]struct{}
	if h.Console != nil {
		cmds := h.Console.Commands()
		existingConsole = make(map[string]struct{}, len(cmds))
		for _, c := range cmds {
			existingConsole[c.Name] = struct{}{}
		}
	}

	// Phase 1: pre-flight validation of all plugins and non-global contributions.
	for _, p := range ps {
		if err := manifest.ValidateName(p.Name); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidPlugin, err)
		}
		if p.Module != "" {
			if err := manifest.ValidateModulePath(p.Module); err != nil {
				return fmt.Errorf("plugin %q: %w: %v", p.Name, ErrInvalidPlugin, err)
			}
		}
		if p.API != APIVersion {
			return fmt.Errorf("plugin %q: %w: got %d, want %d", p.Name, ErrAPIMismatch, p.API, APIVersion)
		}
		if h.Has(p.Name) {
			return fmt.Errorf("plugin %q: %w: already installed on host", p.Name, ErrDuplicatePlugin)
		}
		if _, dup := batchPlugins[p.Name]; dup {
			return fmt.Errorf("plugin %q: %w: listed twice in Install", p.Name, ErrDuplicatePlugin)
		}
		batchPlugins[p.Name] = struct{}{}

		// Components (slice order)
		for _, ct := range p.Components {
			if ct.Name == "" {
				return fmt.Errorf("plugin %q: %w: component type has an empty name", p.Name, ErrInvalidPlugin)
			}
			if prev, dup := compOwners[ct.Name]; dup {
				return fmt.Errorf("plugin %q: %w: component %q already declared by plugin %q", p.Name, ErrConflict, ct.Name, prev)
			}
			if h.Components != nil {
				if _, exists := h.Components.Lookup(ct.Name); exists {
					return fmt.Errorf("plugin %q: %w: component %q is already registered on host", p.Name, ErrConflict, ct.Name)
				}
			}
			if err := tempComps.Register(ct); err != nil {
				return fmt.Errorf("plugin %q: %w: %v", p.Name, ErrInvalidPlugin, err)
			}
			compOwners[ct.Name] = p.Name
		}

		// Systems (slice order)
		for _, sys := range p.Systems {
			if sys.Name == "" {
				return fmt.Errorf("plugin %q: %w: system has an empty name", p.Name, ErrInvalidPlugin)
			}
			if isReservedStageAnchor(sys.Name) {
				return fmt.Errorf("plugin %q: %w: system %q uses a reserved stage anchor name", p.Name, ErrConflict, sys.Name)
			}
			if sys.Fn == nil {
				return fmt.Errorf("plugin %q: %w: system %q has a nil update function", p.Name, ErrInvalidPlugin, sys.Name)
			}
			if sys.Stage != "" && !sys.Stage.Valid() {
				return fmt.Errorf("plugin %q: %w: system %q has unknown stage %q", p.Name, ErrInvalidPlugin, sys.Name, sys.Stage)
			}
			for _, target := range sys.Before {
				if target == "" {
					return fmt.Errorf("plugin %q: %w: system %q has an empty Before target", p.Name, ErrInvalidPlugin, sys.Name)
				}
				if target == sys.Name {
					return fmt.Errorf("plugin %q: %w: system %q cannot run before itself", p.Name, ErrInvalidPlugin, sys.Name)
				}
			}
			for _, target := range sys.After {
				if target == "" {
					return fmt.Errorf("plugin %q: %w: system %q has an empty After target", p.Name, ErrInvalidPlugin, sys.Name)
				}
				if target == sys.Name {
					return fmt.Errorf("plugin %q: %w: system %q cannot run after itself", p.Name, ErrInvalidPlugin, sys.Name)
				}
			}
			if prev, dup := batchSystems[sys.Name]; dup {
				return fmt.Errorf("plugin %q: %w: system %q already declared by plugin %q", p.Name, ErrConflict, sys.Name, prev)
			}
			if h.Scheduler != nil && h.Scheduler.HasSystem(sys.Name) {
				return fmt.Errorf("plugin %q: %w: system %q is already registered on scheduler", p.Name, ErrConflict, sys.Name)
			}
			batchSystems[sys.Name] = p.Name
		}

		// PixelFX (sorted key order)
		owner := pluginOwner{name: p.Name, module: p.Module}
		for _, id := range slices.Sorted(maps.Keys(p.PixelFX)) {
			fx := p.PixelFX[id]
			if err := validatePixelFXID(id); err != nil {
				return fmt.Errorf("plugin %q: %w: %v", p.Name, ErrInvalidPlugin, err)
			}
			if fx == nil {
				return fmt.Errorf("plugin %q: %w: pixelfx effect %q is nil", p.Name, ErrInvalidPlugin, id)
			}
			if prev, dup := batchPixelFX[id]; dup {
				return fmt.Errorf("plugin %q: %w: pixelfx effect %q already declared by plugin %q", p.Name, ErrConflict, id, prev)
			}
			batchPixelFX[id] = p.Name
			pixelFXPlan = append(pixelFXPlan, pixelFXItem{
				id:         id,
				effect:     fx,
				owner:      owner,
				pluginName: p.Name,
			})
		}

		// RowTypes (sorted key order)
		for _, pattern := range slices.Sorted(maps.Keys(p.RowTypes)) {
			rt := p.RowTypes[pattern]
			if rt == nil {
				return fmt.Errorf("plugin %q: %w: row type for pattern %q is nil", p.Name, ErrInvalidPlugin, pattern)
			}
			if _, dup := tempDT.Lookup(pattern); dup {
				return fmt.Errorf("plugin %q: %w: data table pattern %q already declared by plugin %q", p.Name, ErrConflict, pattern, dtOwners[patternKey(pattern)])
			}
			if h.DataTables != nil {
				if _, exists := h.DataTables.Lookup(pattern); exists {
					return fmt.Errorf("plugin %q: %w: data table pattern %q is already registered on host", p.Name, ErrConflict, pattern)
				}
			}
			if err := tempDT.Register(pattern, rt); err != nil {
				return fmt.Errorf("plugin %q: %w: data table pattern %q: %v", p.Name, ErrInvalidPlugin, pattern, err)
			}
			dtOwners[patternKey(pattern)] = p.Name
		}

		// Console commands (slice order)
		for _, cmd := range p.Console {
			if err := validateConsoleCommandName(cmd.Name); err != nil {
				return fmt.Errorf("plugin %q: %w: %v", p.Name, ErrInvalidPlugin, err)
			}
			if cmd.Fn == nil {
				return fmt.Errorf("plugin %q: %w: console command %q has a nil function", p.Name, ErrInvalidPlugin, cmd.Name)
			}
			if prev, dup := batchConsole[cmd.Name]; dup {
				return fmt.Errorf("plugin %q: %w: console command %q already declared by plugin %q", p.Name, ErrConflict, cmd.Name, prev)
			}
			if existingConsole != nil {
				if _, exists := existingConsole[cmd.Name]; exists {
					return fmt.Errorf("plugin %q: %w: console command %q is already registered on console", p.Name, ErrConflict, cmd.Name)
				}
			}
			batchConsole[cmd.Name] = p.Name
		}
	}

	// Phase 2: validate and commit process-global pixelfx registrations atomically under pixelfxMu, before mutating any Host registry.
	if len(pixelFXPlan) > 0 {
		if err := commitPixelFX(pixelFXPlan); err != nil {
			return err
		}
	}

	// Phase 3: commit Host registries in deterministic order.
	if h.Components != nil && len(tempComps.Names()) > 0 {
		if err := h.Components.Merge(tempComps); err != nil {
			return fmt.Errorf("gameplugin: merge components: %w", err)
		}
	}
	if h.Scheduler != nil {
		if err := EnsureStages(h.Scheduler); err != nil {
			return err
		}
		for _, p := range ps {
			for _, sys := range p.Systems {
				opts := make([]entities.SystemOption, 0, 2+len(sys.Before)+len(sys.After))
				if sys.Stage != "" {
					opts = append(opts, entities.After(sys.Stage.Start()), entities.Before(sys.Stage.End()))
				}
				for _, b := range sys.Before {
					opts = append(opts, entities.Before(b))
				}
				for _, a := range sys.After {
					opts = append(opts, entities.After(a))
				}
				if err := h.Scheduler.AddSystem(sys.Name, sys.Fn, opts...); err != nil {
					return fmt.Errorf("plugin %q: add system %q: %w", p.Name, sys.Name, err)
				}
			}
		}
	}
	if h.DataTables != nil {
		for _, p := range ps {
			for _, pattern := range slices.Sorted(maps.Keys(p.RowTypes)) {
				if err := h.DataTables.Register(pattern, p.RowTypes[pattern]); err != nil {
					return fmt.Errorf("plugin %q: register row type %q: %w", p.Name, pattern, err)
				}
			}
		}
	}
	if h.Console != nil {
		for _, p := range ps {
			for _, cmd := range p.Console {
				h.Console.RegisterCommand(cmd.Name, cmd.Help, cmd.Fn)
			}
		}
	}

	if h.installedSet == nil {
		h.installedSet = make(map[string]struct{}, len(ps))
	}
	for _, p := range ps {
		h.installedSet[p.Name] = struct{}{}
		h.installed = append(h.installed, Installed{
			Name:    p.Name,
			Module:  p.Module,
			Version: ModuleVersion(p.Module),
		})
		if p.Codecs != nil {
			h.codecs = append(h.codecs, codecEntry{plugin: p.Name, fn: p.Codecs})
		}
	}

	// Phase 4: run escape-hatch Install hooks last, in argument order.
	for _, p := range ps {
		if p.Install != nil {
			if err := p.Install(h); err != nil {
				return fmt.Errorf("plugin %q: install hook: %w", p.Name, err)
			}
		}
	}
	return nil
}

func commitPixelFX(plan []pixelFXItem) error {
	pixelfxMu.Lock()
	defer pixelfxMu.Unlock()

	for i := range plan {
		item := &plan[i]
		if existingOwner, owned := pixelfxOwners[item.id]; owned {
			if existingOwner != item.owner {
				return fmt.Errorf("plugin %q: %w: pixelfx effect %q is already registered by plugin %q", item.pluginName, ErrConflict, item.id, existingOwner.name)
			}
			item.skip = true
			continue
		}
		if _, exists := pixelfx.Lookup(item.id); exists {
			return fmt.Errorf("plugin %q: %w: pixelfx effect %q is already registered", item.pluginName, ErrConflict, item.id)
		}
	}

	for _, item := range plan {
		if item.skip {
			continue
		}
		pixelfx.Register(item.id, item.effect)
		pixelfxOwners[item.id] = item.owner
	}
	return nil
}

func validatePixelFXID(id string) error {
	cat, name, ok := strings.Cut(id, "/")
	if !ok || cat == "" || name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("pixelfx effect id %q must be <category>/<name>", id)
	}
	return nil
}

func validateConsoleCommandName(name string) error {
	if name == "" {
		return errors.New("console command has an empty name")
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("console command %q must not contain spaces or control characters", name)
	}
	return nil
}

func patternKey(pattern string) string {
	// Mirror datatable's normalization closely for owner diagnostics.
	p := strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	return p
}
