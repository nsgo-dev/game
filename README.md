# game

A game layer for [NSGo](https://github.com/nsgo-dev/nsgo) apps, written in Go, in two stages:

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top.
2. **3D**: meshes, cameras, lights and glTF models on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)).

The loop, input actions, sound and scene switching are shared by both stages. It uses only NSGo's public API, so an app that does not import it links none of it.

## Status

Not started. The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047), and its tasks are this repository's issues. The engine-side primitives it needs (key-up and held keys, display frame times, gamepads, audio everywhere, a batched `DrawAtlas`, the GPU view) are tracked in NSGo.

## Targets

macOS, iOS, Windows, Linux, Android and the web.
