### Added

- `pkg/online/server`: `/metrics` gains per-match tick histograms (`Runner.Health` records every tick, or `Health.ObserveMatchTick` directly), per-match overrun and missed-tick counters (`Health.OnOverrun`), guard totals across every peer seen (`Guard.Totals` and `GuardCollector`), transport inbox drop and refusal counters (`InboxCollector`), and `Health.AddCollector` for a game's own series; an unnamed match is exported as `match_<index>` (#1331).
