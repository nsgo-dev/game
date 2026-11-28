# Agent Note: Editor agent provider configuration

Status: implemented

## Problem

The built-in editor agent had a hard-coded Anthropic endpoint and model pair. Local OpenAI-compatible servers and model upgrades could only be selected by changing Go code.

## Decision

`AgentSettings` stores a validated `builtInFormat`, `builtInEndpoint`, `builtInModel` and `builtInFastModel`. The editor resolves endpoint, protocol and route models from these person-authored settings, preserving existing defaults when fields are empty. Secrets remain outside YAML.

## Alternatives considered

Keeping endpoint and model constants was rejected because it blocks offline and gateway deployments. Storing API keys beside the endpoint was rejected because editor secrets already use the system keychain and project files must remain shareable.

## Consequences

Provider settings round-trip through editor YAML and model routes can use local IDs. The OpenAI request adapter and settings UI connection form remain follow-up work, while validation prevents unsupported protocol names from reaching a runner.
