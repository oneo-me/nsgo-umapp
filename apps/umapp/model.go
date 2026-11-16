package main

import (
	"cmp"
	"context"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// source is what the app reads from Umami: *client, or a fake in tests.
type source interface {
	websites(ctx context.Context) ([]website, error)
	stats(ctx context.Context, id string, from, to time.Time, tag string) (cur, prev stats, err error)
	active(ctx context.Context, id string) (int, error)
	series(ctx context.Context, id string, from, to time.Time, unit, tz, tag string) (views, visitors []point, err error)
	metrics(ctx context.Context, id, kind string, from, to time.Time, limit int, tag string) ([]metric, error)
}

// span is a range of time the app shows, with its buckets.
type span struct {
	label string // in the range control
	long  string // in sentences: "vs. previous 7 days"
	hours bool   // buckets of an hour, or of a day
	n     int    // buckets
}

var spans = []span{
	{"24H", "previous 24 hours", true, 24},
	{"7D", "previous 7 days", false, 7},
	{"30D", "previous 30 days", false, 30},
	{"90D", "previous 90 days", false, 90},
}

// window returns the range of s ending now, and the start of each bucket.
func (s span) window(now time.Time) (from, to time.Time, buckets []time.Time) {
	if s.hours {
		from = now.Truncate(time.Hour).Add(-time.Duration(s.n-1) * time.Hour)
		// Truncate works in UTC: put the hour back in local time.
		from = time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), 0, 0, 0, now.Location())
		for i := range s.n {
			buckets = append(buckets, from.Add(time.Duration(i)*time.Hour))
		}
	} else {
		y, m, d := now.Date()
		from = time.Date(y, m, d-(s.n-1), 0, 0, 0, 0, now.Location())
		for i := range s.n {
			buckets = append(buckets, time.Date(y, m, d-(s.n-1)+i, 0, 0, 0, 0, now.Location()))
		}
	}
	return from, now, buckets
}

func (s span) unit() string {
	if s.hours {
		return "hour"
	}
	return "day"
}

// bucketize spreads points the server gave over buckets, which are
// zero where it gave none.
func bucketize(points []point, buckets []time.Time, hours bool) []float64 {
	out := make([]float64, len(buckets))
	if len(buckets) == 0 {
		return out
	}
	loc := buckets[0].Location()
	for _, p := range points {
		t, ok := parseTime(p.X, loc)
		if !ok {
			continue
		}
		if hours {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		if i := slices.IndexFunc(buckets, t.Equal); i >= 0 {
			out[i] += float64(p.Y)
		}
	}
	return out
}

// parseTime reads the times of series: wall clock times in the zone the
// series was asked in, or instants.
func parseTime(s string, loc *time.Location) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.In(loc), true
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// localZone is the IANA name of the local time zone, which Umami groups series in.
func localZone() string {
	if tz := browserZone(); tz != "" {
		return tz
	}
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(p, "zoneinfo/"); ok {
			return name
		}
	}
	return "UTC"
}

// figures are a website's numbers over the range the app shows.
type figures struct {
	cur, prev stats
	views     []float64 // per bucket
	visitors  []float64
	loaded    bool
}

// site is a website and what the app knows of it.
type site struct {
	website
	figures
	active int
	err    error

	// What its page shows besides, for the range the app shows and the
	// tag it filters by.
	detailGen int
	detailTag string
	tags      []metric // every tag, whatever the filter
	tagged    figures  // the figures of detailTag, when it is set
	pages     []metric
	referrers []metric
	countries []metric
	detailErr error

	// Who is on it now, as its page shows: the visitors in the last five
	// minutes of each tag, and of each page and country of liveTag.
	liveTag       string
	liveTags      []metric
	livePages     []metric
	liveCountries []metric
}

type page int

const (
	pageSites page = iota
	pageSite
	pageSettings
)

// app is the state of the panel. Everything in it is read and changed on
// the main thread: work in other goroutines posts its results back with
// post.
type app struct {
	cfg     config
	cfgDir  string
	src     source
	tz      string
	now     func() time.Time
	post    func(func())     // runs a function on the main thread, then redraws
	spawn   func(func())     // starts work in the background
	changed func(cfg config) // the config changed: save it, update the tray
	onLive  func(n int)      // the count of visitors online changed
	quit    func()

	page     page
	open     string // the ID of the site its page shows
	tag      string // the tag the site's page filters by, or empty for all
	sites    []*site
	listErr  error
	loading  int // loads under way
	gen      int // increments as what is shown changes; results of older ones are dropped
	loadedAt time.Time
	buckets  []time.Time
	selected int // the bucket the pointer is over in the site's chart, or -1
	liveSeq  int // increments with each load of who is on the site's page now

	// The settings' fields, until saved.
	editEndpoint string
	editKey      string
	connecting   bool
	connectErr   string

	// Whether the system starts the app at login, where it can. The
	// system keeps the setting: readLogin asks it, setLogin changes it.
	canLaunchAtLogin bool
	launchAtLogin    bool
	readLogin        func(done func(on bool))
	setLogin         func(on bool)

	// checkUpdates asks the platform's updater for a newer version and
	// shows what it finds; nil where the app does not update itself.
	checkUpdates func()

	// noTray is set where the panel fills the app, as on iOS, with no
	// menu bar icon to count visitors beside.
	noTray bool

	// cx is the app's scope, nil until attach; rev counts the changes to
	// the state, which the views follow.
	cx       *core.Scope
	rev      *core.Signal[uint64]
	revision uint64
}

func newApp(cfg config, src source) *app {
	a := &app{
		cfg:      cfg,
		src:      src,
		tz:       localZone(),
		now:      time.Now,
		post:     func(fn func()) { fn() },
		spawn:    func(fn func()) { fn() },
		changed:  func(config) {},
		onLive:   func(int) {},
		quit:     func() {},
		selected: -1,

		readLogin: func(done func(bool)) { done(false) },
		setLogin:  func(bool) {},
	}
	a.cfg.Range = min(max(a.cfg.Range, 0), len(spans)-1)
	if a.cfg.Sort != sortByViews && a.cfg.Sort != sortByName {
		a.cfg.Sort = sortByVisitors
	}
	a.resetSettings()
	if !a.configured() {
		a.page = pageSettings
	}
	return a
}

// attach makes what shows follow the state, on the app's scope. Later work
// runs in the background and comes back through Post.
func (a *app) attach(cx *core.Scope) {
	a.cx = cx
	a.rev = core.NewSignal(cx, a.revision)
	a.spawn = func(fn func()) { go fn() }
	a.post = func(fn func()) {
		cx.Post(func() {
			fn()
			a.invalidate()
		})
	}
}

// invalidate bumps the revision the views follow after state changed.
func (a *app) invalidate() {
	a.revision++
	if a.rev != nil {
		a.rev.Set(a.revision)
	}
}

func (a *app) commitCfg() {
	a.changed(a.cfg)
	a.invalidate()
}

// setLanguage shows the app in lang, a catalog's language, or the
// system's for "".
func (a *app) setLanguage(lang string) {
	if lang == a.cfg.Language {
		return
	}
	a.cfg.Language = lang
	if a.cx != nil {
		a.cx.SetLocale(lang)
	}
	a.commitCfg()
}

func (a *app) configured() bool { return a.src != nil && a.cfg.Key != "" }

func (a *app) span() span { return spans[a.cfg.Range] }

func (a *app) site(id string) *site {
	for _, s := range a.sites {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// ordered returns every site in the order the app shows them: by name, or
// those with the most visitors or views first.
func (a *app) ordered() []*site {
	out := slices.Clone(a.sites)
	byName := func(x, y *site) int {
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	}
	key := func(s *site) num { return s.cur.Visitors }
	if a.cfg.Sort == sortByViews {
		key = func(s *site) num { return s.cur.Pageviews }
	}
	slices.SortStableFunc(out, func(x, y *site) int {
		if a.cfg.Sort != sortByName {
			if c := cmp.Compare(key(y), key(x)); c != 0 {
				return c
			}
		}
		return byName(x, y)
	})
	return out
}

// setSort shows the sites by visitors, views or name.
func (a *app) setSort(sort int) {
	if sort == a.cfg.Sort || sort != sortByVisitors && sort != sortByViews && sort != sortByName {
		return
	}
	a.cfg.Sort = sort
	a.commitCfg()
}

// totals sums the sites shown.
func (a *app) totals() (cur, prev stats, visitors []float64, active int, loaded bool) {
	loaded = true
	for _, s := range a.ordered() {
		if !s.loaded {
			loaded = false
		}
		cur.Pageviews += s.cur.Pageviews
		cur.Visitors += s.cur.Visitors
		cur.Visits += s.cur.Visits
		prev.Pageviews += s.prev.Pageviews
		prev.Visitors += s.prev.Visitors
		prev.Visits += s.prev.Visits
		active += s.active
		if visitors == nil {
			visitors = make([]float64, len(s.visitors))
		}
		for i := range min(len(visitors), len(s.visitors)) {
			visitors[i] += s.visitors[i]
		}
	}
	return cur, prev, visitors, active, loaded
}

// liveTotal is the count of visitors on the sites shown now.
func (a *app) liveTotal() int {
	n := 0
	for _, s := range a.ordered() {
		n += s.active
	}
	return n
}

// refresh loads the list of websites, then each one's numbers for the range
// shown.
func (a *app) refresh() {
	if !a.configured() {
		return
	}
	a.gen++
	gen, src := a.gen, a.src
	sp := a.span()
	from, to, buckets := sp.window(a.now())
	a.buckets = buckets
	a.loading++
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		list, err := src.websites(ctx)
		a.post(func() {
			if gen != a.gen {
				a.loading--
				return
			}
			a.listErr = err
			if err != nil {
				a.loading--
				return
			}
			a.merge(list)
		})
		if err != nil {
			return
		}
		var wg sync.WaitGroup
		limit := make(chan struct{}, 6)
		for _, w := range list {
			wg.Add(1)
			go func() {
				defer wg.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				a.loadSite(ctx, src, gen, w.ID, sp, from, to, buckets)
			}()
		}
		wg.Wait()
		a.post(func() {
			a.loading--
			if gen == a.gen {
				a.loadedAt = a.now()
			}
		})
	})
	if a.page == pageSite {
		a.loadDetail()
		a.loadLive()
	}
}

// merge keeps the sites the server listed, and what is known of them.
func (a *app) merge(list []website) {
	sites := make([]*site, 0, len(list))
	for _, w := range list {
		s := a.site(w.ID)
		if s == nil {
			s = &site{}
		}
		s.website = w
		sites = append(sites, s)
	}
	a.sites = sites
	if a.page == pageSite && a.site(a.open) == nil {
		a.page = pageSites
	}
}

func (a *app) loadSite(ctx context.Context, src source, gen int, id string, sp span, from, to time.Time, buckets []time.Time) {
	var (
		cur, prev       stats
		views, visitors []point
		active          int
		errs            [3]error
		wg              sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); cur, prev, errs[0] = src.stats(ctx, id, from, to, "") }()
	go func() {
		defer wg.Done()
		views, visitors, errs[1] = src.series(ctx, id, from, to, sp.unit(), a.tz, "")
	}()
	go func() { defer wg.Done(); active, errs[2] = src.active(ctx, id) }()
	wg.Wait()
	a.post(func() {
		s := a.site(id)
		if gen != a.gen || s == nil {
			return
		}
		s.err = nil
		for _, err := range errs {
			if err != nil {
				s.err = err
				break
			}
		}
		if errs[0] == nil {
			s.cur, s.prev = cur, prev
		}
		if errs[1] == nil {
			s.views = bucketize(views, buckets, sp.hours)
			s.visitors = bucketize(visitors, buckets, sp.hours)
		}
		if errs[2] == nil {
			s.active = active
		}
		s.loaded = true
		a.onLive(a.liveTotal())
	})
}

// refreshActive updates how many people are on each site now, as the menu
// bar shows.
func (a *app) refreshActive() {
	if !a.configured() || len(a.sites) == 0 {
		return
	}
	src := a.src
	ids := make([]string, len(a.sites))
	for i, s := range a.sites {
		ids[i] = s.ID
	}
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		counts := make([]int, len(ids))
		errs := make([]error, len(ids))
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Add(1)
			go func() { defer wg.Done(); counts[i], errs[i] = src.active(ctx, id) }()
		}
		wg.Wait()
		a.post(func() {
			for i, id := range ids {
				if s := a.site(id); s != nil && errs[i] == nil {
					s.active = counts[i]
				}
			}
			a.onLive(a.liveTotal())
		})
	})
	if a.page == pageSite {
		a.loadLive()
	}
}

// liveWindow is how far back Umami counts visitors online.
const liveWindow = 5 * time.Minute

// loadLive loads who is on the open site now, over the window Umami counts
// visitors online in: each tag's, and the pages and countries of the tag
// the page filters by.
func (a *app) loadLive() {
	s := a.site(a.open)
	if s == nil || !a.configured() {
		return
	}
	a.liveSeq++
	seq, id, tag, src := a.liveSeq, s.ID, a.tag, a.src
	to := a.now()
	from := to.Add(-liveWindow)
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			tags, pages, countries []metric
			errs                   [2]error
			wg                     sync.WaitGroup
		)
		wg.Add(3)
		// Servers without tags refuse to list them: show none.
		go func() { defer wg.Done(); tags, _ = src.metrics(ctx, id, "tag", from, to, 50, "") }()
		go func() { defer wg.Done(); pages, errs[0] = src.metrics(ctx, id, "path", from, to, 20, tag) }()
		go func() { defer wg.Done(); countries, errs[1] = src.metrics(ctx, id, "country", from, to, 20, tag) }()
		wg.Wait()
		a.post(func() {
			s := a.site(id)
			if s == nil || seq != a.liveSeq || errs[0] != nil || errs[1] != nil {
				return
			}
			s.liveTag, s.liveTags, s.livePages, s.liveCountries = tag, tags, pages, countries
		})
	})
}

// online is how many visitors are on x now, of a breakdown in the last
// five minutes.
func online(live []metric, x string) int {
	for _, m := range live {
		if m.X == x {
			return int(m.Y)
		}
	}
	return 0
}

// withOnline adds to the top values of a breakdown those that visitors
// are on now beyond them, whose count over the range is unknown (-1).
func withOnline(rows, live []metric) []metric {
	out := slices.Clone(rows)
	for _, m := range live {
		if m.Y > 0 && !slices.ContainsFunc(rows, func(r metric) bool { return r.X == m.X }) {
			out = append(out, metric{X: m.X, Y: -1})
		}
	}
	return out
}

// openSite shows a site's page.
func (a *app) openSite(id string) {
	a.page, a.open, a.tag, a.selected = pageSite, id, "", -1
	a.invalidate()
	a.loadDetail()
	a.loadLive()
}

// setTag shows the site's page for the events of a tag, or for all of them
// when it is empty.
func (a *app) setTag(tag string) {
	if tag == a.tag {
		return
	}
	a.tag, a.selected = tag, -1
	a.invalidate()
	a.loadDetail()
	a.loadLive()
}

// shown is what the site's page shows: the site's figures, or the tag's.
func (a *app) shown(s *site) figures {
	if a.tag == "" {
		return s.figures
	}
	if s.detailTag != a.tag {
		return figures{}
	}
	return s.tagged
}

func (a *app) back() {
	if a.page == pageSettings {
		a.resetSettings()
	}
	a.page, a.selected = pageSites, -1
	a.invalidate()
}

// loadDetail loads the tags and the breakdowns the site's page shows, once
// per range and tag, and the tag's figures when it filters by one.
func (a *app) loadDetail() {
	s := a.site(a.open)
	if s == nil || s.detailGen == a.gen && s.detailTag == a.tag {
		return
	}
	s.detailGen, s.detailTag, s.detailErr = a.gen, a.tag, nil
	s.pages, s.referrers, s.countries = nil, nil, nil
	s.tagged = figures{}
	gen, tag, id, src := a.gen, a.tag, s.ID, a.src
	sp := a.span()
	from, to, buckets := sp.window(a.now())
	a.loading++
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var (
			res             [3][]metric
			errs            [5]error
			tags            []metric
			cur, prev       stats
			views, visitors []point
			wg              sync.WaitGroup
		)
		for i, kind := range []string{"path", "referrer", "country"} {
			wg.Add(1)
			go func() { defer wg.Done(); res[i], errs[i] = src.metrics(ctx, id, kind, from, to, 6, tag) }()
		}
		// Servers without tags refuse to list them: show no filter.
		wg.Add(1)
		go func() { defer wg.Done(); tags, _ = src.metrics(ctx, id, "tag", from, to, 50, "") }()
		if tag != "" {
			wg.Add(2)
			go func() { defer wg.Done(); cur, prev, errs[3] = src.stats(ctx, id, from, to, tag) }()
			go func() {
				defer wg.Done()
				views, visitors, errs[4] = src.series(ctx, id, from, to, sp.unit(), a.tz, tag)
			}()
		}
		wg.Wait()
		a.post(func() {
			a.loading--
			s := a.site(id)
			if s == nil || s.detailGen != gen || s.detailTag != tag {
				return
			}
			s.tags = slices.DeleteFunc(tags, func(m metric) bool { return m.X == "" })
			s.pages, s.referrers, s.countries = res[0], res[1], res[2]
			if tag != "" {
				s.tagged = figures{cur: cur, prev: prev, loaded: true,
					views: bucketize(views, buckets, sp.hours), visitors: bucketize(visitors, buckets, sp.hours)}
			}
			for _, err := range errs {
				if err != nil {
					s.detailErr = err
					break
				}
			}
		})
	})
}

// setRange shows another range of time.
func (a *app) setRange(i int) {
	if i == a.cfg.Range || i < 0 || i >= len(spans) {
		return
	}
	a.cfg.Range = i
	a.selected = -1
	for _, s := range a.sites {
		s.loaded = false
	}
	a.commitCfg()
	a.refresh()
}

// stale reports whether the numbers shown are old enough to load again as
// the panel opens.
func (a *app) stale() bool {
	return a.loadedAt.IsZero() || a.now().Sub(a.loadedAt) > time.Minute
}

func (a *app) resetSettings() {
	a.editEndpoint, a.editKey = a.cfg.Endpoint, a.cfg.Key
	if a.editEndpoint == "" {
		a.editEndpoint = cloudEndpoint
	}
	a.connectErr = ""
	if a.canLaunchAtLogin {
		a.readLogin(func(on bool) {
			a.launchAtLogin = on
			a.invalidate()
		})
	}
}

func (a *app) setLaunchAtLogin(on bool) {
	if on == a.launchAtLogin {
		return
	}
	a.setLogin(on)
	a.launchAtLogin = on
	a.invalidate()
}

// connect tries the settings' endpoint and key, and keeps them if Umami
// takes them.
func (a *app) connect(newSource func(endpoint, key string) source) {
	endpoint := normalizeEndpoint(a.editEndpoint)
	key := strings.TrimSpace(a.editKey)
	if key == "" {
		a.connectErr = "Paste an API key."
		a.invalidate()
		return
	}
	a.editEndpoint = endpoint
	a.connecting, a.connectErr = true, ""
	a.invalidate()
	src := newSource(endpoint, key)
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := src.websites(ctx)
		a.post(func() {
			a.connecting = false
			if err != nil {
				a.connectErr = err.Error()
				return
			}
			first := !a.configured()
			a.cfg.Endpoint, a.cfg.Key, a.src = endpoint, key, src
			a.sites, a.listErr = nil, nil
			a.commitCfg()
			if first {
				a.page = pageSites
			}
			a.refresh()
		})
	})
}

// disconnect forgets the API key.
func (a *app) disconnect() {
	a.cfg.Key, a.src = "", nil
	a.sites, a.listErr = nil, nil
	a.gen++
	a.resetSettings()
	a.page = pageSettings
	a.commitCfg()
}

// change is how much cur grew from prev, as a fraction, and whether there
// was anything before to compare with.
func change(cur, prev float64) (float64, bool) {
	if prev == 0 || math.IsNaN(prev) {
		return 0, false
	}
	return (cur - prev) / prev, true
}
