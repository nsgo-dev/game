# Agent Note: Shared ACP transport and permission boundary

Status: implemented

## Problem

Each local agent CLI currently has its own process protocol and parser. Adding Agent Client Protocol agents would otherwise duplicate JSON-RPC framing and risk allowing a new adapter to bypass the editor's review boundary.

## Decision

`pkg/editor/internal/acp` owns newline-delimited JSON-RPC framing over an `io.Reader` and `io.Writer`, with synchronous request/response calls that deliver interleaved notifications to a callback. Convenience methods cover ACP initialization, session creation or resume, model configuration, prompt and cancellation. Permission selection is centralized and fails closed. `allow_once` is accepted only for a tool whose name is scoped to the editor MCP server. Other requests require `reject_once`.

## Alternatives considered

Putting framing in each future agent adapter would keep adapters self-contained but repeats response matching and cancellation edge cases. Reusing a third-party ACP dependency was considered, but the repository needs a small transport surface and no dependency was required for JSON-RPC framing.

## Consequences

Future ACP runners can map protocol notifications to existing editor events without adding another transport implementation. The package does not launch processes or define agent rows yet, so native Claude and Codex behavior stays unchanged while the protocol boundary can be tested independently.

## Verification

Tests use a loopback stdio pipe to verify notification delivery before a matching response, and test the permission policy's editor-tool allow path and fail-closed rejection paths. `go tool nsgo test` and `go tool nsgo check` pass for the package.
