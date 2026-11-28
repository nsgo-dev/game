package wechatpay_test

import (
	"github.com/nsgo-dev/game/pkg/online/storeverify/wechatpay"
	"testing"
)

func TestSignOrderAndIdempotentDelivery(t *testing.T) {
	_, pay, sig, err := wechatpay.SignOrder("app", "session", wechatpay.Order{OfferID: "o", ProductID: "p", OutTradeNo: "n", GoodsPrice: 100, BuyQuantity: 1})
	if err != nil || pay == "" || sig == "" {
		t.Fatal(err)
	}
	n := 0
	h := &wechatpay.Handler{Store: wechatpay.NewMemoryStore(), Env: 1, SignatureKey: "verify", Deliver: func(wechatpay.Delivery) error { n++; return nil }}
	d := wechatpay.Delivery{OpenID: "u", Env: "1", OutTradeNo: "n", GoodsInfo: "coin"}
	if err := h.Handle(d, wechatpay.DeliverySignature("verify", d)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(d, wechatpay.DeliverySignature("verify", d)); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deliveries = %d", n)
	}
}
