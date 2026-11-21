// Package game is a game layer for NSGo apps: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound, built on NSGo's public API only, so an app that does not import it links none of it.
//
// This package holds Loop, which the others share. Each domain has a package of its own: math2d for 2D vectors, rectangles, circles and affine transforms, sprite for atlases, sprites, animations and batches, and sound for the mixer, its effects and music; input, tilemap (with the camera) and scene follow, and examples holds sample games.
package game
