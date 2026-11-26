### Fixed

- `game/editor`: guard `Pick3D` ancestor-descendant box picking against infinite loops on duplicate or empty nested object IDs, and preserve centroid position overrides during multi-object 3D Rotate and Scale drags on instanced or inherited objects (#894).
