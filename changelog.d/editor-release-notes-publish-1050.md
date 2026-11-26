### Added

- `game/editor`: draft player-facing Markdown release notes from git commits since the last tag (or initial commit), kept reviews, and `changelog.d` fragments via pluggable `Editor.ReleaseNotesDrafter`, saving edits directly and exclusively to the file `game release -release-notes` reads (#1050).
- `game/editor`: add confirmation-gated store publishing (`itch.io`, `Google Play · internal testing`, `TestFlight`) with a Spec-tokenized **Review uploads** dialog, unconditional human confirmation regardless of agent permission profile, pluggable `Editor.ReleaseUploader`, and cross-platform system `Keychain` credential storage that never writes secrets to project files (#1050).
- `game/editor/editormcp`: add `release_notes_draft` and `release_review_uploads` MCP tools with agent-actor enforcement so agents can draft notes and stage upload reviews without ever starting an upload (#1050).
