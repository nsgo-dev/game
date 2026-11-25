package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// NavMeshBakeConfig holds the 3D navigation mesh bake parameters edited in the editor's
// NavMesh panel and used by Editor.BakeNavMesh3D. Zero numeric fields take navbake3d's defaults.
type NavMeshBakeConfig struct {
	// AgentName names the baked agent type; empty defaults to project.DefaultNavAgent ("default").
	AgentName string
	// Radius is how far the baked mesh keeps from walls and ledges.
	Radius float32
	// Height is the minimum ceiling clearance the agent needs.
	Height float32
	// MaxClimb is the highest step (riser) the agent can climb.
	MaxClimb float32
	// MaxSlope is the steepest walkable slope in degrees, below 90.
	MaxSlope float32
	// CellSize is the horizontal (XZ) voxel size of the heightfield.
	CellSize float32
	// CellHeight is the vertical (Y) voxel size of the heightfield.
	CellHeight float32
	// OutputPath is the slash-separated project-relative path (or file path when no project is open)
	// where the deterministic .navmesh.yaml file is written. Empty uses SceneDoc.NavMesh or derives
	// a sibling .navmesh.yaml path from the open scene's path.
	OutputPath string
}

// DefaultNavMeshBakeConfig returns NavMeshBakeConfig populated with navbake3d's defaults.
func DefaultNavMeshBakeConfig() NavMeshBakeConfig {
	return NavMeshBakeConfig{
		AgentName:  project.DefaultNavAgent,
		Radius:     navbake3d.DefaultAgentRadius,
		Height:     navbake3d.DefaultAgentHeight,
		MaxClimb:   navbake3d.DefaultAgentMaxClimb,
		MaxSlope:   navbake3d.DefaultAgentMaxSlope,
		CellSize:   navbake3d.DefaultCellSize,
		CellHeight: navbake3d.DefaultCellHeight,
	}
}

type navMesh3DState struct {
	panelOpen      bool
	config         NavMeshBakeConfig
	file           *navbake3d.File
	lastError      string
	lastPath       string
	cachedMesh     *navbake3d.Mesh
	cachedAllLines [][2]math3d.Vec3
	cachedOutlines int
}

func (s *navMesh3DState) setFile(f *navbake3d.File) {
	s.file = f
	s.cachedMesh = nil
	s.cachedAllLines = nil
	s.cachedOutlines = 0
}

// NavMeshPanelOpen reports whether the 3D NavMesh bake settings panel is visible.
func (e *Editor) NavMeshPanelOpen() bool {
	return e != nil && e.navMesh3D.panelOpen
}

// SetNavMeshPanelOpen shows or hides the 3D NavMesh bake settings panel.
func (e *Editor) SetNavMeshPanelOpen(open bool) {
	if e == nil || e.navMesh3D.panelOpen == open {
		return
	}
	e.navMesh3D.panelOpen = open
	e.notify()
}

// SetShowNavMesh3D toggles drawing the baked 3D navigation mesh overlay in the 3D viewport.
func (e *Editor) SetShowNavMesh3D(show bool) {
	if e == nil || e.ShowNavMesh3D == show {
		return
	}
	e.ShowNavMesh3D = show
	e.notify()
}

// NavMeshBakeConfig returns the editor's current 3D navigation mesh bake settings.
func (e *Editor) NavMeshBakeConfig() NavMeshBakeConfig {
	if e == nil {
		return DefaultNavMeshBakeConfig()
	}
	cfg := e.navMesh3D.config
	if cfg == (NavMeshBakeConfig{}) {
		cfg = DefaultNavMeshBakeConfig()
	}
	if cfg.OutputPath == "" && e.Doc != nil && e.Doc.NavMesh != "" {
		cfg.OutputPath = e.Doc.NavMesh
	}
	return cfg
}

// SetNavMeshBakeConfig updates the editor's 3D navigation mesh bake settings.
func (e *Editor) SetNavMeshBakeConfig(cfg NavMeshBakeConfig) {
	if e == nil {
		return
	}
	if e.navMesh3D.config == cfg {
		return
	}
	e.navMesh3D.config = cfg
	e.notify()
}

// NavMesh3DFile returns the currently loaded or baked 3D navigation mesh file, or nil.
func (e *Editor) NavMesh3DFile() *navbake3d.File {
	if e == nil {
		return nil
	}
	return e.navMesh3D.file
}

// SetNavMesh3DFile sets the 3D navigation mesh file previewed in the 3D viewport.
func (e *Editor) SetNavMesh3DFile(f *navbake3d.File) {
	if e == nil {
		return
	}
	e.navMesh3D.setFile(f)
	e.navMesh3D.lastError = ""
	e.notify()
}

// NavMesh3DMesh returns the active preview mesh from NavMesh3DFile: the mesh matching
// NavMeshBakeConfig().AgentName when present, or the first mesh in the file.
func (e *Editor) NavMesh3DMesh() *navbake3d.Mesh {
	if e == nil || e.navMesh3D.file == nil || len(e.navMesh3D.file.Meshes) == 0 {
		return nil
	}
	name := strings.TrimSpace(e.navMesh3D.config.AgentName)
	if name != "" {
		if m, ok := e.navMesh3D.file.Mesh(name); ok {
			return m
		}
	}
	return &e.navMesh3D.file.Meshes[0]
}

// NavMesh3DError returns the error message from the most recent failed navmesh bake or load, or empty.
func (e *Editor) NavMesh3DError() string {
	if e == nil {
		return ""
	}
	return e.navMesh3D.lastError
}

// SetSceneNavMesh updates SceneDoc.NavMesh through History as one undoable step, validated by sceneformat.Validate.
func (e *Editor) SetSceneNavMesh(navPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	e.CancelDrag2D()
	e.CancelCameraDrag()
	navPath = strings.TrimSpace(navPath)
	if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
		d.NavMesh = navPath
	}); err != nil {
		return err
	}
	if e.Doc.NavMesh == navPath {
		return nil
	}
	e.History.Execute(&setSceneNavMeshCmd{
		ed:      e,
		oldPath: e.Doc.NavMesh,
		newPath: navPath,
	})
	return nil
}

type setSceneNavMeshCmd struct {
	ed      *Editor
	oldPath string
	newPath string
}

func (c *setSceneNavMeshCmd) Label() string { return "Set scene navMesh" }

func (c *setSceneNavMeshCmd) apply(navPath string) {
	if c.ed.Doc == nil {
		return
	}
	c.ed.Doc.NavMesh = navPath
	if navPath == "" {
		c.ed.navMesh3D.setFile(nil)
		c.ed.navMesh3D.lastPath = ""
		c.ed.navMesh3D.lastError = ""
		c.ed.navMesh3D.config.OutputPath = c.ed.derivedNavMeshPath()
		return
	}
	c.ed.navMesh3D.config.OutputPath = navPath
	if c.ed.navMesh3D.lastPath != navPath {
		c.ed.navMesh3D.setFile(nil)
		c.ed.navMesh3D.lastPath = ""
		c.ed.navMesh3D.lastError = ""
		c.ed.tryAutoloadNavMesh3D()
	}
}

func (c *setSceneNavMeshCmd) Do() {
	c.apply(c.newPath)
	if c.ed.Doc != nil {
		c.ed.Status = "Updated scene navMesh"
	}
}

func (c *setSceneNavMeshCmd) Undo() {
	c.apply(c.oldPath)
}

// navMeshAssetFS returns the file system used to load glTF models, instanced scenes, and existing .navmesh.yaml files.
func (e *Editor) navMeshAssetFS() fs.FS {
	if e.Project != nil && e.ProjectPath != "" {
		return os.DirFS(filepath.Dir(e.ProjectPath))
	}
	if e.NavMesh3DAssetFS != nil {
		return e.NavMesh3DAssetFS
	}
	if e.FilePath != "" {
		return os.DirFS(filepath.Dir(e.FilePath))
	}
	return nil
}

// derivedNavMeshPath returns the default .navmesh.yaml output path for the open scene.
func (e *Editor) derivedNavMeshPath() string {
	if e == nil {
		return ""
	}
	if e.Doc != nil && strings.TrimSpace(e.Doc.NavMesh) != "" {
		return strings.TrimSpace(e.Doc.NavMesh)
	}
	if e.ScenePath != "" {
		return sceneToNavMeshPath(e.ScenePath)
	}
	if e.FilePath != "" {
		return sceneToNavMeshPath(filepath.Base(e.FilePath))
	}
	return ""
}

func sceneToNavMeshPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	lower := strings.ToLower(p)
	for _, suf := range []string{".scene.yaml", ".scene.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(lower, suf) {
			return p[:len(p)-len(suf)] + ".navmesh.yaml"
		}
	}
	return p + ".navmesh.yaml"
}

func (e *Editor) resetNavMeshForScene() {
	e.navMesh3D.setFile(nil)
	e.navMesh3D.lastError = ""
	e.navMesh3D.lastPath = ""
	if e.navMesh3D.config == (NavMeshBakeConfig{}) {
		e.navMesh3D.config = DefaultNavMeshBakeConfig()
	}
	e.navMesh3D.config.OutputPath = e.derivedNavMeshPath()
}

func (e *Editor) syncNavMeshForProject() {
	if e.navMesh3D.config == (NavMeshBakeConfig{}) {
		e.navMesh3D.config = DefaultNavMeshBakeConfig()
	}
	if e.Project != nil && e.Project.Navigation != nil {
		n := e.Project.Navigation
		if n.CellSize > 0 {
			e.navMesh3D.config.CellSize = n.CellSize
		}
		if n.CellHeight > 0 {
			e.navMesh3D.config.CellHeight = n.CellHeight
		}
		if len(n.Agents) > 0 {
			a := n.Agents[0]
			if a.Name != "" {
				e.navMesh3D.config.AgentName = a.Name
			}
			if a.Radius > 0 {
				e.navMesh3D.config.Radius = a.Radius
			}
			if a.Height > 0 {
				e.navMesh3D.config.Height = a.Height
			}
			if a.MaxClimb > 0 {
				e.navMesh3D.config.MaxClimb = a.MaxClimb
			}
			if a.MaxSlope > 0 {
				e.navMesh3D.config.MaxSlope = a.MaxSlope
			}
		}
	}
	if p := e.derivedNavMeshPath(); p != "" {
		e.navMesh3D.config.OutputPath = p
	}
	e.tryAutoloadNavMesh3D()
}

func (e *Editor) tryAutoloadNavMesh3D() {
	if e == nil || e.Doc == nil || strings.TrimSpace(e.Doc.NavMesh) == "" {
		return
	}
	fsys := e.navMeshAssetFS()
	if fsys == nil {
		return
	}
	rel := strings.TrimSpace(e.Doc.NavMesh)
	if !fs.ValidPath(rel) {
		return
	}
	f, err := navbake3d.LoadFS(fsys, rel)
	if err != nil {
		return
	}
	e.navMesh3D.setFile(f)
	e.navMesh3D.lastPath = rel
	e.navMesh3D.lastError = ""
}

// LoadNavMesh3D reads and parses the scene's navMesh file (SceneDoc.NavMesh) from the open project
// (or scene directory / NavMesh3DAssetFS) and loads it into the 3D viewport preview overlay.
func (e *Editor) LoadNavMesh3D() (*navbake3d.File, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: nil scene document")
	}
	rel := strings.TrimSpace(e.Doc.NavMesh)
	if rel == "" {
		return nil, errors.New("editor: scene has no navMesh path set")
	}
	if !validScenePath(rel) || strings.ContainsRune(rel, '\\') {
		return nil, fmt.Errorf("editor: invalid navMesh path %q", rel)
	}
	fsys := e.navMeshAssetFS()
	if fsys == nil {
		return nil, errors.New("editor: no project or directory open to load navMesh from")
	}
	f, err := navbake3d.LoadFS(fsys, rel)
	if err != nil {
		e.navMesh3D.lastError = err.Error()
		e.notify()
		return nil, err
	}
	e.navMesh3D.setFile(f)
	e.navMesh3D.lastPath = rel
	e.navMesh3D.lastError = ""
	e.Status = "Loaded navmesh " + rel
	e.notify()
	return f, nil
}

// resolveNavMeshOutputPath validates outPath as a slash-separated relative path strictly inside rootDir,
// refusing reserved project file names (game.yaml, nsgo.yaml, game.index.yaml, go.mod), absolute paths,
// path traversal, and symlinks resolving outside rootDir.
func resolveNavMeshOutputPath(rootDir, outPath string) (rel, abs string, err error) {
	if strings.ContainsRune(outPath, '\\') || filepath.IsAbs(outPath) || !validScenePath(outPath) {
		return "", "", fmt.Errorf("editor: invalid navMesh path %q", outPath)
	}
	rel = path.Clean(outPath)
	if !validScenePath(rel) {
		return "", "", fmt.Errorf("editor: invalid navMesh path %q", outPath)
	}
	if base := path.Base(rel); base == project.FileName || base == project.NSGoFileName || base == refs.IndexFileName || base == "go.mod" {
		return "", "", fmt.Errorf("editor: %q is a reserved project file name, not a navmesh output path", rel)
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
			return "", "", fmt.Errorf("editor: navMesh output path %q must not be a symlink", rel)
		}
		if fi.IsDir() {
			return "", "", fmt.Errorf("editor: navMesh output path %q is a directory", rel)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}

	// Verify the deepest existing ancestor directory resolves inside resolvedRoot so
	// os.MkdirAll and writeFileAtomic cannot escape via symlinks.
	cur := filepath.Dir(abs)
	for {
		resolvedCur, err := filepath.EvalSymlinks(cur)
		if err == nil {
			checkRel, err := filepath.Rel(resolvedRoot, resolvedCur)
			if err != nil || checkRel == ".." || strings.HasPrefix(checkRel, ".."+string(filepath.Separator)) || filepath.IsAbs(checkRel) {
				return "", "", fmt.Errorf("editor: navMesh output path %q escapes root via symlink", rel)
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

// BakeNavMesh3D bakes a 3D navigation mesh from the open scene's static geometry (glTF models,
// built-in 3D primitives, static 3D physics colliders, and any geometry supplied by
// OnBakeNavMesh3DGeometry) using the current NavMeshBakeConfig.
//
// When an output path resolves inside the open project (or beside FilePath),
// the deterministic .navmesh.yaml file is written atomically and SceneDoc.NavMesh is linked via
// History as an undoable edit when unset or changed. The baked file is loaded into the 3D viewport
// preview overlay immediately.
func (e *Editor) BakeNavMesh3D() (*navbake3d.File, error) {
	if e == nil || e.Doc == nil {
		return nil, errors.New("editor: nil scene document")
	}
	if e.playing {
		return nil, ErrPlaying
	}

	fail := func(err error) (*navbake3d.File, error) {
		e.navMesh3D.lastError = err.Error()
		e.Status = "NavMesh bake failed: " + err.Error()
		e.notify()
		return nil, err
	}

	cfg := e.NavMeshBakeConfig()
	agentName := strings.TrimSpace(cfg.AgentName)
	if agentName == "" {
		agentName = project.DefaultNavAgent
	}

	// Resolve and validate the output path and scene link before baking and writing to disk.
	outPath := strings.TrimSpace(cfg.OutputPath)
	if outPath == "" {
		outPath = e.derivedNavMeshPath()
	}
	var writeAbs, writtenPath, linkPath string
	inProject := false
	if outPath != "" {
		switch {
		case e.Project != nil && e.ProjectPath != "":
			rel, abs, err := resolveNavMeshOutputPath(filepath.Dir(e.ProjectPath), outPath)
			if err != nil {
				return fail(err)
			}
			writeAbs = abs
			writtenPath = rel
			linkPath = rel
			inProject = true
		case e.FilePath != "":
			rel, abs, err := resolveNavMeshOutputPath(filepath.Dir(e.FilePath), outPath)
			if err != nil {
				return fail(err)
			}
			writeAbs = abs
			writtenPath = rel
			linkPath = rel
		default:
			if filepath.IsAbs(outPath) || strings.ContainsRune(outPath, '\\') || !validScenePath(outPath) {
				return fail(fmt.Errorf("editor: invalid navMesh path %q", outPath))
			}
		}
	}
	if linkPath != "" && e.Doc.NavMesh != linkPath {
		if err := e.validateSceneEdit(func(d *sceneformat.SceneDoc) {
			d.NavMesh = linkPath
		}); err != nil {
			return fail(err)
		}
	}

	settings := navbake3d.Settings{
		CellSize:   cfg.CellSize,
		CellHeight: cfg.CellHeight,
	}
	if e.Project != nil && e.Project.Navigation != nil {
		n := e.Project.Navigation
		settings.MinRegionArea = n.MinRegionArea
		settings.MaxEdgeLength = n.MaxEdgeLength
		settings.MaxSimplificationError = n.MaxSimplificationError
		settings.MaxVertsPerPoly = n.MaxVertsPerPoly
		settings.DetailSampleDistance = n.DetailSampleDistance
		settings.DetailSampleMaxError = n.DetailSampleMaxError
	}

	primaryAgent := navbake3d.AgentType{
		Name:     agentName,
		Radius:   cfg.Radius,
		Height:   cfg.Height,
		MaxSlope: cfg.MaxSlope,
		MaxClimb: cfg.MaxClimb,
	}
	agents := []navbake3d.AgentType{primaryAgent}
	if e.Project != nil && e.Project.Navigation != nil {
		for _, a := range e.Project.Navigation.Agents {
			if a.Name == "" || a.Name == agentName {
				continue
			}
			agents = append(agents, navbake3d.AgentType{
				Name:     a.Name,
				Radius:   a.Radius,
				Height:   a.Height,
				MaxSlope: a.MaxSlope,
				MaxClimb: a.MaxClimb,
			})
		}
	}

	var g navbake3d.Geometry
	if err := g.AddSceneFile(e.Doc, e.navMeshAssetFS()); err != nil {
		return fail(err)
	}
	if e.OnBakeNavMesh3DGeometry != nil {
		if err := e.OnBakeNavMesh3DGeometry(&g); err != nil {
			return fail(err)
		}
	}

	file, err := navbake3d.Bake(&g, settings, agents)
	if err != nil {
		return fail(err)
	}
	yml, err := navbake3d.Encode(file)
	if err != nil {
		return fail(err)
	}

	if writeAbs != "" {
		if err := os.MkdirAll(filepath.Dir(writeAbs), 0o755); err != nil {
			return fail(err)
		}
		if err := writeFileAtomic(writeAbs, yml); err != nil {
			return fail(err)
		}
		if inProject {
			e.filesRev++
		}
	}

	e.navMesh3D.setFile(file)
	e.navMesh3D.lastError = ""
	e.navMesh3D.lastPath = writtenPath
	if linkPath != "" {
		e.navMesh3D.config.OutputPath = linkPath
		if e.Doc.NavMesh != linkPath {
			if err := e.SetSceneNavMesh(linkPath); err != nil {
				return fail(err)
			}
		}
	}

	polyCount, vertCount := 0, 0
	if m := e.NavMesh3DMesh(); m != nil {
		polyCount = len(m.Polygons)
		vertCount = len(m.Vertices)
	}
	if writtenPath != "" {
		e.Status = fmt.Sprintf("Baked 3D navmesh (%d polygons, %d vertices) -> %s", polyCount, vertCount, writtenPath)
	} else {
		e.Status = fmt.Sprintf("Baked 3D navmesh (%d polygons, %d vertices)", polyCount, vertCount)
	}
	e.notify()
	return file, nil
}

// drawNavMeshOverlay3D renders the active baked 3D navigation mesh over the 3D viewport:
// translucent filled walkable triangles, subtle detail edges, and crisp polygon boundary outlines.
func (e *Editor) drawNavMeshOverlay3D(d *core.Drawing, w, h float32) {
	if !e.ShowNavMesh3D || d == nil {
		return
	}
	mesh := e.NavMesh3DMesh()
	if mesh == nil || len(mesh.Polygons) == 0 {
		return
	}

	const lift float32 = 0.05

	// 1. Translucent walkable fill for each detail triangle (clipped against camera near plane).
	d.Save().SetFillColor(core.RGBA(0x22D3EE, 0.24)).BeginPath()
	hasFill := false
	var tri [3][3]float32
	for pi := range mesh.Polygons {
		p := &mesh.Polygons[pi]
		for _, t := range p.Triangles {
			v := mesh.TriangleVertices(p, t)
			tri[0] = [3]float32{v[0].X, v[0].Y + lift, v[0].Z}
			tri[1] = [3]float32{v[1].X, v[1].Y + lift, v[1].Z}
			tri[2] = [3]float32{v[2].X, v[2].Y + lift, v[2].Z}
			pts := e.Camera3D.projectTriangle(tri, w, h)
			if len(pts) < 3 {
				continue
			}
			d.MoveTo(pts[0][0], pts[0][1])
			for _, pt := range pts[1:] {
				d.LineTo(pt[0], pt[1])
			}
			d.ClosePath()
			hasFill = true
		}
	}
	if hasFill {
		d.Fill()
	}
	d.Restore()

	// 2. Detail edges and polygon boundary outlines via cached navbake3d.Mesh.DebugLines.
	if e.navMesh3D.cachedMesh != mesh {
		e.navMesh3D.cachedMesh = mesh
		e.navMesh3D.cachedAllLines = mesh.DebugLines(false)
		e.navMesh3D.cachedOutlines = len(mesh.DebugLines(true))
	}
	allLines := e.navMesh3D.cachedAllLines
	outlines := e.navMesh3D.cachedOutlines

	drawLines := func(segs [][2]math3d.Vec3, col core.Color, lw float32) {
		if len(segs) == 0 {
			return
		}
		d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath()
		hasSeg := false
		for _, seg := range segs {
			a := [3]float32{seg[0].X, seg[0].Y + lift, seg[0].Z}
			b := [3]float32{seg[1].X, seg[1].Y + lift, seg[1].Z}
			if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(a, b, w, h); vis {
				d.MoveTo(x0, y0).LineTo(x1, y1)
				hasSeg = true
			}
		}
		if hasSeg {
			d.Stroke()
		}
		d.Restore()
	}

	if outlines < len(allLines) {
		drawLines(allLines[outlines:], core.RGBA(0x0E7490, 0.55), 1.0)
	}
	if outlines > 0 {
		drawLines(allLines[:outlines], core.RGBA(0x22D3EE, 0.95), 1.75)
	}
}

// projectTriangle transforms a world-space 3D triangle into camera space, clips it against the
// near plane, and projects the resulting convex polygon (3 or 4 vertices) into viewport pixels,
// matching projectSegment in both perspective and orthographic modes.
func (c OrbitCamera3D) projectTriangle(tri [3][3]float32, w, h float32) [][2]float32 {
	if w <= 0 || h <= 0 {
		return nil
	}
	_, _, near, far := c.effectiveParams()
	eye := c.eye64()
	right, up, forward := c.basis64()
	clipNear := near + 1e-3

	var cam [3][3]float64
	allBehind := true
	allBeyond := true
	for i := range 3 {
		rel := sub64(vec64(tri[i]), eye)
		cam[i] = [3]float64{dot64(rel, right), dot64(rel, up), dot64(rel, forward)}
		if cam[i][2] > clipNear {
			allBehind = false
		}
		if cam[i][2] < far {
			allBeyond = false
		}
	}
	if allBehind || allBeyond {
		return nil
	}

	// Sutherland-Hodgman clip against z >= clipNear (at most 4 output vertices for a triangle).
	clipped := make([][3]float64, 0, 4)
	for i := range 3 {
		a := cam[i]
		b := cam[(i+1)%3]
		aIn := a[2] >= clipNear
		bIn := b[2] >= clipNear
		if aIn {
			clipped = append(clipped, a)
		}
		if aIn != bIn {
			t := (clipNear - a[2]) / (b[2] - a[2])
			clipped = append(clipped, lerp64(a, b, t))
		}
	}
	if len(clipped) < 3 {
		return nil
	}

	out := make([][2]float32, 0, len(clipped))
	for _, p := range clipped {
		sx, sy := c.screen64(p, w, h)
		if math.IsNaN(float64(sx)) || math.IsInf(float64(sx), 0) || math.IsNaN(float64(sy)) || math.IsInf(float64(sy), 0) {
			return nil
		}
		out = append(out, [2]float32{sx, sy})
	}
	return out
}
