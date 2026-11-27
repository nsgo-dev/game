// Package cloudsave is a self-hostable cloud save service: per-account save slots over HTTPS, kept in versions so that concurrent writes from two devices are refused rather than silently overwritten, slots over a size limit are rejected, and the last few versions of each slot can be listed and restored.
//
// # Accounts
//
// The service does not keep accounts. Each request carries a bearer token from the account service (package accounts), which the service verifies offline with an authtoken.Verifier; the token's subject is the account ID its slots are filed under. A player signed in to the same account on the web and on a phone reads and writes the same slots.
//
// # Versions and conflicts
//
// Every write or delete adds a version to the slot, numbered from 1, and the service keeps the newest Config.KeepVersions of them; a delete is a version that marks the slot deleted, so a deleted slot can still be restored. A write may name the payload it expects to replace by its hash ("If-Match") or require that the slot does not exist ("If-None-Match: *"); when the slot holds anything else the write fails with ErrChanged (HTTP 412) and changes nothing. liveops.CloudClient makes every write of a liveops.CloudSyncer conditional this way, so a write that loses a race surfaces as a conflict the syncer's policy resolves.
//
// # Limits
//
// A payload over Config.MaxSlotBytes is refused with ErrSlotTooLarge (HTTP 413) and a new slot beyond Config.MaxSlots live slots with ErrTooManySlots (HTTP 409). A deleted slot does not count toward MaxSlots; beyond MaxSlots deleted slots per account, the oldest are purged with their history.
//
// # Storage
//
// The service keeps its records in a Store: NewMemoryStore for tests and prototypes, or the SQL store of package sqlstore, opened on SQLite by package sqlitestore or on Postgres by package pgstore. Package storetest checks any Store against the contract.
//
// # Configuration
//
// Config is a versioned YAML file (ParseConfig, Config.Marshal); Service.Handler serves the service over HTTP, and command cloudsaved runs it as a standalone server.
package cloudsave
