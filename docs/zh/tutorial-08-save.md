# 教程 8：存档

用 `game/save` 的带版本存档槽，跨启动保存玩家的最高分和玩过的局数，并在标题界面和结束界面显示最高分。

完成后的代码在 [`examples/tutorial/ch08-save`](../../examples/tutorial/ch08-save/)。

## 要保存什么

记录是 `records.go` 里一个带 JSON 标签的小结构体：

```go
type records struct {
	Best  int `json:"best"`
	Games int `json:"games"`
}

func (r *records) add(score int) (best bool) {
	r.Games++
	if score > r.Best {
		r.Best = score
		return true
	}
	return false
}
```

## 存档仓库

`save.NewStorageStore` 把存档槽保存在应用的 `core.Storage` 里，所有目标平台都有它，包括 Web：

```go
const (
	savePrefix     = "starcatcher"
	recordsSlot    = "records"
	recordsVersion = 1
)

func newRecordsStore(store core.Storage) *save.Store {
	return save.NewStorageStore(store, savePrefix, save.WithVersion(recordsVersion))
}
```

`Save` 把存档槽写成带版本信封的 JSON，即 `{"version":1,"data":{"best":12,"games":5}}`，保存在键 `starcatcher.slot.records` 下，并把上一份完好的副本留作备份；最新副本损坏时，`Load` 会退回到备份。以后的版本里 `records` 改变结构时，提高 `recordsVersion`，并给仓库加一个从旧版本升级的 `save.WithMigration`；之后 `Load` 会在读取时升级旧存档。数据迁移和基于文件的仓库见 [save 指南](save.md)。

## 启动时加载，结束时保存

加载只在游戏启动时进行一次：

```go
func loadRecords(st *save.Store) (r records, newer bool) {
	if err := st.Load(recordsSlot, &r); err != nil {
		return records{}, errors.Is(err, save.ErrFutureVersion)
	}
	return r, false
}
```

第一次启动时 `Load` 返回 `save.ErrNotFound`，游戏从空记录开始。连备份都读不出来的存档槽同样从头开始，而不是把玩家挡在游戏外面，下一次保存会替换它。由更新版本的游戏写入的存档槽则不同：这个版本读不懂它，但里面存着玩家的记录，比如运行过新版本之后，浏览器里还缓存着旧的网页包。游戏不带记录地运行，`loadRecords` 报告 `newer`，游戏把它保存在 `keepSlot` 里。

一局结束时，游戏记下这一局并保存：

```go
func (s *starCatcher) finish() {
	s.newBest.Set(s.records.add(s.world.score))
	s.best.Set(s.records.Best)
	if !s.keepSlot {
		_ = s.store.Save(recordsSlot, s.records)
	}
	s.setPhase(phaseOver)
}
```

`Save` 只会在存储已满时失败，这时丢失的也只是这一局的记录。只要设置了 `keepSlot`，游戏就完全不保存，新版本回来时看到的记录和它离开时一样。

## 显示最高分

有了最高分之后，标题界面会显示它；玩家打破纪录时，结束界面显示“New best!”：

```go
core.Show{
	When: s.newBest.Get,
	Then: func(*core.Scope) core.Node {
		return core.Text{TestID: "new-best", Content: "New best!", Style: core.Style{Color: bestColor, FontSize: 16, FontWeight: core.FontWeightBold}}
	},
	Else: func(*core.Scope) core.Node { return s.bestLine() },
},
```

运行这一章，玩一局，关掉应用再打开：标题界面会显示你的最高分。

## 测试存档

`nsgotest` 给每个应用一份独立的存储，除非测试用 `nsgotest.Storage` 填充，否则是空的；`app.Storage()` 让测试读取游戏写入的内容。测试在两个仓库之间保存和加载记录，检查存储里的信封是否完全符合预期，并检查损坏的存档槽和来自未来版本的存档槽都会从头开始，而且只有后者被报告为 newer：

```go
func TestUnreadableRecordsStartAfresh(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		newer     bool
	}{
		{"corrupt", "{not json", false},
		{"future", `{"version":99,"data":{"best":5,"games":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := nsgotest.Start(t, empty, nsgotest.Storage(map[string]string{recordsKey: tc.raw}))
			got, newer := loadRecords(newRecordsStore(app.Storage()))
			if got != (records{}) || newer != tc.newer {
				t.Fatalf("records = %+v, newer %v; want none, newer %v", got, newer, tc.newer)
			}
		})
	}
}
```

应用测试玩两局，检查第一局是新纪录、分数更低的第二局不是；启动一个存储里已有最高分 12 的应用，检查标题界面显示了它；再在一个来自未来版本的存档槽上玩一局，检查这个存档槽没有被改动。

## 下一章

游戏已经完整了。[第 9 章](tutorial-09-ship.md)让它准备好交到玩家手里。
