// Package resource reads, validates and writes shared resource files: reusable data that scenes and code reference by path instead of repeating it, as Godot's .tres files are.
//
// # Files
//
// A resource file is one YAML document that starts with a common header, the resource's type, the schema version and an optional uid, followed by the fields of that type:
//
//	type: material
//	version: 1
//	baseColor: "#8a8f99"
//	opacity: 0.75
//	blend: multiply
//
// The uid (Header.UID) is the file's stable ID, made once by NewUID, which stays the same when the file is renamed or moved so that package refs can find it again.
//
// The types are material (Material), curve (Curve), gradient (Gradient), animation (Animation), theme (Theme), busLayout (BusLayout) and physicsMaterial (PhysicsMaterial). Parse reads any of them and returns the matching type; it refuses an unknown type, a version other than CurrentVersion, unknown fields, trailing documents and values out of range. Encode writes deterministic 2-space YAML with fields in a fixed order, so a file that Encode wrote reads back with Parse and encodes to the same bytes.
//
// # Sharing
//
// A Library loads resource files from an fs.FS by slash-separated path and keeps one value per path, so every scene and every piece of code that loads the same path holds the same pointer. Library.Reload replaces a loaded resource's contents in place, so an edit to one file reaches everything that uses it; hotreload.Watcher.WatchResources calls it when a file changes on disk.
//
// sceneformat objects reference a material by path (ObjectDoc.Material) and resolve it through a resolver such as a Library, so two scenes that use one material file both change when it is edited.
package resource
