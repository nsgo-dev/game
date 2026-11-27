package resource

import (
	"fmt"
	"io/fs"
	"slices"
	"sync"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// Library loads resource files from a file system and shares them: every Load of one path returns the same pointer, so the scenes and code that use a resource all see one value, and Reload changes that value in place.
//
// Paths are slash-separated and relative to the file system's root, as fs.ValidPath requires, such as "materials/stone.material.yaml".
//
// Load and Reload are safe to call from any goroutine, but Reload writes the shared value that readers hold, so call it on the goroutine that reads resources, usually the engine's; hotreload.Watcher.WatchResources does.
type Library struct {
	fsys fs.FS

	mu       sync.Mutex
	items    map[string]Resource
	revision uint64
}

// NewLibrary returns a Library that reads files from fsys, such as os.DirFS(projectRoot) or an embed.FS.
func NewLibrary(fsys fs.FS) *Library {
	return &Library{fsys: fsys, items: make(map[string]Resource)}
}

// Load returns the resource at path, parsing it on first use and returning the shared value after that.
func (l *Library) Load(path string) (Resource, error) {
	if l == nil {
		return nil, fmt.Errorf("resource: nil library")
	}
	if !fs.ValidPath(path) || path == "." {
		return nil, fmt.Errorf("resource: %q is not a slash-separated path relative to the library root", path)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, ok := l.items[path]; ok {
		return r, nil
	}
	if l.fsys == nil {
		return nil, fmt.Errorf("resource: library has no file system to load %q from", path)
	}
	data, err := fs.ReadFile(l.fsys, path)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("resource: load %s: %w", path, err), path, nil, "check that the resource file exists and is readable")
	}
	r, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	l.items[path] = r
	return r, nil
}

// LoadAs loads the resource at path from l and returns it as T, failing when the file holds another type.
func LoadAs[T Resource](l *Library, path string) (T, error) {
	var zero T
	r, err := l.Load(path)
	if err != nil {
		return zero, err
	}
	t, ok := r.(T)
	if !ok {
		return zero, fmt.Errorf("resource: %s is a %s, want %s", path, r.Kind(), kindOf[T]())
	}
	return t, nil
}

// Material loads the material at path. It lets a Library serve as a sceneformat.MaterialResolver.
func (l *Library) Material(path string) (*Material, error) { return LoadAs[*Material](l, path) }

// PhysicsMaterial loads the physics material at path. It lets a Library serve as a sceneformat.PhysicsMaterialResolver.
func (l *Library) PhysicsMaterial(path string) (*PhysicsMaterial, error) {
	return LoadAs[*PhysicsMaterial](l, path)
}

// Animation loads the animation clip at path. It lets a Library serve as an animclip.ClipResolver.
func (l *Library) Animation(path string) (*Animation, error) {
	return LoadAs[*Animation](l, path)
}

// Reload parses data as the new contents of path and, when path is loaded, copies them into the shared value so every holder sees them. It reports whether path was loaded; an unloaded path is ignored. On an error, including a file that changed its type, the loaded value is left as it was.
func (l *Library) Reload(path string, data []byte) (bool, error) {
	if l == nil {
		return false, nil
	}
	l.mu.Lock()
	old, ok := l.items[path]
	l.mu.Unlock()
	if !ok {
		return false, nil
	}
	r, err := Parse(data)
	if err != nil {
		return true, loaderr.WithFile(err, path)
	}
	if r.Kind() != old.Kind() {
		return true, loaderr.Locate(loaderr.Errorf("type", fmt.Sprintf("keep type: %s when reloading %s", old.Kind(), path), "resource: reload %s: type changed from %s to %s", path, old.Kind(), r.Kind()), path, data, "")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch dst := old.(type) {
	case *Material:
		*dst = *r.(*Material)
	case *Curve:
		*dst = *r.(*Curve)
	case *Gradient:
		*dst = *r.(*Gradient)
	case *Animation:
		*dst = *r.(*Animation)
	case *Theme:
		*dst = *r.(*Theme)
	case *BusLayout:
		*dst = *r.(*BusLayout)
	case *PhysicsMaterial:
		*dst = *r.(*PhysicsMaterial)
	}
	l.revision++
	return true, nil
}

// Loaded reports whether path has been loaded.
func (l *Library) Loaded(path string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.items[path]
	return ok
}

// Paths returns the loaded paths, sorted.
func (l *Library) Paths() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.items))
	for p := range l.items {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// ReloadFile re-reads path from the library's file system and reloads it in place via Reload. It reports the file's byte size, whether path was loaded in the library, and any read or parse error. An unloaded path returns (0, false, nil). On error, the previously loaded resource stays unchanged.
func (l *Library) ReloadFile(path string) (int64, bool, error) {
	if l == nil {
		return 0, false, nil
	}
	l.mu.Lock()
	_, ok := l.items[path]
	fsys := l.fsys
	l.mu.Unlock()
	if !ok {
		return 0, false, nil
	}
	if fsys == nil {
		return 0, true, fmt.Errorf("resource: library has no file system to reload %q from", path)
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return 0, true, loaderr.Locate(fmt.Errorf("resource: reload %s: %w", path, err), path, nil, "check that the resource file exists and is readable")
	}
	loaded, err := l.Reload(path, data)
	return int64(len(data)), loaded, err
}

// Revision counts the successful reloads, so code that derives values from resources, such as a gameui.Theme built with Theme.Apply, can tell when to derive them again.
func (l *Library) Revision() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.revision
}
