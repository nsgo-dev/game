// Package project reads, validates, writes and applies game.yaml, the one project file at the root of a game that holds its settings.
//
// # The Project File
//
// A game.yaml names the game (name, id, appVersion) and holds its window and virtual resolution (window, display), its input map (input), its audio buses (audio), the scenes and resources it loads at startup (autoload), its export presets per platform (export), the editor plugins the scene editor turns on for it (editor) and the asset pipeline targets game assets builds (assets). Parse decodes a version 1 file into a File and runs Validate, refusing unknown fields, unsupported versions, duplicate names and out-of-range values. Encode validates a File and writes deterministic 2-space YAML in a fixed key order that Parse reads back unchanged, so a load and save never alters a file Encode wrote.
//
// # The Runtime
//
// A game embeds its game.yaml and reads it with Parse (or MustParse for a file that is part of the program). File.WindowSize, File.WindowConstraints and File.Appearance feed nsgo.Run's options, File.Viewport is the screen.Viewport of the virtual resolution, File.BindInput binds the input map into an input.Actions, File.ConfigureMixer gives a sound.Mixer each bus's default volume and DSP chain, and File.LoadAutoloads reads the autoloaded scenes (as sceneformat documents) and resources from an fs.FS.
//
// # The Tools
//
// game.yaml owns the fields NSGo's own nsgo.yaml repeats: the app's name, id and version, its appearance and its build output and targets. SyncNSGo writes them into an nsgo.yaml, keeping its other keys and comments, and CheckNSGo reports where an nsgo.yaml disagrees. The gameproject command runs these on a project directory, and the editor opens and saves game.yaml (editor.Editor.OpenProject, EditProject and SaveProject). PathRefs lists every path game.yaml holds to a file or folder of the project with the YAML node that writes it, and File.EachPath visits the same paths in a parsed File, so that package refs can check and rewrite them when a file moves.
package project
