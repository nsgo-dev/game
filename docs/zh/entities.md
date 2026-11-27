# 实体（entities）

面向 NSGo 游戏的小型、零反射实体组件系统（ECS）：代际实体句柄、强类型稀疏集合组件、零分配查询、拓扑排序系统调度、预制体，以及与 `game/save` 集成的 JSON 世界快照。

```go
import "github.com/nsgo-dev/game/pkg/entities"
```

`go tool game generate component <name>`、`system <name>` 和 `prefab <name>` 向游戏写入新的组件、系统或预制体，连同其注册和测试；见[命令行](cli.md#generate)。

## 架构决策

在实现 `entities`（[#136](https://github.com/nsgo-dev/game/issues/136)）之前，我们对比了三种用于表达游戏对象的架构方案：

1. **小型、零反射、代际索引的稀疏集合 ECS**（采用）。
2. **面向对象场景树**：堆分配节点加虚方法更新。
3. **基于反射的原型（Archetype）ECS**：动态组件表与运行时查询规划器。

选择方案（1）基于以下五点原因：

- **可预测的零分配热路径**：实体是 64 位代际句柄（32 位槽位索引 + 32 位代数），通过空闲列表复用槽位；每种组件存放在连续的强类型稀疏集合（`Store[T]`）中。稳态下创建和销毁 10 万个实体、读写组件以及遍历查询均为 0 堆分配。
- **零反射**：通过 Go 泛型和零大小类型标签区分组件存储，所有组件访问、查询迭代和预制体构造都在编译期静态类型检查，不引入 `reflect`。
- **安全的句柄生命周期**：销毁实体时会递增对应槽位的代数再放回空闲列表，因此陈旧的 `Entity` 句柄永远不会误指复用了同一槽位的新实体。
- **显式且确定的执行顺序**：系统通过 `Before` 和 `After` 声明顺序约束，由 Kahn 拓扑排序结合注册顺序决胜规则一次性解析；循环依赖与未知目标会明确报错，`Order()` 便于调试叠层打印执行顺序。
- **清晰的序列化边界**：只有通过 `RegisterCodec[T]` 注册的组件才会写入确定性的 `WorldSnapshot` JSON 并交由 `game/save` 持久化，从而将精灵、音频声道等临时运行态排除在存档之外，同时完整保留实体 ID 与跨实体引用。

## 实体与 World

`Entity` 是一个 `uint64`，低 32 位为槽位索引，高 32 位为代数（`entities.Zero` 是无效句柄，永远不会存活）。`World` 拥有全部实体及其组件存储。

- `NewWorld()` 返回一个新的空世界（`World` 的零值也立即可用）。
- `Spawn()`、`Destroy(e)`、`Alive(e)` 和 `Count()` 管理实体生命周期并复用已释放槽位。
- `Clear()` 清除所有实体和组件，递增代数使旧句柄失效，同时保留切片容量以实现零分配复用；`Reset()` 释放组件和活跃实体的底层存储，同时保留递增后的槽位代数，因此 `Reset()` 之前的旧句柄即便在重新生成实体后仍保持失效。
- `Attach(cx)` 将世界绑定到 NSGo 的 `*core.Scope`，在作用域销毁时自动调用 `Dispose()`。

```go
world := entities.NewWorld()
world.Attach(cx)

hero := world.Spawn()
world.Alive(hero) // true
world.Destroy(hero)
world.Alive(hero) // false，即使槽位被复用后仍为 false
```

## 组件与查询

每种组件类型 `T` 保存在独立的强类型稀疏集合 `*Store[T]` 中。顶层泛型函数直接操作 `*World`，也可以通过 `Component[T](w)` 获取 `*Store[T]` 在紧密循环中直接访问。

- `Set[T](w, e, v)`、`Get[T](w, e)`、`Has[T](w, e)` 和 `Remove[T](w, e)` 以 O(1) 复杂度添加、读取、判断和移除组件。
- `Each1[A]`、`Each2[A, B]` 和 `Each3[A, B, C]` 从最小的稀疏集合出发，在其余集合中做 O(1) 成员检查，按确定顺序访问精确匹配的实体，零反射且零堆分配。回调返回 `false` 即可提前结束遍历。组件指针只在回调内有效，并且在回调为被遍历的组件类型新增组件后失效：新增可能让存储扩容并移动数据，因此请先收集实体，遍历结束后再添加组件。回调可以销毁传入的实体，但不能增删被遍历存储中的其他实体，需要时请先收集，遍历结束后再处理。

```go
type Position struct{ X, Y float64 }
type Velocity struct{ DX, DY float64 }

entities.Set(world, hero, Position{X: 10, Y: 20})
entities.Set(world, hero, Velocity{DX: 60, DY: 0})

entities.Each2[Position, Velocity](world, func(e entities.Entity, p *Position, v *Velocity) bool {
	p.X += v.DX * dt.Seconds()
	p.Y += v.DY * dt.Seconds()
	return true
})
```

## 系统与更新顺序

`Scheduler` 在每个 `game.Loop` 固定步长上按确定的拓扑顺序执行具名系统。

- `AddSystem(name, fn, opts...)` 注册系统并附带可选的 `Before(other)` 与 `After(other)` 约束，目标系统可以先引用后注册。
- `HasSystem(name)` 报告调度器中是否已注册名为 `name` 的系统。
- `Build()` 校验约束并通过 Kahn 算法解析顺序，无约束冲突时按注册顺序决胜；引用不存在的目标返回 `ErrUnknownSystem`，存在环路返回 `ErrSystemCycle`。
- `Order()` 返回解析后的系统名称列表，`String()` 将其格式化为 `"a -> b -> c"`，便于调试叠层显示。
- `Step(w, dt)` 在必要时完成解析并按顺序执行所有系统，稳态运行零分配。
- 对于可复用插件，`game/pkg/plugin` 会为每个调度器一次性添加四组标准阶段锚点（`StageInput` -> `StageUpdate` -> `StagePhysics` -> `StageLate`），让插件系统无需知晓游戏内部系统名即可挂载到对应阶段；见[插件](plugins.md#标准调度阶段)。

```go
sched := entities.NewScheduler()
_ = sched.AddSystem("input", readInput)
_ = sched.AddSystem("physics", stepPhysics, entities.After("input"))
_ = sched.AddSystem("render", syncSprites, entities.After("physics"))

loop := &game.Loop{
	Rate: 60,
	Update: func(dt time.Duration) {
		_ = sched.Step(world, dt)
	},
}
```

## 预制体（Prefab）

`Prefab` 是带名称的实体模板，每次生成时为新实体批量附着预设组件。

- `NewPrefab(name)` 创建模板，`Clone(name)` 可派生变体而不修改原模板。
- `With[T](p, v)` 附加按值复制的组件模板。
- `WithFunc[T](p, make)` 在每次 `Spawn` 时调用 `make()`，确保包含切片、映射或指针的组件在各实体之间完全独立。
- `Spawn(w)` 和 `SpawnN(w, n)` 按模板生成状态互不干扰的实体。

```go
goblin := entities.NewPrefab("goblin")
entities.With(goblin, Position{X: 10, Y: 20})
entities.WithFunc(goblin, func() Inventory {
	return Inventory{Items: []string{"dagger"}}
})

squad := goblin.SpawnN(world, 5)
```

## 2D 变换层级

`world.Hierarchy2D()`（或 `entities.NewHierarchy2D(world)`）为 World 挂载父子 2D 变换层级。局部变换保存在 `Transform2D` 组件中（`X`、`Y`、`Rotation`、`ScaleX`、`ScaleY`、`Hidden`；零值的缩放为 0，请用 `NewTransform2D` 或 `IdentityTransform2D` 构造），`Sync()` 把它们组合成缓存的 `WorldTransform2D` 组件，可直接用 `Each1`/`Each2`/`Each3` 遍历。

```go
h := world.Hierarchy2D()
ship, turret := world.Spawn(), world.Spawn()
h.SetLocal(ship, entities.NewTransform2D(100, 50))
h.SetLocal(turret, entities.NewTransform2D(0, -12))
_ = h.SetParent(turret, ship, false)
h.SetRotation(ship, 0.5)
h.Sync()
wt, _ := h.WorldTransform(turret)
_ = wt
```

- `SetLocal`、`SetPosition`、`SetRotation`、`SetScale`、`SetVisible` 以及用 `entities.Set` 写入 `Transform2D`，都会把该实体及其子树标记为脏。通过 `Get` 或存储拿到 `*Transform2D` 并直接修改后，需调用 `h.MarkDirty(e)`。
- 可见性会继承：实体本身或任一祖先隐藏时，`WorldTransform2D.WorldVisible` 为 false。
- `SetParent(child, parent, keepWorld)` 与 `Detach(child, keepWorld)` 默认保留子节点的局部变换；传入 `keepWorld` 时会重新计算局部变换，让子节点在屏幕上保持原位。若保持世界变换所需的相对变换含有切变（非均匀缩放父节点下的旋转子节点），会返回 `ErrNotDecomposable` 且不做任何修改；形成环时返回 `ErrCycle`，实体已销毁时返回 `ErrDeadEntity`。
- 销毁父节点时按层级的 `RemovePolicy`（`SetRemovePolicy`）处理子节点：`RemoveDetach`（默认）分离子节点并保留其局部变换，`RemoveDetachKeepWorld` 分离子节点并保留其世界变换，`RemoveCascade` 销毁整棵子树。`DestroyWithPolicy(e, policy)` 可为单次调用指定策略。
- 快照只包含已注册的组件：调用 `RegisterCodec[entities.Transform2D](w, "transform2d")` 才会保存局部变换。`WorldTransform2D` 是派生数据，无需编解码器，层级会根据恢复后的局部变换重新建立。

## 从 YAML 场景生成实体

`game/sceneformat` 可将自包含的 YAML 场景直接生成到 `*World` 中：`SpawnEntities` 仅附加局部变换与 `ObjectRef`，而 `SpawnEntitiesWith` 还会通过 `ComponentRegistry` 中声明的 `ComponentType.Spawn` 附加强类型 ECS 组件：

```go
type Health struct{ HP, Max int }

reg := sceneformat.NewComponentRegistry()
reg.MustRegister(sceneformat.ComponentType{
	Name: "health",
	Fields: []sceneformat.Field{
		{Name: "hp", Kind: sceneformat.FieldInt, Default: 10},
		{Name: "max", Kind: sceneformat.FieldInt, Default: 10},
	},
	Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
		entities.Set(w, e, Health{HP: fields["hp"].(int), Max: fields["max"].(int)})
		return nil
	},
})

spawned, err := sceneformat.SpawnEntitiesWith(doc, world, sceneformat.SpawnOptions{Components: reg})
```

- **生成前校验与默认值**：当 `SpawnOptions.Components` 非 nil 时，会在生成第一个实体之前校验每个对象上的所有组件并填充注册默认值（`ComponentRegistry.Resolve`）。仅用于校验文件的注册表可将 `ComponentType.Spawn` 留空。
- **确定性顺序**：实体按深度优先的文档顺序生成（父对象先于子对象，写入 `Transform2D`、`ObjectRef` 并挂入 `Hierarchy2D`），同一对象内的组件按组件名升序执行 `Spawn`。隐藏图层的顶层对象会以隐藏状态生成（整棵子树在世界中不可见），但仍会正常运行 `Spawn`。
- **出错回滚**：若任一 `ComponentType.Spawn` 返回错误，本次调用期间已创建的实体会按逆序全部销毁（不影响 `w` 中原有实体），并返回包含图层名、对象 ID 与组件名的错误。
- **连接场景**：`sceneformat.SpawnConnected(doc, world, reg, ctx)` 会将 `reg` 传给 `SpawnEntitiesWith`，因此信号处理函数可以直接通过 `entities.Get` 读取强类型组件。

## 世界存档与加载

世界可序列化为确定性的 `WorldSnapshot` JSON 结构，并直接与 `game/save` 协同工作。

- `RegisterCodec[T](w, typeName)` 为组件类型 `T` 注册 JSON 编解码器；未注册的组件不会写入快照。
- `w.Snapshot()` 按槽位索引升序输出存活实体、按编解码器名称升序输出组件，完整保留索引与代数，使组件中保存的 `Entity` 引用在恢复后依然有效。
- `w.Restore(snap)` 先解码到临时世界再原子提交；若缺少编解码器或数据损坏，原世界保持完好。快照未包含的槽位（包括空快照）会保留递增后的代数，使 `Restore` 之前的旧句柄即便在重新生成实体后仍保持失效；回滚到更早代数时也会保留代数下限，避免销毁恢复后的实体时复用到回滚前已发出的代数。
- `SaveWorld(store, slot, w)` 与 `LoadWorld(store, slot, w)` 通过 `save.Store`（同时支持 `save.NewStorageStore` 和 `save.NewFileStore`，以及存档版本和迁移）保存和恢复世界。`LoadWorld` 在解码槽位时就检查快照，因此无法恢复的快照（未知的编解码器、错误的父实体、无法解码的组件）会回退到备份副本，其错误给出槽位名、行号和字段，例如 `data.entities[2].parent`。

```go
_ = entities.RegisterCodec[Position](world, "position")
_ = entities.RegisterCodec[Velocity](world, "velocity")

store := save.NewStorageStore(cx.Storage(), "game")
if err := entities.SaveWorld(store, "slot1", world); err != nil {
	return err
}

fresh := entities.NewWorld()
_ = entities.RegisterCodec[Position](fresh, "position")
_ = entities.RegisterCodec[Velocity](fresh, "velocity")
if err := entities.LoadWorld(store, "slot1", fresh); err != nil {
	return err
}
```
