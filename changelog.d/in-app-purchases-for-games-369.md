### Added

- `game/liveops`: `Purchases` sells in-app products over NSGo's purchase services, crediting consumables into the save exactly once through a `Ledger` saved with them (a kill before finishing redelivers without a second credit), keeping unlocks and subscriptions from the store's entitlements, restoring them, following Ask to Buy approvals and refunds, and handing transactions to an optional server to verify (#369).
