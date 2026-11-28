# Agent Note: Credential and editor-state files are a privacy kind no switch allows

Status: implemented

## Problem

`ClassifyProjectFilePrivacy` (`pkg/editor/agent_settings.go`) sorted every project path into one of the four **WHAT MAY LEAVE THIS COMPUTER** kinds and ended with "default to scene and data files", which is on by default. `scene_read` reads any UTF-8 file in the project, so `.env` files, private keys and keystores, `.netrc` / `.npmrc`, SSH keys, `.git/config` (often holding a token in a remote URL) and the editor's own `.nsgo/` state were returned to any connected agent with nothing asked. Other tools that take a project path (`refs_find`, `file_edit`, `refactor_rename`, `tileset_read`, `states_test`, `cutscene_write`, `playtest_define`, the review proposals) had the same gap, and parse errors from the YAML-reading tools can echo file content.

## Decision

`PrivacySecrets` (`"secrets"`) is a fifth `PrivacyContentKind`. `AgentPrivacyPolicy.Allows` always returns false for it, it has no switch and no `AgentPrivacySettings` field, and `PrivacyRefusalError` gives it its own reason: "looks like a credential or the editor's private state; the editor never sends it to an agent".

`ClassifyProjectFilePrivacy` checks it first (`isSecretProjectPath`), on the slash-separated path lower-cased, then fully case folded and put in NFC (`golang.org/x/text/cases` and `unicode/norm`, already direct dependencies), so case-changed names match, and so do names that differ only by a fold `strings.ToLower` does not make but APFS and NTFS honor when opening, such as U+017F LONG S (`.nſgo`), U+FB06 LIGATURE ST (`keyﬆore.properties`) or U+212A KELVIN SIGN. A path is secret when any component is `.git`, `.nsgo`, `.aws` or `.ssh`, or the base name is `.env`, `.env.*`, `.envrc`, `.netrc`, `_netrc`, `.npmrc`, `.git-credentials`, `keystore.properties`, `credentials.json`, `google-services.json` or `GoogleService-Info.plist`, or matches `service-account*.json` / `service_account*.json`, or has the extension `.env`, `.pem`, `.key`, `.p8`, `.p12`, `.pfx`, `.jks` or `.keystore`, or is an undotted `id_*` name with an optional `.pub` (so `id_table.data.yaml` stays game data). Each component is compared without an NTFS stream suffix and without trailing dots and spaces, which Windows drops when opening.

A name alone does not settle what a file system opens: an NTFS 8.3 short name such as `KEYSTO~1.PRO` opens `keystore.properties`. `IsSecretProjectFile(root, rel)` therefore also classifies the path with each existing component that holds a non-ASCII rune or a `~` replaced by the name its directory stores for it (`projectPathOnDisk`, matching `os.ReadDir` entries with `os.SameFile`), and the in-project symlink target the same way. ASCII components are not looked up, since ASCII case, stream suffixes and trailing dots are already normalized, which keeps the check to one `Lstat` per component for ordinary paths.

Enforcement sits at four layers, all through `IsSecretProjectFile`:

- `checkPrivacyPre` runs `refuseSecretPathArgs` for every tool before it runs: any string under a `file`, `path` or `files` key, at any depth of the arguments, is cleaned and classified both as given and through in-project symlinks (`resolveProjectPrivacyKinds`). `refactor_rename`'s `from` and `to` go through `checkPathPrivacy`, which sees the new kind as well.
- `readProjectFileResolved`, the shared reader behind `scene_read`, `tileset_read`, `states_test` and `playtest_define`, refuses a secret requested or symlink-resolved path before opening the file.
- `resolveSafeProjectRegularPath` in `pkg/editor/agent_composer.go`, behind every composer read (file, prefab, rule and always-on chip payloads, `@`-mention first-line details and image sizes), refuses a secret path, and `AttachFileChip` refuses one even with no project open.
- The built-in agent's own `scene_read` gate (`executeToolWithPrivacy`) uses the same classifier, and the `@`-mention picker skips secret files, since a chip for a file the agent can never read only invites a refused call.

The project search index, Quick Open and the person's own editing are unchanged: the kind restricts what goes to an agent, not what the person sees.

## Alternatives considered

1. **A sixth switch, off by default**: lets a person opt in when a file was misclassified. Rejected because the files this kind covers are credentials and private state that no game-editing task needs, and a switch is one more way to leak them; a misclassified file can be renamed.
2. **Treating every path the project's `.gitignore` excludes as secret**: catches credentials with unusual names, such as a gitignored `config/secrets.yaml`. Not done here: it needs either a new gitignore matcher dependency or running `git check-ignore` on every path-taking call, and ignored files include build output, logs and caches the agent reads on purpose, so it changes what agents may read well beyond credentials and needs the maintainer's decision; it stays an open follow-up, as does a `game check` warning for tracked secret files.
4. **Only refusing names whose `strings.ToLower` differs from their full case fold**: cheaper than looking up stored names, but misses NTFS short names and normalization variants, and refuses harmless non-ASCII names. The fold is used for matching instead, with the stored-name lookup as a second layer.
3. **Checking only `scene_read`**: the smallest change. Rejected because the same path reaches file content through edits, renames, reviews and parse errors of other tools; the argument-level check covers all of them, including tools added later that follow the `file` / `path` / `files` naming.

## Consequences

- **Benefits**: no default or switch combination sends a credential file or `.git` / `.nsgo` content to an agent, through any path-taking tool, symlink or case variant.
- **Costs**: an agent can no longer read or edit an `.env.example` template or a `.key` file that holds no secret; the person edits those directly. A tool whose path argument uses another name than `file`, `path` or `files` relies on `readProjectFileResolved` or its own case in `checkPrivacyPre`.
- **Testing**: `TestSecretFilesNeverLeaveUnderDefaultPrivacy` and `TestClassifyProjectFilePrivacySecrets` (`pkg/editor/editormcp/secrets_privacy_test.go`) cover each pattern through `scene_read`, `refs_find`, `tileset_read`, `states_test`, `file_edit` and `refactor_rename`, symlinks, case changes, Unicode case-fold variants and every switch on, check that ordinary scenes, data and `AGENTS.md` still read, that `AttachFileChip` and `ResolveChipPayload` refuse every fixture, and that the mention picker offers no secret file. `TestProjectPathOnDiskUsesStoredNames` (`pkg/editor/agent_secret_paths_internal_test.go`) checks the stored-name lookup on a case-insensitive file system and skips on a case-sensitive one.

Related: [Settings pages for the agent's models, tools, usage and privacy](../../implemented/feature/2026-11-26-editor-settings-agent-pages.md).
