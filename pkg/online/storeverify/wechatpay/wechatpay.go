// Package wechatpay is the server side of WeChat mini game virtual payments (道具直购): it signs the order a client passes to wx.requestMidasPaymentGameItem and verifies the platform's goods delivery notifications, delivering each order exactly once however often the platform retries it.
//
// The package has no NSGo dependency, so it runs in a game's backend. It does not receive the HTTP push itself: the push URL's token check and the safe-mode AES decryption stay with the server that hosts it, which passes the decoded event, Payload and PayEventSig to Handler.Handle and answers {"ErrCode":0,"ErrMsg":"Success"} when Handle returns nil.
package wechatpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ErrInvalid reports an order or a notification that fails validation or verification. Handle wraps it with the reason.
var ErrInvalid = errors.New("wechatpay: invalid request")

// EventGoodsDeliver is the Event of the push WeChat sends to deliver goods bought in the game with wx.requestMidasPaymentGameItem.
const EventGoodsDeliver = "minigame_game_pay_goods_deliver_notify"

// Midas environments for Order.Env, Delivery.Env and Handler.Env.
const (
	EnvProduction = 0
	EnvSandbox    = 1
)

// Order is one purchase the game's server creates for wx.requestMidasPaymentGameItem. Prices are in fen.
type Order struct {
	// OfferID is the offer ID from the mini game's payment settings (mp-支付基础配置).
	OfferID string
	// ProductID is the item's ID as configured on the platform.
	ProductID string
	// GoodsPrice is the unit price in fen; the platform checks it against the configured price.
	GoodsPrice int
	// BuyQuantity is the number of items bought.
	BuyQuantity int
	// OutTradeNo is the game's order number, the key deliveries are deduplicated by: at most 32 characters of letters, digits and _-|*@, not starting with an underscore.
	OutTradeNo string
	// Env is EnvProduction or EnvSandbox.
	Env int
	// ZoneID is the optional zone; the platform defaults it to "1".
	ZoneID string
	// Attach is optional data the platform passes back in the delivery notification.
	Attach string
}

// signData is the JSON the client passes to wx.requestMidasPaymentGameItem; the field order is fixed so the same order always signs the same.
type signData struct {
	Mode         string `json:"mode"`
	OfferID      string `json:"offerId"`
	BuyQuantity  int    `json:"buyQuantity"`
	Env          int    `json:"env"`
	CurrencyType string `json:"currencyType"`
	ZoneID       string `json:"zoneId,omitempty"`
	ProductID    string `json:"productId"`
	GoodsPrice   int    `json:"goodsPrice"`
	OutTradeNo   string `json:"outTradeNo"`
	Attach       string `json:"attach,omitempty"`
}

// SignOrder builds the signData JSON for wx.requestMidasPaymentGameItem with its paySig (HMAC-SHA256 with the payment environment's AppKey over "requestMidasPaymentGameItem&" + signData) and the user-state signature (HMAC-SHA256 with the player's session key over signData). Call it on the server: the AppKey must never reach a client. The client passes all three strings to the platform unchanged.
func SignOrder(appKey, sessionKey string, o Order) (signDataJSON, paySig, signature string, err error) {
	if appKey == "" || sessionKey == "" {
		return "", "", "", fmt.Errorf("%w: missing AppKey or session key", ErrInvalid)
	}
	if o.OfferID == "" || o.ProductID == "" || o.GoodsPrice <= 0 || o.BuyQuantity <= 0 {
		return "", "", "", fmt.Errorf("%w: order needs an offer ID, a product ID, a positive price and a positive quantity", ErrInvalid)
	}
	if !validOutTradeNo(o.OutTradeNo) {
		return "", "", "", fmt.Errorf("%w: OutTradeNo %q must be 1 to 32 letters, digits or _-|*@ and not start with _", ErrInvalid, o.OutTradeNo)
	}
	if o.Env != EnvProduction && o.Env != EnvSandbox {
		return "", "", "", fmt.Errorf("%w: unknown environment %d", ErrInvalid, o.Env)
	}
	b, err := json.Marshal(signData{
		Mode:         "goods",
		OfferID:      o.OfferID,
		BuyQuantity:  o.BuyQuantity,
		Env:          o.Env,
		CurrencyType: "CNY",
		ZoneID:       o.ZoneID,
		ProductID:    o.ProductID,
		GoodsPrice:   o.GoodsPrice,
		OutTradeNo:   o.OutTradeNo,
		Attach:       o.Attach,
	})
	if err != nil {
		return "", "", "", err
	}
	signDataJSON = string(b)
	return signDataJSON, hmacHex(appKey, "requestMidasPaymentGameItem&"+signDataJSON), hmacHex(sessionKey, signDataJSON), nil
}

func validOutTradeNo(s string) bool {
	if s == "" || len(s) > 32 || s[0] == '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c == '_' || c == '-' || c == '|' || c == '*' || c == '@':
		default:
			return false
		}
	}
	return true
}

func hmacHex(key, msg string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// PayEventSig is the signature WeChat puts on a payment push: HMAC-SHA256 with the payment environment's AppKey over event + "&" + payload, in lowercase hex. Handle checks it; tests and mock pushes use it to sign a payload.
func PayEventSig(appKey, event, payload string) string {
	return hmacHex(appKey, event+"&"+payload)
}

// Delivery is the Payload of a goods delivery notification.
type Delivery struct {
	OpenID     string    `json:"OpenId"`
	Env        int       `json:"Env"`
	OutTradeNo string    `json:"OutTradeNo"`
	GoodsInfo  GoodsInfo `json:"GoodsInfo"`
}

// GoodsInfo is what a Delivery grants. Prices are in fen.
type GoodsInfo struct {
	ProductID   string `json:"ProductId"`
	Quantity    int    `json:"Quantity"`
	ZoneID      string `json:"ZoneId"`
	OrigPrice   int    `json:"OrigPrice"`
	ActualPrice int    `json:"ActualPrice"`
	Attach      string `json:"Attach"`
	OrderSource int    `json:"OrderSource"`
}

// DeliveryStore records the orders a Handler has delivered, keyed by OutTradeNo. A durable store keeps retries that arrive after a restart from delivering again.
type DeliveryStore interface {
	// Get reports whether outTradeNo was delivered, with the Payload recorded for it.
	Get(ctx context.Context, outTradeNo string) (payload []byte, ok bool, err error)
	// Put records outTradeNo as delivered with its Payload. Putting an order already recorded keeps the first record.
	Put(ctx context.Context, outTradeNo string, payload []byte) error
}

// MemoryStore is an in-process DeliveryStore for tests and single-process servers that can afford to forget deliveries on restart.
type MemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore { return &MemoryStore{values: map[string][]byte{}} }

// Get implements DeliveryStore.
func (s *MemoryStore) Get(_ context.Context, outTradeNo string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[outTradeNo]
	return append([]byte(nil), v...), ok, nil
}

// Put implements DeliveryStore.
func (s *MemoryStore) Put(_ context.Context, outTradeNo string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[outTradeNo]; !ok {
		s.values[outTradeNo] = append([]byte(nil), payload...)
	}
	return nil
}

// Handler verifies goods delivery notifications and calls Deliver once per OutTradeNo. Retries of a delivered order, including ones that arrive while the first is still being delivered in the same process, return nil without calling Deliver. A Deliver error is not recorded, so the platform's next retry delivers again.
//
// Several server processes sharing one durable store can still race between Get and Put; there Deliver itself must be idempotent on OutTradeNo, for example by granting in the same database transaction that records the order.
//
// A Handler must not be copied after first use.
type Handler struct {
	// AppKey is the payment environment's AppKey, which signs every push.
	AppKey string
	// Env is the environment this server delivers for; a notification from the other environment is refused, so sandbox purchases never grant goods in production.
	Env int
	// Store records delivered orders.
	Store DeliveryStore
	// Deliver grants the goods.
	Deliver func(context.Context, Delivery) error

	mu    sync.Mutex
	locks map[string]*keyLock
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

// Handle verifies one push's event, Payload and PayEventSig, and delivers the order unless it was delivered before. A nil error means the server should answer {"ErrCode":0,"ErrMsg":"Success"}; any error means a non-zero ErrCode, so the platform retries.
func (h *Handler) Handle(ctx context.Context, event, payload, payEventSig string) error {
	if h.AppKey == "" || h.Store == nil || h.Deliver == nil {
		return fmt.Errorf("%w: handler needs an AppKey, a Store and Deliver", ErrInvalid)
	}
	if event != EventGoodsDeliver {
		return fmt.Errorf("%w: unexpected event %q", ErrInvalid, event)
	}
	if !hmac.Equal([]byte(PayEventSig(h.AppKey, event, payload)), []byte(payEventSig)) {
		return fmt.Errorf("%w: PayEventSig does not match", ErrInvalid)
	}
	var d Delivery
	if err := json.Unmarshal([]byte(payload), &d); err != nil {
		return fmt.Errorf("%w: payload: %v", ErrInvalid, err)
	}
	if d.Env != h.Env {
		return fmt.Errorf("%w: notification for environment %d, handler serves %d", ErrInvalid, d.Env, h.Env)
	}
	if d.OutTradeNo == "" || d.OpenID == "" {
		return fmt.Errorf("%w: payload has no OutTradeNo or OpenId", ErrInvalid)
	}

	unlock := h.lock(d.OutTradeNo)
	defer unlock()
	if _, ok, err := h.Store.Get(ctx, d.OutTradeNo); err != nil {
		return err
	} else if ok {
		return nil
	}
	if err := h.Deliver(ctx, d); err != nil {
		return err
	}
	return h.Store.Put(ctx, d.OutTradeNo, []byte(payload))
}

// lock serializes the handling of one OutTradeNo within this Handler and returns the unlock function.
func (h *Handler) lock(key string) func() {
	h.mu.Lock()
	if h.locks == nil {
		h.locks = map[string]*keyLock{}
	}
	l := h.locks[key]
	if l == nil {
		l = &keyLock{}
		h.locks[key] = l
	}
	l.refs++
	h.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		h.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(h.locks, key)
		}
		h.mu.Unlock()
	}
}
