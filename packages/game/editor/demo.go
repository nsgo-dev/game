package editor

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// DemoAtlasMap returns the sprite atlases used by the editor's built-in Demo Room scene.
func DemoAtlasMap() sceneformat.AtlasMap {
	return sceneformat.AtlasMap{
		"tiles": {
			Image: core.ImageSource{URI: "asset:tiles.png"},
			Frames: map[string]sprite.Frame{
				"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
		"actors": {
			Image: core.ImageSource{URI: "asset:actors.png"},
			Frames: map[string]sprite.Frame{
				"hero":  {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"torch": {Name: "torch", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
	}
}

// DemoSceneDoc returns a fresh copy of the editor's built-in Demo Room scene document.
func DemoSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Demo Room",
		VirtualWidth:    320,
		VirtualHeight:   180,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "floor",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  16,
					TileHeight: 16,
					Width:      8,
					Height:     5,
					Tileset:    "tiles",
					Data: []int{
						1, 1, 1, 1, 1, 1, 1, 1,
						1, 2, 2, 2, 2, 2, 2, 1,
						1, 2, 0, 0, 0, 0, 2, 1,
						1, 2, 2, 2, 2, 2, 2, 1,
						1, 1, 1, 1, 1, 1, 1, 1,
					},
				},
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:      "hero",
						Name:    "Hero",
						X:       64,
						Y:       40,
						OriginX: 8,
						OriginY: 8,
						Z:       5,
						Sprite:  &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
						Components: map[string]any{
							sceneformat.Body2DComponentName: map[string]any{
								"version":      sceneformat.PhysicsComponentVersion,
								"kind":         "dynamic",
								"mass":         1.0,
								"gravityScale": 0.0,
								"vx":           24.0,
							},
							sceneformat.Collider2DComponentName: map[string]any{
								"version": sceneformat.PhysicsComponentVersion,
								"shape":   "box",
								"width":   16.0,
								"height":  16.0,
							},
						},
						Properties: map[string]any{
							"hp":     10,
							"tag":    "player",
							"active": true,
						},
					},
				},
			},
		},
	}
}

// DemoComponentRegistry returns the built-in 2D and 3D physics component registry used by the Demo Room scene.
func DemoComponentRegistry() *sceneformat.ComponentRegistry {
	return sceneformat.PhysicsComponentRegistry()
}

// NewDemo constructs an Editor preloaded with the built-in Demo Room scene, atlases, component registry, and play update callback.
func NewDemo() (*Editor, error) {
	ed, err := New(DemoSceneDoc(), DemoAtlasMap())
	if err != nil {
		return nil, err
	}
	if err := ed.SetComponentRegistry(DemoComponentRegistry()); err != nil {
		return nil, err
	}
	ed.SetActiveLayer("actors")
	ed.SelectObject("hero")
	ed.attachDemoPlayUpdate()
	ed.markSceneSaved()
	return ed, nil
}

func (e *Editor) attachDemoPlayUpdate() {
	var elapsed float64
	e.OnPlayUpdate = func(scene *sceneformat.BuiltScene, dt time.Duration) {
		elapsed += dt.Seconds()
		if hero := scene.Object("hero"); hero != nil && hero.Sprite != nil {
			dx := float32(math.Cos(elapsed*2) * 20 * dt.Seconds())
			hero.Sprite.X += dx
			hero.Doc.X = hero.Sprite.X
		}
	}
}

// OpenDemo replaces the current project or scene with the built-in Demo Room (or invokes Editor.OnOpenDemo when configured),
// clearing project state and undo history and closing the Start Screen.
func (e *Editor) OpenDemo() error {
	if e == nil {
		return nil
	}
	if e.OnOpenDemo != nil {
		if err := e.OnOpenDemo(); err != nil {
			return err
		}
		e.setStartScreenEntered()
		e.notify()
		return nil
	}
	if e.playing {
		if err := e.Stop(); err != nil {
			return err
		}
	}
	e.CancelInspectorScrub()
	e.resetInspectorSessionState()
	e.resetFirstRunFlow()
	e.drag2D = drag2DState{}
	e.stroke = nil
	e.clearTileBrushTransient()
	e.stopClipLoop()
	e.stopRigLoop()
	e.stopStatesLoop()
	e.resetCutsceneState()
	e.stopFetch()
	e.stopAudioAudition()
	e.resetAssetDrawerState()
	e.stopParticlePreviewLoop()
	e.stopSceneEmitterLoop()
	e.closeModelPreview()
	e.stopExportBuild()
	e.resetExportPanelCache()
	e.stopAssetsBuild()
	e.resetAutoSaveState()
	e.resetInputMapTransient()
	e.settingsOpen = false
	e.shortcutCaptureActive = false
	_ = e.StopGame()

	e.Project = nil
	e.ProjectPath = ""
	e.projectSaved = nil
	e.projectSavedOnce = false
	e.clip = nil
	e.clipMeta = clipMeta{}
	e.clipScene = nil
	e.clipPlayer = nil
	e.clipLoading = false
	e.clipErr = nil
	e.clipSaved = nil
	e.table = nil
	e.tableSaved = nil
	e.rig = nil
	e.l10n = nil
	e.stopTileAnimLoop()
	e.tileset = nil
	e.tileReview = nil
	e.paletteImgCache = paletteFileImageCache{}
	e.ActiveTerrain = ""
	e.particles = nil
	e.material = nil
	e.lookThroughCamera3D = false
	e.envPanelOpen = false
	e.merge = mergeConflictState{}
	e.resetChangesState()
	e.selectFile("")
	e.resetSearchForProject()
	e.clearAllDiskState()
	e.filesRev++
	e.dropStalePluginTool()
	e.dropStaleFocusedPluginPanel()
	e.syncAudioMixer()
	e.resetExportPanelForProject()
	e.resetBuildReleaseForProject()
	e.resetHUDForProject()
	e.hud.open = false
	e.resetPlaytestForProject()
	e.resetPlayMarksForProject()
	e.resetUsagesForProject()
	e.resetContextMenuForProject()
	e.resetAssetGenForProject()
	e.loadAgentDocsForProject("")
	e.resetViewport3DAgentPrompt(true)
	e.resetPermissionsForProject()
	e.resetTextInputFocus()
	e.resetAgentForProject()
	e.resetProblemsForProject()
	e.resetTasksForProject()
	e.applyViewStateSnap(nil)

	e.Doc = DemoSceneDoc()
	e.Resolver = DemoAtlasMap()
	e.assetsScene = ""
	e.assetsRev++
	e.components = DemoComponentRegistry()
	e.FilePath = ""
	e.ScenePath = ""
	e.OnSave = nil
	e.view2DFree = false
	e.inspectAssetFile = false
	e.inspectLayer = false
	e.connPick = nil
	e.marquee = marqueeState{}
	e.hidden, e.locked, e.layerLocked = nil, nil, nil
	e.ActiveLayer = "actors"
	e.setSelectionQuiet([]string{"hero"}, "hero")
	e.Camera3D = DefaultOrbitCamera3D()
	e.History = e.newTabHistory()
	e.keepTileInPalette()
	e.attachDemoPlayUpdate()
	e.Rebuild()
	e.History.Clear()
	e.markSceneSaved()
	e.resetTabsToActive(false)
	e.setStartScreenEntered()
	e.Status = "Opened Demo Room"
	e.notify()
	return nil
}
