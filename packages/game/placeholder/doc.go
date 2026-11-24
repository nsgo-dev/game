// Package placeholder is a small asset pack for prototypes, tutorials and the examples, dedicated to the public domain under CC0 1.0 (License): sprite sheets, a tileset with a Tiled tileset and a sample level, a pixel font, sound effects and a music loop, and three glTF models.
//
// Every file was drawn, synthesized or modeled in Go for this repository by the generator under internal/gen, which writes the same bytes on every run, and FS embeds them. Each image and sound has the import settings sidecar game assets reads (package importsettings): a sprite sheet's slices it into 16 by 16 frames, the tileset's and the font page's keep their pixels sharp, and a sound's sets its loudness and, for the music, its loop. CREDITS.md in FS lists every file with its license, so a game that ships the pack's files can ship its credits too.
//
// Sheet, Font, Level, Model and Sound load the pack's assets ready for packages sprite, text, tilemap, gltf and NSGo's audio. A game that copies the files into its own project instead builds them with game assets like any other art.
package placeholder
