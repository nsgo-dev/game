package editor

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// MoveFile moves or renames the file or directory from to to, both slash-separated paths from the root of the open project (the directory of ProjectPath), and rewrites every reference to what it moves across the project, as refs.PlanMove plans and `game mv` does: in the scenes, resource files, game.yaml, Tiled and LDtk files on disk, and in the open scene, project and clip, which keep their unsaved edits. When the open scene, or the open clip's file, is among the files moved, FilePath, ScenePath and ClipPath follow it. The move cannot be undone in the editor, so it empties the undo history, whose steps hold the old paths. It refuses while playing (ErrPlaying) or without a project (ErrNoProject), and changes nothing when the plan fails.
func (e *Editor) MoveFile(from, to string) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	root := filepath.Dir(e.ProjectPath)
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

	e.Status = "Moved " + from + " to " + to
	// Clear rebuilds the scene and notifies listeners.
	e.History.Clear()
	return nil
}

// moveProjectPaths applies move to the path of every autoload entry of f.
func moveProjectPaths(f *project.File, move func(*string)) {
	if f == nil || f.Autoload == nil {
		return
	}
	for i := range f.Autoload.Scenes {
		move(&f.Autoload.Scenes[i].Path)
	}
	for i := range f.Autoload.Resources {
		move(&f.Autoload.Resources[i].Path)
	}
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
