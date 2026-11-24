package assetcheck

import (
	"errors"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"go.yaml.in/yaml/v3"
)

// errRefUnavailable is returned by sceneLoader when a referenced scene file is missing or failed to parse; its error is reported on that file or by checkRefs, so Resolve does not double-report it on the caller.
var errRefUnavailable = errors.New("referenced scene unavailable")

// parsedScene holds one scene file parsed once during Check.
type parsedScene struct {
	path      string
	data      []byte
	doc       *sceneformat.SceneDoc
	root      *yaml.Node
	lines     int
	project   string
	err       error
	sanitized *sceneformat.SceneDoc
	badConns  bool
}

// loadScene reads and parses the scene at p once. Read and parse errors are recorded as problems on p at most once.
func (c *checker) loadScene(p string) *parsedScene {
	if ps, ok := c.parsedScenes[p]; ok {
		return ps
	}
	ps := &parsedScene{path: p, project: c.projectRoot(path.Dir(p))}
	c.parsedScenes[p] = ps
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		ps.err = err
		c.problem(p, 0, "%v", unwrapPathError(err))
		return ps
	}
	ps.data = data
	ps.lines = yamlLineCount(data)
	doc, err := sceneformat.Parse(data)
	if err != nil {
		ps.err = err
		c.yamlError(p, data, err)
		return ps
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		ps.err = err
		c.yamlError(p, data, err) // Parse read it, so this does not happen.
		return ps
	}
	ps.doc = doc
	ps.root = &root
	return ps
}

// checkScene checks the scene file at p: that sceneformat parses and validates it, that what it refers to exists and loads, and that its components (when Options.Components is set) and resolved connections are valid.
func (c *checker) checkScene(p string) {
	c.report.Scenes++
	ps := c.loadScene(p)
	if ps.err != nil {
		return
	}
	s := &sceneCheck{checker: c, path: p, root: ps.root, lines: ps.lines, project: ps.project}
	for _, name := range slices.Sorted(maps.Keys(ps.doc.Tilemaps)) {
		s.tilemap(ps.doc.Tilemaps[name], "tilemaps", name)
	}
	for i := range ps.doc.Layers {
		l := &ps.doc.Layers[i]
		li := strconv.Itoa(i)
		if l.Tilemap != nil && !l.Tilemap.IsRef() {
			s.tilemap(l.Tilemap, "layers", li, "tilemap")
		}
		s.objects(l.Objects, "layers", li, "objects")
	}
	c.checkSceneSemantics(ps)
}

// sceneLoader resolves referenced scene files (inherits and instanced scene) relative to project for sceneformat.Resolve.
type sceneLoader struct {
	checker      *checker
	project      string
	hadBrokenRef bool
}

func (l *sceneLoader) Scene(ref string) (*sceneformat.SceneDoc, error) {
	p, err := resolve(l.project, ref)
	if err != nil {
		return nil, errRefUnavailable
	}
	if err := l.checker.fileMissing(p); err != nil {
		return nil, errRefUnavailable
	}
	ps := l.checker.loadScene(p)
	if ps.err != nil {
		l.hadBrokenRef = true
		return nil, errRefUnavailable
	}
	if l.checker.resolving[p] {
		// Hand the raw document back so sceneformat.Resolve reports the exact cycle chain.
		return ps.doc, nil
	}
	l.checker.checkSceneSemantics(ps)
	if ps.badConns || l.checker.broken[p] {
		l.hadBrokenRef = true
	}
	if ps.sanitized != nil {
		return ps.sanitized, nil
	}
	return ps.doc, nil
}

// checkSceneSemantics checks ps's components (when Options.Components is set), resolves any inherited or instanced scenes, and validates every connection of ps with its exact file, line and column.
func (c *checker) checkSceneSemantics(ps *parsedScene) {
	if ps.err != nil || c.checkedConns[ps.path] {
		return
	}
	c.checkedConns[ps.path] = true
	reg := c.opts.Components

	if reg != nil {
		if _, err := sceneformat.ParseWith(ps.data, sceneformat.ParseOptions{File: ps.path, Components: reg}); err != nil {
			if ce, ok := errors.AsType[*sceneformat.ComponentError](err); ok {
				msg := *ce
				msg.File, msg.Line, msg.Column = "", 0, 0
				c.problemAt(ps.path, min(ce.Line, ps.lines), ce.Column, "%s", msg.Error())
				ps.badConns = true
				cp := ps.doc.Clone()
				cp.Connections = nil
				ps.sanitized = cp
				return
			}
			// ConnectionError from ParseWith is checked per connection below so every broken connection is reported.
		}
	}

	res := ps.doc
	var baseConns []sceneformat.ConnectionDoc
	var loader *sceneLoader
	if ps.doc.RefersToScenes() {
		c.resolving[ps.path] = true
		defer delete(c.resolving, ps.path)
		loader = &sceneLoader{checker: c, project: ps.project}
		probe := *ps.doc
		probe.Connections = nil
		var err error
		res, err = sceneformat.Resolve(&probe, loader)
		if err != nil {
			if errors.Is(err, errRefUnavailable) {
				return
			}
			line := c.resolveErrorLine(ps, err)
			c.problem(ps.path, min(line, ps.lines), "%v", err)
			ps.badConns = true
			return
		}
		baseConns = res.Connections
		if reg != nil && !loader.hadBrokenRef {
			if err := sceneformat.ValidateComponents(res, reg); err != nil {
				if ce, ok := errors.AsType[*sceneformat.ComponentError](err); ok {
					line, col := yamlOverrideComponentPos(ps.root, ce.Object, ce.Component, ce.Field)
					msg := *ce
					msg.File, msg.Line, msg.Column = "", 0, 0
					c.problemAt(ps.path, min(line, ps.lines), col, "%s", msg.Error())
					ps.badConns = true
				}
			}
		}
	}

	objects := make(map[string]*sceneformat.ObjectDoc)
	for li := range res.Layers {
		sceneformat.WalkObjects(res.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			objects[o.ID] = o
			return true
		})
	}

	// If an override in ps broke an inherited or instanced connection, report it once on ps.
	if reg != nil && len(baseConns) > 0 && (loader == nil || !loader.hadBrokenRef) {
		if err := sceneformat.ValidateConnections(res, reg); err != nil {
			if ce, ok := errors.AsType[*sceneformat.ConnectionError](err); ok {
				line, col := yamlPos(ps.root, []string{"overrides"})
				if line == 0 {
					line, col = yamlPos(ps.root, []string{"inherits"})
				}
				msg := *ce
				msg.File, msg.Line, msg.Column = "", 0, 0
				c.problemAt(ps.path, min(line, ps.lines), col, "%s", msg.Error())
				ps.badConns = true
			}
		}
	}

	if len(ps.doc.Connections) == 0 {
		ps.sanitized = ps.doc
		return
	}

	seenRaw := make(map[[4]string]int, len(baseConns)+len(ps.doc.Connections))
	for _, bc := range baseConns {
		seenRaw[[4]string{bc.From, bc.Event, bc.To, bc.Handler}] = -1
	}

	baseOffset := len(baseConns)
	validResolved := slices.Clone(baseConns)
	var validOwnIdx []int
	validOwn := make([]sceneformat.ConnectionDoc, 0, len(ps.doc.Connections))

	for i, conn := range ps.doc.Connections {
		src, srcOK := objects[conn.From]
		dst, dstOK := objects[conn.To]
		if !srcOK {
			line, col := yamlConnectionPos(ps.root, i, "from", "")
			c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): from %q names no object of the scene", i, conn.From, conn.Event, conn.To, conn.Handler, conn.From)
			ps.badConns = true
		}
		if !dstOK {
			line, col := yamlConnectionPos(ps.root, i, "to", "")
			c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): to %q names no object of the scene", i, conn.From, conn.Event, conn.To, conn.Handler, conn.To)
			ps.badConns = true
		}
		if !srcOK || !dstOK {
			continue
		}

		rawKey := [4]string{conn.From, conn.Event, conn.To, conn.Handler}
		if prev, dup := seenRaw[rawKey]; dup {
			line, col := yamlConnectionPos(ps.root, i, "", "")
			if prev < 0 {
				c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): repeats an inherited or instanced connection", i, conn.From, conn.Event, conn.To, conn.Handler)
			} else {
				c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): repeats connection %d", i, conn.From, conn.Event, conn.To, conn.Handler, prev)
			}
			ps.badConns = true
			continue
		}

		if reg == nil {
			bad := false
			if comp, _, qualified := strings.Cut(conn.Event, "."); qualified {
				if _, has := src.Components[comp]; !has {
					line, col := yamlConnectionPos(ps.root, i, "event", "")
					c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): object %q has no component %q", i, conn.From, conn.Event, conn.To, conn.Handler, src.ID, comp)
					ps.badConns = true
					bad = true
				}
			}
			if comp, _, qualified := strings.Cut(conn.Handler, "."); qualified {
				if _, has := dst.Components[comp]; !has {
					line, col := yamlConnectionPos(ps.root, i, "handler", "")
					c.problemAt(ps.path, min(line, ps.lines), col, "sceneformat: connection %d (%q %s -> %q %s): object %q has no component %q", i, conn.From, conn.Event, conn.To, conn.Handler, dst.ID, comp)
					ps.badConns = true
					bad = true
				}
			}
			if bad {
				continue
			}
			seenRaw[rawKey] = i
			validOwn = append(validOwn, conn)
			continue
		}

		probeDoc := *res
		probeDoc.Connections = append(slices.Clone(validResolved), conn)
		if err := sceneformat.ValidateConnections(&probeDoc, reg); err != nil {
			if ce, ok := errors.AsType[*sceneformat.ConnectionError](err); ok && ce.Index == len(validResolved) {
				line, col := yamlConnectionPos(ps.root, i, ce.Field, ce.Arg)
				msg := *ce
				msg.File, msg.Line, msg.Column = "", 0, 0
				msg.Index = i
				if rest, ok := strings.CutPrefix(msg.Msg, "connects the same event and handler as connection "); ok {
					if dupIdx, err := strconv.Atoi(rest); err == nil {
						if dupIdx < baseOffset {
							msg.Msg = "connects the same event and handler as an inherited or instanced connection"
						} else if dupIdx-baseOffset < len(validOwnIdx) {
							msg.Msg = "connects the same event and handler as connection " + strconv.Itoa(validOwnIdx[dupIdx-baseOffset])
						}
					}
				}
				c.problemAt(ps.path, min(line, ps.lines), col, "%s", msg.Error())
				ps.badConns = true
				continue
			}
			// If an error occurred in baseConns (ce.Index < baseOffset), it was already reported above or in the referenced scene.
			if ce, ok := errors.AsType[*sceneformat.ConnectionError](err); ok && ce.Index < baseOffset {
				// Still validate this connection on its own so errors in ps are not masked by a broken base connection.
				solo := *res
				solo.Connections = []sceneformat.ConnectionDoc{conn}
				if serr := sceneformat.ValidateConnections(&solo, reg); serr != nil {
					if sce, ok := errors.AsType[*sceneformat.ConnectionError](serr); ok {
						line, col := yamlConnectionPos(ps.root, i, sce.Field, sce.Arg)
						msg := *sce
						msg.File, msg.Line, msg.Column = "", 0, 0
						msg.Index = i
						c.problemAt(ps.path, min(line, ps.lines), col, "%s", msg.Error())
						ps.badConns = true
						continue
					}
				}
			}
		}
		seenRaw[rawKey] = i
		validResolved = append(validResolved, conn)
		validOwnIdx = append(validOwnIdx, i)
		validOwn = append(validOwn, conn)
	}

	if len(validOwn) == len(ps.doc.Connections) {
		ps.sanitized = ps.doc
	} else {
		cp := ps.doc.Clone()
		cp.Connections = validOwn
		ps.sanitized = cp
	}
}

// resolveErrorLine picks a useful line in ps for a Resolve error.
func (c *checker) resolveErrorLine(ps *parsedScene, err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "layerOverrides"):
		return yamlLine(ps.root, []string{"layerOverrides"})
	case strings.Contains(msg, "overrides names object"):
		return yamlLine(ps.root, []string{"overrides"})
	case strings.Contains(msg, "inherits") || strings.Contains(msg, "extends the layer of base scene") || strings.Contains(msg, "scene cycle"):
		if ps.doc.Inherits != "" {
			return yamlLine(ps.root, []string{"inherits"})
		}
		return yamlLine(ps.root, []string{"layers"})
	default:
		return yamlLine(ps.root, []string{"layers"})
	}
}

// objects checks the sprites and materials of objs, found at steps in the scene, and of their descendants.
func (s *sceneCheck) objects(objs []sceneformat.ObjectDoc, steps ...string) {
	for j := range objs {
		o := &objs[j]
		op := append(slices.Clip(steps), strconv.Itoa(j))
		if o.Sprite != nil {
			s.sprite(o.Sprite, append(slices.Clip(op), "sprite")...)
		}
		if o.Material != "" {
			s.material(o.Material, append(slices.Clip(op), "material")...)
		}
		s.objects(o.Children, append(slices.Clip(op), "children")...)
	}
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

// tilemap checks a tilemap definition at steps: an external source must be a Tiled map that checks, and an inline grid's tileset atlas must have a frame for every tile ID it uses.
func (s *sceneCheck) tilemap(tm *sceneformat.TilemapDoc, steps ...string) {
	switch {
	case tm.IsExternal():
		line := s.line(append(steps, "source")...)
		if ext := strings.ToLower(path.Ext(tm.Source)); ext != ".tmj" {
			s.problem(s.path, line, "tilemap source %q is not a Tiled JSON map (.tmj)", tm.Source)
			return
		}
		if p, ok := s.fileRef(s.path, line, s.project, tm.Source, "tilemap source"); ok {
			s.checkTiledMap(p)
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

// sprite checks that a sprite's atlas exists and has its frame.
func (s *sceneCheck) sprite(ref *sceneformat.SpriteRef, steps ...string) {
	atlas := s.findAtlas(s.path, s.line(append(steps, "atlas")...), s.project, ref.Atlas, "atlas")
	if atlas == nil || atlas.frames == nil {
		return
	}
	if !atlas.frames[ref.Frame] {
		s.problem(s.path, s.line(append(steps, "frame")...), "frame %q is not in atlas %q (%s)", ref.Frame, ref.Atlas, atlas.path)
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

// yamlLineRE finds the "line N: " that yaml's errors carry.
var yamlLineRE = regexp.MustCompile(`line (\d+): `)

// yamlError records err, from parsing the YAML file at p, at the line it is about: the field a sceneformat.FieldError names (with column for connections), each line a yaml.TypeError lists, the line a syntax error states, or the line a *loaderr.Error gives.
func (c *checker) yamlError(p string, data []byte, err error) {
	if fe, ok := errors.AsType[*sceneformat.FieldError](err); ok {
		var root yaml.Node
		line, col := 0, 0
		if yaml.Unmarshal(data, &root) == nil {
			if len(fe.Path) >= 2 && fe.Path[0] == "connections" {
				line, col = yamlValuePos(&root, fe.Path)
			} else {
				line = yamlLine(&root, fe.Path)
			}
			line = min(line, yamlLineCount(data))
		}
		c.problemAt(p, line, col, "%v", fe)
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
	if e, ok := loaderr.As(err); ok && line == 0 && e.Line > 0 {
		// A loader that reports a *loaderr.Error has already found the line; its message without the location is the problem.
		line, rest = e.Line, (&loaderr.Error{Field: e.Field, Problem: e.Problem, Expected: e.Expected, Err: e.Err}).Error()
	}
	c.problem(p, min(line, yamlLineCount(data)), "%s", rest)
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
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	line := n.Line
	for _, step := range steps {
		for n.Kind == yaml.AliasNode && n.Alias != nil {
			n = n.Alias
		}
		switch n.Kind {
		case yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == step {
					line, next = n.Content[i].Line, n.Content[i+1]
				}
			}
			if next == nil {
				return line
			}
			n = next
		case yaml.SequenceNode:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(n.Content) {
				return line
			}
			n = n.Content[i]
			line = n.Line
		default:
			return line
		}
	}
	return line
}

// yamlResolveAlias follows YAML alias nodes up to a bounded depth.
func yamlResolveAlias(n *yaml.Node) *yaml.Node {
	for depth := 0; n != nil && n.Kind == yaml.AliasNode && depth < 64; depth++ {
		n = n.Alias
	}
	return n
}

// yamlMappingValue returns the value node for key in mapping n, or nil.
func yamlMappingValue(n *yaml.Node, key string) *yaml.Node {
	n = yamlResolveAlias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i].Tag != "!!merge" {
			return yamlResolveAlias(n.Content[i+1])
		}
	}
	return nil
}

// yamlMappingKey returns the key node for key in mapping n, or nil.
func yamlMappingKey(n *yaml.Node, key string) *yaml.Node {
	n = yamlResolveAlias(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i].Tag != "!!merge" {
			return n.Content[i]
		}
	}
	return nil
}

// yamlDocRoot returns the top-level content node of root.
func yamlDocRoot(root *yaml.Node) *yaml.Node {
	n := yamlResolveAlias(root)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = yamlResolveAlias(n.Content[0])
	}
	return n
}

// yamlConnectionPos returns the 1-based (line, column) of connection index's field (and optional arg) in root, matching sceneformat's ConnectionError locations.
func yamlConnectionPos(root *yaml.Node, index int, field, arg string) (int, int) {
	n := yamlDocRoot(root)
	seq := yamlMappingValue(n, "connections")
	if seq == nil || seq.Kind != yaml.SequenceNode || index < 0 || index >= len(seq.Content) {
		if n != nil {
			return n.Line, n.Column
		}
		return 0, 0
	}
	c := yamlResolveAlias(seq.Content[index])
	pos := c
	if field != "" {
		if v := yamlMappingValue(c, field); v != nil {
			pos = v
			if arg != "" {
				if a := yamlMappingValue(v, arg); a != nil {
					pos = a
				}
			}
		}
	}
	if pos == nil {
		return 0, 0
	}
	return pos.Line, pos.Column
}

// yamlValuePos returns the 1-based (line, column) of the value node reached by steps, falling back to the nearest ancestor when a step is absent.
func yamlValuePos(root *yaml.Node, steps []string) (int, int) {
	n := yamlDocRoot(root)
	if n == nil {
		return 0, 0
	}
	pos := n
	for _, step := range steps {
		n = yamlResolveAlias(n)
		if n == nil {
			break
		}
		switch n.Kind {
		case yaml.MappingNode:
			next := yamlMappingValue(n, step)
			if next == nil {
				return pos.Line, pos.Column
			}
			pos, n = next, next
		case yaml.SequenceNode:
			i, err := strconv.Atoi(step)
			if err != nil || i < 0 || i >= len(n.Content) {
				return pos.Line, pos.Column
			}
			n = yamlResolveAlias(n.Content[i])
			if n == nil {
				return pos.Line, pos.Column
			}
			pos = n
		default:
			return pos.Line, pos.Column
		}
	}
	return pos.Line, pos.Column
}

// yamlPos returns the 1-based (line, column) of the key at steps, or (0, 0) when the first step does not exist.
func yamlPos(root *yaml.Node, steps []string) (int, int) {
	n := yamlDocRoot(root)
	if n == nil || len(steps) == 0 {
		return 0, 0
	}
	var pos *yaml.Node
	for _, step := range steps {
		n = yamlResolveAlias(n)
		if n == nil || n.Kind != yaml.MappingNode {
			break
		}
		k := yamlMappingKey(n, step)
		v := yamlMappingValue(n, step)
		if k == nil {
			break
		}
		pos, n = k, v
	}
	if pos == nil {
		return 0, 0
	}
	return pos.Line, pos.Column
}

// yamlOverrideComponentPos locates a component error introduced by a scene-level or object-level overrides mapping in root.
func yamlOverrideComponentPos(root *yaml.Node, objectID, comp, field string) (int, int) {
	n := yamlDocRoot(root)
	if n == nil {
		return 0, 0
	}
	if ovs := yamlMappingValue(n, "overrides"); ovs != nil {
		if line, col, ok := findComponentInOverride(ovs, objectID, comp, field); ok {
			return line, col
		}
	}
	if layers := yamlMappingValue(n, "layers"); layers != nil && layers.Kind == yaml.SequenceNode {
		for _, ln := range layers.Content {
			if line, col, ok := findComponentInObjectOverrides(yamlMappingValue(ln, "objects"), objectID, comp, field); ok {
				return line, col
			}
		}
	}
	return n.Line, n.Column
}

func findComponentInOverride(ovs *yaml.Node, key, comp, field string) (int, int, bool) {
	ov := yamlMappingValue(ovs, key)
	if ov == nil {
		return 0, 0, false
	}
	comps := yamlMappingValue(ov, "components")
	if comps == nil {
		return 0, 0, false
	}
	ck := yamlMappingKey(comps, comp)
	cv := yamlMappingValue(comps, comp)
	if ck == nil {
		return 0, 0, false
	}
	if field != "" && cv != nil {
		if fv := yamlMappingValue(cv, field); fv != nil {
			return fv.Line, fv.Column, true
		}
		if fk := yamlMappingKey(cv, field); fk != nil {
			return fk.Line, fk.Column, true
		}
	}
	return ck.Line, ck.Column, true
}

func findComponentInObjectOverrides(seq *yaml.Node, objectID, comp, field string) (int, int, bool) {
	seq = yamlResolveAlias(seq)
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return 0, 0, false
	}
	for _, on := range seq.Content {
		on = yamlResolveAlias(on)
		idNode := yamlMappingValue(on, "id")
		if idNode != nil {
			if sub, ok := strings.CutPrefix(objectID, idNode.Value+"/"); ok {
				if ovs := yamlMappingValue(on, "overrides"); ovs != nil {
					if line, col, found := findComponentInOverride(ovs, sub, comp, field); found {
						return line, col, true
					}
				}
			}
		}
		if line, col, ok := findComponentInObjectOverrides(yamlMappingValue(on, "children"), objectID, comp, field); ok {
			return line, col, true
		}
	}
	return 0, 0, false
}
