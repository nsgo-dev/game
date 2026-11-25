package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/project"
)

// ErrNoProject is returned by EditProject and SaveProject before OpenProject has opened a game.yaml.
var ErrNoProject = errors.New("editor: no project open")

// OpenProject opens the game's project file: path is a game.yaml or the directory holding one. When the project names a main scene (project.Autoload.MainScene), it opens that scene too, as OpenFile does. It empties the undo history either way, so no step can bring back a project opened before. The registered plugins the project names under editor.plugins turn on, and the status bar names those it lists that this editor was built without (MissingPlugins). On an error it leaves the editor as it was.
func (e *Editor) OpenProject(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, project.FileName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := project.Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	saved, err := project.Encode(f)
	if err != nil {
		return err
	}
	if a := f.Autoload; a != nil && a.MainScene != "" {
		for _, s := range a.Scenes {
			if s.Name == a.MainScene {
				if err := e.OpenFile(filepath.Join(filepath.Dir(path), filepath.FromSlash(s.Path))); err != nil {
					return fmt.Errorf("editor: main scene %q: %w", s.Name, err)
				}
			}
		}
	}
	e.stopFetch()
	e.stopAudioAudition()
	e.stopExportBuild()
	e.resetInputMapTransient()
	e.Project = f
	e.ProjectPath = path
	e.projectSaved = saved
	e.projectSavedOnce = false
	e.selectFile("")
	e.resetSearchForProject()
	e.filesRev++
	e.History.Clear()
	e.dropStalePluginTool()
	e.syncAudioMixer()
	e.resetExportPanelForProject()
	e.Status = "Opened project " + path
	if missing := e.MissingPlugins(); len(missing) > 0 {
		e.Status += "; plugins not built into this editor: " + strings.Join(missing, ", ")
	}
	e.notify()
	e.CheckProblems()
	return nil
}

// EditProject applies edit to a copy of the open project file and, when the result passes project.Validate, makes it the project as one undoable step labeled label. An edit Validate refuses changes nothing and returns its error; so does any edit while playing (ErrPlaying) or without a project (ErrNoProject).
func (e *Editor) EditProject(label string, edit func(f *project.File)) error {
	if e.playing {
		return ErrPlaying
	}
	if e.Project == nil {
		return ErrNoProject
	}
	next := e.Project.Clone()
	edit(next)
	if err := project.Validate(next); err != nil {
		return err
	}
	e.History.Execute(&projectCmd{ed: e, label: label, before: e.Project.Clone(), after: next})
	return nil
}

// ProjectDirty reports whether the open project differs from the game.yaml it was opened from or last saved to.
func (e *Editor) ProjectDirty() bool {
	if e.Project == nil {
		return false
	}
	data, err := project.Encode(e.Project)
	return err != nil || !bytes.Equal(data, e.projectSaved)
}

// SaveProject writes the open project to ProjectPath in the canonical form project.Encode writes, which keeps no comments, and writes the fields game.yaml owns into the nsgo.yaml beside it when there is one (project.File.SyncNSGo), so the two never disagree.
func (e *Editor) SaveProject() error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	data, err := project.Encode(e.Project)
	if err != nil {
		return err
	}
	nsgoPath := filepath.Join(filepath.Dir(e.ProjectPath), project.NSGoFileName)
	var nsgo []byte
	old, err := os.ReadFile(nsgoPath)
	switch {
	case err == nil:
		if nsgo, err = e.Project.SyncNSGo(old); err != nil {
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := writeFileAtomic(e.ProjectPath, data); err != nil {
		return err
	}
	if nsgo != nil && !bytes.Equal(nsgo, old) {
		if err := writeFileAtomic(nsgoPath, nsgo); err != nil {
			return err
		}
	}
	e.projectSaved = data
	e.projectSavedOnce = true
	e.invalidateProjectIndex()
	e.Status = "Saved project " + e.ProjectPath
	e.notify()
	e.CheckProblems()
	return nil
}

// projectCmd replaces the project file with after, and back with before.
type projectCmd struct {
	ed            *Editor
	label         string
	before, after *project.File
}

func (c *projectCmd) Label() string { return c.label }

func (c *projectCmd) Do() { c.ed.Project = c.after.Clone() }

func (c *projectCmd) Undo() { c.ed.Project = c.before.Clone() }
