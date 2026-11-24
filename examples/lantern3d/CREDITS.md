# Lantern 3D — Asset Credits & Licenses

All 3D scene geometry and embedded textures in `examples/lantern3d/assets/` are **100% original works** created specifically for the `github.com/nsgo-dev/game` repository. They are deterministically generated from scratch using only the Go standard library by [`tools/genassets/main.go`](tools/genassets/main.go), with zero third-party models or textures.

## Asset Inventory

| File | Kind | Author | License |
| --- | --- | --- | --- |
| `assets/lantern_shrine.glb` | Binary glTF 2.0 3D scene (courtyard shrine, lantern, pillars, crate, crystal cluster, orbiting light wisp, and embedded stone/cedar PNG textures) | NSGo Game Contributors (`tools/genassets`) | [CC0-1.0](#cc0-10-universal) |

## Regenerating Assets

`examples/lantern3d/assets/lantern_shrine.glb` can be reproduced identically from the repository root with:

```sh
go run ./examples/lantern3d/tools/genassets/main.go
```

## Licenses

### CC0-1.0 Universal

All generated files under `examples/lantern3d/assets/` are dedicated to the public domain under the **Creative Commons Zero v1.0 Universal** (`CC0-1.0`) dedication:

> To the extent possible under law, the authors have waived all copyright and related or neighboring rights to the Lantern 3D assets in `examples/lantern3d/assets/`. You can copy, modify, distribute, and perform the work, even for commercial purposes, all without asking permission.
>
> Full text: <https://creativecommons.org/publicdomain/zero/1.0/legalcode>

### Generator Source Code

The Go asset generator source code in `examples/lantern3d/tools/genassets/` is licensed under the repository's [Apache License, Version 2.0](../../LICENSE) (`Apache-2.0`).
