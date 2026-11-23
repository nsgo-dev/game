# Agent notes

This directory holds decision notes that outlive a single session, kept as the write-notes-like-deepseek skill describes. The method, the format, and which changes must carry a note are in [../skills/write-notes-like-deepseek/SKILL.md](../skills/write-notes-like-deepseek/SKILL.md).

- The path is the status: `{proposed|implemented|rejected|archived}/{feature|bug-fix|simplification|architecture|process|testing}/yyyy-mm-dd-topic.md`. Create a directory when its first note needs it.
- Notes are written in English.
- There is no INDEX.md: a note's place in the tree is its status, so search this directory with ripgrep.
- Verify from the repository root with `bun run verify-notes`; the Notes workflow runs the same checks on every pull request. When a decision is superseded, archive and seal the old note with `bun run archive-agent-note <path>`.
