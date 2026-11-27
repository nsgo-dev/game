### Fixed

- `pkg/editor`: route 3D viewport mouse gestures through `Navigation3D.Resolve` so Maya (`Alt+left-drag` orbit, middle-drag pan, `Alt+right-drag` dolly) and Blender (middle-drag orbit, `Shift+middle-drag` pan) mappings apply to all mouse drags and plain left-drag stays on selection and gizmos (#1509).
