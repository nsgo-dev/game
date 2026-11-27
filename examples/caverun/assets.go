package main

import (
	"embed"
	"fmt"
	"io/fs"
	"path"

	"github.com/nsgo-dev/game/pkg/assets"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

// titleAssets embeds only the bitmap font needed for the title screen so the
// first frame renders immediately; the level group (sprites, tileset, tilemap,
// sound effects and music) lives under resources/ and loads through
// assets.Manager from the app's resources on every target.
//
//go:embed assets/font.fnt assets/font.png
var titleAssets embed.FS

// gameAssets holds the embedded title font, the asset manager that streams the
// level group from resources/, and the resolved level assets and audio sources.
type gameAssets struct {
	mgr        *assets.Manager
	font       *text.Font
	atlas      *sprite.Atlas
	tilesetImg core.ImageSource
	levelJSON  []byte

	sfxJump   audio.Source
	sfxGem    audio.Source
	sfxHurt   audio.Source
	sfxWin    audio.Source
	musicCave audio.Source
}

func loadTitleAssets(cx *core.Scope) (*gameAssets, error) {
	fontPNG, err := titleAssets.ReadFile("assets/font.png")
	if err != nil {
		return nil, fmt.Errorf("read font.png: %w", err)
	}
	fontFNT, err := titleAssets.ReadFile("assets/font.fnt")
	if err != nil {
		return nil, fmt.Errorf("read font.fnt: %w", err)
	}
	fontImg := core.ImageSource{
		URI:  "asset:font.png",
		Data: fontPNG,
		Size: core.Size{Width: 128, Height: 48},
	}
	font, err := text.LoadBMFontImage(fontImg, fontFNT)
	if err != nil {
		return nil, fmt.Errorf("load bitmap font: %w", err)
	}

	ga := &gameAssets{
		font:      font,
		sfxJump:   audio.Source{URI: "asset:sfx_jump.wav"},
		sfxGem:    audio.Source{URI: "asset:sfx_gem.wav"},
		sfxHurt:   audio.Source{URI: "asset:sfx_hurt.wav"},
		sfxWin:    audio.Source{URI: "asset:sfx_win.wav"},
		musicCave: audio.Source{URI: "asset:music_cave.wav"},
	}

	if cx != nil {
		ga.mgr = assets.NewManager(cx)
		ga.mgr.Group("level1").
			AddAtlas("sprites", "asset:sprites.json", core.ImageSource{
				URI:  "asset:sprites.png",
				Size: core.Size{Width: 128, Height: 64},
			}).
			AddImage("tileset", core.ImageSource{
				URI:  "asset:tileset.png",
				Size: core.Size{Width: 64, Height: 32},
			}).
			AddBytes("level1", "asset:level1.tmj")
	}

	return ga, nil
}

// loadLevelGroup asynchronously loads Cave Run's level group from the app's
// resources/ directory through assets.Manager and populates atlas, tilesetImg
// and levelJSON on the engine goroutine before calling onDone.
func (a *gameAssets) loadLevelGroup(onDone func(error)) {
	if a.mgr == nil {
		if onDone != nil {
			onDone(fmt.Errorf("asset manager not initialized"))
		}
		return
	}
	a.mgr.LoadGroup("level1", func(err error) {
		if err != nil {
			if onDone != nil {
				onDone(err)
			}
			return
		}
		atlas, ok := a.mgr.Atlas("sprites")
		if !ok {
			if onDone != nil {
				onDone(fmt.Errorf("missing sprites atlas in level1 group"))
			}
			return
		}
		tilesetImg, ok := a.mgr.Image("tileset")
		if !ok {
			if onDone != nil {
				onDone(fmt.Errorf("missing tileset image in level1 group"))
			}
			return
		}
		levelJSON, ok := a.mgr.Bytes("level1")
		if !ok {
			if onDone != nil {
				onDone(fmt.Errorf("missing level1 map in level1 group"))
			}
			return
		}
		a.atlas = atlas
		a.tilesetImg = tilesetImg
		a.levelJSON = levelJSON
		if onDone != nil {
			onDone(nil)
		}
	})
}

// loadGameAssetsFromFS loads the title font from embedded assets and the level
// assets synchronously from fsys (such as os.DirFS("resources")) for headless
// unit tests and benchmarks that run without an NSGo app scope.
func loadGameAssetsFromFS(fsys fs.FS) (*gameAssets, error) {
	ga, err := loadTitleAssets(nil)
	if err != nil {
		return nil, err
	}
	spritesPNG, err := fs.ReadFile(fsys, "sprites.png")
	if err != nil {
		return nil, fmt.Errorf("read sprites.png: %w", err)
	}
	spritesJSON, err := fs.ReadFile(fsys, "sprites.json")
	if err != nil {
		return nil, fmt.Errorf("read sprites.json: %w", err)
	}
	tilesetPNG, err := fs.ReadFile(fsys, "tileset.png")
	if err != nil {
		return nil, fmt.Errorf("read tileset.png: %w", err)
	}
	levelJSON, err := fs.ReadFile(fsys, "level1.tmj")
	if err != nil {
		return nil, fmt.Errorf("read level1.tmj: %w", err)
	}
	spritesImg := core.ImageSource{
		URI:  "asset:sprites.png",
		Data: spritesPNG,
		Size: core.Size{Width: 128, Height: 64},
	}
	atlas, err := sprite.LoadAtlas(spritesImg, spritesJSON)
	if err != nil {
		return nil, fmt.Errorf("load sprite atlas: %w", err)
	}
	ga.atlas = atlas
	ga.tilesetImg = core.ImageSource{
		URI:  "asset:tileset.png",
		Data: tilesetPNG,
		Size: core.Size{Width: 64, Height: 32},
	}
	ga.levelJSON = levelJSON
	return ga, nil
}

func (a *gameAssets) loadLevelMap() (*tilemap.Tilemap, error) {
	return tilemap.LoadTiled(a.levelJSON, tilemap.TilesetLoader{
		LoadImage: func(imagePath string) (core.ImageSource, error) {
			if path.Base(imagePath) == "tileset.png" {
				return a.tilesetImg, nil
			}
			return core.ImageSource{URI: imagePath}, nil
		},
	})
}
