### Added

- `game/navbake`: bake `ai.NavMesh` polygons from an outline with concave obstacles and holes, or from a tilemap's solid cells on orthogonal, isometric, staggered and hexagonal maps, shrunk by an agent radius so paths keep clear of walls; one mesh per agent type with `BakeAll`, a versioned YAML mesh file with byte-identical bakes and lossless round trips, and a `Draw` debug overlay of polygons and portals (#822).
