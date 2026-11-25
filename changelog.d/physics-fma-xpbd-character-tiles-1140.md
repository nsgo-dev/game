### Fixed

- `game/physics`: `SoftWorld`, `CharacterBody`, `MoveAndSlide` and `World.QueryOverlapLayer` no longer use fused multiply-adds or `math` trigonometry and `math.Pow` on arm64, so XPBD ropes and soft bodies, the character controller and tile collision give bit-identical results on amd64 and arm64, covered by new golden trajectories and an arm64 disassembly check (#1140).
