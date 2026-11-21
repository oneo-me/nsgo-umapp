# Umapp

Umapp is a native menu bar app, and an iPhone app, that shows the websites of an Umami account and their visitors, with a server of its own on Cloudflare Workers. Read [ARCHITECTURE.md](ARCHITECTURE.md) before changing code.

## Principles

- Keep each app under `apps/`: the Go code, `nsgo.yaml` and runtime payload of the Umapp app in `apps/umapp`; run Go and NSGo commands there. The server lives in `apps/server`; install its packages at the root, which holds the npm workspaces, and run its scripts there. The website lives in `apps/website` and uses Bun on its own, outside the workspaces.
- `apps/umapp/go.mod` pins NSGo to a commit of its `main`; update it with `go get github.com/nsgo-dev/nsgo@main`, and change NSGo itself in its own repository.
- Keep documentation at the repository root.
- Use NSGo's `ui` components and its default theme; build reusable UI in NSGo rather than here.
- Pad every side of a bar, page or row alike: `gutter` for bars and pages, `inset` for rows.
- Keep the server's API in Umami's form for what the app reads, so the app talks to Umami and the server alike.
- Run Umami requests off the UI thread. Apply results on the main thread and never start that work inside a builder.
- Target macOS on Apple silicon (`macos_arm64`), iOS (`ios`) and the web (`web`, deployed to Cloudflare Workers); test iOS on the iPhone 18 Pro simulator.
- Keep the API key in the macOS keychain; on iOS it stays in the app's sandboxed config file, and on the web in the site's local storage.

## Working with the user

- Never create sub-Agents or Git worktrees; work directly in the current checkout.
- Do not commit, tag, push or publish without explicit authorization.

## Verification

Build and test each app from its directory, following [Development](README.md#development). A green headless run does not prove native behavior: review UI changes in a desktop session.

## Documentation

- Update a document only when a fact its reader needs changes.
- Avoid secondary labels and descriptions that restate a heading or value; let the layout explain.
- Keep each document within its budget and written for its reader.
- Record completed user-visible outcomes as one-clause `Unreleased` bullets in [CHANGELOG.md](CHANGELOG.md) with references such as `(#12 by @alice)`, ordered by type and merging related ones.
- Repair a document you touch instead of appending to it.
- Keep `README.md` and `README.zh-CN.md` equivalent; everything else, including code comments, is English.
- Keep documentation on the code branch and reconcile it during merges.
