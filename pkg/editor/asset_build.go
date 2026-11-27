package editor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrAssetsBuilding is returned by BuildAssets and ReimportAsset when an asset build is already running.
var ErrAssetsBuilding = errors.New("editor: asset build is already running")

// ErrAssetsBuildUnsupported is returned by BuildAssets and ReimportAsset on non-desktop targets when AssetsCommandRunner is nil.
var ErrAssetsBuildUnsupported = errors.New("editor: building assets is only supported on desktop")

// AssetsBuildOptions configures one `go tool game assets` run started by BuildAssetsWith.
type AssetsBuildOptions struct {
	// Force passes -force to `game assets`, rebuilding every target even when the cache says it is up to date.
	Force bool
}

// AssetsBuildSpec describes one `go tool game assets` command executed by BuildAssets or ReimportAsset.
type AssetsBuildSpec struct {
	// Dir is the open project's root directory where the command executes.
	Dir string
	// Name is the executable to run ("go").
	Name string
	// Args are the command arguments (for example ["tool", "game", "assets"]).
	Args []string
	// Asset is the project-relative path being reimported when launched by ReimportAsset, or empty for BuildAssets.
	Asset string
	// Force reports whether -force was requested.
	Force bool
}

// AssetReloader may optionally be implemented by a sceneformat.AssetResolver to reload
// built atlases and textures after BuildAssets or ReimportAsset writes new outputs.
type AssetReloader interface {
	ReloadAssets(root string, changed []string) error
}

// ReloadAssetsResult summarizes which built atlases and textures changed on disk and were reloaded by ReloadAssets.
type ReloadAssetsResult struct {
	// Atlases lists the names of sprite atlases reloaded from disk, sorted.
	Atlases []string
	// Textures lists the project-relative paths of texture outputs that changed on disk, sorted.
	Textures []string
}

type assetBuildRun struct {
	project  string
	cancel   context.CancelFunc
	done     chan struct{}
	doneOnce sync.Once
}

func (r *assetBuildRun) finish() {
	r.doneOnce.Do(func() {
		close(r.done)
	})
}

type assetBuildState struct {
	mu          sync.Mutex
	run         *assetBuildRun
	lastErr     error
	scope       *core.Scope
	assetHashes map[string][sha256.Size]byte
	imageVer    atomic.Uint64
}

func (s *assetBuildState) setScope(cx *core.Scope) {
	s.mu.Lock()
	s.scope = cx
	s.mu.Unlock()
}

// AssetsBuilding reports whether a BuildAssets or ReimportAsset command is currently in flight. It is safe to call from any goroutine.
func (e *Editor) AssetsBuilding() bool {
	if e == nil {
		return false
	}
	e.assetBuild.mu.Lock()
	defer e.assetBuild.mu.Unlock()
	return e.assetBuild.run != nil
}

// AssetsBuildLastError returns the error from the most recent completed BuildAssets or ReimportAsset run, or nil.
func (e *Editor) AssetsBuildLastError() error {
	if e == nil {
		return nil
	}
	e.assetBuild.mu.Lock()
	defer e.assetBuild.mu.Unlock()
	return e.assetBuild.lastErr
}

// WaitAssetsBuild blocks until any in-flight BuildAssets or ReimportAsset run finishes and returns its final error.
func (e *Editor) WaitAssetsBuild() error {
	if e == nil {
		return nil
	}
	e.assetBuild.mu.Lock()
	run := e.assetBuild.run
	e.assetBuild.mu.Unlock()
	if run != nil {
		<-run.done
	}
	e.assetBuild.mu.Lock()
	err := e.assetBuild.lastErr
	e.assetBuild.mu.Unlock()
	return err
}

// CancelAssetsBuild cancels an in-flight BuildAssets or ReimportAsset run, if any.
func (e *Editor) CancelAssetsBuild() {
	if e == nil {
		return
	}
	e.assetBuild.mu.Lock()
	run := e.assetBuild.run
	e.assetBuild.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

func (e *Editor) stopAssetsBuild() {
	if e == nil {
		return
	}
	e.assetBuild.mu.Lock()
	run := e.assetBuild.run
	e.assetBuild.run = nil
	e.assetBuild.mu.Unlock()
	if run != nil {
		run.cancel()
		run.finish()
	}
}

// BuildAssets runs `go tool game assets` for the open project off the UI goroutine, streaming stdout and stderr into the Output panel, and reloads changed atlases and textures once the build succeeds.
func (e *Editor) BuildAssets() error {
	return e.BuildAssetsWith(AssetsBuildOptions{})
}

// BuildAssetsWith runs `go tool game assets` with opts for the open project off the UI goroutine, streaming stdout and stderr into the Output panel, and reloads changed atlases and textures once the build succeeds.
func (e *Editor) BuildAssetsWith(opts AssetsBuildOptions) error {
	return e.startAssetsBuild("", opts)
}

// ReimportAsset runs `go tool game assets` for relPath (or Editor.SelectedFile when relPath is empty) off the UI goroutine, streaming output into the Output panel, and reloads changed atlases and textures once the build succeeds.
func (e *Editor) ReimportAsset(relPath string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if relPath == "" {
		relPath = e.SelectedFile
	}
	clean, err := normalizeProjectAssetPath(relPath)
	if err != nil {
		return err
	}
	if e.AssetImportKind(clean) == AssetImportNone {
		return fmt.Errorf("editor: %q is not an importable asset", relPath)
	}
	return e.startAssetsBuild(clean, AssetsBuildOptions{})
}

func (e *Editor) startAssetsBuild(assetRel string, opts AssetsBuildOptions) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if e.playing {
		return ErrPlaying
	}

	e.assetBuild.mu.Lock()
	if e.assetBuild.run != nil {
		e.assetBuild.mu.Unlock()
		return ErrAssetsBuilding
	}
	runner := e.AssetsCommandRunner
	if runner == nil {
		if !canRunGameDesktop {
			e.assetBuild.mu.Unlock()
			return ErrAssetsBuildUnsupported
		}
		runner = defaultAssetsCommandRunner
	}

	projectPath := e.ProjectPath
	projectDir := filepath.Dir(projectPath)
	args := []string{"tool", "game", "assets"}
	if opts.Force {
		args = append(args, "-force")
	}
	spec := AssetsBuildSpec{
		Dir:   projectDir,
		Name:  "go",
		Args:  args,
		Asset: assetRel,
		Force: opts.Force,
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &assetBuildRun{
		project: projectPath,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	e.assetBuild.run = run
	e.assetBuild.lastErr = nil
	e.assetBuild.mu.Unlock()

	e.output.mu.Lock()
	e.output.open = true
	e.output.mu.Unlock()

	var startMsg, statusMsg string
	if assetRel != "" {
		startMsg = fmt.Sprintf("Reimporting %s: %s %s", assetRel, spec.Name, strings.Join(spec.Args, " "))
		statusMsg = "Reimporting " + assetRel + "…"
	} else {
		startMsg = fmt.Sprintf("Building assets: %s %s", spec.Name, strings.Join(spec.Args, " "))
		statusMsg = "Building assets…"
	}
	e.appendOutputEntry(OutputEditor, OutputInfo, startMsg, projectDir)
	e.setStatusWithoutLogging(statusMsg)
	e.notify()

	stdoutWriter := newOutputStreamWriter(func(line string) {
		e.appendAssetBuildOutput(OutputStdout, OutputInfo, line, projectDir)
	})
	stderrWriter := newOutputStreamWriter(func(line string) {
		e.appendAssetBuildOutput(OutputStderr, OutputError, line, projectDir)
	})

	go func() {
		defer cancel()
		runErr := runner(ctx, spec, stdoutWriter, stderrWriter)
		stdoutWriter.Flush()
		stderrWriter.Flush()

		e.postAssetBuildUI(func() {
			e.assetBuild.mu.Lock()
			active := e.assetBuild.run == run
			if !active {
				e.assetBuild.mu.Unlock()
				run.finish()
				return
			}
			e.assetBuild.run = nil
			e.assetBuild.mu.Unlock()

			var finalErr error
			if runErr != nil {
				finalErr = runErr
				var msg string
				if assetRel != "" {
					msg = "Reimport failed: " + runErr.Error()
				} else {
					msg = "Assets build failed: " + runErr.Error()
				}
				e.assetBuild.mu.Lock()
				e.assetBuild.lastErr = finalErr
				e.assetBuild.mu.Unlock()
				e.reportStatusError(msg)
				run.finish()
				return
			}

			reloaded, reloadErr := e.ReloadAssets()
			if reloadErr != nil {
				finalErr = reloadErr
				e.assetBuild.mu.Lock()
				e.assetBuild.lastErr = finalErr
				e.assetBuild.mu.Unlock()
				e.reportStatusError("Assets reload failed: " + reloadErr.Error())
				run.finish()
				return
			}

			e.assetBuild.mu.Lock()
			e.assetBuild.lastErr = nil
			e.assetBuild.mu.Unlock()

			doneMsg := formatAssetBuildDoneStatus(assetRel, reloaded)
			e.appendOutputEntry(OutputEditor, OutputInfo, doneMsg, projectDir)
			e.setStatusWithoutLogging(doneMsg)
			e.notify()
			run.finish()
		})
	}()

	return nil
}

func formatAssetBuildDoneStatus(assetRel string, reloaded ReloadAssetsResult) string {
	var base string
	if assetRel != "" {
		base = "Reimported " + assetRel
	} else {
		base = "Built assets"
	}
	var parts []string
	if n := len(reloaded.Atlases); n > 0 {
		word := "atlases"
		if n == 1 {
			word = "atlas"
		}
		parts = append(parts, fmt.Sprintf("reloaded %d %s", n, word))
	}
	if n := len(reloaded.Textures); n > 0 {
		word := "textures"
		if n == 1 {
			word = "texture"
		}
		parts = append(parts, fmt.Sprintf("reloaded %d %s", n, word))
	}
	if len(parts) == 0 {
		return base
	}
	return base + " (" + strings.Join(parts, ", ") + ")"
}

func (e *Editor) appendAssetBuildOutput(source OutputSource, level OutputLevel, line, projectDir string) {
	e.appendOutputEntry(source, level, line, projectDir)
	e.assetBuild.mu.Lock()
	scope := e.assetBuild.scope
	e.assetBuild.mu.Unlock()
	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
	}
}

func (e *Editor) postAssetBuildUI(fn func()) {
	e.postOrRun(fn)
}

func defaultAssetsCommandRunner(ctx context.Context, spec AssetsBuildSpec, stdout, stderr io.Writer) error {
	var cmd *exec.Cmd
	if spec.Name == "go" {
		cmd = gotoolchain.CommandContext(ctx, spec.Args...)
	} else {
		cmd = exec.CommandContext(ctx, spec.Name, spec.Args...)
	}
	cmd.Dir = spec.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// snapshotAssetOutputs records content hashes of any already-built atlas and texture outputs when a project opens so ReloadAssets can tell what a subsequent build changed.
func (e *Editor) snapshotAssetOutputs() {
	if e == nil {
		return
	}
	hashes := make(map[string][sha256.Size]byte)
	if e.Project != nil && e.ProjectPath != "" && e.Project.Assets != nil {
		root := filepath.Dir(e.ProjectPath)
		for _, at := range e.Project.Assets.Atlases {
			if jsonRel, _, _, _, sum, ok := readAtlasTargetFiles(root, at.Out, at.Name); ok {
				hashes[jsonRel] = sum
			}
		}
		for _, tex := range e.Project.Assets.Textures {
			rel, ok := cleanProjectRelPath(tex.Out)
			if !ok {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
				hashes[rel] = sha256.Sum256(data)
			}
		}
	}
	e.assetBuild.mu.Lock()
	e.assetBuild.assetHashes = hashes
	e.assetBuild.mu.Unlock()
}

// ReloadAssets scans the open project's built atlas and texture outputs declared in game.yaml, reloads any that changed on disk into Editor.Resolver (updating AtlasMap entries or existing *sprite.Atlas values in place and invoking AssetReloader when implemented), rebuilds the open scene so the viewport reflects the new frames immediately, refreshes the asset browser and tile palette, and re-runs Problems check.
func (e *Editor) ReloadAssets() (ReloadAssetsResult, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ReloadAssetsResult{}, ErrNoProject
	}
	var res ReloadAssetsResult
	if e.Project.Assets == nil {
		e.filesRev++
		e.assetsRev++
		e.invalidateProjectIndex()
		e.Rebuild()
		e.notify()
		e.CheckProblems()
		return res, nil
	}

	root := filepath.Dir(e.ProjectPath)
	var changedPaths []string
	var reloadErr error

	e.assetBuild.mu.Lock()
	if e.assetBuild.assetHashes == nil {
		e.assetBuild.assetHashes = make(map[string][sha256.Size]byte)
	}
	e.assetBuild.mu.Unlock()

	for _, at := range e.Project.Assets.Atlases {
		jsonRel, imgRel, jsonData, pngData, sum, ok := readAtlasTargetFiles(root, at.Out, at.Name)
		if !ok {
			continue
		}
		e.assetBuild.mu.Lock()
		prevSum, hadPrev := e.assetBuild.assetHashes[jsonRel]
		e.assetBuild.mu.Unlock()

		// Reload when the built output hash changed, or when the resolver doesn't yet hold this atlas.
		_, existingAtlas := e.lookupAtlasForReload(at.Name, path.Join(at.Out, at.Name))
		if hadPrev && prevSum == sum && existingAtlas != nil {
			continue
		}

		ver := e.assetBuild.imageVer.Add(1)
		imgSrc := core.ImageSource{
			URI:     "asset:" + imgRel,
			Data:    pngData,
			Version: ver,
		}
		loaded, err := sprite.LoadAtlas(imgSrc, jsonData)
		if err != nil {
			reloadErr = errors.Join(reloadErr, fmt.Errorf("reload atlas %s: %w", jsonRel, err))
			continue
		}

		e.storeReloadedAtlas(at.Name, path.Join(at.Out, at.Name), loaded)
		e.assetBuild.mu.Lock()
		e.assetBuild.assetHashes[jsonRel] = sum
		e.assetBuild.mu.Unlock()

		res.Atlases = append(res.Atlases, at.Name)
		changedPaths = append(changedPaths, jsonRel, imgRel)
	}

	for _, tex := range e.Project.Assets.Textures {
		rel, ok := cleanProjectRelPath(tex.Out)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		e.assetBuild.mu.Lock()
		prevSum, hadPrev := e.assetBuild.assetHashes[rel]
		if !hadPrev || prevSum != sum {
			e.assetBuild.assetHashes[rel] = sum
			e.assetBuild.mu.Unlock()
			res.Textures = append(res.Textures, rel)
			changedPaths = append(changedPaths, rel)
		} else {
			e.assetBuild.mu.Unlock()
		}
	}

	slices.Sort(res.Atlases)
	slices.Sort(res.Textures)
	slices.Sort(changedPaths)
	changedPaths = slices.Compact(changedPaths)

	if len(changedPaths) > 0 {
		if reloader, ok := e.Resolver.(AssetReloader); ok {
			if err := reloader.ReloadAssets(root, changedPaths); err != nil {
				reloadErr = errors.Join(reloadErr, err)
			}
		}
	}

	// Keep SelectedAsset pointing at the refreshed frame rect/image (or clear if removed).
	if e.SelectedAsset != nil && e.Resolver != nil {
		if a, err := e.Resolver.Atlas(e.SelectedAsset.Atlas); err == nil && a != nil {
			if frame, ok := a.Frames[e.SelectedAsset.Frame]; ok {
				updated := AssetEntry{
					Atlas: e.SelectedAsset.Atlas,
					Frame: e.SelectedAsset.Frame,
					Image: a.Image,
					Rect:  frame,
				}
				e.SelectedAsset = &updated
			} else {
				e.SelectedAsset = nil
			}
		}
	}

	e.filesRev++
	e.assetsRev++
	e.invalidate3DBuild()
	e.invalidateProjectIndex()
	e.keepTileInPalette()
	e.Rebuild()
	if e.ExportPanelOpen() {
		e.CheckExportCredits()
	}
	e.notify()
	e.CheckProblems()

	if e.OnReloadAssets != nil && (len(res.Atlases) > 0 || len(res.Textures) > 0) {
		e.OnReloadAssets(res)
	}
	return res, reloadErr
}

func (e *Editor) lookupAtlasForReload(name, fullPathKey string) (string, *sprite.Atlas) {
	if e.Resolver == nil {
		return "", nil
	}
	if a, err := e.Resolver.Atlas(name); err == nil && a != nil {
		return name, a
	}
	if fullPathKey != name {
		if a, err := e.Resolver.Atlas(fullPathKey); err == nil && a != nil {
			return fullPathKey, a
		}
	}
	return "", nil
}

func (e *Editor) storeReloadedAtlas(name, fullPathKey string, loaded *sprite.Atlas) {
	if e.Resolver == nil {
		e.Resolver = sceneformat.AtlasMap{name: loaded}
		return
	}
	if am, ok := e.Resolver.(sceneformat.AtlasMap); ok {
		updatedAny := false
		for _, key := range []string{name, fullPathKey} {
			if existing, exists := am[key]; exists && existing != nil {
				*existing = *loaded
				updatedAny = true
			}
		}
		if !updatedAny {
			am[name] = loaded
		}
		return
	}
	if _, existing := e.lookupAtlasForReload(name, fullPathKey); existing != nil {
		*existing = *loaded
	}
}

// cleanProjectRelPath cleans a slash-separated project-relative file path and reports whether it stays strictly inside the project root.
func cleanProjectRelPath(p string) (string, bool) {
	if p == "" || strings.Contains(p, "\\") {
		return "", false
	}
	clean := path.Clean(p)
	if !fs.ValidPath(clean) || clean == "." {
		return "", false
	}
	return clean, true
}

// readAtlasTargetFiles reads an atlas target's primary JSON metadata and its referenced PNG image, returning their resolved relative paths, bytes, and combined SHA-256 digest.
func readAtlasTargetFiles(root, outDir, name string) (jsonRel, imgRel string, jsonData, pngData []byte, sum [sha256.Size]byte, ok bool) {
	if strings.Contains(outDir, "\\") || strings.Contains(name, "\\") {
		return "", "", nil, nil, sum, false
	}
	primaryRel, valid := cleanProjectRelPath(path.Join(outDir, name+".json"))
	if !valid {
		return "", "", nil, nil, sum, false
	}
	jsonRel = primaryRel
	jsonBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(jsonRel)))
	if err != nil {
		// Multi-page atlases write name-0.json when split across pages.
		altRel, altValid := cleanProjectRelPath(path.Join(outDir, name+"-0.json"))
		if !altValid {
			return "", "", nil, nil, sum, false
		}
		jsonBytes, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(altRel)))
		if err != nil {
			return "", "", nil, nil, sum, false
		}
		jsonRel = altRel
	}
	imgRel, imgValid := atlasMetaImagePath(outDir, name, jsonBytes)
	if !imgValid {
		return "", "", nil, nil, sum, false
	}
	pngBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(imgRel)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "", nil, nil, sum, false
	}
	h := sha256.New()
	_, _ = h.Write(jsonBytes)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(pngBytes)
	copy(sum[:], h.Sum(nil))
	return jsonRel, imgRel, jsonBytes, pngBytes, sum, true
}

func atlasMetaImagePath(outDir, name string, jsonData []byte) (string, bool) {
	if strings.Contains(outDir, "\\") || strings.Contains(name, "\\") {
		return "", false
	}
	fallback, ok := cleanProjectRelPath(path.Join(outDir, name+".png"))
	var doc struct {
		Meta struct {
			Image string `json:"image"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(jsonData, &doc); err == nil && doc.Meta.Image != "" {
		// meta.image must itself be a clean relative path with no ".." segments so it stays inside outDir and the project root.
		if metaClean, valid := cleanProjectRelPath(doc.Meta.Image); valid {
			if candidate, candValid := cleanProjectRelPath(path.Join(outDir, metaClean)); candValid {
				return candidate, true
			}
		}
	}
	return fallback, ok
}
