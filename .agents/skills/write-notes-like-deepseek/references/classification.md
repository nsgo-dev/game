# Read on demand: classes

> Expands SKILL §1. Consult it when choosing the class of a new note.

## A closed set (6; adding one means changing the check script)

| Class | Covers |
|---|---|
| `feature` | New capabilities facing users or the model |
| `bug-fix` | Fixing defects, or closing gaps a postmortem exposed |
| `simplification` | Removing code, behavior or surface without adding capability |
| `architecture` | Structural decisions about shipped source: how packages relate, what the runtime vocabulary is |
| `process` | Tools, policy and workflow outside the code: gates, package management, vendoring and so on |
| `testing` | Test infrastructure and strategy |

Tips for choosing:

- `architecture` vs `process`: the first is "what the shipped source looks like", the second is "the toolchain and workflow around the source".
- There is no `refactor` class; use the `simplification` criterion "did observable behavior change?": unchanged behavior is simplification, changed behavior goes to feature, bug-fix or architecture.
- `feature` is not only for "new capability": **a non-obvious behavior or product choice that users or the model can see, too small to be architecture, is also `feature`**. The test is "can users or downstream code observe the behavior", not "how much code changed". Only local implementation details, ordinary refactors and dependency patches that keep behavior carry no note at all.
- When unsure, think about who will search for the note later: capability history is `feature`, structural decisions are `architecture`, gates and releases are `process`.

Semantic requirements for every note (which the check script cannot enforce):

- **Alternatives record only rivals really considered**: state each one's strongest argument before rejecting it; a rejection that only names weaknesses is a straw man. "Do nothing / reuse" goes in only if it was really weighed at the time; do not invent it.
- **A simplification states its cost ceiling**: the `## Consequences` of a `simplification` says what the known limit of the trade-off is and which signal means it should be revisited. A simplification without an upgrade trigger quietly becomes permanent.
- **Verification is confirmable**: when saying something "works", point at a surface that can be checked (which path, what magnitude, which command confirms it). Without a baseline, do not use relative words; "improved" or "faster" without a baseline is an unverified claim, worse than saying nothing.

## The check script

`scripts/agent-note-tree.ts` defines the `AGENT_NOTE_CLASSES` constant; an unknown class folder or a stray `.md` directly under a lifecycle folder is an error. Adding a class means changing the constant and this document together, or the check fails.

The closed set of classes and the criteria follow the notes classification practice of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness).
