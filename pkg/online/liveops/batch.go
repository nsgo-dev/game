package liveops

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// BatchSink receives a durable JSON-lines batch. Returning an error leaves the
// queue intact for a later retry.
type BatchSink func([]byte) error

// BatchProvider is an AnalyticsProvider backed by a bounded JSON-lines queue.
type BatchProvider struct {
	mu       sync.Mutex
	path     string
	maxBytes int
	sink     BatchSink
}

func NewBatchProvider(path string, maxBytes int, sink BatchSink) (*BatchProvider, error) {
	if path == "" || sink == nil {
		return nil, errors.New("liveops: batch provider needs path and sink")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return &BatchProvider{path: path, maxBytes: maxBytes, sink: sink}, nil
}
func (b *BatchProvider) Send(ev Event) error {
	raw, e := json.Marshal(ev)
	if e != nil {
		return e
	}
	raw = append(raw, '\n')
	b.mu.Lock()
	defer b.mu.Unlock()
	f, e := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(raw); e != nil {
		return e
	}
	st, _ := f.Stat()
	if st.Size() > int64(b.maxBytes) {
		data, _ := os.ReadFile(b.path)
		if len(data) > b.maxBytes {
			data = data[len(data)-b.maxBytes:]
			if i := indexNewline(data); i >= 0 {
				data = data[i+1:]
			}
		}
		_ = os.WriteFile(b.path, data, 0600)
	}
	return nil
}
func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}
func (b *BatchProvider) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, e := os.ReadFile(b.path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(data) == 0 {
		return nil
	}
	if e = b.sink(data); e != nil {
		return e
	}
	return os.WriteFile(b.path, nil, 0600)
}
func (b *BatchProvider) Purge() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e := os.Remove(b.path); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
func (b *BatchProvider) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, e := os.Open(b.path)
	if e != nil {
		return 0
	}
	defer f.Close()
	n := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		n++
	}
	return n
}
