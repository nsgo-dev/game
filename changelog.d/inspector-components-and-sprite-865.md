### Added

- `game/editor`: the inspector adds a registered component with its defaults from a searchable list of the types the object lacks and removes one with the connections that need it, adds properties by key and kind and deletes them, and edits the sprite (picked from the asset browser), origin, flips and material, each as an undoable step with a method on `Editor` (`AddComponent`, `RemoveComponent`, `ComponentConnections`, `AddProperty`, `SetObjectSprite`, `SetObjectOrigin`, `SetObjectFlip`, `SetObjectMaterial`); a duplicate component or an empty property key is refused in the status bar (#865).

### Fixed

- `game/sceneformat`: `Encode` writes a whole-number float in an object's custom properties with a decimal point (`2.0`), so it reads back as a float and not an int (#865).
