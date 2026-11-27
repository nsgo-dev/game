package modding

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/tsx"
	"github.com/nsgo-dev/nsgo/tsx/bundler"
	"github.com/nsgo-dev/nsgo/tsx/pkg"
)

const (
	defaultMaxLogs = 256
	maxEventDepth  = 16
)

// ModState is the lifecycle state of a managed mod.
type ModState uint8

const (
	// StateDiscovered means the mod manifest was discovered and validated, and is not loaded.
	StateDiscovered ModState = iota
	// StateLoaded means the mod is compiled, running in its sandbox, and receiving hooks.
	StateLoaded
	// StateDisabled means the mod was explicitly disabled and will not be loaded.
	StateDisabled
	// StateFailed means the mod failed to compile, load, or faulted at runtime.
	StateFailed
)

// String returns a human-readable state name.
func (s ModState) String() string {
	switch s {
	case StateDiscovered:
		return "discovered"
	case StateLoaded:
		return "loaded"
	case StateDisabled:
		return "disabled"
	case StateFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// ModInfo is a snapshot of a managed mod's manifest, state, effective capabilities, error, and logs.
type ModInfo struct {
	Manifest         ModManifest
	State            ModState
	Enabled          bool
	Capabilities     Capability
	HostCapabilities []string
	Error            error
	Logs             []LogEntry
	Order            int
	// Dir is the mod's directory name inside the discovered mods file system.
	Dir string
}

// ManagerOptions configures a [Manager].
type ManagerOptions struct {
	// Bridge connects mod API calls to the host game.
	Bridge GameBridge
	// StorageKey, when non-empty, persists mod enabled/disabled state in cx.Storage()
	// under "<StorageKey>.enabled.<id>" and isolates per-mod CapStorage keys under
	// "<StorageKey>.mod.<id>.<key>".
	StorageKey string
	// GameVersion, when non-empty, checks each discovered mod's manifest.GameVersion constraint.
	GameVersion string
	// Platform overrides the host platform checked against a v2 manifest's platforms list
	// (such as "windows", "macos", "linux", "ios", "android", or "web"). When empty, it
	// defaults to the current runtime platform (mapping "darwin" to "macos" and "js" to "web").
	Platform string
	// Grant, when non-nil, narrows every mod's manifest capabilities to (manifest.Capabilities & *Grant).
	Grant *Capability
	// GrantHost, when non-nil, narrows every mod's manifest host capabilities to those listed here.
	// A nil slice applies no host-level narrowing; a non-nil empty slice ([]string{}) denies all host capabilities.
	GrantHost []string
	// Modules registers host TSX modules that mods can import when granted the module's capability.
	// Each module's functions are registered with [tsx.RegisterFunctions] (or attached to an earlier [RegisterModule] registration with matching signatures).
	Modules []ModModule
	// Capabilities declares the host-defined capabilities that mods may request in mod.yaml.
	Capabilities []HostCapability
	// Limits overrides tsx.DefaultLimits for each mod's sandbox.
	Limits *tsx.Limits
	// MaxLogs caps the number of retained log entries per mod (256 when <= 0).
	MaxLogs int
	// WebUnisolated lets mods run on web targets, where the browser cannot isolate a mod from the page: a mod there can use the page's own APIs (XMLHttpRequest, WebSocket, localStorage) whatever its capabilities say. Without it, loading a mod on the web fails with ErrWebUnisolated. Other targets isolate mods and ignore it. It is forwarded as tsx.Sandbox.WebUnisolated.
	WebUnisolated bool
}

// pageAuthorityTarget reports whether mods run with the page's authority, which is the case in the browser.
var pageAuthorityTarget = runtime.GOOS == "js"

func runtimePlatform() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "js":
		return "web"
	default:
		return runtime.GOOS
	}
}

type managedMod struct {
	manifest     ModManifest
	fsys         fs.FS
	dir          string // the mod's directory in the mods file system, which names its mod.yaml in errors
	order        int
	enabled      bool
	state        ModState
	err          error
	discoveryErr error       // non-nil when the mod failed during discovery and cannot be loaded
	grant        *Capability // optional per-mod grant override
	hostGrantSet bool
	hostGrant    []string // optional per-mod host capability grant override

	scope  *core.Scope
	plugin *tsx.Plugin
	api    *ModAPI
	logs   []LogEntry // preserved across unload/failure for inspection
}

// Manager discovers, validates, loads, isolates, and steps TSX/JS game mods under a [*core.Scope].
// All methods should be called on the engine's goroutine, as NSGo scopes and game.Loop are.
type Manager struct {
	cx               *core.Scope
	opts             ManagerOptions
	mods             map[string]*managedMod
	order            []string
	failed           []*managedMod
	contentSnap      atomic.Pointer[contentSnapshot]
	state            map[string]any
	memStorage       map[string]string
	spawnSeq         uint64
	eventDepth       int
	panelSeq         int
	panelVersion     *core.Signal[int]
	closed           bool
	declaredHostCaps map[string]HostCapability
	hostCapsOrder    []HostCapability
	hostModules      map[string]*boundHostModule
	hostModuleOrder  []string

	// Ephemeral package signing state for feeding compiled mod bundles to tsx.Load.
	trust    *pkg.Trust
	release  ed25519.PrivateKey
	cert     *pkg.Certificate
	buildSeq uint64
}

// NewManager creates a mod manager scoped to cx. Disposing cx automatically unloads all mods.
//
// Any host modules in opts.Modules are validated against opts.Capabilities and registered with [tsx.RegisterFunctions] (or attached to a prior [RegisterModule] registration). NewManager panics if opts.Capabilities or opts.Modules contain invalid names, duplicates, a name colliding with "game" or "nsgo", or a module without a declared capability.
func NewManager(cx *core.Scope, opts ManagerOptions) *Manager {
	declaredCaps, orderedCaps, modules, moduleOrder, err := validateHostConfig(opts)
	if err != nil {
		panic(err)
	}
	for _, name := range moduleOrder {
		if err := ensureHostModuleRegistered(modules[name]); err != nil {
			panic(err)
		}
	}
	if opts.GrantHost != nil {
		opts.GrantHost = slices.Clone(opts.GrantHost)
	}
	m := &Manager{
		cx:               cx,
		opts:             opts,
		mods:             map[string]*managedMod{},
		state:            map[string]any{},
		memStorage:       map[string]string{},
		panelVersion:     core.NewSignal(cx, 0),
		declaredHostCaps: declaredCaps,
		hostCapsOrder:    orderedCaps,
		hostModules:      modules,
		hostModuleOrder:  moduleOrder,
	}
	m.contentSnap.Store(&contentSnapshot{})
	cx.Cleanup(m.Close)
	return m
}

// HostCapabilities returns a copy of the host capabilities declared in ManagerOptions.Capabilities.
func (m *Manager) HostCapabilities() []HostCapability {
	return slices.Clone(m.hostCapsOrder)
}

func (m *Manager) hostPlatform() string {
	if p := strings.TrimSpace(m.opts.Platform); p != "" {
		return p
	}
	return runtimePlatform()
}

// Discover scans immediate subdirectories of dir for mod.yaml manifests, validates them, resolves dependency order, and registers them with the manager.
func (m *Manager) Discover(dir string) ([]ModManifest, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("modding: discover %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("modding: discover %q: not a directory", dir)
	}
	return m.DiscoverFS(os.DirFS(dir))
}

// fixReadManifest is the fix for a mod.yaml that cannot be read.
const fixReadManifest = "check that mod.yaml is a readable file in the mod's directory"

// checkGameVersion reports, as a *loaderr.Error about the manifest's gameVersion, whether host, the game's version, satisfies manifest's gameVersion constraint. A mismatch matches ErrVersionMismatch with errors.Is.
func checkGameVersion(host string, manifest ModManifest) error {
	ok, err := CheckConstraint(host, manifest.GameVersion)
	if err != nil {
		return loaderr.Errorf("gameVersion", "set ManagerOptions.GameVersion to a version such as \"1.2.0\", and write gameVersion as a constraint such as \">=1.2.0\"", "cannot check game version %q against %q: %w", host, manifest.GameVersion, err)
	}
	if !ok {
		return loaderr.Errorf("gameVersion", fmt.Sprintf("install a release of the mod made for game version %s, or remove the mod", host), "%w: mod %q requires game version %s, host is %s", ErrVersionMismatch, manifest.ID, manifest.GameVersion, host)
	}
	return nil
}

// checkPlatform reports, as a *loaderr.Error about the manifest's platforms list, whether host is supported. A mismatch matches ErrUnsupportedPlatform with errors.Is.
func checkPlatform(host string, manifest ModManifest) error {
	if len(manifest.Platforms) == 0 || slices.Contains(manifest.Platforms, host) {
		return nil
	}
	return loaderr.Errorf("platforms", fmt.Sprintf("add %q to platforms or remove the mod on this platform", host), "%w: mod %q does not support platform %q (supports %s)", ErrUnsupportedPlatform, manifest.ID, host, strings.Join(manifest.Platforms, ", "))
}

// validateModFiles verifies that manifest's entry script (if non-empty) and declared contributes directories exist inside sub, have the right file/directory type, and contain no symlinks escaping sub.
func validateModFiles(sub fs.FS, manifest ModManifest, manifestPath string, data []byte) error {
	if manifest.Entry != "" {
		entryPath := path.Clean(manifest.Entry)
		if err := checkConfinedPath(sub, entryPath); err != nil {
			return loaderr.Locate(loaderr.Errorf("entry", "keep entry inside the mod directory without escaping symlinks", "%w: mod %q entry %q: %w", ErrInvalidManifest, manifest.ID, manifest.Entry, err), manifestPath, data, "")
		}
		fi, err := fs.Stat(sub, entryPath)
		if err != nil {
			return loaderr.Locate(loaderr.Errorf("entry", "add the script at this path in the mod's directory, or correct entry", "%w: mod %q entry %q not found: %v", ErrInvalidManifest, manifest.ID, manifest.Entry, err), manifestPath, data, "")
		}
		if fi.IsDir() {
			return loaderr.Locate(loaderr.Errorf("entry", "name the mod's script file, not a directory", "%w: mod %q entry %q is a directory", ErrInvalidManifest, manifest.ID, manifest.Entry), manifestPath, data, "")
		}
	}
	for _, item := range []struct {
		key string
		dir string
	}{
		{"assets", manifest.Contributes.Assets},
		{"scenes", manifest.Contributes.Scenes},
		{"l10n", manifest.Contributes.L10n},
		{"datatables", manifest.Contributes.DataTables},
	} {
		if item.dir == "" {
			continue
		}
		if err := checkNoEscapingSymlinksInDir(sub, item.dir); err != nil {
			field := loaderr.Path("contributes", item.key)
			if errors.Is(err, ErrUnsafePath) {
				return loaderr.Locate(loaderr.Errorf(field, "keep contributed files and symlinks inside the mod directory", "%w: mod %q contributes.%s %q: %w", ErrInvalidManifest, manifest.ID, item.key, item.dir, err), manifestPath, data, "")
			}
			return loaderr.Locate(loaderr.Errorf(field, "create this directory in the mod, or correct contributes", "%w: mod %q contributes.%s %q: %v", ErrInvalidManifest, manifest.ID, item.key, item.dir, err), manifestPath, data, "")
		}
	}
	return nil
}

type scannedMod struct {
	manifest     ModManifest
	fsys         fs.FS
	dir          string
	manifestPath string
	data         []byte
}

// DiscoverFS scans immediate subdirectories of fsys for mod.yaml manifests ([ManifestFile]), validates them, checks GameVersion, platform compatibility, entry files, and contributes directories, resolves dependency order via topological sort, and registers them with the manager. Subdirectories without a manifest are skipped; one that still has only a mod.json fails with [ErrInvalidManifest].
//
// Failures are isolated per mod: a bad manifest, missing entry or contributes directory, unsupported platform, duplicate ID (failing both mods), conflict (failing both mods), unsatisfied or failed dependency, or dependency cycle records that mod as [StateFailed] with its Error on [ModInfo] and continues scanning the rest. Healthy mods are registered and returned in deterministic topological order, and any per-mod failures are returned as a joined error.
//
// If the top-level directory itself cannot be read, existing loaded mods are left untouched.
func (m *Manager) DiscoverFS(fsys fs.FS) ([]ModManifest, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("modding: discover: %w", err)
	}

	hostPlatform := m.hostPlatform()
	byIDList := map[string][]*scannedMod{}
	failedIDs := map[string]bool{}
	var failedMods []*managedMod
	var errs []error

	recordFailed := func(dir string, manifest ModManifest, sub fs.FS, failErr error) {
		failedMods = append(failedMods, &managedMod{
			manifest:     manifest,
			fsys:         sub,
			dir:          dir,
			order:        -1,
			enabled:      manifest.ID == "" || m.loadEnabledPref(manifest.ID),
			state:        StateFailed,
			err:          failErr,
			discoveryErr: failErr,
		})
		if manifest.ID != "" {
			failedIDs[manifest.ID] = true
		}
		errs = append(errs, failErr)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		manifestPath := path.Join(dirName, ManifestFile)
		data, err := fs.ReadFile(fsys, manifestPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A directory with only the old JSON manifest fails rather than being skipped, so a mod is never silently left out.
				if _, legacyErr := fs.Stat(fsys, path.Join(dirName, legacyManifestFile)); legacyErr == nil {
					recordFailed(dirName, ModManifest{}, nil, fmt.Errorf("%w: %s: mod.json is no longer read; rewrite it as %s", ErrInvalidManifest, dirName, ManifestFile))
				}
				continue
			}
			recordFailed(dirName, ModManifest{}, nil, loaderr.Locate(fmt.Errorf("read: %w", err), manifestPath, nil, fixReadManifest))
			continue
		}
		manifest, err := ParseManifest(data)
		if err != nil {
			recordFailed(dirName, ModManifest{}, nil, loaderr.WithFile(err, manifestPath))
			continue
		}
		if m.opts.GameVersion != "" && manifest.GameVersion != "" {
			if err := checkGameVersion(m.opts.GameVersion, manifest); err != nil {
				recordFailed(dirName, manifest, nil, loaderr.Locate(err, manifestPath, data, ""))
				continue
			}
		}
		if err := checkPlatform(hostPlatform, manifest); err != nil {
			recordFailed(dirName, manifest, nil, loaderr.Locate(err, manifestPath, data, ""))
			continue
		}
		sub, err := fs.Sub(fsys, dirName)
		if err != nil {
			recordFailed(dirName, manifest, nil, fmt.Errorf("modding: open mod dir %q: %w", dirName, err))
			continue
		}
		if err := validateModFiles(sub, manifest, manifestPath, data); err != nil {
			recordFailed(dirName, manifest, sub, err)
			continue
		}
		byIDList[manifest.ID] = append(byIDList[manifest.ID], &scannedMod{
			manifest:     manifest,
			fsys:         sub,
			dir:          dirName,
			manifestPath: manifestPath,
			data:         data,
		})
	}

	// Duplicate IDs fail every mod claiming that ID without aborting the scan.
	candidates := make(map[string]*scannedMod, len(byIDList))
	for _, id := range slices.Sorted(maps.Keys(byIDList)) {
		group := byIDList[id]
		if len(group) == 1 {
			candidates[id] = group[0]
			continue
		}
		first := group[0]
		// Record duplicates after the first ahead of the first in errs so errors.As(&loadErr) points at the duplicate file, matching prior behavior.
		for _, dup := range group[1:] {
			dupErr := loaderr.Locate(loaderr.Errorf("id", fmt.Sprintf("give each mod its own id; %s already uses this one", first.manifestPath), "%w: %q", ErrDuplicateMod, id), dup.manifestPath, dup.data, "")
			recordFailed(dup.dir, dup.manifest, dup.fsys, dupErr)
		}
		firstErr := loaderr.Locate(loaderr.Errorf("id", fmt.Sprintf("give each mod its own id; %s also uses this one", group[1].manifestPath), "%w: %q", ErrDuplicateMod, id), first.manifestPath, first.data, "")
		recordFailed(first.dir, first.manifest, first.fsys, firstErr)
	}

	// Conflicts among remaining candidates fail both sides symmetrically.
	conflictErrs := map[string]error{}
	for _, id := range slices.Sorted(maps.Keys(candidates)) {
		c := candidates[id]
		for _, conflictID := range slices.Sorted(maps.Keys(c.manifest.Conflicts)) {
			other, ok := candidates[conflictID]
			if !ok {
				continue
			}
			constraint := c.manifest.Conflicts[conflictID]
			matches, err := CheckConstraint(other.manifest.Version, constraint)
			if err != nil || !matches {
				continue
			}
			if _, exists := conflictErrs[id]; !exists {
				conflictErrs[id] = loaderr.Locate(
					loaderr.Errorf(loaderr.Path("conflicts", conflictID), fmt.Sprintf("remove or disable either %q or %q", id, conflictID), "%w: mod %q conflicts with %q (%s, found %s)", ErrConflictingMods, id, conflictID, constraint, other.manifest.Version),
					c.manifestPath, c.data, "",
				)
			}
			if _, exists := conflictErrs[conflictID]; !exists {
				conflictErrs[conflictID] = fmt.Errorf("%w: mod %q conflicts with %q (%s, found %s)", ErrConflictingMods, conflictID, id, constraint, other.manifest.Version)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(conflictErrs)) {
		c := candidates[id]
		delete(candidates, id)
		recordFailed(c.dir, c.manifest, c.fsys, conflictErrs[id])
	}

	// Iteratively fail mods whose required dependencies are missing, failed, or version-mismatched,
	// or whose present optional dependencies fail their version constraint.
	for {
		changed := false
		for _, id := range slices.Sorted(maps.Keys(candidates)) {
			c := candidates[id]
			var depErr error
			for _, depID := range slices.Sorted(maps.Keys(c.manifest.Dependencies)) {
				if failedIDs[depID] {
					depErr = fmt.Errorf("%w: mod %q depends on failed mod %q", ErrModFailed, id, depID)
					break
				}
				dep, ok := candidates[depID]
				if !ok {
					depErr = fmt.Errorf("%w: mod %q requires missing dependency %q", ErrMissingDependency, id, depID)
					break
				}
				constraint := c.manifest.Dependencies[depID]
				okVer, err := CheckConstraint(dep.manifest.Version, constraint)
				if err != nil {
					depErr = fmt.Errorf("%w: mod %q dependency %q: %v", ErrInvalidManifest, id, depID, err)
					break
				}
				if !okVer {
					depErr = fmt.Errorf("%w: mod %q requires %q %s, found %s", ErrVersionMismatch, id, depID, constraint, dep.manifest.Version)
					break
				}
			}
			if depErr == nil {
				for _, depID := range slices.Sorted(maps.Keys(c.manifest.OptionalDependencies)) {
					dep, ok := candidates[depID]
					if !ok {
						// Absent or failed optional dependencies are ignored.
						continue
					}
					constraint := c.manifest.OptionalDependencies[depID]
					okVer, err := CheckConstraint(dep.manifest.Version, constraint)
					if err != nil {
						depErr = fmt.Errorf("%w: mod %q optional dependency %q: %v", ErrInvalidManifest, id, depID, err)
						break
					}
					if !okVer {
						depErr = fmt.Errorf("%w: mod %q optionally requires %q %s, found %s", ErrVersionMismatch, id, depID, constraint, dep.manifest.Version)
						break
					}
				}
			}
			if depErr != nil {
				delete(candidates, id)
				recordFailed(c.dir, c.manifest, c.fsys, depErr)
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	// Topologically sort remaining candidates (required + present optional dependencies) and isolate any cycle.
	candIDs := slices.Sorted(maps.Keys(candidates))
	inDegree := make(map[string]int, len(candIDs))
	dependents := make(map[string][]string, len(candIDs))
	for _, id := range candIDs {
		c := candidates[id]
		for _, depID := range slices.Sorted(maps.Keys(c.manifest.Dependencies)) {
			dependents[depID] = append(dependents[depID], id)
			inDegree[id]++
		}
		for _, depID := range slices.Sorted(maps.Keys(c.manifest.OptionalDependencies)) {
			if _, ok := candidates[depID]; ok {
				dependents[depID] = append(dependents[depID], id)
				inDegree[id]++
			}
		}
	}

	var ready []string
	for _, id := range candIDs {
		if inDegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	ordered := make([]ModManifest, 0, len(candIDs))
	for len(ready) > 0 {
		curr := ready[0]
		ready = ready[1:]
		ordered = append(ordered, candidates[curr].manifest)
		for _, next := range dependents[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				idx, _ := slices.BinarySearch(ready, next)
				ready = slices.Insert(ready, idx, next)
			}
		}
	}
	if len(ordered) != len(candIDs) {
		var cyclic []string
		for _, id := range candIDs {
			if inDegree[id] > 0 {
				cyclic = append(cyclic, id)
			}
		}
		cycleErr := fmt.Errorf("%w: cycle involving [%s]", ErrCyclicDependency, strings.Join(cyclic, ", "))
		for _, id := range cyclic {
			c := candidates[id]
			recordFailed(c.dir, c.manifest, c.fsys, cycleErr)
		}
	}

	// Replace the registry with healthy mods in load order and isolated failed mods.
	m.unloadAll()
	m.mods = make(map[string]*managedMod, len(ordered)+len(failedMods))
	m.order = make([]string, 0, len(ordered))

	for i, manifest := range ordered {
		c := candidates[manifest.ID]
		enabled := m.loadEnabledPref(manifest.ID)
		state := StateDiscovered
		if !enabled {
			state = StateDisabled
		}
		m.mods[manifest.ID] = &managedMod{
			manifest: manifest,
			fsys:     c.fsys,
			dir:      c.dir,
			order:    i,
			enabled:  enabled,
			state:    state,
		}
		m.order = append(m.order, manifest.ID)
	}

	slices.SortFunc(failedMods, func(a, b *managedMod) int {
		if c := strings.Compare(a.dir, b.dir); c != 0 {
			return c
		}
		return strings.Compare(a.manifest.ID, b.manifest.ID)
	})
	m.failed = failedMods
	for _, fm := range failedMods {
		if fm.manifest.ID != "" {
			if _, exists := m.mods[fm.manifest.ID]; !exists {
				m.mods[fm.manifest.ID] = fm
			}
		}
	}
	m.refreshContentOverlay()

	return slices.Clone(ordered), errors.Join(errs...)
}

// Install validates a single mod directory on disk and installs or updates it in the manager
// without disturbing unrelated mods. If a mod with the same ID is already registered, its
// running instance (if any) is unloaded and replaced. Call [Manager.Load] or [Manager.LoadAll]
// to start the installed mod.
func (m *Manager) Install(dir string) (ModManifest, error) {
	if m.closed {
		return ModManifest{}, errors.New("modding: manager is closed")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return ModManifest{}, fmt.Errorf("modding: install %q: %w", dir, err)
	}
	if !info.IsDir() {
		return ModManifest{}, fmt.Errorf("modding: install %q: not a directory", dir)
	}
	return m.InstallFS(os.DirFS(dir), filepath.Base(filepath.Clean(dir)))
}

// InstallFS validates a single mod rooted at fsys and installs or updates it in the manager
// without disturbing unrelated mods. dirName labels the mod directory in manifest errors.
// Dependency order across all registered mods is re-validated; if validation fails, existing
// mods remain untouched.
func (m *Manager) InstallFS(fsys fs.FS, dirName string) (ModManifest, error) {
	if m.closed {
		return ModManifest{}, errors.New("modding: manager is closed")
	}
	manifest, err := m.validateModFS(fsys, dirName)
	if err != nil {
		return ModManifest{}, err
	}
	if err := m.installValidated([]validatedMod{{
		manifest: manifest,
		fsys:     fsys,
		dir:      dirName,
	}}); err != nil {
		return ModManifest{}, err
	}
	return manifest, nil
}

// Uninstall unloads mod id (if loaded) and removes it from the manager.
func (m *Manager) Uninstall(id string) error {
	if m.closed {
		return errors.New("modding: manager is closed")
	}
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	m.unloadMod(mod)
	delete(m.mods, id)
	m.order = slices.DeleteFunc(m.order, func(existing string) bool { return existing == id })
	m.failed = slices.DeleteFunc(m.failed, func(existing *managedMod) bool { return existing.manifest.ID == id })
	for i, existing := range m.order {
		if other := m.mods[existing]; other != nil {
			other.order = i
		}
	}
	m.refreshContentOverlay()
	return nil
}

type validatedMod struct {
	manifest ModManifest
	fsys     fs.FS
	dir      string
}

func (m *Manager) validateModFS(fsys fs.FS, dirName string) (ModManifest, error) {
	manifestPath := ManifestFile
	if dirName != "" && dirName != "." {
		manifestPath = path.Join(dirName, ManifestFile)
	}
	data, err := fs.ReadFile(fsys, ManifestFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if _, legacyErr := fs.Stat(fsys, legacyManifestFile); legacyErr == nil {
				return ModManifest{}, fmt.Errorf("%w: %s: mod.json is no longer read; rewrite it as %s", ErrInvalidManifest, dirName, ManifestFile)
			}
			return ModManifest{}, loaderr.Locate(fmt.Errorf("%w: %w", ErrInvalidManifest, err), manifestPath, nil, fixReadManifest)
		}
		return ModManifest{}, loaderr.Locate(fmt.Errorf("read: %w", err), manifestPath, nil, fixReadManifest)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return ModManifest{}, loaderr.WithFile(err, manifestPath)
	}
	if m.opts.GameVersion != "" && manifest.GameVersion != "" {
		if err := checkGameVersion(m.opts.GameVersion, manifest); err != nil {
			return ModManifest{}, loaderr.Locate(err, manifestPath, data, "")
		}
	}
	if err := checkPlatform(m.hostPlatform(), manifest); err != nil {
		return ModManifest{}, loaderr.Locate(err, manifestPath, data, "")
	}
	if err := validateModFiles(fsys, manifest, manifestPath, data); err != nil {
		return ModManifest{}, err
	}
	return manifest, nil
}

// installValidated installs or updates a validated set of mods atomically after verifying
// topological dependency order against all already-registered mods.
func (m *Manager) installValidated(incoming []validatedMod) error {
	if len(incoming) == 0 {
		return nil
	}
	incomingByID := make(map[string]validatedMod, len(incoming))
	for _, item := range incoming {
		if _, dup := incomingByID[item.manifest.ID]; dup {
			return fmt.Errorf("%w: %q", ErrDuplicateMod, item.manifest.ID)
		}
		incomingByID[item.manifest.ID] = item
	}

	manifests := make([]ModManifest, 0, len(m.mods)+len(incoming))
	for id, existing := range m.mods {
		if existing.discoveryErr != nil {
			continue
		}
		if _, replaced := incomingByID[id]; replaced {
			continue
		}
		manifests = append(manifests, existing.manifest)
	}
	for _, item := range incoming {
		manifests = append(manifests, item.manifest)
	}

	ordered, err := ResolveOrder(manifests)
	if err != nil {
		return err
	}

	for _, item := range incoming {
		m.failed = slices.DeleteFunc(m.failed, func(existing *managedMod) bool { return existing.manifest.ID == item.manifest.ID })
		if existing, ok := m.mods[item.manifest.ID]; ok {
			if existing.state == StateLoaded {
				m.unloadMod(existing)
			}
			existing.manifest = item.manifest
			existing.fsys = item.fsys
			existing.dir = item.dir
			existing.discoveryErr = nil
			if existing.enabled {
				existing.state = StateDiscovered
			} else {
				existing.state = StateDisabled
			}
			existing.err = nil
			continue
		}
		enabled := m.loadEnabledPref(item.manifest.ID)
		state := StateDiscovered
		if !enabled {
			state = StateDisabled
		}
		m.mods[item.manifest.ID] = &managedMod{
			manifest: item.manifest,
			fsys:     item.fsys,
			dir:      item.dir,
			enabled:  enabled,
			state:    state,
		}
	}

	m.order = m.order[:0]
	for i, manifest := range ordered {
		if mod := m.mods[manifest.ID]; mod != nil {
			mod.order = i
		}
		m.order = append(m.order, manifest.ID)
	}
	m.refreshContentOverlay()
	return nil
}

// SetGrant sets a per-mod capability grant for id, further narrowing its manifest capabilities (and ManagerOptions.Grant if set).
//
// On a loaded mod it takes effect at once (#546): the game module checks the new capabilities from the next call, revoking [CapUI] removes the panels the mod mounted, and when the change reaches what the sandbox itself enforces ([CapAudio], [CapStorage]), the mod is unloaded and loaded again with a sandbox built for them, running its entry script again. The error is that load's.
func (m *Manager) SetGrant(id string, caps Capability) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	c := caps
	mod.grant = &c
	if mod.state != StateLoaded || mod.api == nil {
		return nil
	}
	prev := mod.api.caps
	next := m.effectiveCapabilities(mod)
	mod.api.caps = next
	// A mod that loses CapUI could no longer unmount its panels, so they go with the capability.
	if next&CapUI == 0 && len(mod.api.panels) > 0 {
		mod.api.panels = nil
		m.notifyPanelsChanged()
	}
	if prev&sandboxCapabilities == next&sandboxCapabilities {
		return nil
	}
	m.unloadMod(mod)
	mod.state = StateDiscovered
	mod.err = nil
	return m.Load(id)
}

// sandboxCapabilities are the capabilities the tsx sandbox enforces itself, which loadSingle bakes into a mod's sandbox: changing them needs a new sandbox.
const sandboxCapabilities = CapAudio | CapStorage

// SetHostGrant sets a per-mod host capability grant for id, further narrowing its manifest host capabilities (and ManagerOptions.GrantHost if non-nil).
// Passing nil or an empty slice revokes all host capabilities for id.
//
// On a loaded mod it takes effect at once: host module trampolines and [ModAPI.RequireHost] enforce the narrowed host capabilities from the next call. On subsequent loads or reloads, the mod's sandbox module import allowlist also reflects the grant.
func (m *Manager) SetHostGrant(id string, caps []string) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	mod.hostGrantSet = true
	mod.hostGrant = slices.Clone(caps)
	if mod.state != StateLoaded || mod.api == nil {
		return nil
	}
	mod.api.setHostCapabilities(m.effectiveHostCapabilities(mod))
	return nil
}

// Enable marks mod id as enabled and persists the preference when StorageKey is configured.
func (m *Manager) Enable(id string) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	mod.enabled = true
	if mod.state == StateDisabled {
		mod.state = StateDiscovered
		mod.err = nil
	}
	return m.saveEnabledPref(id, true)
}

// Disable marks mod id as disabled, unloads it if currently loaded, and persists the preference
// when StorageKey is configured.
func (m *Manager) Disable(id string) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	mod.enabled = false
	if mod.state == StateLoaded {
		m.unloadMod(mod)
	}
	if mod.discoveryErr == nil {
		mod.state = StateDisabled
		mod.err = nil
	}
	m.refreshContentOverlay()
	return m.saveEnabledPref(id, false)
}

// LoadAll loads all enabled discovered mods in deterministic topological order.
// A failure in one mod is isolated (marking that mod StateFailed) and does not prevent
// sibling mods from loading. Returns a joined error if any mod failed.
func (m *Manager) LoadAll() error {
	var errs []error
	for _, id := range m.order {
		mod := m.mods[id]
		if !mod.enabled {
			mod.state = StateDisabled
			continue
		}
		if mod.state == StateLoaded {
			continue
		}
		if err := m.Load(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Load compiles and starts mod id in its sandbox (or activates its contributed content for
// content-only v2 mods without an entry script). Any required dependencies and present enabled
// optional dependencies not yet loaded are loaded first. Compilation, entry execution, or panic
// failures are caught cleanly, marking the mod StateFailed without crashing the host or other mods.
func (m *Manager) Load(id string) error {
	if m.closed {
		return errors.New("modding: manager is closed")
	}
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if mod.discoveryErr != nil {
		mod.state = StateFailed
		mod.err = mod.discoveryErr
		return mod.discoveryErr
	}
	if !mod.enabled {
		return fmt.Errorf("%w: %q", ErrModDisabled, id)
	}
	if mod.state == StateLoaded {
		return nil
	}

	// Ensure required dependencies are loaded first in deterministic order.
	for _, depID := range slices.Sorted(maps.Keys(mod.manifest.Dependencies)) {
		dep, exists := m.mods[depID]
		if !exists {
			err := fmt.Errorf("%w: mod %q requires missing dependency %q", ErrMissingDependency, id, depID)
			mod.state = StateFailed
			mod.err = err
			return err
		}
		if !dep.enabled {
			err := fmt.Errorf("%w: mod %q requires disabled dependency %q", ErrModDisabled, id, depID)
			mod.state = StateFailed
			mod.err = err
			return err
		}
		if dep.state == StateFailed {
			err := fmt.Errorf("%w: mod %q depends on failed mod %q", ErrModFailed, id, depID)
			mod.state = StateFailed
			mod.err = err
			return err
		}
		if dep.state != StateLoaded {
			if err := m.Load(depID); err != nil {
				depErr := fmt.Errorf("%w: mod %q dependency %q failed: %v", ErrModFailed, id, depID, err)
				mod.state = StateFailed
				mod.err = depErr
				return depErr
			}
		}
	}

	// Load present, enabled optional dependencies first; absent, disabled, or failing ones do not block id.
	for _, optID := range slices.Sorted(maps.Keys(mod.manifest.OptionalDependencies)) {
		opt, exists := m.mods[optID]
		if !exists || !opt.enabled || opt.discoveryErr != nil || opt.state == StateFailed || opt.state == StateLoaded {
			continue
		}
		_ = m.Load(optID)
	}

	return m.loadSingle(mod)
}

func (m *Manager) loadSingle(mod *managedMod) (loadErr error) {
	// Reset previous logs for a fresh load.
	mod.logs = nil
	mod.err = nil

	// Content-only v2 mods declare contributes without an entry script; activate them directly without a TSX sandbox.
	if mod.manifest.Entry == "" {
		mod.state = StateLoaded
		mod.err = nil
		m.refreshContentOverlay()
		return nil
	}

	// On the web a mod's realm shares the page's authority (XMLHttpRequest, WebSocket, localStorage) past the game module's capability checks, so refuse unless the host opted in (#546).
	if pageAuthorityTarget && !m.opts.WebUnisolated {
		err := fmt.Errorf("modding: load mod %q: %w", mod.manifest.ID, ErrWebUnisolated)
		mod.state = StateFailed
		mod.err = err
		return err
	}

	// Fail only this mod if its manifest requests a host capability not declared on this manager.
	for _, hc := range mod.manifest.HostCapabilities {
		if _, declared := m.declaredHostCaps[hc]; !declared {
			err := fmt.Errorf("modding: load mod %q: %w: %q", mod.manifest.ID, ErrUnknownCapability, hc)
			mod.state = StateFailed
			mod.err = err
			return err
		}
	}

	// Compile the mod bundle from its directory FS (handles TSX, TS, JSX, JS, and relative imports).
	code, err := bundler.Compile(mod.fsys, path.Clean(mod.manifest.Entry))
	if err != nil {
		err = fmt.Errorf("modding: compile mod %q: %w", mod.manifest.ID, err)
		mod.state = StateFailed
		mod.err = err
		return err
	}

	effCaps := m.effectiveCapabilities(mod)
	effHostCaps := m.effectiveHostCapabilities(mod)
	verifiedPkg, err := m.buildPackage(mod.manifest, code, effCaps)
	if err != nil {
		err = fmt.Errorf("modding: package mod %q: %w", mod.manifest.ID, err)
		mod.state = StateFailed
		mod.err = err
		return err
	}

	var modScope *core.Scope
	m.cx.Run(func() {
		modScope = m.cx.Child()
	})
	api := &ModAPI{
		manifest:   mod.manifest,
		caps:       effCaps,
		hostCaps:   effHostCaps,
		manager:    m,
		scope:      modScope,
		eventHooks: map[string][]*eventHook{},
	}

	limits := tsx.DefaultLimits
	if m.opts.Limits != nil {
		limits = *m.opts.Limits
	}
	sandbox := tsx.Sandbox{
		Limits:         &limits,
		Modules:        m.sandboxModules(effHostCaps),
		Storage:        m.sandboxStoragePartition(mod.manifest.ID),
		DisableStorage: !effCaps.Has(CapStorage),
		WebUnisolated:  m.opts.WebUnisolated,
	}

	var plugin *tsx.Plugin
	modScope.Run(func() {
		sessionKey.Provide(modScope, api)
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(error); ok {
					loadErr = e
				} else {
					loadErr = fmt.Errorf("modding: mod %q panicked during load: %v", mod.manifest.ID, r)
				}
			}
		}()
		plugin, loadErr = tsx.Load(modScope, verifiedPkg, sandbox)
	})

	if loadErr != nil {
		mod.logs = slices.Clone(api.logs)
		hadPanels := api.cleanup()
		modScope.Dispose()
		mod.scope = nil
		mod.plugin = nil
		mod.api = nil
		mod.state = StateFailed
		mod.err = fmt.Errorf("modding: load mod %q: %w", mod.manifest.ID, loadErr)
		if hadPanels {
			m.notifyPanelsChanged()
		}
		m.refreshContentOverlay()
		return mod.err
	}

	mod.scope = modScope
	mod.plugin = plugin
	mod.api = api
	mod.state = StateLoaded
	mod.err = nil
	m.refreshContentOverlay()
	return nil
}

// Unload stops mod id, removes all of its hooks and UI panels, and disposes its sandbox scope.
func (m *Manager) Unload(id string) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	m.unloadMod(mod)
	if mod.discoveryErr != nil {
		mod.state = StateFailed
		mod.err = mod.discoveryErr
	} else if mod.enabled {
		mod.state = StateDiscovered
		mod.err = nil
	} else {
		mod.state = StateDisabled
		mod.err = nil
	}
	m.refreshContentOverlay()
	return nil
}

// Reload re-reads mod id's manifest if present, validates it, then unloads the mod and loads it fresh from its FS.
//
// A manifest that cannot be read or fails validation (bad syntax, changed ID, incompatible game version, unsupported platform, missing entry/contributes, or unresolvable dependencies/conflicts) is reported without unloading, so the previously loaded version keeps running (#546). A mod whose mod.yaml has been replaced by a mod.json is refused the same way with [ErrInvalidManifest], as [Manager.DiscoverFS] refuses it.
func (m *Manager) Reload(id string) error {
	mod, ok := m.mods[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if mod.discoveryErr != nil {
		return mod.discoveryErr
	}
	if !mod.enabled {
		return fmt.Errorf("%w: %q", ErrModDisabled, id)
	}
	manifestPath := path.Join(mod.dir, ManifestFile)
	data, readErr := fs.ReadFile(mod.fsys, ManifestFile)
	if errors.Is(readErr, fs.ErrNotExist) {
		if _, legacyErr := fs.Stat(mod.fsys, legacyManifestFile); legacyErr == nil {
			return fmt.Errorf("%w: mod %q: mod.json is no longer read; rewrite it as %s", ErrInvalidManifest, id, ManifestFile)
		}
	}
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return loaderr.Locate(fmt.Errorf("read: %w", readErr), manifestPath, nil, fixReadManifest)
	}
	var (
		newManifest ModManifest
		ordered     []ModManifest
	)
	if readErr == nil {
		manifest, err := ParseManifest(data)
		if err != nil {
			return loaderr.WithFile(err, manifestPath)
		}
		if manifest.ID != id {
			return loaderr.Locate(loaderr.Errorf("id", fmt.Sprintf("put the id back to %q; a mod keeps its id across reloads, so call DiscoverFS again to load it under a new one", id), "%w: mod %q manifest id changed to %q", ErrInvalidManifest, id, manifest.ID), manifestPath, data, "")
		}
		// Re-apply the checks DiscoverFS made, so a mod updated on disk cannot
		// load against an incompatible game version, platform, files, or dependencies.
		if m.opts.GameVersion != "" && manifest.GameVersion != "" {
			if err := checkGameVersion(m.opts.GameVersion, manifest); err != nil {
				return loaderr.Locate(err, manifestPath, data, "")
			}
		}
		if err := checkPlatform(m.hostPlatform(), manifest); err != nil {
			return loaderr.Locate(err, manifestPath, data, "")
		}
		if err := validateModFiles(mod.fsys, manifest, manifestPath, data); err != nil {
			return err
		}
		manifests := make([]ModManifest, 0, len(m.mods))
		for otherID, other := range m.mods {
			if otherID == id || other.manifest.ID == "" || other.discoveryErr != nil {
				continue
			}
			manifests = append(manifests, other.manifest)
		}
		manifests = append(manifests, manifest)
		resolved, err := ResolveOrder(manifests)
		if err != nil {
			return err
		}
		newManifest, ordered = manifest, resolved
	}

	m.unloadMod(mod)
	if readErr == nil {
		mod.manifest = newManifest
		// A reload may change dependencies; adopt the resolved order so Step,
		// Emit, and ContentFS keep running in topological order.
		m.order = m.order[:0]
		for i := range ordered {
			entryID := ordered[i].ID
			if other := m.mods[entryID]; other != nil {
				other.order = i
			}
			m.order = append(m.order, entryID)
		}
	}
	mod.state = StateDiscovered
	mod.err = nil
	m.refreshContentOverlay()
	return m.Load(id)
}

// Step invokes all loaded mods' onStep (and registerHook("step", ...)) callbacks in
// deterministic load order. A runtime error or panic inside one mod isolates and marks
// that mod StateFailed without interrupting other mods.
func (m *Manager) Step(dt time.Duration) {
	if m.closed {
		return
	}
	dtSec := dt.Seconds()
	for _, id := range m.order {
		mod := m.mods[id]
		if mod == nil || mod.state != StateLoaded || mod.api == nil {
			continue
		}
		hooks := slices.Clone(mod.api.stepHooks)
		for _, h := range hooks {
			if !h.active || mod.state != StateLoaded {
				continue
			}
			m.runMod(mod, func() { h.fn(dtSec) })
		}
	}
}

// Emit dispatches a host event to all loaded mods subscribed via onEvent or registerHook,
// in deterministic load order, with fault isolation.
func (m *Manager) Emit(name string, payload any) {
	if m.closed || strings.TrimSpace(name) == "" {
		return
	}
	_ = m.dispatchEvent(Event{Name: name, Payload: payload})
}

func (m *Manager) dispatchEvent(ev Event) error {
	if m.closed {
		return nil
	}
	if m.eventDepth >= maxEventDepth {
		return fmt.Errorf("modding: event %q exceeded maximum dispatch depth (%d)", ev.Name, maxEventDepth)
	}
	m.eventDepth++
	defer func() { m.eventDepth-- }()

	if ev.ModID != "" && m.opts.Bridge.OnEmit != nil {
		m.opts.Bridge.OnEmit(ev)
	}

	for _, id := range m.order {
		mod := m.mods[id]
		if mod == nil || mod.state != StateLoaded || mod.api == nil {
			continue
		}
		hooks := slices.Clone(mod.api.eventHooks[ev.Name])
		for _, h := range hooks {
			if !h.active || mod.state != StateLoaded {
				continue
			}
			m.runMod(mod, func() { h.fn(ev.Payload) })
		}
	}
	return nil
}

// Call invokes an exported JavaScript function on loaded mod id.
func (m *Manager) Call(id, fnName string, args []any, done func(result any, err error)) {
	mod, ok := m.mods[id]
	if !ok {
		if done != nil {
			done(nil, fmt.Errorf("%w: %q", ErrNotFound, id))
		}
		return
	}
	if mod.state != StateLoaded || mod.plugin == nil {
		if done != nil {
			done(nil, fmt.Errorf("modding: mod %q is not loaded (state: %s)", id, mod.state))
		}
		return
	}
	mod.scope.Run(func() {
		mod.plugin.Call(fnName, args, func(res any, err error) {
			if done != nil {
				done(res, err)
			}
		})
	})
}

// List returns snapshots of all discovered mods in deterministic load order, followed by any
// mods that failed during discovery in deterministic directory order.
func (m *Manager) List() []ModInfo {
	out := make([]ModInfo, 0, len(m.order)+len(m.failed))
	for _, id := range m.order {
		if mod := m.mods[id]; mod != nil {
			out = append(out, m.modInfoSnapshot(mod))
		}
	}
	for _, fm := range m.failed {
		out = append(out, m.modInfoSnapshot(fm))
	}
	return out
}

// Info returns the current [ModInfo] snapshot for mod id.
func (m *Manager) Info(id string) (ModInfo, bool) {
	mod, ok := m.mods[id]
	if !ok {
		return ModInfo{}, false
	}
	return m.modInfoSnapshot(mod), true
}

func (m *Manager) modInfoSnapshot(mod *managedMod) ModInfo {
	var logs []LogEntry
	if mod.api != nil {
		logs = slices.Clone(mod.api.logs)
	} else {
		logs = slices.Clone(mod.logs)
	}
	return ModInfo{
		Manifest:         mod.manifest,
		State:            mod.state,
		Enabled:          mod.enabled,
		Capabilities:     m.effectiveCapabilities(mod),
		HostCapabilities: m.effectiveHostCapabilities(mod),
		Error:            mod.err,
		Logs:             logs,
		Order:            mod.order,
		Dir:              mod.dir,
	}
}

// API returns the active [*ModAPI] for a loaded mod.
func (m *Manager) API(id string) (*ModAPI, bool) {
	mod, ok := m.mods[id]
	if !ok || mod.state != StateLoaded || mod.api == nil {
		return nil, false
	}
	return mod.api, true
}

// Logs returns a copy of the captured log buffer for mod id.
func (m *Manager) Logs(id string) []LogEntry {
	info, ok := m.Info(id)
	if !ok {
		return nil
	}
	return info.Logs
}

// GetState reads a value from the manager's built-in scoped game state map.
func (m *Manager) GetState(key string) (any, bool) {
	v, ok := m.state[key]
	return v, ok
}

// SetState writes a value to the manager's built-in scoped game state map.
func (m *Manager) SetState(key string, val any) {
	m.state[key] = val
}

// Panels returns all currently mounted UI panels across loaded mods in deterministic order.
// When slot is non-empty, only panels mounted for that slot are returned.
func (m *Manager) Panels(slot string) []Panel {
	var out []Panel
	for _, id := range m.order {
		mod := m.mods[id]
		if mod == nil || mod.state != StateLoaded || mod.api == nil {
			continue
		}
		for _, p := range mod.api.panels {
			if slot != "" && p.slot != slot {
				continue
			}
			node := m.resolvePanelNode(mod, p)
			if node == nil {
				continue
			}
			out = append(out, Panel{
				ModID: mod.manifest.ID,
				ID:    p.id,
				Slot:  p.slot,
				Node:  node,
			})
		}
	}
	return out
}

// PanelsView returns a reactive [core.Node] rendering all TSX UI panels mounted for slot
// (or all slots when slot is empty). Each panel is wrapped in a [core.Catch] boundary so a
// render error in one mod's UI never crashes the host game or other mods.
func (m *Manager) PanelsView(slot string) core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		_ = m.panelVersion.Get()
		var children []core.Node
		for _, id := range m.order {
			mod := m.mods[id]
			if mod == nil || mod.state != StateLoaded || mod.api == nil {
				continue
			}
			for _, p := range mod.api.panels {
				if slot != "" && p.slot != slot {
					continue
				}
				panelMod := mod
				panelRef := p
				children = append(children, core.Catch{
					Try: func(cx *core.Scope) core.Node {
						node := m.resolvePanelNode(panelMod, panelRef)
						if node == nil {
							return core.View{}
						}
						return node
					},
					Fallback: func(cx *core.Scope, err error, _ func()) core.Node {
						m.failMod(panelMod, err)
						return core.View{}
					},
				})
			}
		}
		return core.View{Children: children}
	})
}

func (m *Manager) resolvePanelNode(mod *managedMod, p *mountedPanel) core.Node {
	if p.node != nil {
		return p.node
	}
	var node core.Node
	ok := m.runMod(mod, func() {
		node = p.render()
	})
	if !ok {
		return nil
	}
	p.node = node
	return node
}

// Close unloads all mods and releases resources held by the manager. Afterwards no mod reports [StateLoaded]: [Manager.Info] shows each loaded mod as discovered, or disabled when it is.
func (m *Manager) Close() {
	if m.closed {
		return
	}
	m.closed = true
	hadPanels := m.unloadAll()
	for _, mod := range m.mods {
		if mod.state == StateLoaded {
			if mod.enabled {
				mod.state = StateDiscovered
			} else {
				mod.state = StateDisabled
			}
		}
	}
	m.refreshContentOverlay()
	if hadPanels {
		// Disposing the mods' scopes removed any panels they contributed; tell
		// the host so it stops rendering nodes from disposed scopes.
		m.bumpPanels()
	}
}

func (m *Manager) unloadAll() (hadPanels bool) {
	for i := len(m.order) - 1; i >= 0; i-- {
		if mod := m.mods[m.order[i]]; mod != nil {
			if m.unloadMod(mod) {
				hadPanels = true
			}
		}
	}
	return hadPanels
}

func (m *Manager) unloadMod(mod *managedMod) (hadPanels bool) {
	if mod.api != nil {
		mod.logs = slices.Clone(mod.api.logs)
		hadPanels = mod.api.cleanup()
		mod.api = nil
	}
	if mod.plugin != nil {
		mod.plugin.Close()
		mod.plugin = nil
	}
	if mod.scope != nil {
		mod.scope.Dispose()
		mod.scope = nil
	}
	if hadPanels {
		m.notifyPanelsChanged()
	}
	return hadPanels
}

func (m *Manager) runMod(mod *managedMod, fn func()) (ok bool) {
	if mod == nil || mod.state != StateLoaded || mod.api == nil {
		return false
	}
	runScope := mod.api.vmScope
	if runScope == nil {
		runScope = mod.scope
	}
	if runScope == nil || runScope.Disposed() {
		return false
	}
	var fault error
	runScope.Run(func() {
		defer func() {
			if r := recover(); r != nil {
				if e, isErr := r.(error); isErr {
					fault = e
				} else {
					fault = fmt.Errorf("modding: mod %q panicked: %v", mod.manifest.ID, r)
				}
			}
		}()
		fn()
	})
	if fault != nil {
		m.failMod(mod, fault)
		return false
	}
	return true
}

func (m *Manager) failMod(mod *managedMod, err error) {
	if mod == nil {
		return
	}
	m.unloadMod(mod)
	mod.state = StateFailed
	mod.err = fmt.Errorf("modding: mod %q failed: %w", mod.manifest.ID, err)
	m.refreshContentOverlay()
}

func (m *Manager) effectiveCapabilities(mod *managedMod) Capability {
	caps := mod.manifest.Capabilities
	if m.opts.Grant != nil {
		caps &= *m.opts.Grant
	}
	if mod.grant != nil {
		caps &= *mod.grant
	}
	return caps
}

func (m *Manager) effectiveHostCapabilities(mod *managedMod) []string {
	if len(mod.manifest.HostCapabilities) == 0 {
		return nil
	}
	var out []string
	for _, hc := range mod.manifest.HostCapabilities {
		if _, declared := m.declaredHostCaps[hc]; !declared {
			continue
		}
		if m.opts.GrantHost != nil && !slices.Contains(m.opts.GrantHost, hc) {
			continue
		}
		if mod.hostGrantSet && !slices.Contains(mod.hostGrant, hc) {
			continue
		}
		out = append(out, hc)
	}
	slices.Sort(out)
	return out
}

func (m *Manager) sandboxModules(effHostCaps []string) []string {
	modules := make([]string, 1, 1+len(m.hostModuleOrder))
	modules[0] = GameModule
	for _, name := range m.hostModuleOrder {
		if hm := m.hostModules[name]; hm != nil && hm.allowedBy(effHostCaps) {
			modules = append(modules, name)
		}
	}
	return modules
}

func (m *Manager) maxLogs() int {
	if m.opts.MaxLogs > 0 {
		return m.opts.MaxLogs
	}
	return defaultMaxLogs
}

func (m *Manager) notifyPanelsChanged() {
	if m.closed {
		return
	}
	m.bumpPanels()
}

// bumpPanels advances the panel version and tells the host even while the
// manager is closing, because the last change is the panels going away.
func (m *Manager) bumpPanels() {
	m.panelSeq++
	m.panelVersion.Set(m.panelSeq)
	if m.opts.Bridge.OnPanelsChange != nil {
		m.opts.Bridge.OnPanelsChange()
	}
}

func (m *Manager) ensureSigner() error {
	if m.trust != nil {
		return nil
	}
	root, err := pkg.GenerateKey()
	if err != nil {
		return err
	}
	release, err := pkg.GenerateKey()
	if err != nil {
		return err
	}
	now := time.Now()
	cert, err := pkg.Certify(
		root,
		release.Public().(ed25519.PublicKey),
		[]string{"*"},
		now.Add(-time.Hour),
		now.Add(365*24*time.Hour),
	)
	if err != nil {
		return err
	}
	trust := pkg.NewTrust()
	trust.AddRoot(root.Public().(ed25519.PublicKey))
	m.trust = trust
	m.release = release
	m.cert = cert
	return nil
}

func (m *Manager) buildPackage(manifest ModManifest, source string, caps Capability) (*pkg.Package, error) {
	if err := m.ensureSigner(); err != nil {
		return nil, err
	}
	m.buildSeq++
	data, err := pkg.Build(pkg.Contents{
		App: pkg.App{
			ID:      manifest.ID,
			Name:    manifest.Name,
			Version: manifest.Version,
			Build:   m.buildSeq,
		},
		Source:       source,
		Capabilities: caps.pkgCapabilities(),
	}, m.release, m.cert)
	if err != nil {
		return nil, err
	}
	return m.trust.Verify(data)
}

func (m *Manager) sandboxStoragePartition(modID string) string {
	if m.opts.StorageKey != "" {
		return m.opts.StorageKey + ".sandbox." + modID
	}
	return "modding.sandbox." + modID
}

func (m *Manager) enabledStorageKey(id string) string {
	return m.opts.StorageKey + ".enabled." + id
}

func (m *Manager) loadEnabledPref(id string) bool {
	if m.opts.StorageKey == "" {
		return true
	}
	v, ok := m.cx.Storage().GetItem(m.enabledStorageKey(id))
	if !ok {
		return true
	}
	return v != "0" && v != "false"
}

func (m *Manager) saveEnabledPref(id string, enabled bool) error {
	if m.opts.StorageKey == "" {
		return nil
	}
	val := "1"
	if !enabled {
		val = "0"
	}
	return m.cx.Storage().SetItem(m.enabledStorageKey(id), val)
}

func (m *Manager) modStorageKey(modID, key string) string {
	prefix := m.opts.StorageKey
	if prefix == "" {
		prefix = "modding"
	}
	// Escape the mod ID's own dots (and any escape marker) so "a" + "b.c" and
	// "a.b" + "c" cannot collide on the same storage key.
	escapedID := strings.ReplaceAll(strings.ReplaceAll(modID, "%", "%25"), ".", "%2E")
	return prefix + ".mod." + escapedID + "." + key
}

func (m *Manager) modStorageGet(modID, key string) (string, bool) {
	full := m.modStorageKey(modID, key)
	if m.opts.StorageKey != "" {
		return m.cx.Storage().GetItem(full)
	}
	v, ok := m.memStorage[full]
	return v, ok
}

func (m *Manager) modStorageSet(modID, key, val string) error {
	full := m.modStorageKey(modID, key)
	if m.opts.StorageKey != "" {
		return m.cx.Storage().SetItem(full, val)
	}
	m.memStorage[full] = val
	return nil
}

func (m *Manager) modStorageRemove(modID, key string) {
	full := m.modStorageKey(modID, key)
	if m.opts.StorageKey != "" {
		m.cx.Storage().RemoveItem(full)
		return
	}
	delete(m.memStorage, full)
}
