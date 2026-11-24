package assetcheck

import (
	"errors"
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

// yamlError records err, from parsing the YAML file at p, at the line it is about: the field a sceneformat.FieldError names, each line a yaml.TypeError lists, the line a syntax error states, or the line a *loaderr.Error gives.
func (c *checker) yamlError(p string, data []byte, err error) {
	if fe, ok := errors.AsType[*sceneformat.FieldError](err); ok {
		var root yaml.Node
		line := 0
		if yaml.Unmarshal(data, &root) == nil {
			line = min(yamlLine(&root, fe.Path), yamlLineCount(data))
		}
		c.problem(p, line, "%v", fe)
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
