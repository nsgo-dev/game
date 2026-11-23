# idle

`game/idle` holds what idle and incremental games need: a `Number` that reaches far past float64's 1.8e308, progress earned while the game was closed, and formatting of very large numbers as 1.23K, 1.23e45 or 1.23万.

```go
import "github.com/nsgo-dev/game/packages/game/idle"
```

## Numbers past float64

A `Number` is a plain float64 for magnitudes from 1e-300 to 1e300 and a float64 mantissa with an int64 power of ten beyond, up to 10^`MaxExponent` (10^(10^18)). Inside the plain range its arithmetic is float64 arithmetic, so integers up to 2^53 stay exact; past it a result keeps float64's 15 to 17 significant digits.

```go
gold := idle.New(250)
rate := idle.NewExp(1.5, 400)               // 1.5e400 a second
gold = gold.Add(rate.Mul(idle.Seconds(dt))) // in game.Loop's Update

cost := idle.New(10).Mul(idle.New(1.15).Pow(owned)) // base × growth^owned
if gold.Cmp(cost) >= 0 {
	gold = gold.Sub(cost)
	owned++
}
```

- `Add`, `Sub`, `Mul`, `Div`, `Scale`, `Pow` (an integer power by repeated squaring), `Neg`, `Abs`, `Min` and `Max` return new values; `Div` by zero panics.
- `Cmp`, `Sign` and `IsZero` compare; equal values are also `==`, since every value has one representation.
- `Mantissa` and `Exponent` give the decimal parts, `Float64` the nearest float64 (±Inf past its range), and `Log10` a float for progress bars.
- Results past 10^`MaxExponent` saturate to the largest value of that sign, and magnitudes below 10^-`MaxExponent` become zero.
- Every operation is a plain IEEE-754 float64 operation with no fused multiply-add, so the same sequence gives the same bits on every platform; the package's tests pin one on linux/amd64 and darwin/arm64. `Log10` is the exception and is meant only for display.

### Text in saves and data files

A `Number` reads and writes itself as text, so it is a string in a JSON save slot (`"gold":"1.23e45"`) and a plain scalar in YAML data a person writes (`cost: 7.5e400`). `String` writes positional text for exponents from -6 to 20 (`1500`, `0.25`) and scientific beyond (`1.23e45`), with the shortest digits that read back exactly, so `Parse(n.String())` returns `n`.

`Parse` takes an optional sign, digits with an optional point and an optional exponent up to ±`MaxExponent`. It refuses spaces, underscores, hexadecimal, `inf` and `NaN` with `ErrSyntax`, and exponents past the range with `ErrRange`.

## Offline progress

A game stores the time it was last seen, and on its next start, or on return from the background, `Offline` credits the time in between:

```go
type Progress struct {
	Gold     idle.Number `json:"gold"`
	Rate     idle.Number `json:"rate"` // per second
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

- `Cap` is the longest absence credited (0 for none), `Min` the shortest (shorter credits nothing), and `Efficiency` the fraction of the online rate earned while away (0 means the full rate). Negative values panic.
- `Earn` returns rate × credited seconds × efficiency; `Absence` returns only the `Away` record, for a game that computes its own rewards.
- `Away.Steps(step)` splits the credited time into whole fixed steps and a remainder, for a game that replays its simulation step by step instead of multiplying a rate.
- The difference is wall-clock time: monotonic readings are dropped, so time asleep counts and a timestamp read back from a save compares like one taken now. Time zones do not matter.
- A zero last-seen time, as on a first launch, credits nothing. A clock set back before the last-seen time sets `Rewound` and credits nothing; `Seen` keeps the later of the two times, so storing it means that setting the clock back and forward again cannot credit the same hours twice. A clock set forward cannot be told from real time away, so a game that needs that guarantee measures the absence against a server's time.

`Seconds(d)` converts a `time.Duration` to a `Number` of seconds.

## Formatting large numbers

`Format` writes a `Number` in a notation and locale. The zero `Format` writes `Short` notation with three significant digits in English:

| Value | `Short` en | `Short` zh-Hans | `Scientific` | `Engineering` | `Letters` | `Full` |
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
buf = hud.Append(buf[:0], gold) // no allocation when buf has room
```

- `Short` divides by the locale's largest unit at or below the value: K, M, B, T, Qa, Qi, Sx, Sp, Oc, No, Dc, Ud, Dd, Td, Qad, Qid, Sxd, Spd, Ocd, Nod and Vg (10^3 to 10^63) in English, and the myriad units 万, 亿, 兆, 京, 垓, 秭, 穰, 沟, 涧, 正 and 载 (10^4 to 10^44) in Chinese. Values below the first unit are positional with grouping (`9,999`), and values past the last unit's range are scientific.
- `Scientific` writes one integer digit (`1.23e45`), `Engineering` a power of ten that is a multiple of three (`12.3e45`), and `Letters` K, M, B and T followed by aa, ab, … zz, aaa and so on, which never runs out. All three are positional below 1000.
- `Full` writes every integer digit with grouping (`1,234,567`) up to 10^21, then scientific.
- `Digits` is the number of significant digits (default 3, at most 17). Integer digits are never dropped: 123456 is `123K`, and 1234 in `Full` is `1,234`.
- Trailing fraction zeros are trimmed unless `Fixed` is set: `1.2K`, or `1.20K` with `Fixed`, which keeps a counter's width steady.
- Rounding happens before the unit is chosen, so 999999 is `1M` and 99999999 in Chinese is `1亿`, never `1000K` or `10000万`.
- Magnitudes below 10^-6 are scientific in every notation.

## Locales

`English()`, `ChineseSimplified()` and `ChineseTraditional()` return new built-in locales; `LocaleFor(tag)` picks one from a BCP 47 tag (zh with Hant, TW, HK or MO is Traditional, other zh is Simplified, anything else English). Each call returns a copy, so a game can change one, for example to name its own units.

A game adds a language, or its own units, in a versioned YAML file:

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

- `version` must be 1, and unknown fields and trailing documents are refused.
- `decimal` and `exponent` are required, and `group` may be empty for no grouping; none may contain an ASCII digit or sign, and `decimal` and `group` must differ.
- `units` lists units in ascending `exp` order, from 1 to 1000000, with distinct, non-empty suffixes. A suffix carries its own spacing, such as `' million'`.
- `EncodeLocale` writes the canonical form: fields in a fixed order and 2-space indentation, so `ParseLocale` followed by `EncodeLocale` reproduces the same bytes. `Validate` checks a locale built in code.
