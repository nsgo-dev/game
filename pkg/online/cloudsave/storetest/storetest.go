// Package storetest checks a cloudsave.Store against the contract the service relies on. A store's tests call Run with a function that opens an empty store.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/cloudsave"
)

var epoch = time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)

// Run runs the contract tests as subtests of t, each on a fresh store from open.
func Run(t *testing.T, open func(t *testing.T) cloudsave.Store) {
	tests := []struct {
		name string
		f    func(*testing.T, cloudsave.Store)
	}{
		{"Empty", testEmpty},
		{"ConditionalWrites", testConditionalWrites},
		{"KeepsNewestVersions", testKeepsNewestVersions},
		{"DeleteAndRestore", testDeleteAndRestore},
		{"MaxSlots", testMaxSlots},
		{"PurgesOldestDeletedSlots", testPurgesOldestDeletedSlots},
		{"AccountsAreSeparate", testAccountsAreSeparate},
		{"Purge", testPurge},
		{"ConcurrentWritesOneWins", testConcurrentWritesOneWins},
		{"HeadDuringAppends", testHeadDuringAppends},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.f(t, open(t)) })
	}
}

func write(data string, m cloudsave.Match, at time.Duration) cloudsave.Write {
	return cloudsave.Write{Data: []byte(data), Match: m, Created: epoch.Add(at), Keep: 5, MaxSlots: 10}
}

func mustAppend(t *testing.T, s cloudsave.Store, account, slot string, w cloudsave.Write) cloudsave.Version {
	t.Helper()
	v, err := s.Append(context.Background(), account, slot, w)
	if err != nil {
		t.Fatalf("Append %s/%s: %v", account, slot, err)
	}
	return v
}

func numbers(vs []cloudsave.Version) string {
	var b bytes.Buffer
	for _, v := range vs {
		fmt.Fprintf(&b, "%s:%d", v.Slot, v.Number)
		if v.Deleted {
			b.WriteString("x")
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func testEmpty(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	if _, err := s.Head(ctx, "a", "main"); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("Head = %v", err)
	}
	if _, err := s.Version(ctx, "a", "main", 1); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("Version = %v", err)
	}
	if _, err := s.Versions(ctx, "a", "main"); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("Versions = %v", err)
	}
	if vs, err := s.Slots(ctx, "a"); err != nil || len(vs) != 0 {
		t.Fatalf("Slots = %v, %v", vs, err)
	}
	if _, err := s.Append(ctx, "a", "main", cloudsave.Write{Delete: true, Match: cloudsave.MatchAny, Created: epoch, Keep: 5, MaxSlots: 10}); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("delete of a missing slot = %v", err)
	}
	if err := s.Purge(ctx, "a", "main"); err != nil {
		t.Fatalf("Purge = %v", err)
	}
}

func testConditionalWrites(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	v1 := mustAppend(t, s, "a", "main", write("one", cloudsave.MatchHash(""), 0))
	if v1.Number != 1 || v1.Hash != cloudsave.Hash([]byte("one")) || v1.Size != 3 || !v1.Created.Equal(epoch) || v1.Data != nil || v1.Slot != "main" {
		t.Fatalf("v1 = %+v", v1)
	}
	for _, m := range []cloudsave.Match{cloudsave.MatchHash(""), cloudsave.MatchHash(cloudsave.Hash([]byte("other")))} {
		if _, err := s.Append(ctx, "a", "main", write("lost", m, time.Second)); !errors.Is(err, cloudsave.ErrChanged) {
			t.Fatalf("Append with stale %+v = %v", m, err)
		}
	}
	v2 := mustAppend(t, s, "a", "main", write("two", cloudsave.MatchHash(v1.Hash), 2*time.Second))
	if v2.Number != 2 {
		t.Fatalf("v2 = %+v", v2)
	}
	v3 := mustAppend(t, s, "a", "main", write("three", cloudsave.MatchAny, 3*time.Second))
	head, err := s.Head(ctx, "a", "main")
	if err != nil || head.Number != 3 || string(head.Data) != "three" || head.Hash != v3.Hash || !head.Created.Equal(epoch.Add(3*time.Second)) {
		t.Fatalf("Head = %+v, %v", head, err)
	}
	old, err := s.Version(ctx, "a", "main", 1)
	if err != nil || string(old.Data) != "one" || old.Number != 1 {
		t.Fatalf("Version 1 = %+v, %v", old, err)
	}
	// Binary and empty payloads round trip.
	bin := []byte{0, 0xff, 0x80, '\n'}
	mustAppend(t, s, "a", "bin", cloudsave.Write{Data: bin, Match: cloudsave.MatchAny, Created: epoch, Keep: 5, MaxSlots: 10})
	mustAppend(t, s, "a", "empty", cloudsave.Write{Match: cloudsave.MatchAny, Created: epoch, Keep: 5, MaxSlots: 10})
	if v, err := s.Head(ctx, "a", "bin"); err != nil || !bytes.Equal(v.Data, bin) {
		t.Fatalf("binary = %v, %v", v.Data, err)
	}
	if v, err := s.Head(ctx, "a", "empty"); err != nil || v.Data == nil || len(v.Data) != 0 || v.Hash != cloudsave.Hash(nil) {
		t.Fatalf("empty = %+v, %v", v, err)
	}
	// Times keep milliseconds.
	v := mustAppend(t, s, "a", "ms", write("x", cloudsave.MatchAny, 1234567*time.Microsecond))
	if !v.Created.Equal(epoch.Add(1234 * time.Millisecond)) {
		t.Fatalf("created = %v", v.Created)
	}
}

func testKeepsNewestVersions(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	for i := range 6 {
		w := write(fmt.Sprint("v", i+1), cloudsave.MatchAny, time.Duration(i)*time.Second)
		w.Keep = 3
		mustAppend(t, s, "a", "main", w)
	}
	vs, err := s.Versions(ctx, "a", "main")
	if err != nil || numbers(vs) != "main:6 main:5 main:4 " {
		t.Fatalf("Versions = %q, %v", numbers(vs), err)
	}
	for _, v := range vs {
		if v.Data != nil || v.Hash != cloudsave.Hash([]byte(fmt.Sprint("v", v.Number))) || v.Size != 2 {
			t.Fatalf("listed version = %+v", v)
		}
	}
	if _, err := s.Version(ctx, "a", "main", 3); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("pruned version = %v", err)
	}
}

func testDeleteAndRestore(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	v1 := mustAppend(t, s, "a", "main", write("one", cloudsave.MatchAny, 0))
	if _, err := s.Append(ctx, "a", "main", cloudsave.Write{Delete: true, Match: cloudsave.MatchHash(cloudsave.Hash([]byte("x"))), Created: epoch, Keep: 5, MaxSlots: 10}); !errors.Is(err, cloudsave.ErrChanged) {
		t.Fatalf("stale delete = %v", err)
	}
	d := mustAppend(t, s, "a", "main", cloudsave.Write{Delete: true, Match: cloudsave.MatchHash(v1.Hash), Created: epoch.Add(time.Second), Keep: 5, MaxSlots: 10})
	if d.Number != 2 || !d.Deleted || d.Hash != "" || d.Size != 0 {
		t.Fatalf("deletion = %+v", d)
	}
	head, err := s.Head(ctx, "a", "main")
	if err != nil || !head.Deleted || head.Data != nil {
		t.Fatalf("Head after delete = %+v, %v", head, err)
	}
	if _, err := s.Append(ctx, "a", "main", cloudsave.Write{Delete: true, Match: cloudsave.MatchAny, Created: epoch, Keep: 5, MaxSlots: 10}); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	if v, err := s.Version(ctx, "a", "main", 2); err != nil || !v.Deleted || v.Data != nil {
		t.Fatalf("deletion version = %+v, %v", v, err)
	}
	if slots, err := s.Slots(ctx, "a"); err != nil || numbers(slots) != "main:2x " {
		t.Fatalf("Slots = %q, %v", numbers(slots), err)
	}
	// A deleted slot counts as absent for preconditions.
	if _, err := s.Append(ctx, "a", "main", write("stale", cloudsave.MatchHash(v1.Hash), 2*time.Second)); !errors.Is(err, cloudsave.ErrChanged) {
		t.Fatalf("write over a deletion with the old hash = %v", err)
	}
	old, err := s.Version(ctx, "a", "main", 1)
	if err != nil {
		t.Fatal(err)
	}
	r := mustAppend(t, s, "a", "main", write(string(old.Data), cloudsave.MatchHash(""), 3*time.Second))
	if r.Number != 3 || r.Hash != v1.Hash {
		t.Fatalf("restored = %+v", r)
	}
	if vs, err := s.Versions(ctx, "a", "main"); err != nil || numbers(vs) != "main:3 main:2x main:1 " {
		t.Fatalf("Versions = %q, %v", numbers(vs), err)
	}
}

func testMaxSlots(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	w := func(data string) cloudsave.Write {
		w := write(data, cloudsave.MatchAny, 0)
		w.MaxSlots = 2
		return w
	}
	mustAppend(t, s, "a", "one", w("1"))
	mustAppend(t, s, "a", "two", w("2"))
	mustAppend(t, s, "a", "two", w("2b")) // rewriting a live slot is not a new slot
	if _, err := s.Append(ctx, "a", "three", w("3")); !errors.Is(err, cloudsave.ErrTooManySlots) {
		t.Fatalf("third slot = %v", err)
	}
	if _, err := s.Head(ctx, "a", "three"); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("refused slot was stored: %v", err)
	}
	del := cloudsave.Write{Delete: true, Match: cloudsave.MatchAny, Created: epoch, Keep: 5, MaxSlots: 2}
	mustAppend(t, s, "a", "one", del)
	mustAppend(t, s, "a", "three", w("3"))
	// Writing the deleted slot again would make three live slots.
	if _, err := s.Append(ctx, "a", "one", w("1b")); !errors.Is(err, cloudsave.ErrTooManySlots) {
		t.Fatalf("revived slot = %v", err)
	}
}

func testPurgesOldestDeletedSlots(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	for i, slot := range []string{"c", "a", "b", "d"} {
		w := write(slot, cloudsave.MatchAny, time.Duration(i)*time.Second)
		w.MaxSlots = 2
		mustAppend(t, s, "acct", slot, w)
		d := cloudsave.Write{Delete: true, Match: cloudsave.MatchAny, Created: epoch.Add(time.Duration(10+i) * time.Second), Keep: 5, MaxSlots: 2}
		mustAppend(t, s, "acct", slot, d)
	}
	// c and a were deleted first, so they are purged once more than two slots are deleted.
	slots, err := s.Slots(ctx, "acct")
	if err != nil || numbers(slots) != "b:2x d:2x " {
		t.Fatalf("Slots = %q, %v", numbers(slots), err)
	}
	if _, err := s.Versions(ctx, "acct", "c"); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("purged slot history = %v", err)
	}
}

func testAccountsAreSeparate(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	mustAppend(t, s, "alice", "main", write("alice", cloudsave.MatchAny, 0))
	mustAppend(t, s, "bob", "main", write("bob", cloudsave.MatchHash(""), 0))
	for _, acct := range []string{"alice", "bob"} {
		v, err := s.Head(ctx, acct, "main")
		if err != nil || string(v.Data) != acct || v.Number != 1 {
			t.Fatalf("%s Head = %+v, %v", acct, v, err)
		}
	}
	if slots, err := s.Slots(ctx, "carol"); err != nil || len(slots) != 0 {
		t.Fatalf("carol Slots = %v, %v", slots, err)
	}
}

func testPurge(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	for _, slot := range []string{"z", "main", "a"} {
		mustAppend(t, s, "a", slot, write(slot, cloudsave.MatchAny, 0))
	}
	mustAppend(t, s, "a", "main", write("main2", cloudsave.MatchAny, time.Second))
	if slots, err := s.Slots(ctx, "a"); err != nil || numbers(slots) != "a:1 main:2 z:1 " {
		t.Fatalf("Slots = %q, %v", numbers(slots), err)
	}
	if err := s.Purge(ctx, "a", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Versions(ctx, "a", "main"); !errors.Is(err, cloudsave.ErrNotFound) {
		t.Fatalf("purged Versions = %v", err)
	}
	// A purged slot starts over from version 1.
	if v := mustAppend(t, s, "a", "main", write("again", cloudsave.MatchHash(""), 2*time.Second)); v.Number != 1 {
		t.Fatalf("after purge = %+v", v)
	}
}

func testConcurrentWritesOneWins(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	base := mustAppend(t, s, "a", "main", write("base", cloudsave.MatchAny, 0))
	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			_, errs[i] = s.Append(ctx, "a", "main", write(fmt.Sprint("device-", i), cloudsave.MatchHash(base.Hash), time.Second))
		})
	}
	wg.Wait()
	won := -1
	for i, err := range errs {
		switch {
		case err == nil && won < 0:
			won = i
		case err == nil:
			t.Fatalf("writers %d and %d both overwrote version 1", won, i)
		case !errors.Is(err, cloudsave.ErrChanged):
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	if won < 0 {
		t.Fatal("no writer won")
	}
	head, err := s.Head(ctx, "a", "main")
	if err != nil || head.Number != 2 || string(head.Data) != fmt.Sprint("device-", won) {
		t.Fatalf("Head = %+v, %v", head, err)
	}
}

// testHeadDuringAppends reads a live slot while another device keeps writing it with Keep 1, so every write prunes the version a reader may have just seen. Head must always return a live version with its data, never ErrNotFound.
func testHeadDuringAppends(t *testing.T, s cloudsave.Store) {
	ctx := context.Background()
	w := write("v0", cloudsave.MatchAny, 0)
	w.Keep = 1
	mustAppend(t, s, "a", "main", w)
	const writes = 200
	done := make(chan error, 1)
	go func() {
		for i := range writes {
			w := write(fmt.Sprint("v", i+1), cloudsave.MatchAny, time.Duration(i+1)*time.Millisecond)
			w.Keep = 1
			if _, err := s.Append(ctx, "a", "main", w); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Append: %v", err)
			}
			head, err := s.Head(ctx, "a", "main")
			if err != nil || head.Number != writes+1 || string(head.Data) != fmt.Sprint("v", writes) {
				t.Fatalf("final Head = %+v, %v", head, err)
			}
			return
		default:
		}
		head, err := s.Head(ctx, "a", "main")
		if err != nil {
			t.Fatalf("Head while appending: %v", err)
		}
		if head.Deleted || string(head.Data) != fmt.Sprint("v", head.Number-1) || head.Hash != cloudsave.Hash(head.Data) {
			t.Fatalf("Head while appending = %+v", head)
		}
	}
}
