# rooms

`game/rooms` 像银河恶魔城类平台游戏那样，把 2D 世界划分为各自带相机边界的房间。玩家从一个房间走进下一个房间时，`Director` 把 `tilemap.Camera` 限制在新房间内，并播放该房间的过渡效果：切换（cut）、滑动（slide）或淡入淡出（fade）。

```go
import "github.com/nsgo-dev/game/packages/game/rooms"
```

## 房间地图

房间写在带版本号的 YAML 文件里。每个房间是一个以世界坐标点表示的矩形；地图级的 `transition` 在进入任意房间时播放，房间自己的 `transition` 会覆盖它。

```yaml
version: 1
transition:
  kind: slide
  duration: 300ms
rooms:
  - id: entrance
    name: room.entrance
    x: 0
    "y": 0
    width: 320
    height: 180
  - id: hall
    x: 320
    "y": 0
    width: 640
    height: 180
  - id: crypt
    x: 320
    "y": 180
    width: 320
    height: 360
    transition:
      kind: fade
      duration: 500ms
      easing: easeIn
      color: '#102030'
```

| 字段 | 含义 |
| --- | --- |
| `version` | 格式版本，`1`（`rooms.FormatVersion`） |
| `transition` | 进入未设置过渡的房间时播放；省略表示直接切换 |
| `rooms[].id` | 唯一且非空的 ID，供代码和存档使用 |
| `rooms[].name` | 显示名称或本地化键；本包不解释 |
| `rooms[].x`、`y`、`width`、`height` | 房间矩形（世界坐标点）；尺寸必须为正 |
| `rooms[].transition` | 进入此房间时播放 |
| `transition.kind` | `cut`、`slide` 或 `fade` |
| `transition.duration` | 带单位的 Go 时长，如 `400ms`；省略时滑动为 400 毫秒、淡入淡出为 600 毫秒。切换不接受时长 |
| `transition.easing` | `linear`、`ease`、`easeIn`、`easeOut` 或 `easeInOut`；省略时滑动为 `easeInOut`、淡入淡出为 `linear`。切换不接受缓动 |
| `transition.color` | 淡入淡出遮罩的 CSS 颜色；省略为黑色。只有淡入淡出接受颜色 |

`rooms.Parse` 和 `rooms.LoadFS` 会拒绝未知字段、其他版本、缺失或重复的 ID、空的或非有限的矩形、不带单位的时长数字、未知的过渡类型或缓动以及格式错误的颜色，错误都包装 `rooms.ErrInvalidMap`。`rooms.Encode` 会逐字节写回同一份地图：字段顺序固定、2 空格缩进、不含任何生成值。它会给 `y` 键加引号，因为 YAML 1.1 会把裸写的 `y` 读成 `true`；手写文件可以不加。负零会写成 `0`，因此载入后连续保存两次得到相同的字节。

点位于房间左边和上边之内、右边和下边之前时属于该房间，因此共用一条边的两个房间不会同时包含边上的点。`Map.RoomAt` 按列表顺序返回第一个包含该点的房间。

## 在编辑器、LDtk 或 Tiled 中布置房间

可以在[场景编辑器](editor.md)中直接叠加在场景之上可视化编辑 `*.rooms.yaml`：从文件浏览器或场景设置检查器在场景旁打开 `*.rooms.yaml`，使用吸附到瓦片网格的手柄创建、移动、缩放和删除房间，在检查器中编辑每个房间的 `id`、`name` 与 `transition`（支持完整撤销/重做），并通过 `rooms.Encode` 确定性保存。

房间也可以来自外部关卡编辑器，而不必手写。`rooms.FromLDtk` 为 Free 或 GridVania 布局的 LDtk 项目的每个关卡生成一个以关卡命名的房间，`rooms.FromTiledLayer` 为 Tiled 对象层中的每个矩形生成一个以对象命名的房间，并按该层的偏移（包括外层分组的偏移）平移。两者遇到无法作为房间的内容（如 Linear 布局、椭圆或未命名对象）都会报错，而不是跳过。

```go
data, _ := fs.ReadFile(assets, "world.ldtk")
list, err := rooms.FromLDtk(data, "") // "" 读取单世界项目
if err != nil {
	return err
}
m := &rooms.Map{
	Version:    rooms.FormatVersion,
	Transition: &rooms.Transition{Kind: rooms.KindSlide},
	Rooms:      list,
}
if err := rooms.Validate(m); err != nil {
	return err
}
```

## 在房间之间移动相机

`Director` 接管相机的位置和边界。在玩家出生点调用一次 `Start`，之后每个固定步长用玩家位置调用 `Update`，替代 `Camera.Follow`：

```go
dir := rooms.NewDirector(m, cam)
dir.Smoothing = 10 // 过渡之外的 Camera.Follow 平滑系数；0 表示锁定玩家
dir.Start(player.Pos)

// 在 game.Loop.Update 中：
if !dir.Transitioning() {
	player.Update(dt) // 相机移动时冻结玩家，大多数银河恶魔城都是这样
}
if dir.Update(player.Pos, dt) {
	hud.ShowRoomName(dir.Room().Name)
}
cam.Update(dt) // 屏幕震动照常运行

// 在 game.Loop.Draw 中：
d.Save()
cam.Apply(d)
drawWorld(d)
d.Restore()
dir.DrawFade(d) // 视图空间：在整个视图上绘制淡入淡出遮罩
```

每一步中，只要玩家仍在当前房间内，`Update` 就保持当前房间，所以站在两个房间重叠处不会来回切换；玩家不在任何房间内时也保持当前房间。玩家进入另一个房间时，`Update` 把 `Camera.Bounds` 设为该房间、返回 `true`，并开始该房间的过渡：

- **cut**：相机立即移到玩家处，并限制在新房间内。
- **slide**：相机沿缓动曲线从原位置平移到玩家在新房间内的限制位置；玩家继续移动时会跟随，时长结束时恰好落在限制位置上。
- **fade**：前半段相机保持不动，视图逐渐被遮罩颜色覆盖；中点时相机切到新房间；后半段遮罩逐渐消失。`FadeAlpha` 返回遮罩不透明度，`DrawFade` 把它绘制在 `Camera.Width` × `Camera.Height` 的视图上。

淡入淡出途中玩家再次切换房间时，遮罩不会跳变：新的淡入淡出从相同的不透明度继续覆盖，即使缓动不同也是如此；切到滑动或直切时，遮罩会在相机移动的同时按被打断的淡入淡出原本的方式逐渐消失。滑动被另一次房间切换打断时，会从相机当前位置继续平移。目标坐标为 NaN 或无穷大时，这一步会被忽略，错误的位置不会传给相机。`Room`、`Previous`、`Transition` 和 `Progress` 分别报告相机要去的房间、来自的房间、正在播放的过渡以及进度。一切都只按固定的 `dt` 推进，因此回放会逐位产生相同的相机路径。
