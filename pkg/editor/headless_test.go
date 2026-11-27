package editor_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #1547: RunHeadless, Post, and PostOrRunHeadless share a single headless owner so concurrent
// callbacks and direct headless mutations serialize without races, reentrant Post never deadlocks,
// and PostOrRunHeadless honors context cancellation while waiting for ownership.
func TestHeadlessExecutorSerializesAndSupportsReentrantPostAndCancel(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ed.RunHeadless(func() { _ = ed.Close() })
	})

	var active atomic.Int32
	var overlap atomic.Bool
	var ran atomic.Int32
	checkSingleOwner := func() {
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		defer active.Add(-1)
		_ = ed.Revision()
		ed.SetInspectorFilter("tick")
		ran.Add(1)
	}

	// Reentrant Post from inside RunHeadless must enqueue without deadlocking and run after RunHeadless releases.
	reentrantDone := make(chan struct{})
	var order []string
	var orderMu sync.Mutex
	ed.RunHeadless(func() {
		orderMu.Lock()
		order = append(order, "outer-start")
		orderMu.Unlock()
		ed.Post(func() {
			checkSingleOwner()
			orderMu.Lock()
			order = append(order, "reentrant-post")
			orderMu.Unlock()
			close(reentrantDone)
		})
		checkSingleOwner()
		orderMu.Lock()
		order = append(order, "outer-end")
		orderMu.Unlock()
	})
	select {
	case <-reentrantDone:
	case <-time.After(3 * time.Second):
		t.Fatal("reentrant Post inside RunHeadless did not drain")
	}
	orderMu.Lock()
	if len(order) != 3 || order[0] != "outer-start" || order[1] != "outer-end" || order[2] != "reentrant-post" {
		t.Fatalf("reentrant order = %v, want [outer-start outer-end reentrant-post]", order)
	}
	orderMu.Unlock()

	// PostOrRunHeadless honors context cancellation while waiting for an active RunHeadless call.
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ed.RunHeadless(func() {
			close(entered)
			<-release
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledRan := false
	if err := ed.PostOrRunHeadless(ctx, func() { canceledRan = true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("PostOrRunHeadless with canceled ctx = %v, want context.Canceled", err)
	}
	if canceledRan {
		t.Fatal("PostOrRunHeadless ran callback despite canceled ctx")
	}
	close(release)
	wg.Wait()

	// Stress concurrent RunHeadless, Post, PostOrRunHeadless, and Subscribe/notify under -race.
	const workers = 6
	const iters = 25
	var postDone sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				switch (id + i) % 3 {
				case 0:
					ed.RunHeadless(func() {
						unsub := ed.Subscribe(func() {})
						checkSingleOwner()
						unsub()
					})
				case 1:
					postDone.Add(1)
					ed.Post(func() {
						defer postDone.Done()
						checkSingleOwner()
					})
				case 2:
					if err := ed.PostOrRunHeadless(context.Background(), checkSingleOwner); err != nil {
						t.Errorf("PostOrRunHeadless: %v", err)
					}
				}
			}
		}(w)
	}
	wg.Wait()
	postDone.Wait()

	if overlap.Load() {
		t.Fatal("detected concurrent execution across headless owner")
	}
	if got, want := ran.Load(), int32(2+workers*iters); got != want {
		t.Fatalf("ran = %d, want %d", got, want)
	}
}

// #1547: AttachScope drains in-flight headless work and forwards queued headless callbacks to cx.Post
// in FIFO order before EditorView subscribes signals.
func TestAttachScopeDrainsInFlightAndForwardsQueuedHeadlessWork(t *testing.T) {
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ed.RunHeadless(func() { _ = ed.Close() })
	})

	inFlightStarted := make(chan struct{})
	releaseInFlight := make(chan struct{})
	var inFlightFinished atomic.Bool
	queuedOrder := make([]int, 0, 2)
	var mu sync.Mutex
	queuedDone := make(chan struct{})

	// Start an in-flight headless callback and queue two follow-ups behind it.
	ed.Post(func() {
		close(inFlightStarted)
		<-releaseInFlight
		ed.SetInspectorFilter("in-flight-done")
		inFlightFinished.Store(true)
	})
	<-inFlightStarted
	ed.Post(func() {
		mu.Lock()
		queuedOrder = append(queuedOrder, 1)
		mu.Unlock()
	})
	ed.Post(func() {
		mu.Lock()
		queuedOrder = append(queuedOrder, 2)
		mu.Unlock()
		close(queuedDone)
	})

	// Release the in-flight callback shortly after EditorView begins AttachScope.
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(releaseInFlight)
	}()

	var sawFinishedOnMount bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		node := editor.EditorView(cx, ed)
		sawFinishedOnMount = inFlightFinished.Load()
		return node
	}, nsgotest.Size(800, 600), focusDesktopEnv)
	app.Settle()

	if !sawFinishedOnMount {
		t.Fatal("AttachScope returned before in-flight headless callback finished")
	}
	select {
	case <-queuedDone:
	case <-time.After(3 * time.Second):
		t.Fatal("queued headless callbacks were not forwarded to cx.Post")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queuedOrder) != 2 || queuedOrder[0] != 1 || queuedOrder[1] != 2 {
		t.Fatalf("forwarded callback order = %v, want [1 2]", queuedOrder)
	}
}
