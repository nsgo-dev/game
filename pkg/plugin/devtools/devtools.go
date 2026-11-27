package devtools

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/mcp"
	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/plugin"
	"github.com/nsgo-dev/game/pkg/plugin/manifest"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// PrefabSpec describes one prefab that a devtools [Plugin] registers on [Host.Bridge].
// Exactly one of Prefab, Scene, or Spawn must be non-nil.
type PrefabSpec struct {
	// Name is the prefab name registered on the Bridge. When Prefab is set and Name is empty, Prefab.Name() is used.
	Name string
	// Prefab registers an *entities.Prefab via Bridge.RegisterEntityPrefab.
	Prefab *entities.Prefab
	// Scene registers a self-contained single-root *sceneformat.SceneDoc via Bridge.RegisterScenePrefab.
	Scene *sceneformat.SceneDoc
	// Spawn registers a custom mcp.PrefabSpawner via Bridge.RegisterPrefab.
	Spawn mcp.PrefabSpawner
}

// Plugin declares a plugin's development-time contributions: MCP bridge prefabs and playtest bot profiles.
type Plugin struct {
	// Name uniquely identifies the plugin using the same rule as plugin.yaml's name.
	Name string
	// Module is the plugin's Go module path, matching go.mod and plugin.yaml when set.
	Module string
	// API must equal [plugin.APIVersion].
	API int
	// Prefabs lists prefabs registered on Host.Bridge in slice order.
	Prefabs []PrefabSpec
	// Bots maps playtest bot profile names to factories registered in the process-global playtest registry in sorted key order, once per process per owning plugin.
	Bots map[string]playtest.BotFactory
	// Install is an optional escape hatch called last after all declarative contributions have been validated and registered.
	Install func(*Host) error
}

// Host holds the development-time registries that [Install] wires plugins into. Nil fields skip that kind of contribution.
type Host struct {
	// Bridge receives plugin Prefabs when non-nil.
	Bridge *mcp.Bridge

	installed    []plugin.Installed
	installedSet map[string]struct{}
}

// Plugins returns the devtools plugins installed on h in installation order.
func (h *Host) Plugins() []plugin.Installed {
	if h == nil || len(h.installed) == 0 {
		return nil
	}
	return slices.Clone(h.installed)
}

// Has reports whether a devtools plugin named name has been installed on h.
func (h *Host) Has(name string) bool {
	if h == nil || name == "" {
		return false
	}
	_, ok := h.installedSet[name]
	return ok
}

type botOwner struct {
	name   string
	module string
}

var (
	botMu     sync.Mutex
	botOwners = map[string]botOwner{}
)

type botItem struct {
	name       string
	factory    playtest.BotFactory
	owner      botOwner
	pluginName string
	skip       bool
}

type prefabPlanItem struct {
	pluginName string
	name       string
	prefab     *entities.Prefab
	scene      *sceneformat.SceneDoc
	spawn      mcp.PrefabSpawner
}

// Install validates every plugin in ps against h and the process-global playtest registry before registering anything, then installs their bots, prefabs, and Install hooks in deterministic order (plugins in argument order, Prefabs in slice order, Bots in sorted key order).
func Install(h *Host, ps ...Plugin) error {
	if h == nil {
		return plugin.ErrNilHost
	}
	if len(ps) == 0 {
		return nil
	}

	batchPlugins := make(map[string]struct{}, len(ps))
	batchPrefabs := make(map[string]string)
	batchBots := make(map[string]string)
	var prefabPlan []prefabPlanItem
	var botPlan []botItem

	// Phase 1: pre-flight validation of all plugins and contributions.
	for _, p := range ps {
		if err := manifest.ValidateName(p.Name); err != nil {
			return fmt.Errorf("%w: %v", plugin.ErrInvalidPlugin, err)
		}
		if p.Module != "" {
			if err := manifest.ValidateModulePath(p.Module); err != nil {
				return fmt.Errorf("plugin %q: %w: %v", p.Name, plugin.ErrInvalidPlugin, err)
			}
		}
		if p.API != plugin.APIVersion {
			return fmt.Errorf("plugin %q: %w: got %d, want %d", p.Name, plugin.ErrAPIMismatch, p.API, plugin.APIVersion)
		}
		if h.Has(p.Name) {
			return fmt.Errorf("plugin %q: %w: already installed on devtools host", p.Name, plugin.ErrDuplicatePlugin)
		}
		if _, dup := batchPlugins[p.Name]; dup {
			return fmt.Errorf("plugin %q: %w: listed twice in Install", p.Name, plugin.ErrDuplicatePlugin)
		}
		batchPlugins[p.Name] = struct{}{}

		// Prefabs (slice order)
		for i, spec := range p.Prefabs {
			item, err := validatePrefabSpec(p.Name, i, spec)
			if err != nil {
				return err
			}
			if prev, dup := batchPrefabs[item.name]; dup {
				return fmt.Errorf("plugin %q: %w: prefab %q already declared by plugin %q", p.Name, plugin.ErrConflict, item.name, prev)
			}
			if h.Bridge != nil && h.Bridge.HasPrefab(item.name) {
				return fmt.Errorf("plugin %q: %w: prefab %q is already registered on bridge", p.Name, plugin.ErrConflict, item.name)
			}
			batchPrefabs[item.name] = p.Name
			prefabPlan = append(prefabPlan, item)
		}

		// Bots (sorted key order)
		owner := botOwner{name: p.Name, module: p.Module}
		for _, botName := range slices.Sorted(maps.Keys(p.Bots)) {
			factory := p.Bots[botName]
			if strings.TrimSpace(botName) == "" || strings.TrimSpace(botName) != botName {
				return fmt.Errorf("plugin %q: %w: bot profile name %q must be non-empty without surrounding whitespace", p.Name, plugin.ErrInvalidPlugin, botName)
			}
			if factory == nil {
				return fmt.Errorf("plugin %q: %w: bot profile %q has a nil factory", p.Name, plugin.ErrInvalidPlugin, botName)
			}
			if prev, dup := batchBots[botName]; dup {
				return fmt.Errorf("plugin %q: %w: bot profile %q already declared by plugin %q", p.Name, plugin.ErrConflict, botName, prev)
			}
			batchBots[botName] = p.Name
			botPlan = append(botPlan, botItem{
				name:       botName,
				factory:    factory,
				owner:      owner,
				pluginName: p.Name,
			})
		}
	}

	// Phase 2: validate and commit process-global playtest bots atomically under botMu, before mutating Host.Bridge.
	if len(botPlan) > 0 {
		if err := commitBots(botPlan); err != nil {
			return err
		}
	}

	// Phase 3: commit Bridge prefabs in deterministic order.
	if h.Bridge != nil {
		for _, item := range prefabPlan {
			switch {
			case item.prefab != nil:
				if err := h.Bridge.RegisterEntityPrefab(item.prefab); err != nil {
					return fmt.Errorf("plugin %q: register prefab %q: %w", item.pluginName, item.name, err)
				}
			case item.scene != nil:
				if err := h.Bridge.RegisterScenePrefab(item.name, item.scene); err != nil {
					return fmt.Errorf("plugin %q: register scene prefab %q: %w", item.pluginName, item.name, err)
				}
			case item.spawn != nil:
				if err := h.Bridge.RegisterPrefab(item.name, item.spawn); err != nil {
					return fmt.Errorf("plugin %q: register prefab %q: %w", item.pluginName, item.name, err)
				}
			}
		}
	}

	if h.installedSet == nil {
		h.installedSet = make(map[string]struct{}, len(ps))
	}
	for _, p := range ps {
		h.installedSet[p.Name] = struct{}{}
		h.installed = append(h.installed, plugin.Installed{
			Name:    p.Name,
			Module:  p.Module,
			Version: plugin.ModuleVersion(p.Module),
		})
	}

	// Phase 4: run escape-hatch Install hooks last, in argument order.
	for _, p := range ps {
		if p.Install != nil {
			if err := p.Install(h); err != nil {
				return fmt.Errorf("plugin %q: devtools install hook: %w", p.Name, err)
			}
		}
	}
	return nil
}

func validatePrefabSpec(pluginName string, index int, spec PrefabSpec) (prefabPlanItem, error) {
	kinds := 0
	if spec.Prefab != nil {
		kinds++
	}
	if spec.Scene != nil {
		kinds++
	}
	if spec.Spawn != nil {
		kinds++
	}
	if kinds != 1 {
		return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: prefab %d must set exactly one of Prefab, Scene, or Spawn", pluginName, plugin.ErrInvalidPlugin, index)
	}

	switch {
	case spec.Prefab != nil:
		name := spec.Name
		p := spec.Prefab
		if name == "" {
			name = p.Name()
		} else if p.Name() != "" && p.Name() != name {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: prefab %d Name %q does not match Prefab.Name() %q", pluginName, plugin.ErrInvalidPlugin, index, name, p.Name())
		} else if p.Name() == "" {
			p = p.Clone(name)
		}
		if name == "" {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: prefab %d has an empty name", pluginName, plugin.ErrInvalidPlugin, index)
		}
		return prefabPlanItem{pluginName: pluginName, name: name, prefab: p}, nil

	case spec.Scene != nil:
		if spec.Name == "" {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: scene prefab %d has an empty name", pluginName, plugin.ErrInvalidPlugin, index)
		}
		if err := sceneformat.Validate(spec.Scene); err != nil {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: scene prefab %q: %v", pluginName, plugin.ErrInvalidPlugin, spec.Name, err)
		}
		if spec.Scene.RefersToScenes() {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: scene prefab %q refers to external scenes", pluginName, plugin.ErrInvalidPlugin, spec.Name)
		}
		top := 0
		for li := range spec.Scene.Layers {
			top += len(spec.Scene.Layers[li].Objects)
		}
		if top != 1 {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: scene prefab %q has %d top-level objects, want exactly 1", pluginName, plugin.ErrInvalidPlugin, spec.Name, top)
		}
		return prefabPlanItem{pluginName: pluginName, name: spec.Name, scene: spec.Scene}, nil

	default:
		if spec.Name == "" {
			return prefabPlanItem{}, fmt.Errorf("plugin %q: %w: custom prefab %d has an empty name", pluginName, plugin.ErrInvalidPlugin, index)
		}
		return prefabPlanItem{pluginName: pluginName, name: spec.Name, spawn: spec.Spawn}, nil
	}
}

func commitBots(plan []botItem) error {
	botMu.Lock()
	defer botMu.Unlock()

	existingProfiles := playtest.BotProfiles()
	for i := range plan {
		item := &plan[i]
		if existingOwner, owned := botOwners[item.name]; owned {
			if existingOwner != item.owner {
				return fmt.Errorf("plugin %q: %w: bot profile %q is already registered by plugin %q", item.pluginName, plugin.ErrConflict, item.name, existingOwner.name)
			}
			item.skip = true
			continue
		}
		if slices.Contains(existingProfiles, item.name) {
			return fmt.Errorf("plugin %q: %w: bot profile %q is already registered", item.pluginName, plugin.ErrConflict, item.name)
		}
	}

	for _, item := range plan {
		if item.skip {
			continue
		}
		playtest.RegisterBot(item.name, item.factory)
		botOwners[item.name] = item.owner
	}
	return nil
}
