package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type fake struct {
	sites  []website
	scale  map[string]float64
	tags   map[string]float64 // the share of each tag's events
	err    error
	calls  int
	mu     sync.Mutex
	keyErr bool
	// ownServer makes the fake answer as Umapp's own server.
	ownServer bool
	// cf are the Cloudflare accounts' websites the own server lists, and cfErr what listing them fails with.
	cf    []cfSite
	cfErr error
	// deleteWarning is what deleting a website warns of.
	deleteWarning string
}

func newFake() *fake {
	return &fake{
		sites: []website{
			{ID: "a", Name: "Acme Docs", Domain: "docs.acme.dev"},
			{ID: "b", Name: "Personal blog", Domain: "egoist.dev"},
			{ID: "c", Name: "Landing page", Domain: "acme.dev"},
			{ID: "d", Name: "Changelog", Domain: "changelog.acme.dev"},
			{ID: "e", Name: "Status", Domain: "status.acme.dev"},
		},
		scale: map[string]float64{"a": 120, "b": 46, "c": 210, "d": 9, "e": 0},
		tags:  map[string]float64{"website": 0.75, "gallery-web": 0.25},
	}
}

// k is a site's scale, for the events of tag when it is set.
func (f *fake) k(id, tag string) float64 {
	if tag == "" {
		return f.scale[id]
	}
	return f.scale[id] * f.tags[tag]
}

func (f *fake) websites(context.Context) ([]website, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.keyErr {
		return nil, &apiError{status: 401}
	}
	return f.sites, f.err
}

func (f *fake) stats(_ context.Context, id string, from, to time.Time, tag string) (cur, prev stats, err error) {
	k := f.k(id, tag) * to.Sub(from).Hours() / 24
	cur = stats{Pageviews: num(k * 3.1), Visitors: num(k), Visits: num(k * 1.3), Bounces: num(k * 0.55), Totaltime: num(k * 1.3 * 94)}
	p := k * (0.8 + 0.1*float64(len(id)+int(id[0])%4))
	prev = stats{Pageviews: num(p * 3), Visitors: num(p), Visits: num(p * 1.25), Bounces: num(p * 0.6), Totaltime: num(p * 1.25 * 80)}
	return cur, prev, nil
}

func (f *fake) active(_ context.Context, id string) (int, error) {
	return int(f.scale[id] / 30), nil
}

func (f *fake) series(_ context.Context, id string, from, to time.Time, unit, _, tag string) (views, visitors []point, err error) {
	step := 24 * time.Hour
	if unit == "hour" {
		step = time.Hour
	}
	for t, i := from, 0; !t.After(to); t, i = t.Add(step), i+1 {
		v := f.k(id, tag) / 24
		if unit == "day" {
			v = f.k(id, tag) * (1 + 0.25*math.Sin(float64(i)/2.3))
		} else {
			v *= 1 + 0.8*math.Sin(float64(t.Hour()-9)/24*2*math.Pi)
		}
		v = math.Round(v)
		if v <= 0 {
			continue
		}
		x := t.Format("2006-01-02 15:04:05")
		visitors = append(visitors, point{X: x, Y: num(v)})
		views = append(views, point{X: x, Y: num(v * 2.9)})
	}
	return views, visitors, nil
}

func (f *fake) metrics(_ context.Context, id, kind string, from, to time.Time, limit int, tag string) ([]metric, error) {
	k := f.k(id, tag)
	if to.Sub(from) <= liveWindow {
		// Who is on the site now: its visitors online.
		k = math.Floor(f.k(id, tag) / 30)
	}
	var rows []metric
	switch kind {
	case "path":
		rows = []metric{{"/", num(k * 2)}, {"/docs/getting-started", num(k * 1.1)}, {"/pricing", num(k * 0.6)}, {"/blog/introducing-acme-2", num(k * 0.3)}, {"/about", num(k * 0.1)}}
	case "referrer":
		rows = []metric{{"", num(k)}, {"www.google.com", num(k * 0.7)}, {"github.com", num(k * 0.4)}, {"news.ycombinator.com", num(k * 0.2)}}
	case "country":
		rows = []metric{{"US", num(k * 0.9)}, {"DE", num(k * 0.4)}, {"JP", num(k * 0.3)}, {"CN", num(k * 0.2)}}
	case "tag":
		rows = []metric{{"website", num(k * f.tags["website"])}, {"gallery-web", num(k * f.tags["gallery-web"])}, {"", num(k * 0.1)}}
	}
	if k == 0 {
		rows = nil
	}
	return rows[:min(limit, len(rows))], nil
}

func (f *fake) addWebsite(_ context.Context, name, domain string) (website, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return website{}, f.err
	}
	w := website{ID: fmt.Sprintf("new%d", len(f.sites)), Name: name, Domain: domain}
	f.sites = append(f.sites, w)
	f.scale[w.ID] = 0
	return w, nil
}

func (f *fake) updateWebsite(_ context.Context, id, name, domain string) (website, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return website{}, f.err
	}
	for i := range f.sites {
		if f.sites[i].ID == id {
			f.sites[i].Name, f.sites[i].Domain = name, domain
			return f.sites[i], nil
		}
	}
	return website{}, &apiError{status: 404}
}

func (f *fake) deleteWebsite(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.sites = slices.DeleteFunc(f.sites, func(w website) bool { return w.ID == id })
	return f.deleteWarning, nil
}

func (f *fake) hasDashboard() bool { return !f.ownServer }

func (f *fake) cloudflareSites(context.Context) ([]cfSite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cf), f.cfErr
}

func (f *fake) trackSite(_ context.Context, domain string) (website, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return website{}, f.err
	}
	for i := range f.cf {
		if f.cf[i].Domain == domain {
			f.cf[i].Tracked = true
		}
	}
	w := website{ID: fmt.Sprintf("new%d", len(f.sites)), Name: domain, Domain: domain, AutoTrack: true}
	f.sites = append(f.sites, w)
	f.scale[w.ID] = 0
	return w, nil
}

func testApp(t *testing.T, f *fake) *app {
	t.Helper()
	cfg := config{Endpoint: cloudEndpoint, Key: "api_test"}
	var src source
	if f != nil {
		src = f
	}
	a := newApp(cfg, src)
	if f == nil {
		a.cfg.Key = ""
		a.page = pageSettings
		a.resetSettings()
	}
	loc := time.FixedZone("CST", 8*3600)
	a.now = func() time.Time { return time.Date(2026, 10, 12, 15, 20, 0, 0, loc) }
	return a
}

type tester struct {
	*nsgotest.App
	a *app
}

func build(t *testing.T, a *app) *tester {
	t.Helper()
	return &tester{App: nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale("en")
		a.attach(cx)
		return a.root(cx)
	}, nsgotest.Size(panelWidth, panelHeight)), a: a}
}

func (tt *tester) hasText(s string) bool {
	for _, text := range tt.Texts() {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

func names(sites []*site) []string {
	var out []string
	for _, s := range sites {
		out = append(out, s.Name)
	}
	return out
}

func TestList(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	if v := a.ordered(); v[0].ID != "c" || v[len(v)-1].ID != "e" {
		t.Errorf("order %v", []string{v[0].ID, v[len(v)-1].ID})
	}
	if got := a.liveTotal(); got != 4+1+7 {
		t.Errorf("live %d", got)
	}
	tt := build(t, a)
	for _, s := range []string{"Acme Docs", "Personal blog", "Landing page", "Visitors"} {
		if !tt.hasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
}

func TestRange(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := build(t, a)
	tt.Press("7D")
	tt.WaitUntil(2*time.Second, func() bool { return a.cfg.Range == 1 && a.loading == 0 })
	if a.cfg.Range != 1 || len(a.buckets) != 7 {
		t.Fatalf("range %d, %d buckets", a.cfg.Range, len(a.buckets))
	}
	tt.Key("mod+3")
	tt.WaitUntil(2*time.Second, func() bool { return a.cfg.Range == 2 && a.loading == 0 })
	if a.cfg.Range != 2 || len(a.site("a").visitors) != 30 {
		t.Errorf("range %d after Cmd+3", a.cfg.Range)
	}
}

func TestDetail(t *testing.T) {
	a := testApp(t, newFake())
	a.cfg.Range = 1
	a.refresh()
	tt := build(t, a)
	tt.Press("Acme Docs")
	tt.WaitUntil(2*time.Second, func() bool { return a.page == pageSite && a.loading == 0 })
	if a.page != pageSite || a.open != "a" {
		t.Fatalf("page %v %q", a.page, a.open)
	}
	for _, s := range []string{"Bounce rate", "Visit duration", "/docs/getting-started", "Direct", "google.com", "United States"} {
		if !tt.hasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	tt.Press("Open in Umami")
	if got := tt.OpenedURLs; len(got) != 1 || got[0] != "https://cloud.umami.is/websites/a" {
		t.Errorf("opened %q", got)
	}
	tt.Key("escape")
	if a.page != pageSites {
		t.Errorf("page %v after Escape", a.page)
	}
}

func TestOnlineNow(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("Acme Docs")
	s := a.site("a")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 && s.livePages != nil })
	if online(s.livePages, "/") != 8 || online(s.liveTags, "website") != 3 || online(s.liveCountries, "US") != 3 {
		t.Fatalf("pages %v, tags %v, countries %v", s.livePages, s.liveTags, s.liveCountries)
	}
	// Each with its visitors online over its count: tags, pages, countries.
	for _, text := range []string{"/117", "/90", "/240", "/108"} {
		if !tt.hasText(text) {
			t.Errorf("no %q in %q", text, tt.Texts())
		}
	}
	tt.Press("tag-gallery-web")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 && s.liveTag == "gallery-web" })
	if online(s.livePages, "/") != 2 || online(s.liveTags, "website") != 3 {
		t.Errorf("pages %v not filtered, or tags %v", s.livePages, s.liveTags)
	}
}

func TestWithOnline(t *testing.T) {
	got := withOnline([]metric{{"/", 9}}, []metric{{"/", 2}, {"/new", 1}, {"/gone", 0}})
	if want := []metric{{"/", 9}, {"/new", -1}}; !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestTagFilter(t *testing.T) {
	a := testApp(t, newFake())
	a.cfg.Range = 1
	a.refresh()
	tt := build(t, a)
	tt.Press("Acme Docs")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 && len(a.site("a").tags) > 0 })
	s := a.site("a")
	if got := len(s.tags); got != 2 || !tt.hasText("gallery-web") || !tt.hasText("All") || !tt.hasText("90") {
		t.Fatalf("%d tags, texts %q", got, tt.Texts())
	}
	all := a.shown(s).cur.Visitors
	tt.Press("tag-gallery-web")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 && s.tagged.loaded })
	if got := a.shown(s).cur.Visitors; a.tag != "gallery-web" || got != all*0.25 {
		t.Fatalf("tag %q, visitors %v of %v", a.tag, got, all)
	}
	if s.pages[0].Y != num(120*0.25*2) {
		t.Errorf("pages %v not filtered", s.pages)
	}
	tt.Press("tag-all")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 })
	if a.tag != "" || a.shown(s).cur.Visitors != all || s.pages[0].Y != 240 {
		t.Errorf("tag %q, visitors %v after All", a.tag, a.shown(s).cur.Visitors)
	}
	tt.Key("escape")
	tt.Press("Personal blog")
	if a.tag != "" {
		t.Errorf("tag %q carried to another site", a.tag)
	}
}

func TestOnboarding(t *testing.T) {
	a := testApp(t, nil)
	tt := build(t, a)
	if !tt.hasText("Connect to Umami") {
		t.Fatalf("texts %q", tt.Texts())
	}

	f := newFake()
	f.keyErr = true
	connect := func() { a.connect(func(string, string) source { return f }) }
	a.editKey = "api_wrong"
	connect()
	tt.WaitUntil(2*time.Second, func() bool { return a.connectErr != "" })
	if a.configured() || !tt.hasText("The API key was rejected.") {
		t.Fatalf("configured %v, texts %q", a.configured(), tt.Texts())
	}

	f.keyErr = false
	saved := 0
	a.changed = func(config) { saved++ }
	a.editKey = "api_right"
	connect()
	tt.WaitUntil(2*time.Second, func() bool { return a.configured() && a.loading == 0 })
	if !a.configured() || a.page != pageSites || saved == 0 || len(a.sites) != 5 {
		t.Fatalf("configured %v, page %v, saved %d", a.configured(), a.page, saved)
	}
}

func TestSettings(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	if a.page != pageSettings {
		t.Fatalf("page %v", a.page)
	}
	if tt.hasText("by visitors") {
		t.Error("settings show the list")
	}
	tt.Press("back")
	if !tt.hasText("Landing page") || !tt.hasText("acme.dev") {
		t.Error("the list misses a website or its domain")
	}
}

func TestLaunchAtLogin(t *testing.T) {
	a := testApp(t, newFake())
	var set []bool
	a.canLaunchAtLogin = true
	a.readLogin = func(done func(bool)) { done(true) }
	a.setLogin = func(on bool) { set = append(set, on) }
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	if !a.launchAtLogin {
		t.Fatal("the system's setting is not read")
	}
	tt.Press("Open at login")
	if a.launchAtLogin || !slices.Equal(set, []bool{false}) {
		t.Fatalf("on %v, set %v", a.launchAtLogin, set)
	}
}

func TestAnalytics(t *testing.T) {
	a := testApp(t, newFake())
	var sent []string
	a.sendView = func(path string) { sent = append(sent, path) }
	a.tracked = -1
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	tt.Press("analytics")
	if !a.cfg.NoAnalytics {
		t.Fatal("the switch leaves analytics on")
	}
	tt.Press("back")
	tt.Press("settings")
	tt.Press("analytics")
	tt.Press("back")
	if want := []string{"/", "/settings", "/settings", "/"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %q, want %q", sent, want)
	}
}

func TestPhone(t *testing.T) {
	a := testApp(t, newFake())
	a.noTray = true
	a.refresh()
	tt := &tester{App: nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale("en")
		a.attach(cx)
		return a.root(cx)
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSIOS, Idiom: core.IdiomPhone, Locale: "en-US"})), a: a}
	if !tt.hasText("Acme Docs") {
		t.Fatalf("texts %q", tt.Texts())
	}
	tt.Press("settings")
	if tt.hasText("Visitors online") || tt.hasText("Check for Updates…") {
		t.Errorf("settings offer the menu bar's options: %q", tt.Texts())
	}
	if !tt.hasText("Language") || !tt.hasText("Disconnect") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestLanguage(t *testing.T) {
	a := testApp(t, newFake())
	var saved config
	a.changed = func(cfg config) { saved = cfg }
	a.refresh()
	tt := &tester{App: nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a.attach(cx)
		return a.root(cx)
	}, nsgotest.Environment(core.Environment{Locale: "en-US"})), a: a}
	tt.Press("settings")
	tt.Press("language")
	tt.Press("language-2")
	if saved.Language != "zh-Hans" || !tt.hasText("语言") {
		t.Fatalf("language %q, texts %q", saved.Language, tt.Texts())
	}
	tt.Press("language")
	tt.Press("language-0")
	if saved.Language != "" || !tt.hasText("Language") {
		t.Fatalf("language %q, texts %q", saved.Language, tt.Texts())
	}
}

func TestErrors(t *testing.T) {
	f := newFake()
	f.err = errors.New("Couldn't reach the server.")
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	if !tt.hasText("Couldn't load your websites") {
		t.Fatalf("texts %q", tt.Texts())
	}
	f.err = nil
	tt.Press("retry")
	tt.WaitUntil(2*time.Second, func() bool { return a.loading == 0 && len(a.sites) > 0 })
	if !tt.hasText("Acme Docs") {
		t.Errorf("texts %q after retrying", tt.Texts())
	}
}

func TestBucketize(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 12, 15, 20, 0, 0, loc)
	from, _, buckets := spans[0].window(now)
	if len(buckets) != 24 || !from.Equal(time.Date(2026, 10, 11, 16, 0, 0, 0, loc)) {
		t.Fatalf("from %v, %d buckets", from, len(buckets))
	}
	got := bucketize([]point{
		{X: "2026-10-12 15:00:00", Y: 3},
		{X: "2026-10-11T16:00:00+08:00", Y: 2},
		{X: "2026-10-12T06:00:00Z", Y: 5},
		{X: "nonsense", Y: 9},
	}, buckets, true)
	if got[23] != 3 || got[0] != 2 || got[22] != 5 {
		t.Errorf("buckets %v", got)
	}
	_, _, days := spans[1].window(now)
	if len(days) != 7 || days[6].Day() != 12 || days[0].Day() != 6 {
		t.Errorf("days %v", days)
	}
}

func TestFormat(t *testing.T) {
	for v, want := range map[float64]string{0: "0", 999: "999", 9999: "9,999", 12_480: "12.4k", 123_456: "123k", 1_250_000: "1.2M"} {
		if got := count(v); got != want {
			t.Errorf("count(%v) = %q, want %q", v, got, want)
		}
	}
	for d, want := range map[time.Duration]string{48 * time.Second: "48s", 125 * time.Second: "2m 05s", 72 * time.Minute: "1h 12m"} {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
	for in, want := range map[string]string{
		"":                               cloudEndpoint,
		"cloud.umami.is":                 cloudEndpoint,
		"umami.example.com":              "https://umami.example.com/api",
		"https://umami.example.com/api/": "https://umami.example.com/api",
		"https://api.umami.is/v1":        cloudEndpoint,
		"http://localhost:3000":          "http://localhost:3000/api",
	} {
		if got := normalizeEndpoint(in); got != want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
	if got := dashboardURL("https://umami.example.com/api", "x"); got != "https://umami.example.com/websites/x" {
		t.Errorf("dashboard %q", got)
	}
	if !strings.HasPrefix(percent(0.082), "8.2") {
		t.Errorf("percent %q", percent(0.082))
	}
}

func TestStatsShapes(t *testing.T) {
	var cur stats
	if err := json.Unmarshal([]byte(`{"pageviews":"12","visitors":5,"visits":6,"bounces":3,"totaltime":600}`), &cur); err != nil || cur.Pageviews != 12 || cur.Visitors != 5 {
		t.Errorf("v3 %+v %v", cur, err)
	}
	var old stats
	if err := json.Unmarshal([]byte(`{"pageviews":{"value":10,"prev":8},"visitors":{"value":4,"prev":2}}`), &old); err != nil || old.Pageviews != 10 || old.Visitors != 4 {
		t.Errorf("v2 %+v %v", old, err)
	}
}

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/websites":
			w.Write([]byte(`{"data":[{"id":"a","name":"A","domain":"a.dev"}],"count":1,"page":1,"pageSize":100}`))
		case "/api/websites/a/stats":
			if r.URL.Query().Get("startAt") == "" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"pageviews":30,"visitors":10,"visits":12,"bounces":6,"totaltime":1200,"comparison":{"pageviews":20,"visitors":8,"visits":9,"bounces":5,"totaltime":900}}`))
		case "/api/websites/a/active":
			w.Write([]byte(`{"visitors":3}`))
		case "/api/websites/a/pageviews":
			if r.URL.Query().Get("tag") != "app" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"pageviews":[{"x":"2026-10-12 15:00:00","y":4}],"sessions":[]}`))
		case "/api/websites/a/metrics":
			if r.URL.Query().Get("type") == "path" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`[{"x":"/","y":5}]`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	cl := newClient(srv.URL+"/api/", "k")
	list, err := cl.websites(ctx)
	if err != nil || len(list) != 1 || list[0].Domain != "a.dev" {
		t.Fatalf("websites %v %v", list, err)
	}
	cur, prev, err := cl.stats(ctx, "a", time.Now().Add(-time.Hour), time.Now(), "")
	if err != nil || cur.Visitors != 10 || prev.Visitors != 8 || cur.visitDuration() != 100*time.Second {
		t.Errorf("stats %+v %+v %v", cur, prev, err)
	}
	if n, err := cl.active(ctx, "a"); n != 3 || err != nil {
		t.Errorf("active %d %v", n, err)
	}
	if m, err := cl.metrics(ctx, "a", "path", time.Now(), time.Now(), 5, ""); len(m) != 1 || err != nil {
		t.Errorf("metrics %v %v", m, err)
	}
	if v, _, err := cl.series(ctx, "a", time.Now(), time.Now(), "hour", "", "app"); len(v) != 1 || err != nil {
		t.Errorf("tagged series %v %v", v, err)
	}
	_, err = newClient(srv.URL+"/api", "wrong").websites(ctx)
	if err == nil || err.Error() != "The API key was rejected." {
		t.Errorf("wrong key: %v", err)
	}
}

func TestSort(t *testing.T) {
	a := testApp(t, newFake())
	saved := 0
	a.changed = func(config) { saved++ }
	a.refresh()
	if got := names(a.ordered()); got[0] != "Landing page" || got[len(got)-1] != "Status" {
		t.Errorf("by visitors %q", got)
	}
	a.setSort(sortByName)
	got := names(a.ordered())
	want := []string{"Acme Docs", "Changelog", "Landing page", "Personal blog", "Status"}
	if !slices.Equal(got, want) || a.cfg.Sort != sortByName || saved == 0 {
		t.Fatalf("by name %q, saved %d", got, saved)
	}
	a.setSort(sortByVisitors)
	if a.cfg.Sort != sortByVisitors || a.ordered()[0].Name != "Landing page" {
		t.Errorf("by visitors again %q", names(a.ordered()))
	}
	a.site("a").cur.Pageviews = 1e6
	a.setSort(sortByViews)
	if got := names(a.ordered()); a.cfg.Sort != sortByViews || got[0] != "Acme Docs" || got[1] != "Landing page" {
		t.Errorf("by views %q", got)
	}
}

func TestFaviconHost(t *testing.T) {
	for in, want := range map[string]string{
		"Acme.dev": "acme.dev", "https://www.google.com/search": "www.google.com", "": "", "localhost": "",
	} {
		if got := faviconHost(in); got != want {
			t.Errorf("faviconHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrayIcon(t *testing.T) {
	b := trayIcon()
	if len(b) < 100 {
		t.Fatalf("icon %d bytes", len(b))
	}
}

func TestCheckForUpdates(t *testing.T) {
	a := testApp(t, newFake())
	checks := 0
	a.checkUpdates = func() { checks++ }
	a.refresh()
	tt := &tester{App: nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale("en")
		a.attach(cx)
		return a.root(cx)
	}), a: a}
	tt.Press("settings")
	tt.Press("check-updates")
	if checks != 1 {
		t.Fatalf("checks %d", checks)
	}
}

func TestAddWebsite(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("add")
	if a.page != pageAdd {
		t.Fatalf("page %v", a.page)
	}
	tt.Type("domain", "https://Shop.Acme.dev/cart")
	tt.Press("add-website")
	tt.WaitUntil(5*time.Second, func() bool { return a.page == pageManage && !a.checking && a.loading == 0 })
	if a.page != pageManage || a.managed.Domain != "shop.acme.dev" || a.managed.Name != "shop.acme.dev" {
		t.Fatalf("page %v, managed %+v", a.page, a.managed)
	}
	want := `<script defer src="https://cloud.umami.is/script.js" data-website-id="` + a.managed.ID + `"></script>`
	if !tt.hasText(want) {
		t.Errorf("no tracking code in %q", tt.Texts())
	}
	if !tt.hasText("No data yet") {
		t.Errorf("a new website is said to send data: %q", tt.Texts())
	}
	tt.Press("copy")
	if tt.Clipboard != want {
		t.Errorf("clipboard %q", tt.Clipboard)
	}
	if !tt.hasText("Copied") {
		t.Error("no sign of the copy")
	}
	tt.Press("back")
	if a.page != pageSites || !tt.hasText("shop.acme.dev") {
		t.Errorf("page %v, texts %q", a.page, tt.Texts())
	}
}

func TestAddWebsiteError(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("add")
	f.err = &apiError{status: 400, msg: "The domain is missing or isn't a domain."}
	tt.Type("domain", "example.com")
	tt.Press("add-website")
	tt.WaitUntil(5*time.Second, func() bool { return !a.adding })
	if a.page != pageAdd || !tt.hasText("The domain is missing") {
		t.Errorf("page %v, texts %q", a.page, tt.Texts())
	}
}

func TestManageWebsite(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	a.openSite("a")
	tt.Settle()
	tt.Press("manage")
	if a.page != pageManage || a.managed.ID != "a" {
		t.Fatalf("page %v, managed %+v", a.page, a.managed)
	}
	tt.WaitUntil(5*time.Second, func() bool { return a.checked })
	if !tt.hasText("Receiving data") || !tt.hasText("in the last 24 hours") {
		t.Errorf("no install status in %q", tt.Texts())
	}
	tt.Press("copy")
	if !strings.Contains(tt.Clipboard, `data-website-id="a"`) {
		t.Errorf("clipboard %q", tt.Clipboard)
	}
	tt.Type("site-name", " Docs")
	tt.Press("save-site")
	tt.WaitUntil(5*time.Second, func() bool { return !a.saving })
	if f.sites[0].Name != "Acme Docs Docs" || a.site("a").Name != "Acme Docs Docs" {
		t.Errorf("saved %q, shown %q", f.sites[0].Name, a.site("a").Name)
	}
	tt.Press("back")
	if a.page != pageSite || a.open != "a" {
		t.Errorf("back to page %v, site %q", a.page, a.open)
	}
}

func TestDeleteWebsite(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	tt.Press("manage-b")
	if a.page != pageManage || a.managed.ID != "b" {
		t.Fatalf("page %v, managed %+v", a.page, a.managed)
	}
	var asked []string
	tt.OnAlert = func(c core.AlertConfig) int { asked = append(asked, c.Title); return 0 } // Cancel
	tt.Press("delete-site")
	tt.Settle()
	if len(asked) != 1 || !strings.Contains(asked[0], "Personal blog") || a.site("b") == nil {
		t.Fatalf("asked %q, site %v", asked, a.site("b"))
	}
	tt.OnAlert = func(core.AlertConfig) int { return 1 } // Delete
	tt.Press("delete-site")
	tt.WaitUntil(5*time.Second, func() bool { return !a.deleting && a.loading == 0 })
	if a.site("b") != nil || slices.ContainsFunc(f.sites, func(w website) bool { return w.ID == "b" }) {
		t.Error("the website is still there")
	}
	if a.page != pageSettings || tt.hasText("Personal blog") {
		t.Errorf("page %v, texts %q", a.page, tt.Texts())
	}
}

func TestNoWebsites(t *testing.T) {
	f := newFake()
	f.sites = nil
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	if !tt.hasText("No websites yet") {
		t.Fatalf("texts %q", tt.Texts())
	}
	tt.Press("empty-add")
	if a.page != pageAdd {
		t.Errorf("page %v", a.page)
	}
}

func TestSettingsListsWebsites(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	for _, s := range []string{"Acme Docs", "docs.acme.dev", "Status", "Add website"} {
		if !tt.hasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	tt.Press("settings-add")
	if a.page != pageAdd {
		t.Errorf("page %v", a.page)
	}
}

func TestTrackingCode(t *testing.T) {
	for endpoint, src := range map[string]string{
		cloudEndpoint:                           "https://cloud.umami.is/script.js",
		"https://stats.example.com/api":         "https://stats.example.com/script.js",
		"https://example.com/umami/api":         "https://example.com/umami/script.js",
		"https://umapp.example.workers.dev/api": "https://umapp.example.workers.dev/script.js",
	} {
		want := `<script defer src="` + src + `" data-website-id="w"></script>`
		if got := trackingCode(endpoint, "w"); got != want {
			t.Errorf("%s: %s", endpoint, got)
		}
	}
}

func TestBareDomain(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":                  "example.com",
		" https://Blog.Example.com/a ": "blog.example.com",
		"example.com:8080/x":           "example.com",
		"":                             "",
	} {
		if got := bareDomain(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestClientAddWebsite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if r.Method != http.MethodPost || r.URL.Path != "/api/websites" || r.Header.Get("Authorization") != "Bearer k" ||
			r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil {
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(website{ID: "n", Name: body["name"], Domain: body["domain"]})
	}))
	defer srv.Close()
	got, err := newClient(srv.URL+"/api", "k").addWebsite(context.Background(), "Shop", "shop.acme.dev")
	if err != nil || got != (website{ID: "n", Name: "Shop", Domain: "shop.acme.dev"}) {
		t.Errorf("%+v, %v", got, err)
	}
}

func TestDeleteFromItsPage(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := build(t, a)
	a.openSite("c")
	tt.Settle()
	tt.Press("manage")
	tt.OnAlert = func(core.AlertConfig) int { return 1 }
	tt.Press("delete-site")
	tt.WaitUntil(5*time.Second, func() bool { return !a.deleting && a.loading == 0 })
	if a.page != pageSites || a.site("c") != nil {
		t.Errorf("page %v, site %v", a.page, a.site("c"))
	}
}

func TestClientUpdateDeleteWebsite(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(website{ID: "w", Name: body["name"], Domain: body["domain"]})
		case http.MethodDelete:
			// Umami answers a deletion with no body.
		}
	}))
	defer srv.Close()
	cl := newClient(srv.URL+"/api", "k")
	got, err := cl.updateWebsite(context.Background(), "w", "Shop", "shop.acme.dev")
	if err != nil || got != (website{ID: "w", Name: "Shop", Domain: "shop.acme.dev"}) {
		t.Errorf("%+v, %v", got, err)
	}
	if warning, err := cl.deleteWebsite(context.Background(), "w"); err != nil || warning != "" {
		t.Error(warning, err)
	}
	if want := []string{"POST /api/websites/w", "DELETE /api/websites/w"}; !slices.Equal(calls, want) {
		t.Errorf("calls %q", calls)
	}
}

func TestManageAutoTrackedWebsite(t *testing.T) {
	f := newFake()
	f.sites[0].AutoTrack = true
	f.scale["a"] = 0
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("settings")
	tt.Press("manage-a")
	tt.WaitUntil(5*time.Second, func() bool { return a.checked })
	if !tt.hasText("Added automatically") || tt.hasText("<script") {
		t.Errorf("texts %q", tt.Texts())
	}
	if !tt.hasText("Visit the website once.") {
		t.Errorf("no hint for an automatic website in %q", tt.Texts())
	}
}

func TestWebsiteAutoTrackDecodes(t *testing.T) {
	var w website
	if err := json.Unmarshal([]byte(`{"id":"a","name":"A","domain":"a.dev","autoTrack":true}`), &w); err != nil || !w.AutoTrack {
		t.Errorf("%+v, %v", w, err)
	}
	var umami website
	if err := json.Unmarshal([]byte(`{"id":"b","name":"B","domain":"b.dev"}`), &umami); err != nil || umami.AutoTrack {
		t.Errorf("Umami's website reads as automatic: %+v, %v", umami, err)
	}
}

func TestOpenConnectLink(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := build(t, a)
	if !a.openLink("umapp://connect?endpoint=https%3A%2F%2Fumapp-server.example.workers.dev%2Fapi&key=umapp_abc") {
		t.Fatal("the link was refused")
	}
	tt.Settle()
	if a.page != pageSettings || a.editEndpoint != "https://umapp-server.example.workers.dev/api" || a.editKey != "umapp_abc" {
		t.Fatalf("page %v, endpoint %q, key %q", a.page, a.editEndpoint, a.editKey)
	}
	if a.cfg.Key != "api_test" || a.cfg.Endpoint != cloudEndpoint {
		t.Errorf("a link changed the saved settings: %+v", a.cfg)
	}
	if !tt.hasText("Filled in from a link") || !tt.hasText("Save") {
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Press("back")
	if a.fromLink || a.editKey != "api_test" {
		t.Errorf("leaving settings kept the link: fromLink %v, key %q", a.fromLink, a.editKey)
	}
}

func TestOpenLinkRefusesOthers(t *testing.T) {
	a := testApp(t, newFake())
	for _, link := range []string{
		"https://connect?endpoint=https://x.example/api&key=k",
		"umapp://settings",
		"umapp://connect?endpoint=https://x.example/api",
		"umapp://connect?endpoint=javascript:alert(1)&key=k",
		"umapp://connect?endpoint=file:///etc/passwd&key=k",
		"umapp://connect?key=k",
	} {
		if a.openLink(link) {
			t.Errorf("took %q", link)
		}
	}
	if a.fromLink || a.page != pageSites {
		t.Errorf("a refused link changed the state: page %v", a.page)
	}
}

func TestNoDashboardOnOwnServer(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	a.openSite("a")
	tt.Settle()
	if len(tt.Query("open")) == 0 {
		t.Fatalf("Umami's dashboard isn't offered")
	}
	f.ownServer = true
	a.back()
	tt.Settle()
	a.openSite("b")
	tt.Settle()
	if len(tt.Query("open")) != 0 {
		t.Error("Umapp's own server is offered a dashboard")
	}
}

func TestClientKnowsOwnServer(t *testing.T) {
	own := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if own {
			w.Header().Set("X-Umapp-Server", "1")
		}
		w.Write([]byte(`{"data":[],"count":0}`))
	}))
	defer srv.Close()
	cl := newClient(srv.URL+"/api", "k")
	if !cl.hasDashboard() {
		t.Error("a client that heard nothing yet has no dashboard")
	}
	if _, err := cl.websites(context.Background()); err != nil || cl.hasDashboard() {
		t.Errorf("own server not recognized: %v", err)
	}
	own = false
	umami := newClient(srv.URL+"/api", "k")
	if _, err := umami.websites(context.Background()); err != nil || !umami.hasDashboard() {
		t.Errorf("Umami taken for Umapp's server: %v", err)
	}
}

func buildWindow(t *testing.T, a *app) *tester {
	t.Helper()
	return &tester{App: nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale("en")
		a.attach(cx)
		return a.windowRoot(cx)
	}, nsgotest.Size(windowWidth, windowHeight)), a: a}
}

func TestWindowShowsWebsitesBesideTheirPages(t *testing.T) {
	a := testApp(t, newFake())
	a.refresh()
	tt := buildWindow(t, a)
	for _, s := range []string{"Overview", "Acme Docs", "Landing page", "Visitors / Views"} {
		if !tt.hasText(s) {
			t.Errorf("no %q in %q", s, tt.Texts())
		}
	}
	tt.Press("window-site-b")
	tt.WaitUntil(5*time.Second, func() bool { return a.page == pageSite && a.loading == 0 })
	if a.open != "b" || len(tt.Query("detail-b")) == 0 {
		t.Fatalf("page %v, open %q", a.page, a.open)
	}
	if !tt.hasText("Personal blog") || !tt.hasText("Pages") {
		t.Errorf("the site's page is missing: %q", tt.Texts())
	}
	tt.Press("window-manage")
	if a.page != pageManage || a.managed.ID != "b" || len(tt.Query("manage")) == 0 {
		t.Errorf("manage: page %v, managed %+v", a.page, a.managed)
	}
	tt.Press("window-overview")
	if a.page != pageSites || len(tt.Query("sites")) == 0 {
		t.Errorf("overview: page %v", a.page)
	}
	tt.Press("window-settings")
	if a.page != pageSettings || len(tt.Query("settings")) == 0 {
		t.Errorf("settings: page %v", a.page)
	}
	tt.Press("window-add")
	if a.page != pageAdd || len(tt.Query("add")) == 0 {
		t.Errorf("add: page %v", a.page)
	}
}

func TestWindowBeforeConnectingShowsTheConnectForm(t *testing.T) {
	a := testApp(t, nil)
	tt := buildWindow(t, a)
	if len(tt.Query("key")) == 0 || !tt.hasText("Connect to Umami") {
		t.Errorf("texts %q", tt.Texts())
	}
	if tt.hasText("Overview") {
		t.Error("the sidebar lists websites before connecting")
	}
}

func TestPanelOffersTheWindow(t *testing.T) {
	a := testApp(t, newFake())
	opened := 0
	a.openWindow = func() { opened++ }
	a.refresh()
	tt := build(t, a)
	tt.Press("open-window")
	if opened != 1 {
		t.Errorf("opened %d", opened)
	}
	b := testApp(t, newFake())
	b.refresh()
	if len(build(t, b).Query("open-window")) != 0 {
		t.Error("a panel without a window offers one")
	}
}

func TestAddFromCloudflare(t *testing.T) {
	f := newFake()
	f.ownServer = true
	f.cf = []cfSite{
		{Domain: "acme.dev", Account: "Acme", Tracked: true},
		{Domain: "msime.app", Account: "Metasequoia"},
		{Domain: "taken.dev", Account: "Acme", Unavailable: `taken.dev/* goes to the Worker "other"`},
	}
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("add")
	tt.WaitUntil(5*time.Second, func() bool { return !a.cfLoading })
	if len(tt.Query("track-msime.app")) != 1 || len(tt.Query("track-acme.dev")) != 0 || len(tt.Query("track-taken.dev")) != 0 {
		t.Fatalf("offered %q", tt.Texts())
	}
	if !tt.hasText("On Cloudflare") || !tt.hasText("Metasequoia") || !tt.hasText("Another website") {
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Press("track-msime.app")
	tt.WaitUntil(5*time.Second, func() bool { return a.page == pageManage && !a.checking && a.loading == 0 })
	if a.managed.Domain != "msime.app" || !a.managed.AutoTrack || tt.hasText("<script") {
		t.Errorf("managed %+v, texts %q", a.managed, tt.Texts())
	}
}

func TestAddFromCloudflareNeedsAToken(t *testing.T) {
	f := newFake()
	f.ownServer = true
	f.cfErr = errNoCloudflare
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("add")
	tt.WaitUntil(5*time.Second, func() bool { return !a.cfLoading })
	if !tt.hasText("Run npm run deploy with a Cloudflare token") || len(tt.Query("domain")) != 1 {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestAddOnUmamiOffersNoCloudflare(t *testing.T) {
	f := newFake()
	f.cf = []cfSite{{Domain: "msime.app"}}
	a := testApp(t, f)
	a.refresh()
	tt := build(t, a)
	tt.Press("add")
	tt.Settle()
	if a.cfLoading || len(tt.Query("cloudflare")) != 0 || tt.hasText("Another website") {
		t.Errorf("Umami is offered Cloudflare's websites: %q", tt.Texts())
	}
}

func TestClientCloudflareSites(t *testing.T) {
	token := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/cloudflare" && !token:
			w.WriteHeader(http.StatusNotImplemented)
			w.Write([]byte(`{"message":"no token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/cloudflare":
			w.Write([]byte(`{"sites":[{"domain":"msime.app","account":"M","tracked":false,"unavailable":""}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/cloudflare/sites":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(website{ID: "n", Name: body["domain"], Domain: body["domain"], AutoTrack: true})
		default:
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()
	cl := newClient(srv.URL+"/api", "k")
	sites, err := cl.cloudflareSites(context.Background())
	if err != nil || len(sites) != 1 || sites[0] != (cfSite{Domain: "msime.app", Account: "M"}) {
		t.Errorf("%+v, %v", sites, err)
	}
	w, err := cl.trackSite(context.Background(), "msime.app")
	if err != nil || w != (website{ID: "n", Name: "msime.app", Domain: "msime.app", AutoTrack: true}) {
		t.Errorf("%+v, %v", w, err)
	}
	token = false
	if _, err := cl.cloudflareSites(context.Background()); !errors.Is(err, errNoCloudflare) {
		t.Errorf("without a token: %v", err)
	}
}

func TestWindowSiteMenu(t *testing.T) {
	f := newFake()
	a := testApp(t, f)
	a.refresh()
	tt := buildWindow(t, a)
	tt.Press("window-site-b")
	tt.WaitUntil(5*time.Second, func() bool { return a.page == pageSite && a.loading == 0 })

	tt.RightClick("window-site-menu-c")
	for _, s := range []string{"Website settings", "Open website", "Open in Umami", "Delete website"} {
		if len(tt.Query("role:menuitem "+s)) != 1 {
			t.Errorf("no %q in the menu: %q", s, tt.Texts())
		}
	}
	tt.Press("role:menuitem Website settings")
	if a.page != pageManage || a.managed.ID != "c" {
		t.Fatalf("page %v, managed %+v", a.page, a.managed)
	}

	// Deleting the website whose page is open shows the overview.
	tt.Press("window-site-b")
	tt.WaitUntil(5*time.Second, func() bool { return a.page == pageSite && a.open == "b" })
	tt.OnAlert = func(core.AlertConfig) int { return 0 } // Cancel
	tt.RightClick("window-site-menu-b")
	tt.Press("role:menuitem Delete website")
	if a.site("b") == nil {
		t.Fatal("deleted though cancelled")
	}
	tt.OnAlert = func(core.AlertConfig) int { return 1 } // Delete
	tt.RightClick("window-site-menu-b")
	tt.Press("role:menuitem Delete website")
	tt.WaitUntil(5*time.Second, func() bool { return !a.deleting && a.loading == 0 })
	if a.site("b") != nil || a.page != pageSites || len(tt.Query("window-site-b")) != 0 {
		t.Errorf("page %v, open %q, b %v", a.page, a.open, a.site("b"))
	}
}

func TestWindowSiteMenuDeleteFails(t *testing.T) {
	f := newFake()
	f.ownServer = true
	a := testApp(t, f)
	a.refresh()
	tt := buildWindow(t, a)
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 })
	tt.RightClick("window-site-menu-a")
	if len(tt.Query("role:menuitem Open in Umami")) != 0 {
		t.Error("Umapp's own server is offered Umami's dashboard")
	}
	f.err = &apiError{status: 500, msg: "D1 is down."}
	tt.OnAlert = func(core.AlertConfig) int { return 1 }
	tt.Press("role:menuitem Delete website")
	tt.WaitUntil(5*time.Second, func() bool { return !a.deleting })
	if a.page != pageManage || a.managed.ID != "a" || !tt.hasText("D1 is down.") {
		t.Errorf("page %v, texts %q", a.page, tt.Texts())
	}
}

func TestDeleteWarnsOfCloudflare(t *testing.T) {
	f := newFake()
	f.ownServer = true
	f.deleteWarning = "Cloudflare refused the server's token: Authentication error."
	a := testApp(t, f)
	a.refresh()
	tt := buildWindow(t, a)
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 })
	var shown []string
	tt.OnAlert = func(c core.AlertConfig) int {
		shown = append(shown, c.Title+" | "+c.Message)
		return len(c.Actions) - 1
	}
	tt.RightClick("window-site-menu-a")
	tt.Press("role:menuitem Delete website")
	tt.WaitUntil(5*time.Second, func() bool { return !a.deleting && a.site("a") == nil })
	if len(shown) != 2 || !strings.Contains(shown[1], "Acme Docs is deleted, but not all of it on Cloudflare") || !strings.Contains(shown[1], "Authentication error") {
		t.Errorf("alerts %q", shown)
	}
}

func TestClientDeleteWarning(t *testing.T) {
	body := `{"ok":true,"warning":"routes left"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	cl := newClient(srv.URL+"/api", "k")
	if warning, err := cl.deleteWebsite(context.Background(), "w"); err != nil || warning != "routes left" {
		t.Errorf("%q, %v", warning, err)
	}
	body = ""
	if warning, err := cl.deleteWebsite(context.Background(), "w"); err != nil || warning != "" {
		t.Errorf("an empty answer: %q, %v", warning, err)
	}
}
