# netplay/wire

`game/pkg/online/netplay/wire` 定义了供 `pkg/online/netplay` 与游戏自定义二进制负载（如带类型的 `EntityState.Props`）使用的 YAML 线路消息 Schema 格式、纯整数浮点量化辅助函数、MSB 优先位打包，以及由 `game wire` 驱动的确定性零分配 Go 编解码代码生成器。

```go
import "github.com/nsgo-dev/game/pkg/online/netplay/wire"
```

## 为什么采用 YAML Schema 加二进制编解码器

`pkg/online/netplay` 在网络线路上保留紧凑二进制格式，基于四个原因：存在位掩码增量、变长整数、跨 amd64/arm64/WebAssembly 的逐位确定性（见[确定性](determinism.md)），以及零分配编解码热路径（见[GC 与帧分配](gc-and-allocations.md)）。用带版本号的 YAML Schema 描述协议后，可以在不引入逐字段 tag、运行时堆分配或外部编译器工具链的前提下，获得机器可读规范、可校验的生成编解码器、基于 JSON Schema（`https://game.nsgo.dev/schemas/wire.v1.json`）的编辑器校验，以及面向抓包检查器与跨语言代码生成的共享元数据。

## Schema 文件结构

每个 Schema 都是命名为 `*.wire.yaml`、顶层带有 `version: 1` 的 YAML 文件。`wire.Parse` 会拒绝未知键、重复键、尾随多文档以及不支持的版本号。

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/wire.v1.json
version: 1
package: props
order: le
enums:
  - name: Stance
    type: u8
    values:
      - name: idle
        value: 0
      - name: running
        value: 1
messages:
  - name: FighterProps
    fields:
      - name: team
        type: bits
        bits: 4
      - name: combo
        type: bits
        bits: 4
      - name: aim_x
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: aim_y
        type: qfloat
        qfloat:
          min: -1
          max: 1
          bits: 12
      - name: stance
        type: Stance
      - name: hp_delta
        type: svarint
        bits: 32
      - name: stamina
        type: fixed
        fixed:
          scale: 100
          type: u16
      - name: state
        type: mask
        mask:
          type: u8
          delta: true
          fields:
            - name: shield
              type: qfloat
              qfloat:
                min: 0
                max: 100
                bits: 10
            - name: tag
              type: string
```

顶层键说明：

| 键 | 必填 | 说明 |
| --- | --- | --- |
| `version` | 是 | Schema 格式版本号（`1`，对应 `wire.CurrentVersion`） |
| `package` | 是 | 未指定 `-pkg` 时 `game wire` 输出的默认 Go 包名 |
| `order` | 否 | 多字节字段的默认字节序：`be`（默认）或 `le`，消息与单个字段均可覆盖 |
| `imports` | 否 | 同包下相邻 `*.wire.yaml` 文件的相对路径，用于引用其中声明的枚举与消息 |
| `enums` | 否 | 解码时会校验取值的命名整数枚举 |
| `messages` | 否 | 按精确线路顺序排列的二进制消息布局 |
| `unions` | 否 | 由前导 tag 或半字节拆分头判别的消息联合体 |

## 字段类型

| 类型 | Go 类型 | 线路编码 | 可选配置 |
| --- | --- | --- | --- |
| `u8`、`u16`、`u32`、`u64` | `uint8`..`uint64` | 按 `order`（`be` 或 `le`）写入 1/2/4/8 字节 | `order`、`const` |
| `i8`、`i16`、`i32`、`i64` | `int8`..`int64` | 按 `order` 写入 1/2/4/8 字节补码整数 | `order` |
| `uvarint` | `uint64`（配 `bits` 时为 `uint8`/`uint16`/`uint32`） | 无符号 LEB128（`encoding/binary`） | `bits`（`8`、`16`、`32`、`64`）、`const` |
| `svarint` | `int64`（配 `bits` 时为 `int8`/`int16`/`int32`） | ZigZag 有符号变长整数（`encoding/binary`） | `bits`（`8`、`16`、`32`、`64`） |
| `bool` | `bool` | 1 字节（`0` 或 `1`；大于 1 返回 `ErrInvalidBool`） | 无 |
| `f32` | `float32` | 按 `order` 写入原始 IEEE-754 32 位位模式 | `order` |
| `qfloat` | `float32` | 在 `[min, max]` 内用纯整数算法量化为 `bits`（1..32）位 | `qfloat: {min, max, bits}`、`order` |
| `fixed` | `float64` | 将 `round(v * scale)` 编码为 `fixed.type` 整数 | `fixed: {scale, type, order}` |
| `bytes` | `[]byte` | 长度前缀（默认 `uvarint`）后接原始字节 | `length`（`uvarint`、`u8`、`u16`、`u32`、`rest`）、`order`、`maxLength` |
| `string` | `string` | 长度前缀（默认 `uvarint`）后接 UTF-8 字节 | `length`（`uvarint`、`u8`、`u16`、`u32`、`rest`）、`order`、`maxLength` |
| `array` | `[N]T` 或 `[]T` | 固定 `count` 个元素，或带 `length` 前缀的 `of` 切片 | `array: {count, length, order, of, ofOrder, ofBits, maxLength}` |
| `bits` | `uint8`..`uint64` | 1..64 位，按 MSB 优先紧凑打包进相邻位流 | `bits`、`const` |
| `mask` | `<Name>Mask` 与受控字段 | 存在位掩码整数（`u8`..`u64`）后按位序跟随置位字段 | `mask: {type, order, delta, fields}` |
| `<Enum>` | `<Enum>` | 底层整数类型；未知取值返回 `ErrInvalidEnum` | `const` |
| `<Message>` | `<Message>` | 按字段顺序内联嵌入消息字节 | 无 |

### 常量字段（`const`）

在无符号整数、`bits` 或枚举字段上设置 `const` 后，生成器会输出 Go 常量（`<Message><Field>`），不将其放入结构体；`AppendX` 自动写入该常量，`DecodeX` 则校验字节是否等于该常量（不匹配时返回 `ErrInvalidConst`）。`pkg/net` 将其用于 `HandshakeHello.version = 1` 与 `RelayFrame.version = 1` 等固定协议头字段。

### 位打包与 `qfloat` 对齐

连续的 `bits` 字段以及大端或任意位宽的 `qfloat` 字段通过 `wire.BitWriter` 与 `wire.BitReader` 按 MSB 优先紧凑打包。例如两个 12 位 `qfloat` 在线路上恰好占 3 字节而非 4 字节。当位流在字节对齐字段或消息末尾结束时，未满字节的低位在编码时补零，解码时若非零则返回 `ErrNonZeroPadding`。位宽为 8、16 或 32 且显式指定 `order: le` 的 `qfloat` 会按小端序字节对齐编码。

### 存在位掩码与 `delta: true`

`mask` 字段声明一个存在位掩码（`u8`、`u16`、`u32` 或 `u64`，默认 `u8`），后面按位序跟随其受控字段（索引 `i` 对应 `1 << i`）。解码时，未置位字段会被清为零值（同时保留切片容量），确保复用结构体不会泄露上一帧的残留值；若掩码中存在超出已声明字段范围的高位，则返回 `ErrInvalidMask`。

当 `delta: true` 开启时，`game wire` 还会额外生成：

- `Diff<Message>(base, cur *<Message>) <maskType>`：浮点数按位模式（`math.Float32bits` / `math.Float64bits`）比较，字节切片用 `bytes.Equal` 比较，标量直接比较，与 `pkg/net` 的复制增量比较完全一致。
- `Apply<Message>(dst, delta *<Message>)`：将 `delta` 中掩码置位的字段复制到 `dst`，并复用 `dst` 的切片容量以保持零分配。

### 判别联合体与半字节拆分头

`union` 先编码前导判别头（`u8`、`u16`、`uvarint` 或 `nibble`），再编码选中的分支消息。生成的 Go 结构体包含 `Tag`（`nibble` 还会包含高位字段）以及各分支的值类型字段，不做接口装箱或堆分配。

使用 `discriminator.type: nibble` 时，首字节拆分为高 `highBits` 位（默认 4，默认字段名 `version`）与低 `8 - highBits` 位 tag（默认 4）。复制协议 v2 使用 `highBits: 4`、`highValue: 2` 及分支 tag `1..5`：`DecodeReplicationMessage` 会先校验低 4 位 tag（未知返回 `ErrInvalidTag`），再校验高 4 位版本（不匹配返回 `ErrInvalidVersion`，同时把收到的版本号写入 `m.Version`）。

## 纯整数浮点量化（`qfloat` 与 `fixed`）

`QuantizeFloat`、`DequantizeFloat`、`QuantizeFixed`、`DequantizeFixed`、`QuantizeFixedUint` 与 `DequantizeFixedUint` 直接拆解 IEEE-754 位模式，全程仅使用 `math/bits` 整数运算完成缩放、除法与舍入，编解码路径中不执行任何浮点算术指令（因此不存在 arm64 FMA 收缩差异）：

- `QuantizeFloat(v, min, max, bits)` 将 `NaN` 及 `<= min` 的值钳位到 `0`，将 `>= max` 的值钳位到 `(1<<bits)-1`，中间半点向上舍入。
- `DequantizeFloat(q, min, max, bits)` 对 `0` 精确返回 `min`，对 `(1<<bits)-1` 精确返回 `max`，中间值按 IEEE-754 最近偶数舍入还原为精确 `float32`。
- `QuantizeFixed(v, scale, minInt, maxInt)` 计算 `round(v * scale)`（半点远离零舍入）并钳位到 `[minInt, maxInt]`。
- `pkg/net/wire/quantize_test.go` 中的黄金测试向量会在 CI 的 `amd64` 与 `arm64` 上逐位校验。

## 生成的 Go API 与零分配契约

针对每条消息和联合体 `X`，`game wire` 生成：

```go
func AppendX(dst []byte, m *X) []byte
func DecodeX(src []byte, m *X) (int, error)
```

- 只要 `dst` 容量充足，`AppendX` 将编码结果追加到 `dst` 且零分配（可用 `dst = AppendX(dst[:0], &m)` 复用缓冲区）。
- `DecodeX` 写入调用方持有的 `m` 并返回消耗的精确字节数。变长 `bytes` 与 `array` 字段会复用已有切片底层容量（`m.Slice[:0]` / `m.Slice[:count]`），包括结构体元素内部的嵌套切片，使稳态解码保持零堆分配。
- `DecodeX` 在扩展变长 `array` 切片前会按剩余输入字节数校验下界，防止畸形数据包触发超大分配。
- Go 的 `string` 不可变；当字符串为空或与 `m` 中旧值相同时，`DecodeX` 不会重新分配。对于每帧内容都会变化的热路径文本字段，请优先使用 `bytes`。

## 使用 `game wire`

从目录或单个 Schema 文件生成 Go 或 TypeScript 编解码器：

```sh
go tool game wire -in ./netmsg -out ./netmsg
go tool game wire -lang ts -in ./netmsg -out ./netmsg/ts
```

或接入 `go generate`：

```go
//go:generate go tool game wire -in . -out .
//go:generate go tool game wire -lang ts -in . -out ts
```

命令行参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-in` | `.` | 输入 `*.wire.yaml` 文件或包含 `*.wire.yaml` 的目录（也接受位置参数传入多个文件） |
| `-out` | 输入目录 | 输出目录；当只传入单个输入文件时，也可指定单个 `.go` 或 `.ts` 输出路径 |
| `-pkg` | Schema 的 `package` | 覆盖生成的 Go 包名 |
| `-lang` | `go` | 目标语言（`go` 或 `ts`） |
| `-self` | `false` | 省略 `pkg/online/netplay/wire` 导入，供 `pkg/online/netplay/wire` 自身生成 Go 代码时使用 |
| `-check` | `false` | 校验已提交的生成文件是否与 Schema 一致且不改写磁盘；缺失或过期时返回非零退出码 |

CI 会运行 `go run ./cmd/game wire -in pkg/online/netplay/wire -out pkg/online/netplay/wire -self -check`、`go run ./cmd/game wire -lang ts -in pkg/online/netplay/wire -out pkg/online/netplay/wire/ts -check` 与 `go -C tools run ./gameschema -check`。

## 生成的 TypeScript API 与跨语言黄金测试向量

使用 `-lang ts` 时，`game wire` 会输出共享的零依赖 `runtime.ts`（包含 `Writer`、`Reader`、`WireError` 与纯 `BigInt` 量化函数）、每个 Schema 对应的 `<stem>_wire.ts` 以及汇总导出的 `index.ts`（若 `-out` 以 `.ts` 结尾，则内联运行时输出单个自包含文件）：

| 线路类型 | TypeScript 类型 | 说明 |
| --- | --- | --- |
| `u8`、`u16`、`u32`、`i8`、`i16`、`i32` | `number` | 基于 `DataView` 的定长整数 |
| `u64`、`i64` | `bigint` | 超过 `Number.MAX_SAFE_INTEGER` 仍保持精确的 64 位整数 |
| `uvarint`、`svarint`（`bits: 8\|16\|32`） | `number` | 带范围校验的 LEB128 / ZigZag 变长整数 |
| `uvarint`、`svarint`（默认或 `bits: 64`） | `bigint` | 完整 64 位无符号或 ZigZag 有符号变长整数 |
| `bits`（`1..32` / `33..64`） | `number` / `bigint` | MSB 优先位打包并校验补零位 |
| `bool` | `boolean` | 严格校验 `0` 或 `1` 字节 |
| `f32`、`qfloat`、`fixed` | `number` | 原始 `DataView` float32 或纯 `BigInt` 整数量化 |
| `bytes`、定长 `u8` `array` | `Uint8Array` | 基于调用方缓冲区读写；定长字节数组就地解码 |
| `string`、`array` | `string`、`T[]` | 带长度限制校验的 UTF-8 字符串与复用元素的数组 |

- 每条消息与联合体 `X` 都会导出 `createX()`、`encodeX(m: X, w: Writer): void` 与 `decodeX(r: Reader, m: X): void`，就地写入调用方对象以避免 60 Hz 下的逐包分配。
- 增量 `mask` 组会导出 `diffX(base, cur)`（通过 `float32Bits`/`float64Bits` 按位比较浮点、通过 `bytesEqual` 比较字节）与 `applyX(base, delta)`。
- `pkg/online/netplay/wire/testdata/vectors/*.yaml` 下的共享黄金测试向量覆盖全部内置 Schema、变长整数与 ZigZag 边界、超过 `2^53` 的 `u64`、混合字节序、`-0.0` 与 `NaN` 的 `f32` 位模式、`qfloat` 边界舍入、空与最大长度字节、增量 `diff`/`apply` 以及畸形包。CI 会在 Go（`go tool nsgo test ./pkg/online/netplay/wire`）与 Bun（`bun test pkg/online/netplay/wire/testdata/ts`）两侧逐字节校验全部向量。

## 内置 `pkg/online/netplay` 线路 Schema

`pkg/online/netplay` 现有的全部七种线路格式都已按原字节布局描述在 `pkg/online/netplay/wire/` 下（并通过 `wire.BuiltinFS` / `wire.LoadBuiltins()` 嵌入），差异模糊测试验证了生成编解码器与现有手写实现逐字节一致：

| Schema 文件 | 字节序 | 协议与消息 |
| --- | --- | --- |
| `transport.wire.yaml` | `be` | `Channel`、`TransportFrame`（`channel(1) seq(4) payload`）、`UDPPeerFrame`（`from(4) channel(1) seq(4) payload`） |
| `udp_sealed.wire.yaml` | `be` | `UDPDatagramType`（`0xB1`..`0xB7`）、`UDPSealedDatagram`（`type(1) peer(4) seq(4) channel(1) sealed`，ChaCha20-Poly1305 密文 + 16 字节认证标签） |
| `handshake.wire.yaml` | `be` | `HandshakeMessage`（`0xA1` `HandshakeHello`、`0xA2` `HandshakeWelcome`、`0xA3` `HandshakeReject`，版本 1） |
| `relay.wire.yaml` | `be` | `RelayFrame`（版本 1）、`RelayMembers`（`0x01`）、`RelayBody`（`0x01` `RelayUnreliable`、`0x02` `RelayReliable`、`0x03` `RelayAck`） |
| `replication.wire.yaml` | `le` | `EntityRecord`（增量位掩码字段）、`ReplicationMessage`（半字节版本 2 + 类型 `1..5`：`SnapshotFull`、`SnapshotDelta`、`SnapshotAck`、`ResyncRequest`、`SnapshotFullPart`） |
| `rollback.wire.yaml` | `be` | `InputFrame`（20 字节）、`RollbackMessage`（tag `10..12`：`SingleInput`、`InputBundle`、`ChecksumMsg`） |
| `membership.wire.yaml` | `be` | `MemberChangeKind`、`MatchMember`、`JoinState`、`MembershipMessage`（tag `13..14`：`MemberChange`、`JoinStatePart`） |
