### Fixed

- `pkg/editor`, `pkg/editor/editormcp`: serialize all scope-less `Editor` callbacks (`Post`, agent, automation, first-run, task, start-screen, asset-build, export, and nil-`Run` `editormcp.Server`) through a single headless executor (`Editor.RunHeadless` / `Editor.PostOrRunHeadless`) drained by `Editor.AttachScope`, fixing data races in headless mode (#1547).
