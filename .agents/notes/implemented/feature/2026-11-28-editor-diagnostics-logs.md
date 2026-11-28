# Agent Note: Editor diagnostics logs

Status: implemented

## Problem

Editor child-process output disappeared with the process, leaving bug reports without reproducible logs and making accidental secret disclosure hard to audit.

## Decision

`pkg/editor/diaglog` writes synchronized JSON-line-compatible text streams with 64 KiB line caps, size rotation, bounded file counts, and redaction of paths, tokens, headers, private keys, secret environment variables, URL queries, and email addresses. `ExportDiagnostics` packages the rotated files and a versioned YAML manifest into a local zip. Help exposes the export command.

## Alternatives considered

Uploading diagnostics automatically would simplify support intake, but would violate the editor's local-data boundary and require consent and a service. Keeping logs only in memory avoids files but loses the evidence when the editor closes.

## Consequences

Diagnostics remain local and bounded. The current integration provides the export command and reusable logging primitives; child-process call sites can attach a stream without changing their panel output.
