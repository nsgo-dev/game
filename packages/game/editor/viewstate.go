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

	"go.yaml.in/yaml/v3"
)

// ViewStateFile is where the editor keeps its view of each scene of a project, the 3D viewport's camera among it, as a slash-separated path from the project's root: a file of the editor's own, under the .game folder a project leaves out of version control, so moving the camera never changes a file anyone reviews.
const ViewStateFile = ".game/editor.yaml"

// viewStateVersion is the schema version of ViewStateFile.
const viewStateVersion = 1

// viewState is ViewStateFile's document: the view of each scene the editor has shown, sorted by path.
type viewState struct {
	Version int              `yaml:"version"`
	Scenes  []sceneViewState `yaml:"scenes,omitempty"`
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

func (s camera3DState) camera() OrbitCamera3D {
	return OrbitCamera3D{Target: s.Target, Yaw: s.Yaw, Pitch: s.Pitch, Distance: s.Distance, FOV: s.FOV, Near: s.Near, Far: s.Far, Orthographic: s.Orthographic}
}

// parseViewState decodes ViewStateFile, refusing unknown fields, trailing documents, a version other than viewStateVersion, a scene without a path or listed twice, and a camera value that is not finite.
func parseViewState(data []byte) (*viewState, error) {
	var v viewState
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("editor: view state: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("editor: view state: %w", err)
	}
	if v.Version != viewStateVersion {
		return nil, fmt.Errorf("editor: view state: unsupported version %d (want %d)", v.Version, viewStateVersion)
	}
	seen := map[string]bool{}
	for i, sc := range v.Scenes {
		if !validScenePath(sc.Path) || strings.ContainsRune(sc.Path, '\\') {
			return nil, fmt.Errorf("editor: view state: scenes[%d].path %q is not a slash-separated path inside the project", i, sc.Path)
		}
		if seen[sc.Path] {
			return nil, fmt.Errorf("editor: view state: scenes[%d].path %q is listed twice", i, sc.Path)
		}
		seen[sc.Path] = true
		c := sc.Camera3D
		for _, f := range []float32{c.Target[0], c.Target[1], c.Target[2], c.Yaw, c.Pitch, c.Distance, c.FOV, c.Near, c.Far} {
			if !isFiniteFloat32(f) {
				return nil, fmt.Errorf("editor: view state: scenes[%d].camera3D holds a value that is not finite", i)
			}
		}
	}
	return &v, nil
}

// encodeViewState writes v as deterministic YAML, its scenes sorted by path.
func encodeViewState(v *viewState) ([]byte, error) {
	sorted := viewState{Version: v.Version, Scenes: slices.Clone(v.Scenes)}
	slices.SortFunc(sorted.Scenes, func(a, b sceneViewState) int { return strings.Compare(a.Path, b.Path) })
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&sorted); err != nil {
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

// loadViewState reads the open project's ViewStateFile. A missing file is an empty state; one that does not read or parse is an empty state too, with the error, so the camera falls back to its default and the next save writes the file afresh.
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
		return empty, fmt.Errorf("%s: %w", p, err)
	}
	return v, nil
}

// SaveViewState records the open scene's view, the 3D viewport's camera, in the open project's ViewStateFile, so opening the scene again shows it as it is now. The editor saves it itself as another scene opens, as the scene is saved and as the window closes; an app that ends the editor another way calls it first. It writes nothing without a project or for a scene outside it, and leaves the file as it is when it already holds this view. Creating the .game folder, it writes a .gitignore there that ignores everything in it, unless one is there already. Scenes the file lists whose files are gone are dropped from it, and a file that does not parse is written afresh.
func (e *Editor) SaveViewState() error {
	if e == nil || e.ScenePath == "" {
		return nil
	}
	p, ok := e.viewStatePath()
	if !ok {
		return nil
	}
	// A damaged file is replaced, as a lost one would be: it holds only views.
	v, _ := e.loadViewState()
	root := filepath.Dir(e.ProjectPath)
	cam := camera3DStateOf(e.Camera3D)
	found := false
	kept := v.Scenes[:0]
	for _, sc := range v.Scenes {
		if sc.Path == e.ScenePath {
			sc.Camera3D, found = cam, true
		} else if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sc.Path))); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		kept = append(kept, sc)
	}
	if !found {
		kept = append(kept, sceneViewState{Path: e.ScenePath, Camera3D: cam})
	}
	v.Scenes = kept
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

// saveViewStateQuietly is SaveViewState for the moments the editor saves the view by itself, which report a failure in the status bar rather than fail what triggered them.
func (e *Editor) saveViewStateQuietly() {
	if err := e.SaveViewState(); err != nil {
		e.Status = "Could not save the view: " + err.Error()
	}
}
