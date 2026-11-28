# Agent Note: anti-addiction age and spending rules

Status: implemented

## Problem

Games targeting mainland China had no shared implementation for minor play windows or age-band spending caps.

## Decision

Add a core-free `antiaddiction` package. `Window` evaluates the Friday-to-Sunday 20:00–21:00 Asia/Shanghai window with a server clock and holiday date set. `SpendLimit` and `SpendAllowed` enforce the statutory age bands and monthly totals.

## Consequences

Account and purchase services can apply deterministic rules without trusting a client clock. Real-name verification, NPPA request signing, holiday calendar file loading, and session kicking remain integration work.

## Alternatives considered

Using local time from a game client was rejected because it is trivially changed. Embedding a fixed holiday list was rejected because statutory calendars change and need server deployment updates.
