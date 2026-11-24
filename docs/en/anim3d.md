# anim3d

`game/anim3d` provides 3D skeletal animation driven on `game.Loop`'s fixed steps: glTF 2.0 node and skin playback (`Player`, `ClipFromGLTF`), pose state machines with cross-fade transitions, 1D/2D blend spaces with foot-phase synchronization, bone-masked override and additive layers, timed clip events, two-bone and FABRIK inverse kinematics, and root motion extraction.

```go
import "github.com/nsgo-dev/game/packages/game/anim3d"
```

All joint transforms follow [math3d](math3d.md) conventions (right-handed Y-up).

## Playing glTF animations

Create a `Player` from any loaded `*gltf.Document` to advance translation, rotation, scale, and morph-target weight channels on `game.Loop.Update` fixed steps without mutating the shared document. Multiple players can animate the same `*gltf.Document` independently at different clips or timestamps:

```go
player, err := anim3d.NewPlayer(doc)
if err != nil {
	return err
}
if err := player.PlayByName("Walk"); err != nil {
	return err
}

// In game.Loop.Update:
player.Step(dt)
player.ApplyToScene(scene)            // optional: sync named nodes in scene3d.Scene
joints, err := player.SkinMatrices(0) // joint matrices for skinned rendering
_ = joints
```

`Player` evaluates `STEP`, `LINEAR` (with shortest-path quaternion Slerp), and `CUBICSPLINE` (Hermite spline with normalized rotations) interpolation modes directly from the glTF channels, preserving matrix-authored non-animated nodes (such as COLLADA `Z_UP` roots). It provides `Play`, `PlayByName`, `Crossfade`, `CrossfadeByName`, `Pause`, `Stop`, `Seek`, `SetLoop`, `SetSpeed` (including negative values for reverse playback), `Time`, `Duration`, `Playing`, `LocalTransforms`, `WorldTransforms`, `MorphWeights`, `SkinMatrices`, `SkinMatricesForNode`, `ApplyToScene`, and `ApplyToNodes`. `Player` also implements `PoseSampler` via `Update` and `Pose`.

`SkinMatrices(skinIndex)` computes glTF 2.0 joint matrices normalized by the skinned mesh node's inverse world transform (`inverse(worldMatrix[meshNode]) * worldMatrix[joint] * inverseBindMatrix[i]`), so bind-pose joints evaluate to identity matrices even under rotated root nodes.

To feed glTF clips into `StateMachine`, `BlendSpace1D`, `BlendSpace2D`, `LayerStack`, or root motion extraction, convert them with `ClipFromGLTF` or `ClipsFromGLTF`. Unanimated TRS components on targeted nodes are seeded from each node's rest transform so bone offsets stay intact, untargeted nodes are omitted from `Tracks` so bone-masked layers leave them untouched, and any timed events in glTF `extras` are attached automatically.

## Clips, state machines, and blend spaces

Build a `StateMachine` whose states sample single `*Clip`s, `*BlendSpace1D` (for example idle -> walk -> run by speed), `*BlendSpace2D` (directional strafing), or any custom `PoseSampler`:

```go
// Locomotion clips cycle, so mark them looping; a blend-space sample whose clip has Loop false plays once and holds its last frame.
idleClip.Loop, walkClip.Loop, runClip.Loop = true, true, true

loco := anim3d.NewBlendSpace1D(
	anim3d.BlendSample1D{Param: 0.0, Clip: idleClip},
	anim3d.BlendSample1D{Param: 2.0, Clip: walkClip, FootPhaseOffset: 0.0},
	anim3d.BlendSample1D{Param: 6.0, Clip: runClip, FootPhaseOffset: 0.0},
)
loco.ParamName = "speed"

sm := anim3d.NewStateMachine("locomotion")
sm.AddState(anim3d.State{Name: "locomotion", Blend1D: loco, ParamName: "speed"})
sm.AddState(anim3d.State{Name: "jump", Clip: jumpClip})
sm.AddTransition(anim3d.Transition{
	From:          "locomotion",
	To:            "jump",
	BlendDuration: 120 * time.Millisecond,
	When:          func() bool { return sm.Param("airborne") > 0.5 },
})

// In game.Loop.Update:
sm.SetParam("speed", moveSpeed)
pose := sm.Update(dt)
_ = pose
```

`BlendSpace1D` and `BlendSpace2D` advance a shared normalized phase (`[0, 1)`) using weighted cycle speeds and per-sample `FootPhaseOffset` so blending between clips of different lengths avoids foot skating.

A sample whose clip has `Loop: true` wraps with the shared phase; a clip with `Loop: false` plays once from the blend space's last `Reset` and holds its final frame, and fires each of its events once. Samples placed at the same parameter (or the same 2D point) split that position's weight equally, so none of them is shadowed.

`StateMachine.Update(0)` is a paused step: it returns the current pose without advancing time or starting conditional transitions. A state with a negative `Speed` plays backward and fires the events it crosses in reverse order. When the two states of a crossfade share one blend space or sampler, it advances once per step.

## Bone-masked layers and additive clips

Combine a base locomotion sampler with upper-body actions or additive recoil/breathing using `LayerStack` and `BoneMask`:

```go
upperMask := anim3d.NewBoneMask(jointCount, 0)
upperMask.SetBranch(jointParents, spineJoint, 1.0)

stack := anim3d.NewLayerStack(jointCount)
stack.AddLayer(anim3d.AnimLayer{Name: "base", Mode: anim3d.LayerOverride, Weight: 1, Sampler: sm})
stack.AddLayer(anim3d.AnimLayer{Name: "aim", Mode: anim3d.LayerOverride, Weight: 0.9, Mask: upperMask, Clip: aimClip, Loop: true})
stack.AddLayer(anim3d.AnimLayer{Name: "recoil", Mode: anim3d.LayerAdditive, Weight: 0.6, Mask: upperMask, Clip: recoilClip, ReferencePose: &idleRef})

finalPose := stack.Update(dt)
_ = finalPose
```

When a layer plays a `Clip`, only the joints the clip has a track for take part: an override layer leaves the other joints at the pose below it and an additive layer adds nothing to them. `BlendPosesMasked` likewise keeps base joints the overlay pose does not have, and `MakeAdditivePose` gives them the identity delta.

For manual composition, use `BlendPoses`, `BlendPosesWeighted`, `BlendPosesMasked`, `MakeAdditivePose`, and `ApplyAdditivePose`.

## Clip events

Attach timed `ClipEvent` markers (`Time`, `Name`, `Payload`, `Data`) to clips directly via `clip.AddEvent` or load them from glTF `extras` (`LoadEventsFromGLTFExtras`) or JSON sidecar files (`LoadEventsSidecar` / `MarshalEventsSidecar`):

```go
walkClip.AddEvent(anim3d.ClipEvent{Time: 0.25, Name: "footstep", Payload: "left"})
sm.SetEventWeightThreshold(0.5) // suppress events from low-weight cross-fading states
sm.OnEvent(func(ev anim3d.FiredEvent) {
	if ev.Event.Name == "footstep" {
		playFootstepSFX(ev.Event.Payload)
	}
})
```

During a crossfade each state fires its events at its crossfade weight at the midpoint of the part of the step inside the crossfade, so the step that completes the crossfade still fires the outgoing state's events.

You can also pull fired events once per step with `sm.DrainEvents()` or drive standalone clips through `EventDispatcher`.

## Inverse kinematics and root motion

- **Two-bone IK** (legs/arms): `newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(hip, knee, ankle, footTarget, kneePole)` solves an exact analytic hinge bend oriented toward `pole`. If one bone has zero length, the other bone alone is aimed at the target.
- **FABRIK** (arbitrary chains such as spines, tails, or tentacles): `solved := anim3d.SolveFABRIK(jointPositions, target, 10, 0.001)`.
- **Root motion**: extract per-step translation and yaw/rotation deltas from an authored clip, strip root movement from the sampled pose so the mesh does not double-move, and apply it to the character's world transform:

```go
opts := anim3d.RootMotionOptions{RootJoint: 0, ExtractYawOnly: true, StripRootXZ: true, StripRootRot: true}
rm := anim3d.ExtractRootMotion(attackClip, prevTime, currTime, opts)
pose = anim3d.StripRootMotion(pose, opts)
characterTransform = anim3d.ApplyRootMotion(characterTransform, rm)
```

A step that crosses the end of a looping clip, whether passed as wrapped times (`currTime < prevTime`) or monotonic ones, is split at each loop boundary and its rotations compose in playback order, so one long step turns the character exactly as the shorter steps on either side of the boundary would, including clips that pitch and yaw at once. `DeltaPos` is measured in the clip's root parent space and the pieces add.
