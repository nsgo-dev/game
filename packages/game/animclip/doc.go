// Package animclip plays animation clips, the resource files of type animation that key any registered property of scene objects on a timeline, as Godot's AnimationPlayer does.
//
// # Properties
//
// A clip track names its property as written in the scene file: an object field such as "x", "rotation", "alpha" or the discrete atlas frame property "sprite.frame" (ObjectProperties lists them), or a registered component field written "component.field", such as "body.speed". Component fields are keyable when a sceneformat.ComponentRegistry registers them with a numeric or bool kind; a string field has no number to key. Lookup resolves a name and Check checks a whole clip against a registry: every property must resolve, numeric tracks must hold values their property can take (0 or 1 for a bool, a whole number for an int, inside the field's range), and sprite.frame tracks must hold discrete frame keys.
//
// # Flipbook clips
//
// FromAsepriteTag and FromSpriteAnimation build a sprite.frame clip matching an Aseprite tag's frames, exact durations and loop mode; FromFrames builds a clip from a run of atlas frame names at a fixed frame rate.
//
// # Playing
//
// NewPlayer turns a clip into a tween.Timeline: one tween per pair of neighboring keys, so playback, seeking, looping and the clip's events run on the same deterministic clock as every other timeline. A Binder connects each track to what it drives; BindDoc writes into a sceneformat.SceneDoc and BindScene into a sceneformat.BuiltScene, keeping its sprites and their atlas frames in step, and a game can bind its own state with a Binder of its own.
//
// The editor previews a clip with the same Player and BindScene, so a clip keyed in the editor plays the same values at the same times in the game.
package animclip
