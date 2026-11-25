// Package cutscene plays video and in-engine timeline cutscenes in a game: in a scene or between scenes, skippable by a press, a key or back, and calling the game back when one ends or is skipped.
//
// A cutscene is a Cutscene, written in YAML (Parse, Encode) in either version 1 (video only) or version 2 (video or timeline).
//
// A video cutscene names a video source, mixer group, skip rules, picture fit and transparency; Player plays it in NSGo's own video view (core.Video) over the game's view while pausing the game's loop and suspending its mixer.
//
// A timeline cutscene names a length and ordered camera, actor, dialogue (localization line IDs only), sound and music tracks, with clips carrying start, duration and easing. TimelinePlayer runs one deterministically on game.Loop's fixed steps against a sceneformat.BuiltScene or sceneformat.SceneDoc (with Seek for scrubbing), and Player.PlayTimeline wires the same skip input, lifecycle and between-scene transitions as video cutscenes.
package cutscene
