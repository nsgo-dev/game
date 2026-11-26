# Agent Note: Editor release notes drafting and confirmation-gated store uploads

Status: implemented

## Problem

The Build and Release screen ([editor build and release screen](2026-11-26-editor-build-release-screen.md)) displayed a basic in-memory text box for release notes and a placeholder **Review uploads…** button. Authors still had to write player-facing release notes by hand into a separate file for `game release -release-notes`, and store publishing to itch.io, Google Play internal testing, and TestFlight lacked an explicit confirmation dialog, a strict human-only trust boundary across agent permission profiles, and OS keychain credential storage (#1050).

## Decision

1. **Release notes drafted from commits and kept reviews, saved exclusively to the `-release-notes` file (`packages/game/editor/build_release_publish.go`)**:
   - Read git commits since the last release tag (`git describe --tags --abbrev=0` + `rev-list`/`log`) or since the initial commit when no tag exists, combined with kept reviews (`SetReleaseKeptReviews` / `AddReleaseKeptReview`) and `changelog.d/*.md` fragments.
   - Draft player-facing Markdown through pluggable `Editor.ReleaseNotesDrafter` (defaulting to deterministic `DefaultReleaseNotesDraft`, which converts `feat:`/`fix:` commit subjects into player-facing bullets, formats fixes as `Fixed: ...`, and filters out internal `chore:`/`ci:`/`test:` plumbing when player-facing items exist), displaying provenance `"Drafted from N commits since <tag/ref>"`.
   - Persist drafted notes and in-place edits (`SetReleaseNotesText`, `SaveReleaseNotes`, `DraftReleaseNotes`, `RedraftReleaseNotes`) atomically to the exact Markdown file read by `game release -release-notes` (`ReleaseNotesFile` / `SetReleaseNotesFile`, defaulting to `release-notes.md`), validated via `resolveBuildReleasePath(..., true)` so symlinks and project escapes are refused and editing release notes modifies ONLY that file.
2. **Unconditional human confirmation boundary for store uploads (`itch.io`, `Google Play · internal testing`, `TestFlight`)**:
   - Add `AgentPermissionProfile` (`PermissionProfileCautious`, `PermissionProfileBalanced`, `PermissionProfileAutonomous`) and `StoreUploadRequiresConfirmation(profile)` (which unconditionally returns `true`).
   - Opening **Review uploads…** (`ReviewReleaseUploads` / `OpenReviewUploads`) opens the Spec-tokenized `buildReleaseReviewUploadsDialog` modal listing every artifact (name, path, size), store destination, version, and keychain status with a fresh monotonic single-use `SessionID`, without ever starting an upload.
   - `StartReleaseUpload()` unconditionally returns `ErrUploadRequiresConfirmation`. `ConfirmReleaseUploads(sessionID)` rejects any call while `Editor.AgentActorActive()` is true (`ErrUploadRequiresPersonConfirmation`), rejects any call when the dialog is closed or `sessionID` does not match (`ErrUploadRequiresConfirmation`), and consumes `sessionID` immediately before starting sequential uploads on a background goroutine through pluggable `Editor.ReleaseUploader`.
   - Wire `editormcp.Server.asAgent` to `Editor.WithAgentActor`, and expose `release_notes_draft` and `release_review_uploads` MCP tools so even an `Autonomous` agent can draft notes and stage the Review uploads dialog but can never confirm or start a store upload.
3. **System keychain credential storage (`packages/game/editor/keychain*.go`)**:
   - Define a platform `Keychain` interface (`Set`, `Get`, `Delete`) with native desktop backends (`keychain_darwin.go` via `/usr/bin/security -i` reading hex-encoded secrets from stdin so secrets never appear in `argv`, `keychain_linux.go` via `secret-tool`, `keychain_windows.go` via pure-Go `advapi32.dll` `CredWriteW`/`CredReadW`/`CredDeleteW`, `keychain_other.go` returning `ErrKeychainUnavailable` for iOS/Android/other targets) and an injectable `MemoryKeychain` for tests.
   - Mark `KeychainCredential` fields `yaml:"-" json:"-"` with redacted `String`/`GoString`, load secrets fresh from the keychain only when executing a user-confirmed upload, and never write secrets into project files or editor snapshots.

## Alternatives considered

- **Storing release notes inside `game.yaml` or an editor-only cache**: Avoids managing a standalone Markdown file. Rejected because `AGENTS.md` and #1050 require `game release -release-notes` to read the exact Markdown file edited in the screen, and editing notes must touch only that single file without dirtying `game.yaml`.
- **Allowing store uploads automatically under `PermissionProfileAutonomous`**: Matches how Autonomous skips confirmation for local scene and Go edits. Rejected because publishing packages to external stores is irreversible and `Build.dc.html` / #1050 explicitly mandate `"Each upload asks first, whatever the permission profile"`.
- **Passing macOS keychain secrets via `security add-generic-password -w <secret>` on argv**: Shorter implementation than `security -i`. Rejected because process arguments are visible via `ps`, whereas piping `add-generic-password -U -s ... -a ... -X <hex>` over stdin keeps secrets out of `argv`.

## Consequences

- **Benefits**:
  - Release notes stay a single plain-text Markdown file shared directly between the editor and `game release -release-notes`, drafted automatically from commits, kept reviews, and changelog fragments.
  - Store uploads to `itch.io`, `Google Play · internal testing`, and `TestFlight` cannot be triggered by any agent or code path without explicit human confirmation in **Review uploads**, with full visibility of every artifact, size, destination, and version.
  - Store tokens stay isolated in the OS keychain and are structurally prevented from entering project files, snapshots, or logs.
- **Costs and known limits**:
  - Live external store protocols (`butler`, Google Play Developer API, App Store Connect / TestFlight) are injected via `Editor.ReleaseUploader`; the default uploader validates artifact presence and keychain credentials and directs callers to configure a store runner or `nsgo publish` rather than faking network uploads.

## Verification

- `(cd packages/game && env -u GOROOT go tool nsgo check ./editor && env -u GOROOT go tool nsgo check -target ios ./editor && env -u GOROOT go tool nsgo check ./editor/editormcp && env -u GOROOT go tool nsgo check -target ios ./editor/editormcp)`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor -- -run 'TestBuildRelease|TestOpenFromTheFileBrowserOpensTabs|TestDiscardFollowsTheProjectBack|TestLightingHandlesAlignOnParallaxLayer|TestLocaleCatalogsComplete|TestDesktopMenuBarMatchesRegistryAndExecutesActions')`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor/editormcp -- -run 'TestReleaseMCPTools|ExampleServer_AddTool')`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test -target macos ./editor/editorcmd -- -run TestBuildReleaseScreenMacOSScreenshots)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
