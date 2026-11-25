package scenemodel

import (
	"fmt"
	"image"
	_ "image/jpeg" // decode the JPEG textures material files name
	_ "image/png"  // decode the PNG textures material files name
	"io/fs"
	"sync"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// ModelResolver loads the glTF models objects name in sceneformat.ObjectDoc.Model, as sceneformat.MaterialResolver loads their material files. path is the slash-separated path from the project root that the scene file writes, such as "models/crate.glb". A resolver that returns the same document for the same path lets every object that names it share its decoded textures.
//
// The ModelResolver given to Build may also implement sceneformat.MaterialResolver, to resolve the material files objects name in ObjectDoc.Material, ImageResolver, to load the textures those material files name, and GridMapResolver, to load the 3D grid maps objects name in ObjectDoc.GridMap. Assets implements all four.
type ModelResolver interface {
	Model(path string) (*gltf.Document, error)
}

// GridMapResolver loads the 3D grid map files objects name in sceneformat.ObjectDoc.GridMap.
// path is the slash-separated path from the project root that the scene file writes, such as "levels/dungeon.gridmap.yaml".
type GridMapResolver interface {
	GridMap(path string) (*world3d.GridMapDoc, error)
}

// ImageResolver loads the image at path, a slash-separated path from the project root, for the texture of a material file (resource.Material.Texture).
type ImageResolver interface {
	Image(path string) (image.Image, error)
}

// Assets loads models, grid maps, material files and images from a project's files, such as os.DirFS(projectRoot) or an embed.FS, and keeps each one it loaded, so it serves Build as a ModelResolver, a GridMapResolver, a sceneformat.MaterialResolver and an ImageResolver. Models load with gltf.LoadFS, so a .gltf file's separate buffers and images are read from beside it; grid maps parse with world3d.ParseGridMapWith; material files load through a resource.Library, which shares one value per path; images decode as PNG or JPEG.
//
// Its methods are safe to call from any goroutine.
type Assets struct {
	fsys      fs.FS
	materials *resource.Library

	mu       sync.Mutex
	models   map[string]*gltf.Document
	gridMaps map[string]*world3d.GridMapDoc
	images   map[string]image.Image
}

// NewAssets returns Assets that read from fsys, rooted at the project root.
func NewAssets(fsys fs.FS) *Assets {
	return &Assets{
		fsys:      fsys,
		materials: resource.NewLibrary(fsys),
		models:    make(map[string]*gltf.Document),
		gridMaps:  make(map[string]*world3d.GridMapDoc),
		images:    make(map[string]image.Image),
	}
}

// Model loads the glTF model at path, a .gltf or .glb file, on first use and returns the same document after that.
func (a *Assets) Model(path string) (*gltf.Document, error) {
	if err := a.check(path); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if doc, ok := a.models[path]; ok {
		return doc, nil
	}
	doc, err := gltf.LoadFS(a.fsys, path)
	if err != nil {
		return nil, err
	}
	a.models[path] = doc
	return doc, nil
}

// GridMap loads and parses the YAML grid map at path (*.gridmap.yaml or *.gridmap.yml) on first use and returns the same document after that.
func (a *Assets) GridMap(path string) (*world3d.GridMapDoc, error) {
	if err := a.check(path); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if doc, ok := a.gridMaps[path]; ok {
		return doc, nil
	}
	data, err := fs.ReadFile(a.fsys, path)
	if err != nil {
		return nil, fmt.Errorf("scenemodel: gridMap %s: %w", path, err)
	}
	doc, err := world3d.ParseGridMapWith(data, path)
	if err != nil {
		return nil, err
	}
	a.gridMaps[path] = doc
	return doc, nil
}

// Material loads the material file at path through the Assets' resource.Library. It lets Assets serve as a sceneformat.MaterialResolver.
func (a *Assets) Material(path string) (*resource.Material, error) {
	if err := a.check(path); err != nil {
		return nil, err
	}
	return a.materials.Material(path)
}

// Library returns the resource.Library that Material loads from, so a game can reload edited material files into it (resource.Library.Reload).
func (a *Assets) Library() *resource.Library {
	if a == nil {
		return nil
	}
	return a.materials
}

// Image decodes the PNG or JPEG image at path on first use and returns the same image after that.
func (a *Assets) Image(path string) (image.Image, error) {
	if err := a.check(path); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if img, ok := a.images[path]; ok {
		return img, nil
	}
	f, err := a.fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("scenemodel: image %s: %w", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("scenemodel: image %s: %w", path, err)
	}
	a.images[path] = img
	return img, nil
}

// check returns why a cannot read path, or nil.
func (a *Assets) check(path string) error {
	switch {
	case a == nil || a.fsys == nil:
		return fmt.Errorf("scenemodel: no file system to load %q from", path)
	case !fs.ValidPath(path) || path == ".":
		return fmt.Errorf("scenemodel: %q is not a slash-separated path relative to the project root", path)
	}
	return nil
}
