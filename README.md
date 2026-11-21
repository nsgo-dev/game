# game

A 2D game layer for [NSGo](https://github.com/nsgo-dev/nsgo) apps, written in Go. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top.

It uses only NSGo's public API, so an app that does not import it links none of it.

## Status

Not started. The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047), and its tasks are this repository's issues. The engine-side primitives it needs (key-up and held keys, display frame times, gamepads, audio everywhere, a batched `DrawAtlas`) are tracked in NSGo.

## Targets

macOS, iOS, Windows, Linux, Android and the web.
