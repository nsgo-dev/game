// Package sceneformat reads, validates, encodes and builds versioned YAML scene documents for NSGo games.
//
// # Editor Scope Decision
//
// NSGo's game layer keeps Tiled and Go code first-class while growing into a lightweight scene editor built with NSGo's own UI views (option b growing into c, tracked in #159). Rather than building a monolithic IDE or locking scenes inside an opaque binary format, both hand-authored scenes and the future NSGo-native editor (object placement, tile painting, typed property editing, undo/redo, play-in-editor and asset browsing) share the human-editable YAML format defined here (#160).
//
// # Scene Documents and Runtime Building
//
// Parse decodes a version 1 YAML scene into a SceneDoc and runs Validate to reject unknown fields, unsupported versions, duplicate layer names or object IDs, invalid coordinate spaces, malformed colors and bad transforms. Encode validates a SceneDoc and writes deterministic 2-space YAML that round-trips through Parse.
//
// Build resolves a SceneDoc's atlas and tilemap references through an AssetResolver and produces a BuiltScene holding sprite.Batch values ready to draw into a core.Drawing. BuiltScene.Draw paints the background color over the virtual viewport, applies the camera transform to world-space layers, leaves screen-space layers untransformed, and preserves exact layer order and per-layer Z order.
package sceneformat
