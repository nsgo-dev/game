# Changelog fragments

A pull request with a change users can see records it here, in a file of its own, instead of in `CHANGELOG.md`, so pull requests no longer conflict over the end of `Unreleased`. Name the file after the change in kebab case, such as the branch's name (`keep-keys-held-through-reset.md`), and put each bullet, with its reference, under the Keep a Changelog type it is:

```markdown
### Fixed

- `game/input`: keys held through `Reset` are released on every path (#24).
```

The types are `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed` and `Security`, and a fragment may hold several. `go run ./tools/changelog check` validates the fragments, and the Changelog workflow runs it on every pull request.

To release, run `go run ./tools/changelog release X.Y.Z`. It appends each fragment's bullets to the matching subsection of `Unreleased` in `CHANGELOG.md`, deletes the fragments, and turns `Unreleased` into the version's section, which the release workflow reads. Edit the section, merge it, and tag `vX.Y.Z`. `go run ./tools/changelog collect` folds the fragments without cutting a release.
