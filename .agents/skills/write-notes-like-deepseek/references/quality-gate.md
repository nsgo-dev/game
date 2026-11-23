# Read on demand: semantic self-check (after writing a note)

> Scripts check structure; this list checks meaning. Go through it after writing, and report to the user only the gaps and what is done well; do not recite every item.
> Scope: this is a self-check for the agent, not rules for a script. Every item below is a semantic judgment and never goes into the verify scripts.

## Before acting: look it up yourself, then ask the user

- [ ] Is this "question" really a decision? Facts the §3 search methods (entry-point comments if any / the class tree / rg / module docs) can answer are not for the user. Finding facts is your job; only trade-offs and calls take the user's time.

## Problem

- [ ] The motivation stands on its own without the solution: delete Decision and reread; is it still the same problem?
- [ ] The trigger is clear: what broke, what must change, what happens if nothing is done.

## Decision / Proposal

- [ ] Concrete enough to act on: not "use a better database" but "use SQLite for session storage".
- [ ] The reasoning is forced by a "vs": when writing "we use X", add "X vs Y, and why X".

## Alternatives considered

- [ ] At least 2 real alternatives; do not invent options nobody weighed. "Do nothing / reuse" counts only if it was really considered at the time; it is not mandatory.
- [ ] Each rejected option states its strongest argument first, then why it was rejected; a rejection that only names weaknesses is a straw man.
- [ ] The reason for rejection points at concrete driving conditions, not "experience says it does not work".

## Consequences

- [ ] Both costs and benefits. Ask "what got harder? What does it cost to maintain?"; consequences with only benefits have been cherry-picked.
- [ ] A simplification states its known limit and when to revisit it. A simplification without an upgrade trigger quietly becomes permanent.
- [ ] No relative words without a baseline: when "improved / faster" has no clear baseline, reduce it to a statement of fact.

## Verification / Testing (implemented)

- [ ] Points at a confirmable surface: which path, what magnitude, which command.
- [ ] No "seems to work": acceptance criteria are either checkable or explicitly left for later.

## How to report

After the self-check, report like this (one line per gap; close out if there are none):

```markdown
**Semantic self-check: <filename>**
✅ Solid: motivation stands alone / 2 real alternatives / consequences cover costs and benefits
⚠️ Gaps:
- Alternatives — there is a "do it vs do nothing" straw man; add a real option parallel to the chosen one
- Consequences — "better performance" needs a path and a magnitude
Suggestion: fix these 2 points before landing / gaps acceptable, ship as is
```

If the user accepts the gaps, it ships; if not, fix them and then land the note. Judgments of meaning are not a script; do not grow this into another gate.

## Is the self-check working (observable signals)

Look back now and then; these signals mean the self-check is working, and their absence means it is idling:

- Your questions to the user **no longer include** facts that a look at the note tree or an rg would answer.
- Every gap in a report comes with a **concrete fix** (which section, what to add), not "could be improved".
- Paths rejected in `rejected/` and in Alternatives **are no longer proposed again by new sessions**; this is the reason the whole system exists.
