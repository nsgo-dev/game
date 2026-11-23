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

// sniffAtlas reads the JSON file at p and records it as an atlas when it has top-level frames and meta objects, as TexturePacker and Aseprite exports do. Any other JSON file, valid or not, is none of the check's business.
func (c *checker) sniffAtlas(p string) {
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		return
	}
	tree, err := parseJSON(data)
	if err != nil {
		return
	}
	frames, meta := tree.field("frames"), tree.field("meta")
	if frames == nil || (frames.kind != jsonObject && frames.kind != jsonArray) || meta == nil || meta.kind != jsonObject {
		return
	}
	c.atlasPaths = append(c.atlasPaths, p)
	c.atlases[p] = &atlasInfo{path: p, data: data, tree: tree}
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

// findAtlas returns the atlas a scene at scene, in project root, names name, or records a problem at line and returns nil. The name is the path without .json of an atlas file in the same project, from the project root or as its base name; among several matches the one sharing the longest directory with the scene wins.
func (c *checker) findAtlas(scene string, line int, root, name, what string) *atlasInfo {
	var best []*atlasInfo
	bestShared := -1
	for _, p := range c.atlasPaths {
		if c.projectRoot(path.Dir(p)) != root {
			continue // another project's atlas
		}
		stem := strings.TrimSuffix(p, path.Ext(p))
		if stem != path.Join(root, name) && path.Base(stem) != name {
			continue
		}
		shared := sharedDirs(path.Dir(scene), path.Dir(p))
		switch {
		case shared > bestShared:
			best, bestShared = []*atlasInfo{c.atlases[p]}, shared
		case shared == bestShared:
			best = append(best, c.atlases[p])
		}
	}
	switch len(best) {
	case 0:
		where := "the checked directory"
		if root != "." {
			where = root
		}
		c.problem(scene, line, "%s %q not found: no atlas JSON named %s.json in %s", what, name, path.Base(name), where)
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

// sharedDirs returns how many leading directories a and b have in common.
func sharedDirs(a, b string) int {
	if a == "." || b == "." {
		return 0
	}
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}
