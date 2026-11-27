### Fixed

- `pkg/editor`: detect, label, continue, and abort scene conflicts left by `git rebase`, `git pull --rebase`, `git cherry-pick`, `git revert`, and `git stash pop` in the scene conflict resolver, swapping stages during rebase so the author's replayed commit appears as Mine (#1531).
