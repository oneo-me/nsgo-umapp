<p align="center">
  <img src="assets/icon.png" alt="Umapp" width="128">
</p>

<h1 align="center">Umapp</h1>

<p align="center">
  <strong>English</strong> |
  <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/oneo-me"><img alt="GitHub" src="https://img.shields.io/badge/GitHub-oneo--me-181717?logo=github&logoColor=white"></a>
  <a href="https://oneo.me"><img alt="Blog" src="https://img.shields.io/badge/Blog-oneo.me-0969da"></a>
  <a href="https://x.com/oneo666"><img alt="X" src="https://img.shields.io/badge/X-%40oneo666-000000?logo=x&logoColor=white"></a>
  <a href="https://github.com/sponsors/oneo-me"><img alt="GitHub Sponsors" src="https://img.shields.io/badge/GitHub%20Sponsors-ea4aaa?logo=githubsponsors&logoColor=white"></a>
</p>

<p align="center">
  A native menu bar app, and an iPhone app, that shows the websites of an <a href="https://umami.is">Umami</a> account and their visitors. It is built with <a href="https://github.com/nsgo-dev/nsgo">NSGo</a>.
</p>

<p align="center">
  <img src="screenshots/macos-list-en.png" alt="Umapp's panel on macOS: the websites and their visitors" width="360">
  <img src="screenshots/macos-site-en.png" alt="A website's page on macOS" width="360">
</p>

<p align="center">
  <img src="screenshots/ios-list-en.png" alt="Umapp on iPhone" width="280">
  <img src="screenshots/ios-site-en.png" alt="A website's page on iPhone" width="280">
</p>

## Features

- Open a panel from the menu bar with every website the API key can see, and how many people are on them now.
- Show visitors / views and a sparkline for 24 hours, 7 days, 30 days or 90 days, compared with the range before.
- Open one website for bounce rate, duration, a chart with counts under the pointer, and the top pages, referrers and countries with their icons and flags, all of them or of one tag.
- Connect to Umami Cloud or a server of your own, and sort the websites, each with its icon and domain, by visitors, views or name.
- Show the count of visitors online beside the icon, or hide it.
- Open at login.
- Show English or Simplified Chinese, following the system unless you choose one.
- Update on its own on macOS, or from *Check for Updates…* in the right-click menu and settings.
- Run on iPhone, the panel filling the screen.
- Run in a browser from a Cloudflare Worker.

## Install

On macOS with Apple silicon, install or update Umapp with:

```sh
curl -fsSL https://github.com/oneo-me/nsgo-umapp/releases/latest/download/install.sh | sh
```

Or download the disk image from [Releases](https://github.com/oneo-me/nsgo-umapp/releases). Umapp then keeps itself up to date. The iPhone app builds from source with the [Development](#development) commands, which need Go 1.27 or later and Xcode.

## Getting started

1. Start Umapp. It adds an icon to the menu bar.
2. Open the panel and set the API endpoint (Umami Cloud by default) and an API key from *Settings → API keys*.
3. Browse the websites, change the range, or open one for its breakdown.
4. Right-click the icon to quit.

On iPhone and in a browser, the app opens straight to the panel. The key is kept in the login keychain on macOS, in the app's own storage on iPhone, and in the site's local storage in a browser.

## Development

The repository holds each app under `apps/`. The Go module of the Umapp app and its packaging configuration, `nsgo.yaml`, live in `apps/umapp`. NSGo comes from a checkout beside this repository's, which `go.mod` replaces it with. Run Go and NSGo commands from `apps/umapp`:

```sh
cd apps/umapp
go tool nsgo dev
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target ios
go tool nsgo build
GOWORK=off go test ./...
```

`go tool nsgo test -target macos -run TestScreenshots` and `-target ios_simulator` save the screenshots, with made-up data, under `.nsgo/test/screenshots/`.

To release, set `apps/umapp/nsgo.yaml`'s version and turn `CHANGELOG.md`'s `## [Unreleased]` into that version on `main`, then push the tag `vX.Y.Z`. The [release workflow](.github/workflows/release.yml) builds the macOS app, signs it for updates with the `NSGO_UPDATES_KEY` secret and publishes a GitHub release with that version's notes.

### Cloudflare Workers

`apps/umapp/wrangler.jsonc` builds the web target and serves it from a Worker, which forwards the app's requests to Umami. It reaches Umami Cloud; add a server of your own to `UMAMI_ENDPOINTS`, as `https://stats.example.com/api`. With [Wrangler](https://developers.cloudflare.com/workers/wrangler/) signed in:

```sh
cd apps/umapp
wrangler dev
wrangler deploy
```

Anyone who can open the site can use it with their own key. Put [Cloudflare Access](https://developers.cloudflare.com/cloudflare-one/policies/access/) in front to keep it to yourself.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the layout and [AGENTS.md](AGENTS.md) for the working rules.

See [CHANGELOG.md](CHANGELOG.md) for what changed.

## License

Licensed under the [MIT License](LICENSE).
