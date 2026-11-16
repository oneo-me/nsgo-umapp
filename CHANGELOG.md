# Changelog

## [Unreleased]

- Say when deleting a website leaves something of it on Cloudflare, and stop the relay adding its tracker (#19 by @houko).

## [0.2026.4] - 2026-11-16

Umapp adds the websites of your Cloudflare accounts in one tap, with nothing to embed.

- Add a website of any of your Cloudflare accounts from + in one tap, tracked with nothing to embed, once `npm run deploy` gives the server a Cloudflare token (#15 by @houko).
- Right-click a website in the window's sidebar for its settings, the website, its Umami page and deleting it (#17 by @houko).

## [0.2026.3] - 2026-11-16

Umapp opens in a window on macOS, beside its menu bar panel.

- Open a window on macOS with the websites in a sidebar beside each one's page, from the menu bar icon, the panel, ⌘0 or by opening the app again (#11 by @houko).

## [0.2026.2] - 2026-11-16

Umapp gets a server of its own on Cloudflare, which counts the visitors of your websites without Umami, and manages the websites it shows.

- Add a server on Cloudflare Workers and D1, deployed with `npm run deploy`, that counts the visitors of websites for Umapp to connect to as to Umami, and tracks the websites of its own Cloudflare account with nothing to embed (#4, #5, #7, #9 by @houko).
- Connect Umapp on the Mac that deploys through a link that fills in its settings (#8 by @houko).
- Manage websites in the app: add, rename or delete one, copy its tracking code and check that its visits arrive, from settings or its page (#4, #6 by @houko).
- Count Umapp's use on Umami: the web build and product site always, the apps unless turned off in Settings.
- Run in a browser, deployed to Cloudflare Workers (#1 by @houko).
- Add Umapp's product site in English and Simplified Chinese, light and dark, at <https://oneo-me.github.io/nsgo-umapp/>.

## [0.2026.1] - 2026-11-16

Umapp is a native menu bar app for macOS, and an iPhone app, that shows the websites of an Umami account and who is visiting them right now.

- Add a menu bar panel listing every website of an Umami Cloud or self-hosted account with its visitors, views and sparkline over 24 hours to 90 days.
- Add a page for each website with bounce rate, duration, a chart, and its top pages, referrers and countries, for every event or one tag.
- Show visitors online beside the menu bar icon, and on each tag, page and country of a website.
- Show each website's icon and domain, and sort them by visitors, views or name.
- Keep the API key in the macOS keychain, and open at login.
- Run on iPhone, the panel filling the screen.
- Show English or Simplified Chinese, following the system unless chosen in settings.
- Install on macOS with one command, and update automatically or from Check for Updates in the menu and settings.
