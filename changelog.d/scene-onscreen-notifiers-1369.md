### Added

- `pkg/sceneformat`: added the built-in `on_screen` component, which emits `entered`/`exited` events (wireable through scene `connections`) when an object's rectangle enters or leaves every camera view, with a `margin` hysteresis band and a `pause` list that stops the object's particle emitter or skeletal rig updates while off-screen; `BuiltScene.Notifiers` holds the runtime states and `BuiltScene.UpdateWithViews` accepts extra views, such as every split-screen viewport's camera (#1369).
- `pkg/scene`: added `ScreenNotifier`, the code counterpart of the `on_screen` component for games that do not use scene files: a watched world `Rect`, a `Margin`, `OnEnter`/`OnExit` callbacks and a `Step(views ...*tilemap.Camera)` method (#1369).
