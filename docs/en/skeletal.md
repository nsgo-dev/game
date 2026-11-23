# skeletal

```go
import "github.com/nsgo-dev/game/packages/game/skeletal"
```

2D skeletal rigs, forward kinematics, skins, keyframe animation and crossfade blending, drawn into a `core.Drawing` through `sprite.Batch`.

## Format decision

`skeletal` defines an open, versioned YAML format (`.skel.yaml`, version `1`), read by `Load` and written by `Encode` with `go.yaml.in/yaml/v3`:

- **No proprietary runtime license**: Spine's official runtimes and export format are governed by the Spine Runtimes License, which requires every user of the runtime to hold a paid Spine license. An open schema keeps NSGo games unencumbered under MIT.
- **Text for review**: Like every file a game's author edits in this repository, a rig is plain YAML with one bone, slot, attachment or keyframe per line, so a person or an AI can edit it by hand and a diff shows exactly what moved.
- **No extra dependencies**: The YAML library is the one `sceneformat` and the data tables already use; there is no binary decoder or code generation.
- **Native screen coordinates**: Uses the same coordinate system as `sprite` and `core.Drawing` (`+X` right, `+Y` down, rotations in radians clockwise on the screen), so rig data and runtime transforms match without axis flips or unit conversion.
- **Simple to export**: The flat layout (bones, slots, skins, animations) makes writing a short exporter from DragonBones, Blender or custom tools — or authoring rigs by hand — straightforward.
- **Strict and versioned**: Every file carries `version: 1`; `Load` rejects missing or unsupported versions, unknown fields, duplicate keys, trailing documents and non-finite numbers (`.nan`, `.inf`).
- **JSON still loads**: JSON is YAML flow syntax, so a version-1 rig written as JSON, as earlier releases read it, loads unchanged; `Encode` writes it back as YAML.

## Coordinates and units

- **Position (`x`, `y`, `originX`, `originY`)**: points in NSGo's 2D drawing space (`+X` right, `+Y` down).
- **Rotation (`rotation`, `angle`)**: radians, clockwise on the screen.
- **Scale (`scaleX`, `scaleY`)**: multipliers about the bone or attachment origin, defaulting to `1` when omitted.
- **Time (`duration`, `time`)**: seconds as YAML numbers in `[0, MaxSeconds]` (one million seconds), converted to `time.Duration` to the nearest nanosecond on load.

## YAML schema (version 1)

| Field | Type | Description |
| --- | --- | --- |
| `version` | `int` (required) | Schema version. Must be `1`. |
| `name` | `string` | Optional rig name. |
| `bones` | `[]Bone` | Bone hierarchy in bind pose. Parents may appear in any order; cycles and unknown parents are rejected. |
| `slots` | `[]Slot` | Draw-ordered slots, back to front. Each binds to a bone. |
| `skins` | `map[string]map[string]Attachment` | Maps skin name (default `"default"`) to a map of slot name to attachment. |
| `animations` | `map[string]Animation` | Maps animation name to its bone and slot timelines. |

### Bone

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | `string` (required) | — | Unique bone name. |
| `parent` | `string` | `""` | Parent bone name, or empty for a root bone. |
| `x`, `y` | `float32` | `0` | Local bind-pose offset in the parent's space. |
| `rotation` | `float32` | `0` | Local bind-pose rotation in radians, clockwise. |
| `scaleX`, `scaleY` | `float32` | `1` | Local bind-pose scale. |
| `length` | `float32` | `0` | Optional bone length for editors and debug views. |

### Slot

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | `string` (required) | — | Unique slot name. |
| `bone` | `string` (required) | — | Name of the bone whose world transform the slot follows. |
| `attachment` | `string` | `""` | Optional default atlas frame name when no skin supplies one. |
| `alpha` | `float32` | `1` | Bind-pose slot opacity in `[0, 1]`. |

### Attachment (inside `skins[skin][slot]`)

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `frame` | `string` | `""` | Atlas frame name in `sprite.Atlas.Frames`; empty hides the slot. |
| `x`, `y` | `float32` | `0` | Local offset from the slot's bone. |
| `rotation` | `float32` | `0` | Local rotation in radians, clockwise. |
| `scaleX`, `scaleY` | `float32` | `1` | Local scale. |
| `originX`, `originY` | `float32` | `0` | Pivot point in the untrimmed atlas frame (matches `sprite.Sprite`). |

When the active skin does not define an entry for a slot, the skeleton falls back to the `"default"` skin. Setting `frame: ""` explicitly hides the slot in that skin.

### Animation

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `duration` | `float64` | last keyframe | Duration in seconds. Derived from the latest keyframe when omitted. |
| `loop` | `bool` | `false` | File-level default looping preference. |
| `bones` | `map[string]BoneTimeline` | — | Per-bone `translate`, `rotate`, and `scale` tracks. |
| `slots` | `map[string]SlotTimeline` | — | Per-slot `attachment` and `alpha` tracks. |

Bone timelines apply relative to the bone's bind pose: `translate` (`x`, `y`) and `rotate` (`angle`) add to the bind pose, while `scale` (`x`, `y`, each defaulting to `1`) multiplies it. Rotation tracks interpolate along the shortest arc across `±π`.

Slot `attachment` keyframes (`{time, frame}`) step immediately to `frame` at `time` (empty hides the slot). Slot `alpha` keyframes (`{time, value}`) interpolate opacity in `[0, 1]`.

Every continuous keyframe accepts an optional easing curve applied to the segment starting at that keyframe:

- `"linear"` (default): constant speed to the next keyframe.
- `"stepped"`: holds the keyframe's value until the next keyframe's time.
- `"bezier"`: cubic Bezier easing with `bezier: [cx1, cy1, cx2, cy2]`, where `cx1` and `cx2` lie in `[0, 1]` (`cy1` and `cy2` may overshoot). Control points on any other curve are rejected.

### Sample rig

A complete 7-bone character with two skins (`"default"` and `"armored"`) and three animations (`"walk"`, `"run"`, `"jump"`) lives in [runner.skel.yaml](../../packages/game/skeletal/testdata/runner.skel.yaml). A minimal excerpt:

```yaml
version: 1
name: runner
bones:
  - {name: root}
  - {name: torso, parent: root, y: -18, length: 16}
  - {name: head, parent: torso, y: -16, length: 12}
slots:
  - {name: torso, bone: torso}
  - {name: head, bone: head}
skins:
  armored:
    head: {frame: helmet, originX: 6, originY: 12}
    torso: {frame: torso_armor, originX: 6, originY: 16}
  default:
    head: {frame: head, originX: 6, originY: 12}
    torso: {frame: torso, originX: 6, originY: 16}
animations:
  walk:
    duration: 0.8
    loop: true
    bones:
      torso:
        translate:
          - {time: 0, x: 0, y: 0, curve: bezier, bezier: [0.25, 0.1, 0.25, 1]}
          - {time: 0.2, x: 0, y: -2}
          - {time: 0.4, x: 0, y: 0}
```

### Canonical form

`Encode` writes one canonical form, so saving a rig never produces a diff by itself:

- Keys follow the schema order above; skins, attachments, animations and timelines are sorted by name; bones and slots keep their order.
- Every bone, slot, attachment and keyframe is one flow mapping on its own line.
- Fields equal to their defaults are left out, except that a keyframe always writes its `time` and values and an animation always writes its `duration`.
- Numbers use the shortest text that reads back to the same value, and times read back to the same nanosecond.
- Comments, anchors and other formatting are not kept.

Loading a file already in this form and encoding it gives the same bytes back, and loading what `Encode` wrote gives the same rig. `Encode` refuses, with the same errors as `Load`, any rig `Load` would refuse.

## Runtime API

### Loading and setup

- `Load(data []byte) (*SkeletonData, error)` parses and validates the rig, rejecting cycles, missing parents, duplicate names, unknown targets and invalid curves. One `*SkeletonData` can be shared across any number of live skeletons.
- `Encode(sd *SkeletonData) ([]byte, error)` writes the rig as a canonical `.skel.yaml` document, for editors and exporters that save rigs.
- `NewSkeleton(data *SkeletonData, atlas *sprite.Atlas) *Skeleton` creates a live skeleton at scale `(1, 1)` and alpha `1` in its bind pose, activating `"default"` if defined.

### Root transform, skins and attachments

- Root fields `X`, `Y`, `Rotation`, `ScaleX`, `ScaleY`, `FlipX`, `FlipY`, `Alpha` and `Z` position, orient, mirror and fade the whole rig.
- `SetSkin(name string) error` switches the active skin (falling back to `"default"` for slots not overridden by `name`).
- `SetAttachment(slot, frame string)` overrides a slot's atlas frame (empty hides it); `ClearAttachment(slot string)` removes the override.
- `UpdateWorldTransforms()` runs forward kinematics down the bone tree; `WorldBone(name string) (BoneTransform, bool)` reads any bone's world position, rotation and scale for attaching weapons, hitboxes or particles.
- `Draw(d *core.Drawing)` updates world transforms and draws every visible slot in slot draw order through a reusable `sprite.Batch`.

### Animation and crossfade blending

- `Play(anim string, loop bool) error` starts `anim` at time `0`, cancels any crossfade and updates world transforms immediately.
- `Crossfade(anim string, duration time.Duration, loop bool) error` blends smoothly from the outgoing animation pose into `anim` over `duration` using shortest-angle rotation blending and linear translation, scale and alpha blending. Interrupting an in-progress crossfade snapshots the current blended pose so transitions never pop.
- `Update(dt time.Duration)` advances active playback and crossfades deterministically and refreshes world transforms.
- `Animation() string`, `Done() bool` and `Blending() bool` report playback state.

```go
data, err := skeletal.Load(runnerYAML) // the bytes of runner.skel.yaml
if err != nil {
	return err
}
hero := skeletal.NewSkeleton(data, atlas)
hero.X, hero.Y = 200, 300
_ = hero.Play("walk", true)

loop := &game.Loop{
	Update: func(dt time.Duration) {
		if sprinting && hero.Animation() != "run" {
			_ = hero.Crossfade("run", 200*time.Millisecond, true)
		}
		hero.Update(dt)
		if hand, ok := hero.WorldBone("arm_r"); ok {
			emitSpark(hand.X, hand.Y)
		}
	},
}
// In a core.Canvas's Draw:
hero.Draw(d)
```
