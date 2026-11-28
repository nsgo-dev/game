// Package economy provides a concurrency-safe in-memory wallet core. A
// durable records-backed adapter can implement the same operation semantics.
package economy

import (
	"errors"
	"sync"
)

var ErrInsufficient = errors.New("economy: insufficient balance")
var ErrUnknownCurrency = errors.New("economy: unknown currency")

type Wallet struct {
	mu       sync.Mutex
	balances map[string]map[string]int64
	ops      map[string]Result
}
type Result struct {
	Key      string
	Balances map[string]int64
}

func New() *Wallet { return &Wallet{balances: map[string]map[string]int64{}, ops: map[string]Result{}} }
func (w *Wallet) Balance(account, currency string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.balances[account][currency]
}
func (w *Wallet) Grant(account, currency, key string, amount int64) (Result, error) {
	return w.apply(account, currency, key, amount, false)
}
func (w *Wallet) Spend(account, currency, key string, amount int64) (Result, error) {
	return w.apply(account, currency, key, amount, true)
}
func (w *Wallet) apply(account, currency, key string, amount int64, spend bool) (Result, error) {
	if amount <= 0 {
		return Result{}, errors.New("economy: amount must be positive")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if r, ok := w.ops[key]; ok {
		return r, nil
	}
	m := w.balances[account]
	if m == nil {
		m = map[string]int64{}
		w.balances[account] = m
	}
	if spend && m[currency] < amount {
		return Result{}, ErrInsufficient
	}
	if spend {
		m[currency] -= amount
	} else {
		m[currency] += amount
	}
	r := Result{Key: key, Balances: map[string]int64{currency: m[currency]}}
	w.ops[key] = r
	return r, nil
}
func (w *Wallet) Exchange(account, key string, spend, grant map[string]int64) (Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r, ok := w.ops[key]; ok {
		return r, nil
	}
	m := w.balances[account]
	if m == nil {
		m = map[string]int64{}
		w.balances[account] = m
	}
	for c, n := range spend {
		if n <= 0 || m[c] < n {
			return Result{}, ErrInsufficient
		}
	}
	for c, n := range spend {
		m[c] -= n
	}
	for c, n := range grant {
		if n <= 0 {
			return Result{}, errors.New("economy: amount must be positive")
		}
		m[c] += n
	}
	out := map[string]int64{}
	for c := range grant {
		out[c] = m[c]
	}
	r := Result{Key: key, Balances: out}
	w.ops[key] = r
	return r, nil
}
