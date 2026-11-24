### Added

- `game/mcp`: `Bridge` and `Attach` register `game_scenes_list`, `game_layers_list`, `game_entities_list`, `game_entity_inspect` and `game_component_get` through `nsgo/automation` so `nsgo mcp` can inspect scenes, draw-ordered layers, live entities with stable generational references (`entity:<index>:<generation>`) and component values (from `entities.RegisterCodec`, `mcp.RegisterComponent`/`RegisterComponentFunc`, and built-in `Transform2D`/`WorldTransform2D`/`sceneformat.ObjectRef`) (#461).
- `game/scene`: `Manager.Stack` returns active scene controllers from bottom to top, and `Layer.Parallax` returns effective parallax scroll factors (#461).
- `game/entities`: read-only, zero-reflection inspection helpers `MakeEntity`, `World.Resolve`, `World.CodecNames`, `CodecName`, `World.HasCodec`, `World.EncodeComponent`, `World.EntityComponents`, `World.ParentOf` and `World.ChildrenOf` (#461).
