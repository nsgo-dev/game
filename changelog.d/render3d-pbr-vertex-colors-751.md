### Added

- `game/render3d`: draw glTF `COLOR_0` linear RGBA vertex colors in the metallic-roughness and lit PBR pipelines via `MaterialPipelineOptions.VertexColors` (`MaterialColoredWGSL`) and `LitPBRPipelineOptions.VertexColors` (`LitPBRColoredWGSL` and its fog, IBL, shadow, and CSM variants), with matching CPU reference support in `EvaluateMaterial` (`MaterialEvalInput.VertexColor` / `HasVertexColor`) and `PBRMaterial.WithVertexColor` (#751).
