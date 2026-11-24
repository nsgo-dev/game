### Added

- `game/mcp`: add `game_prefabs_list`, `game_entity_spawn`, `game_entity_destroy`, `game_component_set`, and `game_component_remove` tools along with prefab registration (`Config.Prefabs`, `Bridge.RegisterPrefab`, `Bridge.RegisterEntityPrefab`, `Bridge.RegisterScenePrefab`) and typed component editing (`RegisterMutableComponent`, `RegisterComponentEditor`) (#464).
- `game/entities`: add `World.DecodeComponent`, `World.PatchComponent`, and `World.RemoveComponent` strict JSON codec mutation helpers (#464).
