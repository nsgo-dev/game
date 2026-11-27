package library

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
)

// Change is what installing or removing a pack does to a project, worked out by PlanInstall or PlanRemove and carried out by Apply.
type Change struct {
	// Project is the project file afterwards.
	Project *project.File
	// Pack is the pack's record: the one Project holds after an install, or the one it held before a removal.
	Pack project.Pack
	// Write maps each file to write, by its slash-separated path from the project root, to its content.
	Write map[string][]byte
	// Remove lists the files to remove, by their slash-separated paths from the project root, sorted.
	Remove []string

	// unchanged is whether Project encodes as the project file before did.
	unchanged bool
	// cleanTop is the directory a removal deletes the emptied directories up to and including (cleanBound), or empty to delete none.
	cleanTop string
}

// NoOp reports whether applying c would change nothing: no file to write or remove, and the same project file.
func (c *Change) NoOp() bool { return c.unchanged && len(c.Write) == 0 && len(c.Remove) == 0 }

// PlanInstall works out what installing p into the project at the root of fsys, whose project file is f, does, without writing anything. The pack's files go under dir; when dir is empty, an update keeps them in the directory the installed version went to, and a first install puts them in the manifest's into or DefaultDir. The directory must stay inside the project, and none of its elements may be hidden (start with '.') or be a name some platform cannot hold.
//
// When f already records the pack, the install updates it: the files of the old version the new one lacks are removed, the others replaced, and the autoload entries and plugins it added are replaced by the new version's, keeping their places. A file the project already has is refused unless the old version wrote it or it has the content the pack would write, and so is a file another pack wrote; an autoload scene or resource of the pack's name that the project already has is refused too. A plugin the project already turns on is left as it is and not recorded as the pack's.
//
// f is not changed; the returned Change's Project is a validated copy.
func PlanInstall(fsys fs.FS, f *project.File, p *Pack, dir string) (*Change, error) {
	m := p.Manifest
	if err := ValidateManifest(m); err != nil {
		return nil, err
	}
	at := slices.IndexFunc(f.Packs, func(r project.Pack) bool { return r.Name == m.Name })
	if dir == "" && at >= 0 {
		dir = f.Packs[at].Dir
	}
	if dir == "" {
		dir = m.Into
	}
	if dir == "" {
		dir = DefaultDir(m.Name)
	}
	if err := validateInstallDir(dir); err != nil {
		return nil, err
	}
	names := p.Names()
	if !slices.Equal(names, manifestFiles(m)) {
		return nil, fmt.Errorf("library: pack %s: its files are not those its manifest lists", m.Name)
	}
	before, err := project.Encode(f)
	if err != nil {
		return nil, err
	}
	next := f.Clone()
	c := &Change{Project: next, Write: map[string][]byte{}}

	var oldPaths map[string]bool
	var old project.Pack
	if at >= 0 {
		old = next.Packs[at]
		oldPaths = make(map[string]bool, len(old.Files))
		for _, p := range old.Paths() {
			oldPaths[p] = true
		}
		c.cleanTop = cleanBound(old)
	}
	// others maps each file another pack wrote, folded to lower case as a case-insensitive file system sees it, to that pack.
	others := map[string]string{}
	for _, r := range next.Packs {
		if r.Name != m.Name {
			for _, p := range r.Paths() {
				others[strings.ToLower(p)] = r.Name
			}
		}
	}

	rec := project.Pack{Name: m.Name, Kind: m.Kind, PackVersion: m.PackVersion, SHA256: p.Digest(), Dir: dir, Files: names}
	newPaths := make(map[string]bool, len(names))
	for _, name := range names {
		dest := path.Join(dir, name)
		newPaths[dest] = true
		if owner, ok := others[strings.ToLower(dest)]; ok {
			return nil, fmt.Errorf("library: pack %s would write %s, which pack %s wrote", m.Name, dest, owner)
		}
		existing, err := fs.ReadFile(fsys, dest)
		switch {
		case err == nil && bytes.Equal(existing, p.Files[name]):
		case err == nil && oldPaths[dest]:
			c.Write[dest] = p.Files[name]
		case err == nil:
			return nil, fmt.Errorf("library: pack %s would overwrite %s, which the project already has; move it away to install the pack", m.Name, dest)
		case errors.Is(err, fs.ErrNotExist):
			c.Write[dest] = p.Files[name]
		default:
			blocked, berr := blockedByOld(fsys, dest, oldPaths)
			if berr != nil {
				return nil, fmt.Errorf("library: pack %s: %w", m.Name, berr)
			}
			if !blocked {
				return nil, fmt.Errorf("library: pack %s: %w", m.Name, err)
			}
			// The old version's file is where the new one has a directory, or the other way round; ApplyFiles removes the old before it writes the new.
			c.Write[dest] = p.Files[name]
		}
	}
	for _, p := range old.Paths() {
		if newPaths[p] {
			continue
		}
		if _, err := fs.Stat(fsys, p); err == nil {
			c.Remove = append(c.Remove, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("library: pack %s: %w", m.Name, err)
		}
	}
	slices.Sort(c.Remove)

	var scenes, resources []project.Entry
	if a := m.Autoload; a != nil {
		for _, e := range a.Scenes {
			scenes = append(scenes, project.Entry{Name: e.Name, Path: path.Join(dir, e.Path)})
		}
		for _, e := range a.Resources {
			resources = append(resources, project.Entry{Name: e.Name, Path: path.Join(dir, e.Path)})
		}
	}
	if err := relink(next, old, scenes, resources, m.Plugins, &rec); err != nil {
		return nil, fmt.Errorf("library: pack %s: %w", m.Name, err)
	}
	if at >= 0 {
		next.Packs[at] = rec
	} else {
		next.Packs = append(next.Packs, rec)
	}
	if err := project.Validate(next); err != nil {
		return nil, fmt.Errorf("library: pack %s: %w", m.Name, err)
	}
	c.Pack = rec
	return c, c.finish(before)
}

// PlanRemove works out what removing the installed pack called name from the project at the root of fsys, whose project file is f, does, without writing anything: its files are removed, edited or not, and so are the autoload entries and editor plugins it added. A main scene that was the pack's is unset.
func PlanRemove(fsys fs.FS, f *project.File, name string) (*Change, error) {
	before, err := project.Encode(f)
	if err != nil {
		return nil, err
	}
	next := f.Clone()
	at := slices.IndexFunc(next.Packs, func(r project.Pack) bool { return r.Name == name })
	if at < 0 {
		return nil, fmt.Errorf("library: the project has no pack %q installed", name)
	}
	old := next.Packs[at]
	c := &Change{Project: next, Pack: old, cleanTop: cleanBound(old)}
	for _, p := range old.Paths() {
		if _, err := fs.Stat(fsys, p); err == nil {
			c.Remove = append(c.Remove, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("library: pack %s: %w", name, err)
		}
	}
	slices.Sort(c.Remove)
	if err := relink(next, old, nil, nil, nil, &project.Pack{}); err != nil {
		return nil, fmt.Errorf("library: pack %s: %w", name, err)
	}
	next.Packs = slices.Delete(next.Packs, at, at+1)
	if len(next.Packs) == 0 {
		next.Packs = nil
	}
	if err := project.Validate(next); err != nil {
		return nil, fmt.Errorf("library: pack %s: %w", name, err)
	}
	return c, c.finish(before)
}

// blockedByOld reports whether dest, which cannot be read as a file, is blocked only by files of the old version of the pack, recorded in oldPaths, that the update removes: one of its ancestors is such a file, or it is a directory holding only such files.
func blockedByOld(fsys fs.FS, dest string, oldPaths map[string]bool) (bool, error) {
	for d := path.Dir(dest); d != "."; d = path.Dir(d) {
		if oldPaths[d] {
			return true, nil
		}
	}
	info, err := fs.Stat(fsys, dest)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	blocked := true
	err = fs.WalkDir(fsys, dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !oldPaths[p] {
			blocked = false
			return fs.SkipAll
		}
		return nil
	})
	return blocked, err
}

// cleanBound returns the directory removing the files of the installed pack r deletes emptied directories up to, and including: r.Dir, or packs for a pack in its DefaultDir. A directory above it, which the project may have had before the install, is kept even when empty.
func cleanBound(r project.Pack) string {
	if r.Dir == DefaultDir(r.Name) {
		return path.Dir(r.Dir)
	}
	return r.Dir
}

// within reports whether the slash-separated path p is dir or inside it.
func within(p, dir string) bool {
	return dir != "" && (p == dir || strings.HasPrefix(p, dir+"/"))
}

// finish records whether c's project encodes as before did.
func (c *Change) finish(before []byte) error {
	after, err := project.Encode(c.Project)
	if err != nil {
		return err
	}
	c.unchanged = bytes.Equal(before, after)
	return nil
}

// manifestFiles returns the files a pack with manifest m holds, pack.yaml included, sorted.
func manifestFiles(m *Manifest) []string {
	names := append([]string{ManifestFileName}, m.Files...)
	slices.Sort(names)
	return names
}

// relink replaces in f the autoload entries and editor plugins that old, the pack's record before, added with scenes, resources and plugins, recording in rec those it added. An entry of the old version that the new one has again keeps its place; the rest of the new ones are appended. An autoload section or editor section left empty by a removal is dropped.
func relink(f *project.File, old project.Pack, scenes, resources []project.Entry, plugins []string, rec *project.Pack) error {
	hadAutoload := f.Autoload != nil && (len(f.Autoload.Scenes) > 0 || len(f.Autoload.Resources) > 0)
	if f.Autoload == nil && (len(scenes) > 0 || len(resources) > 0) {
		f.Autoload = &project.Autoload{}
	}
	if a := f.Autoload; a != nil {
		var err error
		if a.Scenes, rec.Scenes, err = relinkEntries("scene", a.Scenes, old.Scenes, scenes); err != nil {
			return err
		}
		if a.Resources, rec.Resources, err = relinkEntries("resource", a.Resources, old.Resources, resources); err != nil {
			return err
		}
		if a.MainScene != "" && !slices.ContainsFunc(a.Scenes, func(e project.Entry) bool { return e.Name == a.MainScene }) {
			a.MainScene = ""
		}
		if hadAutoload && len(a.Scenes) == 0 && len(a.Resources) == 0 && a.MainScene == "" {
			f.Autoload = nil
		}
	}
	hadPlugins := f.Editor != nil && len(f.Editor.Plugins) > 0
	if f.Editor == nil && len(plugins) > 0 {
		f.Editor = &project.Editor{}
	}
	if e := f.Editor; e != nil {
		var kept []string
		for _, p := range e.Plugins {
			// A plugin the old version turned on goes when the new one does not.
			if !slices.Contains(old.Plugins, p) || slices.Contains(plugins, p) {
				kept = append(kept, p)
			}
		}
		for _, p := range plugins {
			switch {
			case !slices.Contains(kept, p):
				kept = append(kept, p)
				rec.Plugins = append(rec.Plugins, p)
			case slices.Contains(old.Plugins, p):
				rec.Plugins = append(rec.Plugins, p)
			}
		}
		e.Plugins = kept
		if hadPlugins && len(kept) == 0 {
			f.Editor = nil
		}
	}
	return nil
}

// relinkEntries returns list with the entries named in old replaced by add: an entry add has again keeps its place, the rest of add is appended, and the names of add are returned to record. An entry of list that add names and old does not is refused.
func relinkEntries(kind string, list []project.Entry, old []string, add []project.Entry) ([]project.Entry, []string, error) {
	pending := make(map[string]project.Entry, len(add))
	for _, e := range add {
		pending[e.Name] = e
	}
	var out []project.Entry
	for _, e := range list {
		if slices.Contains(old, e.Name) {
			if n, ok := pending[e.Name]; ok {
				out = append(out, n)
				delete(pending, e.Name)
			}
			continue
		}
		if _, clash := pending[e.Name]; clash {
			return nil, nil, fmt.Errorf("game.yaml already autoloads a %s called %q", kind, e.Name)
		}
		out = append(out, e)
	}
	var added []string
	for _, e := range add {
		if _, ok := pending[e.Name]; ok {
			out = append(out, e)
		}
		added = append(added, e.Name)
	}
	return out, added, nil
}

// Apply carries out c in the project directory projectDir: it removes and writes c's files, deleting the directories a removal leaves empty inside the pack's directory (and packs, for a pack in its DefaultDir), then writes c.Project to game.yaml in the canonical form project.Encode writes, which keeps no comments. When a step fails, it restores the files it had changed and returns the error.
func (c *Change) Apply(projectDir string) error {
	if c.NoOp() {
		return nil
	}
	undo, err := c.ApplyFiles(projectDir)
	if err != nil {
		return err
	}
	if !c.unchanged {
		data, err := project.Encode(c.Project)
		if err == nil {
			err = writeFile(projectDir, project.FileName, data)
		}
		if err != nil {
			return errors.Join(err, undo())
		}
	}
	return nil
}

// ApplyFiles removes and writes c's files in the project directory projectDir, leaving game.yaml to the caller, such as an editor that saves its open project. It removes first, so that an update may put a file where the old version had a directory or the other way round, and deletes the directories a removal leaves empty inside the pack's directory (and packs, for a pack in its DefaultDir), never above it. It returns undo, which restores every file it changed, for when saving the project then fails. When ApplyFiles itself fails, it has already restored them.
func (c *Change) ApplyFiles(projectDir string) (undo func() error, err error) {
	root, err := os.OpenRoot(projectDir)
	if err != nil {
		return nil, fmt.Errorf("library: %w", err)
	}
	defer root.Close()

	type saved struct {
		name string
		data []byte
	}
	// removed holds the files removed, with their content; written holds the files written, with their content before when they existed.
	var removed, written []saved
	var existed []bool
	var madeDirs []string
	undo = func() error {
		r, err := os.OpenRoot(projectDir)
		if err != nil {
			return err
		}
		defer r.Close()
		var errs []error
		restore := func(s saved) {
			if err := r.MkdirAll(path.Dir(s.name), 0o755); err != nil {
				errs = append(errs, err)
				return
			}
			errs = append(errs, writeRoot(r, s.name, s.data))
		}
		for i, s := range slices.Backward(written) {
			if existed[i] {
				restore(s)
			} else if err := r.Remove(s.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		for _, d := range slices.Backward(madeDirs) {
			_ = r.Remove(d)
		}
		for _, s := range slices.Backward(removed) {
			restore(s)
		}
		return errors.Join(errs...)
	}
	fail := func(err error) (func() error, error) {
		return nil, errors.Join(fmt.Errorf("library: %w", err), undo())
	}

	for _, name := range c.Remove {
		prev, err := root.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fail(err)
		}
		removed = append(removed, saved{name, prev})
		if err := root.Remove(name); err != nil {
			return fail(err)
		}
		// Delete the directories the removal emptied, up to the pack's own; Remove fails on one that is not empty.
		for d := path.Dir(name); within(d, c.cleanTop); d = path.Dir(d) {
			if root.Remove(d) != nil {
				break
			}
		}
	}

	names := make([]string, 0, len(c.Write))
	for name := range c.Write {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		prev, err := root.ReadFile(name)
		ok := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fail(err)
		}
		var missing []string
		for d := path.Dir(name); d != "."; d = path.Dir(d) {
			if _, err := root.Stat(d); err != nil {
				missing = append(missing, d)
			}
		}
		if err := root.MkdirAll(path.Dir(name), 0o755); err != nil {
			return fail(err)
		}
		// missing runs from the deepest directory up; madeDirs runs down, so undo removes the deepest first.
		for _, d := range slices.Backward(missing) {
			madeDirs = append(madeDirs, d)
		}
		written = append(written, saved{name, prev})
		existed = append(existed, ok)
		if err := writeRoot(root, name, c.Write[name]); err != nil {
			return fail(err)
		}
	}
	return undo, nil
}

// writeFile writes data to name in the directory dir through a temporary file renamed over it, so a reader never sees half a file.
func writeFile(dir, name string, data []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	defer root.Close()
	if err := writeRoot(root, name, data); err != nil {
		return fmt.Errorf("library: %s: %w", filepath.Join(dir, filepath.FromSlash(name)), err)
	}
	return nil
}

// writeRoot writes data to name in root through a temporary file beside it renamed over it.
func writeRoot(root *os.Root, name string, data []byte) error {
	tmp := path.Join(path.Dir(name), "."+path.Base(name)+".library-tmp")
	_ = root.Remove(tmp)
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if err := errors.Join(werr, f.Close()); err != nil {
		return errors.Join(err, root.Remove(tmp))
	}
	if err := root.Rename(tmp, name); err != nil {
		return errors.Join(err, root.Remove(tmp))
	}
	return nil
}

// Install installs p into the Go project in projectDir, whose game.yaml it reads, as PlanInstall and Change.Apply describe, and returns what it did. An install that changes nothing, such as the same release of a pack installed again, writes nothing.
func Install(projectDir string, p *Pack, dir string) (*Change, error) {
	f, err := readProject(projectDir)
	if err != nil {
		return nil, err
	}
	c, err := PlanInstall(os.DirFS(projectDir), f, p, dir)
	if err != nil {
		return nil, err
	}
	return c, c.Apply(projectDir)
}

// Remove removes the installed pack called name from the project in projectDir, as PlanRemove and Change.Apply describe, and returns what it did.
func Remove(projectDir, name string) (*Change, error) {
	f, err := readProject(projectDir)
	if err != nil {
		return nil, err
	}
	c, err := PlanRemove(os.DirFS(projectDir), f, name)
	if err != nil {
		return nil, err
	}
	return c, c.Apply(projectDir)
}

// readProject reads and parses the game.yaml in dir with project.ReadFS.
func readProject(dir string) (*project.File, error) {
	f, err := project.ReadFS(os.DirFS(dir))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, project.FileName), err)
	}
	return f, nil
}
