### Added

- `game/sceneformat`: `Signals.Enqueue` and `Flush` defer events to the end of an update step, running them in enqueue order without re-entering a handler that emits, and `SpawnConnected` spawns a scene's objects as entities after checking its connections, so `EmitEntity`, `EnqueueEntity` and each `Call`'s `SourceEntity` and `TargetEntity` address the entities (#694).
- `game/tooling/assetcheck`: `CheckWith` takes the game's component registry and reports a connection naming an event, handler or argument its objects do not declare, or a component that breaks its type, at the file and line; in a scene that instances or inherits others it also checks the components its overrides write and the connections of the resolved scene, reporting an override that breaks one at the override (#694).

### Changed

- `game check` resolves every scene that instances or inherits others, failing on one that does not resolve at the instance or `inherits` line, and on a connection whose `from` or `to` names no object of the resolved scene, at the line that names it (#694).
