package placeholder

import (
	"bytes"
	"embed"
	"fmt"
	"image/png"
	"io/fs"
	"path"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/text"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// License is the SPDX identifier of the license of every file in the pack: Creative Commons Zero v1.0 Universal, a dedication to the public domain.
const License = "CC0-1.0"

//go:embed pack
var embedded embed.FS

// FS holds the pack's files by their slash-separated paths, such as "sprites/player.png": the sprite sheets with their JSON and import settings sidecars under sprites/, the tileset, its Tiled tileset and a sample level under tiles/, the bitmap font under fonts/, the sounds under sounds/, the models under models/, and CREDITS.md, which lists every file with its license.
var FS fs.FS = mustSub(embedded, "pack")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Sheet loads the sprite sheet sprites/<name>.png with its frames and animations from sprites/<name>.json: "player", "slime", "bat", "coin", "items", "effects" or "ui". Frame i is named <name>_<i>, and each animation is named after its Aseprite tag, such as "run" in the player's sheet or "heart" in the items'.
func Sheet(name string) (*sprite.Atlas, error) {
	file := "sprites/" + name + ".json"
	data, err := fs.ReadFile(FS, file)
	if err != nil {
		return nil, fmt.Errorf("placeholder: no sprite sheet %q: %w", name, err)
	}
	img, err := Image("sprites/" + name + ".png")
	if err != nil {
		return nil, err
	}
	a, err := sprite.LoadAtlas(img, data)
	if err != nil {
		return nil, loaderr.WithFile(err, file)
	}
	return a, nil
}

// Font loads the pack's proportional pixel font, fonts/pixel.fnt and its page fonts/pixel.png: printable ASCII, 7 pixels tall above the baseline with descenders below it, lines 10 pixels apart.
func Font() (*text.Font, error) {
	const file = "fonts/pixel.fnt"
	data, err := fs.ReadFile(FS, file)
	if err != nil {
		return nil, fmt.Errorf("placeholder: %w", err)
	}
	img, err := Image("fonts/pixel.png")
	if err != nil {
		return nil, err
	}
	f, err := text.LoadBMFontImage(img, data)
	if err != nil {
		return nil, loaderr.WithFile(err, file)
	}
	return f, nil
}

// Level loads the sample level tiles/level.tmj: 40 by 15 tiles of 16 pixels on the tileset tiles/tileset.tsj, with a "decor" and a "ground" tile layer and an "objects" layer of points whose types are spawn, coin, gem, slime and bat, and an exit rectangle. The tileset's tiles carry the properties solid, oneWay, ladder, hazard and water, so the map's tile flags mark solid and one-way tiles, and water, lava and the torch are animated.
func Level() (*tilemap.Tilemap, error) {
	const file = "tiles/level.tmj"
	data, err := fs.ReadFile(FS, file)
	if err != nil {
		return nil, fmt.Errorf("placeholder: %w", err)
	}
	m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{
		LoadJSON: func(source string) ([]byte, error) {
			return fs.ReadFile(FS, path.Join("tiles", source))
		},
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			return Image(path.Join("tiles", imagePath))
		},
	})
	if err != nil {
		return nil, loaderr.WithFile(err, file)
	}
	return m, nil
}

// Model loads the glTF model models/<file>: "crate.glb", a 1 m cube; "robot_arm.gltf", a two-bone arm, skinned, with a "Wave" animation; or "gem.glb", a gem 1 m tall. Each stands on the origin, +Y up and -Z forward.
func Model(file string) (*gltf.Document, error) {
	return gltf.LoadFS(FS, "models/"+file)
}

// Sound returns the WAV file sounds/<name>.wav as an audio source: the effects "jump", "coin", "hit", "explosion", "shoot", "powerup", "click" and "select", or "music_loop", four bars that loop without a seam.
func Sound(name string) (core.AudioSource, error) {
	data, err := fs.ReadFile(FS, "sounds/"+name+".wav")
	if err != nil {
		return core.AudioSource{}, fmt.Errorf("placeholder: no sound %q: %w", name, err)
	}
	return core.AudioSource{Data: data}, nil
}

// Image returns the PNG image at the slash-separated path file in FS as an image source of its PNG data, sized in points as it is in pixels.
func Image(file string) (core.ImageSource, error) {
	data, err := fs.ReadFile(FS, file)
	if err != nil {
		return core.ImageSource{}, fmt.Errorf("placeholder: %w", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return core.ImageSource{}, fmt.Errorf("placeholder: %s: %w", file, err)
	}
	return core.ImageSource{Data: data, Size: core.Size{Width: core.Points(cfg.Width), Height: core.Points(cfg.Height)}}, nil
}
