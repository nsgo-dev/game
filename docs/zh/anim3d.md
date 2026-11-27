# anim3d

`game/anim3d` 提供运行在 `game.Loop` 固定步长上的 3D 骨骼动画系统：glTF 2.0 节点与蒙皮动画播放（`Player`、`ClipFromGLTF`）、带平滑交叉淡化的姿态状态机、支持脚步相位同步的 1D/2D 混合空间、骨骼遮罩分层与叠加片段、动画帧事件、双骨骼/FABRIK 反向动力学（IK），以及根骨骼运动（root motion）提取。

```go
import "github.com/nsgo-dev/game/pkg/anim3d"
```

所有骨骼变换均遵循 [math3d](math3d.md) 的右手 Y 轴向上约定。

## 播放 glTF 动画

从已加载的 `*gltf.Document` 创建 `Player`，在 `game.Loop.Update` 固定步长中推进平移、旋转、缩放与变形目标（morph target）权重轨道，且不会修改共享的文档对象；多个 `Player` 实例可各自以不同片段或时间独立播放同一份 `*gltf.Document`：

```go
player, err := anim3d.NewPlayer(doc)
if err != nil {
	return err
}
if err := player.PlayByName("Walk"); err != nil {
	return err
}

// 在 game.Loop.Update 中：
player.Step(dt)
player.ApplyToScene(scene)            // 可选：按名称同步到 scene3d.Scene 节点
joints, err := player.SkinMatrices(0) // 计算蒙皮渲染用的关节矩阵
_ = joints
```

`Player` 直接按 glTF 轨道求值 `STEP`、`LINEAR`（四元数最短路径球面线性插值）与 `CUBICSPLINE`（带归一化旋转的 Hermite 样条）三种插值模式，并保留未被动画驱动的矩阵节点（例如 COLLADA 导出的 `Z_UP` 根节点）。支持 `Play`、`PlayByName`、`Crossfade`、`CrossfadeByName`、`Pause`、`Stop`、`Seek`、`SetLoop`、`SetSpeed`（支持负值倒放；当 `Loop` 为 `false` 且速度为负时，`Play` 与 `Crossfade` 从片段的 `Duration` 起倒放至 `0`，播放中在被钳制的端点处翻转速度正负号时会跳到另一端）、`Time`、`Duration`、`Playing`、`LocalTransforms`、`WorldTransforms`、`MorphWeights`、`SkinMatrices`、`SkinMatricesForNode`、`ApplyToScene` 与 `ApplyToNodes`，并通过 `Update` 和 `Pose` 实现 `PoseSampler`。

`SkinMatrices(skinIndex)` 按 glTF 2.0 规范以蒙皮网格节点的世界变换逆矩阵归一化关节矩阵（`inverse(worldMatrix[meshNode]) * worldMatrix[joint] * inverseBindMatrix[i]`），因此即使骨架位于带旋转的根节点之下，绑定姿态下的关节矩阵也严格为单位矩阵。

如需将 glTF 动画接入 `StateMachine`、`BlendSpace1D`、`BlendSpace2D`、`LayerStack` 或根骨骼运动提取，可使用 `ClipFromGLTF` 或 `ClipsFromGLTF` 转换为 `*Clip`。被动画驱动的节点上未设轨道的 TRS 分量会保留其绑定姿态值以维持骨骼偏移，未被驱动的节点不加入 `Tracks` 以配合骨骼遮罩分层，glTF `extras`（`events` 或 `nsgo_events`）中的定时事件也会自动挂载——非对象 `extras` 与无关字段会被忽略，而格式错误的事件条目会返回标明动画索引的 `*loaderr.Error`。

## 片段、状态机与混合空间

创建 `StateMachine`，每个状态可绑定单个 `*Clip`、`*BlendSpace1D`（如按速度混合待机 -> 行走 -> 奔跑）、`*BlendSpace2D`（多方向平移）或任意自定义 `PoseSampler`：

```go
// 移动类片段需要循环，所以设置 Loop；混合空间中 Loop 为 false 的片段只播放一遍并停在最后一帧。
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

// 在 game.Loop.Update 中：
sm.SetParam("speed", moveSpeed)
pose := sm.Update(dt)
_ = pose
```

`BlendSpace1D` 与 `BlendSpace2D` 会按加权周期速度推进共享归一化相位（`[0, 1)`），并结合每个样本的 `FootPhaseOffset` 对齐步态，在混合不同长度的动画片段时消除滑步。

片段 `Loop: true` 的样本随共享相位循环；`Loop: false` 的片段自混合空间上次 `Reset` 起只播放一遍，随后停在最后一帧，其事件也只触发一次。参数相同（或二维坐标相同）的多个样本平分该位置的权重，不会有样本被遮蔽。

`StateMachine.Update(0)` 视为暂停：返回当前姿态，既不推进时间也不触发条件过渡。`Speed` 为负的状态倒放，并按倒放顺序触发经过的事件：进入非循环片段状态（`Loop: false`）且 `Speed` 为负时，会从 `Duration` 开始倒放至 `0`（循环片段仍从 `0` 开始向后回绕）；在被钳制的端点处以相反正负号的 `Speed` 替换当前非循环片段状态时，会跳到另一端继续播放。交叉淡化的两个状态共用同一个混合空间或采样器时，每步只推进一次。

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

层播放 `Clip` 时，只有片段有轨道的关节参与混合：覆盖层不改动其余关节，保持下层的姿态；叠加层对其余关节不叠加任何增量。同样，`BlendPosesMasked` 会保留覆盖姿态中缺少的基础关节，`MakeAdditivePose` 对这些关节给出单位增量。

需要手动混合姿态时，可直接调用 `BlendPoses`、`BlendPosesWeighted`、`BlendPosesMasked`、`MakeAdditivePose` 与 `ApplyAdditivePose`。

## 动画帧事件

通过 `clip.AddEvent` 在片段上标记定时 `ClipEvent`（`Time`、`Name`、`Payload`、`Data`），或从 glTF `extras`（`LoadEventsFromGLTFExtras`）及带版本号的 JSON 伴随文件（`LoadEventsSidecar` / `MarshalEventsSidecar`）加载：

```go
walkClip.AddEvent(anim3d.ClipEvent{Time: 0.25, Name: "footstep", Payload: "left"})
sm.SetEventWeightThreshold(0.5) // 过滤淡出中低权重状态的事件
sm.OnEvent(func(ev anim3d.FiredEvent) {
	if ev.Event.Name == "footstep" {
		playFootstepSFX(ev.Event.Payload)
	}
})
```

`LoadEventsSidecar` 要求带版本号的 `{"version": 1, "clips": {...}}` 外层结构（`EventsSidecarVersion`），并对缺失或不支持的版本、未知或拼写错误的字段、重复键以及尾随数据返回带行号与字段路径的 `*loaderr.Error`。`LoadEventsFromGLTFExtras` 与 `ClipFromGLTF` 会忽略缺失、`null` 或非对象的 `extras` 以及其中的无关字段；当 `events` 或 `nsgo_events` 存在但格式错误时，会返回标明 `animations[i].extras...` 的 `*loaderr.Error`。

交叉淡化期间，每个状态以本步落在淡化区间内那一段中点处的淡化权重触发事件，因此完成淡化的那一步仍会触发淡出状态的事件。

也可每帧调用 `sm.DrainEvents()` 拉取事件，或用 `EventDispatcher` 驱动独立片段。

## 反向动力学（IK）与根骨骼运动

- **双骨骼 IK**（手臂/腿部）：`newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(hip, knee, ankle, footTarget, kneePole)` 精确解析铰链弯曲并朝向极向量 `pole`。若其中一根骨骼长度为零，则仅用另一根骨骼对准目标。
- **FABRIK**（脊柱、尾巴、触手等任意关节链）：`solved := anim3d.SolveFABRIK(jointPositions, target, 10, 0.001)`。
- **根骨骼运动（Root Motion）**：从动画片段提取每帧位移与偏航旋转增量，从采样姿态中剥离根位移以避免双重移动，再应用到角色世界变换：

```go
opts := anim3d.RootMotionOptions{RootJoint: 0, ExtractYawOnly: true, StripRootXZ: true, StripRootRot: true}
rm := anim3d.ExtractRootMotion(attackClip, prevTime, currTime, opts)
pose = anim3d.StripRootMotion(pose, opts)
characterTransform = anim3d.ApplyRootMotion(characterTransform, rm)
```

跨过循环片段末尾的一步，无论以回绕后的时间（`currTime < prevTime`）还是单调递增的时间传入，都会在每个循环边界处切开，各段旋转按播放顺序组合，因此一大步让角色转过的角度与边界两侧的几个小步依次应用完全一致，同时俯仰和偏航的片段也是如此。`DeltaPos` 以片段根骨骼的父空间度量，各段直接相加。
