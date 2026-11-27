package assetcheck

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/refs"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// checkScene checks the scene file at p: that sceneformat parses and validates it, and that what it refers to exists and loads.
func (c *checker) checkScene(p string) {
	c.report.Scenes++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err) // Parse read it, so this does not happen.
		return
	}
	s := &sceneCheck{checker: c, path: p, root: &root, lines: yamlLineCount(data), project: c.projectRoot(path.Dir(p))}
	if doc.Environment != nil && doc.Environment.Sky != nil && doc.Environment.Sky.HDR != "" {
		s.skyHDR(doc.Environment.Sky.HDR, "environment", "sky", "hdr")
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Tilemaps)) {
		s.tilemap(doc.Tilemaps[name], "tilemaps", name)
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		li := strconv.Itoa(i)
		if l.Tilemap != nil && !l.Tilemap.IsRef() {
			s.tilemap(l.Tilemap, "layers", li, "tilemap")
		}
		s.objects(l.Objects, "layers", li, "objects")
	}
	ownLocalWarn, ownDirWarn := s.checkSceneLights(doc)
	registry := c.components
	if registry != nil {
		// Parse has accepted the file, so what the registry adds can only be a component or connection error, which names its line. Its connections are not checked against components that break their types.
		if _, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: registry}); err != nil {
			s.located(err)
			registry = nil
		}
	}
	if doc.RefersToScenes() {
		s.sceneRefs(doc, registry, ownLocalWarn, ownDirWarn)
	}
}

// located records err, a *sceneformat.ComponentError or *sceneformat.ConnectionError that names its line and column in the scene, at that position.
func (s *sceneCheck) located(err error) {
	if ce, ok := errors.AsType[*sceneformat.ComponentError](err); ok {
		e := *ce
		e.File, e.Line, e.Column = "", 0, 0
		s.problemOnObject(s.path, min(ce.Line, s.lines), ce.Column, ce.Object, "%v", &e)
		return
	}
	if ce, ok := errors.AsType[*sceneformat.ConnectionError](err); ok {
		e := *ce
		e.File, e.Line, e.Column = "", 0, 0
		obj := connectionErrorObject(ce)
		s.problemOnObject(s.path, min(ce.Line, s.lines), ce.Column, obj, "%v", &e)
		return
	}
	s.problem(s.path, 0, "%v", err)
}

func connectionErrorObject(ce *sceneformat.ConnectionError) string {
	if ce == nil {
		return ""
	}
	switch ce.Field {
	case "handler", "args":
		if ce.To != "" {
			return ce.To
		}
		return ce.From
	case "event":
		if ce.From != "" {
			return ce.From
		}
		return ce.To
	case "to":
		// "to" errors report a missing target object; attach the Problem to the live source endpoint.
		if ce.From != "" {
			return ce.From
		}
		return ce.To
	case "from":
		// "from" errors report a missing source object; attach the Problem to the live target endpoint.
		if ce.To != "" {
			return ce.To
		}
		return ce.From
	default:
		if ce.Arg != "" && ce.To != "" {
			return ce.To
		}
		if ce.From != "" {
			return ce.From
		}
		return ce.To
	}
}

// sceneRefs checks doc, a scene that instances or inherits other scenes: that it resolves, that its own connections name objects of the resolved scene and, given component types reg, that the components its overrides change keep their types and that every connection of the resolved scene, its own and those the scenes it instances and inherits bring, names events, handlers and arguments its objects declare. A fault a scene brings unchanged from a file it instances or inherits is reported on that file, which is checked on its own; one this scene's overrides cause is reported at the override, so each fault is reported once.
func (s *sceneCheck) sceneRefs(doc *sceneformat.SceneDoc, reg *sceneformat.ComponentRegistry, ownLocalWarn, ownDirWarn bool) {
	projectFS := s.fsys
	if s.project != "." {
		sub, err := fs.Sub(s.fsys, s.project)
		if err != nil {
			s.problem(s.path, 0, "%v", err)
			return
		}
		projectFS = sub
	}
	files := &sceneFiles{fsys: projectFS}
	own := doc.Clone()
	conns := own.Connections
	own.Connections = nil
	resolved, err := sceneformat.Resolve(own, files)
	if err != nil {
		// A scene file that is missing or does not load is reported on its reference or on the file itself.
		if !files.failed {
			s.unresolved(own, files, err)
		}
		return
	}
	byID := make(map[string]*sceneformat.ObjectDoc)
	for li := range resolved.Layers {
		sceneformat.WalkObjects(resolved.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			byID[o.ID] = o
			return true
		})
	}
	overrides := s.overrides(doc)
	for _, ov := range overrides {
		s.withObject(s.path, ov.id, func() {
			if o := byID[ov.id]; o != nil && o.Model != "" && (ov.model != nil || ov.modelNode != nil || ov.modelMesh != nil) {
				s.overrideModel(o, ov)
			}
			if o := byID[ov.id]; o != nil && o.GridMap != "" && ov.gridMap != nil {
				s.gridMap(o.GridMap, s.line(append(slices.Clip(ov.steps), "gridMap")...))
			}
			if ov.reflectionProbe != nil && ov.reflectionProbe.Cubemap != "" {
				s.reflectionProbeCubemap(ov.reflectionProbe.Cubemap, append(slices.Clip(ov.steps), "reflectionProbe", "cubemap")...)
			}
			if ov.lightProbeGrid != nil && ov.lightProbeGrid.Data != "" {
				s.lightProbeGridData(ov.lightProbeGrid.Data, append(slices.Clip(ov.steps), "lightProbeGrid", "data")...)
			}
		})
	}
	if !ownLocalWarn || !ownDirWarn {
		s.checkResolvedSceneLights(doc, resolved, ownLocalWarn, ownDirWarn)
	}
	missing := false
	for i, c := range conns {
		for _, end := range []struct{ field, id, other string }{{"from", c.From, c.To}, {"to", c.To, c.From}} {
			if byID[end.id] == nil {
				missing = true
				line, col := s.pos("connections", strconv.Itoa(i), end.field)
				liveObj := ""
				if byID[end.other] != nil {
					liveObj = end.other
				}
				s.problemOnObject(s.path, line, col, liveObj, "sceneformat: connection %d (%q %s -> %q %s): the scene has no object %q, counting those of the scenes it instances and inherits", i, c.From, c.Event, c.To, c.Handler, end.id)
			}
		}
	}
	if missing || reg == nil {
		return
	}
	for _, ov := range overrides {
		o := byID[ov.id]
		if o == nil {
			continue // Resolve refuses an override that names no object.
		}
		for _, name := range slices.Sorted(maps.Keys(ov.components)) {
			err := reg.CheckComponent(name, o.Components[name])
			if err == nil {
				continue
			}
			ce, ok := errors.AsType[*sceneformat.ComponentError](err)
			if !ok {
				line, col := s.pos(append(slices.Clip(ov.steps), "components", name)...)
				s.problemOnObject(s.path, line, col, ov.id, "sceneformat: object %q component %q: %v", ov.id, name, err)
				continue
			}
			e := *ce
			e.File, e.Line, e.Column, e.Object = "", 0, 0, ov.id
			line, col := s.pos(append(slices.Clip(ov.steps), "components", name, ce.Field)...)
			s.problemOnObject(s.path, line, col, ov.id, "%v", &e)
		}
	}
	s.resolvedConnections(resolved, len(resolved.Connections), conns, overrides, reg)
}

// resolvedConnections checks every connection of resolved, a scene that holds merged connections brought by the scenes it instances and inherits followed by own, its own connections, against reg. A fault in an own connection is reported at the line that names it; one in a brought connection only when an override of this scene changes the object at fault, at that override, since the file the connection comes from reports the others.
func (s *sceneCheck) resolvedConnections(resolved *sceneformat.SceneDoc, merged int, own []sceneformat.ConnectionDoc, overrides []objectOverride, reg *sceneformat.ComponentRegistry) {
	resolved.Connections = append(resolved.Connections, own...)
	// index maps a connection of resolved.Connections to its index before faulty ones were taken out.
	index := make([]int, len(resolved.Connections))
	for i := range index {
		index[i] = i
	}
	// Each pass reports the first fault and takes its connection out, so every faulty connection is reported and the others are still checked against each other.
	for len(resolved.Connections) > 0 {
		err := sceneformat.ValidateConnections(resolved, reg)
		if err == nil {
			return
		}
		ce, ok := errors.AsType[*sceneformat.ConnectionError](err)
		if !ok {
			s.problem(s.path, 0, "%v", err)
			return
		}
		e := *ce
		e.File, e.Line, e.Column = "", 0, 0
		e.Index = index[ce.Index]
		if rest, ok := strings.CutPrefix(e.Msg, duplicatePrefix); ok {
			if j, err := strconv.Atoi(rest); err == nil && j >= 0 && j < len(index) {
				if index[j] >= merged {
					e.Msg = duplicatePrefix + strconv.Itoa(index[j]-merged)
				} else {
					e.Msg = "connects the same event and handler as a connection that a scene it instances or inherits brings"
				}
			}
		}
		if e.Index >= merged {
			e.Index -= merged
			steps := []string{"connections", strconv.Itoa(e.Index), e.Field}
			if e.Arg != "" {
				steps = append(steps, e.Arg)
			}
			line, col := s.pos(steps...)
			s.problemOnObject(s.path, line, col, connectionErrorObject(&e), "%v", &e)
		} else {
			at := e.To
			if e.Field == "from" || e.Field == "event" {
				at = e.From
			}
			if i := slices.IndexFunc(overrides, func(ov objectOverride) bool { return ov.id == at }); i >= 0 {
				arg := ""
				if e.Arg != "" {
					arg = fmt.Sprintf(" argument %q", e.Arg)
				}
				line, col := s.pos(overrides[i].steps...)
				s.problemOnObject(s.path, line, col, at, "sceneformat: the override of object %q breaks connection (%q %s -> %q %s)%s, which a scene it instances or inherits brings: %s", at, e.From, e.Event, e.To, e.Handler, arg, e.Msg)
			}
		}
		resolved.Connections = slices.Delete(resolved.Connections, ce.Index, ce.Index+1)
		index = slices.Delete(index, ce.Index, ce.Index+1)
	}
}

// duplicatePrefix starts the message of a sceneformat.ConnectionError for a connection that repeats an earlier one, which ends with the earlier one's index.
const duplicatePrefix = "connects the same event and handler as connection "

// objectOverride is an override a scene applies to an object of a scene it instances or inherits: the object's ID in the resolved scene, the components it changes, the model, node, mesh and gridMap it sets, nil when it leaves them, and the steps to it in the scene file.
type objectOverride struct {
	id                                   string
	components                           map[string]any
	model, modelNode, modelMesh, gridMap *string
	reflectionProbe                      *sceneformat.ReflectionProbeDoc
	lightProbeGrid                       *sceneformat.LightProbeGridDoc
	steps                                []string
}

// newObjectOverride returns the objectOverride for ov, which changes the object id of the resolved scene and is found at steps in the scene file.
func newObjectOverride(id string, ov *sceneformat.ObjectOverride, steps []string) objectOverride {
	return objectOverride{
		id:              id,
		components:      ov.Components,
		model:           ov.Model,
		modelNode:       ov.ModelNode,
		modelMesh:       ov.ModelMesh,
		gridMap:         ov.GridMap,
		reflectionProbe: ov.ReflectionProbe,
		lightProbeGrid:  ov.LightProbeGrid,
		steps:           steps,
	}
}

// overrides returns the overrides doc applies, those of SceneDoc.Overrides first and then those of its instances in document order, each sorted by ID.
func (s *sceneCheck) overrides(doc *sceneformat.SceneDoc) []objectOverride {
	var out []objectOverride
	for _, id := range slices.Sorted(maps.Keys(doc.Overrides)) {
		ov := doc.Overrides[id]
		out = append(out, newObjectOverride(id, &ov, []string{"overrides", id}))
	}
	walkInstances(doc, func(o *sceneformat.ObjectDoc, steps []string) {
		for _, id := range slices.Sorted(maps.Keys(o.Overrides)) {
			ov := o.Overrides[id]
			out = append(out, newObjectOverride(o.ID+"/"+id, &ov, append(slices.Clip(steps), "overrides", id)))
		}
	})
	return out
}

// unresolved reports err, why own (doc without its connections) does not resolve, at the instance that causes it when resolving each instance alone finds one, and otherwise at the inherits line, or at line 0 for a scene that inherits nothing.
func (s *sceneCheck) unresolved(own *sceneformat.SceneDoc, files *sceneFiles, err error) {
	found := false
	walkInstances(own, func(o *sceneformat.ObjectDoc, steps []string) {
		alone := o.Clone()
		alone.Children = nil
		one := &sceneformat.SceneDoc{Version: own.Version, Layers: []sceneformat.LayerDoc{{Name: "instance", Objects: []sceneformat.ObjectDoc{alone}}}}
		_, ierr := sceneformat.Resolve(one, files)
		if ierr == nil {
			return
		}
		found = true
		field := "scene"
		if len(o.Overrides) > 0 && strings.Contains(ierr.Error(), " overrides ") {
			field = "overrides"
		}
		s.problemOnObject(s.path, s.line(append(slices.Clip(steps), field)...), 0, o.ID, "%v", ierr)
	})
	if found {
		return
	}
	line := 0
	if own.Inherits != "" {
		line = s.line("inherits")
	}
	s.problem(s.path, line, "%v", err)
}

// walkInstances calls fn for every object of doc's own layers, at any depth, that instances a scene file, with the steps to it in the scene file, in document order.
func walkInstances(doc *sceneformat.SceneDoc, fn func(o *sceneformat.ObjectDoc, steps []string)) {
	var walk func(objs []sceneformat.ObjectDoc, steps []string)
	walk = func(objs []sceneformat.ObjectDoc, steps []string) {
		for j := range objs {
			o := &objs[j]
			op := append(slices.Clip(steps), strconv.Itoa(j))
			if o.Scene != "" {
				fn(o, op)
			}
			walk(o.Children, append(slices.Clip(op), "children"))
		}
	}
	for li := range doc.Layers {
		walk(doc.Layers[li].Objects, []string{"layers", strconv.Itoa(li), "objects"})
	}
}

// sceneFiles loads the scenes a scene instances and inherits from a project's files, and records whether one failed to load.
type sceneFiles struct {
	fsys   fs.FS
	failed bool
}

// Scene loads the scene file at p, a path from the project root.
func (r *sceneFiles) Scene(p string) (*sceneformat.SceneDoc, error) {
	doc, err := sceneformat.FSScenes{FS: r.fsys}.Scene(p)
	if err != nil {
		r.failed = true
	}
	return doc, err
}

// sceneLightSummary counts directional and unshadowed local (point/spot) lights in visible layers of a scene, matching scenemodel.Built.SceneLights (at most one shadow-casting point light and one shadow-casting spot light live in rig slot 0, and every other point and spot light goes through render3d.ClusteredLightSystem).
type sceneLightSummary struct {
	directional     int
	firstExcessDir  []string
	firstExcessDirO string
	local           int
	firstExcessLoc  []string
	firstExcessLocO string
}

func countSceneLights(doc *sceneformat.SceneDoc) sceneLightSummary {
	var sum sceneLightSummary
	hasShadowPt, hasShadowSpot := false, false
	var walk func(objs []sceneformat.ObjectDoc, steps []string)
	walk = func(objs []sceneformat.ObjectDoc, steps []string) {
		for j := range objs {
			o := &objs[j]
			op := append(slices.Clip(steps), strconv.Itoa(j))
			if o.Light != nil {
				switch o.Light.Kind {
				case sceneformat.LightDirectional:
					sum.directional++
					if sum.directional == render3d.MaxDirectionalLights+1 {
						sum.firstExcessDir = append(slices.Clip(op), "light")
						sum.firstExcessDirO = o.ID
					}
				case sceneformat.LightPoint:
					if o.Light.Shadows && !hasShadowPt {
						hasShadowPt = true
					} else {
						sum.local++
						if sum.local == render3d.DefaultFallbackMaxLights+1 {
							sum.firstExcessLoc = append(slices.Clip(op), "light")
							sum.firstExcessLocO = o.ID
						}
					}
				case sceneformat.LightSpot:
					if o.Light.Shadows && !hasShadowSpot {
						hasShadowSpot = true
					} else {
						sum.local++
						if sum.local == render3d.DefaultFallbackMaxLights+1 {
							sum.firstExcessLoc = append(slices.Clip(op), "light")
							sum.firstExcessLocO = o.ID
						}
					}
				}
			}
			walk(o.Children, append(slices.Clip(op), "children"))
		}
	}
	for li := range doc.Layers {
		l := &doc.Layers[li]
		if l.Visible != nil && !*l.Visible {
			continue
		}
		walk(l.Objects, []string{"layers", strconv.Itoa(li), "objects"})
	}
	return sum
}

func (s *sceneCheck) reportLightCapWarnings(sum sceneLightSummary, locSteps, dirSteps []string, locObj, dirObj string, reportLocal, reportDir bool) (warnedLocal, warnedDir bool) {
	if reportLocal && sum.local > render3d.DefaultFallbackMaxLights {
		warnedLocal = true
		line, col := 1, 0
		if len(locSteps) > 0 {
			line, col = s.pos(locSteps...)
		}
		fallbackDrop := sum.local - render3d.DefaultFallbackMaxLights
		if sum.local > render3d.DefaultClusterMaxLights {
			clusterDrop := sum.local - render3d.DefaultClusterMaxLights
			s.problemOnObject(s.path, line, col, locObj, "warning: scene has %d local lights (point/spot), exceeding the %d-light fallback cap (%d dropped on targets without storage buffers) and the %d-light clustered cap (%d dropped on clustered targets)", sum.local, render3d.DefaultFallbackMaxLights, fallbackDrop, render3d.DefaultClusterMaxLights, clusterDrop)
		} else {
			s.problemOnObject(s.path, line, col, locObj, "warning: scene has %d local lights (point/spot), exceeding the %d-light fallback cap on targets without storage buffers (%d dropped per frame on WebGL 2 / OpenGL ES 3.0; clustered targets support up to %d)", sum.local, render3d.DefaultFallbackMaxLights, fallbackDrop, render3d.DefaultClusterMaxLights)
		}
	}
	if reportDir && sum.directional > render3d.MaxDirectionalLights {
		warnedDir = true
		line, col := 1, 0
		if len(dirSteps) > 0 {
			line, col = s.pos(dirSteps...)
		}
		dropped := sum.directional - render3d.MaxDirectionalLights
		s.problemOnObject(s.path, line, col, dirObj, "warning: scene has %d directional lights, exceeding the %d-light cap (%d dropped)", sum.directional, render3d.MaxDirectionalLights, dropped)
	}
	return warnedLocal, warnedDir
}

func (s *sceneCheck) checkSceneLights(doc *sceneformat.SceneDoc) (warnedLocal, warnedDir bool) {
	sum := countSceneLights(doc)
	return s.reportLightCapWarnings(sum, sum.firstExcessLoc, sum.firstExcessDir, sum.firstExcessLocO, sum.firstExcessDirO, true, true)
}

func (s *sceneCheck) checkResolvedSceneLights(doc, resolved *sceneformat.SceneDoc, ownLocalWarn, ownDirWarn bool) {
	sum := countSceneLights(resolved)
	var refSteps []string
	var refObj string
	walkInstances(doc, func(o *sceneformat.ObjectDoc, steps []string) {
		if len(refSteps) == 0 {
			refSteps = append(slices.Clip(steps), "scene")
			refObj = o.ID
		}
	})
	if len(refSteps) == 0 && doc.Inherits != "" {
		refSteps = []string{"inherits"}
	}
	s.reportLightCapWarnings(sum, refSteps, refSteps, refObj, refObj, !ownLocalWarn, !ownDirWarn)
}

// objects checks the sprites and materials of objs, found at steps in the scene, and of their descendants.
func (s *sceneCheck) objects(objs []sceneformat.ObjectDoc, steps ...string) {
	for j := range objs {
		o := &objs[j]
		op := append(slices.Clip(steps), strconv.Itoa(j))
		s.withObject(s.path, o.ID, func() {
			if o.Sprite != nil {
				s.sprite(o.Sprite, append(slices.Clip(op), "sprite")...)
			}
			if o.Material != "" {
				s.material(o.Material, append(slices.Clip(op), "material")...)
			}
			if o.Particles != nil {
				s.particlesRef(o.Particles, append(slices.Clip(op), "particles")...)
			}
			if o.Model != "" {
				at := func(key string) int { return s.line(append(slices.Clip(op), key)...) }
				s.model(o.Model, o.ModelNode, o.ModelMesh, at("model"), at("modelNode"), at("modelMesh"), true)
			}
			if o.GridMap != "" {
				s.gridMap(o.GridMap, s.line(append(slices.Clip(op), "gridMap")...))
			}
			if o.ReflectionProbe != nil && o.ReflectionProbe.Cubemap != "" {
				s.reflectionProbeCubemap(o.ReflectionProbe.Cubemap, append(slices.Clip(op), "reflectionProbe", "cubemap")...)
			}
			if o.LightProbeGrid != nil && o.LightProbeGrid.Data != "" {
				s.lightProbeGridData(o.LightProbeGrid.Data, append(slices.Clip(op), "lightProbeGrid", "data")...)
			}
			if o.Components != nil {
				s.animator(o, op...)
			}
		})
		s.objects(o.Children, append(slices.Clip(op), "children")...)
	}
}

// gridMap checks that ref, a 3D grid map file named from the project root, exists and checks it if not already checked.
func (s *sceneCheck) gridMap(ref string, line int) {
	if p, ok := s.fileRef(s.path, line, s.project, ref, "grid map"); ok && !s.checked[p] {
		s.checkGridMap(p)
	}
}

// overrideModel checks the model that o, an object of the resolved scene, draws after ov, an override of this scene that sets its model, node or mesh, changed it. An override that sets the model is checked like an object that names it, a node or mesh it keeps from the object it overrides reported at the model line; one that sets only the node or mesh is checked against the model the overridden object names, whose own faults its scene reports.
func (s *sceneCheck) overrideModel(o *sceneformat.ObjectDoc, ov objectOverride) {
	at := func(key string) int { return s.line(append(slices.Clip(ov.steps), key)...) }
	if ov.model != nil {
		ref, node, mesh := at("model"), at("model"), at("model")
		if ov.modelNode != nil {
			node = at("modelNode")
		}
		if ov.modelMesh != nil {
			mesh = at("modelMesh")
		}
		s.model(o.Model, o.ModelNode, o.ModelMesh, ref, node, mesh, true)
		return
	}
	var node, mesh string
	if ov.modelNode != nil {
		node = o.ModelNode
	}
	if ov.modelMesh != nil {
		mesh = o.ModelMesh
	}
	s.model(o.Model, node, mesh, 0, at("modelNode"), at("modelMesh"), false)
}

// model checks that ref, a glTF model file named from the project root, loads, and that node and mesh, when not "", are a node and a mesh of it, reporting them at refLine, nodeLine and meshLine of the scene. When own is true the scene names ref, and a ref that is not a usable path or names no file is reported at refLine; otherwise the scene that names it reports that and model checks nothing more. A model that does not load is reported against the model file itself.
func (s *sceneCheck) model(ref, node, mesh string, refLine, nodeLine, meshLine int, own bool) {
	var p string
	if own {
		var ok bool
		if p, ok = s.fileRef(s.path, refLine, s.project, ref, "model"); !ok {
			return
		}
	} else {
		var err error
		if p, err = resolve(s.project, ref); err != nil || s.fileMissing(p) != nil {
			return
		}
	}
	doc := s.loadModel(p)
	if doc == nil {
		return
	}
	if node != "" && !slices.ContainsFunc(doc.Nodes, func(n gltf.Node) bool { return n.Name == node }) {
		s.problem(s.path, nodeLine, "modelNode %q is not a node of model %q (%s)", node, ref, p)
	}
	if mesh != "" && !slices.ContainsFunc(doc.Meshes, func(m gltf.Mesh) bool { return m.Name == mesh }) {
		s.problem(s.path, meshLine, "modelMesh %q is not a mesh of model %q (%s)", mesh, ref, p)
	}
}

// loadModel loads the glTF model at p once, recording a problem against it and returning nil when it does not load.
func (c *checker) loadModel(p string) *gltf.Document {
	if doc, ok := c.models[p]; ok {
		return doc
	}
	doc, err := gltf.LoadFS(c.fsys, p)
	if err != nil && c.modelFileMissing(p) {
		// checkRefs reports the missing buffer or image at the line of the .gltf file that names it, which says more than the load error.
		err = nil
		doc = nil
	}
	if err != nil {
		line := 0
		if e, ok := loaderr.As(err); ok {
			// A line counts the JSON of a .gltf file; a .glb is binary and has none.
			if strings.EqualFold(path.Ext(p), ".gltf") {
				line = e.Line
			}
			err = &loaderr.Error{Field: e.Field, Problem: e.Problem, Expected: e.Expected, Fix: e.Fix, Err: e.Err}
		}
		c.problem(p, line, "%v", err)
		doc = nil
	}
	c.models[p] = doc
	return doc
}

// modelFileMissing reports whether a buffer or image file the glTF model at p names (refs.FieldModelBuffer, refs.FieldModelImage) cannot be read.
func (c *checker) modelFileMissing(p string) bool {
	for _, r := range c.refs.Refs {
		if r.File == p && r.Target != "" && (r.Field == refs.FieldModelBuffer || r.Field == refs.FieldModelImage) && c.fileMissing(r.Target) != nil {
			return true
		}
	}
	return false
}

// sceneCheck checks the references of one valid scene.
type sceneCheck struct {
	*checker
	path    string
	root    *yaml.Node
	lines   int
	project string
}

// line returns the line of the field at steps in the scene.
func (s *sceneCheck) line(steps ...string) int { return min(yamlLine(s.root, steps), s.lines) }

// pos returns the 1-based line and column of the field at steps in the scene.
func (s *sceneCheck) pos(steps ...string) (int, int) {
	line, col := yamlPos(s.root, steps)
	if line > s.lines {
		line = s.lines
	}
	return line, col
}

// tilemap checks a tilemap definition at steps: an external source must be a Tiled map or LDtk project that checks, with the level it names, an inline grid's tileset atlas must have a frame for every tile ID it uses, and a layered grid's YAML tileset must check and hold every tile its cells place.
func (s *sceneCheck) tilemap(tm *sceneformat.TilemapDoc, steps ...string) {
	switch {
	case tm.IsExternal():
		line := s.line(append(steps, "source")...)
		ext := strings.ToLower(path.Ext(tm.Source))
		if ext != ".tmj" && ext != ".tmx" && ext != ".ldtk" {
			s.problem(s.path, line, "tilemap source %q is not a Tiled map (.tmj, .tmx) or LDtk project (.ldtk)", tm.Source)
			return
		}
		p, ok := s.fileRef(s.path, line, s.project, tm.Source, "tilemap source")
		if !ok {
			return
		}
		switch ext {
		case ".tmj":
			s.checkTiledMap(p)
		case ".tmx":
			s.checkTMXMap(p)
		case ".ldtk":
			s.checkLDtk(p)
			if tm.Level != "" && !s.broken[p] {
				if data, ok := s.readFile(p); ok {
					if _, err := tilemap.LoadLDtk(data, tm.Level, func(rel string) core.ImageSource { return core.ImageSource{URI: rel} }); err != nil {
						s.problem(s.path, s.line(append(steps, "level")...), "tilemap level %q: %v", tm.Level, err)
					}
				}
			}
		}
	case tm.HasTilesetFile() || tm.IsLayered():
		p, ok := s.fileRef(s.path, s.line(append(steps, "tilesetFile")...), s.project, tm.TilesetFile, "tileset file")
		if !ok {
			return
		}
		doc := s.checkYAMLTileset(p)
		if doc == nil {
			return
		}
		count := uint32(doc.Count())
		if len(tm.TileLayers) > 0 {
			for i := range tm.TileLayers {
				l := &tm.TileLayers[i]
				for r, row := range l.Rows {
					// The scene validates, so every row parses.
					cells, _ := sceneformat.ParseTileRow(row)
					for col, cell := range cells {
						if id := cell & tilemap.TileIDMask; id > count {
							s.problem(s.path, s.line(append(steps, "tileLayers", strconv.Itoa(i), "rows", strconv.Itoa(r))...), "tile layer %q row %d column %d places tile %d, but tileset %q (%s) has %d tiles", l.Name, r, col, id-1, tm.TilesetFile, p, count)
							break
						}
					}
				}
			}
			return
		}
		reported := make(map[int]bool)
		for i, gid := range tm.Data {
			if gid <= 0 || uint32(gid) <= count || reported[gid] {
				continue
			}
			reported[gid] = true
			s.problem(s.path, s.line(append(steps, "data", strconv.Itoa(i))...), "tile %d is outside tileset %q (%s), which has %d tiles", gid, tm.TilesetFile, p, count)
		}
	case tm.IsInline():
		atlas := s.findAtlas(s.path, s.line(append(steps, "tileset")...), s.project, tm.Tileset, "tileset atlas")
		if atlas == nil || atlas.frames == nil {
			return
		}
		reported := make(map[int]bool)
		for i, gid := range tm.Data {
			if gid == 0 || reported[gid] || atlas.frames[strconv.Itoa(gid)] {
				continue
			}
			reported[gid] = true
			s.problem(s.path, s.line(append(steps, "data", strconv.Itoa(i))...), "tile %d has no frame %q in tileset atlas %q (%s)", gid, strconv.Itoa(gid), tm.Tileset, atlas.path)
		}
	}
}

// checkYAMLTileset checks the YAML tileset at p once, as the package comment describes, and returns it, or nil when it does not parse.
func (c *checker) checkYAMLTileset(p string) *tilemap.TilesetDoc {
	if doc, ok := c.tilesetDocs[p]; ok {
		return doc
	}
	c.tilesetDocs[p] = nil
	c.report.Tilesets++
	data, ok := c.readFile(p)
	if !ok {
		return nil
	}
	doc, err := tilemap.ParseTileset(data)
	if err != nil {
		c.yamlError(p, data, err)
		return nil
	}
	c.tilesetDocs[p] = doc
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err) // ParseTileset read it, so this does not happen.
		return doc
	}
	lines := yamlLineCount(data)
	line := func(steps ...string) int { return min(yamlLine(&root, steps), lines) }
	proj := c.projectRoot(path.Dir(p))
	if doc.Image != "" {
		img := c.imageRef(p, line("image"), proj, doc.Image, "tileset image")
		if img != nil && img.sized {
			rows := (doc.TileCount + doc.Columns - 1) / doc.Columns
			needW := 2*doc.Margin + doc.Columns*doc.TileWidth + (doc.Columns-1)*doc.Spacing
			needH := 2*doc.Margin + rows*doc.TileHeight + (rows-1)*doc.Spacing
			if img.width < needW || img.height < needH {
				c.problem(p, line("image"), "tileset image %q is %dx%d, smaller than the %dx%d that %d columns of %d tiles of %dx%d need", doc.Image, img.width, img.height, needW, needH, doc.Columns, doc.TileCount, doc.TileWidth, doc.TileHeight)
			}
		}
		return doc
	}
	atlas := c.findAtlas(p, line("atlas"), proj, doc.Atlas, "tileset atlas")
	if atlas == nil || atlas.frames == nil {
		return doc
	}
	for i, name := range doc.Frames {
		if !atlas.frames[name] {
			c.problem(p, line("frames", strconv.Itoa(i)), "frame %q is not in tileset atlas %q (%s)", name, doc.Atlas, atlas.path)
		}
	}
	return doc
}

// sprite checks that a sprite's atlas exists and has its frame.
func (s *sceneCheck) sprite(ref *sceneformat.SpriteRef, steps ...string) {
	atlas := s.findAtlas(s.path, s.line(append(steps, "atlas")...), s.project, ref.Atlas, "atlas")
	if atlas == nil || atlas.frames == nil {
		return
	}
	if !atlas.frames[ref.Frame] {
		line, col := s.pos(append(steps, "frame")...)
		s.problemAt(s.path, line, col, "frame %q is not in atlas %q (%s)", ref.Frame, ref.Atlas, atlas.path)
	}
}

// skyHDR checks that an HDR sky panorama exists in the project and decodes as a Radiance .hdr image, reporting a decode error against the HDR file itself.
func (s *sceneCheck) skyHDR(ref string, steps ...string) {
	line := s.line(steps...)
	p, ok := s.fileRef(s.path, line, s.project, ref, "sky hdr")
	if !ok || s.hdrs[p] {
		return
	}
	s.hdrs[p] = true
	data, ok := s.readFile(p)
	if !ok {
		return
	}
	if _, err := render3d.DecodeHDR(bytes.NewReader(data)); err != nil {
		s.problem(p, 0, "%v", err)
	}
}

// reflectionProbeCubemap checks that a reflection probe's baked HDR cubemap exists and decodes as a valid cubemap strip or equirectangular HDR image.
func (s *sceneCheck) reflectionProbeCubemap(ref string, steps ...string) {
	line := s.line(steps...)
	p, ok := s.fileRef(s.path, line, s.project, ref, "reflection probe cubemap")
	if !ok || s.reflProbes[p] {
		return
	}
	s.reflProbes[p] = true
	data, ok := s.readFile(p)
	if !ok {
		return
	}
	if _, err := render3d.DecodeCubemapHDR(bytes.NewReader(data)); err != nil {
		s.problem(p, 0, "%v", err)
	}
}

// lightProbeGridData checks that a light probe grid's baked YAML file exists and parses cleanly with render3d.ParseLightProbeGrid.
func (s *sceneCheck) lightProbeGridData(ref string, steps ...string) {
	line := s.line(steps...)
	p, ok := s.fileRef(s.path, line, s.project, ref, "light probe grid")
	if !ok || s.probeGrids[p] {
		return
	}
	s.probeGrids[p] = true
	data, ok := s.readFile(p)
	if !ok {
		return
	}
	if _, err := render3d.ParseLightProbeGrid(data); err != nil {
		s.yamlError(p, data, err)
	}
}

// material checks that a material file exists in the project and holds a valid material, reporting a fault inside the file against the file itself.
func (s *sceneCheck) material(ref string, steps ...string) {
	line := s.line(steps...)
	p, ok := s.fileRef(s.path, line, s.project, ref, "material")
	if !ok || s.materials[p] {
		return
	}
	s.materials[p] = true
	data, ok := s.readFile(p)
	if !ok {
		return
	}
	mat, err := resource.ParseAs[*resource.Material](data)
	if err != nil {
		s.yamlError(p, data, err)
		return
	}
	if mat.Texture != "" {
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			s.yamlError(p, data, err) // ParseAs read it, so this does not happen.
			return
		}
		s.imageRef(p, yamlLine(&root, []string{"texture"}), s.project, mat.Texture, "texture")
	}
}

// particlesRef checks that an object's particle emitter file exists and parses with particles.UnmarshalConfigYAML, and when an atlas is named, that it exists and contains every frame in the emitter's frameNames.
func (s *sceneCheck) particlesRef(ref *sceneformat.ParticlesRef, steps ...string) {
	fileLine := s.line(append(slices.Clip(steps), "file")...)
	p, ok := s.fileRef(s.path, fileLine, s.project, ref.File, "particles")
	if !ok {
		return
	}
	cfg := s.loadParticles(p)
	if cfg == nil || ref.Atlas == "" {
		return
	}
	atlasLine := s.line(append(slices.Clip(steps), "atlas")...)
	atlas := s.findAtlas(s.path, atlasLine, s.project, ref.Atlas, "particles atlas")
	if atlas == nil || atlas.frames == nil {
		return
	}
	var checkFrames func(c particles.Config)
	checkFrames = func(c particles.Config) {
		for _, fn := range c.FrameNames {
			if !atlas.frames[fn] {
				s.problem(s.path, atlasLine, "frame %q of particles %q is not in atlas %q (%s)", fn, ref.File, ref.Atlas, atlas.path)
			}
		}
		for _, sub := range c.SubEmitters {
			checkFrames(sub.Config)
		}
	}
	checkFrames(*cfg)
}

func (c *checker) loadParticles(p string) *particles.Config {
	if cfg, ok := c.particles[p]; ok {
		return cfg
	}
	c.particles[p] = nil
	data, ok := c.readFile(p)
	if !ok {
		return nil
	}
	cfg, err := particles.UnmarshalConfigYAML(data)
	if err != nil {
		c.yamlError(p, data, err)
		return nil
	}
	c.particles[p] = &cfg
	return &cfg
}

// yamlLineRE finds the "line N: " that yaml's errors carry.
var yamlLineRE = regexp.MustCompile(`line (\d+): `)

// yamlError records err, from parsing the YAML file at p, at the line and column it is about: the field a sceneformat.FieldError names, each line a yaml.TypeError lists, the line a syntax error states, or the line and column a *loaderr.Error gives.
func (c *checker) yamlError(p string, data []byte, err error) {
	if fe, ok := errors.AsType[*sceneformat.FieldError](err); ok {
		var root yaml.Node
		line, col := 0, 0
		obj := ""
		if yaml.Unmarshal(data, &root) == nil {
			line, col = yamlPos(&root, fe.Path)
			line = min(line, yamlLineCount(data))
			obj = yamlFieldErrorObject(&root, fe.Path)
		}
		c.problemOnObject(p, line, col, obj, "%v", fe)
		return
	}
	if te, ok := errors.AsType[*yaml.TypeError](err); ok {
		for _, msg := range te.Errors {
			line, rest := splitYAMLLine(msg)
			c.problem(p, min(line, yamlLineCount(data)), "%s", rest)
		}
		return
	}
	line, rest := splitYAMLLine(err.Error())
	col := 0
	if e, ok := loaderr.As(err); ok {
		if line == 0 && e.Line > 0 {
			// A loader that reports a *loaderr.Error has already found the line; its message without the location is the problem.
			line, rest = e.Line, (&loaderr.Error{Field: e.Field, Problem: e.Problem, Expected: e.Expected, Err: e.Err}).Error()
		}
		if e.Column > 0 {
			col = e.Column
		}
	}
	c.problemAt(p, min(line, yamlLineCount(data)), col, "%s", rest)
}

// yamlLineCount returns how many lines YAML counts in data, breaking them at LF, CR, CRLF, NEL and the Unicode line and paragraph separators. YAML numbers the end of a document that stops without a final line break as the line after it, which a problem must not point past.
func yamlLineCount(data []byte) int {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	return 1 + strings.Count(s, "\n") + strings.Count(s, "\r") + strings.Count(s, "\u0085") + strings.Count(s, "\u2028") + strings.Count(s, "\u2029")
}

// splitYAMLLine takes the first "line N: " out of a yaml message and returns N and the rest, or 0 and msg when it names no line.
func splitYAMLLine(msg string) (int, string) {
	m := yamlLineRE.FindStringSubmatchIndex(msg)
	if m == nil {
		return 0, msg
	}
	n, err := strconv.Atoi(msg[m[2]:m[3]])
	if err != nil {
		return 0, msg
	}
	return n, msg[:m[0]] + msg[m[1]:]
}

// yamlLine returns the line of the field that steps lead to from root, one mapping key or decimal sequence index per step: the line of a mapping key, or of a sequence item. When a step is not in the document, such as a required field left out, it returns the line of the nearest field above it.
func yamlLine(root *yaml.Node, steps []string) int {
	line, _ := yamlPos(root, steps)
	return line
}

// yamlPos returns the 1-based line and column of the field that steps lead to from root, one mapping key or decimal sequence index per step. When a step is not in the document, it returns the position of the nearest field above it.
func yamlPos(root *yaml.Node, steps []string) (int, int) {
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	line, col := n.Line, n.Column
	for si, step := range steps {
		for n.Kind == yaml.AliasNode && n.Alias != nil {
			n = n.Alias
		}
		switch n.Kind {
		case yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == step {
					key, val := n.Content[i], n.Content[i+1]
					line, col, next = key.Line, key.Column, val
					if si == len(steps)-1 && val != nil && val.Line == key.Line && val.Column > 0 {
						col = val.Column
					}
				}
			}
			if next == nil {
				return line, col
			}
			n = next
		case yaml.SequenceNode:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(n.Content) {
				return line, col
			}
			n = n.Content[i]
			line, col = n.Line, n.Column
		default:
			return line, col
		}
	}
	return line, col
}

// yamlFieldErrorObject resolves the scene object ID associated with a sceneformat.FieldError
// path (under layers[*].objects[*]..., overrides.<id>..., or connections[*].from/to) when present in root.
func yamlFieldErrorObject(root *yaml.Node, steps []string) string {
	if root == nil || len(steps) < 2 {
		return ""
	}
	doc := root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return ""
	}
	lookup := func(n *yaml.Node, key string) *yaml.Node {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
		return nil
	}
	// Collect existing object IDs in the scene so connection errors attach to a live endpoint.
	liveIDs := make(map[string]bool)
	var collectIDs func(seq *yaml.Node)
	collectIDs = func(seq *yaml.Node) {
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return
		}
		for _, item := range seq.Content {
			if idNode := lookup(item, "id"); idNode != nil && idNode.Value != "" {
				liveIDs[idNode.Value] = true
			}
			collectIDs(lookup(item, "children"))
		}
	}
	if layers := lookup(doc, "layers"); layers != nil && layers.Kind == yaml.SequenceNode {
		for _, layer := range layers.Content {
			collectIDs(lookup(layer, "objects"))
		}
	}

	switch steps[0] {
	case "layers":
		// Walk down layers[i].objects[j](.children[k])* and remember the innermost object ID.
		cur := doc
		lastID := ""
		for i := 0; i < len(steps); i++ {
			step := steps[i]
			switch cur.Kind {
			case yaml.MappingNode:
				next := lookup(cur, step)
				if next == nil {
					return lastID
				}
				cur = next
			case yaml.SequenceNode:
				idx, err := strconv.Atoi(step)
				if err != nil || idx < 0 || idx >= len(cur.Content) {
					return lastID
				}
				cur = cur.Content[idx]
				if i >= 3 && (steps[i-1] == "objects" || steps[i-1] == "children") {
					if idNode := lookup(cur, "id"); idNode != nil && idNode.Value != "" {
						lastID = idNode.Value
					}
				}
			default:
				return lastID
			}
		}
		return lastID
	case "overrides":
		return steps[1]
	case "connections":
		conns := lookup(doc, "connections")
		idx, err := strconv.Atoi(steps[1])
		if conns == nil || conns.Kind != yaml.SequenceNode || err != nil || idx < 0 || idx >= len(conns.Content) {
			return ""
		}
		conn := conns.Content[idx]
		fromID := ""
		toID := ""
		if n := lookup(conn, "from"); n != nil {
			fromID = n.Value
		}
		if n := lookup(conn, "to"); n != nil {
			toID = n.Value
		}
		field := ""
		if len(steps) >= 3 {
			field = steps[2]
		}
		// Attach to the live object endpoint.
		if field == "from" {
			if liveIDs[toID] {
				return toID
			}
			if liveIDs[fromID] {
				return fromID
			}
		} else {
			if liveIDs[fromID] {
				return fromID
			}
			if liveIDs[toID] {
				return toID
			}
		}
	}
	return ""
}
