# Read on demand: when to write, when to update, and how decisions move

> Expands SKILL §4 in depth. Check against it once for every significant change or review.

## The core idea: update the owning note, never create a duplicate

DeepSeek's public history (the git log of deepseek-ai/deepseek-harness) shows the same shape: new notes are rare, and **in-place fact corrections** of existing notes make up the vast majority.
**Do not treat a note as a one-off blog post or a running log. It is "current law" that lives and dies with the code.**

A note is never rewritten into a different decision: facts (paths, symbols, defaults) are edited in place; when the decision or its reasoning flips, open a new note and link the two. Never rewrite `## Decision` into its opposite, and never leave git as the only copy of the old reasoning.

---

## 1. Decision matrix

| Situation | Action | Typical pattern |
|---|---|---|
| **Refactoring, renaming, moving a package, changing a default** | **Update the facts in the existing note in place** | The core reasoning of Decision stays; change the paths, class names and defaults directly (no change log appended). If the reasoning changed, write a new note instead |
| **Before writing a new note** | **Audit active notes on the spot** | Search proposed, implemented and rejected by module name or keyword; classify every hit right away (unrelated / partial overlap / fully absorbed / stale proposal) and land the result with the new note. Never postpone it to a big cleanup |
| **A new architectural attempt or technology choice** | **Create a `proposed/` note** | Before coding, state the motivation, the alternatives really considered and the acceptance criteria |
| **Development done, ready to merge into main** | **Move it to `implemented/`** | See the rewrite list in §3; it lands with the code (with git, the same commit or PR) |
| **A plan partly superseded by a new decision** | **Keep both and link them** | Update only the facts that still hold. Do not archive |
| **A plan rejected in review** | **Move it to `rejected/` or delete it** | Keep it, with the reason, only if the reasoning prevents a future repeat; otherwise delete it entirely |
| **A new architecture fully replaces the old** | **Absorb first; delete if possible, otherwise archive** | See §4. The pointer goes in the new note, never in the archived one |
| **Release tagging, small dependency patches, formatting** | **Exempt (not applicable)** | Commit the code directly; no note needed |

---

## 2. In-place fact sync

When the module an existing note protects evolves but the core decision is not overturned:

1. **Edit directly; no change log**:
   - ❌ Wrong: appending `### 2026-09-02 update: class renamed to X` at the end
   - ✅ Right: replace the old class name with the new one in the body, so that whenever the note is opened it reflects the facts at HEAD.
2. **Always the present tense**:
   - Always the present tense (`the system uses SQLite as its persistence engine`), never a change narrative (`the system no longer uses JSONL and now uses SQLite`). The gate only rejects proposal headings; the present tense relies on this discipline, not on a lexical scan.

---

## 3. The proposed → implemented rewrite list

All in the same change:

1. Move it to `implemented/<class>/`, keeping the filename date (the day it was first proposed)
2. `Status: proposed` → `Status: implemented`
3. `## Proposal` → `## Decision`, rewritten in the present tense (landed facts)
4. Fold `## Acceptance criteria` / `## Risks` into `## Consequences` (or a present-tense `## Testing` / `## Verification`)
5. Delete the planning sections (`## Plan` / migration steps / open questions)

The gate catches this through the skeleton: a `## Proposal` left in an implemented note fails.

---

## 4. Supersession: partial overlap vs full absorption

**Partial overlap**: the new plan covers only part of the old one → keep both, add relative links both ways, and update only the facts that still hold. Do not archive, and do not postpone.

**Full absorption** (the old plan is entirely retired):

1. **Audit what the old note holds**: its unique security boundaries, negative guarantees, failure scenarios, conditions for reintroduction and verification gaps.
2. **Move and absorb**: write them into the new note's `## Decision` / `## Alternatives considered` / `## Consequences`.
3. **One of two for the old note**:
   - **Delete**: the new note holds all of the old note's unique content, and inbound links are updated → delete it; it does not go into `archived/`.
   - **Archive**: the old note still has independent leverage (negative guarantees, ownership boundaries, conditions for reintroduction) → `archive-agent-note.ts`. Only the `Archived:` line is inserted; the links go in the new note.
4. **Fix inbound links**: point relative links that still target the old path at the new note (point at `archived/` only when citing the historical snapshot).

`--superseded-by` only checks that the new note exists, and appends a relative link to the archived path at the end of the new note.

---

## 5. Optional: a reverse comment at the source entry point

If the host already has the convention, a core entry point may carry one line:

```go
// Note: session persistence goes through a pool of file handles to avoid concurrent write conflicts — see .agents/notes/implemented/architecture/2026-08-27-handle-based-session-persistence.md
type SessionFileHandlePool struct {
	// ...
}
```

It is not a gate. `check-note-anchors` only produces a soft report and stays out of CI.
