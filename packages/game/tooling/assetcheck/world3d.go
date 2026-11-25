package assetcheck

import (
	"path"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"go.yaml.in/yaml/v3"
)

type meshLibInfo struct {
	lib *world3d.MeshLibrary
	ok  bool
}

// meshLibrary loads and caches a glTF 2.0 mesh library at p, recording any load
// error on p once.
func (c *checker) meshLibrary(p string) *meshLibInfo {
	if info, ok := c.meshLibs[p]; ok {
		return info
	}
	info := &meshLibInfo{}
	c.meshLibs[p] = info
	data, ok := c.readFile(p)
	if !ok {
		return info
	}
	lib, err := world3d.LoadMeshLibraryFromGLTF(data)
	if err != nil {
		line := 0
		msg := err.Error()
		if e, ok := loaderr.As(err); ok {
			line = e.Line
			msg = (&loaderr.Error{Field: e.Field, Problem: e.Problem, Expected: e.Expected, Err: e.Err}).Error()
		}
		c.problem(p, line, "%s", msg)
		return info
	}
	info.lib = lib
	info.ok = true
	return info
}

// checkGridMap checks the 3D grid map at p: that world3d.ParseGridMap parses
// and validates it, that its referenced meshLibrary exists (and loads as glTF
// 2.0 when named *.gltf or *.glb), and that every box and cell tile name exists
// in that mesh library.
func (c *checker) checkGridMap(p string) {
	if c.checked[p] {
		return
	}
	c.checked[p] = true
	c.report.GridMaps++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := world3d.ParseGridMap(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err)
		return
	}
	proj := c.projectRoot(path.Dir(p))
	if doc.MeshLibrary == "" {
		if len(doc.Boxes) > 0 {
			c.problem(p, yamlLine(&root, []string{"boxes", "0", "tile"}), "grid map box tile %q: meshLibrary is required to resolve tile names", doc.Boxes[0].Tile)
		} else if len(doc.Cells) > 0 {
			c.problem(p, yamlLine(&root, []string{"cells", "0", "tile"}), "grid map cell tile %q: meshLibrary is required to resolve tile names", doc.Cells[0].Tile)
		}
		return
	}
	libPath, ok := c.fileRef(p, yamlLine(&root, []string{"meshLibrary"}), proj, doc.MeshLibrary, "mesh library")
	ext := strings.ToLower(path.Ext(libPath))
	if !ok || (ext != ".gltf" && ext != ".glb") {
		return
	}
	info := c.meshLibrary(libPath)
	if !info.ok {
		return
	}
	for i, b := range doc.Boxes {
		if _, found := info.lib.ByName(b.Tile); !found {
			line := yamlLine(&root, []string{"boxes", strconv.Itoa(i), "tile"})
			c.problem(p, line, "grid map box tile %q: not found in mesh library %q", b.Tile, doc.MeshLibrary)
		}
	}
	for i, cell := range doc.Cells {
		if _, found := info.lib.ByName(cell.Tile); !found {
			line := yamlLine(&root, []string{"cells", strconv.Itoa(i), "tile"})
			c.problem(p, line, "grid map cell tile %q: not found in mesh library %q", cell.Tile, doc.MeshLibrary)
		}
	}
}

// checkTerrain checks the 3D terrain definition at p: that world3d.ParseTerrain
// parses and validates it, and that its referenced heightmap (at least 2x2),
// splat maps, and layer PBR textures exist and decode.
func (c *checker) checkTerrain(p string) {
	c.report.Terrains++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := world3d.ParseTerrain(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err)
		return
	}
	proj := c.projectRoot(path.Dir(p))
	if doc.Heightmap != "" {
		line := yamlLine(&root, []string{"heightmap"})
		if img := c.imageRef(p, line, proj, doc.Heightmap, "terrain heightmap"); img != nil && img.sized && (img.width < 2 || img.height < 2) {
			c.problem(p, line, "terrain heightmap %q is %dx%d; need at least 2x2 samples", doc.Heightmap, img.width, img.height)
		}
	}
	for i, sm := range doc.SplatMaps {
		c.imageRef(p, yamlLine(&root, []string{"splatMaps", strconv.Itoa(i)}), proj, sm, "terrain splat map")
	}
	for i, l := range doc.Layers {
		li := strconv.Itoa(i)
		if l.Albedo != "" {
			c.imageRef(p, yamlLine(&root, []string{"layers", li, "albedo"}), proj, l.Albedo, "terrain layer albedo")
		}
		if l.Normal != "" {
			c.imageRef(p, yamlLine(&root, []string{"layers", li, "normal"}), proj, l.Normal, "terrain layer normal")
		}
		if l.MetallicRoughness != "" {
			c.imageRef(p, yamlLine(&root, []string{"layers", li, "metallicRoughness"}), proj, l.MetallicRoughness, "terrain layer metallicRoughness")
		}
		if l.Height != "" {
			c.imageRef(p, yamlLine(&root, []string{"layers", li, "height"}), proj, l.Height, "terrain layer height")
		}
	}
}

// checkFoliage checks the 3D foliage definition at p: that world3d.ParseFoliage
// parses and validates it, and that its referenced terrain, meshes, materials,
// and density maps exist and decode.
func (c *checker) checkFoliage(p string) {
	c.report.Foliage++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := world3d.ParseFoliage(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err)
		return
	}
	proj := c.projectRoot(path.Dir(p))
	if doc.Terrain != "" {
		c.fileRef(p, yamlLine(&root, []string{"terrain"}), proj, doc.Terrain, "foliage terrain")
	}
	for i, l := range doc.Layers {
		li := strconv.Itoa(i)
		c.fileRef(p, yamlLine(&root, []string{"layers", li, "mesh"}), proj, l.Mesh, "foliage mesh")
		if l.Material != "" {
			c.fileRef(p, yamlLine(&root, []string{"layers", li, "material"}), proj, l.Material, "foliage material")
		}
		if l.DensityMap != "" {
			c.imageRef(p, yamlLine(&root, []string{"layers", li, "densityMap"}), proj, l.DensityMap, "foliage densityMap")
		}
	}
}
