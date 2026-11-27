// Package mempool provides deterministic, allocation-free object pools for hot game loops: [Pool], [Arena] with its [Handle]s, and [SlicePool]. They reuse memory across frames and, unlike sync.Pool, never drop entries during garbage collection.
package mempool
