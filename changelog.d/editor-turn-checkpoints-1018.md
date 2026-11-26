### Added

- Editor: persistent per-turn checkpoints stored as strict versioned YAML manifests and verbatim pre-turn files under `.nsgo/checkpoints/` (ignored by git, enforcing You-only boundaries, symlink refusal, and automatic size-cap pruning), pre-start checkpoints for autonomous tasks and automations, unified History panel (`⌘Y`) integration with cross-session checkpoint preview and one-step undoable multi-file restore, plus `checkpoint_list` and permission-checked `checkpoint_restore` MCP tools.
