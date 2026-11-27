// Package audit writes an append-only JSON Lines log of a server's
// administrative and security-relevant events — kicks, replaced sessions,
// operator commands and destructive calls — with the actor and the subject
// of each, so support questions, anti-cheat review and accountability for
// admin power have a trail that survives restarts. One file per UTC day
// rolls at midnight; the files are text, greppable and shippable to a log
// store, and need no database.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SchemaVersion is the record schema's version, written as each line's "v".
const SchemaVersion = 1

// DefaultQueue is a Writer's buffer size when Options.Queue is 0.
const DefaultQueue = 4096

// dayLayout is the file name's date, in UTC; ISO dates order as strings.
const dayLayout = "2006-01-02"

// timeLayout is a line's "time": UTC, RFC 3339 with milliseconds.
const timeLayout = "2006-01-02T15:04:05.000Z"

// Record is one audited event: who did what to whom, with the action's
// details in Data.
type Record struct {
	// V is the schema version; Log fills in SchemaVersion when 0.
	V int `json:"v"`
	// Time is when the event happened; Log fills in the current time when
	// zero. The record lands in the file of its own UTC day, so a replayed
	// batch lands in the files of the days it happened.
	Time time.Time `json:"time"`
	// Actor is the account ID, an admin token subject, or "system" when empty.
	Actor string `json:"actor"`
	// Action is what happened, such as "peer.kick", "session.replaced" or
	// "admin.run".
	Action string `json:"action"`
	// Subject is what the action acted on: a peer, an account, a board/slot.
	Subject string `json:"subject"`
	// Data holds the action's details; omitted from the line when empty.
	Data map[string]any `json:"data,omitempty"`
}

// Options are a Writer's settings.
type Options struct {
	// Dir is the directory the log lives in, one <yyyy-mm-dd>.jsonl file per
	// UTC day, created when missing. Required.
	Dir string
	// Queue is how many records buffer while the writer appends earlier
	// ones, DefaultQueue when 0. Log drops and counts a record past it
	// rather than blocking the caller.
	Queue int
	// Retain is how many days of files to keep, counting back from the day
	// of the record being written; older files are deleted as the log opens
	// a new day's file. 0 keeps every file.
	Retain int
}

// Writer appends records to the day's file on one background goroutine,
// fsyncing in batches. It is safe for concurrent use.
type Writer struct {
	dir      string
	retain   int
	now      func() time.Time
	syncFile func(*os.File) error

	queue   chan Record
	dropped atomic.Uint64

	mu     sync.RWMutex // closed against sends on queue
	closed bool
	done   chan struct{}
	err    error // the first append failure, returned by Close
}

// Open returns a Writer logging to o.Dir, creating the directory when
// missing.
func Open(o Options) (*Writer, error) {
	return open(o, time.Now, nil)
}

// open is Open with the clock and the file sync replaced, for tests.
func open(o Options, now func() time.Time, syncFile func(*os.File) error) (*Writer, error) {
	if o.Dir == "" {
		return nil, errors.New("audit: Options.Dir is empty")
	}
	if o.Queue < 0 {
		return nil, fmt.Errorf("audit: Options.Queue %d is negative", o.Queue)
	}
	if o.Retain < 0 {
		return nil, fmt.Errorf("audit: Options.Retain %d is negative", o.Retain)
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	if syncFile == nil {
		syncFile = (*os.File).Sync
	}
	queue := o.Queue
	if queue == 0 {
		queue = DefaultQueue
	}
	w := &Writer{
		dir:      o.Dir,
		retain:   o.Retain,
		now:      now,
		syncFile: syncFile,
		queue:    make(chan Record, queue),
		done:     make(chan struct{}),
	}
	go w.run()
	return w, nil
}

// Log queues a record, filling in its schema version, time and actor when
// unset. It never blocks: a record that does not fit in the queue is
// dropped and counted by Dropped, as is one logged after Close.
func (w *Writer) Log(r Record) {
	if r.V == 0 {
		r.V = SchemaVersion
	}
	if r.Time.IsZero() {
		r.Time = w.now()
	}
	if r.Actor == "" {
		r.Actor = "system"
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.dropped.Add(1)
		return
	}
	select {
	case w.queue <- r:
	default:
		w.dropped.Add(1)
	}
}

// Dropped reports how many records never reached the file: they arrived
// while the queue was full, after Close, or their append failed.
func (w *Writer) Dropped() uint64 {
	return w.dropped.Load()
}

// Close flushes the queued records, fsyncs and closes the file, and returns
// the first append failure, if any. It is safe to call twice.
func (w *Writer) Close() error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	<-w.done
	return w.err
}

// run appends queued records until Close closes the queue, then flushes.
func (w *Writer) run() {
	defer close(w.done)
	var f *os.File
	day := ""
	finish := func() {
		if f == nil {
			return
		}
		if err := w.syncFile(f); err != nil {
			w.setErr(fmt.Errorf("audit: sync %s: %w", day, err))
		}
		if err := f.Close(); err != nil {
			w.setErr(fmt.Errorf("audit: close %s: %w", day, err))
		}
		f = nil
	}
	defer finish()
	dirty := false
	for r := range w.queue {
		d := r.Time.UTC().Format(dayLayout)
		if f != nil && d != day {
			finish()
		}
		if f == nil {
			nf, err := os.OpenFile(filepath.Join(w.dir, d+".jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
			if err != nil {
				w.setErr(fmt.Errorf("audit: open %s: %w", d, err))
				w.dropped.Add(1)
				continue
			}
			f, day = nf, d
			w.expire(d)
		}
		line, err := json.Marshal(lineOf(r))
		if err != nil {
			w.setErr(fmt.Errorf("audit: encode %s: %w", r.Action, err))
			w.dropped.Add(1)
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			w.setErr(fmt.Errorf("audit: append %s: %w", day, err))
			w.dropped.Add(1)
			continue
		}
		dirty = true
		// Fsync once a burst is written, not once per record: while more
		// records wait in the queue they join the same sync.
		if dirty && len(w.queue) == 0 {
			if err := w.syncFile(f); err != nil {
				w.setErr(fmt.Errorf("audit: sync %s: %w", day, err))
			}
			dirty = false
		}
	}
}

// setErr keeps the first failure for Close to return; the goroutine keeps
// appending what it can.
func (w *Writer) setErr(err error) {
	if w.err == nil {
		w.err = err
	}
}

// expire deletes the files older than Options.Retain days before day. Files
// that are not <yyyy-mm-dd>.jsonl are left alone.
func (w *Writer) expire(day string) {
	if w.retain <= 0 {
		return
	}
	date, err := time.Parse(dayLayout, day)
	if err != nil {
		return // day comes from dayLayout itself
	}
	cutoff := date.AddDate(0, 0, -w.retain).Format(dayLayout)
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		w.setErr(fmt.Errorf("audit: read %s: %w", w.dir, err))
		return
	}
	for _, e := range entries {
		d, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok {
			continue
		}
		if _, err := time.Parse(dayLayout, d); err != nil {
			continue
		}
		if d >= cutoff {
			continue
		}
		if err := os.Remove(filepath.Join(w.dir, e.Name())); err != nil {
			w.setErr(fmt.Errorf("audit: expire %s: %w", e.Name(), err))
		}
	}
}

// line is a Record as a log line writes it: the keys in a fixed order and
// the time in UTC, RFC 3339 with milliseconds, so the same records always
// give the same bytes.
type line struct {
	V       int            `json:"v"`
	Time    string         `json:"time"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Subject string         `json:"subject"`
	Data    map[string]any `json:"data,omitempty"`
}

func lineOf(r Record) line {
	return line{
		V:       r.V,
		Time:    r.Time.UTC().Format(timeLayout),
		Actor:   r.Actor,
		Action:  r.Action,
		Subject: r.Subject,
		Data:    r.Data,
	}
}
