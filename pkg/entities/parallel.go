package entities

import (
	"runtime"
	"sync"
)

// ParOptions controls deterministic query partitioning. ChunkSize is fixed by
// the caller, so changing Workers never changes which entities share a chunk.
type ParOptions struct {
	Workers   int
	ChunkSize int
}

// ParEach2 visits entities carrying A and B in deterministic dense order. The
// callback must only write the two components it receives and must not change
// world structure during the call. Workers <= 1 or a web build run inline.
func ParEach2[A, B any](w *World, opt ParOptions, fn func(Entity, *A, *B)) {
	sa := existingStore[A](w)
	sb := existingStore[B](w)
	if sa == nil || sb == nil || len(sa.dense) == 0 || len(sb.dense) == 0 || fn == nil {
		return
	}
	chunk := opt.ChunkSize
	if chunk <= 0 {
		chunk = 64
	}
	workers := opt.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers <= 1 {
		parEach2Range(sa, sb, 0, len(sa.dense), fn)
		return
	}
	var wg sync.WaitGroup
	for start := 0; start < len(sa.dense); start += chunk {
		end := min(start+chunk, len(sa.dense))
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			parEach2Range(sa, sb, start, end, fn)
		}(start, end)
	}
	wg.Wait()
}

func parEach2Range[A, B any](sa *Store[A], sb *Store[B], start, end int, fn func(Entity, *A, *B)) {
	for i := start; i < end; i++ {
		e := sa.dense[i]
		if j, ok := sb.denseIndex(e); ok {
			fn(e, &sa.values[i], &sb.values[j])
		}
	}
}
