// Package cutscene plays video cutscenes in a game: in a scene or between scenes, over the game's view, with the game's loop and sound paused while one plays, skippable by a press, a key or back, and calling the game back when it ends or is skipped.
//
// A cutscene is a Cutscene: the video's source, the mixer group whose volume it plays at, whether and when it may be skipped, how its picture fits the screen and whether the game shows through it. Its YAML form (Parse, Encode) is the file a game's author writes for one.
//
// A Player plays them in NSGo's own video view (core.Video), which Player.View puts over the game's view. The platform's player decodes and plays the video, so its sound does not go through the game's mixer: the mixer's volumes set how loud it plays, but its bus effects do not apply, and nothing the game draws can go on top of the picture except views over it.
package cutscene
