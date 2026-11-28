# Agent Note: Tutorial chapter source completeness

Status: implemented

## Problem

Chapter 1 showed snippets that referenced hidden constants and helpers, so copying the page into a new module failed to compile. Later chapters also used excerpts without stating the boundary between tutorial prose and complete source.

## Decision

Chapter 1 now includes complete English source blocks for `game.go` and `game_test.go`, while the Chinese page points to the synchronized repository example. The tutorial overview states that snippets are excerpts unless a chapter labels complete source files and every chapter directory remains the complete runnable reference. A repository test checks the required Chapter 1 declarations and bilingual links.

## Alternatives considered

- Tell readers to copy the example directory from the start. That avoids missing declarations but leaves the own-module learning path broken and hides the source introduced by the chapter.
- Repeat complete files in every language. That duplicates large Go blocks and risks divergence, so the Chinese page links to the same complete example while retaining translated instructions.

## Consequences

The first chapter is copyable from the English page and CI detects drift in its declarations. Readers following later chapters must use each linked example directory when an excerpt omits unchanged code.
