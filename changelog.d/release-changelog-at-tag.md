### Fixed

- Releases: the release workflow fails at a tag whose `changelog.d` still holds fragments or whose `Unreleased` section is not empty, and a release whose notes exceed GitHub's limit now keeps its Deprecated, Removed, Changed and Security subsections whole and links to the full section in `CHANGELOG.md` instead of cutting the notes partway through a list (#1554).
