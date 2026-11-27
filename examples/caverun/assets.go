package main

import (
	"embed"
	"fmt"
	"path"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed assets/*
var embeddedAssets embed.FS

// gameAssets holds all decoded images, sprite atlas, bitmap font, map bytes, and audio sources.
type gameAssets struct {
	atlas      *sprite.Atlas
	font       *text.Font
	tilesetImg core.ImageSource
	levelJSON  []byte

	sfxJump   audio.Source
	sfxGem    audio.Source
	sfxHurt   audio.Source
	sfxWin    audio.Source
	musicCave audio.Source
}

func loadGameAssets() (*gameAssets, error) {
	spritesPNG, err := embeddedAssets.ReadFile("assets/sprites.png")
	if err != nil {
		return nil, fmt.Errorf("read sprites.png: %w", err)
	}
	spritesJSON, err := embeddedAssets.ReadFile("assets/sprites.json")
	if err != nil {
		return nil, fmt.Errorf("read sprites.json: %w", err)
	}
	tilesetPNG, err := embeddedAssets.ReadFile("assets/tileset.png")
	if err != nil {
		return nil, fmt.Errorf("read tileset.png: %w", err)
	}
	autoloaded, err := gameProject.LoadAutoloads(embeddedAssets)
	if err != nil {
		return nil, err
	}
	levelJSON := autoloaded.Resources["level1"]
	fontPNG, err := embeddedAssets.ReadFile("assets/font.png")
	if err != nil {
		return nil, fmt.Errorf("read font.png: %w", err)
	}
	fontFNT, err := embeddedAssets.ReadFile("assets/font.fnt")
	if err != nil {
		return nil, fmt.Errorf("read font.fnt: %w", err)
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

	fontImg := core.ImageSource{
		URI:  "asset:font.png",
		Data: fontPNG,
		Size: core.Size{Width: 128, Height: 48},
	}
	font, err := text.LoadBMFontImage(fontImg, fontFNT)
	if err != nil {
		return nil, fmt.Errorf("load bitmap font: %w", err)
	}

	tilesetImg := core.ImageSource{
		URI:  "asset:tileset.png",
		Data: tilesetPNG,
		Size: core.Size{Width: 64, Height: 32},
	}

	readAudio := func(name string) (audio.Source, error) {
		data, readErr := embeddedAssets.ReadFile(path.Join("assets", name))
		if readErr != nil {
			return audio.Source{}, fmt.Errorf("read %s: %w", name, readErr)
		}
		return audio.Source{URI: "asset:" + name, Data: data}, nil
	}

	sfxJump, err := readAudio("sfx_jump.wav")
	if err != nil {
		return nil, err
	}
	sfxGem, err := readAudio("sfx_gem.wav")
	if err != nil {
		return nil, err
	}
	sfxHurt, err := readAudio("sfx_hurt.wav")
	if err != nil {
		return nil, err
	}
	sfxWin, err := readAudio("sfx_win.wav")
	if err != nil {
		return nil, err
	}
	musicCave, err := readAudio("music_cave.wav")
	if err != nil {
		return nil, err
	}

	return &gameAssets{
		atlas:      atlas,
		font:       font,
		tilesetImg: tilesetImg,
		levelJSON:  levelJSON,
		sfxJump:    sfxJump,
		sfxGem:     sfxGem,
		sfxHurt:    sfxHurt,
		sfxWin:     sfxWin,
		musicCave:  musicCave,
	}, nil
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
