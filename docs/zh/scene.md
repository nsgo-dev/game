# scene

`game/scene` 按顺序组合世界空间与屏幕空间图层，在视图坐标中对精灵和自定义绘制项做命中测试，并管理场景生命周期、叠加栈和过渡动画。

```go
import "github.com/nsgo-dev/game/pkg/scene"
```

## Scene、图层与 Drawable

`Scene` 持有一个共享的 `*tilemap.Camera` 和一组按 `Order` 稳定升序绘制的 `*Layer`。每个图层可以运行在 `SpaceWorld`（受 `Scene.Camera` 变换）或 `SpaceScreen`（直接以视图坐标绘制，适合 HUD）：

```go
sc := scene.NewScene(cam)

terrain := sc.AddLayer(scene.NewLayer("terrain", scene.SpaceWorld, 0))
terrain.AddTilemap(levelMap)

actors := sc.AddLayer(scene.NewLayer("actors", scene.SpaceWorld, 10))
actors.AddBatch(actorBatch)

hud := sc.AddLayer(scene.NewLayer("hud", scene.SpaceScreen, 100))
hud.AddDrawable(scene.NewDrawable(func(d *core.Drawing) {
	drawMinimap(d)
}))
```

- **图层项**：通过 `AddTilemap`、`AddTileLayer`、`AddBatch`、`AddDrawable` 或显式 `AddItem(scene.Item{...})` 添加。同一图层内的各项按 `Z` 稳定升序绘制。
- **视差与平铺重复**：`SpaceWorld` 图层支持 `ParallaxX` / `ParallaxY` 视差滚动系数（`0` 固定不随相机平移，`0.5` 以半速滚动，`1.0` 与相机同步，大于 `1.0` 用于滚动更快的前景层；可通过 `scene.NewParallaxLayer` 或 `Layer.SetParallax` 配置）、`OffsetX` / `OffsetY`，以及通过 `RepeatX` / `RepeatY` 配合 `RepeatWidth` / `RepeatHeight` 实现无缝平铺重复。YAML 场景格式（`game/sceneformat` 版本 3）提供 `parallaxX`、`parallaxY`、`repeatX`、`repeatY`、`repeatWidth` 和 `repeatHeight`，与 `scene.Layer` 具有一致的相机语义（见[编辑器与场景格式](editor.md)）。
- **Y 轴排序（`DepthSort = scene.SortY`）**：将精灵、自定义 `Drawable` 以及开启了 `YSort: true` 的 `tilemap.TileLayer` 中的可见瓦片合并到同一份稳定绘制列表中按 Y 升序绘制（支持 `SortKey` / `SpriteSortKey` 自定义键以及逐瓦片 `sort_origin_y` 偏移）。位于 Y 排序图层之前的普通瓦片层绘制在精灵后方，位于其后的普通瓦片层（如屋顶）绘制在精灵前方；也可通过 `AddTileLayer(m, layer)` 将单个瓦片层绑定到指定场景图层。
- **`Drawable`**：封装自定义 `Draw`、`Update`、`Hit`、`OnPress` 和 `OnTouch` 回调，自带 `Z` 与 `Visible` 字段。零值的 `Visible` 为 `false`，建议用 `scene.NewDrawable(fn)` 创建。
- **`Scene.Update(dt)`** 按图层顺序推进 `Camera`、瓦片地图和自定义 `Drawable`；**`Scene.Draw(d)`** 绘制所有可见图层，并在完成后恢复 `d` 的变换与画笔状态。

## 命中测试与输入派发

`Scene.HitTest(p)` 按绘制的逆序（最高 `Layer.Order` 优先，再按最高项/精灵 `Z` 优先）测试视图坐标点 `p`：`SpaceWorld` 图层会先经由 `Camera.ViewToWorld` 转换，`SpaceScreen` 图层则直接在视图坐标下判定：

```go
if hit, ok := sc.HitTest(ev.Position); ok {
	if hit.Sprite != nil {
		selectUnit(hit.Sprite, hit.Point)
	}
}
```

需要获取穿透所有层的命中列表时，使用 `HitTestAll(p)`（按从顶层到底层排序）。

### 自定义命中区域与回调

默认情况下，精灵使用其变换后的未裁剪画框做命中测试（`SpriteContains` / `SpriteToLocal`）。可用 `SetSpriteHit` 覆盖判定形状或绑定按压/触摸回调（用 `ClearSpriteHit` 移除）：

```go
sc.SetSpriteHit(&coinSprite, scene.HitOptions{
	Hit: scene.SpriteLocalHit(&coinSprite, scene.CircleHit(core.Point{X: 8, Y: 8}, 7)),
	OnPress: func(hit scene.HitResult, ev core.PressEvent) {
		collectCoin()
	},
})
```

内置的 `HitFunc` 辅助函数包括 `RectHit(bounds)`、`CircleHit(center, radius)`、`PolygonHit(vertices...)` 和 `SpriteLocalHit(sp, local)`。将 `HitOptions{Ignore: true}` 设为 `true` 可让装饰性精灵忽略点击和触摸。

调用 `sc.DispatchPress(ev)` 和 `sc.DispatchTouch(ev)` 可直接把视图事件派发给最顶层的命中目标（若目标只设置了 `OnPress` 而未设置 `OnTouch`，`DispatchTouch` 会在 `TouchStart` 时回退调用 `OnPress`）。

## Manager、场景栈与过渡动画

`Manager` 在父级 `*core.Scope` 下管理 `*Controller` 场景栈。每个进入的场景都会在 `Enter(cx)` 中获得独立的子 `*core.Scope`，在该子作用域下创建的循环、信号、计时器和音频会在场景退出时自动释放：

```go
mgr := scene.NewManager(cx)

play := &scene.Controller{
	Scene: gameScene,
	Enter: func(scx *core.Scope) {
		// 归属本场景子作用域的资源
		startLevelMusic(scx)
	},
	Pause:  func() { pauseSimulation() },
	Resume: func() { resumeSimulation() },
	Exit:   func() { cleanupLevel() },
	Update: func(dt time.Duration) {
		gameScene.Update(dt)
	},
}

pauseMenu := &scene.Controller{
	Scene:       pauseScene,
	Transparent: true, // 在下方继续绘制被暂停的场景
}

mgr.Switch(play, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
// 之后压入暂停界面而不退出主场景：
mgr.Push(pauseMenu)
// 弹出并恢复主场景：
mgr.Pop()
```

### 过渡动画

向 `mgr.Switch(next, tr)` 传入 `*Transition`：

| 构造函数 | 视觉效果 |
| --- | --- |
| `Fade(d, color)` | 前半段将旧场景淡出到 `color`，后半段从 `color` 淡入新场景（`color` 为透明或 0 时直接交叉淡化）。 |
| `Crossfade(d)` | 在 `d` 时长内直接从旧场景交叉淡化到新场景。 |
| `Wipe(d, dir)` | 沿 `DirectionLeft`、`DirectionRight`、`DirectionUp` 或 `DirectionDown` 方向擦除切换。 |
| `CustomTransition(d, draw)` | 按缓动后的 `progress`（`[0, 1]`）调用自定义绘制函数，并传入 `drawOld` 与 `drawNew`。 |

可将 `tr.Curve` 设为任意 `core.Curve` 缓动曲线。过渡进行期间（`mgr.Transitioning()` 为 `true`），新场景会立即进入，旧场景保留至过渡结束再退出并销毁其子作用域。

在 `game.Loop` 和 `core.Canvas` 中驱动管理器：

```go
// 在 Loop.Update 中：
mgr.Update(dt)

// 在 Canvas.Draw 中（自动订阅场景切换与过渡帧）：
mgr.Draw(d, size, alpha.Get())
```

## 3D 视图上的 2D 图层（`Overlay3D`）

`Overlay3D` 在透明的 `core.Canvas` 中绘制 2D 屏幕空间图层（`SpaceScreen`）、独立的 `*sprite.Batch` 与自定义绘制回调，并直接叠放在 3D `gpu.View` 之上（同时位于 `gpu.ViewProps.Children` 中原生 NSGo HUD 视图之下）：

```go
ov := scene.NewOverlay3D(cx)
ov.Scene = hudScene      // 仅绘制可见的 SpaceScreen 图层（自动跳过 SpaceWorld 图层）
ov.AddLayer(crosshair)   // 额外的 2D 图层，按 Layer.Order 稳定升序合并绘制
ov.AddBatch(markerBatch) // 在图层之上绘制的独立精灵批次
ov.Viewport = &screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}

// 直接叠放在 3D gpu.View 之上：
return ov.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady:     onReady,
	OnFrame:     onFrame,
	Children:    nativeHUDViews, // 渲染在 2D 叠加画布之上
})
```

- **绘制顺序**：`Scene` 中可见的 `SpaceScreen` 图层与 `Layers` 中可见图层合并后按 `Layer.Order` 稳定升序绘制，随后按切片顺序绘制 `Batches`，最后在 `Save`/`Restore` 保护下执行 `OnDraw`。如果自行管理画布，也可直接调用 `Scene.DrawScreenLayers(d)`。
- **透明叠层与虚拟视口**：叠加画布默认无背景填充，透明区域会完整透出下方的 3D 画面，半透明精灵则直接与 3D 画面进行 Alpha 混合。配置 `Viewport` 且 `BarColor` 为零值时，上下/左右黑边区域保持透明而不遮挡 3D 视图；如需绘制不透明黑边，请显式设置 `BarColor`（例如 `core.RGB(0x000000)`）。
- **指针穿透与命中测试**：叠加画布默认设置 `core.PointerEventsNone`，点击和触摸会无阻挡地穿透到下方的 3D `gpu.View`。需要画布直接接收指针事件时可设置 `Interactive: true`（或提供 `OnPress`），也可结合视图尺寸手动调用 `ov.HitTest`、`ov.HitTestAll`、`ov.DispatchPress` 和 `ov.DispatchTouch`。
- **3D 到 2D 投影**：使用 `scene.ProjectToOverlay(cam.ViewProj(), p.X, p.Y, p.Z, viewW, viewH)`（或使用 `ov.Project(cam.ViewProj(), p.X, p.Y, p.Z, windowSize)` 自动转换到 `ov.Viewport` 虚拟坐标）通过列主序视图投影矩阵（例如 `game/scene3d` 的 `cam.ViewProj()`）将 3D 世界坐标投影到 2D 叠加层，便于让血条、准星和标记精准跟随 3D 物体。动态 2D 叠加内容更新后调用 `ov.Invalidate()`（或 `ov.SetHidden(bool)`）即可触发重绘。
## 3D 视图上的原生 HUD（`HUD3D`）

`HUD3D` 将原生 NSGo 视图（`ui.Button`、`ui.Card`、`ui.Badge`、`ui.Progress`、`ui.Switch`、`core.Text` 及自定义 `core.View` 子树）叠放在 3D `gpu.View` 之上，自带安全区缩进、响应式显隐切换与鼠标/触摸穿透：

```go
hud := scene.NewHUD3D(cx)
hud.Padding = core.All(12)

// 装饰性读数：鼠标与触摸直接穿透到下方 3D 场景。
hud.AddPassThrough(scene.HUDTopLeft, ui.Badge{Text: "HP 100"})

// 锚定到安全区角落的交互控件：
hud.AddAt(scene.HUDTopRight, ui.Button{
	Text:    "Pause",
	OnPress: togglePause,
})
hud.AddAt(scene.HUDBottomRight, ui.Button{
	Text:    "Jump",
	OnPress: triggerJump,
})

// 直接叠放在 3D gpu.View 之上：
return hud.View(cx, gpu.ViewProps{
	DepthFormat: gpu.FormatDepth24Plus,
	OnReady:     onReady,
	OnFrame:     onFrame,
	OnPress:     pick3DObject,
})
```

- **鼠标与触摸穿透**：HUD 叠层容器使用 `core.PointerEventsBoxNone`，交互式 HUD 控件直接响应点击与触摸，空白区域则将鼠标与触摸事件无阻挡地穿透给下方的 3D 视图。使用 `AddPassThrough`（或 `HUDSlot{PassThrough: true}`）可为装饰性元素（如准星、分数面板）设置 `core.PointerEventsNone`。
- **锚点与自定义布局**：通过 `AddAt`、`AddPassThrough`、`AddSlot` 将控件定位到 9 个安全区锚点（`HUDTopLeft`、`HUDTopCenter`、`HUDTopRight`、`HUDCenterLeft`、`HUDCenter`、`HUDCenterRight`、`HUDBottomLeft`、`HUDBottomCenter`、`HUDBottomRight`）或 `HUDFull`，也可以通过 `Add` / `Children` 直接添加自定义穿透容器。
- **安全区缩进**：默认自动启用 `core.EnvSafeArea` 内边距（并与 `HUD3D.Padding` 叠加），让 HUD 避开刘海、状态栏与底部指示条，并在屏幕旋转时响应式更新；需要关闭时可设置 `IgnoreSafeArea: true` 或调用 `hud.SetSafeArea(false)`。
- **显隐切换**：调用 `hud.SetHidden(true)`、`hud.SetVisible(false)` 或 `hud.Toggle()` 即可隐藏 HUD（应用 `core.DisplayNone` 与 `core.PointerEventsNone`）而无需重新挂载子树；隐藏期间所有点击和触摸都会直接传递给 3D 视图。
- **与 3D 输入容器或 2D 叠层组合**：使用 `hud.Wrap(cx, view3D)` 可将 HUD 作为同级节点 Z 轴叠放在任意已构建的 3D 视图之上（例如外层 `core.View` 监听 `OnMouseDown`、`OnMouseMove` 和 `OnTouch` 实现 3D 镜头旋转的场景），也可以直接将 `hud.Layer(cx)` 放入 `gpu.ViewProps.Children` 并置于 2D 叠加画布之上。

## 在 2D 与 3D 场景之间切换

`Manager.View(opts)` 挂载响应式视图宿主，可在 2D 画布场景（`Controller.Scene` / `Controller.Draw`）与 3D 或原生视图场景（`Controller.View`，例如 `gpu.View` 或 `Overlay3D.View`）之间无缝切换，所有场景共享同一个 `*input.Actions` 与同一个 `*sound.Mixer`：

```go
mgr := scene.NewManager(cx)
actions.WatchKeys(cx) // 无论舞台视图是否持有焦点，按键都会送达 Actions
mgr.Actions = actions // 须在 mgr.View 之前设置：绑定在持久舞台视图上，跨 2D <-> 3D 切换保持生效
mixer := sound.NewMixer(cx, "game.sound") // 建在 Manager 的作用域而非场景的作用域上，切换时音乐不中断
mixer.Music("music").Play(theme, 0) // 在此处启动一次，不归任何场景所有

var overworld2D, dungeon3D *scene.Controller
overworld2D = &scene.Controller{
	Scene: mapScene,
	Update: func(dt time.Duration) {
		if mgr.Actions.JustPressed("enter_3d") {
			mgr.Switch(dungeon3D, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
		}
	},
}
dungeon3D = &scene.Controller{
	View: func(scx *core.Scope) core.Node {
		return ov.View(scx, gpu.ViewProps{
			Style:       core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady:     onReady,
			OnFrame:     onFrame,
		})
	},
	Update: func(dt time.Duration) {
		if mgr.Actions.JustPressed("exit_3d") {
			mgr.Switch(overworld2D, scene.Fade(400*time.Millisecond, core.RGB(0x000000)))
		}
	},
}

mgr.Switch(overworld2D, nil)
return mgr.View(scene.HostOptions{
	Alpha:    func() float64 { _ = frameTick.Get(); return alpha },
	Viewport: &viewport,
	Children: persistentHUD,
})
```

- **稳定的 GPU 视图挂载**：每个进入的场景都会获得唯一实例 ID 并通过 `core.Each` 渲染。已挂载的 3D `gpu.View` 表面与 GPU 管线在过渡动画帧之间以及压入/弹出 2D 叠加场景（透明或不透明）期间均保持存活不重建，并在场景退出时干净卸载。
- **视图作用域**：传给 `Controller.View` 的 `cx` 是挂载行的作用域，而不是 `Enter` 收到的作用域。场景离开时先运行 `Exit` 并销毁 `Enter` 的作用域，随后在同一次 `Switch`、`Pop` 或过渡步骤中卸载该行以及 `View` 的 `cx` 下创建的一切，两者之间不会运行任何帧或 `OnFrame` 回调。3D 视图每帧读取的状态应在 `View` 中或 Manager 的作用域上创建。
- **跨 2D ↔ 3D 的过渡动画**：`Fade(d, color)` 在前半段通过顶层画布将旧场景淡出到 `color`，在中点（`0.5`）切换显示新场景并在后半段淡入；`Crossfade(d)` 直接动画化旧/新场景包装层的 `Opacity`；`Wipe(d, dir)` 在纯 2D 之间使用精确路径裁剪，涉及 `Controller.View` 场景时回退为平滑交叉淡化；`CustomTransition`（或替换了 `Draw` 的内置过渡）在纯 2D 之间自行绘制每一帧，涉及 `Controller.View` 场景时于中点切换并在顶层绘制自定义过渡特效。`Draw` 为 nil 的 `Transition` 在整个 `Duration` 内直接显示新场景，与 `Manager.Draw` 一致。
- **共享输入**：`Manager.View` 在被调用时把 `Manager.Actions` 附加到持久舞台视图，因此要先设置 `Actions`。按键事件只在舞台视图持有焦点时送达：传入 `HostOptions.Ref` 并在挂载后调用其 `Focus()`（焦点与指针锁定随后在每次切换中保持），或像上例那样调用 `Actions.WatchKeys`。`Switch`、`Push`、`Pop` 默认调用 `Actions.Reset()` 清理瞬态边沿与按住状态而不丢失绑定、轴和指针锁定（设置 `Manager.KeepInputOnSwitch = true` 可在切换时保留按住状态）。
- **共享音频**：Manager 不需要自己的混音器。建在 Manager 作用域（或其祖先作用域）上的 `sound.Mixer` 不会随场景退出而销毁，因此背景音乐与音效组在每次 2D ↔ 3D 切换期间持续播放，各场景通过自身闭包访问该混音器。建在场景 `Enter` 作用域下的混音器会随该场景一起停止。
- **2D 叠加层与按需重绘**：在 3D 场景上 `Push` 一个 `Transparent: true` 的 2D 控制器会保持下方 3D 画面可见，同时暂停其 `Update` 并将被暂停层的指针事件设为 `PointerEventsNone`。配置 `HostOptions.Viewport` 时，透明压入的 2D 叠加层会将零值 `BarColor` 视为 `core.Transparent` 以免黑边遮挡 3D 视图。调用 `mgr.Invalidate()` 可按需重绘已挂载的 2D 画布，而不会重建 3D 视图。

如需查看完整的可运行示例（通过 `Manager.View` 在 2D 庭院大地图与 3D glTF 神龛之间淡入淡出切换、在 3D 视图上压入透明 2D 暂停叠层，并跨切换共享同一个 `*input.Actions` 与 `*sound.Mixer`），请参阅 [`examples/lantern3d`](https://github.com/nsgo-dev/game/tree/main/examples/lantern3d)。

## 场景文件中的物理

使用 [`game/sceneformat`](editor.md#内置物理组件817) 编写的 YAML 场景可以直接在对象上声明 2D（`body2d`、`collider2d`、`trigger2d`）和 3D（`body3d`、`collider3d`、`trigger3d`）物理组件，并复用 [`game.yaml`](project.md) 中的具名碰撞层。将构建好的场景传给 `sceneformat.BuildPhysics`，即可实例化对应的 `physics.World`、`physics.RigidWorld` 与 `physics3d.World` 并在每个固定时间步与场景一起推进。

## 场景文件中的 2D 光照

YAML 场景还可以设置 `ambientColor`，并在对象上附加内置的 `light2d` 与 `occluder2d`（矩形、多边形或瓦片地图碰撞）组件。`sceneformat.Build` 会自动构建 `BuiltScene.Lights2D`（`*lights2d.Scene`），使 `BuiltScene.Draw` 在游戏与编辑器的光照预览中渲染出完全一致的 2D 光照与阴影（[`game/editor`](editor.md#内置-2d-光照与遮挡体组件887)）。

## 场景文件中的 2D 导航区域

YAML 场景可以在对象上直接声明可行走或障碍物 `navregion2d` 多边形，并可选扣除瓦片地图的实心碰撞几何（`subtractTileCollision: true`）。`sceneformat.Build` 会通过 [`game/pathfind/navbake`](navbake.md) 将其烘焙为 `BuiltScene.NavBake2D`（`*navbake.Mesh`）与 `BuiltScene.NavMesh2D`（`*pathfind.NavMesh`），从而使编辑器的 2D 导航网格叠加层和 **Test Path** 工具得到与游戏完全一致的寻路结果（[`game/editor`](editor.md#内置-2d-导航区域组件888)）。

## 场景文件中的在屏通知器

对象的内置 `on_screen` 组件会以相机视野为参照，监视对象局部空间中的一个矩形，并在它进入或离开画面时发出通知，与 Godot 的 `VisibleOnScreenNotifier2D` 相同：

```yaml
objects:
  - id: torch
    x: 100
    y: 50
    particles: {file: fx/fire.particles.yaml, atlas: fx}
    components:
      on_screen:
        x: -16          # 被监视的局部空间矩形（默认为原点处的 16x16）
        y: -16
        width: 32
        height: 32
        margin: 32      # 迟滞带：离开视野超过该距离后才发出 exited
        pause: [particles]   # 离屏时 BuiltScene.Update 跳过的部分：particles、skeleton
connections:
  - {from: torch, event: on_screen.entered, to: hud, handler: show_torch}
  - {from: torch, event: on_screen.exited, to: hud, handler: hide_torch}
```

`sceneformat.Build` 会把通知器收集到 `BuiltScene.Notifiers`，`BuiltScene.Update` 以场景自身的相机视野（虚拟视口经过 `BuiltScene.Camera`）为参照逐个步进：矩形开始与某个视野重叠时发出 `entered`（或第一次更新时已在视野内），离开所有视野（含 `margin` 迟滞带）时发出 `exited`。旋转或缩放后的对象矩形用包含其角点的轴对齐矩形覆盖，旋转或缩放的相机可视范围也按同样方式计算，因此判定始终是廉价的矩形相交。事件经由场景的 `connections` 分发：把 `Connect` 返回的 `Signals` 赋给 `BuiltScene.ScreenSignals`，通知器状态翻转时就会运行连接的处理器。只要任一视野能看到对象就算在屏，因此分屏游戏把每个视口的相机传给 `BuiltScene.UpdateWithViews(dt, views...)`。`pause` 列出的部分（`particles` 表示对象的粒子发射器，`skeleton` 表示骨骼动画）在离屏期间不更新，回到画面时从停下的位置继续，对应 Godot 的 `VisibleOnScreenEnabler2D`。

不使用场景文件的游戏可以用 `scene.ScreenNotifier` 获得同样的行为：一个带有世界坐标 `Rect`、`Margin` 以及 `OnEnter`、`OnExit` 回调的 Go 值，每次更新调用一次 `Step(views ...*tilemap.Camera)`。

## 将场景组件生成到 ECS

`sceneformat.ComponentType` 可以声明可选的 `Spawn func(w *entities.World, e entities.Entity, fields map[string]any) error` 工厂函数，将 YAML 组件转换为 `game/entities` 中的强类型 ECS 组件状态（[实体](entities.md#从-yaml-场景生成实体)）。将组件注册表传入 `sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{Components: reg})`（或传入 `sceneformat.SpawnConnected`，它会自动转发注册表）即可启用。在生成第一个实体之前，所有组件都会先按注册表校验并填充默认值；随后实体按深度优先的文档顺序生成，同一对象上的组件按组件名升序执行 `Spawn`。若任一 `Spawn` 返回错误，已生成的实体会全部销毁回滚，并返回包含图层名、对象 ID 与组件名的错误。`SpawnEntities(doc, world)` 仍等价于 `SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{})`。
# 场景分区

`sceneformat.Partition` 将大型场景划分为确定性的网格单元。对象按边界分配，`Needed` 根据流式来源及加载、卸载半径计算排序后的单元列表。
