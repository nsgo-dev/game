### Fixed

- `pkg/physics3d`: calling `World.RemoveBody` inside `OnContact` or `OnTrigger` now suppresses remaining queued events for the removed body in the same step, avoids orphan `ContactExit` events for undelivered `ContactEnter` pairs, and keeps `World.ContactEvents()` in callback delivery order (#1595).
