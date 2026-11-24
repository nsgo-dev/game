// Package importsettings reads and writes import settings sidecars: a small YAML file beside an image or a sound, named after it with ".import.yaml" appended (Path), that says how game assets imports it, as Godot's .import files do.
//
// An image's sidecar holds its texture compression format, mipmaps, sRGB and sampling filter, which a texture target encodes it with, and the grid an atlas target slices a sprite sheet into frames along. A sound's sidecar holds its output format, sample rate, channels, loudness, bitrate and loop points, which an audio target converts it with. A setting in the sidecar overrides the target's setting in game.yaml, so a person edits one asset's import by editing the file beside it, and game assets rebuilds the targets that read the asset when its sidecar changes.
//
// Parse decodes a version 1 sidecar and runs Validate, refusing unknown fields, other versions, a sidecar with no section or with both, and values out of range. Encode validates a File and writes deterministic 2-space YAML in a fixed key order, after a first line that points a YAML language server at SchemaURL, which Parse reads back unchanged.
package importsettings
