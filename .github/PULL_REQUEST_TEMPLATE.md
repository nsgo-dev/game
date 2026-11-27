## Summary

<!-- Briefly describe what this pull request changes and why. -->

## Related issue

Fixes #

## Verification

- [ ] `gofmt -l .` produces no output
- [ ] `go tool nsgo test ./...` passes from the repository root
- [ ] `go tool nsgo check -target macos,windows,linux,android,web` passes from the repository root
- [ ] `go tool nsgo check -target ios,ios_simulator` passes (if on macOS with Xcode)
- [ ] `bun run build` passes (for `docs/` or `website/` changes)
- [ ] Both `docs/en/` and `docs/zh/` updated (when applicable)

## Screenshots

<!-- Attach screenshots or a short recording for website, example, or visual changes. -->

## Acceptance for every editor PR

<!-- Complete this checklist for any PR touching `pkg/editor` (from `design/editor/Spec.dc.html`). -->

- [ ] Real screenshots from `nsgotest` on macOS, before and after, beside the matching artboard.
- [ ] Uses only the tokens in `pkg/editor/spec_theme.yaml` (`SpecPalette`), and every text pair is in the WCAG 2.2 contrast table.
- [ ] Every control shows the focus ring and is reachable with `Tab`, `F6`, or arrow keys.
- [ ] Every action has a shortcut row with a scope and a matching MCP tool (when applicable).
- [ ] Overlays follow the Spec line-style legend; nothing is told apart by hue alone.
- [ ] Reviewed by a person looking at the screenshots, not only by tests.
