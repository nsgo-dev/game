# dialogue

`game/dialogue` 运行用 [Yarn Spinner](https://www.yarnspinner.dev/) 脚本编写的分支对话。`ParseYarn` 把由带标题节点组成的脚本读成 `Script`，`Runner` 每次推进到一句台词或一个选项菜单，保存变量和节点访问次数，并把每个 `<<命令>>` 交给游戏。[vn](vn.md) 在 `Runner` 之上把脚本呈现为视觉小说。

```go
import "github.com/nsgo-dev/game/pkg/dialogue"
```

`dialogue` 不读取挂钟时间、不启动 goroutine、也没有包级状态：`Runner` 只在游戏调用它时推进，所以同一段对话每次播放结果一致，在 `nsgotest` 下也一样。

## 脚本

脚本是纯文本，通常是与游戏其他资源放在一起的 `.yarn` 文件。每个节点在 `---` 之上有 `title:` 头（以及其他任意 `key: value` 头），正文到 `===` 结束：

```yarn
title: Shop
---
<<declare $gold = 15>>
Merchant: You have {$gold} gold. #line:shop_01
-> Buy a potion (10 gold) <<if $gold >= 10>>
    <<set $gold -= 10>>
    <<give_item potion 1>>
    Merchant: Here you go.
-> Leave
    <<jump Farewell>>
===
title: Farewell
---
Merchant: Come back soon.
===
```

`ParseYarn` 支持：

- 以 `//` 开头的注释。
- 台词：`Speaker: Text`，或不带说话人的旁白，支持 `{expr}` 插值、可选的 `#line:<id>` 以及其他 `#标签`。
- 选项：`-> 选项文字`，每个选项可带 `<<if cond>>` 条件、`#line:<id>` 和标签，以及缩进的正文。
- 条件：`<<if expr>>`、`<<elseif expr>>`、`<<else>>` 与 `<<endif>>`。
- 变量：`<<set $var = expr>>`（也可写 `to`、`+=`、`-=`、`*=`、`/=`）与 `<<declare $var = expr>>`，后者只在变量未设置时赋值。
- `<<jump Node>>` 与 `<<stop>>`。
- 游戏命令：其他任何名字的 `<<command arg "带引号的参数" $var {$expr}>>`。

表达式支持数字、字符串、`true` 与 `false`、`$变量`、算术运算符、比较（`==` 或 `is`、`!=`、`<`、`<=`、`>`、`>=`）、`and`/`&&`、`or`/`||`、`not`/`!`，以及 `visited("Node")` 与 `visited_count("Node")`。

它的错误是 `*loaderr.Error`，给出出错的行和修正方法；脚本来自文件时，用 `loaderr.WithFile` 标上文件名（见[错误](errors.md)）。解析出的 `Script` 不会再改变，因此可以由多个 runner 共享；`Nodes`、`HasNode` 与 `NodeHeaders` 列出其中的内容。

## 运行脚本

```go
script, err := dialogue.ParseYarn(src)
if err != nil {
	return err
}
r := dialogue.NewRunner(script, dialogue.Options{
	OnCommand: func(e dialogue.Event) { runCommand(e.Command, e.Args) },
})

step, err := r.Start("Shop")
for err == nil && step.Kind != dialogue.StepEnded {
	switch step.Kind {
	case dialogue.StepLine:
		show(step.Line.Speaker, step.Line.Text)
		step, err = r.Continue()
	case dialogue.StepChoices:
		step, err = r.Choose(pick(step.Choices))
	}
}
```

- `Start`、`Continue` 与 `Choose` 都把脚本运行到下一句台词（`StepLine`）、选项菜单（`StepChoices`）或结束（`StepEnded`），并以 `Step` 返回。
- `<<if>>` 条件不成立的 `Choice` 的 `Available` 为 false：可以把它显示为灰色或隐藏。对它调用 `Choose` 返回 `ErrChoiceUnavailable`；菜单等待选择时调用 `Continue` 返回 `ErrChoiceRequired`。
- 对脚本中不存在的节点调用 `Start`，或 `<<jump>>` 到这样的节点，返回 `ErrUnknownNode`。`Options.MaxStepsPerContinue`（默认 `DefaultMaxStepsPerContinue`，即 1024）限制一次调用执行的语句数，因此 `<<jump>>` 死循环会返回 `ErrStepLimitExceeded` 而不会卡住。
- `SetVar`、`Var` 与 `Vars` 从 Go 读写变量，开头的 `$` 可写可不写；与 Yarn 一样，Go 数字会变成 `float64`。`Visited` 给出节点被进入的次数，`CurrentNode` 给出当前运行的节点。

## 命令

每个不属于 Yarn 语法的 `<<命令>>` 都会变成一个 `Event`，带有命令名、参数（变量与 `{expr}` 已求值）以及所在节点。runner 把它交给 `Options.OnCommand`，并发布到 `Options.Bus`（一个 [flow](flow.md) `Bus`）上，其他系统可以用 `flow.Subscribe` 订阅：

```go
var bus flow.Bus
flow.Subscribe(&bus, func(e dialogue.Event) {
	if e.Command == "give_item" {
		inventory.Add(e.Args[0], 1)
	}
})
r := dialogue.NewRunner(script, dialogue.Options{Bus: &bus})
```

## 本地化

台词或选项的 `#line:<id>` 标签是它在字符串表中的键。设置 `Options.StringResolver` 后，runner 会以该 id、脚本自身的文字（作为回退）和变量副本调用它，再对返回文字中剩下的 `{expr}` 做插值，因此译文可以移动占位符的位置。解析函数通常在 [l10n](l10n.md) 字符串表中查找该 id，表中没有条目时返回回退文字。

## 存档

`State` 以 `State` 返回 runner 的变量与节点访问次数，可以编码为 JSON 存入 [save](save.md) 存档槽；`LoadState` 把它恢复到由同一脚本创建的 runner 中。读档后从要继续的节点调用 `Start`。
