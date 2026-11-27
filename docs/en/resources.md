# resource

`game/resource` reads and writes shared resource files: data that several scenes and pieces of code use, kept in a YAML file of its own and referenced by path instead of being repeated, as Godot's `.tres` files are. Materials, curves, gradients, animation clips, UI themes, sound bus layouts and physics materials are resource files.

```go
import "github.com/nsgo-dev/game/pkg/resource"
```

## The common header

Every resource file is one YAML document that starts with the same fields:

| Field | Description |
| --- | --- |
| `type` | The resource's type: `material`, `curve`, `gradient`, `animation`, `theme`, `busLayout` or `physicsMaterial`. |
| `version` | The schema version. Must be `1`. |
| `uid` | Optional stable ID that stays the same when the file is renamed or moved, 13 characters of `0`-`9` and `a`-`z` without `i`, `l`, `o` and `u`. `nsgo game index` writes it; see [Stable references](project.md#stable-references). |

The fields of that type follow. `Parse` reads the header first, refuses an unknown type or a version other than `1` with a message that says so, then decodes the whole document into the matching Go type (`*resource.Material`, `*resource.Curve`, …) and refuses unknown fields, trailing documents and values out of range. `Encode` writes the header first and the other fields in a fixed order, leaving out fields at their defaults, so a file it wrote reads back and encodes to the same bytes and a diff shows only what changed.

A file's name is free; `<name>.<type>.yaml`, such as `stone.material.yaml`, keeps a folder readable.

## Types

### material

How a surface draws. 2D sprites use `opacity` and `blend`; 3D renderers also read the PBR fields.

```yaml
type: material
version: 1
name: Stone
baseColor: '#8a8f99'
texture: textures/stone.png
opacity: 0.75
blend: multiply
roughness: 0.9
```

| Field | Default | Description |
| --- | --- | --- |
| `name` | | Optional display name. |
| `baseColor` | white | A CSS color. |
| `texture` | | A slash-separated image path from the project root. |
| `opacity` | `1` | Multiplies the alpha of what draws with the material, from `0` to `1`. |
| `blend` | `sourceOver` | A canvas composite operation in camel case, such as `multiply`, `screen` or `lighter` (`resource.BlendModes`). |
| `metallic` | `0` | PBR metalness, from `0` to `1`. |
| `roughness` | `1` | PBR roughness, from `0` to `1`. |
| `emissive` | | A CSS color the surface gives off. |
| `doubleSided`, `unlit` | `false` | Draw back faces; skip lighting. |

### curve

A value keyed over time, such as an easing or a particle's size over its life. Keys are in strictly increasing `time` order, and `interp` is how a key's value moves to the next: `linear` (the default), `step` or `smooth`.

```yaml
type: curve
version: 1
name: Jump height
keys:
  - time: 0
    value: 0
    interp: smooth
  - time: 0.5
    value: 1
  - time: 1
    value: 0
```

`Curve.Eval(t)` returns the value at `t`, holding the end values beyond the first and last keys; its method value fits `tween.EaseFunc`.

### gradient

Colors keyed over offsets from `0` to `1`, in non-decreasing order; two stops at one offset make a hard edge.

```yaml
type: gradient
version: 1
stops:
  - offset: 0
    color: '#ffcc66'
  - offset: 1
    color: '#331a4d'
```

`Gradient.At(t)` mixes the colors around `t`, and `Gradient.CoreStops()` returns `core.GradientStop` values for a canvas gradient.

### animation

A clip of property tracks keyed from `0` to `length` seconds. `target` names the object a track drives, the ID of an object in the scene when [animclip](animclip.md) plays it (empty is the object the clip plays on), and each target and property pair has one track. A track sets either numeric `keys` (`time`, `value`, optional `interp`) or discrete sprite `frames` (`time`, `frame`), never both; `frames` steps through atlas frame names without interpolation, used for `sprite.frame` flipbook tracks. `events` are named cues in time order, each at a `time` from `0` to `length`, which a player reports as playback crosses them.

```yaml
type: animation
version: 1
name: Torch flicker
length: 1.5
loop: true
tracks:
  - target: flame
    property: alpha
    keys:
      - time: 0
        value: 1
      - time: 0.75
        value: 0.6
        interp: smooth
      - time: 1.5
        value: 1
  - property: sprite.frame
    frames:
      - time: 0
        frame: torch_0
      - time: 0.75
        frame: torch_1
events:
  - time: 0.75
    name: flare
```

`Animation.Sample(t, set)` wraps or clamps `t` to the clip (`ClipTime`) and calls `set` with each numeric track's target, property and value; `Animation.SampleFrames(t, set)` does the same for discrete frame tracks (`Track.IsFrameTrack`, `Track.EvalFrame`). [animclip](animclip.md) plays a clip on scene objects, with its properties checked against the registered component types, and the [editor's animation panel](editor.md#animation-editor) keys and saves clips.

### theme

A `gameui` theme. Every field is optional, and `Theme.Apply(base)` replaces only the fields the file sets, so a file can restyle a few colors of `gameui.DefaultTheme()`.

```yaml
type: theme
version: 1
colors:
  panelBackground: '#10141c'
  accent: '#f97316'
padding: 10
font:
  family: Inter
  size: 18
  weight: semibold
```

`colors` has one key per `gameui.Theme` color (`panelBackground`, `panelBorder`, `background`, `hover`, `active`, `focusedBorder`, `disabled`, `border`, `text`, `disabledText`, `mutedText`, `accent`, `accentHover`, `accentActive`, `track`). The sizes are `padding`, `cornerRadius`, `borderWidth` and `focusWidth`, and `font.weight` is `light`, `normal`, `medium`, `semibold` or `bold`.

### busLayout

The sound buses a `sound.Mixer` plays through and the effect chain on each. Each effect entry sets exactly one of `lowPass`, `highPass` (`cutoffHz`, `q`), `delay` (`seconds`, `feedback`, `wet`), `reverb` (`roomSize`, `damping`, `wet`), `gain` (`factor`) or `bitcrush` (`bits`, `downsample`).

```yaml
type: busLayout
version: 1
buses:
  - name: music
    effects:
      - lowPass:
          cutoffHz: 8000
  - name: effects
    effects:
      - reverb:
          roomSize: 0.5
          wet: 0.2
```

`BusLayout.Apply(mixer)` sets each bus's chain with `SetGroupEffects`; a bus with no effects clears its chain. Volumes stay the player's own settings.

### physicsMaterial

Surface properties of 2D and 3D rigid bodies: `friction`, `restitution` (from `0` to `1`), `linearDamping` and `angularDamping`. `PhysicsMaterial.ApplyTo(body)` sets the fields the file holds on a `physics.RigidBody` and `PhysicsMaterial.ApplyTo3D(body)` sets them on a `physics3d.Body`, leaving unset fields unchanged. `Library.PhysicsMaterial(path)` loads and shares one `*PhysicsMaterial` per path and implements `sceneformat.PhysicsMaterialResolver` for scene physics components.

```yaml
type: physicsMaterial
version: 1
friction: 0.02
restitution: 0.1
```

## Sharing resources with a Library

A `Library` loads resource files from an `fs.FS` by slash-separated path, such as `materials/stone.material.yaml`, and keeps one value per path: every `Load` of a path returns the same pointer, so every scene and piece of code using the file holds one value.

```go
lib := resource.NewLibrary(os.DirFS("project"))

stone, err := lib.Material("materials/stone.material.yaml")
jump, err := resource.LoadAs[*resource.Curve](lib, "curves/jump.curve.yaml")
```

`Library.Reload(path, data)` parses new contents and copies them into the loaded value in place, so everything holding it sees the edit. A file that no longer parses, or that changed its type, leaves the last good value and returns an error. `Library.Revision()` counts successful reloads, for code that derives values from resources, such as a `gameui.Theme` built with `Theme.Apply`. Call `Reload` on the goroutine that reads resources, usually the engine's.

## Materials in scenes

A `sceneformat` object references a material file by path with `material`:

```yaml
objects:
  - id: wall
    sprite:
      atlas: cave_tiles
      frame: "1"
    material: materials/stone.material.yaml
```

`sceneformat.Build` resolves it through a resolver that implements `sceneformat.MaterialResolver`; a `Library` does, so a resolver can embed one beside the atlases:

```go
type resolver struct {
	sceneformat.AtlasMap
	*resource.Library
}

cave, err := sceneformat.Build(caveDoc, resolver{atlases, lib})
keep, err := sceneformat.Build(keepDoc, resolver{atlases, lib})
```

The object's sprite draws with its own alpha times the material's opacity, and with the material's blend mode. `BuiltScene.Draw` copies the material's current state onto the sprites first (`SyncMaterials`), so when the file is reloaded both scenes draw the edit.

## Reloading on edit

During development, `hotreload.Watcher.WatchResources` watches every `.yaml` and `.yml` file under its directory and passes changed files the library has loaded to `Library.Reload` on the engine goroutine. The watcher's directory must be the library's root, so their paths match:

```go
lib := resource.NewLibrary(os.DirFS(root))
w := hotreload.NewWatcher(cx, root)
w.WatchResources(lib, func(path string, err error) {
	if err != nil {
		log.Printf("reload %s: %v", path, err)
	}
})
```

In release builds (`-tags release`) `WatchResources` does nothing.
