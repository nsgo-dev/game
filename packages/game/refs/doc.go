// Package refs keeps the references between a game project's files working when files are renamed or moved: it finds every reference, rewrites the ones a move breaks, and finds a file again after it was moved without the tools.
//
// # References
//
// Scan reads a project and lists every reference one of its files makes to another, with the file, line and column that holds it:
//
//   - game.yaml (project.FileName): the path of each autoloaded scene and resource;
//   - scenes (sceneformat), the files named *.scene.yaml or *.scene.yml and every other YAML file a reference names as a scene: the scene a scene inherits, the scene an object instances, an object's material and an override's material, and a tilemap's source;
//   - resource files (resource), YAML files whose header has a resource type: a material's texture;
//   - Tiled maps (.tmj) and tilesets (.tsj): external tilesets, tileset and tile images and image layers;
//   - LDtk projects (.ldtk): tileset images, level backgrounds and levels saved in separate files.
//
// A path in game.yaml, a scene or a resource file is relative to the project root, the nearest directory above the file that holds game.yaml, nsgo.yaml or go.mod, or the scanned directory when none does. A path in a Tiled or LDtk file is relative to that file's directory, as those tools write it. Atlas, tileset and prefab names in scenes are names an AssetResolver looks up, not paths, and are not references to a file.
//
// # Stable IDs and the Index
//
// Every scene and resource file carries a stable ID in its header, a uid field (sceneformat.SceneDoc.UID, resource.Header.UID) that a tool writes once with resource.NewUID and that never changes, so it travels with the file through any rename or move. The project index, IndexFileName at the project root, records where each file was the last time a tool looked: each scene and resource file's path with its uid, and each other file a reference names, such as an image or a Tiled map, with the SHA-256 of its contents. It is a YAML file the tools keep (Index), written in a fixed order so that it changes only when files do, and it belongs in version control with the files it describes.
//
// When a reference names a file that is gone, Project.Moved looks the old path up in the index and finds the file again: the scene or resource file that now holds the same uid, or the one file elsewhere in the project with the same extension and contents that the index does not list. game check reports the broken reference with its file and line and where the file went.
//
// # Moving Files
//
// PlanMove plans the move of a file or directory and the rewrite of every reference to what it moves, the moved files' own relative references included, and PlanIndex plans what game index does: point every broken reference whose file Moved finds at the file's new path, give every scene and resource file without a uid a new one, give a copied file that shares its original's uid a new one, and bring each index up to date. A Plan only describes the changes, so a caller can show them first; Plan.Apply makes them in a directory on disk. A reference is rewritten in place, keeping the rest of its file byte for byte, comments and formatting included, and a uid is added as one line after the file's version, so a move shows in review as the lines it changes. The game mv and game index commands and the editor's MoveFile run these.
package refs
