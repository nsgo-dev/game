package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
)

// SetObjectReflectionProbe sets or clears object id's reflection probe definition as an undoable
// command on History. Setting a reflection probe on a scene below version 4 raises its version to
// sceneformat.CurrentVersion, which Undo restores.
func (e *Editor) SetObjectReflectionProbe(id string, probe *sceneformat.ReflectionProbeDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	next := probe.Clone()
	if reflect.DeepEqual(obj.ReflectionProbe, next) {
		return nil
	}
	if next != nil {
		check := obj.Clone()
		check.ReflectionProbe = next.Clone()
		if err := sceneformat.CheckObjectReflectionProbe(&check); err != nil {
			return err
		}
	}
	e.History.Execute(&setObjectReflectionProbeCmd{
		ed:          e,
		objectID:    id,
		before:      obj.ReflectionProbe.Clone(),
		after:       next,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type setObjectReflectionProbeCmd struct {
	ed          *Editor
	objectID    string
	before      *sceneformat.ReflectionProbeDoc
	after       *sceneformat.ReflectionProbeDoc
	prevVersion int
}

func (c *setObjectReflectionProbeCmd) Label() string {
	if c.after == nil {
		return "Remove Reflection Probe on " + c.objectID
	}
	return "Edit Reflection Probe on " + c.objectID
}

func (c *setObjectReflectionProbeCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if c.after != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	obj.ReflectionProbe = c.after.Clone()
	c.ed.Status = c.Label()
}

func (c *setObjectReflectionProbeCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	obj.ReflectionProbe = c.before.Clone()
	c.ed.Doc.Version = c.prevVersion
	c.ed.Status = "Undo " + c.Label()
}

// SetObjectLightProbeGrid sets or clears object id's light probe grid definition as an undoable
// command on History. Setting a light probe grid on a scene below version 4 raises its version to
// sceneformat.CurrentVersion, which Undo restores.
func (e *Editor) SetObjectLightProbeGrid(id string, grid *sceneformat.LightProbeGridDoc) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if err := e.checkAgentTouchLock(id); err != nil {
		return err
	}
	if e.Doc == nil {
		return errors.New("editor: nil scene document")
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", id)
	}
	next := grid.Clone()
	if reflect.DeepEqual(obj.LightProbeGrid, next) {
		return nil
	}
	if next != nil {
		check := obj.Clone()
		check.LightProbeGrid = next.Clone()
		if err := sceneformat.CheckObjectLightProbeGrid(&check); err != nil {
			return err
		}
	}
	e.History.Execute(&setObjectLightProbeGridCmd{
		ed:          e,
		objectID:    id,
		before:      obj.LightProbeGrid.Clone(),
		after:       next,
		prevVersion: e.Doc.Version,
	})
	return nil
}

type setObjectLightProbeGridCmd struct {
	ed          *Editor
	objectID    string
	before      *sceneformat.LightProbeGridDoc
	after       *sceneformat.LightProbeGridDoc
	prevVersion int
}

func (c *setObjectLightProbeGridCmd) Label() string {
	if c.after == nil {
		return "Remove Light Probe Grid on " + c.objectID
	}
	return "Edit Light Probe Grid on " + c.objectID
}

func (c *setObjectLightProbeGridCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if c.after != nil && c.ed.Doc.Version < sceneformat.CurrentVersion {
		c.ed.Doc.Version = sceneformat.CurrentVersion
	}
	obj.LightProbeGrid = c.after.Clone()
	c.ed.Status = c.Label()
}

func (c *setObjectLightProbeGridCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	obj.LightProbeGrid = c.before.Clone()
	c.ed.Doc.Version = c.prevVersion
	c.ed.Status = "Undo " + c.Label()
}

// AddReflectionProbe3D places a new 3D reflection probe object with sensible defaults on the
// active world layer as an undoable command, selects it, and returns its ID.
func (e *Editor) AddReflectionProbe3D() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	sz := [3]float32{8, 4, 8}
	obj := sceneformat.ObjectDoc{
		Name: "Reflection Probe",
		Y:    1.5,
		ReflectionProbe: &sceneformat.ReflectionProbeDoc{
			Size:          &sz,
			BlendDistance: 0.5,
			Intensity:     sceneformat.Float32(1),
			BoxProjection: sceneformat.Bool(true),
			Resolution:    64,
		},
	}
	return e.PlaceObject(e.worldLayerName(), obj)
}

// AddLightProbeGrid3D places a new 3D light probe grid object with sensible defaults on the
// active world layer as an undoable command, selects it, and returns its ID.
func (e *Editor) AddLightProbeGrid3D() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	if e.playing {
		return "", ErrPlaying
	}
	sz := [3]float32{8, 4, 8}
	cnt := [3]int{3, 2, 3}
	obj := sceneformat.ObjectDoc{
		Name: "Light Probe Grid",
		Y:    1.5,
		LightProbeGrid: &sceneformat.LightProbeGridDoc{
			Size:      &sz,
			Counts:    &cnt,
			Intensity: sceneformat.Float32(1),
		},
	}
	return e.PlaceObject(e.worldLayerName(), obj)
}

// HasProbes3D reports whether the open scene contains at least one reflection probe or light probe grid.
func (e *Editor) HasProbes3D() bool {
	if e == nil || e.Doc == nil {
		return false
	}
	found := false
	for li := range e.Doc.Layers {
		sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.ReflectionProbe != nil || o.LightProbeGrid != nil {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// probeOutputRoot returns the directory against which baked probe paths are resolved and written.
func (e *Editor) probeOutputRoot() (root string, inProject bool, err error) {
	switch {
	case e.ProjectPath != "":
		return filepath.Dir(e.ProjectPath), true, nil
	case e.FilePath != "":
		return filepath.Dir(e.FilePath), false, nil
	default:
		return "", false, errors.New("editor: open a project or save the scene before baking probes")
	}
}

func sanitizeProbeFileToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "probe"
	}
	return out
}

func (e *Editor) sceneStemForProbes() string {
	p := e.ScenePath
	if p == "" && e.FilePath != "" {
		p = filepath.Base(e.FilePath)
	}
	base := path.Base(filepath.ToSlash(strings.TrimSpace(p)))
	lower := strings.ToLower(base)
	for _, suf := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(lower, suf) {
			base = base[:len(base)-len(suf)]
			break
		}
	}
	if base == "" || base == "." || base == "/" {
		if e.Doc != nil && strings.TrimSpace(e.Doc.Name) != "" {
			return sanitizeProbeFileToken(e.Doc.Name)
		}
		return "scene"
	}
	return sanitizeProbeFileToken(base)
}

func (e *Editor) derivedReflectionProbePath(objectID string) string {
	return path.Join("probes", e.sceneStemForProbes()+"_"+sanitizeProbeFileToken(objectID)+".hdr")
}

func (e *Editor) derivedLightProbeGridPath(objectID string) string {
	return path.Join("probes", e.sceneStemForProbes()+"_"+sanitizeProbeFileToken(objectID)+".probes.yaml")
}

// resolveProbeOutputPath validates outPath as a slash-separated relative path strictly inside rootDir,
// refusing reserved project file names, absolute paths, path traversal, and symlinks escaping rootDir.
func resolveProbeOutputPath(rootDir, outPath string) (rel, abs string, err error) {
	if strings.ContainsRune(outPath, '\\') || filepath.IsAbs(outPath) || !validScenePath(outPath) {
		return "", "", fmt.Errorf("editor: invalid probe output path %q", outPath)
	}
	rel = path.Clean(outPath)
	if !validScenePath(rel) {
		return "", "", fmt.Errorf("editor: invalid probe output path %q", outPath)
	}
	if base := path.Base(rel); base == project.FileName || base == project.NSGoFileName || base == refs.IndexFileName || base == "go.mod" {
		return "", "", fmt.Errorf("editor: %q is a reserved project file name, not a probe output path", rel)
	}

	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", "", err
	}
	resolvedRoot := rootAbs
	if r, err := filepath.EvalSymlinks(rootAbs); err == nil {
		resolvedRoot = r
	}

	abs = filepath.Join(rootAbs, filepath.FromSlash(rel))
	if fi, err := os.Lstat(abs); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("editor: probe output path %q must not be a symlink", rel)
		}
		if fi.IsDir() {
			return "", "", fmt.Errorf("editor: probe output path %q is a directory", rel)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}

	cur := filepath.Dir(abs)
	for {
		resolvedCur, err := filepath.EvalSymlinks(cur)
		if err == nil {
			checkRel, err := filepath.Rel(resolvedRoot, resolvedCur)
			if err != nil || checkRel == ".." || strings.HasPrefix(checkRel, ".."+string(filepath.Separator)) || filepath.IsAbs(checkRel) {
				return "", "", fmt.Errorf("editor: probe output path %q escapes root via symlink", rel)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return rel, abs, nil
}

func (e *Editor) probeBakeOptions(built *scenemodel.Built) scenemodel.ProbeBakeOptions {
	var opts scenemodel.ProbeBakeOptions
	if e.Lights3D != nil {
		lights := *e.Lights3D
		opts.FallbackLights = &lights
	} else if len(built.Lights) == 0 && !built.Environment.Present {
		lights := DefaultLights3D()
		opts.FallbackLights = &lights
	}
	return opts
}

// buildScene3DForProbeBake builds a fresh 3D scene snapshot for offline probe baking with all
// ReflectionProbe.Cubemap and LightProbeGrid.Data file paths cleared. Because baking renders with
// DisableProbes: true, clearing baked probe file references ensures baking succeeds even when an
// author entered a new output path before baking or deleted stale baked files, and avoids
// redundant cubemap prefiltering while baking multiple probes.
func (e *Editor) buildScene3DForProbeBake() (*scenemodel.Built, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: nil scene document")
	}
	doc := e.Doc
	if doc.RefersToScenes() {
		scenes, _ := e.Resolver.(sceneformat.SceneResolver)
		resolved, err := sceneformat.Resolve(doc, scenes)
		if err != nil {
			return nil, fmt.Errorf("editor: resolve 3D scene for probe bake: %w", err)
		}
		doc = resolved
	} else {
		doc = doc.Clone()
	}
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.ReflectionProbe != nil {
				o.ReflectionProbe.Cubemap = ""
			}
			if o.LightProbeGrid != nil {
				o.LightProbeGrid.Data = ""
			}
			return true
		})
	}
	var files *scenemodel.Assets
	if root := e.scene3DFilesRoot(); root != "" {
		files = scenemodel.NewAssets(newConfinedProjectDirFS(root))
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), scene3DAssets{
		models:   e.ModelResolver,
		resolver: e.Resolver,
		files:    files,
	})
	if err != nil {
		return nil, fmt.Errorf("editor: build 3D scene for probe bake: %w", err)
	}
	e.syncBuilt3D(built)
	return built, nil
}

// BakeReflectionProbe3D renders the open 3D scene from object id's reflection probe position into
// a 6-face Radiance HDR cubemap strip, writes it atomically to disk, links ReflectionProbe.Cubemap
// via History when unset or changed, and refreshes the 3D scene build so the viewport shows the
// baked reflection immediately. Returns the slash-separated output path relative to the project root.
func (e *Editor) BakeReflectionProbe3D(id string) (string, error) {
	return e.bakeReflectionProbe3DWith(id, nil)
}

func (e *Editor) bakeReflectionProbe3DWith(id string, prebuilt *scenemodel.Built) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: nil scene document")
	}
	if e.playing {
		return "", ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil || obj.ReflectionProbe == nil {
		return "", fmt.Errorf("editor: object %q has no reflection probe", id)
	}
	rootDir, inProject, err := e.probeOutputRoot()
	if err != nil {
		return "", err
	}
	outPath := strings.TrimSpace(obj.ReflectionProbe.Cubemap)
	if outPath == "" {
		outPath = e.derivedReflectionProbePath(id)
	}
	if !sceneformat.IsHDRPath(outPath) {
		return "", fmt.Errorf("editor: reflection probe cubemap path %q must end with .hdr or .pic", outPath)
	}
	rel, abs, err := resolveProbeOutputPath(rootDir, outPath)
	if err != nil {
		return "", err
	}

	built := prebuilt
	if built == nil {
		built, err = e.buildScene3DForProbeBake()
		if err != nil {
			return "", err
		}
	}

	var rp *scenemodel.ReflectionProbe
	for _, cand := range built.ReflectionProbes {
		if cand != nil && cand.Object == id {
			rp = cand
			break
		}
	}
	if rp == nil {
		return "", fmt.Errorf("editor: reflection probe %q was not found in the 3D scene build", id)
	}

	cube, err := scenemodel.BakeReflectionProbe(built, rp, e.probeBakeOptions(built))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := render3d.EncodeCubemapHDR(&buf, cube); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(abs, buf.Bytes()); err != nil {
		return "", err
	}

	if obj.ReflectionProbe.Cubemap != rel {
		next := obj.ReflectionProbe.Clone()
		next.Cubemap = rel
		if err := e.SetObjectReflectionProbe(id, next); err != nil {
			return "", err
		}
	}
	e.assetsRev++
	if inProject {
		e.filesRev++
	}
	e.s3d.stale = true
	e.Status = fmt.Sprintf("Baked reflection probe %q -> %s", id, rel)
	e.notify()
	return rel, nil
}

// BakeLightProbeGrid3D renders the open 3D scene at every sample point of object id's light probe
// grid, encodes the resulting L2 spherical harmonic irradiance coefficients as versioned canonical
// YAML, writes it atomically to disk, links LightProbeGrid.Data via History when unset or changed,
// and refreshes the 3D scene build. Returns the slash-separated output path relative to the project root.
func (e *Editor) BakeLightProbeGrid3D(id string) (string, error) {
	return e.bakeLightProbeGrid3DWith(id, nil)
}

func (e *Editor) bakeLightProbeGrid3DWith(id string, prebuilt *scenemodel.Built) (string, error) {
	if e == nil || e.Doc == nil {
		return "", errors.New("editor: nil scene document")
	}
	if e.playing {
		return "", ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil || obj.LightProbeGrid == nil {
		return "", fmt.Errorf("editor: object %q has no light probe grid", id)
	}
	rootDir, inProject, err := e.probeOutputRoot()
	if err != nil {
		return "", err
	}
	outPath := strings.TrimSpace(obj.LightProbeGrid.Data)
	if outPath == "" {
		outPath = e.derivedLightProbeGridPath(id)
	}
	lowerOut := strings.ToLower(outPath)
	if !strings.HasSuffix(lowerOut, ".yaml") && !strings.HasSuffix(lowerOut, ".yml") {
		return "", fmt.Errorf("editor: light probe grid data path %q must end with .yaml or .yml", outPath)
	}
	rel, abs, err := resolveProbeOutputPath(rootDir, outPath)
	if err != nil {
		return "", err
	}

	built := prebuilt
	if built == nil {
		built, err = e.buildScene3DForProbeBake()
		if err != nil {
			return "", err
		}
	}

	var lp *scenemodel.LightProbeGrid
	for _, cand := range built.LightProbeGrids {
		if cand != nil && cand.Object == id {
			lp = cand
			break
		}
	}
	if lp == nil {
		return "", fmt.Errorf("editor: light probe grid %q was not found in the 3D scene build", id)
	}

	grid, err := scenemodel.BakeLightProbeGrid(built, lp, e.probeBakeOptions(built))
	if err != nil {
		return "", err
	}
	doc, err := render3d.LightProbeGridToDoc(grid)
	if err != nil {
		return "", err
	}
	yml, err := render3d.EncodeLightProbeGrid(doc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := writeFileAtomic(abs, yml); err != nil {
		return "", err
	}

	if obj.LightProbeGrid.Data != rel {
		next := obj.LightProbeGrid.Clone()
		next.Data = rel
		if err := e.SetObjectLightProbeGrid(id, next); err != nil {
			return "", err
		}
	}
	e.assetsRev++
	if inProject {
		e.filesRev++
	}
	e.s3d.stale = true
	e.Status = fmt.Sprintf("Baked light probe grid %q -> %s", id, rel)
	e.notify()
	return rel, nil
}

// BakeAllProbes3D bakes every reflection probe and light probe grid in the open scene, writing
// their .hdr cubemaps and .probes.yaml files to disk and linking any unset paths in a single
// undoable History group. Returns the number of probes baked.
func (e *Editor) BakeAllProbes3D() (int, error) {
	if e == nil || e.Doc == nil {
		return 0, errors.New("editor: nil scene document")
	}
	if e.playing {
		return 0, ErrPlaying
	}
	var reflIDs, gridIDs []string
	for li := range e.Doc.Layers {
		sceneformat.WalkObjects(e.Doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if o.ReflectionProbe != nil {
				reflIDs = append(reflIDs, o.ID)
			}
			if o.LightProbeGrid != nil {
				gridIDs = append(gridIDs, o.ID)
			}
			return true
		})
	}
	if len(reflIDs) == 0 && len(gridIDs) == 0 {
		return 0, errors.New("editor: scene has no reflection probes or light probe grids")
	}
	built, err := e.buildScene3DForProbeBake()
	if err != nil {
		return 0, err
	}
	baked := 0
	err = e.History.Group("Bake Probes", func() error {
		for _, id := range reflIDs {
			if _, err := e.bakeReflectionProbe3DWith(id, built); err != nil {
				return err
			}
			baked++
		}
		for _, id := range gridIDs {
			if _, err := e.bakeLightProbeGrid3DWith(id, built); err != nil {
				return err
			}
			baked++
		}
		return nil
	})
	if err != nil {
		return baked, err
	}
	e.Status = fmt.Sprintf("Baked %d probes", baked)
	e.notify()
	return baked, nil
}
