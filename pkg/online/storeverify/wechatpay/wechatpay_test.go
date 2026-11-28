package wechatpay_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/storeverify/wechatpay"
)

// Fixtures computed independently with `openssl dgst -sha256 -hmac <key>` over the exact strings below.
const (
	appKey     = "app-key-sandbox"
	sessionKey = "session-key"

	wantSignData = `{"mode":"goods","offerId":"1450000000","buyQuantity":2,"env":1,"currencyType":"CNY","productId":"gem_60","goodsPrice":600,"outTradeNo":"order-20261128-0001","attach":"slot=1"}`
	wantPaySig   = "15f6ad866787629a8bbb042046a5e1414f2f0949612849a57c14b10acd13c1f6"
	wantUserSig  = "45270062cdf69a404c45b4f7a85b22d1d6b284518c4e4d6d83a54fe3fc00aaf3"

	// payload carries a field (WeChatPayInfo) Delivery does not declare, as the platform's pushes do.
	payload        = `{"OpenId":"o-player","Env":1,"OutTradeNo":"order-20261128-0001","GoodsInfo":{"ProductId":"gem_60","Quantity":2,"ZoneId":"1","OrigPrice":1200,"ActualPrice":1200,"Attach":"slot=1","OrderSource":1},"WeChatPayInfo":{"MchOrderNo":"m1","TransactionId":"t1"}}`
	wantPayloadSig = "b3933263c20cbfc19e7a0ccc9ad9a290a7e1a6a21dedf5f2dadec3c516becd20"
)

var order = wechatpay.Order{OfferID: "1450000000", ProductID: "gem_60", GoodsPrice: 600, BuyQuantity: 2, OutTradeNo: "order-20261128-0001", Env: wechatpay.EnvSandbox, Attach: "slot=1"}

func TestSignOrderMatchesFixture(t *testing.T) {
	signData, paySig, sig, err := wechatpay.SignOrder(appKey, sessionKey, order)
	if err != nil {
		t.Fatal(err)
	}
	if signData != wantSignData {
		t.Errorf("signData = %s\nwant      %s", signData, wantSignData)
	}
	if paySig != wantPaySig {
		t.Errorf("paySig = %s, want %s", paySig, wantPaySig)
	}
	if sig != wantUserSig {
		t.Errorf("signature = %s, want %s", sig, wantUserSig)
	}
}

func TestSignOrderRefusesInvalidOrders(t *testing.T) {
	cases := map[string]func(*wechatpay.Order){
		"no offer":              func(o *wechatpay.Order) { o.OfferID = "" },
		"no product":            func(o *wechatpay.Order) { o.ProductID = "" },
		"zero price":            func(o *wechatpay.Order) { o.GoodsPrice = 0 },
		"zero quantity":         func(o *wechatpay.Order) { o.BuyQuantity = 0 },
		"no order number":       func(o *wechatpay.Order) { o.OutTradeNo = "" },
		"leading underscore":    func(o *wechatpay.Order) { o.OutTradeNo = "_abc" },
		"order number too long": func(o *wechatpay.Order) { o.OutTradeNo = strings.Repeat("a", 33) },
		"bad character":         func(o *wechatpay.Order) { o.OutTradeNo = "a b" },
		"unknown environment":   func(o *wechatpay.Order) { o.Env = 2 },
	}
	for name, mutate := range cases {
		o := order
		mutate(&o)
		if _, _, _, err := wechatpay.SignOrder(appKey, sessionKey, o); !errors.Is(err, wechatpay.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, _, _, err := wechatpay.SignOrder("", sessionKey, order); !errors.Is(err, wechatpay.ErrInvalid) {
		t.Errorf("missing AppKey: err = %v, want ErrInvalid", err)
	}
}

func TestPayEventSigMatchesFixture(t *testing.T) {
	if got := wechatpay.PayEventSig(appKey, wechatpay.EventGoodsDeliver, payload); got != wantPayloadSig {
		t.Fatalf("PayEventSig = %s, want %s", got, wantPayloadSig)
	}
}

func newHandler(deliver func(context.Context, wechatpay.Delivery) error) *wechatpay.Handler {
	return &wechatpay.Handler{AppKey: appKey, Env: wechatpay.EnvSandbox, Store: wechatpay.NewMemoryStore(), Deliver: deliver}
}

func TestHandleDecodesDeliveryAndRetriesDeliverOnce(t *testing.T) {
	var got []wechatpay.Delivery
	h := newHandler(func(_ context.Context, d wechatpay.Delivery) error { got = append(got, d); return nil })
	for i := 0; i < 10; i++ {
		if err := h.Handle(context.Background(), wechatpay.EventGoodsDeliver, payload, wantPayloadSig); err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
	}
	if len(got) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(got))
	}
	want := wechatpay.Delivery{OpenID: "o-player", Env: 1, OutTradeNo: "order-20261128-0001", GoodsInfo: wechatpay.GoodsInfo{ProductID: "gem_60", Quantity: 2, ZoneID: "1", OrigPrice: 1200, ActualPrice: 1200, Attach: "slot=1", OrderSource: 1}}
	if got[0] != want {
		t.Fatalf("delivery = %+v, want %+v", got[0], want)
	}
	stored, ok, err := h.Store.Get(context.Background(), want.OutTradeNo)
	if err != nil || !ok || string(stored) != payload {
		t.Fatalf("store = %q, %v, %v", stored, ok, err)
	}
}

func TestHandleConcurrentRetriesDeliverOnce(t *testing.T) {
	var n atomic.Int32
	h := newHandler(func(context.Context, wechatpay.Delivery) error { n.Add(1); return nil })
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- h.Handle(context.Background(), wechatpay.EventGoodsDeliver, payload, wantPayloadSig)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("deliveries = %d, want 1", n.Load())
	}
}

func TestHandleRetriesAfterDeliverFails(t *testing.T) {
	fail := errors.New("grant failed")
	calls := 0
	h := newHandler(func(context.Context, wechatpay.Delivery) error {
		calls++
		if calls == 1 {
			return fail
		}
		return nil
	})
	if err := h.Handle(context.Background(), wechatpay.EventGoodsDeliver, payload, wantPayloadSig); !errors.Is(err, fail) {
		t.Fatalf("first push: err = %v, want %v", err, fail)
	}
	if err := h.Handle(context.Background(), wechatpay.EventGoodsDeliver, payload, wantPayloadSig); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("Deliver calls = %d, want 2", calls)
	}
}

func TestHandleRefusesUnverifiedNotifications(t *testing.T) {
	tampered := strings.Replace(payload, `"Quantity":2`, `"Quantity":200`, 1)
	production := strings.Replace(payload, `"Env":1`, `"Env":0`, 1)
	cases := []struct {
		name, event, payload, sig string
		env                       int
	}{
		{"tampered payload", wechatpay.EventGoodsDeliver, tampered, wantPayloadSig, wechatpay.EnvSandbox},
		{"wrong signature", wechatpay.EventGoodsDeliver, payload, strings.Repeat("0", 64), wechatpay.EnvSandbox},
		{"other event", "minigame_coin_deliver_completed", payload, wechatpay.PayEventSig(appKey, "minigame_coin_deliver_completed", payload), wechatpay.EnvSandbox},
		{"sandbox push to production handler", wechatpay.EventGoodsDeliver, payload, wantPayloadSig, wechatpay.EnvProduction},
		{"production push to sandbox handler", wechatpay.EventGoodsDeliver, production, wechatpay.PayEventSig(appKey, wechatpay.EventGoodsDeliver, production), wechatpay.EnvSandbox},
		{"malformed payload", wechatpay.EventGoodsDeliver, "{", wechatpay.PayEventSig(appKey, wechatpay.EventGoodsDeliver, "{"), wechatpay.EnvSandbox},
	}
	for _, c := range cases {
		delivered := false
		h := newHandler(func(context.Context, wechatpay.Delivery) error { delivered = true; return nil })
		h.Env = c.env
		if err := h.Handle(context.Background(), c.event, c.payload, c.sig); !errors.Is(err, wechatpay.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", c.name, err)
		}
		if delivered {
			t.Errorf("%s: delivered", c.name)
		}
	}
}
