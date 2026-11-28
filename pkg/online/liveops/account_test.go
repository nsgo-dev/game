package liveops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type memSecret struct {
	mu sync.Mutex
	v  map[string]string
}

func (m *memSecret) Get(k string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.v[k]
	return v, ok
}
func (m *memSecret) Set(k, v string) error { m.mu.Lock(); defer m.mu.Unlock(); m.v[k] = v; return nil }
func (m *memSecret) Delete(k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.v, k)
	return nil
}
func TestAccountRestoresAndRefreshesOnce(t *testing.T) {
	ms := &memSecret{v: map[string]string{"session": "s1"}}
	var mu sync.Mutex
	calls := 0
	sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"account": "a1", "session": "s1", "token": "t1", "expires": time.Now().Add(time.Hour)})
	}))
	defer sv.Close()
	a := NewAccount(AccountOptions{URL: sv.URL, Audience: "game", Secrets: ms, SecretsKey: "session"})
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tok, err := a.Token(context.Background()); err != nil || tok != "t1" {
				t.Errorf("token=%q err=%v", tok, err)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls=%d", calls)
	}
}
