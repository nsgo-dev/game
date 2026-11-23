---
name: write-notes-like-deepseek
description: Use when a change is non-trivial by DSH standards (behavior, architecture, cross-file contracts, process/tooling, testing strategy, or on-disk/wire/config formats), when choosing between technical alternatives, superseding a decision, or writing a postmortem. Records the why and rejected options in .agents/notes/ with script-enforced gates; skips purely mechanical edits (CRUD, styling, patches, tagging, formatting).
---

# Write Notes Like DeepSeek

> The method is distilled from the engineering practice of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). The agent in the chat breaks down tasks and plans the work; this skill does one thing only: it keeps why a change was made, what was given up, and how it was proven right in one place, for the next person who changes that code.

## Red line: decide whether to write before deciding how

Models are eager to please and reach for a note at every turn. Pass this gate first.

**Return early: these are what DSH calls "purely mechanical or local changes". Never write a note for them; change the code directly:**

- Pure formatting, typos, unambiguous renames
- Styling that does not change behavior
- Dependency patches that do not change behavior, release tagging (RC or release tags)
- Routine CRUD, and explicit logic fixes inside one module that the diff alone explains (no cross-file effect)

**Aligned with DSH: a non-trivial change must carry a note.** A change is non-trivial if it hits any of these: it changes **behavior**, **architecture**, a **cross-file contract**, **process and tooling**, **testing strategy**, or an **on-disk, wire or configuration format**, or it is a decision another maintainer may revisit later.

**Before writing, work out what the note protects, along three directions:**

1. **Looking forward (a new rule)**: a new cross-module communication contract, state transition rule, access boundary or runtime invariant. Unrecorded, later agents each write their own and break through module boundaries at will.
2. **Looking back (a compromise)**: the mainstream or intuitive solution was given up because of a constraint nobody can see. Unrecorded, the rejected path gets walked again.
3. **Subtraction (narrowing)**: a breaking refactor, code removal, or a narrower API surface. Unrecorded, nobody knows the exit criteria and migration boundary, and the subtraction either stalls or goes too far.

In one sentence: **intent, boundaries and trade-offs that the code and its tests cannot state are non-trivial.** When unsure, err on the side of writing.

> For an AI, a rule written in prose is no rule at all. Every discipline that can be checked mechanically has a script behind it (see §6); decision reversals, the on-the-spot audit and present-tense bodies rely on the workflow, not on lexical scans.
> Precedence: the host project's `AGENTS.md` / `CLAUDE.md` and the user's direct instructions **override this skill**; this skill is the default contract, not a higher law.

| The excuse | The reality |
|---|---|
| "It's only a default value or a rename" | Defaults and names are decision facts; updating the old note in place takes 30 seconds |
| "Merge first, fill it in later" | Later means never; rot starts with every "fill it in later" |
| "The code is the documentation" | Code says **what**, never **why** or **what was given up** |
| "The change is too small to bother" | Small is not exempt; a single retry parameter once took down a whole network |
| "Not sure whether to write one" | Check the non-trivial list above: write if it hits, skip if it does not. Do not hide behind this sentence |

- **Update in place first; create only when needed**: in DeepSeek's public history, most note changes are in-place edits of facts in existing notes (paths, class names, default parameters), not new files. When a change already has an owning note, update that note.
- **Never turn a note into a different decision**: facts (paths, symbols, defaults) are edited in place; when the decision or its reasoning flips, open a new note and link the two. Never rewrite `## Decision` into its opposite, and never leave git as the only copy of the old reasoning.
- **Audit on the spot when writing a new note; never postpone it**: search proposed, implemented and rejected by module name or keyword, and classify every hit right away: unrelated / partial overlap (link both) / fully absorbed (delete or archive) / stale proposal (reject or delete). The classification lands together with the new note.
- **Garbage stays out of the archive**: a stale proposal becomes `Status: rejected — <reason>` (never archived); a rejected record with no value in preventing a repeat is deleted outright.

> A "decision" is not heavyweight: in a personal project it is simply "why A and not B". Choices, trade-offs and pitfalls hit are all worth writing down.

## 0. Probe first, then lay out

Do not build the whole tree up front. Choose by how the repository looks now:

1. There is an `AGENTS.md` / `CLAUDE.md` / contributing guide → read it. There are existing decision records such as `docs/adr/`, `docs/decisions/` or issue templates → keep using them, and follow this skill's folders for status and kind.
2. There are no existing decision records → build the standard structure: `.agents/notes/{proposed,implemented,rejected,archived} × 6 classes`. Create each directory when it is first used; empty directories need not exist ahead of time. Even a personal project without git uses the full structure; do not collapse it into a single directory.
3. A long-lived team repository → add process on top of the same structure: one line in `CONTRIBUTING.md` or the PR template saying "a significant change carries a note", and the verification scripts wired into CI.

## 1. The path is the classification

A note's path is its identity: `{lifecycle}/{class}/yyyy-mm-dd-topic.md`

**Lifecycle (first folder level: how far the note has come):**

- `proposed` — an idea with a plan that has not landed yet
- `implemented` — landed, and kept in sync with the code in the same change
- `rejected` — a proposal deliberately turned down, kept only while it prevents a repeat; otherwise delete it
- `archived` — an implemented record that is finished and of little future reference value; frozen and never edited

**Class (second folder level: exactly these 6; adding one means changing the check script):**

- `feature` new capability — a choice users or the model can see (non-obvious behavior counts too)
- `bug-fix` defect fixes — what was fixed, or a gap a postmortem exposed
- `simplification` removal only — no new capability, only less code, behavior or surface
- `architecture` how the structure is built — how shipped source is organized and how packages connect
- `process` tools and workflow — checks, releases, how people collaborate (around the code, not runtime behavior)
- `testing` how tests are written — test strategy and infrastructure

> There is no `refactor` class: if visible behavior changed, use the matching class; if not, it is `simplification`. Do not create an `INDEX.md`. Detailed criteria are in `references/classification.md`.

## 2. File format (checked by the scripts)

Note bodies are written in English (code, commands and proper nouns aside), whatever language the triggering task was given in.

The first three lines are fixed:

```markdown
# Agent Note: <title>

Status: <status>
```

The status must match the lifecycle folder the note lives in (`rejected` carries a one-sentence reason); the date in the filename is the date the note was **first proposed**. Body skeleton:

- `proposed`: `## Problem` → `## Proposal` → …free sections… → `## Alternatives considered` → `## Acceptance criteria` → `## Risks`
- `implemented`: `## Problem` → `## Decision` (present tense) → …free sections… → `## Alternatives considered` → `## Consequences`
- `rejected`: the frozen proposal form, with the verdict on the `Status:` line

> Alternatives are mandatory: record only rivals that were really considered, and state each one's strongest argument before rejecting it. Do not invent options nobody weighed. "Do nothing / keep what exists" goes in only if it was really weighed at the time. The script only checks that `## Alternatives considered` exists. When there truly was no real alternative, keep the heading and write "None" in the body; that passes, so do not make one up to fill the space.
> `## Decision` in an `implemented` note is in the present tense; the gate only rejects proposal headings (`## Proposal` / `## Plan` / `## Migration plan` / `## Acceptance criteria`). Details are in `references/note-format.md`.

Templates are in `templates/`.

## 3. Search past decisions before acting (4 decentralized methods)

Before refactoring or choosing between options, look for earlier constraints, so pitfalls are not hit twice and earlier compromises are not broken:

1. **Entry-point comments (if any)**: if an entry point already carries `// Note: ... see .agents/notes/...`, follow it. If there is none, use the other three methods; do not add anchors just to search.
2. **Slice the class tree**: do not scan everything; go straight to the directory for the intent (`implemented/architecture/` for architecture, `rejected/` for pitfalls).
3. **Precise global search**: use ripgrep for keywords or mechanism names, **always with `--hidden` and excluding `archived/`**:
   ```bash
   rg --hidden --glob '!.agents/notes/archived/**' "<mechanism or keyword>" .agents/notes/
   ```
4. **Drill down from module docs**: when a module README covers design reasons, follow its relative Markdown links to the note.

**Look it up yourself before asking the user.** Facts the four methods can answer are not questions for the user; only real decisions take the user's time.

## 4. When to write and when to update

**Triggers in the conversation**: when the user or you say something like this, start writing (except for the mechanical changes in the red-line list at the top; these phrases are no excuse for over-recording):

- Settling on a new direction: "go with X", "we decided on X", "let's use X for now" → write a note
- Comparing: "X or Y?", "why lean towards X?" → record the alternatives
- The same reasoning has been explained a second time → it is time to write it down

Judgment and workflow:

- **Refactoring existing architecture / moving paths / changing parameters** → **update in place first**: fix the facts (code paths, method signatures, defaults) in the old note that holds the decision. Do not start a new note, and do not append a running history to the body. The core reasoning in `## Decision` stays; if the reasoning changes, write a new note.
- **Before writing a new note** → search active notes by module name or keyword and classify on the spot (see "audit on the spot" at the top); never leave it for a later cleanup.
- **A new idea, not started yet** → write a `proposed` note first (why, and which paths were considered), and act after review. See the interaction protocol below.
- **Work finished (proposed → implemented)**, all in the same change: ① move it to `implemented/<class>/`, keeping the filename date; ② `Status: proposed` → `Status: implemented`; ③ `## Proposal` → present-tense `## Decision`; ④ fold `## Acceptance criteria` / `## Risks` into `## Consequences` (or a present-tense `## Testing`); ⑤ delete the planning sections. It lands together with the code; **with git, that is the same commit or PR**.
- **A plan partly superseded by a new decision** → keep both, add relative links both ways, and update only the facts that still hold. Do not archive.
- **A plan fully superseded by a new decision** → the new note takes over and absorbs every unique reason, alternative, consequence and verification gap of the old one. Once inbound links are updated, delete the old note if possible; if it cannot be deleted (it still has independent leverage), run `archive-agent-note.ts`. The pointer goes in the new note, never in the archived one.
- **No note needed**: see the red line at the top; changes on the early-return list are committed directly.

**Interaction protocol (when asking the user):**

1. **Separate facts from decisions**: facts the environment can answer (code, the note tree, rg) are looked up before asking; only real trade-offs take the user's time.
2. **Ask everything in one round**: number every open question, each on its own line with a recommended answer `➡️ <recommendation>`; the user answers by number in one go ("1 yes, 2 the second option"), without a drip of follow-ups.
3. **Converge through a confirmation gate, not a question quota**: before writing or acting, restate every decision and act only after the user confirms. Anything you defaulted in the restatement is corrected by the user at confirmation; nothing happens without confirmation.
4. **More than five open decisions is a signal, not a quota**: the change is too big. Split it into several notes, or submit a proposed draft for review first; do not force it through one conversation.

> Detailed criteria are in `references/when-to-write.md`; archiving and deletion are in `references/archiving.md`.

## 5. How to write well

- `## Consequences` states **both costs and benefits**, not only "what was given up".
- Free sections (package topology, wire contracts, schemas and so on) sit between `Decision` and `Alternatives`, keeping searchable mechanism names and `must / may / never` emphasis on ordering.
- Reference other notes with relative Markdown links such as `[topic](../../implemented/architecture/2026-…-….md)`, never bare numbers, so links can be checked mechanically.
- If the host already has the convention, a core entry point may carry one line `// Note: ... see .agents/notes/...`; it is not a gate. When a decision is superseded, these comments are the list of code to update.
- Style and removing reasoning traces: `references/prose-checklist.md`; simplification opportunities: `references/simplification-checklist.md`.
- After writing, go through the semantic self-check in `references/quality-gate.md` and report to the user only **gaps and what is done well** (at most 5 lines), with a concrete fix for each gap. Scripts check structure; people sign off on meaning.

## 6. Verification and maintenance commands

Run from the repository root:

```sh
bun run verify-agent-note-tree     # directory layout, classes, filenames, relative links between notes
bun run verify-agent-note-format   # head block, status, required sections, alternatives, no proposal headings in implemented
bun run verify-archived            # archive seals: head layout, manifest hashes, append-only (degrades without git)
bun run verify-notes               # all three in sequence (CI runs this)
bun run archive-agent-note <path> [--superseded-by <new note>]  # archive in one step; optionally link from the new note, and report inbound dead links
bun run check-anchors              # soft report: if the code has // Note: anchors, check both directions; not a CI gate
bun run init-board                 # generate the ~69 KB lightweight board board.html (recommended for daily work)
bun run bundle-board               # bundle a self-contained demo.html with every note's data inline
```

The scripts live in this skill's `scripts/`, are called from the **host project root**, and check `.agents/notes/` under the current directory by default. Work out the skill's location from the path of the loaded `SKILL.md`; do not assume a `scripts/` directory at the host root, and do not change into the skill directory to run them. Installing the skill does not add the host's package scripts.

After writing or changing any note, run the verification: `bun run verify-notes` where the package script exists, otherwise the direct commands below. "No verification entry point is configured" is never a reason to skip it.

With a project-level install under `.agents/skills/`, run them directly:

```sh
bun .agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-tree.ts
bun .agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-format.ts
bun .agents/skills/write-notes-like-deepseek/scripts/verify-archived-agent-notes.ts
bun .agents/skills/write-notes-like-deepseek/scripts/build-board.ts --init board.html "Engineering Decisions"
```

The scripts use only Node's built-in modules, so `node` 22.18 or later (which strips TypeScript types by default) or `npx tsx` run them too. With an install under `.claude/skills/` or a global directory, replace the prefix with the real skill path. The board bundle takes `--bundle <notes dir> <output.html> "name"`; arguments and exemptions are in `references/verification.md`.

A team can copy this skill's `templates/verify-notes.yml` to the host's `.github/workflows/verify-notes.yml`, adjust it to the real install path, and commit the project-level skill with it; the template does not run from inside the skill directory. `CONTRIBUTING.md` or the PR template can add one line: "a significant change carries a note".

## References

Load on demand:

- `references/note-format.md` — the head block and body skeleton in detail
- `references/classification.md` — the 6 classes and their boundaries
- `references/when-to-write.md` — when to create, update or move a note (including how it aligns with DSH's criteria)
- `references/archiving.md` — archiving, merging and deletion (including how to judge "future reference value")
- `references/prose-checklist.md` — prose and leak self-check
- `references/simplification-checklist.md` — simplification self-check
- `references/quality-gate.md` — semantic self-check after writing: Problem / Alternatives / Consequences / Verification, and how to report
- `references/verification.md` — the verification scripts
