// Package assetbuild builds a game's assets from the assets section of its game.yaml, which package project defines: it packs sprite atlases, encodes KTX2 textures, converts audio and generates the Go asset manifest, and keeps a text cache of what each output was built from so a later run rebuilds only the targets whose inputs or settings changed.
package assetbuild
