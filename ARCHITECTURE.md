# Umapp architecture

Umapp is a native macOS menu bar application, built for Apple silicon, an iPhone app and a web app served by a Cloudflare Worker, that reads an Umami account and shows its websites and visitors in a panel. It is a single Go module in `apps/umapp`, built with NSGo's `ui` components in its default theme. Beside it, `apps/server` is a server of its own on Cloudflare Workers and D1, which counts visitors and answers the part of Umami's API the app reads.

## Code map

| Path | Responsibility |
| --- | --- |
| `apps/umapp/go.mod`, `apps/umapp/nsgo.yaml` | The only Go module and the packaging configuration; run Go and the `nsgo` tool in `apps/umapp`. |
| `apps/umapp/main.go` | Entry point; the tray icon, the panel or the iPhone's main view, saving config and the refresh tick. |
| `apps/umapp/model.go` | Panel state: websites, ranges, order, load and connect. |
| `apps/umapp/umami.go` | Umami HTTP client for Cloud and self-hosted servers. |
| `apps/umapp/config.go` | Config and endpoint normalization. |
| `apps/umapp/platform_native.go` | Config file and the macOS keychain; requests straight to Umami. |
| `apps/umapp/platform_js.go` | Config in the site's storage, the browser's time zone, and requests through the Worker. |
| `apps/umapp/wrangler.jsonc`, `apps/umapp/cloudflare/` | The web build's deployment: the build script and the Worker that serves it and forwards requests to Umami. |
| `package.json` | npm workspaces of the TypeScript apps. |
| `apps/server/src/index.ts` | The server's routes on its own hostnames, `/script.js`, `/api/send` and `/api`, and every other hostname to its site. |
| `apps/server/src/inject.ts` | A site whose route leads to the Worker: its website by hostname, `/_umapp/`, and the tracker added to its pages. |
| `apps/server/src/collect.ts` | Events the tracker sends, without bots, keyed by hashed sessions and visits. |
| `apps/server/src/api.ts` | The API key, websites to list, add, edit and delete, and Umami 3's stats, active, pageviews and metrics. |
| `apps/server/src/cloudflare.ts` | `/cloudflare`: the sites of every account the server's Cloudflare token sees, and adding one with its routes. |
| `apps/server/src/relay.ts` | The relay put in another account, adding the server's tracker to that account's sites. |
| `apps/server/src/zone.ts` | The hours and days of a time zone as UTC ranges, which D1 groups series by. |
| `apps/server/src/tracker.ts` | The script websites embed, sending what Umami's does. |
| `apps/server/migrations/` | D1's schema: websites, whether the Worker tracks each through its routes, and their events. |
| `apps/server/scripts/deploy.mjs` | `npm run deploy`: the account, the D1 database and its migrations, the Worker with an API key the first time and a Cloudflare token once, the account's sites to track, and Umapp on this Mac. |
| `apps/server/scripts/sites.mjs` | What `deploy.mjs` decides without I/O: zones, routes, the sites chosen, and the routes and websites that changes. |
| `apps/umapp/view.go` | List, header, summary and sparklines; the shortcuts the panel and the window share. |
| `apps/umapp/window.go` | The main window: the websites in a sidebar beside the page the panel shows, wide. |
| `apps/umapp/detail.go` | One website's tag filter, metrics, chart and breakdowns, with favicons and flags. |
| `apps/umapp/settings.go` | Connect and menu bar options. |
| `apps/umapp/add.go` | Adding a website. |
| `apps/umapp/manage.go` | A website's own page: its name and domain, tracking code, whether its visits arrive, and its deletion. |
| `apps/umapp/screenshots_test.go` | The README's and the website's screenshots, on a made-up account, light and dark. |
| `apps/umapp/locales/` | NSGo message catalogs, and the accessors the `nsgo` tool writes from them. |
| `apps/umapp/assets/` | Icon sources. |
| `.github/workflows/release.yml` | Builds, signs and publishes a release for each version tag. |
| `apps/website/` | The product site, in Astro with Bun, outside the npm workspaces: its words in `src/copy.ts`, its sections in `src/layouts/Home.astro`. |
| `.github/workflows/website.yml` | Builds the site and publishes it to GitHub Pages. |

## Boundaries

- Run network work off the UI thread. Apply results on the main thread and never start that work inside a builder.
- Views follow the state through a revision: actions change plain data and call `invalidate`, and a region builds again only when its key changes.
- Present Umapp as a menu bar app: the tray icon opens the panel on a left click and offers the window and Quit on a right click. The window opens from there, the panel's bar, ⌘0 or a second launch; the app shows in the Dock only while it is open. It follows the same state as the panel, so it shows the same page with a sidebar of the websites, and the panel's pages without their bars, forms centered. Where panels are not available, as on iOS and the web, the panel's content is the main view inside the safe area, without the menu bar's options (`noTray`).
- Keep the API key in the macOS keychain. On iOS, which has no `security` command, it stays in the config file inside the app's sandbox. In a browser it stays in the site's local storage, as NSGo keeps the app's storage there.

## Core flows

**Connect.** Settings sends the endpoint and key to `/websites`. If Umami takes them, they are saved and the list loads. A link `umapp://connect?endpoint=…&key=…`, which `npm run deploy` opens on a Mac, only fills them in and shows settings, marked as from a link: anyone can make one, so nothing connects until the user does. Older versions without the scheme get the endpoint written into `config.json` and the key into the keychain instead.

**Refresh.** Loads every website the key can see, then each one's stats, series and visitors online for the range shown. Visitors online refresh every minute, and on a website's page so do the tags, pages and countries they are on, counted over Umami's same five minutes and marked beside each one's count; the rest every five, or as the panel opens, or the iPhone app becomes active again, on numbers older than a minute.

**Web requests.** In a browser the app sends each request to the Worker's `/umami-api` on its own origin, the Umami URL in `X-Umami-URL`. Browsers refuse Umami's answers to another origin's requests that carry `Authorization`, as its CORS headers allow headers by `*`, which leaves `Authorization` out. The Worker forwards GETs and POSTs of JSON, only to the APIs `UMAMI_ENDPOINTS` lists.

**Manage websites.** Settings list the websites, and a website's page opens its own. Adding posts the name and domain to `/websites`, saving them to `/websites/:id`, and deleting, once confirmed, sends `DELETE /websites/:id`; Umami and Umapp's server take all three. The page shows the tag to embed, Umami Cloud's script for Umami Cloud, else the one served beside the endpoint's `/api`, and whether the website had views in the last 24 hours, which shows the tag at work. A new website opens on its page.

**Server.** `/api/send` takes Umami's tracker events from any origin. A session is a hash of the website, address and browser with a salt that changes each month, and a visit a hash of the session and the hour, so no address is kept. The API answers in Umami 3's form; D1 keeps times in UTC, so series group by ranges `zone.ts` computes for the zone asked for.

**Sites of the account.** `npm run deploy` lists the account's active zones, with Wrangler's login, and the user chooses which to track. For each it makes a website and routes `<host>/*`, plus `www.<host>/*` for a zone's apex, to the Worker, and never touches another Worker's routes. The Worker answers Umapp only on its own hostnames: `*.workers.dev`, its custom domains, which the script passes as `SERVER_HOSTS`, and `localhost`. Any other hostname is a site: if its website exists, the Worker serves the tracker and its events under the site's own `/_umapp/`, forwards everything else to the origin, and adds `<script src="/_umapp/script.js">` to the end of `<head>` of its HTML pages, unless one is there; otherwise the request goes to the origin untouched. Failures fall back to the origin. A site counts as tracked while it has both its routes and its website, so one deleted in Umapp loses its routes at the next deploy. The script marks the websites it tracks (`auto_track`), and Umapp shows them as added automatically instead of offering the tracker to embed. Its API answers carry `X-Umapp-Server`, so Umapp offers no Umami dashboard for its websites.

**Sites from Umapp.** `npm run deploy` asks once for a user API token, from a dashboard link with Zone Read, Workers Routes Edit, Workers Scripts Edit and Account Settings Read on every account filled in, as Wrangler's login can't make one; the Worker keeps it as `CF_API_TOKEN`, with its own account as `CF_ACCOUNT_ID`. On Umapp's server, + then loads `GET /cloudflare`, the zones of every account the token sees, and adds one with `POST /cloudflare/sites`, which makes its website and its routes as the script does. A route leads only to a Worker of its zone's account, so a site of another account routes to `umapp-relay`, which the server uploads there with the domains and website ids of that account's tracked sites; the relay adds the tracker from the server's own address, so events reach it from the visitor's. Deleting a website removes the routes the server or the relay has for it, and nothing else, and uploads the relay again without it; when Cloudflare refuses, the website is still deleted and Umapp shows why. Without a token, `/cloudflare` answers 501 and + says how to give one.

**Tags.** A website's page lists the tags of its events in the range. Choosing one loads that tag's totals, series and breakdowns, while the list keeps every event's; opening a website starts on all of them.

**Order.** Websites with the most visitors come first, or the most views, or by name, as the user chooses.

## Data and state

Preferences, the language among them (empty follows the system), live in `config.json` under `~/Library/Application Support/umapp`, in the app's container on iOS, and under `nsgo:me.oneo.umapp:config` in the site's local storage on the web. The API key is in the login keychain under `umapp: Umami API key`. Whether the app opens at login is the system's, read and changed through NSGo. The list of websites is held in memory only: each launch loads it again. The server keeps its websites and events in D1 and the API key as a Worker secret.

## Subjects

| Document | When to read |
| --- | --- |
| [NSGo's updates](https://github.com/nsgo-dev/nsgo/blob/main/docs/architecture/updates.md) | Changing how releases install or update the app |

## Website

The site is static, in English at its base and Simplified Chinese under `zh/`, light or dark as the system or the visitor chooses. It follows the template of [Agio's site](https://github.com/oneo-me/nsgo-agio/tree/main/website) and ships no UI framework. It shows the screenshots in `screenshots/` rather than copies, and Astro makes their AVIF and WebP versions. `bun run og` redraws the social cards and touch icons in `apps/website/public/` with Chrome; they are committed, so a build needs neither. Production builds use the base `/nsgo-umapp/`; CI sets `SITE_URL` and `BASE_PATH`, which a custom domain overrides.

## Verification

The Umami client, formatting, bucketizing and sort logic are pure and fully testable headless. Localization and the view builders have headless tests. The phone layout has a headless test. The panel and tray need review in a desktop session, the iPhone app in the simulator, and the web app in a browser under `wrangler dev`. The server's tests run in workerd with a migrated D1 through `@cloudflare/vitest-plugin`.
