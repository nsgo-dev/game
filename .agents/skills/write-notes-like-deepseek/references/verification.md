# Read on demand: the verification scripts

> Expands SKILL §6. Consult it when wiring CI; skip it for light local use.

## The check scripts (TypeScript using only Node's built-in modules; each runs on its own with `bun`, `node` 22.18 or later, or `npx tsx`)

1. **`verify-agent-note-tree`** (`scripts/agent-note-tree.ts` + `scripts/verify-agent-note-tree.ts`)
   - Checks the closed lifecycle set `proposed/implemented/rejected` plus `archived`, the closed set of 6 classes, the path depth `{lifecycle}/{class}/file.md`, the filename `yyyy-mm-dd-topic.md`, that there is no `INDEX.md`, and that relative Markdown links inside active notes resolve.

2. **`verify-agent-note-format`** (`scripts/verify-agent-note-format.ts`)
   - Head: line 1 is the `# Agent Note:` title (a fullwidth `：` is accepted too, as Chinese input methods often type it), lines 2 and 4 are blank, and line 3 is `Status:`, matching the lifecycle and appearing exactly once in the note.
   - Skeleton: the first section must be `## Problem`; the required sections per lifecycle must be present (`## Decision`, `## Consequences` and so on; a parenthetical suffix such as `## Decision (summary)` is stripped before matching); `implemented` must not use proposal headings (`## Proposal` / `## Plan` / `## Migration plan` / `## Acceptance criteria`). The present tense is a prose rule; the body is not scanned for it.
   - Alternatives: `## Alternatives considered` is mandatory. The script does not check for a "do nothing / reuse" entry, and accepts no placeholder comment as an exemption.
   - Chinese aliases: notes in this repository are English, but the script still accepts the Chinese spellings listed in it (`状态：` status lines, and headings such as `## 问题`, `## 决策`, `## 后果` and `## 备选方案`), and bans the Chinese aliases of the proposal headings in `implemented` as well. This keeps notes written through a Chinese input method or imported from Chinese projects checkable; `references/note-format.md` explains it.
   - Compatibility: CRLF line endings and a BOM are normalized.

3. **`verify-archived`** (`scripts/verify-archived-agent-notes.ts`, standard)
   - For every archived note, the head layout is checked line by line (line 3 `Status: implemented` / line 4 `Archived: YYYY-MM-DD` directly below / line 5 blank). A `Superseded-by:` field is not recognized.
   - `archived/manifest.json`: every file has a seal entry, every entry has a file, and each sha256 matches the content on disk (the mechanical meaning of "frozen" is "cannot be altered").
   - Append-only: with git, the manifest is compared entry by entry with the one at a baseline ref (env `AGENT_NOTE_ARCHIVE_BASE_REF`, defaulting to HEAD locally). CI must point it at the commit before the change (this repository's workflow uses `pull_request.base.sha` for pull requests and `github.event.before` for pushes); using HEAD there checks nothing. A sealed entry changed or removed is an error. **Without git it degrades** to checking hashes against disk and prints a notice; it runs without git, only without the version baseline.
   - `--write`: first proves the existing seals are unchanged, then appends seals for unsealed files (use it to backfill a historical corpus).

4. **`check-note-anchors`** (`scripts/check-note-anchors.ts`, a soft report that always exits 0 and **stays out of CI**)
   - Scans source files (env `AGENT_NOTE_CODE_ROOT` sets the root, defaulting to the current directory; node_modules, .git, dist and the like are skipped) for `// Note:` / `# Note:` anchors, and reports dangling anchors, implemented notes no anchor points at, and anchor lines without a path. A host that does not use anchors need not run it.

## Wiring it in

- Light (personal / no git): the first two scripts are enough; archive seals take effect naturally after the first archive.
- Full (team): `bun run verify-notes` runs all three in CI and fails on any error. This repository runs them in `.github/workflows/notes.yml`; for another host, this skill's `templates/verify-notes.yml` is a starting point to copy into `.github/workflows/` and adjust the script paths in.

The three check scripts follow the method of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness), trimmed for a monolingual host: no bilingual triplets and no DSH repository-migration exemptions. The anchor check is an optional soft report, not a DSH gate.

## Script paths

`scripts/` here is relative to the skill's install directory. Keep the current directory at the host project root when running commands, and call the scripts by the real skill path; installing the skill does not add the host's package scripts.
