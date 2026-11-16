package main

import (
	"net/url"
	"strings"
)

// config is what the app remembers between launches: a file, with the API key in the macOS keychain and elsewhere in the file, readable by the user alone (platform_native.go), or in a browser the site's storage (platform_js.go).
type config struct {
	Endpoint string `json:"endpoint"`
	Key      string `json:"key,omitempty"`
	Range    int    `json:"range"`
	HideLive bool   `json:"hideLive,omitempty"` // no count of live visitors in the menu bar
	// Sort is how the list orders websites: by visitors, views or name.
	Sort int `json:"sort,omitempty"`
	// Language is the catalog the app shows, such as "zh-Hans"; "" follows
	// the system.
	Language string `json:"language,omitempty"`
}

// The orders of the list.
const (
	sortByVisitors = 0
	sortByViews    = 1
	sortByName     = 2
)

// normalizeEndpoint completes what the user typed: a scheme, and the /api
// of a server of their own given by its address alone.
func normalizeEndpoint(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return cloudEndpoint
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	if u.Host == "cloud.umami.is" {
		return cloudEndpoint
	}
	if u.Host != "api.umami.is" && u.Path == "" {
		u.Path = "/api"
	}
	return u.String()
}
