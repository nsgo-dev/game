# animclip

`game/animclip` plays animation clips, the [resource files](resources.md#animation) of type `animation`, on scene objects: any registered property keyed on a timeline, as Godot's AnimationPlayer does. The [editor](editor.md#animation-editor) keys clips with the same package, so a clip plays the same in the game as in the editor's preview.

```go
import "github.com/nsgo-dev/game/packages/game/animclip"
```

## Properties

A track's `property` is written as in the scene file:

- an object field: `x`, `y`, `rotation`, `scaleX`, `scaleY`, `alpha` (0 to 1), `originX`, `originY`, `flipX`, `flipY`, `posZ`, `rotX`, `rotY`, `rotZ` and `scaleZ` (`ObjectProperties` lists them with their documentation);
- a registered component field, written `component.field` and split at the first dot, such as `body.speed`. The field must be registered in a `sceneformat.ComponentRegistry` (see [Component types](editor.md#component-types)) as a `FieldFloat`, `FieldInt` or `FieldBool`; a string field has no number to key.

`Properties(reg)` returns every keyable property, the object fields first, and `Lookup(reg, name)` resolves one. A property's `Kind` says how a keyed number is written: a float as it is, an int rounded to the nearest whole number, a bool as true from 0.5 up. Every value is clamped to the field's range.

`Check(clip, reg)` checks a whole clip: every property must resolve, and every key must hold a value its property can take, 0 or 1 for a bool, a whole number for an int, and inside the field's range. A track's `target` is the ID of another object in the scene, and an empty target is the object the clip plays on.

## Playing a clip

```go
lib := resource.NewLibrary(os.DirFS("."))
clip, err := resource.LoadAs[*resource.Animation](lib, "anims/hop.animation.yaml")
if err != nil {
	return err
}
player, err := animclip.NewPlayer(clip, components, animclip.BindScene(built, "hero"))
if err != nil {
	return err // an unknown property, a bad key, or a target the scene does not have
}
player.OnEvent = func(name string) {
	if name == "land" {
		mixer.Play(landSound)
	}
}
player.Play()

// Inside game.Loop.Update:
player.Update(dt)
```

`NewPlayer` turns the clip into a `tween.Timeline`, one tween per pair of neighboring keys eased by the first key's `interp` (`linear`, `smooth` or `step`), so playback runs on the same deterministic clock as every other [timeline](tween.md). A track holds its first key's value before it and its last key's value after it, as `Animation.Sample` does. The player writes nothing until the first `Seek` or `Update`.

- `Play`, `Pause` and `Restart` start, stop and replay it; `Done` reports a clip that does not loop at its end.
- `Seek(at)` writes every track's value at `at` without firing events. A looping clip wraps a time before `0` or past `Length` into `[0, Length)` and shows its end at exactly `Length`, so a playhead dragged to the end shows the last keys; one that does not loop clamps `at`.
- `OnEvent` is called with the name of each clip event playback crosses, in time order.
- `SetLoop` overrides the clip's `loop` for one player.
- Key times and values are widened from the file's float32 by their shortest decimal (`KeyValue`, `Seconds`), so a key written `0.1` sits at exactly 100ms and plays the value 0.1.

The player reads the clip once; to play a clip edited or reloaded by `Library.Reload`, make a new player.

## Binding

A `Binder` connects each track to what it drives. It receives the track's target and resolved `Property` and returns the function that writes the value, which arrives already rounded and clamped for the property; an error fails `NewPlayer`, so a clip that names a missing object is refused before it plays.

- `BindDoc(doc, self)` writes into the objects of a `sceneformat.SceneDoc`.
- `BindScene(built, self)` writes into the objects of a `sceneformat.BuiltScene` and syncs each object it writes (`BuiltObject.Sync`), so the next `Draw` shows the object and its children at their new world transform and alpha, multiplied by the material's opacity as `Build` does. A track's target may be a nested child object's ID.

Component fields are written into the object's `components` mapping as `float64`, `int` or `bool`, creating the component's mapping when the object has none, so code that reads them with `ComponentRegistry.Resolve` sees the animated value. A game that keeps its own state binds it with a `Binder` of its own:

```go
bind := func(target string, p animclip.Property) (func(float64), error) {
	e := world.Find(target)
	if e == nil {
		return nil, fmt.Errorf("no entity %q", target)
	}
	switch p.Name {
	case "x":
		return func(v float64) { e.Pos.X = float32(v) }, nil
	case "body.speed":
		return func(v float64) { e.Speed = v }, nil
	}
	return nil, fmt.Errorf("entity cannot play %s", p.Name)
}
```
