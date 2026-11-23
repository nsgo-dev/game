# statedump

`game/statedump` 以无窗口方式让游戏运行固定步数，并把实体、组件和场景状态写成确定性的 YAML。种子和输入相同的两次运行写出完全相同的文件，所以智能体或测试可以把一次运行与另一次运行做 diff 来检查自己的结果，而一处改动只显示为几行变化。

在同一种 CPU 架构上，文件逐字节相同。游戏自身的浮点运算在 arm64 和 amd64 上可能不同，因为 Go 编译器在 arm64 上可能把一次乘法和一次加法合并成一条 FMA 指令；要在多台机器间共用的基准转储，需要对这类乘积做显式转换，例如 `x += float64(v * speed * dt)`，转换会先把乘积舍入，从而阻止合并。

```go
import "github.com/nsgo-dev/game/packages/game/statedump"
```

## 无窗口运行

`Run` 用一个 [`replay.Recording`](replay.md) 驱动模拟，并在 `rec.TotalSteps` 步之后捕获其状态。与 `replay.Run` 一样，`makeSim` 收到录像的种子和一个作为 `ActionSource` 的 `*replay.Player`；不同的是 `Run` 不需要 `testing.TB`，所以命令行程序或智能体的工具也能调用它。如果 `Step` 没有在每一步调用一次 `src.Step()`，`Run` 返回错误，因为从不读取录像的模拟对任何输入都会转储出同样的状态。

```go
rec, err := replay.LoadFile("runs/boss-bug.replay.yaml")
if err != nil {
	return err
}
dump, err := statedump.Run(rec, func(seed uint64, src replay.ActionSource) (statedump.Sim, error) {
	g := newGame(seed)
	return statedump.Sim{
		Step: func(dt time.Duration) { g.Update(src, dt) }, // calls src.Step() once, as game.Loop's Update does
		Source: statedump.Source{
			World:     g.World,
			Scenes:    g.Scenes,
			SceneName: func(c *scene.Controller) string { return g.SceneNames[c] },
			Values: func() map[string]any {
				return map[string]any{"score": g.Score, "level": g.Level}
			},
		},
	}, nil
})
if err != nil {
	return err
}
return statedump.WriteFile("runs/boss-bug.state.yaml", dump)
```

要在没有输入的情况下运行 N 步，传入一个没有事件的录像：`&replay.Recording{Version: replay.FormatVersion, Seed: seed, Rate: 60, TotalSteps: n}`。

`Capture(src)` 可在任意时刻捕获同样的状态，种子、步进速率和步数留给调用方填写。

## 转储包含的内容

- `version`、`seed`、`rate` 和 `steps`：结构版本（1）以及这次运行的参数。
- `scene`：`scene.Manager` 场景栈的 `depth`、设置了 `SceneName` 时当前场景的名字 `current`，以及 `Switch` 过渡进行中时的 `transitioning` 和经过缓动的 `progress`；会越界的曲线（例如 ease-out-back）在过渡的一段时间里会让 `progress` 超过 1。没有场景管理器时省略。
- `values`：实体世界之外的全局状态，例如分数、关卡或随机数状态，每个值用 `encoding/json` 编码并按名字存放，名字按字节序升序排列。
- `entities`：所有存活实体，按槽位索引升序排列，带有 `generation`、层级父节点 `parent`，以及按编解码器名字升序排列的组件。与世界快照一样，只包含用 [`entities.RegisterCodec`](entities.md) 注册过的组件。

```yaml
version: 1
seed: 42
rate: 60
steps: 90
scene:
  depth: 2
  current: pause
values:
  score: 40
entities:
  - index: 0
    generation: 1
    components:
      - type: health
        data:
          hp: 100
      - type: pos
        data:
          x: 12.5
          y: 12
  - index: 1
    generation: 1
    parent: 0
    components:
      - type: name
        data: sword
```

组件和值的数据写成 YAML 映射和序列，而不是内嵌的 JSON 字符串，所以改动一个字段就只改动一行。它们保留 Go 结构体的 JSON 字段顺序，以及 `encoding/json` 为每个数字写出的原样文本。会被 YAML 1.2 或 YAML 1.1 读取器读成其他类型的字符串（`"1"`、`"true"`、`"yes"`、`"2001-12-14"`、`"1:20"`）会加上引号，JSON 的 `null` 仍写作 `null`。

## 读取与比较

`Encode` 写出转储，`Parse` 读取转储，`WriteFile` 和 `ReadFile` 在磁盘上做同样的事。对 `Encode` 写出的任意 `b`，`Encode(Parse(b))` 都返回 `b`。`Parse` 拒绝未知字段、其他版本、多余的 YAML 文档、未按槽位排序的实体、未按名字排序的组件、转储中不存在的父实体，以及 JSON 无法表示的数据：非字符串键、锚点和别名、二进制、NaN 和无穷大。

`Diff(a, b)` 返回带三行上下文的统一格式行 diff，两份转储相同时返回 `""`：

```diff
--- a
+++ b
@@ -28,7 +28,7 @@
           second line
       - type: pos
         data:
-          x: 4.0000009393637015e-7
+          x: 119.99999559999982
           y: 12
   - index: 1
     generation: 1
```

测试可以把黄金转储放在 `testdata/` 下，当运行结果不再一致时用这个 diff 报告失败：

```go
got, err := statedump.Encode(dump)
if err != nil {
	t.Fatal(err)
}
want, err := os.ReadFile("testdata/boss-bug.state.yaml")
if err != nil {
	t.Fatal(err)
}
if diff := statedump.Diff(want, got); diff != "" {
	t.Fatalf("state differs from the golden dump:\n%s", diff)
}
```

`dump.Snapshot()` 以 `entities.WorldSnapshot` 返回其中的实体，所以注册了相同编解码器的世界可以用 `World.Restore` 把转储加载回来。
