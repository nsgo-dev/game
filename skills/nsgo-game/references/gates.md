# Production gates

Choose the Fast track for a single-scene 2D game without generated assets. Use the Full track for multi-scene games, generated assets, 3D, or platform-specific behavior.

Run a skilled reference route, a deliberately degraded route, and `playtest.ProfileIdle` on fixed seeds. The reference must reach its declared win goal. The degraded route must struggle or lose. Idle must lose or stall within its declared bound. Put the three outcomes and the three to five highest-risk invariants in `playtest.yaml` and report them in `playtest.report.yaml`.

Severity is P0 for crashes, freezes, failed invariants, refused files, or a blocking performance goal. P1 covers core-loop feel, difficulty, and readability. P2 covers polish. P0 and in-scope P1 findings block delivery. Spend at most three P1 fix rounds. If a P0 recurs across two rounds, change the mechanic instead of tuning thresholds again.

Never weaken a metric goal, brief target, or bot parameter just to make a run pass. Never add a test-only gameplay branch. Separate checked goals and replay evidence from unchecked judgments such as fun, real-device behavior, touch, audio, and visual polish. List no more than five numbered P2 items.
