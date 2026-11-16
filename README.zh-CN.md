<p align="center">
  <img src="assets/icon.png" alt="Umapp" width="128">
</p>

<h1 align="center">Umapp</h1>

<p align="center">
  <a href="README.md">English</a> |
  <strong>简体中文</strong>
</p>

<p align="center">
  <a href="https://github.com/oneo-me"><img alt="GitHub" src="https://img.shields.io/badge/GitHub-oneo--me-181717?logo=github&logoColor=white"></a>
  <a href="https://oneo.me"><img alt="博客" src="https://img.shields.io/badge/%E5%8D%9A%E5%AE%A2-oneo.me-0969da"></a>
  <a href="https://x.com/oneo666"><img alt="X" src="https://img.shields.io/badge/X-%40oneo666-000000?logo=x&logoColor=white"></a>
  <a href="https://github.com/sponsors/oneo-me"><img alt="GitHub 赞助" src="https://img.shields.io/badge/GitHub%20%E8%B5%9E%E5%8A%A9-ea4aaa?logo=githubsponsors&logoColor=white"></a>
</p>

<p align="center">
  一个原生菜单栏应用和 iPhone 应用，显示 <a href="https://umami.is">Umami</a> 账户下的网站和访客。基于 <a href="https://github.com/nsgo-dev/nsgo">NSGo</a> 构建。
</p>

<p align="center">
  <img src="screenshots/macos-list-zh-Hans.png" alt="Umapp 在 macOS 上的面板：网站和访客" width="360">
  <img src="screenshots/macos-site-zh-Hans.png" alt="macOS 上的单个网站页面" width="360">
</p>

<p align="center">
  <img src="screenshots/ios-list-zh-Hans.png" alt="iPhone 上的 Umapp" width="280">
  <img src="screenshots/ios-site-zh-Hans.png" alt="iPhone 上的单个网站页面" width="280">
</p>

## 功能

- 从菜单栏打开面板，查看该 API 密钥能看到的每个网站，以及当前有多少人在浏览。
- 按 24 小时、7 天、30 天或 90 天显示访客/浏览和趋势，并与上一段对比。
- 打开单个网站，查看跳出率、访问时长、指针悬停显示数量的图表，以及带图标和国旗的热门页面、来源和国家/地区，可看全部或只看某个标签。
- 连接 Umami Cloud 或自建服务器，并按访客、浏览或名称为网站排序，每个网站都附上图标和域名。
- 在图标旁显示在线访客数，也可以关掉。
- 开机时启动。
- 显示英文或简体中文，默认跟随系统，也可以手动选择。
- 在 macOS 上自动更新，也可以在右键菜单和设置中“检查更新…”。
- 在 iPhone 上运行，面板铺满屏幕。
- 通过 Cloudflare Worker 在浏览器中运行。

## 安装

在 Apple 芯片的 macOS 上，用以下命令安装或更新 Umapp：

```sh
curl -fsSL https://github.com/oneo-me/nsgo-umapp/releases/latest/download/install.sh | sh
```

也可以从 [Releases](https://github.com/oneo-me/nsgo-umapp/releases) 下载磁盘映像。之后 Umapp 会自动保持更新。iPhone 版需按[开发](#开发)一节从源码构建，需要 Go 1.27 或更高版本和 Xcode。

## 快速开始

1. 启动 Umapp。它会在菜单栏添加一个图标。
2. 打开面板，设置 API 地址（默认为 Umami Cloud）和在 *设置 → API 密钥* 中创建的密钥。
3. 浏览网站、切换时间范围，或打开某个网站查看明细。
4. 右键点击图标可以退出。

在 iPhone 和浏览器中，应用直接打开面板。密钥在 macOS 上保存在登录钥匙串中，在 iPhone 上保存在应用自己的存储中，在浏览器中保存在该站点的本地存储中。

## 开发

仓库把每个应用放在 `apps/` 下。Umapp 应用的 Go 模块和打包配置 `nsgo.yaml` 位于 `apps/umapp`。NSGo 来自与本仓库并列的检出，由 `go.mod` 的 replace 指向。请在 `apps/umapp` 下运行 Go 和 NSGo 命令：

```sh
cd apps/umapp
go tool nsgo dev
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target ios
go tool nsgo build
GOWORK=off go test ./...
```

`go tool nsgo test -target macos -run TestScreenshots` 和 `-target ios_simulator` 会用虚构的数据把截图保存到 `.nsgo/test/screenshots/`。

发布时，在 `main` 上把 `apps/umapp/nsgo.yaml` 的版本和 `CHANGELOG.md` 的 `## [Unreleased]` 改为新版本，再推送标签 `vX.Y.Z`。[发布工作流](.github/workflows/release.yml)会构建 macOS 应用，用 `NSGO_UPDATES_KEY` 密钥为自动更新签名，并以该版本的更新记录发布 GitHub Release。

### Cloudflare Workers

`apps/umapp/wrangler.jsonc` 会构建 web 目标并用一个 Worker 提供服务，由它把应用的请求转发给 Umami。默认只连 Umami Cloud；要连自建服务器，把它加到 `UMAMI_ENDPOINTS`，例如 `https://stats.example.com/api`。登录 [Wrangler](https://developers.cloudflare.com/workers/wrangler/) 后：

```sh
cd apps/umapp
wrangler dev
wrangler deploy
```

能打开该站点的任何人都可以用自己的密钥使用它。要只供自己使用，请在前面加上 [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/)。

布局见 [ARCHITECTURE.md](ARCHITECTURE.md)，协作规则见 [AGENTS.md](AGENTS.md)。

变更见 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

以 [MIT License](LICENSE) 授权。
