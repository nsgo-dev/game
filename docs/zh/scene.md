# scene

`game/scene` 按顺序组合世界空间与屏幕空间图层，在视图坐标中对精灵和自定义绘制项做命中测试，并管理场景生命周期、叠加栈和过渡动画。

```go
import "github.com/nsgo-dev/game/packages/game/scene"
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

- **图层项**：通过 `AddTilemap`、`AddBatch`、`AddDrawable` 或显式 `AddItem(scene.Item{...})` 添加。同一图层内的各项按 `Z` 稳定升序绘制。
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
