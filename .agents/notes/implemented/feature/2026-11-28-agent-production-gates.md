# Agent Note: Agent production gates and idle bot

Status: implemented

## Problem

The agent skill described a deterministic checking loop without severity tiers, a fix-round stop rule, unchecked-result reporting, or a shared no-input playtest bot.

## Decision

The skill adds a gates reference defining Fast and Full tracks, P0/P1/P2 severities, a three-round P1 cap, anti-reward-hacking rules, and checked versus unchecked report sections. `pkg/tooling/playtest` registers `ProfileIdle`, whose decisions are always empty and observation-independent.

## Alternatives considered

Keeping idle tests in each template would duplicate a trivial no-input policy and produce inconsistent names. A new bot interface was unnecessary because the existing `Bot.Decide` contract already supports empty `InputState` values.

## Consequences

Templates and agents can declare an idle baseline without game-specific bot code. The skill now gives agents a shared stop and reporting policy, while the full reference/degraded runner integration remains with the baseline work.
