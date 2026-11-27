package sceneformat

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/nsgo-dev/game/pkg/sprite"
)

// FSAssets is an AssetResolver that reads a project's files from a file system: its scene files (SceneResolver), and through FileResolver the YAML tilesets and Tiled and LDtk maps its tilemaps name, so Build builds a scene whose layers use them with no resolver of the game's own. Atlases come from Atlases, and images are named by their paths (core.ImageSource{URI: path}).
type FSAssets struct {
	// FS holds the project's files, with paths relative to the project root, such as os.DirFS of the directory that holds game.yaml.
	FS fs.FS
	// Atlases are the sprite atlases the scene's sprites and atlas tilesets name.
	Atlases AtlasMap
}

// errNilFS is returned by FSAssets with no file system.
var errNilFS = errors.New("sceneformat: FSAssets has a nil file system")

// Atlas returns the atlas named name from a.Atlases.
func (a FSAssets) Atlas(name string) (*sprite.Atlas, error) {
	if a.Atlases == nil {
		return nil, fmt.Errorf("atlas %q not found", name)
	}
	return a.Atlases.Atlas(name)
}

// ReadFile reads the file at path from a.FS.
func (a FSAssets) ReadFile(path string) ([]byte, error) {
	if a.FS == nil {
		return nil, errNilFS
	}
	return fs.ReadFile(a.FS, path)
}

// Scene reads the scene file at path from a.FS and parses it, as FSScenes does.
func (a FSAssets) Scene(path string) (*SceneDoc, error) {
	if a.FS == nil {
		return nil, errNilFS
	}
	return FSScenes{FS: a.FS}.Scene(path)
}
