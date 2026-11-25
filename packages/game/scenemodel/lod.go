package scenemodel

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// PartLOD is one level of detail of a Part, ordered from finest (index 0) to coarsest.
type PartLOD struct {
	// Mesh is the triangle geometry in the space of the node that draws it.
	Mesh render3d.Mesh
	// MeshID identifies Mesh among the meshes of one Built, matching render3d.LODLevel.MeshID.
	MeshID uint32
	// MinScreenRatio is the minimum viewport-height fraction in [0, 1] at which this level is drawn (render3d.LODLevel.MinScreenRatio).
	MinScreenRatio float32
	// MaxDistance is the optional maximum camera distance in world units at which this level is drawn (render3d.LODLevel.MaxDistance).
	MaxDistance float32
	// Triangles and Vertices are the geometry counts of Mesh.
	Triangles uint32
	Vertices  uint32
}

// defaultLODMinScreenRatios are the viewport-height fractions used when LOD levels
// are discovered via the _LOD1/_LOD2 naming convention without an import sidecar.
var defaultLODMinScreenRatios = []float32{0.25, 0.10, 0.04, 0.015}

// DefaultLODScreenRatio returns the default MinScreenRatio threshold for level
// (0-indexed from finest to coarsest) in an LOD chain of count levels. The
// coarsest level (level >= count-1) always returns 0 (unbounded fallback).
func DefaultLODScreenRatio(level, count int) float32 {
	if count <= 1 || level < 0 || level >= count-1 {
		return 0
	}
	if level < len(defaultLODMinScreenRatios) {
		return defaultLODMinScreenRatios[level]
	}
	r := defaultLODMinScreenRatios[len(defaultLODMinScreenRatios)-1]
	for i := len(defaultLODMinScreenRatios); i <= level; i++ {
		r *= 0.5
	}
	return r
}

func meshTriCount(m render3d.Mesh) uint32 {
	if len(m.Indices) > 0 {
		return uint32(len(m.Indices) / 3)
	}
	return uint32(len(m.Positions) / 3)
}

func partLODEntry(p Part, minScreenRatio, maxDistance float32) PartLOD {
	return PartLOD{
		Mesh:           p.Mesh,
		MeshID:         p.MeshID,
		MinScreenRatio: minScreenRatio,
		MaxDistance:    maxDistance,
		Triangles:      meshTriCount(p.Mesh),
		Vertices:       uint32(len(p.Mesh.Positions)),
	}
}

func attachPartLODs(baseParts []Part, coarseParts [][]Part, ratios []float32, dists []float32) {
	if len(baseParts) == 0 || len(coarseParts) == 0 {
		return
	}
	for pi := range baseParts {
		lods := make([]PartLOD, 0, 1+len(coarseParts))
		lods = append(lods, partLODEntry(baseParts[pi], ratios[0], dists[0]))
		for k, cparts := range coarseParts {
			if pi >= len(cparts) {
				continue
			}
			lods = append(lods, partLODEntry(cparts[pi], ratios[k+1], dists[k+1]))
		}
		if len(lods) < 2 {
			continue
		}
		// Ensure the last level is reachable even if intermediate levels dropped out.
		levels := make([]render3d.LODLevel, len(lods))
		for i, lv := range lods {
			levels[i] = render3d.LODLevel{
				MeshID:         lv.MeshID,
				MaxDistance:    lv.MaxDistance,
				MinScreenRatio: lv.MinScreenRatio,
				Triangles:      lv.Triangles,
				Vertices:       lv.Vertices,
			}
		}
		baseParts[pi].LODs = lods
		baseParts[pi].lodLevels = levels
	}
}

func (b *builder) loadImportSettings(modelPath string) (*importsettings.File, error) {
	if b.sidecarLoaded[modelPath] {
		return b.sidecars[modelPath], nil
	}
	isr, ok := b.models.(ImportSettingsResolver)
	if !ok {
		b.sidecarLoaded[modelPath] = true
		return nil, nil
	}
	f, err := isr.ImportSettings(modelPath)
	if err != nil {
		return nil, err
	}
	b.sidecars[modelPath] = f
	b.sidecarLoaded[modelPath] = true
	return f, nil
}

func hasExplicitLODTargets(lods []importsettings.ModelLOD) bool {
	for _, lv := range lods {
		if lv.Model != "" || lv.Node != "" || lv.Mesh != "" {
			return true
		}
	}
	return false
}

type lodCandidate struct {
	idx   int
	level int
}

// groupSiblingLODs inspects a list of sibling glTF node indices and pairs nodes
// that share a base name under the _LOD0/_LOD1/_LOD2 convention.
//
// primary maps each finest (level-0) node index to its coarser sibling node indices
// in ascending LOD order. consumed marks those coarser sibling node indices so they
// are not emitted as separate scene nodes.
func groupSiblingLODs(doc *gltf.Document, siblings []int) (primary map[int][]int, consumed map[int]bool) {
	type group struct {
		cands []lodCandidate
	}
	byBase := make(map[string]*group)
	var order []string

	for _, ni := range siblings {
		if ni < 0 || ni >= len(doc.Nodes) {
			continue
		}
		gn := &doc.Nodes[ni]
		if gn.Name == "" || gn.Mesh < 0 || gn.Mesh >= len(doc.Meshes) {
			continue
		}
		base, lv, ok := gltf.ParseLODNodeName(gn.Name)
		if !ok {
			base = gn.Name
			lv = 0
		}
		g := byBase[base]
		if g == nil {
			g = &group{}
			byBase[base] = g
			order = append(order, base)
		}
		g.cands = append(g.cands, lodCandidate{idx: ni, level: lv})
	}

	for _, base := range order {
		g := byBase[base]
		if len(g.cands) < 2 {
			continue
		}
		// Only treat as an LOD group when at least one member has an explicit _LOD<N> suffix
		// and distinct LOD levels are present.
		hasSuffixed := false
		for _, c := range g.cands {
			if _, _, ok := gltf.ParseLODNodeName(doc.Nodes[c.idx].Name); ok {
				hasSuffixed = true
				break
			}
		}
		if !hasSuffixed {
			continue
		}
		slices.SortStableFunc(g.cands, func(a, b lodCandidate) int {
			return cmp.Compare(a.level, b.level)
		})
		// Deduplicate equal level numbers, keeping the first node at each level.
		var levels []lodCandidate
		for _, c := range g.cands {
			if len(levels) > 0 && levels[len(levels)-1].level == c.level {
				continue
			}
			levels = append(levels, c)
		}
		if len(levels) < 2 {
			continue
		}
		if primary == nil {
			primary = make(map[int][]int)
			consumed = make(map[int]bool)
		}
		finest := levels[0].idx
		coarse := make([]int, len(levels)-1)
		for i, c := range levels[1:] {
			coarse[i] = c.idx
			consumed[c.idx] = true
		}
		primary[finest] = coarse
	}
	return primary, consumed
}

// findSiblingLODsForNode finds coarser _LOD1/_LOD2 sibling nodes for node ni in doc.
func findSiblingLODsForNode(doc *gltf.Document, ni int) []int {
	if ni < 0 || ni >= len(doc.Nodes) {
		return nil
	}
	var siblings []int
	if parent := doc.Nodes[ni].Parent; parent >= 0 && parent < len(doc.Nodes) {
		siblings = doc.Nodes[parent].Children
	} else {
		siblings = sceneRoots(doc)
	}
	primary, _ := groupSiblingLODs(doc, siblings)
	return primary[ni]
}

// findLODMeshes finds coarser _LOD1/_LOD2 meshes matching mesh mi's base name in doc.Meshes.
func findLODMeshes(doc *gltf.Document, mi int) []int {
	if mi < 0 || mi >= len(doc.Meshes) {
		return nil
	}
	name := doc.Meshes[mi].Name
	if name == "" {
		return nil
	}
	base, baseLv, ok := gltf.ParseLODNodeName(name)
	if !ok {
		base = name
		baseLv = 0
	}
	var cands []lodCandidate
	for i := range doc.Meshes {
		if i == mi {
			continue
		}
		cb, clv, cok := gltf.ParseLODNodeName(doc.Meshes[i].Name)
		if cok && cb == base && clv > baseLv {
			cands = append(cands, lodCandidate{idx: i, level: clv})
		}
	}
	if len(cands) == 0 {
		return nil
	}
	slices.SortStableFunc(cands, func(a, b lodCandidate) int {
		return cmp.Compare(a.level, b.level)
	})
	var out []int
	lastLv := baseLv
	for _, c := range cands {
		if c.level == lastLv {
			continue
		}
		lastLv = c.level
		out = append(out, c.idx)
	}
	return out
}

func resolveThresholds(count int, sidecarLODs []importsettings.ModelLOD) (ratios, dists []float32) {
	ratios = make([]float32, count)
	dists = make([]float32, count)
	if len(sidecarLODs) > 0 {
		for i := range count {
			if i < len(sidecarLODs) {
				ratios[i] = sidecarLODs[i].MinScreenRatio
				dists[i] = sidecarLODs[i].MaxDistance
			} else {
				ratios[i] = DefaultLODScreenRatio(i, count)
			}
		}
		return ratios, dists
	}
	for i := range count {
		ratios[i] = DefaultLODScreenRatio(i, count)
	}
	return ratios, dists
}

// firstDrawableNode returns the first node index in doc's default scene (depth-first) that carries a valid mesh, or -1.
func firstDrawableNode(doc *gltf.Document) int {
	var found = -1
	var walk func(ni int)
	walk = func(ni int) {
		if found >= 0 || ni < 0 || ni >= len(doc.Nodes) {
			return
		}
		if doc.Nodes[ni].Mesh >= 0 && doc.Nodes[ni].Mesh < len(doc.Meshes) {
			found = ni
			return
		}
		for _, c := range doc.Nodes[ni].Children {
			walk(c)
		}
	}
	for _, r := range sceneRoots(doc) {
		walk(r)
	}
	return found
}

// resolveSidecarLODParts builds the geometry parts for one explicit importsettings.ModelLOD entry.
func (g *gltfObject) resolveSidecarLODParts(modelPath string, lv importsettings.ModelLOD, lodIdx int, fallbackBase string) ([]Part, error) {
	targetDoc := g.doc
	targetPath := modelPath
	if lv.Model != "" {
		d, err := g.loadModel(lv.Model)
		if err != nil {
			return nil, fmt.Errorf("lod %d: %w", lodIdx, err)
		}
		targetDoc = d
		targetPath = lv.Model
	}
	sub := gltfObject{builder: g.builder, doc: targetDoc, object: g.object, override: g.override}
	switch {
	case lv.Mesh != "":
		mi := FindMesh(targetDoc, lv.Mesh)
		if mi < 0 {
			return nil, fmt.Errorf("lod %d: model %q has no mesh %q", lodIdx, targetPath, lv.Mesh)
		}
		return sub.meshParts(mi, nil)
	case lv.Node != "":
		ni := FindNode(targetDoc, lv.Node)
		if ni < 0 {
			return nil, fmt.Errorf("lod %d: model %q has no node %q", lodIdx, targetPath, lv.Node)
		}
		return sub.nodeFirstMeshParts(ni, targetPath, lodIdx)
	case lv.Model != "":
		ni := firstDrawableNode(targetDoc)
		if ni < 0 {
			if len(targetDoc.Meshes) == 0 {
				return nil, fmt.Errorf("lod %d: model %q has no mesh", lodIdx, targetPath)
			}
			return sub.meshParts(0, nil)
		}
		gn := &targetDoc.Nodes[ni]
		return sub.meshParts(gn.Mesh, gn.Weights)
	default:
		if fallbackBase != "" {
			want := fmt.Sprintf("%s_LOD%d", fallbackBase, lodIdx)
			if ni := FindNode(targetDoc, want); ni >= 0 {
				return sub.nodeFirstMeshParts(ni, targetPath, lodIdx)
			}
		}
		return nil, fmt.Errorf("lod %d: missing model, node or mesh target", lodIdx)
	}
}

func (g *gltfObject) nodeFirstMeshParts(ni int, targetPath string, lodIdx int) ([]Part, error) {
	gn := &g.doc.Nodes[ni]
	if gn.Mesh >= 0 && gn.Mesh < len(g.doc.Meshes) {
		return g.meshParts(gn.Mesh, gn.Weights)
	}
	var walk func(idx int) int
	walk = func(idx int) int {
		for _, c := range g.doc.Nodes[idx].Children {
			if g.doc.Nodes[c].Mesh >= 0 && g.doc.Nodes[c].Mesh < len(g.doc.Meshes) {
				return c
			}
			if found := walk(c); found >= 0 {
				return found
			}
		}
		return -1
	}
	if child := walk(ni); child >= 0 {
		cn := &g.doc.Nodes[child]
		return g.meshParts(cn.Mesh, cn.Weights)
	}
	return nil, fmt.Errorf("lod %d: node %q of model %q has no mesh", lodIdx, gn.Name, targetPath)
}

// buildExplicitSidecarLODs builds a model object whose *.import.yaml sidecar explicitly declares LOD targets.
func (g *gltfObject) buildExplicitSidecarLODs(o *sceneformat.ObjectDoc, n *scene3d.Node, lods []importsettings.ModelLOD) error {
	// Mark same-file nodes referenced by coarser LODs so whole-model traversal does not draw them twice.
	consumed := make(map[int]bool)
	for _, lv := range lods[1:] {
		if lv.Model == "" && lv.Node != "" {
			if ni := FindNode(g.doc, lv.Node); ni >= 0 {
				consumed[ni] = true
			}
		}
	}

	// Build LOD 0.
	startDrawables := len(g.built.Drawables)
	lod0 := lods[0]
	var fallbackBase string
	switch {
	case o.ModelMesh != "":
		mi := FindMesh(g.doc, o.ModelMesh)
		if mi < 0 {
			return fmt.Errorf("model %q has no mesh %q", o.Model, o.ModelMesh)
		}
		parts, err := g.meshParts(mi, nil)
		if err != nil {
			return err
		}
		g.attach(o.ID, n, parts)
		fallbackBase = g.doc.Meshes[mi].Name
	case o.ModelNode != "":
		ni := FindNode(g.doc, o.ModelNode)
		if ni < 0 {
			return fmt.Errorf("model %q has no node %q", o.Model, o.ModelNode)
		}
		if err := g.nodeWithConsumed(ni, n, false, consumed, nil); err != nil {
			return err
		}
		fallbackBase = g.doc.Nodes[ni].Name
	case lod0.Model != "" || lod0.Node != "" || lod0.Mesh != "":
		parts, err := g.resolveSidecarLODParts(o.Model, lod0, 0, "")
		if err != nil {
			return err
		}
		g.attach(o.ID, n, parts)
		if lod0.Node != "" {
			fallbackBase = lod0.Node
		} else if lod0.Mesh != "" {
			fallbackBase = lod0.Mesh
		}
	default:
		roots := sceneRoots(g.doc)
		primary, autoConsumed := groupSiblingLODs(g.doc, roots)
		for k, v := range autoConsumed {
			consumed[k] = v
		}
		for _, root := range roots {
			if consumed[root] {
				continue
			}
			if err := g.nodeWithConsumed(root, n, true, consumed, primary); err != nil {
				return err
			}
		}
	}

	if b, _, ok := gltf.ParseLODNodeName(fallbackBase); ok {
		fallbackBase = b
	}

	if len(lods) < 2 || len(g.built.Drawables) == startDrawables {
		return nil
	}

	// Resolve coarser LOD parts (1..len-1).
	coarseParts := make([][]Part, len(lods)-1)
	ratios := make([]float32, len(lods))
	dists := make([]float32, len(lods))
	ratios[0], dists[0] = lods[0].MinScreenRatio, lods[0].MaxDistance
	for k := 1; k < len(lods); k++ {
		parts, err := g.resolveSidecarLODParts(o.Model, lods[k], k, fallbackBase)
		if err != nil {
			return err
		}
		coarseParts[k-1] = parts
		ratios[k], dists[k] = lods[k].MinScreenRatio, lods[k].MaxDistance
	}

	// Attach the explicit LOD chain to the primary drawable built for this object.
	attachPartLODs(g.built.Drawables[startDrawables].Parts, coarseParts, ratios, dists)
	return nil
}
