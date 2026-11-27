### Fixed

- `pkg/physics`: substepped 2D `RigidWorld` solver with soft contact push-out, relaxation, and pinched high-mass-ratio block solving keeps 20-box columns standing, 20-base pyramids sleeping within 5 s, and 100:1 / 1000:1 heavy-on-light pairs within 1 px overlap at default settings (#1710).
