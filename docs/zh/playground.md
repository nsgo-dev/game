# 在线试玩

[在线试玩](https://game.nsgo.dev/zh/playground/)页面在浏览器中运行本仓库的示例：它们用 NSGo 的 Web 目标构建，旁边显示各自的源码。想修改示例时，在本机用开发服务器运行它，每次保存都会重新构建并重新加载。

## 在浏览器中

每个示例都有自己的页面，例如 `/zh/playground/caverun/`，分享这个地址即可。页面上有：

- **游戏**：按下 **运行** 后才加载。它的 WebAssembly 约 15 MB，在此之前不会下载任何内容。点击游戏画面后即可用键盘操作。**重新开始** 会从头再加载一次，**在新标签页中打开** 会在单独的页面上运行这个构建。
- **源码**：每个文件一个标签页，先是示例的 main 包（不含测试），然后是它的 `nsgo.yaml`。显示哪个标签页由地址的片段决定，因此 `/zh/playground/caverun/#level.go` 会直接打开 `level.go`。每个文件都链接到它在 GitHub 上的位置。

### 在手机上游玩

在触屏设备（`pointer: coarse`）上，舞台会显示 **全屏游玩** 按钮，并将操作提示替换为触控提示：

- **全屏与全视口覆盖层**：按下 **全屏游玩** 会优先对舞台调用原生元素全屏（Android Chrome 等支持的浏览器），并在允许时将屏幕锁定到示例期望的方向。在不支持元素全屏的浏览器（例如 iPhone Safari）上，舞台会以固定全视口（`100dvh`）覆盖层打开，并附带 **退出** 按钮。
- **防止误滚动**：在移动游玩模式下，页面滚动会被锁定（`overflow: hidden` 与 `overscroll-behavior: none`），舞台也会启用 `touch-action: none`，确保在虚拟按键或棋盘上拖动拇指时既不会滚动页面，也不会取消触控。
- **旋转提示**：每个试玩示例都在 `apps/website/src/content/playground.ts` 中声明期望的屏幕方向（`landscape`、`portrait` 或 `any`）。当屏幕方向不匹配且无法锁定方向时，界面会提示旋转手机（也提供继续游玩的选项）。

站点是托管在 GitHub Pages 上的静态站点，不会编译任何代码：页面运行的是站点部署时的构建，与它显示的源码来自同一个提交。

## 在本机修改

安装 Go 1.27 或更高版本后，克隆仓库，并用 Web 目标的开发服务器启动示例：

```sh
git clone https://github.com/nsgo-dev/game.git
cd game/examples/caverun
go tool nsgo dev -target web
```

它会在浏览器（Chrome 和 Safari 均可）中打开游戏，每次保存示例或根模块（`pkg/`）中的文件时都会重新构建并重新加载；示例通过仓库中的 `go.work` 使用本仓库模块。构建失败时，错误会显示在游戏上方，直到下次保存构建成功。去掉 `-target web`，示例会作为原生应用运行，同样会重新加载。

## 构建是怎样生成的

在仓库根目录运行 `bun run playground`，它会用 `go tool nsgo build -target web` 把每个示例构建到示例自己的 `dist/playground/` 中，再把生成的站点复制到 `apps/website/public/play/<id>/`（该目录被 Git 忽略）。网站在 `/play/<id>/` 提供这个目录，每个示例的页面在框架中显示它。`bun run playground caverun` 只构建一个示例。

网站的工作流会先安装 Go 并运行它，再构建站点；推送到 `main`，以及修改站点、示例或引擎包的拉取请求都会触发。在 CI 中缺少构建会让站点构建失败，因此部署不会发布没有游戏的页面；在本地没有构建时运行 `bun run build` 只会给出警告，示例页面会显示生成其构建的命令。

## 添加示例

1. 把示例放在 `examples/<id>/` 下，包含 `nsgo.yaml` 和能为 Web 目标构建的 main 包，并在 `go.work` 中加入它。
2. 在 `apps/website/src/content/playground.ts` 的 `PLAYGROUND_IDS` 中加入 `<id>`，并在 `PLAYGROUND_ENTRIES` 中声明它期望的手机方向 `orientation`。
3. 在 `apps/website/src/content/showcase.ts` 中为它添加一个条目，在线试玩页面会显示其中的中英文标题和描述。
