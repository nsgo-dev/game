# idle

`game/idle` 提供放置类与增量类游戏所需的功能：远超 float64 上限 1.8e308 的 `Number`、游戏关闭期间的离线收益，以及把超大数字格式化为 1.23K、1.23e45 或 1.23万。

```go
import "github.com/nsgo-dev/game/pkg/idle"
```

## 超出 float64 的数字

`Number` 在 1e-300 到 1e300 的量级内就是普通的 float64，超出后改为 float64 尾数加 int64 的十的幂，最大可达 10^`MaxExponent`（10^(10^18)）。普通范围内的运算就是 float64 运算，2^53 以内的整数保持精确；超出后结果保留 float64 的 15 到 17 位有效数字。

```go
gold := idle.New(250)
rate := idle.NewExp(1.5, 400)               // 每秒 1.5e400
gold = gold.Add(rate.Mul(idle.Seconds(dt))) // 在 game.Loop 的 Update 中

cost := idle.New(10).Mul(idle.New(1.15).Pow(owned)) // 基础价 × 增长率^已拥有数
if gold.Cmp(cost) >= 0 {
	gold = gold.Sub(cost)
	owned++
}
```

- `Add`、`Sub`、`Mul`、`Div`、`Scale`、`Pow`（以平方求幂计算整数次幂）、`Neg`、`Abs`、`Min` 与 `Max` 都返回新值；`Div` 除以零会 panic。
- 用 `Cmp`、`Sign` 和 `IsZero` 比较；每个值只有一种表示，所以相等的值也满足 `==`。
- `Mantissa` 与 `Exponent` 返回十进制尾数和指数，`Float64` 返回 float64（超出范围为 ±Inf），`Log10` 返回供进度条使用的浮点数。在普通范围内 `New(f).Float64()` 精确等于 `f`；超出后十进制尾数与 `f` 的误差不超过一个最低有效位（ulp）。
- 超过 10^`MaxExponent` 的结果饱和为同号的最大值，量级低于 10^-`MaxExponent` 的结果变为零。
- 每个运算都是不含融合乘加的普通 IEEE-754 float64 运算，同样的运算序列在所有平台上得到相同的比特；包内测试在 linux/amd64 与 darwin/arm64 上固定了一条序列的结果。`Log10` 是例外，只用于显示。

### 存档与数据文件中的文本

`Number` 以文本形式读写自身：在 JSON 存档槽中是字符串（`"gold":"1.23e45"`），在人工编写的 YAML 数据中是普通标量（`cost: 7.5e400`）。JSON 也接受普通数字（`"cost":1500`），`null` 不改变字段的值。`String` 在指数为 -6 到 20 时输出定点文本（`1500`、`0.25`），超出时输出科学计数法（`1.23e45`），并使用能精确读回的最短数字，因此 `Parse(n.String())` 返回 `n`。

`Parse` 接受可选的正负号、可带小数点的数字以及可选的指数。空格、下划线、十六进制、`inf` 与 `NaN` 返回 `ErrSyntax`，首位数字的量级超出范围时返回 `ErrRange`；范围在计入数字位置之后判断，因此 `0.001e1000000000000000002` 等于 10^(`MaxExponent`-1)，而零无论指数多大都是 0。

## 离线收益

游戏保存最后在线的时间，下次启动或从后台返回时，由 `Offline` 计入中间经过的时间：

```go
type Progress struct {
	Gold     idle.Number `json:"gold"`
	Rate     idle.Number `json:"rate"` // 每秒
	LastSeen time.Time   `json:"last_seen"`
}

offline := idle.Offline{Cap: 8 * time.Hour, Min: time.Minute, Efficiency: 0.5}

earned, away := offline.Earn(p.Rate, p.LastSeen, time.Now())
p.Gold = p.Gold.Add(earned)
p.LastSeen = away.Seen
store.Save("slot1", p)

if away.Credited > 0 {
	showWelcomeBack(earned, away.Elapsed, away.Capped)
}
```

- `Cap` 是计入收益的最长离线时间（0 表示不设上限），`Min` 是最短离线时间（更短的不计收益），`Efficiency` 是离线期间按在线速率计算的比例（0 表示全额）。负值、非有限的 `Efficiency`，以及大于非零 `Cap` 的 `Min` 都会 panic。
- `Earn` 返回 速率 × 计入秒数 × 效率；`Absence` 只返回 `Away` 记录，供自行计算奖励的游戏使用。
- `Away.Steps(step)` 把计入的时间拆成整数个固定步长与余量，供逐步重放模拟而不是乘以速率的游戏使用。
- 时间差按挂钟时间计算：单调时钟读数会被丢弃，所以设备休眠的时间也计入，从存档读回的时间戳与当前时间可以直接比较。时区不影响结果。
- 最后在线时间为零值（例如首次启动）时不计收益。时钟被调到最后在线时间之前时设置 `Rewound` 且不计收益；`Seen` 保留两者中较晚的时间，保存它之后，把时钟往回调再调回来也不能重复领取同一段时间。时钟被往前调无法与真实离线区分，需要这一保证的游戏应以服务器时间计算离线时长。

`Seconds(d)` 把 `time.Duration` 转为以秒为单位的 `Number`。

## 格式化超大数字

`Format` 按记数法与区域设置输出 `Number`。零值 `Format` 以英文 `Short` 记数法输出三位有效数字：

| 数值 | `Short` en | `Short` zh-Hans | `Scientific` | `Engineering` | `Letters` | `Full` |
| --- | --- | --- | --- | --- | --- | --- |
| 999 | 999 | 999 | 999 | 999 | 999 | 999 |
| 12345 | 12.3K | 1.23万 | 1.23e4 | 12.3e3 | 12.3K | 12,345 |
| 123456789 | 123M | 1.23亿 | 1.23e8 | 123e6 | 123M | 123,456,789 |
| 4.56e15 | 4.56Qa | 4560兆 | 4.56e15 | 4.56e15 | 4.56aa | 4,560,000,000,000,000 |
| 1.23e45 | 1.23Qad | 12.3载 | 1.23e45 | 1.23e45 | 1.23ak | 1.23e45 |
| 1e70 | 1e70 | 1e70 | 1e70 | 10e69 | 10as | 1e70 |

```go
hud := idle.Format{Locale: idle.LocaleFor(localizer.Locale())}
goldText := hud.Format(gold)

var buf []byte
buf = hud.Append(buf[:0], gold) // buf 容量足够时不分配内存
```

- `Short` 用不超过数值的最大单位相除：英文为 K、M、B、T、Qa、Qi、Sx、Sp、Oc、No、Dc、Ud、Dd、Td、Qad、Qid、Sxd、Spd、Ocd、Nod 与 Vg（10^3 到 10^63），中文为万进制单位 万、亿、兆、京、垓、秭、穰、沟、涧、正 与 载（10^4 到 10^44）。小于第一个单位的值按定点输出并分组（`9,999`），超出最后一个单位范围的值改用科学计数法；需要超过 21 位整数的值也改用科学计数法，例如落在自定义区域设置两个单位之间较大空档中的值。
- `Scientific` 只保留一位整数（`1.23e45`），`Engineering` 的指数为 3 的倍数（`12.3e45`），`Letters` 依次使用 K、M、B、T，再接 aa、ab……zz、aaa 等，永远不会用尽。三者在 1000 以下都按定点输出。
- `Full` 在 10^21 以内输出全部整数位并分组（`1,234,567`），超出后改用科学计数法。
- `Digits` 是有效数字位数（默认 3，最多 17）。整数位不会被舍去：123456 输出 `123K`，`Full` 下的 1234 输出 `1,234`。
- 未设置 `Fixed` 时去掉小数末尾的零：`1.2K`；设置 `Fixed` 后为 `1.20K`，使计数器宽度保持稳定。
- 先舍入再选单位，所以 999999 输出 `1M`，中文的 99999999 输出 `1亿`，不会出现 `1000K` 或 `10000万`。
- 量级低于 10^-6 的值在所有记数法下都使用科学计数法。

## 区域设置

`English()`、`ChineseSimplified()` 与 `ChineseTraditional()` 返回新的内置区域设置；`LocaleFor(tag)` 根据 BCP 47 标签选择（带 Hant、TW、HK 或 MO 的 zh 为繁体，其他 zh 为简体，其余为英文）。每次调用都返回副本，游戏可以修改它，例如改用自己的单位名称。

游戏可以用带版本号的 YAML 文件添加语言或自定义单位：

```yaml
version: 1
name: de
decimal: ','
group: .
exponent: E
units:
  - exp: 3
    suffix: ' Tsd.'
  - exp: 6
    suffix: ' Mio.'
```

```go
de, err := idle.ParseLocale(data)
if err != nil {
	return err
}
idle.Format{Locale: de}.Format(idle.New(1500))                                 // "1,5 Tsd."
idle.Format{Notation: idle.Full, Digits: 6, Locale: de}.Format(idle.New(1234.5)) // "1.234,5"
```

- `version` 必须为 1，未知字段和多余的文档会被拒绝。
- `decimal` 与 `exponent` 必填，`group` 可以为空表示不分组；三者都不能包含 ASCII 数字或正负号，且互不相同。
- `units` 按 `exp` 升序列出单位，`exp` 取 1 到 1000000，后缀非空且互不相同，不能等于任一分隔符，也不能以 `exponent` 开头。后缀自带所需的空格，例如 `' million'`。
- `EncodeLocale` 输出规范形式：字段顺序固定、缩进 2 个空格，因此先 `ParseLocale` 再 `EncodeLocale` 会得到相同的字节。在代码中构造的区域设置可用 `Validate` 检查。
