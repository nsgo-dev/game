### Fixed

- `game/navbake`: skip edge endpoints and prevent FMA contraction in polygon convexity validation so arm64 and amd64 agree on collinear vertices.
- `game/editor`: let the tools strip wrap independently in the editor toolbar and clamp the scene path label container so long scene paths cannot push tools off the right edge of the window.
