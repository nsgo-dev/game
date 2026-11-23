# Read on demand: archiving, lifecycle governance and freezing

> Distilled from the notes governance practice of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). Consult it when wrapping up, in a retrospective, or at a major version jump; daily changes carry no extra burden beyond replacing nearby notes as they go.

**Contents**: §0 How often (as you go vs. a big cleanup) | §1 Judging staleness and the keep lines (1.1 always keep / 1.2 what to archive / 1.3 garbage / 1.4 calibration examples) | §2 The archiving workflow (CLI) | §3 The permanent freeze contract

---

## 0. How often: as you go vs. a milestone cleanup

There is no need to scan everything on every code change; scale the effort to the situation:

1. **Daily commits (90% of cases: no whole-tree burden)**:
   - **Update in place (80%)**: when you change a module, update the facts in the existing note that holds its decision.
   - **Audit on the spot (10%)**: when adding a note, search active notes by module name or keyword and classify on the spot (unrelated / partial overlap, link both / fully absorbed, then delete or archive / stale proposal, reject or delete). Never postpone a known match to the big cleanup.
2. **Milestone cleanup (10% of cases: governance when needed)**:
   - Catch only what searches missed at the time: a quarterly review, a major release, or when active notes have piled up enough to hurt search.

---

## 1. Judging staleness and the keep lines

Staleness is judged **neither by word count nor by age**, strictly by the note's "future value as a guard rail":

### 1.1 Always keep in the active area (stay in `implemented/` however old)

A note with any of these traits is never stale and must never be archived:

- **Ownership boundaries**: it defines who may call a module and how architectural responsibilities are split (such as "the UI layer never talks to the database directly").
- **Negative guarantees**: it promises "this capability is never offered" or "the old protocol is never supported again".
- **Security and compliance floors**: hard constraints against injection, authentication bypass or data leaks.
- **Rejected options easy to step into again (Alternatives considered)**: it records a very tempting path that proved in practice to deadlock or crash.

### 1.2 What to archive (move to `archived/`)

Archive only records in `implemented/` that meet these conditions:

- **Fully replaced by a new architecture but still with independent leverage**: a later note has absorbed the core logic, while the old note still holds negative guarantees, ownership boundaries or conditions for reintroduction. Archive it and put the link in the new note. Delete it if you can; see `references/when-to-write.md` §4.
- **One-off designs and narrow details**: a one-off UI margin tweak, a narrow adapter for a platform since dropped, a simple bug fix fully closed out.

### 1.3 Garbage (never enters the archive)

- **`proposed/` is never archived**: a proposal whose premise is gone (a dependency upgrade now provides the capability, say) or that is no longer pursued becomes `Status: rejected — <one-sentence reason>`; one absorbed by another plan is closed directly.
- **Worthless `rejected/` notes are deleted**: if the old technology or API a rejected option depended on is completely gone, so no future agent could propose that wrong path again, delete the file outright and keep nothing redundant.

### 1.4 Calibration examples (to set the bar; the word counts show length is not the criterion)

Ported from DeepSeek Harness's archiving skill. Compare against the **structure of the reasoning** in these examples, not the word counts.

**Implemented notes to archive (the decision is done; its reasoning is unlikely to guide future work):**

- Collapsing the sidebar control bar (~530 words): a closed-out minor UI behavior;
- A CLI argument adapter (~1,500 words): heavy implementation detail, but almost no leverage on future design;
- The documentation generation pipeline (~920 words): the machinery has done its job, and the current generator itself is the authority.

**Implemented notes to keep (the reasoning still owns or constrains something):**

- Event-sourced session storage (~250 words): foundational authority and a persistence boundary;
- Cross-product ownership resolution rules (~600 words): a law on an ownership boundary;
- Project session directory policy (~630 words): durable storage and identity policy;
- Parallel pre-push gates (~400 words): a borderline case, but it still guides gate scheduling;
- Image content blocks taken offline (~330 words): kept until multimodal support lands, because it records the agreed conditions for reintroduction.

**Keeping or deleting rejected notes:**

- Keep "splitting the merged archive" (~430 words): the temptation to merge remains, so it still prevents a repeat;
- Delete "pushing streaming progress through tool calls" (~970 words): its ACP/UI premise is outdated;
- Delete "dropping ACP terminal metadata" (~360 words): a later automation decision already answered the question.

---

## 2. The archiving workflow

Use the CLI (it moves the file, seals it and reports inbound dead links in one go, **without needing git**):

```bash
bun .agents/skills/write-notes-like-deepseek/scripts/archive-agent-note.ts \
  .agents/notes/implemented/<class>/<filename>.md \
  [--superseded-by .agents/notes/implemented/<class>/<new note>.md]
```

The CLI does, in order:

1. **Writes the head marker**: inserts `Archived: YYYY-MM-DD` **directly below** `Status: implemented` (no blank line between lines 3 and 4; this is a hard contract matching the original DSH, and the seal verifier checks it line by line):
   ```markdown
   # Agent Note: xxx

   Status: implemented
   Archived: 2026-08-31

   ## Problem
   ...
   ```
2. **Moves the file**: `implemented/<class>/...` → `archived/<class>/...` (with git, `git mv` keeps history better; the CLI moves the file itself, so it works without git too).
3. **Seals it in `archived/manifest.json`**: records the file's SHA-256. After sealing, any edit to the archived content makes `verify-archived` fail.
4. **Reports inbound dead links**: scans active notes for relative Markdown links to the note (exact link resolution, not substring matching) and lists those that need fixing by hand.
5. **Optional `--superseded-by`**: checks that the new note exists, and appends a relative link to the archived path at the end of the **new note**. The archived note is not touched.

Apart from the single `Archived:` line in the head, **no other character of the archived body may change**.

> Backfilling history: if `archived/` already holds notes but has no manifest yet (a corpus imported from DSH, say), run
> `bun .agents/skills/write-notes-like-deepseek/scripts/verify-archived-agent-notes.ts --write` once to seal them. It first verifies that the existing seals are unchanged, then appends the missing entries, and never rewrites history.

---

## 3. The permanent freeze contract (Frozen Immutable Contract)

Once in `archived/`:

- **Read-only forever**: never edit, translate, reformat, update, move or delete it.
- **Mechanically enforced**: `verify-archived` checks the head layout, the manifest hashes, and that seals are append-only (with git, against a baseline ref; `AGENT_NOTE_ARCHIVE_BASE_REF` sets the baseline, defaulting to HEAD; without git it degrades to checking hashes against disk and says so). Seals are only ever added; changing or removing a sealed entry is an error.
- **Exempt from daily scans**: `verify-agent-note-tree` and `verify-agent-note-format` skip `archived/` by default, so broken outbound links in archived files never block the daily build.
- **Never the basis for current behavior**: in code conflicts or architecture reviews, `implemented/` is authoritative and `archived/` is only historical reference; supersession is expressed by the new note's links and by rewritten inbound links.

Run these commands from the host project root; if the skill is installed elsewhere, replace `.agents/skills/write-notes-like-deepseek` with the real path.
