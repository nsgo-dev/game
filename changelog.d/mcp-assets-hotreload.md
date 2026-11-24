### Added

- `game/mcp`: added `game_assets_list`, `game_asset_reload`, and `game_assets_reload_group` tools plus `RegisterAsset`, `RegisterAtlas`, and `Config` asset/hot-reload hooks to inspect loaded asset groups/memory and hot-reload atlases, images, sounds, bytes, custom assets, shaders, materials, and YAML resources in place (#469).
- `game/assets`: added read-only `Manager.Disposed`, `Manager.GroupNames`, `Manager.LookupGroup`, `Manager.GroupStats`, `Group.Stats`, and on-demand in-place `Manager.ReloadAsset` and `Manager.ReloadGroup` helpers (#469).
- `game/tooling/hotreload`, `game/resource`: added `ShaderReloader.ShaderPath`, `ShaderReloader.ReloadShaderFile`, `MaterialReloader.MaterialPath`, `MaterialReloader.ReloadMaterialFile`, and `Library.ReloadFile` helpers for on-demand hot reload (#469).
