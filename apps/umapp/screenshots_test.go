package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

// demo is the made-up account the README's screenshots show.
type demo struct{}

type demoSite struct {
	website
	daily float64 // visitors a day
	trend float64 // this range over the one before
	phase float64 // where its weekly wave starts
}

var demoSites = []demoSite{
	{website{ID: "a", Name: "NSGo", Domain: "nsgo.dev"}, 1840, 1.18, 0},
	{website{ID: "b", Name: "China Holidays", Domain: "holidays.oneo.me"}, 1260, 0.91, 2},
	{website{ID: "c", Name: "Blog", Domain: "oneo.me"}, 620, 1.07, 4},
	{website{ID: "d", Name: "Awesome NSGo", Domain: "awesomensgo.dev"}, 410, 1.32, 1},
	{website{ID: "e", Name: "Gallery", Domain: "gallery.nsgo.dev"}, 95, 0.84, 3},
}

var demoTags = []metric{{"website", 0.72}, {"gallery-web", 0.2}, {"gallery-app", 0.08}}

func demoOf(id string) demoSite {
	for _, s := range demoSites {
		if s.ID == id {
			return s
		}
	}
	return demoSite{}
}

// demoShare is the share of a tag's events, or 1 for all of them.
func demoShare(tag string) float64 {
	if tag == "" {
		return 1
	}
	for _, m := range demoTags {
		if m.X == tag {
			return float64(m.Y)
		}
	}
	return 0
}

func (demo) websites(context.Context) ([]website, error) {
	var out []website
	for _, s := range demoSites {
		out = append(out, s.website)
	}
	return out, nil
}

func (demo) stats(_ context.Context, id string, from, to time.Time, tag string) (cur, prev stats, err error) {
	s := demoOf(id)
	k := s.daily * demoShare(tag) * to.Sub(from).Hours() / 24
	cur = stats{Pageviews: num(k * 3.1), Visitors: num(k), Visits: num(k * 1.3), Bounces: num(k * 0.55), Totaltime: num(k * 1.3 * 94)}
	p := k / s.trend
	prev = stats{Pageviews: num(p * 2.9), Visitors: num(p), Visits: num(p * 1.25), Bounces: num(p * 0.6), Totaltime: num(p * 1.25 * 82)}
	return cur, prev, nil
}

func (demo) active(_ context.Context, id string) (int, error) {
	return int(demoOf(id).daily / 90), nil
}

func (demo) series(_ context.Context, id string, from, to time.Time, unit, _, tag string) (views, visitors []point, err error) {
	s := demoOf(id)
	k := s.daily * demoShare(tag)
	step := 24 * time.Hour
	if unit == "hour" {
		step = time.Hour
	}
	for t, i := from, 0; !t.After(to); t, i = t.Add(step), i+1 {
		var v float64
		if unit == "day" {
			v = k * (1 + 0.22*math.Sin(float64(i)/1.3+s.phase) + 0.08*math.Cos(float64(i)*2.1))
		} else {
			v = k / 24 * (1 + 0.7*math.Sin(float64(t.Hour()-9)/24*2*math.Pi+s.phase/4))
		}
		v = math.Round(v)
		if v <= 0 {
			continue
		}
		x := t.Format("2006-01-02 15:04:05")
		visitors = append(visitors, point{X: x, Y: num(v)})
		views = append(views, point{X: x, Y: num(math.Round(v * (2.6 + 0.4*math.Sin(float64(i)))))})
	}
	return views, visitors, nil
}

func (demo) metrics(_ context.Context, id, kind string, from, to time.Time, limit int, tag string) ([]metric, error) {
	k := demoOf(id).daily * demoShare(tag) * to.Sub(from).Hours() / 24
	if to.Sub(from) <= liveWindow {
		k = math.Floor(demoOf(id).daily * demoShare(tag) / 90)
	}
	views := k * 3.1
	var rows []metric
	switch kind {
	case "path":
		rows = []metric{{"/", num(views * 0.31)}, {"/docs/getting-started", num(views * 0.18)}, {"/components", num(views * 0.12)}, {"/gallery", num(views * 0.08)}, {"/blog/nsgo-0-1", num(views * 0.05)}, {"/docs/testing", num(views * 0.03)}}
	case "referrer":
		rows = []metric{{"", num(k * 0.42)}, {"www.google.com", num(k * 0.26)}, {"github.com", num(k * 0.15)}, {"x.com", num(k * 0.07)}, {"news.ycombinator.com", num(k * 0.05)}, {"www.bing.com", num(k * 0.02)}}
	case "country":
		rows = []metric{{"US", num(k * 0.31)}, {"CN", num(k * 0.24)}, {"JP", num(k * 0.11)}, {"DE", num(k * 0.08)}, {"GB", num(k * 0.06)}, {"CA", num(k * 0.04)}}
	case "tag":
		for _, m := range demoTags {
			rows = append(rows, metric{m.X, num(math.Round(k * float64(m.Y)))})
		}
	}
	for i := range rows {
		rows[i].Y = num(math.Round(float64(rows[i].Y)))
	}
	return rows[:min(limit, len(rows))], nil
}

func (demo) addWebsite(context.Context, string, string) (website, error) {
	return website{}, errors.New("the demo adds no websites")
}

func (demo) updateWebsite(context.Context, string, string, string) (website, error) {
	return website{}, errors.New("the demo changes no websites")
}

func (demo) hasDashboard() bool { return true }

func (demo) cloudflareSites(context.Context) ([]cfSite, error) { return nil, errNoCloudflare }

func (demo) trackSite(context.Context, string) (website, error) {
	return website{}, errors.New("demo")
}

func (demo) deleteWebsite(context.Context, string) (string, error) {
	return "", errors.New("the demo deletes no websites")
}

// TestScreenshots and the others save the README's and the website's
// screenshots, of the list and of a website's page in each language and
// appearance, under nsgo test -target macos, ios or ios_simulator: one app to a
// test, as a phone runs one.
func TestScreenshots(t *testing.T)       { screenshots(t, "en", core.AppearanceLight) }
func TestScreenshotsDark(t *testing.T)   { screenshots(t, "en", core.AppearanceDark) }
func TestScreenshotsZh(t *testing.T)     { screenshots(t, "zh-Hans", core.AppearanceLight) }
func TestScreenshotsZhDark(t *testing.T) { screenshots(t, "zh-Hans", core.AppearanceDark) }

// TestScreenshotsWindow and the others save the main window's, on macOS alone, which has it.
func TestScreenshotsWindow(t *testing.T)       { windowScreenshot(t, "en", core.AppearanceLight) }
func TestScreenshotsWindowDark(t *testing.T)   { windowScreenshot(t, "en", core.AppearanceDark) }
func TestScreenshotsWindowZh(t *testing.T)     { windowScreenshot(t, "zh-Hans", core.AppearanceLight) }
func TestScreenshotsWindowZhDark(t *testing.T) { windowScreenshot(t, "zh-Hans", core.AppearanceDark) }

func windowScreenshot(t *testing.T, locale string, appearance core.Appearance) {
	if nsgotest.Target() != "macos" {
		t.Skip("only macOS has the window")
	}
	a := newApp(config{Endpoint: cloudEndpoint, Key: "api_demo", Range: 1}, demo{})
	a.openWindow = func() {}
	a.refresh()
	tt := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale(locale)
		cx.SetAppearance(appearance)
		a.attach(cx)
		return a.windowRoot(cx)
	}, nsgotest.Size(windowWidth, windowHeight))
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 })
	tt.Press("window-site-a")
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 && a.site("a").livePages != nil })
	// Let the icons and flags load: the sidebar's and the page's, more than the panel shows.
	time.Sleep(8 * time.Second)
	suffix := ""
	if appearance == core.AppearanceDark {
		suffix = "-dark"
	}
	tt.Screenshot(locale + "/window" + suffix)
}

func screenshots(t *testing.T, locale string, appearance core.Appearance) {
	a := newApp(config{Endpoint: cloudEndpoint, Key: "api_demo", Range: 1}, demo{})
	size := nsgotest.Size(panelWidth, panelHeight)
	if t := nsgotest.Target(); t == "ios" || t == "ios_simulator" {
		a.noTray = true
		size = nsgotest.Size(390, 844)
	}
	a.refresh()
	tt := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetLocale(locale)
		cx.SetAppearance(appearance)
		a.attach(cx)
		return a.root(cx)
	}, size)
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 })
	// Let the icons and flags load.
	time.Sleep(3 * time.Second)
	suffix := ""
	if appearance == core.AppearanceDark {
		suffix = "-dark"
	}
	tt.Screenshot(locale + "/list" + suffix)

	a.openSite("a")
	tt.WaitUntil(5*time.Second, func() bool { return a.loading == 0 && a.site("a").livePages != nil })
	time.Sleep(3 * time.Second)
	tt.Screenshot(locale + "/site" + suffix)
}
