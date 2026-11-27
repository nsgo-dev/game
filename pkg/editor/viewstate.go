package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// ViewStateFile is where the editor keeps its view of each scene of a project, the 3D viewport's camera among it, as a slash-separated path from the project's root: a file of the editor's own, under the .game folder a project leaves out of version control, so moving the camera never changes a file anyone reviews.
const ViewStateFile = ".game/editor.yaml"

// viewStateVersion is the schema version of ViewStateFile.
const viewStateVersion = 1

// errViewStateNewer marks a ViewStateFile written by a newer editor, with a version above viewStateVersion: this editor neither reads it nor writes over it, so it never downgrades the views a newer editor kept.
var errViewStateNewer = errors.New("written by a newer editor")

// viewState is ViewStateFile's document: the view of each scene the editor has shown, sorted by path, plus per-project editor preferences such as snapping.
type viewState struct {
	Version int              `yaml:"version"`
	Scenes  []sceneViewState `yaml:"scenes,omitempty"`
	Snap    *SnapSettings    `yaml:"snap,omitempty"`
}

// sceneViewState is one scene's view.
type sceneViewState struct {
	// Path is the scene file's slash-separated path from the project's root, as ScenePath gives it.
	Path string `yaml:"path"`
	// Camera3D is the 3D viewport's camera as it was when the scene was last left, saved or closed.
	Camera3D camera3DState `yaml:"camera3D"`
}

// camera3DState is OrbitCamera3D as ViewStateFile writes it.
type camera3DState struct {
	Target       [3]float32 `yaml:"target,flow"`
	Yaw          float32    `yaml:"yaw"`
	Pitch        float32    `yaml:"pitch"`
	Distance     float32    `yaml:"distance"`
	FOV          float32    `yaml:"fov"`
	Near         float32    `yaml:"near"`
	Far          float32    `yaml:"far"`
	Orthographic bool       `yaml:"orthographic,omitempty"`
}

func camera3DStateOf(c OrbitCamera3D) camera3DState {
	return camera3DState{Target: c.Target, Yaw: c.Yaw, Pitch: c.Pitch, Distance: c.Distance, FOV: c.FOV, Near: c.Near, Far: c.Far, Orthographic: c.Orthographic}
}

// finite reports whether every number of the camera is finite, as parseViewState requires.
func (s camera3DState) finite() bool {
	for _, f := range []float32{s.Target[0], s.Target[1], s.Target[2], s.Yaw, s.Pitch, s.Distance, s.FOV, s.Near, s.Far} {
		if !isFiniteFloat32(f) {
			return false
		}
	}
	return true
}

func (s camera3DState) camera() OrbitCamera3D {
	return OrbitCamera3D{Target: s.Target, Yaw: s.Yaw, Pitch: s.Pitch, Distance: s.Distance, FOV: s.FOV, Near: s.Near, Far: s.Far, Orthographic: s.Orthographic}
}

// parseViewState decodes ViewStateFile, refusing unknown fields, trailing documents, a version other than viewStateVersion (with errViewStateNewer for a newer one, whatever fields it holds), a scene without a path or listed twice, and a camera value that is not finite.
func parseViewState(data []byte) (*viewState, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("delete %s or write a view state starting with version: %d", ViewStateFile, viewStateVersion), "editor: view state: empty document"), "", data, "")
	}
	// The version is read first and leniently, so a newer file's new fields do not hide that it is newer.
	var probe struct {
		Version int `yaml:"version"`
	}
	if yaml.Unmarshal(data, &probe) == nil && probe.Version > viewStateVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("open this project with a newer editor or remove %s", ViewStateFile), "editor: view state: version %d is %w (this editor writes %d)", probe.Version, errViewStateNewer, viewStateVersion), "", data, "")
	}
	var v viewState
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&v); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("editor: view state: %w", err), "", data, fmt.Sprintf("check the YAML syntax and field names in %s or delete it to reset defaults", ViewStateFile))
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the editor view state and remove any --- separator", "editor: view state: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("editor: view state: %w", err), "", data, "remove trailing YAML content after the editor view state")
	}
	if v.Version != viewStateVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of %s", viewStateVersion, ViewStateFile), "editor: view state: unsupported version %d (want %d)", v.Version, viewStateVersion), "", data, "")
	}
	seen := map[string]bool{}
	for i, sc := range v.Scenes {
		sPath := fmt.Sprintf("scenes[%d]", i)
		if !validScenePath(sc.Path) || strings.ContainsRune(sc.Path, '\\') {
			return nil, loaderr.Locate(loaderr.Errorf(sPath+".path", "set path to a slash-separated scene path relative to the project root", "editor: view state: scenes[%d].path %q is not a slash-separated path inside the project", i, sc.Path), "", data, "")
		}
		if seen[sc.Path] {
			return nil, loaderr.Locate(loaderr.Errorf(sPath+".path", "remove the duplicate scene entry from the view state", "editor: view state: scenes[%d].path %q is listed twice", i, sc.Path), "", data, "")
		}
		seen[sc.Path] = true
		if !sc.Camera3D.finite() {
			return nil, loaderr.Locate(loaderr.Errorf(sPath+".camera3D", "replace NaN or Inf camera values with finite numbers", "editor: view state: scenes[%d].camera3D holds a value that is not finite", i), "", data, "")
		}
	}
	if v.Snap != nil {
		if err := v.Snap.valid(); err != nil {
			return nil, loaderr.Locate(loaderr.Errorf("snap", "set valid snap settings or remove the snap block to use defaults", "editor: view state: %w", err), "", data, "")
		}
	}
	yamlzero.Normalize(&v)
	return &v, nil
}

// encodeViewState writes v as deterministic YAML, its scenes sorted by path.
func encodeViewState(v *viewState) ([]byte, error) {
	var snapCopy *SnapSettings
	if v.Snap != nil {
		s := *v.Snap
		snapCopy = &s
	}
	sorted := viewState{Version: v.Version, Scenes: slices.Clone(v.Scenes), Snap: snapCopy}
	slices.SortFunc(sorted.Scenes, func(a, b sceneViewState) int { return strings.Compare(a.Path, b.Path) })
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(&sorted)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("editor: view state: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("editor: view state: %w", err)
	}
	return buf.Bytes(), nil
}

// viewStatePath returns the OS path of the open project's ViewStateFile, and false without a project.
func (e *Editor) viewStatePath() (string, bool) {
	if e.Project == nil || e.ProjectPath == "" {
		return "", false
	}
	return filepath.Join(filepath.Dir(e.ProjectPath), filepath.FromSlash(ViewStateFile)), true
}

// loadViewState reads the open project's ViewStateFile. A missing file is an empty state; one that does not read or parse is an empty state too, with the error (the *fs.PathError of a failed read, errViewStateNewer for a newer file), so the camera falls back to its default; SaveViewState writes afresh only a file that does not parse.
func (e *Editor) loadViewState() (*viewState, error) {
	empty := &viewState{Version: viewStateVersion}
	p, ok := e.viewStatePath()
	if !ok {
		return empty, nil
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	v, err := parseViewState(data)
	if err != nil {
		return empty, loaderr.WithFile(err, p)
	}
	return v, nil
}

// SaveViewState records the open scene's view, the 3D viewport's camera, in the open project's ViewStateFile, so opening the scene again shows it as it is now. The editor saves it itself as another scene opens, as the scene is saved and as the window closes; an app that ends the editor another way calls it first. It writes nothing without a project or for a scene outside it, and leaves the file as it is when it already holds this view. Creating the .game folder, it writes a .gitignore there that ignores everything in it, unless one is there already. Scenes the file lists whose files are gone are dropped from it, and a file that does not parse is written afresh; a file that does not read, or that a newer editor wrote, is left as it is and reported. A camera holding a value that is not finite is saved as DefaultOrbitCamera3D, so the file always loads.
func (e *Editor) SaveViewState() error {
	if e == nil {
		return nil
	}
	p, ok := e.viewStatePath()
	if !ok {
		return nil
	}
	e.syncActiveTab()
	root := filepath.Dir(e.ProjectPath)
	cams := make(map[string]camera3DState, len(e.tabs))
	for i, t := range e.tabs {
		if t.scenePath == "" {
			continue
		}
		if i != e.activeTab {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(t.scenePath))); errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		cam := camera3DStateOf(t.camera3D)
		if !cam.finite() {
			cam = camera3DStateOf(DefaultOrbitCamera3D())
		}
		cams[t.scenePath] = cam
	}
	snap := e.SnapSettings().normalizedForViewState()
	// A damaged file is replaced when there is new view state to write, as a lost one would be:
	// it holds only views. One that could not be read, or that a newer editor wrote, is not this
	// editor's to replace. When this editor has neither open scenes nor non-default snap settings,
	// leave missing, damaged, unreadable, newer, or already-clean files untouched (only rewrite to
	// clear a stale snap block from a valid file).
	v, err := e.loadViewState()
	var pathErr *fs.PathError
	if errors.Is(err, errViewStateNewer) || errors.As(err, &pathErr) {
		if len(cams) == 0 && snap == nil {
			return nil
		}
		return err
	}
	if len(cams) == 0 && snap == nil && (err != nil || v.Snap == nil) {
		return nil
	}
	seen := make(map[string]bool, len(cams))
	kept := v.Scenes[:0]
	for _, sc := range v.Scenes {
		if cam, ok := cams[sc.Path]; ok {
			sc.Camera3D = cam
			seen[sc.Path] = true
		} else if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sc.Path))); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		kept = append(kept, sc)
	}
	for sp, cam := range cams {
		if !seen[sp] {
			kept = append(kept, sceneViewState{Path: sp, Camera3D: cam})
		}
	}
	v.Scenes = kept
	v.Snap = snap
	data, err := encodeViewState(v)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The folder keeps itself out of version control, so a project that does not ignore it yet never commits a camera move.
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Lstat(ignore); errors.Is(err, fs.ErrNotExist) {
		if err := writeFileAtomic(ignore, []byte("*\n")); err != nil {
			return err
		}
	}
	return writeFileAtomic(p, data)
}

// restoreViewState puts back the view ViewStateFile holds for the open scene, or the default camera for a scene it does not list. Without a project, or for a scene outside it, it leaves the camera as it is. A file that does not read or parse is reported in the status bar and leaves the default camera.
func (e *Editor) restoreViewState() {
	if e.ScenePath == "" {
		return
	}
	if _, ok := e.viewStatePath(); !ok {
		return
	}
	e.endFly3D()
	e.drag3D = drag3DState{}
	e.Camera3D = DefaultOrbitCamera3D()
	v, err := e.loadViewState()
	if err != nil {
		e.Status = "Could not restore the view: " + err.Error()
		return
	}
	for _, sc := range v.Scenes {
		if sc.Path == e.ScenePath {
			e.Camera3D = sc.Camera3D.camera()
			return
		}
	}
}

// restoreProjectSnapState loads the open project's snap preferences from ViewStateFile,
// falling back to the user's EditorSettings.Snap (or DefaultSnapSettings when unset) when the file is missing, has no snap section, or cannot be parsed.
func (e *Editor) restoreProjectSnapState() {
	if _, ok := e.viewStatePath(); !ok {
		return
	}
	v, err := e.loadViewState()
	if err != nil || v.Snap == nil {
		e.applyViewStateSnap(e.Settings().Snap)
		return
	}
	e.applyViewStateSnap(v.Snap)
}

// saveViewStateQuietly is SaveViewState for the moments the editor saves the view by itself, which report a failure in the status bar rather than fail what triggered them.
func (e *Editor) saveViewStateQuietly() {
	if err := e.SaveViewState(); err != nil {
		e.Status = "Could not save the view: " + err.Error()
	}
}
