// Package wechatpay implements the core-free signing and idempotent delivery
// boundary for WeChat mini-game virtual payments.
package wechatpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
)

var ErrInvalid = errors.New("wechatpay: invalid request")

type Order struct {
	OfferID, ProductID, OutTradeNo, Attach, ZoneID string
	GoodsPrice, BuyQuantity, Env                   int
}
type Delivery struct{ OpenID, Env, OutTradeNo, GoodsInfo string }
type DeliveryStore interface {
	Get(string) ([]byte, bool, error)
	Put(string, []byte) error
}
type MemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: map[string][]byte{}} }
func (s *MemoryStore) Get(k string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[k]
	return append([]byte(nil), v...), ok, nil
}
func (s *MemoryStore) Put(k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[k]; ok {
		return nil
	}
	s.values[k] = append([]byte(nil), v...)
	return nil
}
func SignOrder(appKey, sessionKey string, o Order) (signData, paySig, signature string, error error) {
	if appKey == "" || sessionKey == "" || o.OfferID == "" || o.ProductID == "" || o.OutTradeNo == "" || o.GoodsPrice <= 0 || o.BuyQuantity <= 0 {
		return "", "", "", ErrInvalid
	}
	b, _ := json.Marshal(o)
	signData = string(b)
	paySig = sign(appKey, "requestMidasPaymentGameItem&"+signData)
	signature = sign(sessionKey, signData)
	return
}
func sign(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}
func DeliverySignature(key string, d Delivery) string {
	b, _ := json.Marshal(d)
	return sign(key, string(b))
}

type Handler struct {
	Store        DeliveryStore
	Deliver      func(Delivery) error
	Env          int
	SignatureKey string
}

func (h *Handler) Handle(d Delivery, sig string) error {
	if h == nil || h.Store == nil || h.Deliver == nil || d.OutTradeNo == "" || h.SignatureKey == "" {
		return ErrInvalid
	}
	raw, _ := json.Marshal(d)
	if !hmac.Equal([]byte(DeliverySignature(h.SignatureKey, d)), []byte(sig)) {
		return ErrInvalid
	}
	if h.Env != 0 && d.Env != strconv.Itoa(h.Env) {
		return ErrInvalid
	}
	if _, ok, err := h.Store.Get(d.OutTradeNo); err != nil {
		return err
	} else if ok {
		return nil
	}
	if err := h.Deliver(d); err != nil {
		return err
	}
	return h.Store.Put(d.OutTradeNo, raw)
}
