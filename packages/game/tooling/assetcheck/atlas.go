package assetcheck

import (
	"io/fs"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// atlasInfo is a sprite atlas JSON file found in the walk.
type atlasInfo struct {
	path string
	data []byte
	tree *jsonValue
	// frames are the atlas's frame names, set once it loaded with sprite.LoadAtlas; nil when it did not, so that references into it are not checked against frames it may not have.
	frames map[string]bool
}

// sniffAtlas reads the JSON file at p and records it as an atlas when it has top-level frames and meta objects, as TexturePacker and Aseprite exports do. Any other JSON file, valid or not, is kept aside as a non-atlas, which is reported only when a scene names it as an atlas, since a project holds JSON files of other kinds that are none of the check's business.
func (c *checker) sniffAtlas(p string) {
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		c.nonAtlases = append(c.nonAtlases, nonAtlas{path: p, err: &jsonError{msg: unwrapPathError(err).Error()}})
		return
	}
	tree, err := parseJSON(data)
	if err != nil {
		c.nonAtlases = append(c.nonAtlases, nonAtlas{path: p, err: err.(*jsonError)})
		return
	}
	frames, meta := tree.field("frames"), tree.field("meta")
	if frames == nil || (frames.kind != jsonObject && frames.kind != jsonArray) || meta == nil || meta.kind != jsonObject {
		c.nonAtlases = append(c.nonAtlases, nonAtlas{path: p})
		return
	}
	c.atlasPaths = append(c.atlasPaths, p)
	c.atlases[p] = &atlasInfo{path: p, data: data, tree: tree}
}

// nonAtlas is a JSON file found in the walk that is not an atlas.
type nonAtlas struct {
	path string
	// err is why the file does not read or parse, or nil when it parsed but lacks an atlas's top-level frames and meta objects.
	err *jsonError
}

// checkAtlas checks an atlas's image reference, that its frames lie inside the image, and that it loads.
func (c *checker) checkAtlas(a *atlasInfo) {
	c.report.Atlases++
	var img *imageInfo
	if ref, line := a.tree.field("meta").str("image"); ref != "" {
		img = c.imageRef(a.path, line, path.Dir(a.path), ref, "meta.image")
	}
	if img != nil && img.sized {
		for _, f := range atlasFrames(a.tree) {
			r := f.value.field("frame")
			x, okX := r.field("x").number()
			y, okY := r.field("y").number()
			w, okW := r.field("w").number()
			h, okH := r.field("h").number()
			if !okX || !okY || !okW || !okH {
				continue // sprite.LoadAtlas reports a malformed rectangle.
			}
			if x < 0 || y < 0 || x+w > float64(img.width) || y+h > float64(img.height) {
				c.problem(a.path, r.line, "frame %q at (%g, %g) size %gx%g lies outside its %dx%d image", f.name, x, y, w, h, img.width, img.height)
			}
		}
	}
	loaded, err := sprite.LoadAtlas(core.ImageSource{URI: a.path}, a.data)
	if err != nil {
		c.problem(a.path, 0, "%v", err)
		return
	}
	a.frames = make(map[string]bool, len(loaded.Frames))
	for name := range loaded.Frames {
		a.frames[name] = true
	}
}

// namedFrame is one frame entry of an atlas's JSON.
type namedFrame struct {
	name  string
	value *jsonValue
}

// atlasFrames returns the frame entries of an atlas's JSON, from the hash form keyed by name or the array form naming each in filename. The hash form's names come back in no particular order, which does not matter once the problems are sorted.
func atlasFrames(tree *jsonValue) []namedFrame {
	frames := tree.field("frames")
	var out []namedFrame
	if frames.kind == jsonObject {
		for name, v := range frames.fields {
			out = append(out, namedFrame{name: name, value: v})
		}
		return out
	}
	for _, v := range frames.items {
		name, _ := v.str("filename")
		out = append(out, namedFrame{name: name, value: v})
	}
	return out
}

// findAtlas returns the atlas a scene at scene, in project root, names name, or records a problem at line and returns nil. The name is the path without .json of an atlas file in the same project, from the project root or as its base name; among several matches the one nearest the scene, by the directories walked up from the scene and down to the atlas, wins.
func (c *checker) findAtlas(scene string, line int, root, name, what string) *atlasInfo {
	var best []*atlasInfo
	bestDist := -1
	for _, p := range c.atlasPaths {
		if !c.atlasNamed(p, root, name) {
			continue
		}
		d := dirDistance(path.Dir(scene), path.Dir(p))
		switch {
		case bestDist < 0 || d < bestDist:
			best, bestDist = []*atlasInfo{c.atlases[p]}, d
		case d == bestDist:
			best = append(best, c.atlases[p])
		}
	}
	switch len(best) {
	case 0:
		c.atlasNotFound(scene, line, root, name, what)
		return nil
	case 1:
		return best[0]
	default:
		paths := make([]string, len(best))
		for i, a := range best {
			paths[i] = a.path
		}
		c.problem(scene, line, "%s %q is ambiguous: %s all match; name it by its path", what, name, strings.Join(paths, ", "))
		return nil
	}
}

// atlasNamed reports whether the JSON file at p is in project root and is named name there, from the root or by its base name.
func (c *checker) atlasNamed(p, root, name string) bool {
	if c.projectRoot(path.Dir(p)) != root {
		return false // another project's file
	}
	stem := strings.TrimSuffix(p, path.Ext(p))
	return stem == path.Join(root, name) || path.Base(stem) == name
}

// atlasNotFound records that no atlas in project root is named name. When JSON files of that name exist but are not atlases, it says so and reports a file that does not parse against the file itself, so that a corrupt export is not mistaken for a missing one.
func (c *checker) atlasNotFound(scene string, line int, root, name, what string) {
	var named []string
	for _, f := range c.nonAtlases {
		if !c.atlasNamed(f.path, root, name) {
			continue
		}
		named = append(named, f.path)
		if f.err != nil {
			c.problem(f.path, f.err.line, "%s", f.err.msg)
		} else {
			c.problem(f.path, 0, "not an atlas: want top-level frames and meta objects, as TexturePacker and Aseprite write them")
		}
	}
	if len(named) > 0 {
		c.problem(scene, line, "%s %q: %s is not a usable atlas", what, name, strings.Join(named, ", "))
		return
	}
	where := "the checked directory"
	if root != "." {
		where = root
	}
	c.problem(scene, line, "%s %q not found: no atlas JSON named %s.json in %s", what, name, path.Base(name), where)
}

// dirDistance returns how many directories lie between a and b: the steps up from a to the directory they share, plus the steps down from it to b. "." is the root, with no components.
func dirDistance(a, b string) int {
	as, bs := dirParts(a), dirParts(b)
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return len(as) - n + len(bs) - n
}

// dirParts splits a slash-separated directory into its components, with none for ".".
func dirParts(dir string) []string {
	if dir == "." {
		return nil
	}
	return strings.Split(dir, "/")
}
