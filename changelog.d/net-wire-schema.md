### Added

- `game/pkg/online/netplay/wire`, `game/cmd/gamewire`: YAML wire message schema format (`version: 1`, published JSON Schema `wire.v1.json`), integer-only `qfloat` and `fixed` quantization, MSB-first bit packing, presence-bitmask delta helpers (`DiffX`/`ApplyX`), nibble-split and tag-discriminated unions, schemas for all seven `pkg/online/netplay` wire protocols, and a deterministic zero-allocation Go codec generator with `-check` verification (#1301).
