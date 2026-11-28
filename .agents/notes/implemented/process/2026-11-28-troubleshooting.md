# Agent Note: First-run troubleshooting documentation

Status: implemented

## Problem

The documentation explained file-loader errors but did not collect the setup, platform, release, browser-audio and log-location failures newcomers encounter before a game starts.

## Decision

Add mirrored English and Chinese troubleshooting pages with exact error fragments, causes and fixes. Link them from getting started and the documentation overview, and keep file-loader guidance in `errors.md`. Runtime guidance points to `gamedebug` and NSGo's running-and-debugging documentation.

## Alternatives considered

- Add isolated notes to each command page. That leaves users searching several pages for one first-run failure and does not provide a stable exact-message index.
- Rename `errors.md`. Its loader-focused title and API explanation remain useful, so the troubleshooting page links to it instead.

## Consequences

New users have one searchable path for setup and runtime failures in both languages. Exact messages may evolve with upstream NSGo tooling, so future command changes should update this page alongside the affected guide.
