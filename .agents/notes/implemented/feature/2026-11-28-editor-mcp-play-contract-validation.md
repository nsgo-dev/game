# Agent Note: Editor MCP Play contract validation

Status: implemented

## Problem

The editor MCP Play tools had exported wire types and step/input validators, but optional replay paths were not validated before future handlers dispatch them.

## Decision

`ValidatePlayStart` requires replay files to be canonical project-relative `.replay.yaml` paths and rejects traversal, absolute paths, backslash aliases, and other extensions. The existing step and input validators remain the single boundary for their arguments.

## Alternatives considered

Deferring path validation to the eventual handler would duplicate checks across handlers and risk one path bypass. Accepting arbitrary paths would let a tool request escape the project replay directory.

## Consequences

Play start requests have a deterministic, testable path contract before live Play handlers are wired. Starting and stepping the editor's actual Play session remains follow-up work.
