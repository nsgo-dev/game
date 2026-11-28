# tags

`github.com/nsgo-dev/game/pkg/tags` 提供项目级点分层级玩法标签词汇。注册表是有版本且可确定性编码的 YAML 文件：

```yaml
version: 1
tags:
  - name: status
    description: broad state
  - name: status.debuff.stun
  - name: weapon.fire
```

父级标签会自动推导。声明 `status.debuff.stun` 后，`status` 和 `status.debuff` 都可用于匹配，但只有显式声明的名称会写入文件。名称只能使用 ASCII 字母、数字、`_`、`-` 和点号，且必须唯一。

`Registry.Resolve` 将名称内部化为 `Tag`。`Container` 保存带引用计数的授予，因此两个系统可以授予同一标签并分别撤销。`HasExact` 只检查直接授予的标签，`Has` 还会匹配子孙标签，所以授予 `status.debuff.stun` 会满足 `status` 查询。

`Query` 计算 `All`、`Any` 和 `None` 条件，并可组合嵌套的 `AllOf`、`AnyOf` 和 `NoneOf`。`Registry.QueryNames` 解析数据加载器使用的名称形式，并拒绝未声明名称。匹配直接读取容器状态，不产生分配。
