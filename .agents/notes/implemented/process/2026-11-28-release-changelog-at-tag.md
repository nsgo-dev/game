# Agent Note: The release workflow verifies the changelog at the tag and shortens long notes by whole subsections

Status: implemented

## Problem

The v0.1.0 tag shipped with two fragments still in `changelog.d` (`root-module-pkg-layout.md`, `nsgo-game-extension-binary.md`), because `chore(release): prepare v0.1.0` cut the section and a later merge brought the fragments back before the tag was pushed. Nothing in `release.yml` looked at `changelog.d` or `Unreleased` at the tagged commit, so the release notes missed two shipped changes and the next release would announce them again (#1554).

The same release's section is about 244,000 characters. `release.yml` cut `release-notes.md` and the combined GitHub body at 120,000 characters on a line boundary, so the published notes stopped partway through `### Added` and nothing under Changed, Removed, Fixed or Security reached the release page, although those are the subsections an upgrading user needs.

## Decision

- `tools/changelog verify-release X.Y.Z` fails, listing every reason at once, when `changelog.d` holds any fragment besides `README.md`, when `## Unreleased` has any line that is not blank or a heading, or when CHANGELOG.md has no non-empty section for the version. It accepts `vX.Y.Z` as the tag names it.
- The `meta` job of `release.yml` runs `go run ./tools/changelog check` and `verify-release "$TAG"` on a tag push (`mode == release`), before anything is built, so a tag with unreleased notes fails at once. A `workflow_dispatch` upload onto an existing tag skips it, since that tag's tree is already published.
- `tools/changelog notes -max N -link URL X.Y.Z` replaces the awk extraction. It prints the version's section as before (heading excluded, leading and trailing blank lines dropped, ending at the next `## ` or the link reference definitions). When the section is longer than `-max` characters, it keeps the lines before the first subsection and as many whole subsections as fit, in the order Deprecated, Removed, Changed, Security, Fixed, Added, then any other name; each subsection that does not fit keeps its `###` heading and one line counting its entries and linking to the section's anchor in CHANGELOG.md at the tag. A footer says the notes are shortened and links to the same anchor. Subsections stay in their original order, and no list is ever cut partway.
- The `meta` job passes `-max 120000`, leaving room for the downloads table (about 4,000 characters) that the `release` job prepends. The `release` job no longer truncates: it fails if the combined body is over GitHub's 125,000-character limit, which only a much larger downloads table could cause.
- The link's anchor follows GitHub's heading slugs: lower case, spaces become hyphens, and every character but letters, digits, `-` and `_` is dropped, so `## [0.1.0] - 2026-11-27` links to `#010---2026-11-27`.

## Alternatives considered

- **Keep the line-boundary truncation and only move the footer**: the smallest change, and the body stays as long as possible. Rejected because it keeps the order of the file, so a long Added list still pushes Changed and Removed off the page, which is the failure #1554 reports.
- **Publish only a link to CHANGELOG.md when the section is too long**: simplest and never misleading. Rejected because the subsections an upgrading user needs (Removed, Changed, Deprecated, Security) fit easily and belong on the release page; only Added is long.
- **Fill the remaining room with as many Added bullets as fit**: the issue's proposal, which shows more of Added. Rejected because a list cut at an arbitrary bullet reads as complete; a count and a link make clear what is left out.
- **Check the fragments in a pull request job instead of at the tag**: catches the problem earlier. Rejected as the only check because the v0.1.0 fragments arrived by a merge after the release pull request, and only the tagged commit is what ships; a pull request check could be added later on top of this one.

## Consequences

- **Benefits**: a tag cannot ship notes that leave out its own fragments or Unreleased lines; the release page always carries the upgrade-relevant subsections whole; the extraction and shortening are unit-tested in `tools/changelog/main_test.go`, including a test that the repository's 0.1.0 section fits and keeps Changed, Removed and Security.
- **Costs**: the `meta` job sets up Go on releases and uploads, adding a few seconds. The anchor relies on GitHub's slug rules; if they change, the shortened notes' links land at the top of CHANGELOG.md instead of the section, which still holds the full notes. `release-notes.md` also feeds the editor's in-app update notes, which now get the same shortened text when a section is over 120,000 characters.
- **Not covered here**: the two fragments the v0.1.0 tag left behind and the published v0.1.0 notes are repaired separately; this change only stops the next tag from repeating it.
