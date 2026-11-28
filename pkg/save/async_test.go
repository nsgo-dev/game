package save_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func settleSaveAsync(t *testing.T, app *nsgotest.App, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case <-done:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for save callback")
		}
		app.Advance(time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

func receiveSaveError(t *testing.T, app *nsgotest.App, ch <-chan error) error {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case err := <-ch:
			return err
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for save error callback")
		}
		app.Advance(time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

func TestAsyncSaveLoadIsOrderedAndCallbacksArePosted(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		return core.View{}
	})
	store := save.NewFileStore(t.TempDir())
	var mu sync.Mutex
	var callbacks []error
	done := make(chan struct{})
	callback := func(err error) {
		mu.Lock()
		callbacks = append(callbacks, err)
		if len(callbacks) == 2 {
			close(done)
		}
		mu.Unlock()
	}
	store.SaveAsync(cx, context.Background(), "main", playerState{Name: "first", Level: 1}, callback)
	store.SaveAsync(cx, context.Background(), "main", playerState{Name: "second", Level: 2}, callback)
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	select {
	case <-done:
		t.Fatal("SaveAsync callback ran before the engine advanced")
	default:
	}
	settleSaveAsync(t, app, done)
	mu.Lock()
	if len(callbacks) != 2 || callbacks[0] != nil || callbacks[1] != nil {
		t.Fatalf("callbacks = %v", callbacks)
	}
	mu.Unlock()

	var got playerState
	loaded := make(chan struct{})
	store.LoadAsync(cx, context.Background(), "main", &got, func(err error) {
		if err != nil {
			t.Errorf("LoadAsync: %v", err)
		}
		close(loaded)
	})
	settleSaveAsync(t, app, loaded)
	if got.Name != "second" || got.Level != 2 {
		t.Fatalf("LoadAsync = %+v, want second save", got)
	}
}

func TestAsyncSaveCapturesValueBeforeCallerMutation(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		return core.View{}
	})
	store := save.NewFileStore(t.TempDir())
	value := &playerState{Name: "before", Level: 1}
	done := make(chan struct{})
	store.SaveAsync(cx, context.Background(), "main", value, func(err error) {
		if err != nil {
			t.Errorf("SaveAsync: %v", err)
		}
		close(done)
	})
	value.Name = "after"
	value.Level = 2
	settleSaveAsync(t, app, done)
	var got playerState
	if err := store.Load("main", &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "before" || got.Level != 1 {
		t.Fatalf("captured save = %+v, want before mutation", got)
	}
}

func TestAsyncSaveRejectsEngineStorageAndCancellation(t *testing.T) {
	var cx *core.Scope
	app := nsgotest.Start(t, func(scope *core.Scope) core.Node {
		cx = scope
		return core.View{}
	})
	store := save.NewStorageStore(app.Storage(), "save")
	done := make(chan error, 1)
	store.SaveAsync(cx, context.Background(), "main", playerState{Name: "nope"}, func(err error) { done <- err })
	if err := receiveSaveError(t, app, done); !errors.Is(err, save.ErrAsyncUnavailable) {
		t.Fatalf("storage SaveAsync error = %v, want ErrAsyncUnavailable", err)
	}

	fileStore := save.NewFileStore(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelDone := make(chan error, 1)
	fileStore.SaveAsync(cx, ctx, "cancelled", playerState{Name: "nope"}, func(err error) { cancelDone <- err })
	if err := receiveSaveError(t, app, cancelDone); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled SaveAsync error = %v, want context.Canceled", err)
	}
	if fileStore.Exists("cancelled") {
		t.Fatal("cancelled SaveAsync wrote a slot")
	}
}
