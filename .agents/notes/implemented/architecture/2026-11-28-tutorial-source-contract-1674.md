# Agent Note: Tutorial source-file contract

Status: implemented

## Problem

Chapter 1 showed partial Go declarations without the constants and helpers needed to compile them. Later chapters used excerpts without naming the complete files, so a reader could not reconstruct a project from the pages alone.

## Decision

Chapter 1 now includes complete `game.go` and `game_test.go` blocks. Every later chapter names its changed files and links to the matching complete example directory. A repository documentation test checks the chapter 1 source contract and the bilingual overview wording.

## Alternatives considered

- Linking only to the examples would leave the first chapter's copy-and-paste path incomplete, so the complete source remains on the page.
- Duplicating every later chapter's full files would make the docs drift from the examples. Later chapters link to the canonical complete files instead.

## Consequences

The first chapter can be copied into an empty module and has the declarations its snippets use. The examples remain the canonical complete source for chapters 2 through 9, while the docs identify their file-level changes.
