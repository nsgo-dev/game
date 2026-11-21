# anim3d

`game/anim3d` provides 3D skeletal animation driven on `game.Loop`'s fixed steps: pose state machines with cross-fade transitions, 1D/2D blend spaces with foot-phase synchronization, bone-masked override and additive layers, timed clip events, two-bone and FABRIK inverse kinematics, and root motion extraction.

```go
import "github.com/nsgo-dev/game/packages/game/anim3d"
```

All joint transforms follow [math3d](math3d.md) conventions (right-handed Y-up).

## Clips, state machines, and blend spaces

Build a `StateMachine` whose states sample single `*Clip`s, `*BlendSpace1D` (for example idle -> walk -> run by speed), `*BlendSpace2D` (directional strafing), or any custom `PoseSampler`:

```go
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

You can also pull fired events once per step with `sm.DrainEvents()` or drive standalone clips through `EventDispatcher`.

## Inverse kinematics and root motion

- **Two-bone IK** (legs/arms): `newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(hip, knee, ankle, footTarget, kneePole)` solves an exact analytic hinge bend oriented toward `pole`.
- **FABRIK** (arbitrary chains such as spines, tails, or tentacles): `solved := anim3d.SolveFABRIK(jointPositions, target, 10, 0.001)`.
- **Root motion**: extract per-step translation and yaw/rotation deltas from an authored clip, strip root movement from the sampled pose so the mesh does not double-move, and apply it to the character's world transform:

```go
opts := anim3d.RootMotionOptions{RootJoint: 0, ExtractYawOnly: true, StripRootXZ: true, StripRootRot: true}
rm := anim3d.ExtractRootMotion(attackClip, prevTime, currTime, opts)
pose = anim3d.StripRootMotion(pose, opts)
characterTransform = anim3d.ApplyRootMotion(characterTransform, rm)
```
