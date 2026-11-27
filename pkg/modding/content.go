package modding

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// contentCategories are the canonical top-level directories exposed by [Manager.ContentFS], in sorted byte order.
var contentCategories = []string{"assets", "datatables", "l10n", "scenes"}

// maxSymlinkHops bounds symlink traversal during confinement checks to refuse symlink loops.
const maxSymlinkHops = 40

type contentLayer struct {
	modID      string
	modFS      fs.FS
	contribDir string // cleaned relative directory inside modFS
}

type contentSnapshot struct {
	byCategory map[string][]contentLayer // layers in load order (later mods win)
}

// ContentFS returns a read-only [io/fs.FS] overlaying the contributes directories of every
// currently loaded mod in load order (later mods win per path).
//
// Files appear under the canonical category directories declared by [ModContributes]:
// "assets/...", "scenes/...", "l10n/...", and "datatables/...". A host can pass the
// returned FS directly to [assets.WithFS] or take a category subtree with [io/fs.Sub]
// (for example, fs.Sub(mgr.ContentFS(), "assets") or fs.Sub(mgr.ContentFS(), "l10n")).
//
// The returned FS reflects load, unload, reload, and discovery updates safely across
// goroutines. Paths are strictly confined to each mod's directory; path traversal and
// symlinks escaping a mod's directory are refused.
func (m *Manager) ContentFS() fs.FS {
	return &contentOverlayFS{mgr: m}
}

func (m *Manager) refreshContentOverlay() {
	byCat := make(map[string][]contentLayer)
	for _, id := range m.order {
		mod := m.mods[id]
		if mod == nil || mod.state != StateLoaded || mod.fsys == nil {
			continue
		}
		c := mod.manifest.Contributes
		for _, item := range []struct {
			cat string
			dir string
		}{
			{"assets", c.Assets},
			{"datatables", c.DataTables},
			{"l10n", c.L10n},
			{"scenes", c.Scenes},
		} {
			if item.dir == "" {
				continue
			}
			byCat[item.cat] = append(byCat[item.cat], contentLayer{
				modID:      id,
				modFS:      mod.fsys,
				contribDir: path.Clean(item.dir),
			})
		}
	}
	m.contentSnap.Store(&contentSnapshot{byCategory: byCat})
}

type contentOverlayFS struct {
	mgr *Manager
}

var (
	_ fs.FS        = (*contentOverlayFS)(nil)
	_ fs.StatFS    = (*contentOverlayFS)(nil)
	_ fs.ReadDirFS = (*contentOverlayFS)(nil)
)

func (o *contentOverlayFS) snapshot() *contentSnapshot {
	if snap := o.mgr.contentSnap.Load(); snap != nil {
		return snap
	}
	return &contentSnapshot{}
}

func validOverlayPath(name string) bool {
	return fs.ValidPath(name) && !strings.Contains(name, "\\")
}

func (o *contentOverlayFS) Open(name string) (fs.File, error) {
	if !validOverlayPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	snap := o.snapshot()
	if name == "." {
		entries := o.rootEntries(snap)
		return &virtualDirFile{
			info:    virtualDirInfo{name: "."},
			entries: entries,
		}, nil
	}
	cat, rest, hasSlash := strings.Cut(name, "/")
	layers := snap.byCategory[cat]
	if len(layers) == 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if !hasSlash {
		entries, err := mergeDirEntries(layers, "", name)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &virtualDirFile{
			info:    virtualDirInfo{name: cat},
			entries: entries,
		}, nil
	}

	// Search layers from highest priority (latest loaded) down to lowest.
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		modRel := path.Join(layer.contribDir, rest)
		if err := checkConfinedPath(layer.modFS, modRel); err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		fi, err := fs.Stat(layer.modFS, modRel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		if !fi.IsDir() {
			f, err := layer.modFS.Open(modRel)
			if err != nil {
				return nil, &fs.PathError{Op: "open", Path: name, Err: err}
			}
			return f, nil
		}
		// Highest-priority matching entry is a directory: merge all directory layers at rest.
		entries, err := mergeDirEntries(layers[:i+1], rest, name)
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: err}
		}
		return &virtualDirFile{
			info:    virtualDirInfo{name: path.Base(name), modTime: fi.ModTime()},
			entries: entries,
		}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (o *contentOverlayFS) Stat(name string) (fs.FileInfo, error) {
	if !validOverlayPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	snap := o.snapshot()
	if name == "." {
		return virtualDirInfo{name: "."}, nil
	}
	cat, rest, hasSlash := strings.Cut(name, "/")
	layers := snap.byCategory[cat]
	if len(layers) == 0 {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	if !hasSlash {
		return virtualDirInfo{name: cat}, nil
	}
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		modRel := path.Join(layer.contribDir, rest)
		if err := checkConfinedPath(layer.modFS, modRel); err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		fi, err := fs.Stat(layer.modFS, modRel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
		}
		return fi, nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (o *contentOverlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !validOverlayPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	snap := o.snapshot()
	if name == "." {
		return o.rootEntries(snap), nil
	}
	cat, rest, hasSlash := strings.Cut(name, "/")
	layers := snap.byCategory[cat]
	if len(layers) == 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	if !hasSlash {
		entries, err := mergeDirEntries(layers, "", name)
		if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
		return entries, nil
	}
	// Verify the highest-priority layer that owns rest is a directory.
	topDirIdx := -1
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		modRel := path.Join(layer.contribDir, rest)
		if err := checkConfinedPath(layer.modFS, modRel); err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
		fi, err := fs.Stat(layer.modFS, modRel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
		}
		if !fi.IsDir() {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
		}
		topDirIdx = i
		break
	}
	if topDirIdx < 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := mergeDirEntries(layers[:topDirIdx+1], rest, name)
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	return entries, nil
}

func (o *contentOverlayFS) rootEntries(snap *contentSnapshot) []fs.DirEntry {
	var out []fs.DirEntry
	for _, cat := range contentCategories {
		if len(snap.byCategory[cat]) > 0 {
			out = append(out, fs.FileInfoToDirEntry(virtualDirInfo{name: cat}))
		}
	}
	return out
}

// mergeDirEntries merges directory entries across layers in load order (later layers win on name collisions).
func mergeDirEntries(layers []contentLayer, rest, displayPath string) ([]fs.DirEntry, error) {
	byName := make(map[string]fs.DirEntry)
	for _, layer := range layers {
		modRel := layer.contribDir
		if rest != "" {
			modRel = path.Join(layer.contribDir, rest)
		}
		if err := checkConfinedPath(layer.modFS, modRel); err != nil {
			return nil, err
		}
		fi, err := fs.Stat(layer.modFS, modRel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !fi.IsDir() {
			continue
		}
		entries, err := fs.ReadDir(layer.modFS, modRel)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			childRel := path.Join(modRel, entry.Name())
			if err := checkConfinedPath(layer.modFS, childRel); err != nil {
				return nil, fmt.Errorf("%s/%s: %w", displayPath, entry.Name(), err)
			}
			byName[entry.Name()] = entry
		}
	}
	out := make([]fs.DirEntry, 0, len(byName))
	for _, entry := range byName {
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return out, nil
}

type virtualDirInfo struct {
	name    string
	modTime time.Time
}

func (d virtualDirInfo) Name() string       { return d.name }
func (d virtualDirInfo) Size() int64        { return 0 }
func (d virtualDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (d virtualDirInfo) ModTime() time.Time { return d.modTime }
func (d virtualDirInfo) IsDir() bool        { return true }
func (d virtualDirInfo) Sys() any           { return nil }

type virtualDirFile struct {
	info    virtualDirInfo
	entries []fs.DirEntry
	offset  int
}

func (f *virtualDirFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *virtualDirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: f.info.name, Err: errors.New("is a directory")}
}
func (f *virtualDirFile) Close() error { return nil }

func (f *virtualDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		if f.offset >= len(f.entries) {
			return nil, nil
		}
		out := slices.Clone(f.entries[f.offset:])
		f.offset = len(f.entries)
		return out, nil
	}
	if f.offset >= len(f.entries) {
		return nil, io.EOF
	}
	end := min(f.offset+n, len(f.entries))
	out := slices.Clone(f.entries[f.offset:end])
	f.offset = end
	return out, nil
}

// isAbsoluteLinkTarget reports whether target is an absolute POSIX or Windows path.
func isAbsoluteLinkTarget(target string) bool {
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "\\") {
		return true
	}
	if len(target) >= 2 && target[1] == ':' && ((target[0] >= 'a' && target[0] <= 'z') || (target[0] >= 'A' && target[0] <= 'Z')) {
		return true
	}
	return false
}

// checkConfinedPath verifies that rel inside modFS does not escape modFS through path syntax
// or symlinks (relative escaping symlinks, absolute symlinks, or symlink loops). If a path
// component does not exist, it returns nil so callers observe fs.ErrNotExist.
func checkConfinedPath(modFS fs.FS, rel string) error {
	if !fs.ValidPath(rel) {
		return fmt.Errorf("%w: path %q escapes mod directory", ErrUnsafePath, rel)
	}
	rlfs, ok := modFS.(fs.ReadLinkFS)
	if !ok || rel == "." {
		return nil
	}
	remaining := strings.Split(rel, "/")
	var resolved []string
	hops := 0
	for len(remaining) > 0 {
		seg := remaining[0]
		remaining = remaining[1:]
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".." {
			if len(resolved) == 0 {
				return fmt.Errorf("%w: path %q escapes mod directory", ErrUnsafePath, rel)
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		candidate := strings.Join(append(resolved, seg), "/")
		fi, err := rlfs.Lstat(candidate)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			hops++
			if hops > maxSymlinkHops {
				return fmt.Errorf("%w: symlink loop at %q", ErrUnsafePath, candidate)
			}
			target, err := rlfs.ReadLink(candidate)
			if err != nil {
				return err
			}
			if isAbsoluteLinkTarget(target) {
				return fmt.Errorf("%w: symlink %q points outside mod directory (%q)", ErrUnsafePath, candidate, target)
			}
			targetSlash := strings.ReplaceAll(target, "\\", "/")
			targetParts := strings.Split(targetSlash, "/")
			next := make([]string, 0, len(targetParts)+len(remaining))
			next = append(next, targetParts...)
			next = append(next, remaining...)
			remaining = next
			continue
		}
		resolved = append(resolved, seg)
	}
	return nil
}

// checkNoEscapingSymlinksInDir verifies that dir inside modFS exists, is a directory, and
// contains no symlinks that escape modFS.
func checkNoEscapingSymlinksInDir(modFS fs.FS, dir string) error {
	cleaned := path.Clean(dir)
	if err := checkConfinedPath(modFS, cleaned); err != nil {
		return err
	}
	fi, err := fs.Stat(modFS, cleaned)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%q is not a directory", dir)
	}
	rlfs, ok := modFS.(fs.ReadLinkFS)
	if !ok {
		return nil
	}
	return walkSymlinks(modFS, rlfs, cleaned)
}

func walkSymlinks(modFS fs.FS, rlfs fs.ReadLinkFS, dir string) error {
	entries, err := fs.ReadDir(modFS, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := path.Join(dir, entry.Name())
		linfo, err := rlfs.Lstat(child)
		if err != nil {
			return err
		}
		if linfo.Mode()&fs.ModeSymlink != 0 {
			if err := checkConfinedPath(modFS, child); err != nil {
				return err
			}
			continue
		}
		if linfo.IsDir() {
			if err := walkSymlinks(modFS, rlfs, child); err != nil {
				return err
			}
		}
	}
	return nil
}
