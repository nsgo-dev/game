# cues

`github.com/nsgo-dev/game/pkg/cues` 将玩法表现映射保存在有版本的 YAML 文件中。包本身负责确定性的解析和提示生命周期记录，游戏通过 `cues.Hooks` 将映射连接到粒子、音频、镜头和精灵系统。

```yaml
version: 1
cues:
  - tag: cue.hit.fire
    once: true
    particles:
      file: fx/hit.yaml
      attach: chest
    sound:
      file: sfx/hit.ogg
    shake:
      amplitude: 0.5
      duration: 100ms
  - tag: cue.status.burning
    whileActive: true
    animation: burning
```

每个提示都有点分隔的 `tag`，并设置 `once`、`whileActive` 之一或两者。单次提示由标签、目标、实例和模拟步共同确定，因此回滚重演再次分发同一事件时不会重复播放。持续提示会保持激活，直到收到 `EventRemoved`。

`NewPlayer` 会校验集合并建立客户端分发器。`Dispatch` 启动处理器并记录活动提示。`Reconcile` 接收恢复后的玩法状态中的持续提示，按确定性顺序停止缺失的处理器并启动新增的处理器。`Reset` 停止所有活动提示并清除重放去重状态。该包不依赖声音、渲染或场景，服务端可以加载数据而无需建立播放器。

`Hooks` 回调会收到解析后的处理器和原始事件。活动提示被移除或在协调时消失后，`Stop` 只调用一次。资源加载、对象池和目标挂接由游戏层负责。
