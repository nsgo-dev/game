package editor

import (
	"io/fs"
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
// The move is one undoable step: Undo moves the files back, rewriting the references again as a move from to to from does, and removes the directories the move created when they are empty; Redo moves them once more. The uids and the project index the move wrote stay, since they describe the files wherever they are. A step that something on disk now blocks, such as a new file where the move would put one, fails and stays where it was, and the status bar says why. MoveFile refuses while playing (ErrPlaying) or without a project (ErrNoProject), and changes nothing when the plan fails.
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
}

func (c *moveFileCmd) Label() string { return "Move " + c.from + " to " + c.to }

func (c *moveFileCmd) Do() { c.ed.report(c.do()) }

func (c *moveFileCmd) Undo() { c.ed.report(c.undo()) }

func (c *moveFileCmd) do() error {
	var made []string
	for d := path.Dir(c.to); d != "." && d != "/"; d = path.Dir(d) {
		if _, err := os.Lstat(filepath.Join(c.root, filepath.FromSlash(d))); err == nil {
			break
		}
		made = append(made, d)
	}
	slices.Reverse(made)
	if err := c.ed.moveFiles(c.root, c.from, c.to); err != nil {
		return err
	}
	c.made = made
	return nil
}

func (c *moveFileCmd) undo() error {
	if err := c.ed.moveFiles(c.root, c.to, c.from); err != nil {
		return err
	}
	// Directories the move created go again when the move back left them empty; one that now holds other files stays, and its removal fails harmlessly.
	for i := len(c.made) - 1; i >= 0; i-- {
		if err := os.Remove(filepath.Join(c.root, filepath.FromSlash(c.made[i]))); err != nil {
			break
		}
	}
	return nil
}

// moveFiles applies refs.PlanMove's plan to move from to to in the project at root, and moves the paths the editor holds with it.
func (e *Editor) moveFiles(root, from, to string) error {
	plan, err := refs.PlanMove(os.DirFS(root), from, to)
	if err != nil {
		return err
	}
	if err := plan.Apply(root); err != nil {
		return err
	}
	moved := func(p string) (string, bool) {
		if p == from {
			return to, true
		}
		if rest, ok := strings.CutPrefix(p, from+"/"); ok {
			return to + "/" + rest, true
		}
		return p, false
	}
	movePath := func(p *string) {
		if n, ok := moved(*p); ok {
			*p = n
		}
	}
	// An OS path inside the project follows the move too.
	moveFile := func(p *string) {
		if *p == "" {
			return
		}
		rel, err := filepath.Rel(root, *p)
		if err != nil {
			return
		}
		if n, ok := moved(filepath.ToSlash(rel)); ok {
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
	moveScenePaths(e.Doc, movePath)
	moveFile(&e.FilePath)
	movePath(&e.ScenePath)
	moveFile(&e.clipMeta.path)
	e.filesRev++
	e.Status = "Moved " + from + " to " + to
	return nil
}

// ProjectFile is a file or directory of the open project, as the file browser lists it.
type ProjectFile struct {
	// Path is the slash-separated path from the project root.
	Path string
	// Dir reports whether Path is a directory.
	Dir bool
}

// ProjectFiles lists the files and directories of the open project, the directory of ProjectPath, in the order fs.WalkDir visits them: by name, each directory followed by what it holds. It leaves out the files and directories whose names start with "." or "_" and the directories named testdata, as refs.Scan does. It returns ErrNoProject without a project.
func (e *Editor) ProjectFiles() ([]ProjectFile, error) {
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	var out []ProjectFile
	err := fs.WalkDir(os.DirFS(filepath.Dir(e.ProjectPath)), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || (d.IsDir() && name == "testdata") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		out = append(out, ProjectFile{Path: p, Dir: d.IsDir()})
		return nil
	})
	return out, err
}

// moveProjectPaths applies move to every path f holds to a file or folder of the project: the autoload entries and the assets section's targets, as refs rewrites them on disk.
func moveProjectPaths(f *project.File, move func(*string)) {
	if f == nil {
		return
	}
	f.EachPath(func(_ string, p *string) { move(p) })
}

// moveScenePaths applies move to every file path doc holds: the scene it inherits, tilemap sources, and the scenes and materials of objects and overrides.
func moveScenePaths(doc *sceneformat.SceneDoc, move func(*string)) {
	if doc == nil {
		return
	}
	move(&doc.Inherits)
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
			moveOverrides(o.Overrides, move)
			return true
		})
	}
}

func moveOverrides(ovs map[string]sceneformat.ObjectOverride, move func(*string)) {
	for id, ov := range ovs {
		if ov.Material != nil {
			m := *ov.Material
			move(&m)
			ov.Material = &m
			ovs[id] = ov
		}
	}
}
