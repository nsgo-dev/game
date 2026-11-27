### Fixed

- `pkg/physics`: assign world-owned internal IDs to default-ID bodies, triggers, and joints and match handles strictly by identity in `World.Restore` and `RigidWorld.Restore`, preventing surviving pointers from aliasing another entity's state after `Remove`/`RemoveBody` (#1593).
