# Shared library

The packs `go tool game library` and the scene editor's library panel install by default: `index.yaml` lists them and `packs/<name>` holds each one's `pack.yaml` and files. To add a pack, put its directory under `packs`, run `go run ./cmd/game library digest ../../library/packs/<name>` from `packages/game` and add an entry with that `sha256` to `index.yaml`. The formats and rules are in [Asset and Plugin Library](../docs/en/library.md) ([中文](../docs/zh/library.md)).
