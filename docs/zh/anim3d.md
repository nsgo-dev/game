# anim3d

`game/anim3d` 提供运行在 `game.Loop` 固定步长上的 3D 骨骼动画系统：带平滑交叉淡化的姿态状态机、支持脚步相位同步的 1D/2D 混合空间、骨骼遮罩分层与叠加片段、动画帧事件、双骨骼/FABRIK 反向动力学（IK），以及根骨骼运动（root motion）提取。

```go
import "github.com/nsgo-dev/game/packages/game/anim3d"
```

所有骨骼变换均遵循 [math3d](math3d.md) 的右手 Y 轴向上约定。

## 片段、状态机与混合空间

创建 `StateMachine`，每个状态可绑定单个 `*Clip`、`*BlendSpace1D`（如按速度混合待机 -> 行走 -> 奔跑）、`*BlendSpace2D`（多方向平移）或任意自定义 `PoseSampler`：

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

// 在 game.Loop.Update 中：
sm.SetParam("speed", moveSpeed)
pose := sm.Update(dt)
_ = pose
```

`BlendSpace1D` 与 `BlendSpace2D` 会按加权周期速度推进共享归一化相位（`[0, 1)`），并结合每个样本的 `FootPhaseOffset` 对齐步态，在混合不同长度的动画片段时消除滑步。

## 骨骼遮罩层与叠加片段

使用 `LayerStack` 和 `BoneMask` 将基础移动动画与上半身瞄准或叠加后坐力/呼吸动画合成：

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

需要手动混合姿态时，可直接调用 `BlendPoses`、`BlendPosesWeighted`、`BlendPosesMasked`、`MakeAdditivePose` 与 `ApplyAdditivePose`。

## 动画帧事件

通过 `clip.AddEvent` 在片段上标记定时 `ClipEvent`（`Time`、`Name`、`Payload`、`Data`），或从 glTF `extras`（`LoadEventsFromGLTFExtras`）及 JSON 伴随文件（`LoadEventsSidecar` / `MarshalEventsSidecar`）加载：

```go
walkClip.AddEvent(anim3d.ClipEvent{Time: 0.25, Name: "footstep", Payload: "left"})
sm.SetEventWeightThreshold(0.5) // 过滤淡出中低权重状态的事件
sm.OnEvent(func(ev anim3d.FiredEvent) {
	if ev.Event.Name == "footstep" {
		playFootstepSFX(ev.Event.Payload)
	}
})
```

也可每帧调用 `sm.DrainEvents()` 拉取事件，或用 `EventDispatcher` 驱动独立片段。

## 反向动力学（IK）与根骨骼运动

- **双骨骼 IK**（手臂/腿部）：`newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(hip, knee, ankle, footTarget, kneePole)` 精确解析铰链弯曲并朝向极向量 `pole`。
- **FABRIK**（脊柱、尾巴、触手等任意关节链）：`solved := anim3d.SolveFABRIK(jointPositions, target, 10, 0.001)`。
- **根骨骼运动（Root Motion）**：从动画片段提取每帧位移与偏航旋转增量，从采样姿态中剥离根位移以避免双重移动，再应用到角色世界变换：

```go
opts := anim3d.RootMotionOptions{RootJoint: 0, ExtractYawOnly: true, StripRootXZ: true, StripRootRot: true}
rm := anim3d.ExtractRootMotion(attackClip, prevTime, currTime, opts)
pose = anim3d.StripRootMotion(pose, opts)
characterTransform = anim3d.ApplyRootMotion(characterTransform, rm)
```
