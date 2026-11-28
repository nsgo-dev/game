# Agent Note: Scene rule conditions

Status: implemented

## Problem

Scene connections always ran their handler, so a scene file could not express a conditional rule without custom Go glue.

## Decision

`ConnectionDoc.When` stores an optional expression string next to the connection. Empty means unconditional, preserving existing files and connection ordering while leaving expression evaluation to the scene runtime.

## Alternatives considered

Adding a second rule file would split event wiring from its condition and complicate merge behavior. Keeping the condition on the connection preserves deterministic scene data and editor ownership.

## Consequences

Scene formats can carry conditions for future built-in handlers and expression evaluators. Existing runtimes that do not evaluate `When` continue to run unconditional connections as before.

