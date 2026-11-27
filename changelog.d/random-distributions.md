### Added

- `pkg/random`: `Stream.Normal(mean, stddev)` draws from a normal distribution, and `Stream.InCircle(r)`, `Stream.OnCircle()` and `Stream.InRect(x, y, w, h)` return points uniformly distributed inside a circle, on the unit circle (a random direction) and inside a rectangle, all from the seeded stream with a fixed, architecture-independent algorithm so replays and lockstep match (#1366).
