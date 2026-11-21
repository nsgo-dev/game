## Summary

<!-- Briefly describe what this pull request changes and why. -->

## Related issue

Fixes #

## Verification

- [ ] `gofmt -l .` produces no output
- [ ] `go tool nsgo test ./...` passes in `packages/game`
- [ ] `go tool nsgo check -target macos,windows,linux,android,web` passes in `packages/game`
- [ ] `go tool nsgo check -target ios,ios_simulator` passes (if on macOS with Xcode)
- [ ] `bun run build` passes (for `docs/` or `apps/website/` changes)
- [ ] Both `docs/en/` and `docs/zh/` updated (when applicable)

## Screenshots

<!-- Attach screenshots or a short recording for website, example, or visual changes. -->
