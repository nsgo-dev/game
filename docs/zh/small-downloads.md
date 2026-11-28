# 更小的下载与更快的启动

将启动后才需要的文件放在 `resources/`，通过 `asset:` URI 加载。网页端会按需获取。`//go:embed` 适合很小的构建时文件，但嵌入的运行时数据会在首帧前下载并持续占用内存。

为 `assets.Manager` 注册一个小的首组并显示 `assets.LoadingScreen`。首帧可交互后在后台加载后续组。场景结束时释放组，将可选内容放进 DLC 包而非初始包。

网页构建可用 `web.offline` 在首次启动后缓存发布版本。加载视图获得响应式进度，也可以用品牌化 `View` 替换。`nsgo build` 会写出 Brotli 与 gzip 副本，供主机按压缩协商提供。

使用 `mobile.exclude_resources` 排除仅桌面文件。选择合适的压缩音频与采样率，并在游戏模块目录运行 `go run github.com/nsgo-dev/game/cmd/sizecheck -kind wasm path/to/game.wasm`。使用 `-budget-mb` 设置原始 wasm 限制，预算文件中的压缩阈值仍然有效。
