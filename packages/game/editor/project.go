package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ErrNoProject is returned by EditProject and SaveProject before OpenProject has opened a game.yaml.
var ErrNoProject = errors.New("editor: no project open")

// OpenProject opens the game's project file: path is a game.yaml or the directory holding one. When the project names a main scene (project.Autoload.MainScene), it opens that scene too, as OpenFile does, with the view the project's ViewStateFile keeps for it. It empties the undo history either way, so no step can bring back a project opened before. The registered plugins the project names under editor.plugins turn on, and the status bar names those it lists that this editor was built without (MissingPlugins). With ProjectAssets set, Resolver becomes a project.Resolver over the project's directory before the scene opens, and a scene left open when the project names no main scene is built again against it. On an error it leaves the editor as it was.
func (e *Editor) OpenProject(path string) error {
	return e.openProject(path, "", false)
}

// OpenProjectScene opens the project file at path, a game.yaml or the directory holding one, as OpenProject does, and the scene file at scene, an OS path such as one inside the project's directory, in place of the project's main scene. A scene inside the project's directory gets its path from the project root as ScenePath, which Resolver names scene files by, so PlaceScene refuses an instance that leads back to it. On an error it leaves the editor as it was.
func (e *Editor) OpenProjectScene(path, scene string) error {
	return e.openProject(path, scene, true)
}

// openProject opens the project file at path and, when withScene is set, the scene file at scene, or else the project's main scene when it names one.
func (e *Editor) openProject(path, scene string, withScene bool) error {
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
	var openedDoc *sceneformat.SceneDoc
	var openedSaved []byte
	var openedPath string
	opened := false
	conflictedScene := ""
	switch a := f.Autoload; {
	case withScene:
		raw, err := os.ReadFile(scene)
		if err != nil {
			return loaderr.Locate(err, scene, nil, "check that the scene exists at this path and can be read")
		}
		doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
			File:          scene,
			Components:    e.components,
			PhysicsLayers: f.PhysicsLayerTable(),
		})
		if err != nil {
			if isSceneConflictFile(raw) {
				conflictedScene = scene
			} else {
				return loaderr.WithFile(err, scene)
			}
		} else {
			enc, err := sceneformat.Encode(doc)
			if err != nil {
				return loaderr.WithFile(err, scene)
			}
			openedDoc, openedSaved, openedPath, opened = doc, enc, scene, true
		}
	case a != nil && a.MainScene != "":
		for _, s := range a.Scenes {
			if s.Name == a.MainScene {
				sceneFile := filepath.Join(filepath.Dir(path), filepath.FromSlash(s.Path))
				raw, err := os.ReadFile(sceneFile)
				if err != nil {
					return fmt.Errorf("editor: main scene %q: %w", s.Name, loaderr.Locate(err, sceneFile, nil, "check that the scene exists at this path and can be read"))
				}
				doc, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{
					File:          sceneFile,
					Components:    e.components,
					PhysicsLayers: f.PhysicsLayerTable(),
				})
				if err != nil {
					if isSceneConflictFile(raw) {
						conflictedScene = sceneFile
					} else {
						return fmt.Errorf("editor: main scene %q: %w", s.Name, loaderr.WithFile(err, sceneFile))
					}
				} else {
					enc, err := sceneformat.Encode(doc)
					if err != nil {
						return fmt.Errorf("editor: main scene %q: %w", s.Name, loaderr.WithFile(err, sceneFile))
					}
					openedDoc, openedSaved, openedPath, opened = doc, enc, sceneFile, true
				}
				break
			}
		}
	}
	// Save the views of the project closing before replacing tabs.
	e.saveViewStateQuietly()
	if e.playing {
		_ = e.Stop()
	}
	e.cancelTransientInteraction()
	e.clearTileBrushTransient()
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	e.stopClipLoop()
	e.stopRigLoop()
	e.stopStatesLoop()
	e.resetCutsceneState()
	e.stopFetch()
	e.stopAudioAudition()
	e.stopParticlePreviewLoop()
	e.stopSceneEmitterLoop()
	e.closeModelPreview()
	e.stopExportBuild()
	e.stopAssetsBuild()
	e.resetInputMapTransient()
	_ = e.DisconnectRemote()
	_ = e.StopGame()
	e.clip = nil
	e.clipMeta = clipMeta{}
	e.clipScene = nil
	e.clipPlayer = nil
	e.clipErr = nil
	e.clipSaved = nil
	e.table = nil
	e.tableSaved = nil
	e.rig = nil
	e.l10n = nil
	e.tileset = nil
	e.ActiveTerrain = ""
	e.particles = nil
	e.material = nil
	e.lookThroughCamera3D = false
	e.envPanelOpen = false
	if e.ProjectAssets {
		// The scene opened next is read against the project's own assets; syncScenePath then finds them nearest it.
		e.Resolver = project.NewResolver(os.DirFS(filepath.Dir(path)))
		e.assetsScene, e.assetsResolverRev = "", e.assetsRev
	}
	e.Project = f
	e.ProjectPath = path
	e.projectSaved = saved
	e.projectSavedOnce = false

	resetDoc := false
	placeholder := false
	switch {
	case opened:
		e.Doc = openedDoc
		e.sceneSaved = openedSaved
		e.FilePath = openedPath
		e.ScenePath = ""
		e.OnSave = nil
		e.SelectedID = ""
		e.view2DFree = false
		e.Camera3D = DefaultOrbitCamera3D()
		e.hidden, e.locked, e.layerLocked = nil, nil, nil
		if len(openedDoc.Layers) > 0 {
			e.ActiveLayer = openedDoc.Layers[0].Name
		} else {
			e.ActiveLayer = ""
		}
		e.keepTileInPalette()
		e.History = e.newTabHistory()
		e.resetTabsToActive(false)
	case e.StartScreenOpen():
		e.Doc = DefaultSceneDoc()
		e.FilePath = ""
		e.ScenePath = ""
		e.OnSave = nil
		e.SelectedID = ""
		e.view2DFree = false
		e.Camera3D = DefaultOrbitCamera3D()
		e.hidden, e.locked, e.layerLocked = nil, nil, nil
		if len(e.Doc.Layers) > 0 {
			e.ActiveLayer = e.Doc.Layers[0].Name
		} else {
			e.ActiveLayer = ""
		}
		e.keepTileInPalette()
		e.History = e.newTabHistory()
		resetDoc = true
		placeholder = true
		e.resetTabsToActive(true)
	default:
		placeholder = e.canReplacePlaceholderTab()
		e.History = e.newTabHistory()
		e.resetTabsToActive(placeholder)
	}

	e.setStartScreenEntered()
	e.selectFile("")
	e.resetSearchForProject()
	e.syncScenePath()
	if opened {
		e.navMesh3D.config.OutputPath = e.derivedNavMeshPath()
		e.tryAutoloadNavMesh3D()
		e.restoreViewState()
	}
	if opened || e.ProjectAssets || resetDoc {
		e.Rebuild()
	}
	e.snapshotAssetOutputs()
	e.filesRev++
	e.assetsRev++
	if e.ProjectAssets {
		// The resolver was made for this project above and has read nothing that the bump just above could have changed, so it is not refreshed for it.
		e.assetsResolverRev = e.assetsRev
	}
	e.History.Clear()
	e.markSceneSaved()
	e.resetTabsToActive(placeholder)
	e.dropStalePluginTool()
	e.dropStaleFocusedPluginPanel()
	e.syncAudioMixer()
	e.resetExportPanelForProject()
	_ = e.RecordRecentProject(f.Name, filepath.Dir(path))
	e.syncNavMeshForProject()
	e.resetHUDForProject()
	e.Status = "Opened project " + path
	if missing := e.MissingPlugins(); len(missing) > 0 {
		e.Status += "; plugins not built into this editor: " + strings.Join(missing, ", ")
	}
	e.merge.open = false
	e.merge.session = nil
	e.merge.rev++
	e.refreshDetectedMergeConflicts()
	if conflictedScene != "" {
		if merr := e.OpenMergeConflict(conflictedScene); merr != nil {
			e.Status = "Opened project " + path + "; " + merr.Error()
		}
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
