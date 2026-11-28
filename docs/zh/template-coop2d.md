# coop2d 模板

`coop2d` 包含一个 1 到 4 人的 2D 协作房间的权威规则：房间码和邀请链接、固定 tick、在宽限期内离开和重新加入、队友救起倒地的玩家，以及没有队友站着时的单人重生。用下面的命令从它创建项目：

```sh
go run github.com/nsgo-dev/game/cmd/game@latest new -template coop2d mygame
```

也可以用 NSGo 自带的命令：`nsgo init -template github.com/nsgo-dev/game/template/coop2d -module example.com/mygame mygame`。其他模板见[模板](templates.md)。

## 包含的内容

- **按房间码进房。** `Server.CreateRoom` 用一个房间码开房，`RoomCode` 生成六个大写字符的房间码；`Server.Join` 把玩家加入房间，拒绝未知的房间码、已满的房间和已在房间里的玩家。`InviteLink` 把房间码写进链接的 `room` 查询参数，并保留链接原有的其他参数，`RoomFromInvite` 再把它读出来。
- **离开与重新加入。** `Server.Leave` 断开一名玩家，在 `rejoinGrace` 期间保留其位置和状态，倒地的玩家仍然倒地。`Server.Rejoin` 在宽限期内让其重新连接。宽限期结束后，`Server.Tick` 移除该玩家并返回其 ID，房间里没人时关闭房间。
- **救援与重生。** 游戏规则用 `Server.Down` 标记为倒地的玩家，在有队友在线且站着时，连续 `reviveTicks` 个 tick 后被救起；没有人站着时，`respawnTicks` 个 tick 后自己站起来。谁站着在每个 tick 开始时确定，所以结果与玩家的遍历顺序无关。
- **YAML 调优。** `coop.yaml` 包含版本、tick 频率、房间人数、宽限期和两个计时。`ParseCoopConfig` 拒绝未知字段、第二个文档和超出范围的值，`EncodeCoopConfig` 写回的文件与原文件逐字节相同。

`Server.Room` 返回房间状态的副本，玩家按 ID 排序；`Server` 可以并发使用。

## 尚未包含的内容

模板没有网络传输、大厅界面和游戏界面：客户端只有一个显示 `coop.yaml` 规则的界面，测试在进程内驱动 `Server`。游戏要通过 [netplay](netplay.md) 或自己的传输把 `Server` 接到客户端上，并在自己的服务器上运行它；玩家由游戏分配的 ID 标识，而不是恢复令牌。
