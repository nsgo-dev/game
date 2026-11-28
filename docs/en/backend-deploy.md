# Deploying backend services

Use Postgres for production accounts, cloud saves and self-hosted leaderboards. SQLite is suitable for development, tests and a single node with a durable volume. Memory stores are test-only.

Run `accountsd`, `cloudsaved` and `leaderboardd` behind one TLS-terminating proxy with rate limits and health checks. Give each daemon an explicit address and share the account service key set from `accountsd genkey` through a protected secret store.

Back up Postgres with managed snapshots or `pg_dump` and rehearse restores. Run schema migrations before rolling instances. Refuse newer schema versions and roll forward instead of starting an older binary against a newer database.

The file-backed leaderboard keeps boards in memory and is intended for a single process until a shared SQL store is configured. SQLite files require a durable volume and WAL files must be backed up together.
