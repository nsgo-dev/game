# 微信小游戏支付

`online/storeverify/wechatpay` 在无 core 依赖的包中提供订单签名和幂等发货。`SignOrder` 在服务端生成支付签名，AppKey 不会暴露给客户端。`Handler` 使用以 `OutTradeNo` 为键的 `DeliveryStore`，平台重试在验证成功后只调用一次发货函数。
