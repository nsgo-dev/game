# Read on demand: the note file format

> Expands SKILL §2. Consult it when writing or changing a note; skip it when only code changes.

## Head block (the first three lines, checked by the script)

```markdown
# Agent Note: <title>

Status: <status>
```

- `proposed` → `Status: proposed`
- `implemented` → `Status: implemented`
- `rejected` → `Status: rejected — <one-sentence reason>`

The title must carry the `Agent Note: ` prefix; the status holds no date or parentheses and must match the lifecycle folder (the script cross-checks them). The filename date is the day the note was first proposed; git carries the rest of the timeline.

## Body skeleton

Every note starts with `## Problem`: the motivation must stand on its own without the solution. After that it depends on the lifecycle:

**`proposed/`**

```markdown
## Problem
## Proposal
…free sections…
## Alternatives considered
## Acceptance criteria
## Risks
```

`Proposal` may use the future tense; `Acceptance criteria` says which observable state counts as done; `Risks` covers both risks and known trade-offs.

**`implemented/`**

```markdown
## Problem
## Decision
…free sections…
## Alternatives considered
## Consequences
```

`Decision` describes landed facts in the present tense; the gate only rejects proposal headings (`## Proposal` / `## Plan` / `## Migration plan` / `## Acceptance criteria`); `## Consequences` records both costs and benefits; add present-tense fact sections such as `## Testing` / `## Verification` as needed. The present tense is kept by the writer, not by a lexical scan.

**`rejected/`**

The frozen proposal form, with the verdict on the `Status:` line; the `Alternatives` and other sections from the proposal stay as they were, never rewritten into their opposite.

## Alternatives considered (mandatory)

Every note contains `## Alternatives considered`: for each alternative **really considered**, why it was not chosen, one paragraph per alternative (a `### Why not <X>?` subsection works too). Do not invent options nobody weighed. "Do nothing / keep what exists" goes in only if it was really weighed at the time. The script only checks that the section exists.

## Tense and no rewriting

- `proposed` may use the future tense; `implemented` is always in the present tense, describing landed facts.
- A note is never rewritten into a different decision; when it is superseded, a new note takes over, and the old one is handled by the archiving or merging rules.

## Headings the script also accepts

Notes here are written in English, and the headings above are the canonical ones. For compatibility with notes written by Chinese-speaking contributors or imported from elsewhere, the format check also accepts a few Chinese spellings: a fullwidth colon in the title (`# Agent Note：title`, which Chinese input methods often type), `状态：` status lines, and Chinese aliases of the required sections (such as `## 问题` for `## Problem` and `## 备选方案` for `## Alternatives considered`; the full list is in `scripts/verify-agent-note-format.ts`). The same aliases count as proposal headings banned in `implemented`. New notes use the English headings.

## Free sections and style

- Free sections (package topology, wire contracts, schemas and so on) sit between `Decision` and `Alternatives`.
- Keep searchable mechanism names and `must` / `may` / `never` emphasis on ordering; explain each fact fully in one place and link to it from elsewhere.
- Reference other notes with relative Markdown links such as `[topic](../../implemented/architecture/2026-…-….md)`, never bare numbers.

The format and the alias table follow the notes conventions of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). This project's notes are English only, with no bilingual variants.
