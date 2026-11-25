package editor

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// MoveFile moves or renames the file or directory from to to, both slash-separated paths from the root of the open project (the directory of ProjectPath), and rewrites every reference to what it moves across the project, as refs.PlanMove plans and `game mv` does: in the scenes, resource files, game.yaml, Tiled and LDtk files on disk, and in the open scene, project and clip, which keep their unsaved edits. When the open scene, or the open clip's file, is among the files moved, FilePath, ScenePath and ClipPath follow it.
//
// The move is one undoable step. Undo moves the files back and puts back, byte for byte, each file the move rewrote, the project index included, and removes an index the move created and the directories it created when they are left empty; a file changed since the move has its references rewritten back as a move from to to from would, and a file the move left alone, such as one that already named to, stays as it is. Redo moves the files once more. A step that something on disk now blocks, such as a new file where the move would put one, fails and stays where it was, and the status bar says why. MoveFile refuses while playing (ErrPlaying) or without a project (ErrNoProject), and changes nothing when the plan fails.
func (e *Editor) MoveFile(from, to string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	return e.History.execute(&moveFileCmd{ed: e, from: from, to: to, root: filepath.Dir(e.ProjectPath)})
}

// moveFileCmd moves from to to in the project at root, and back.
type moveFileCmd struct {
	ed       *Editor
	from, to string
	root     string
	// made are the directories the last do created to move into, outermost first.
	made []string
	// written are the files the last do wrote, and untouched the files it left alone although they name to, by their paths after the move, with what they held then.
	written   []movedFile
	untouched map[string][]byte
}

// movedFile is a file a move wrote: its path before the move and after, what it held before (when it existed), and what the move wrote.
type movedFile struct {
	before, after string
	old           []byte
	existed       bool
	data          []byte
}

func (c *moveFileCmd) Label() string { return "Move " + c.from + " to " + c.to }

func (c *moveFileCmd) Do() { c.ed.report(c.do()) }

func (c *moveFileCmd) Undo() { c.ed.report(c.undo()) }

func (c *moveFileCmd) do() error {
	fsys := os.DirFS(c.root)
	plan, err := refs.PlanMove(fsys, c.from, c.to)
	if err != nil {
		return err
	}
	back := movedPath(c.to, c.from)
	written := make([]movedFile, 0, len(plan.Writes))
	wrote := make(map[string]bool, len(plan.Writes))
	for _, w := range plan.Writes {
		f := movedFile{before: back(w.Path), after: w.Path, data: w.Data}
		f.old, err = fs.ReadFile(fsys, f.before)
		switch {
		case err == nil:
			f.existed = true
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		written = append(written, f)
		wrote[f.before] = true
	}
	// A file that already names to, a reference to nothing until the move, is one the move leaves alone and its undo must too.
	untouched := make(map[string][]byte)
	if p, err := refs.Scan(fsys); err == nil {
		forth := movedPath(c.from, c.to)
		for _, r := range p.Refs {
			if wrote[r.File] || (r.Target != c.to && !strings.HasPrefix(r.Target, c.to+"/")) {
				continue
			}
			if data, err := fs.ReadFile(fsys, r.File); err == nil {
				untouched[forth(r.File)] = data
			}
		}
	}

	var made []string
	for d := path.Dir(c.to); d != "." && d != "/"; d = path.Dir(d) {
		if _, err := os.Lstat(filepath.Join(c.root, filepath.FromSlash(d))); err == nil {
			break
		}
		made = append(made, d)
	}
	slices.Reverse(made)
	if err := plan.Apply(c.root); err != nil {
		c.removeEmpty(made)
		return err
	}
	c.made, c.written, c.untouched = made, written, untouched
	c.ed.followMove(c.root, c.from, c.to, plan.Writes)
	return nil
}

func (c *moveFileCmd) undo() error {
	fsys := os.DirFS(c.root)
	plan, err := refs.PlanMove(fsys, c.to, c.from)
	if err != nil {
		return err
	}
	writes := make(map[string][]byte, len(plan.Writes))
	for _, w := range plan.Writes {
		writes[w.Path] = w.Data
	}
	back := movedPath(c.to, c.from)
	for after, data := range c.untouched {
		if cur, err := fs.ReadFile(fsys, after); err == nil && bytes.Equal(cur, data) {
			delete(writes, back(after))
		}
	}
	// A file still as the move wrote it gets back what it held; one changed since keeps the rewrite back that the reverse plan made.
	var remove []string
	for _, f := range c.written {
		if cur, err := fs.ReadFile(fsys, f.after); err != nil || !bytes.Equal(cur, f.data) {
			continue
		}
		if f.existed {
			writes[f.before] = f.old
		} else {
			delete(writes, f.before)
			remove = append(remove, f.before)
		}
	}
	plan.Writes = plan.Writes[:0]
	for _, p := range slices.Sorted(maps.Keys(writes)) {
		plan.Writes = append(plan.Writes, refs.Write{Path: p, Data: writes[p]})
	}
	if err := plan.Apply(c.root); err != nil {
		return err
	}
	for _, p := range remove {
		if err := os.Remove(filepath.Join(c.root, filepath.FromSlash(p))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	c.removeEmpty(c.made)
	c.ed.followMove(c.root, c.to, c.from, plan.Writes)
	return nil
}

// removeEmpty removes the directories made, outermost first, from the innermost out, stopping at the first that something else now fills.
func (c *moveFileCmd) removeEmpty(made []string) {
	for i := len(made) - 1; i >= 0; i-- {
		if err := os.Remove(filepath.Join(c.root, filepath.FromSlash(made[i]))); err != nil {
			return
		}
	}
}

// movedPath returns the function that maps a slash-separated project path to where it is once from moves to to.
func movedPath(from, to string) func(string) string {
	return func(p string) string {
		if p == from {
			return to
		}
		if rest, ok := strings.CutPrefix(p, from+"/"); ok {
			return to + "/" + rest
		}
		return p
	}
}

// followMove moves the paths the editor holds as the files moved from from to to in the project at root, where the move wrote the files written, at their paths after it.
func (e *Editor) followMove(root, from, to string, written []refs.Write) {
	forth := movedPath(from, to)
	movePath := func(p *string) { *p = forth(*p) }
	// An OS path inside the project follows the move too.
	moveFile := func(p *string) {
		if *p == "" {
			return
		}
		rel, err := filepath.Rel(root, *p)
		if err != nil {
			return
		}
		if n := forth(filepath.ToSlash(rel)); n != filepath.ToSlash(rel) {
			*p = filepath.Join(root, filepath.FromSlash(n))
		}
	}

	moveProjectPaths(e.Project, movePath)
	if saved, err := project.Parse(e.projectSaved); err == nil {
		moveProjectPaths(saved, movePath)
		if data, err := project.Encode(saved); err == nil {
			e.projectSaved = data
		}
	}
	e.syncActiveTab()
	for _, t := range e.tabs {
		moveScenePaths(t.doc, movePath)
		moveFile(&t.filePath)
		// When the move rewrote an open scene's file, the saved scene changed on disk as the open one did, so the move leaves it as clean or dirty as it was. A file the move did not write, such as one refs does not read as a scene, or a scene kept through OnSave or outside the project, still holds the old paths, so the scene is left dirty for the author to save.
		if rel, ok := e.projectRel(t.filePath); ok && slices.ContainsFunc(written, func(w refs.Write) bool { return w.Path == rel }) {
			if saved, err := sceneformat.Parse(t.sceneSaved); err == nil {
				moveScenePaths(saved, movePath)
				if data, err := sceneformat.Encode(saved); err == nil {
					t.sceneSaved = data
				}
			}
		}
		movePath(&t.scenePath)
	}
	e.loadActiveTab()
	e.syncProjectAssets()
	e.scenesRev++
	e.baseLayers = baseLayerCache{}
	e.rebuildAllInactiveTabs()
	moveFile(&e.clipMeta.path)
	e.followDataTableMove(moveFile)
	e.followGridMapMove(moveFile, movePath)
	e.followParticlesMove(moveFile)
	e.followRigMove(moveFile)
	e.filesRev++
	e.assetsRev++
	e.followSearchMove(forth)
	e.Status = "Moved " + from + " to " + to
	e.CheckProblems()
}

// ProjectFile is a file or directory of the open project, as the file browser lists it.
type ProjectFile struct {
	// Path is the slash-separated path from the project root.
	Path string
	// Dir reports whether Path is a directory.
	Dir bool
}

// ProjectFiles lists the files and directories of the open project, the directory of ProjectPath, in the order fs.WalkDir visits them: by name, each directory followed by what it holds. It leaves out the directories refs.Scan skips, those whose names start with "." or "_" and those named testdata, with what they hold, and like Scan it passes over a directory below the root that it cannot read. It returns ErrNoProject without a project.
func (e *Editor) ProjectFiles() ([]ProjectFile, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	var out []ProjectFile
	err := fs.WalkDir(os.DirFS(filepath.Dir(e.ProjectPath)), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." {
			return nil
		}
		if d.IsDir() && skippedProjectDir(d.Name()) {
			return fs.SkipDir
		}
		out = append(out, ProjectFile{Path: p, Dir: d.IsDir()})
		return nil
	})
	return out, err
}

// skippedProjectDir reports whether ProjectFiles leaves out a directory named name, with what it holds, as refs.Scan does: one whose name starts with "." or "_", or one named testdata.
func skippedProjectDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

// moveProjectPaths applies move to every path f holds to a file or folder of the project: the autoload entries and the assets section's targets, as refs rewrites them on disk.
func moveProjectPaths(f *project.File, move func(*string)) {
	if f == nil {
		return
	}
	f.EachPath(func(_ string, p *string) { move(p) })
}

// moveScenePaths applies move to every file path doc holds: the scene it inherits, its navigation mesh file, tilemap sources, and the scenes, materials and models of objects and the materials and models of overrides.
func moveScenePaths(doc *sceneformat.SceneDoc, move func(*string)) {
	if doc == nil {
		return
	}
	move(&doc.Inherits)
	move(&doc.NavMesh)
	for _, tm := range doc.Tilemaps {
		if tm != nil {
			move(&tm.Source)
		}
	}
	for name, lo := range doc.LayerOverrides {
		if lo.Tilemap != nil {
			move(&lo.Tilemap.Source)
			doc.LayerOverrides[name] = lo
		}
	}
	moveOverrides(doc.Overrides, move)
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.Tilemap != nil {
			move(&l.Tilemap.Source)
		}
		sceneformat.WalkObjects(l.Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			move(&o.Scene)
			move(&o.Material)
			move(&o.Model)
			move(&o.GridMap)
			if o.Particles != nil {
				move(&o.Particles.File)
			}
			moveSkeletalComponentPath(o.Components, move)
			moveOverrides(o.Overrides, move)
			return true
		})
	}
}

func moveSkeletalComponentPath(comps map[string]any, move func(*string)) {
	if comps == nil {
		return
	}
	if m, ok := comps[sceneformat.SkeletalComponentName].(map[string]any); ok {
		if rig, ok := m["rig"].(string); ok && rig != "" {
			move(&rig)
			m["rig"] = rig
		}
	}
}

// moveOverrides applies move to the material, model, gridMap, and skeletal rig paths of every override in ovs.
func moveOverrides(ovs map[string]sceneformat.ObjectOverride, move func(*string)) {
	for id, ov := range ovs {
		for _, p := range []**string{&ov.Material, &ov.Model, &ov.GridMap} {
			if *p != nil {
				v := **p
				move(&v)
				*p = &v
			}
		}
		if ov.Particles != nil {
			cp := *ov.Particles
			move(&cp.File)
			ov.Particles = &cp
		}
		moveSkeletalComponentPath(ov.Components, move)
		ovs[id] = ov
	}
}
