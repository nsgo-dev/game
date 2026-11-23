// Package sceneformat reads, validates, encodes and builds versioned YAML scene documents for NSGo games.
//
// # Editor Scope Decision
//
// NSGo's game layer keeps Tiled and Go code first-class while growing into a lightweight scene editor built with NSGo's own UI views (option b growing into c, tracked in #159). Rather than building a monolithic IDE or locking scenes inside an opaque binary format, both hand-authored scenes and the future NSGo-native editor (object placement, tile painting, typed property editing, undo/redo, play-in-editor and asset browsing) share the human-editable YAML format defined here (#160).
//
// # Scene Documents and Runtime Building
//
// Parse decodes a version 1 or 2 YAML scene into a SceneDoc and runs Validate to reject unknown fields, unsupported versions, duplicate layer names or object IDs, invalid coordinate spaces, malformed colors and bad transforms. Encode validates a SceneDoc and writes deterministic 2-space YAML that round-trips through Parse.
//
// Build resolves a SceneDoc's atlas and tilemap references through an AssetResolver and produces a BuiltScene holding sprite.Batch values ready to draw into a core.Drawing. BuiltScene.Draw paints the background color over the virtual viewport, applies the camera transform to world-space layers, leaves screen-space layers untransformed, and preserves exact layer order and per-layer Z order. An object may name a shared material file (ObjectDoc.Material); Build resolves it through a MaterialResolver such as resource.Library, and the object's sprite draws with the material's current opacity and blend mode, so every scene using the file follows its edits.
//
// # Node Tree
//
// An ObjectDoc holds child objects in Children, each with a transform local to its parent (#484). Build composes the tree into BuiltObject.World and WorldAlpha and draws child sprites at their world transforms, SpawnEntities builds it into an entities.World's Hierarchy2D, and BuildNodes3D builds it into a scene3d node tree. WalkObjects visits a layer's objects depth first, each parent before its children.
//
// # Instancing and Inheritance
//
// A version 2 scene can reuse other scene files, as Godot's scenes do (#483). An object that names a scene file in ObjectDoc.Scene instances it: the instanced scene's objects become the object's children, under IDs prefixed with the object's ID and a slash, and ObjectDoc.Overrides changes them by their IDs in that scene. A scene that names a base scene in SceneDoc.Inherits starts as a copy of the base and changes only what it declares: its own scene fields, SceneDoc.Overrides for inherited objects, SceneDoc.LayerOverrides for inherited layers, and its layers, which add objects to the base's layers of the same name or add new layers. Parse and Encode keep the references and overrides as written, so a scene of instances round-trips unchanged. Resolve loads the referenced files through a SceneResolver, such as FSScenes, refuses cycles, and returns a self-contained copy; Build resolves a scene itself when its AssetResolver also implements SceneResolver, while SpawnEntities and BuildNodes3D take a resolved scene.
package sceneformat
