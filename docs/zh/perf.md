# perf/session

`github.com/nsgo-dev/game/pkg/perf/session` 不依赖调试覆盖层，可在发布构建中记录帧节奏。把已完成帧的时长传给 `Recorder.Observe`，再读取 `Summary` 获取 p50/p95/p99、最长帧、至少两倍目标间隔的帧数、堆峰值、运行时内存以及 GC 周期与暂停观察值。记录器使用固定环形缓冲，可在发布版本采样。
