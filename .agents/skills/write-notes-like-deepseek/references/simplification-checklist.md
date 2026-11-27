# Read on demand: simplification self-check

> Distilled from the `dsh-find-simplifications` skill of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). Scan it when wrapping up or refactoring; do not force unrelated simplifications into a feature change.

## Signals worth deleting, merging or downgrading

- A public method, event, configuration key, package, persisted event or test artifact has no production consumer (tests or docs are its only consumers, and the behavior is not critical).
- Two places represent the same fact (especially a persisted event and a transient event each keeping a copy).
- Every implementation of a capability must implement a method that no consumer uses.
- A package exists only for tests, demos or support, adding release and dependency cost.
- Generality "for later": multiple sessions, background task ledgers, mid-year pivots, tools with their own UI and so on, with no current owner.
- Invariants, rollbacks, snapshots or special cases prepared for an API that does not exist.
- Hand-written capabilities an established package or the language's standard library already provides, where switching deletes the implementation and its tests outright.

## How to confirm

- Use `rg` for the symbol, event, configuration key or package name and check whether production code really uses it (in this repository: the root package and the packages under `cmd/`, `internal/` and `pkg/`, the runtime of `examples`, and the starter in `template`).
- Separate production, non-production and ambiguous hits: occurrences in tests and docs are not production evidence. Tools such as `go vet`'s unused checks or `staticcheck` help but do not replace reading the call sites.
- Record small, certain cleanups as `TODO` / `FIXME` / `XXX`; do not open a note for them. Only a durable trade-off deserves a proposed note.
- Hand-rolled vs a dependency: look at the net deletion (implementation, tests and docs), the dependency's health, and how well its boundary fits. Switch only if it really deletes code; a wrapper of the same complexity is no win.

## How it connects to notes

- A signal matches and the trade-off should be kept → write a `proposed/simplification` note (Problem / Proposal / Alternatives / Acceptance / Risks).
- A landed simplification → `implemented/simplification`, whose Consequences state both the cost of what was deleted and the burden saved.
- When the note tree grows too large, use the "future reference value" criterion in `archiving.md` to decide what to archive, not word count or age.
