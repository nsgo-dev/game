package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// SetObjectLight sets or clears object id's 3D light definition as an undoable command on History.
// Setting a light on a scene below version 4 raises its version to sceneformat.CurrentVersion,
// which Undo restores.
func (e *Editor) SetObjectLight(id string, light *sceneformat.LightDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	next := light.Clone()
	if reflect.DeepEqual(obj.Light, next) {
		return nil
	}
	if next != nil {
		probe := obj.Clone()
		probe.Light = next.Clone()
		if err := sceneformat.CheckObjectLight(&probe); err != nil {
			return err
		}
	}
	e.History.Execute(&setObjectLightCmd{
		ed:          e,
		objectID:    id,
		before:      obj.Light.Clone(),
		after:       next,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type setObjectLightCmd struct {
	ed          *Editor
	objectID    string
	before      *sceneformat.LightDoc
	after       *sceneformat.LightDoc
	prevVersion int
}

func (c *setObjectLightCmd) Label() string {
	if c.after == nil {
		return "Remove Light on " + c.objectID
	}
	return "Edit Light on " + c.objectID
}

func (c *setObjectLightCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if c.after != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	obj.Light = c.after.Clone()
	c.ed.Status = c.Label()
}

func (c *setObjectLightCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	obj.Light = c.before.Clone()
	c.ed.Doc.Version = c.prevVersion
	c.ed.Status = "Undo " + c.Label()
}

// SetObjectCamera3D sets or clears object id's 3D camera definition as an undoable command on History.
// When cam has Current set to true, any other 3D camera in the scene with Current true is cleared
// atomically in the same undo step so the scene stays valid. Setting a 3D camera on a scene below
// version 4 raises its version to sceneformat.CurrentVersion, which Undo restores.
func (e *Editor) SetObjectCamera3D(id string, cam *sceneformat.Camera3DDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	next := cam.Clone()
	if reflect.DeepEqual(obj.Camera, next) {
		return nil
	}
	if next != nil {
		probe := obj.Clone()
		probe.Camera = next.Clone()
		if err := sceneformat.CheckObjectCamera3D(&probe); err != nil {
			return err
		}
	}
	var cleared []string
	if next != nil && next.Current {
		for li := range e.Doc.Layers {
			sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
				if o.ID != id && o.Camera != nil && o.Camera.Current {
					cleared = append(cleared, o.ID)
				}
				return true
			})
		}
	}
	e.History.Execute(&setObjectCamera3DCmd{
		ed:          e,
		objectID:    id,
		before:      obj.Camera.Clone(),
		after:       next,
		clearedIDs:  cleared,
		prevVersion: e.Doc.Version,
	})
	if !e.HasSceneCamera3D() && e.lookThroughCamera3D {
		e.lookThroughCamera3D = false
		e.notify()
	}
	return nil
}

type setObjectCamera3DCmd struct {
	ed          *Editor
	objectID    string
	before      *sceneformat.Camera3DDoc
	after       *sceneformat.Camera3DDoc
	clearedIDs  []string
	prevVersion int
}

func (c *setObjectCamera3DCmd) Label() string {
	if c.after == nil {
		return "Remove 3D Camera on " + c.objectID
	}
	return "Edit 3D Camera on " + c.objectID
}

func (c *setObjectCamera3DCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if c.after != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	for _, cid := range c.clearedIDs {
		if other, _, _ := c.ed.FindObject(cid); other != nil && other.Camera != nil {
			other.Camera.Current = false
		}
	}
	obj.Camera = c.after.Clone()
	c.ed.Status = c.Label()
}

func (c *setObjectCamera3DCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	obj.Camera = c.before.Clone()
	for _, cid := range c.clearedIDs {
		if other, _, _ := c.ed.FindObject(cid); other != nil && other.Camera != nil {
			other.Camera.Current = true
		}
	}
	c.ed.Doc.Version = c.prevVersion
	if !c.ed.HasSceneCamera3D() {
		c.ed.lookThroughCamera3D = false
	}
	c.ed.Status = "Undo " + c.Label()
}

// SetEnvironment replaces the open scene's 3D environment (sky, ambient/IBL, fog, tonemapping,
// exposure) as an undoable command on History, checked by sceneformat.Validate. Setting an
// environment on a scene below version 4 raises its version to sceneformat.CurrentVersion,
// which Undo restores.
func (e *Editor) SetEnvironment(env *sceneformat.EnvironmentDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	next := env.Clone()
	if reflect.DeepEqual(e.Doc.Environment, next) {
		return nil
	}
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		if next != nil && d.Version < sceneformat.CurrentVersion {
			d.Version = sceneformat.CurrentVersion
		}
		d.Environment = next.Clone()
	}); err != nil {
		return err
	}
	e.History.Execute(&setEnvironmentCmd{
		ed:          e,
		before:      e.Doc.Environment.Clone(),
		after:       next,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type setEnvironmentCmd struct {
	ed          *Editor
	before      *sceneformat.EnvironmentDoc
	after       *sceneformat.EnvironmentDoc
	prevVersion int
}

func (c *setEnvironmentCmd) Label() string {
	if c.after == nil {
		return "Clear Environment"
	}
	return "Edit Environment"
}

func (c *setEnvironmentCmd) Do() {
	if c.after != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	c.ed.Doc.Environment = c.after.Clone()
	c.ed.Status = c.Label()
}

func (c *setEnvironmentCmd) Undo() {
	c.ed.Doc.Environment = c.before.Clone()
	c.ed.Doc.Version = c.prevVersion
	c.ed.Status = "Undo " + c.Label()
}

// AddLight3D places a new 3D light object of kind (directional, point, or spot) with sensible
// defaults on the active world layer as an undoable command, selects it, and returns its ID.
func (e *Editor) AddLight3D(kind sceneformat.LightKind) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	var obj sceneformat.ObjectDoc
	switch kind {
	case sceneformat.LightDirectional:
		obj = sceneformat.ObjectDoc{
			Name: "Directional Light",
			Y:    4,
			RotX: -0.9,
			RotY: 0.45,
			Light: &sceneformat.LightDoc{
				Kind:      sceneformat.LightDirectional,
				Color:     "#fff7e6",
				Intensity: sceneformat.Float32(3),
				Shadows:   true,
			},
		}
	case sceneformat.LightPoint:
		obj = sceneformat.ObjectDoc{
			Name: "Point Light",
			Y:    2,
			Light: &sceneformat.LightDoc{
				Kind:      sceneformat.LightPoint,
				Color:     "#ffd2a1",
				Intensity: sceneformat.Float32(5),
				Range:     10,
			},
		}
	case sceneformat.LightSpot:
		obj = sceneformat.ObjectDoc{
			Name: "Spot Light",
			Y:    3,
			PosZ: 2,
			RotX: -0.8,
			Light: &sceneformat.LightDoc{
				Kind:      sceneformat.LightSpot,
				Color:     "#ffffff",
				Intensity: sceneformat.Float32(8),
				Range:     12,
				InnerCone: 18,
				OuterCone: sceneformat.Float32(30),
			},
		}
	default:
		return "", fmt.Errorf("editor: unknown light kind %q", kind)
	}
	return e.PlaceObject(e.worldLayerName(), obj)
}

// AddCamera3D places a new 3D camera object of projection kind (perspective or orthographic) on
// the active world layer as an undoable command, selects it, and returns its ID. When the scene
// has no 3D camera yet, the new camera is marked current.
func (e *Editor) AddCamera3D(proj sceneformat.ProjectionKind) (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	if proj == "" {
		proj = sceneformat.ProjectionPerspective
	}
	cam := &sceneformat.Camera3DDoc{
		Projection: proj,
		Near:       sceneformat.Float32(0.1),
		Far:        sceneformat.Float32(1000),
		Current:    !e.HasSceneCamera3D(),
	}
	switch proj {
	case sceneformat.ProjectionPerspective:
		cam.FOV = sceneformat.Float32(60)
	case sceneformat.ProjectionOrthographic:
		cam.OrthoHeight = sceneformat.Float32(10)
	default:
		return "", fmt.Errorf("editor: unknown camera projection %q", proj)
	}
	obj := sceneformat.ObjectDoc{
		Name:   "Camera 3D",
		Y:      2,
		PosZ:   6,
		Camera: cam,
	}
	return e.PlaceObject(e.worldLayerName(), obj)
}

// worldLayerName returns ActiveLayer when it is a world-space layer, otherwise the first world-space layer in Doc.
func (e *Editor) worldLayerName() string {
	if e == nil || e.Doc == nil {
		return ""
	}
	if li := e.layerIndex(e.ActiveLayer); li >= 0 && e.Doc.Layers[li].EffectiveSpace() != sceneformat.SpaceScreen {
		return e.Doc.Layers[li].Name
	}
	for i := range e.Doc.Layers {
		if e.Doc.Layers[i].EffectiveSpace() != sceneformat.SpaceScreen {
			return e.Doc.Layers[i].Name
		}
	}
	return e.ActiveLayer
}

// HasSceneCamera3D reports whether the open scene defines at least one visible 3D camera object.
func (e *Editor) HasSceneCamera3D() bool {
	if e == nil || e.Doc == nil {
		return false
	}
	s := e.scene3D()
	if s.built == nil {
		return false
	}
	_, ok := s.built.ActiveCameraObject()
	return ok
}

// ActiveSceneCamera3D returns the scene's active 3D camera (the visible camera with Current true,
// or the first visible 3D camera) as a scene3d.Camera for a viewport of size w by h, together with
// its object ID.
func (e *Editor) ActiveSceneCamera3D(w, h float32) (*scene3d.Camera, string, bool) {
	if e == nil || e.Doc == nil {
		return nil, "", false
	}
	s := e.scene3D()
	if s.built == nil {
		return nil, "", false
	}
	c, ok := s.built.ActiveCameraObject()
	if !ok || c == nil {
		return nil, "", false
	}
	aspect := float32(1)
	if w > 0 && h > 0 {
		aspect = w / h
	}
	return c.SceneCamera(aspect), c.Object, true
}

// LookThroughCamera3D reports whether the 3D viewport is currently looking through the scene's active 3D camera.
func (e *Editor) LookThroughCamera3D() bool {
	return e != nil && e.lookThroughCamera3D && e.HasSceneCamera3D()
}

// SetLookThroughCamera3D toggles whether the 3D viewport renders and projects through the scene's
// active 3D camera (matching the game's first frame). Enabling it without a 3D camera in the scene
// returns an error.
func (e *Editor) SetLookThroughCamera3D(on bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if on && !e.HasSceneCamera3D() {
		return errors.New("editor: scene has no 3D camera")
	}
	if e.lookThroughCamera3D == on {
		return nil
	}
	e.endCameraGestures3D()
	e.lookThroughCamera3D = on
	e.Status = "View: " + e.ViewName3D()
	e.notify()
	return nil
}

// ToggleLookThroughCamera3D switches between the orbit camera and the scene's active 3D camera.
// Returns true if looking through the scene camera afterwards.
func (e *Editor) ToggleLookThroughCamera3D() bool {
	if e == nil {
		return false
	}
	if e.LookThroughCamera3D() {
		_ = e.SetLookThroughCamera3D(false)
		return false
	}
	if err := e.SetLookThroughCamera3D(true); err != nil {
		e.report(err)
		return false
	}
	return true
}

// EffectiveCamera3D returns the scene3d.Camera the 3D viewport currently draws through: the scene's
// active 3D camera when LookThroughCamera3D is true, and Camera3D.SceneCamera otherwise.
func (e *Editor) EffectiveCamera3D(w, h float32) *scene3d.Camera {
	if e != nil && e.lookThroughCamera3D {
		if cam, _, ok := e.ActiveSceneCamera3D(w, h); ok {
			return cam
		}
	}
	return e.Camera3D.SceneCamera(w, h)
}

// ProjectHDRs lists the slash-separated paths of Radiance HDR (.hdr, .pic) environment files in
// the open project, sorted alphabetically.
func (e *Editor) ProjectHDRs() []string {
	if e == nil || e.ProjectPath == "" {
		return nil
	}
	root := filepath.Dir(e.ProjectPath)
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "build" || name == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		if !sceneformat.IsHDRPath(name) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(out)
	return out
}

// EnvironmentPanelOpen reports whether the Scene Environment panel is open.
func (e *Editor) EnvironmentPanelOpen() bool {
	return e != nil && e.envPanelOpen
}

// SetEnvironmentPanelOpen opens or closes the Scene Environment panel.
func (e *Editor) SetEnvironmentPanelOpen(open bool) {
	if e == nil || e.envPanelOpen == open {
		return
	}
	e.envPanelOpen = open
	e.notify()
}

// ToggleEnvironmentPanel toggles the Scene Environment panel and reports its new open state.
func (e *Editor) ToggleEnvironmentPanel() bool {
	if e == nil {
		return false
	}
	e.SetEnvironmentPanelOpen(!e.envPanelOpen)
	return e.envPanelOpen
}
