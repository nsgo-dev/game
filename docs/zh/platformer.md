# platformer

`github.com/nsgo-dev/game/pkg/platformer` 把 [`physics.CharacterBody`](physics.md) 变成平台跳跃角色，提供土狼时间、跳跃缓冲、可变跳跃高度、贴墙下滑和蹬墙跳。每个辅助功能的时长都是 YAML 配置里的可调参数。

```go
import "github.com/nsgo-dev/game/pkg/platformer"
```

坐标与 `physics` 一致：X 向右增大，Y 向下增大。速度单位是点每秒，一律写正数，方向由控制器决定。

## Controller

```go
body := physics.NewCharacterBody(physics.NewBoxBody(32, 200, 12, 16))
ctl, err := platformer.NewController(body, platformer.DefaultConfig())
if err != nil { ... }

loop := &game.Loop{
	Update: func(dt time.Duration) {
		r := ctl.Step(platformer.Input{
			Move:        float32(actions.Axis("move")),
			JumpPressed: actions.JustPressed("jump"),
			JumpHeld:    actions.Pressed("jump"),
		}, grid, slopes, platforms, dt)
		if r.Jump != platformer.JumpNone {
			playJumpSound()
		}
		showWallSlide(r.WallSliding)
	},
}
```

`Step` 每个固定步长调用一次。它读取身体上一次 `Move` 留下的接触状态（`Grounded`、`OnLeftWall`、`OnRightWall`），根据 `Input` 和配置设置速度，再用 `CharacterBody.Move` 移动身体，所以斜坡、单向瓦片和移动平台的行为与直接使用身体时相同。如果需要同时与世界中的静态/运动学物体（例如门、方块与 `Body.OneWay` 单向物体）以及静态 `RigidBody` 碰撞体碰撞，可以调用 `StepWorld(in, world, slopes, platforms, dt)`（或设置 `body.World = world`）。`StepResult` 报告 `MoveResult`、本步开始的跳跃（`JumpGround`、`JumpWall` 或 `JumpNone`）、是否截短了跳跃（`JumpCut`）、是否在贴墙下滑（`WallSliding`）、是否忽略了水平输入（`InputLocked`），以及身体可以从哪面墙起跳（`WallSide`，-1 为左，1 为右，0 为无）。

对于瓦片地图，把 [`tileshape.View`](tileshape.md) 作为 `grid`，把它的 `Slopes()`（或身体附近的 `AppendSlopesIn`）作为 `slopes` 传入：角色就能走过在 Tiled 碰撞编辑器中绘制的斜坡、2:1 与半高瓦片，无需手工放置线段。

`Reset` 清空所有计时，例如重生之后；`SetConfig` 在游戏运行中替换可调参数。

## 计时

所有时长都按步长时间计量。事件在发生的那一步打开一个窗口，之后的步只要距离那一步不足窗口时长，窗口就保持打开。在固定 `dt` 下，时长 `D` 覆盖 `ceil(D/dt)` 步（含事件所在的那一步），时长为 0 时只覆盖那一步。例如 100 Hz 下 `coyote_time: 50ms`：在最后一次着地之后第 1 到第 4 步按下跳跃仍然算地面起跳，第 5 步按下则不算。`game.Loop` 传入的 `dt` 是截断到整纳秒的 `time.Second/Rate`，所以每一步留有 1ns 的余量：在默认的 60 Hz 下，`coyote_time: 100ms` 覆盖 6 步而不是 7 步。

| 辅助功能 | 窗口何时开始 | 配置项 |
| --- | --- | --- |
| 土狼时间 | 身体在地面上 | `coyote_time` |
| 跳跃缓冲 | 按下跳跃 | `jump_buffer` |
| 贴墙土狼时间 | 身体在空中碰到墙 | `wall_coyote_time` |
| 蹬墙跳锁定 | 蹬墙跳开始 | `wall_jump_lock` |

- **土狼时间与跳跃缓冲**：某一步里如果还有缓冲着的按键，且身体在地面上或仍在土狼时间内，就会起跳。所以落地前不久按下会在落地时起跳，跑出平台边缘后不久按下也仍能起跳。起跳会关闭这两个窗口，一次按键只跳一次。已经在空中上升的身体（例如被弹簧弹起）不能使用土狼时间，所以按下跳跃不会把这次上抛换成更弱的地面起跳。
- **可变跳跃高度**：控制器发起的跳跃在上升途中，某一步跳跃键是松开的，且向上速度大于 `jump_cut_min_speed`，就把向上速度乘以 `jump_cut`，只乘一次。在同一步内按下又松开的轻点得到最低的跳跃。游戏自己设置的上抛（例如弹簧）不会被截短：身体的 `VY` 与上一次 `Step` 留下的不同时，控制器发起的跳跃就结束了。
- **贴墙下滑**：贴着墙下落并朝墙推动时，下落速度上限为 `wall_slide_speed`。接触状态来自上一步的 `Move`，只有身体朝墙移动时才会报告碰到墙。
- **蹬墙跳**：在空中碰到墙时，或离开墙不足 `wall_coyote_time` 时，缓冲着的按键会以背离墙的 `wall_jump_speed_x` 和向上的 `wall_jump_speed_y` 起跳。两种跳跃都可行时地面起跳优先。之后的 `wall_jump_lock` 内忽略水平输入，所以按住朝墙的方向不会抵消蹬离的速度。

把 `wall_slide_speed` 或 `wall_jump_speed_y` 设为 0 会关闭对应功能，`jump_cut: 1` 关闭可变跳跃高度，`run_speed: 0` 让游戏自己控制水平速度。

## 配置文件

`Config` 是带版本号的 YAML 文档。`ParseConfig` 拒绝未知字段、缺失或不支持的 `version`、负数或非有限值、大于 1 的 `jump_cut` 以及多余的文档；时长写成 Go 的时长字符串。`EncodeConfig` 写出规范形式：所有字段按固定顺序，时长写成 Go 的时长字符串，数字写成最短形式。编码解析得到的配置会得到这种形式，规范形式再解析、编码一次逐字节相同；手写的文件如果带注释、键的顺序不同或省略了字段，会被改写成规范形式。`DefaultConfig()` 如下：

```yaml
version: 1
coyote_time: 100ms
jump_buffer: 100ms
jump_speed: 284
jump_cut: 0.55
jump_cut_min_speed: 110
gravity: 720
max_fall_speed: 320
run_speed: 98
ground_accel: 960
ground_decel: 1150
air_accel: 680
wall_slide_speed: 60
wall_coyote_time: 80ms
wall_jump_speed_x: 150
wall_jump_speed_y: 260
wall_jump_lock: 150ms
```

`ground_accel` 和 `air_accel` 让水平速度趋向 `move * run_speed`，`ground_decel` 在地面上没有输入时减速；设为 0 会立即达到目标速度。`max_fall_speed: 0` 表示下落速度不设上限。
# 场景行为配置

使用版本化的场景行为词汇在对象旁描述控制器设置。运行时组件可以读取编辑器使用的同一组轴、速度、范围和目标参数。
