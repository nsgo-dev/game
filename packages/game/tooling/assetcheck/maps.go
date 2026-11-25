package assetcheck

import (
	"io/fs"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// readTree reads the JSON file at p, recording a problem and returning nil when it cannot be read or parsed or is not an object. kind names what the file should be in that problem, such as "Tiled map".
func (c *checker) readTree(p, kind string) ([]byte, *jsonValue) {
	data, ok := c.readFile(p)
	if !ok {
		return nil, nil
	}
	tree, err := parseJSON(data)
	if err != nil {
		je := err.(*jsonError)
		c.problem(p, je.line, "%s", je.msg)
		return nil, nil
	}
	if tree.kind != jsonObject {
		c.problem(p, tree.line, "not a %s: want a JSON object", kind)
		return nil, nil
	}
	return data, tree
}

// checkTiledMap checks the Tiled map at p once: its external tilesets, its tileset and image-layer images, and that tilemap.LoadTiled loads it.
func (c *checker) checkTiledMap(p string) {
	if c.checked[p] {
		return
	}
	c.checked[p] = true
	c.report.Maps++
	data, tree := c.readTree(p, "Tiled map")
	if tree == nil {
		return
	}
	dir := path.Dir(p)
	var external []string
	for _, ts := range tree.field("tilesets").list() {
		if src, line := ts.str("source"); src != "" {
			if !isJSONTileset(src) {
				c.problem(p, line, "external tileset %q is not a JSON tileset; export it from Tiled as .tsj", src)
				continue
			}
			if tsPath, ok := c.fileRef(p, line, dir, src, "external tileset"); ok {
				c.checkTiledTileset(tsPath)
				external = append(external, tsPath)
			}
			continue
		}
		if img, line := ts.str("image"); img != "" {
			c.imageRef(p, line, dir, img, "tileset image")
		}
	}
	c.checkTiledLayers(p, dir, tree.field("layers").list())

	if c.broken[p] || anyBroken(c.broken, external) {
		return
	}
	_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{
		LoadJSON: func(source string) ([]byte, error) { return fs.ReadFile(c.fsys, path.Join(dir, source)) },
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			return core.ImageSource{URI: imagePath}, nil
		},
	})
	if err != nil {
		c.problem(p, 0, "%v", err)
	}
}

// checkTMXMap checks the Tiled XML map at p once: that tilemap.LoadTiled loads it with the external tilesets it names, and that every tileset and image-layer image it draws exists and decodes. Its problems carry no line, since tilemap.LoadTiled's errors already say where in the XML they are.
func (c *checker) checkTMXMap(p string) {
	if c.checked[p] {
		return
	}
	c.checked[p] = true
	c.report.Maps++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	dir := path.Dir(p)
	_, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{
		LoadTileset: func(source string) ([]byte, error) {
			tsPath, err := resolve(dir, source)
			if err != nil {
				return nil, err
			}
			data, err := fs.ReadFile(c.fsys, tsPath)
			return data, unwrapPathError(err)
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			c.imageRef(p, 0, dir, imagePath, "image")
			return core.ImageSource{URI: imagePath}, nil
		},
	})
	if err != nil {
		c.problem(p, 0, "%v", err)
	}
}

// checkTiledLayers checks the images of a Tiled map's image layers, descending into group layers.
func (c *checker) checkTiledLayers(p, dir string, layers []*jsonValue) {
	for _, l := range layers {
		switch typ, _ := l.str("type"); typ {
		case "imagelayer":
			if img, line := l.str("image"); img != "" {
				c.imageRef(p, line, dir, img, "image layer")
			}
		case "group":
			c.checkTiledLayers(p, dir, l.field("layers").list())
		}
	}
}

// isJSONTileset reports whether a Tiled tileset source names a JSON tileset (.tsj, or .json as older Tiled versions named them), which tilemap.LoadTiled reads, rather than a TSX one, which it does not.
func isJSONTileset(src string) bool {
	ext := strings.ToLower(path.Ext(src))
	return ext == ".tsj" || ext == ".json"
}

// checkTiledTileset checks the Tiled tileset at p once: that it parses and that its image exists.
func (c *checker) checkTiledTileset(p string) {
	if c.checked[p] {
		return
	}
	c.checked[p] = true
	c.report.Tilesets++
	_, tree := c.readTree(p, "Tiled tileset")
	if tree == nil {
		return
	}
	if img, line := tree.str("image"); img != "" {
		c.imageRef(p, line, path.Dir(p), img, "tileset image")
	}
}

// checkLDtk checks the LDtk project at p: its tileset images and level backgrounds, that no level is saved in a separate file, which tilemap.LoadLDtkAll does not read, and that tilemap.LoadLDtkAll loads it.
func (c *checker) checkLDtk(p string) {
	if c.checked[p] {
		return
	}
	c.checked[p] = true
	c.report.Maps++
	data, tree := c.readTree(p, "LDtk project")
	if tree == nil {
		return
	}
	dir := path.Dir(p)
	for _, ts := range tree.field("defs").field("tilesets").list() {
		if rel, line := ts.str("relPath"); rel != "" {
			c.imageRef(p, line, dir, rel, "tileset image")
		}
	}
	for _, lvl := range tree.field("levels").list() {
		if rel, line := lvl.str("bgRelPath"); rel != "" {
			c.imageRef(p, line, dir, rel, "level background")
		}
		if rel, line := lvl.str("externalRelPath"); rel != "" {
			id, _ := lvl.str("identifier")
			c.problem(p, line, "level %q is saved in a separate file (%q), which tilemap.LoadLDtkAll does not read, so it would load empty; turn off \"Save levels to separate files\" in LDtk", id, rel)
		}
	}
	if c.broken[p] {
		return
	}
	if _, err := tilemap.LoadLDtkAll(data, func(rel string) core.ImageSource { return core.ImageSource{URI: rel} }); err != nil {
		c.problem(p, 0, "%v", err)
	}
}

// anyBroken reports whether any of paths has a problem.
func anyBroken(broken map[string]bool, paths []string) bool {
	for _, p := range paths {
		if broken[p] {
			return true
		}
	}
	return false
}
