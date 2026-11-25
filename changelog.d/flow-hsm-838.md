### Added

- `game/flow`: hierarchical state machines (`HSM[S]`) with nested child machines, lowest-common-ancestor transitions, innermost-first parent/child evaluation, deterministic FIFO event transitions with `flow.Bus` binding (`BindBus`, `OnEvent`), optional shallow history, and active-path inspection (`Current`, `Leaf`, `In`, `TimeIn`) (#838).
