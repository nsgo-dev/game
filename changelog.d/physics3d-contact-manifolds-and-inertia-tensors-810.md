### Added

- `game/physics3d`: body-space inertia tensors (`DiagSymMat3`, `Body.InvInertiaTensor`, `Body.WorldInvInertiaTensor`), per-body overrides (`Body.SetInertia`, `Body.SetInertiaTensor`, `Body.SetCenterOfMass`), and implicit midpoint gyroscopic Euler torque integration so asymmetric spinning bodies exhibit intermediate-axis (Dzhanibekov) flips while symmetric boxes remain stable (#810).
- `game/physics3d`: multi-point contact manifolds with up to 4 clipped face contacts for box-box and convex hull/cylinder faces, up to 2 contacts for capsule-on-face pairs, persistent feature IDs, per-point warm starting and 3D Coulomb friction, and regularized active-set block LCP normal solving (#810).

### Changed

- `game/physics3d`: `World.Step` uses velocity-first symplectic Euler integration and scopes shock propagation to vertical support columns with high mass ratios (`> 4:1`) or stacks taller than 12 bodies, so ordinary 10-box towers stand and sleep without residual drift or shock intervention; all seven built-in `Joint3D` types now use world-space inverse inertia tensors and `WorldCenterOfMass` (#810).
- `game/vehicle`: chassis suspension, tire friction, and ground impulse transfer now use `Body.WorldInvInertiaTensor()` and `Body.WorldCenterOfMass()` so braking pitch and roll reflect the `Config` mass distribution (#810).
