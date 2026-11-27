# Editor design

The design of the NSGo Game Editor: AI first, with the agent editing the project through the editor's MCP server while people ask, watch, review and fine-tune, and few manual tools, each finished. Every editor issue names the artboard it implements, and every editor pull request shows real nsgotest screenshots beside that artboard.

Each `.dc.html` file is one artboard of a design canvas; `canvas.json` lays them out. A file is plain HTML with its state in a small script, so it reads as a spec in review; it renders on the design canvas, which supplies the `support.js` runtime the files load. [`Spec.dc.html`](Spec.dc.html) holds the rules every artboard and every editor change follows: tokens, contrast, line styles, focus, keyboard scopes and shortcuts.

| Artboard | Shows |
| --- | --- |
| [`Main.dc.html`](Main.dc.html) | Editor: AI-first scene |
| [`Viewport3D.dc.html`](Viewport3D.dc.html) | Editor: 3D courtyard |
| [`Tiles.dc.html`](Tiles.dc.html) | Editor: tile painting |
| [`Inspector.dc.html`](Inspector.dc.html) | Inspector, multi-select, prefab overrides, context menu, viewport overlays |
| [`References.dc.html`](References.dc.html) | Find usages and safe rename of a prefab, as a review |
| [`PlayMode.dc.html`](PlayMode.dc.html) | Play mode: live tree and inspector, pause and step, live changes, ask about a frame |
| [`HUDLayout.dc.html`](HUDLayout.dc.html) | HUD and menus: anchors, safe areas, all sizes, 中文 overflow |
| [`Palette.dc.html`](Palette.dc.html) | Command palette (⌘K): commands with MCP tools, nodes, files, history, settings, ask the agent |
| [`Build.dc.html`](Build.dc.html) | Build and release: presets, readiness, running build, failure with agent fix, result and QR |
| [`Settings.dc.html`](Settings.dc.html) | Settings: editor and project |
| [`Start.dc.html`](Start.dc.html) | Start screen |
| [`Connect.dc.html`](Connect.dc.html) | Connect an agent |
| [`Plan.dc.html`](Plan.dc.html) | Plan first: plan card, clarifying questions, planned ghosts |
| [`Working.dc.html`](Working.dc.html) | Agent working: steps, permission, stop |
| [`Review.dc.html`](Review.dc.html) | Review: grouped changes, notes and round 2, conflicts, YAML errors, keep with undo |
| [`ReviewData.dc.html`](ReviewData.dc.html) | Review a data table: 40 items, old → new and Δ%, per-row keep or revert |
| [`ReviewCode.dc.html`](ReviewCode.dc.html) | Review Go code: nsgo check and test, ask the agent to fix, confirm each file |
| [`Problem.dc.html`](Problem.dc.html) | Problem: check failed, agent proposes a fix |
| [`Playtest.dc.html`](Playtest.dc.html) | Playtest report: bot profiles and metrics from the game's goals |
| [`AskInPlace.dc.html`](AskInPlace.dc.html) | Ask in place and history |
| [`Toasts.dc.html`](Toasts.dc.html) | Toast: variants, placement and queue |
| [`FirstRun.dc.html`](FirstRun.dc.html) | First run: Start, Connect, first plan, building, first review |
| [`AgentStates.dc.html`](AgentStates.dc.html) | Agent states: status chips and Partial, Stuck, Lost, Stopped, Mismatch, Offline |
| [`Permissions.dc.html`](Permissions.dc.html) | Permissions: denied, blocked by a rule, proposed deletion, You only draft |
| [`Checkpoints.dc.html`](Checkpoints.dc.html) | Files changed elsewhere, save status, project checkpoints and rewind |
| [`Threads.dc.html`](Threads.dc.html) | Threads and context: thread switcher, empty thread, @-mentions, context ring |
| [`Tasks.dc.html`](Tasks.dc.html) | Background tasks: queue, parallel overlays, notifications, status bar |
| [`Sketch.dc.html`](Sketch.dc.html) | Sketch for the agent: marks on the viewport, agent's reading, review overlay, images |
| [`Assets.dc.html`](Assets.dc.html) | Assets: browse, import, drag to viewport; generate, pick, style and license |
| [`Animation.dc.html`](Animation.dc.html) | Animation: sprite clips, state machine, cutscene timeline review |
| [`VersionControl.dc.html`](VersionControl.dc.html) | Version control: changes and agent-drafted commit, scene merge conflict per object |
| [`Spec.dc.html`](Spec.dc.html) | Design spec: tokens, contrast, overlay legend, focus, keyboard scopes, shortcuts, feedback channels |
