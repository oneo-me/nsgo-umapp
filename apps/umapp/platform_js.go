//go:build js

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"syscall/js"

	"github.com/nsgo-dev/nsgo/core"
)

// prefsKey is the entry of the app's storage that holds the config.
const prefsKey = "config"

// prefs keeps the config, the API key with it, in the app's storage, which NSGo keeps in the site's localStorage.
type prefs struct {
	st core.Storage
}

func openPrefs(cx *core.Scope) (*prefs, error) {
	return &prefs{st: cx.Storage()}, nil
}

func (p *prefs) load() config {
	cfg := config{Endpoint: cloudEndpoint}
	if raw, ok := p.st.GetItem(prefsKey); ok {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

// save writes cfg on the UI thread, which the storage belongs to.
func (p *prefs) save(cfg config) {
	b, err := json.Marshal(cfg)
	if err != nil {
		log.Print("config: ", err)
		return
	}
	if err := p.st.SetItem(prefsKey, string(b)); err != nil {
		log.Print("config: ", err)
	}
}

// browserZone is the IANA name of the browser's time zone.
func browserZone() string {
	return js.Global().Get("Intl").Call("DateTimeFormat").Call("resolvedOptions").Get("timeZone").String()
}

// proxyPath is where the site's Worker (cloudflare/worker.js) forwards a request to the Umami URL in proxyHeader. Browsers refuse Umami's answers to a request of another origin that carries Authorization, as its CORS headers allow headers by "*", which leaves Authorization out.
const (
	proxyPath   = "/umami-api"
	proxyHeader = "X-Umami-URL"
)

// apiRequest is a GET of u, an Umami endpoint's URL, made through the site's proxy.
func apiRequest(ctx context.Context, u string) (*http.Request, error) {
	origin := js.Global().Get("location").Get("origin").String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+proxyPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(proxyHeader, u)
	return req, nil
}
