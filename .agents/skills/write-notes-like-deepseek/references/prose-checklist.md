# Read on demand: note prose and leak self-check

> Distilled from the portable parts of two skills in the [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) repository, `dsh-prose-standard` and `dsh-trim-cot-leakage`. Check against this list when writing or changing a note; skip it when only code changes.

## Principles

- Write only what the code cannot say: behavior, failures, ordering, ownership, consequences and trade-offs. Do not retell the code or the reasoning that led to it.
- An invariant that can be encoded in types, visibility or API errors should not live only in a note.
- Write plainly: name who does what, under which condition, with what result. Avoid abstract words such as "contract", "boundary" or "shape" unless they mean that technical thing.
- Explain each fact fully in one place and link to it from elsewhere; do not copy the same rule everywhere.
- After writing, ask: does this paragraph stand on its own without the implementation? Can a reader today verify it without the original chat?

## Before writing (from prose-standard)

- Every paragraph keeps the whole proposition: subject and action, condition and ordering, must / may / never, negative guarantees and exceptions, ownership, side effects, failures and consequences. None may be missing.
- `implemented` uses the present tense to describe landed facts; drop the planning voice and checklists.
- Keep searchable mechanism names and the key must / may / never, ordering and negative emphasis; use decorative bold and exclamations sparingly.
- When you learn a rule during a change, apply it to every similar paragraph in that change, not just one.

## By location

- **Decision / Consequences**: are distinctions, errors and refusals, side effects, ownership, ordering, cancellation and persistence all clear?
- **Free sections** (topology, contracts, schemas): explain responsibilities, dependencies and non-obvious choices, and what they point to.
- **Alternatives / Consequences**: state both costs and benefits; name the capability given up and the conditions for bringing it back.
- **Testing / Verification**: explain only "why it is verified this way" (why the fixture, assertion or real entry point is needed); no walkthroughs.

## Leak self-check (from trim-cot-leakage)

For every suspicious passage, ask: **is it visible only in the original session, PR or draft? Can a reader at HEAD verify it alone?** If not, restate it from the repository's point of view and remove the session traces. Resolvable is not the same as safe: verifiable only means it passes the leak check, and on current-state surfaces such as READMEs and docs, a resolvable change narrative is still a change narrative, handled as category 3.

Common leaks (fix on sight):

1. Dead references: `(decision 7)` / `(audit C2)` / `§N` / `plan §1.4`. Replace with a named path to a committed note or document; if there is no owner, drop the reference and restate the fact.
2. Stack or PR perspective: `a later PR` / `this PR adds` / `the previous commit`. Replace with the landed mechanism or extension point; for work not done, use a `TODO` or issue reference, or collect it in a `## Deferred` section.
3. Change narrative: `used to` / `no longer` / `the old X` / `this cut` / `now`. Use the present tense; express a regression as a present-tense counterfactual, "without X, Y happens".
4. Review choreography: `rejected in review` / `the reviewer confirmed` / `v5`. Keep only the decision and its reason; drop who said it and when.
5. Self-justification: `this conversion is safe because…`. Replace with the invariant that makes it safe, or delete it (the code already says it).
6. Process retelling: `first X, then Y` / test walkthroughs. Delete; keep only non-obvious contracts.
7. Vagueness and placeholders: `should be enough` / `probably fine`. Turn into a `TODO` / `FIXME` or an explicit bound.
8. Language mixing: fragments of working language in another language mixed into the prose. Translate or delete them.

**Not leaks (keep):** issue references `#1470` / `TODO(name):` (a bare `TODO` needs care in the body scan, see the exemption above), merged PR references inside agent notes and postmortems, suppression reasons, present-tense counterfactuals, bounds with measured values, old/new runtime states, and `§` numbers of committed documents.

## Overcorrection traps (from trim-cot-leakage)

Before deleting, list every proposition in the paragraph, then check against the four ways of deleting too much:

- Turning a must or must-not obligation into a "you may do this" endorsement: the constraint becomes optional.
- Turning a "planned / considered" assumption into a landed fact: proposed passes for implemented.
- Deleting a true fact only because it reads like process narration.
- Dropping load-bearing provenance: in `measured: 512 levels of nesting ≈ 0.15s`, drop `measured`, the source note or the numbering, and the conclusion loses its footing.
