# WeChat mini-game payments

`online/storeverify/wechatpay` keeps order signing and delivery idempotency in a core-free package. `SignOrder` produces server-side pay signatures without exposing the AppKey to a client. `Handler` uses a `DeliveryStore` keyed by `OutTradeNo`, so platform retries call the delivery function once after successful verification.
