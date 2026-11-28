# State trees

`ai.ParseStateTree` validates version 1 YAML state trees with nested states, enter conditions, behavior-tree task names and parameters, completion modes and prioritized transitions. `ai.EncodeStateTree` writes the schema comment and deterministic YAML, while `ai.LoadStateTreeFS` loads the same format from an asset filesystem. The format parser is the current boundary. Runtime stepping, bindings, snapshots and inspector integration remain follow-up work.
