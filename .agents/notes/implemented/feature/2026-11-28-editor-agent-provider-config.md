# Agent Note: Editor agent provider configuration

Status: implemented

## Problem

The built-in editor agent had a hard-coded Anthropic endpoint and model pair (`DefaultBuiltInAgentEndpoint`, `DefaultBuiltInModel`, `DefaultBuiltInFastModel` in `pkg/editor/agent_builtin.go`). `Editor.BuiltInAgentEndpoint` could point elsewhere, but only from Go code, so gateways, local servers and model upgrades needed a new editor build. Issue #1747.

## Decision

`AgentSettings` in `editor.yaml` gains four person-only fields: `builtInFormat`, `builtInEndpoint`, `builtInModel` and `builtInFastModel` (`pkg/editor/agent_settings.go`). They are flat keys under `agent` beside the existing `builtInProvider`, not the nested `agent.builtIn` block the issue sketched, so the existing field joins them without a migration. `validateAgentSettings` refuses a format other than `anthropic` or `openai`, control characters, and an endpoint that fails `validateAndResolveBuiltInEndpoint` (https, or http to a loopback host), so a bad value fails when `editor.yaml` loads rather than at the first request.

Resolution happens on the editor goroutine: `AgentBuiltInEndpointForSettings` returns the settings endpoint, then `Editor.BuiltInAgentEndpoint`, then the default; `AgentBuiltInModelForRoute` returns the settings model for the route, then the default. `newBuiltInAgentRunner` and the Start Screen key check (`defaultValidateAPIKey`) both use them, so a pasted key is tested against the same server and fast model the runner will call. The person's file wins over the embedder's Go field because the setting is the person's explicit choice for their machine.

The runner speaks only Anthropic Messages today. `openai` is accepted in the file so the format is stable once the Chat Completions adapter lands, but `newBuiltInAgentRunner` returns `ErrBuiltInFormatUnsupported` for it, which `SubmitAgentTaskPrompt` and Start with AI surface, rather than silently sending Anthropic requests to an OpenAI-compatible server. The fields are You-only because `ApplySettings` already refuses agent-actor changes to `EditorSettings`. Keys stay in the keychain under the single `AgentAPIKeySecretAccount`.

## Alternatives considered

- **Keep the endpoint and model constants**: no new format to maintain, but it blocks offline and gateway use and ages the model IDs with every release, which is the issue's main complaint.
- **Store API keys beside the endpoint**: one place to configure a connection, but editor secrets already live in the system keychain and `editor.yaml` must never hold credentials.
- **Reject `openai` until the adapter exists**: avoids a value that does nothing yet, but it would change which files load once the adapter lands; refusing at request time with a named error keeps the file format fixed and the failure explicit.

## Consequences

- A person can point the built-in agent at an Anthropic-compatible gateway or a loopback server and choose model IDs without a new build, and the settings round-trip through `editor.yaml` unchanged.
- Not delivered yet (remaining #1747 work): the OpenAI-compatible request adapter shared with `tools/agenteval`, a per-endpoint keychain account, the Settings › Models and connections form with Test connection and a Local model preset, the "Local" status chip and per-model capability flags. Route labels still read "Anthropic API · default/fast" whatever the endpoint.
- Tests in `pkg/editor/agent_provider_settings_test.go` cover the round trip, invalid values, the You-only refusal, the runner calling the configured endpoint with the configured fast model ahead of `Editor.BuiltInAgentEndpoint`, and the `openai` refusal sending no request.
