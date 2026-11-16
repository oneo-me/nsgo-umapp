package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/base/plot"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// detailHeader is the site's page's bar: back to the list, and to Umami.
func (a *app) detailHeader(cx *core.Scope, s *site) core.Node {
	tr := msgsOf(cx)
	nav := []core.Node{a.backButton(cx), ui.Spacer{}}
	nav = append(nav, a.toolButton(cx, lucide.SlidersHorizontal, tr.WebsiteSettings(), nil, func() { a.manageSite(s.ID) }, "manage"))
	if u := dashboardURL(a.cfg.Endpoint, s.ID); u != "" && a.src != nil && a.src.hasDashboard() {
		nav = append(nav, a.toolButton(cx, lucide.ExternalLink, tr.OpenUmami(), nil, func() { cx.OpenURL(u, nil) }, "open"))
	}
	return bar(cx, append(nav, a.refreshButton(cx))...)
}

// backButton returns to the list: a ghost button whose chevron sits close
// to the bar's edge.
func (a *app) backButton(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	return base.Button{
		Label: tr.Websites(), TestID: "back",
		OnPress: func(core.PressEvent) { a.back() },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
			Height: core.Pt(ui.ButtonHeight), Padding: core.Edges{Left: 2, Right: inset}, CornerRadius: 6},
		StyleFunc: func() core.Style {
			return core.Style{
				Hover:  &core.Style{Background: colors.Hover()},
				Active: &core.Style{Background: colors.Pressed()},
			}
		},
		Children: []core.Node{
			ui.Icon(lucide.ChevronLeft, 16, colors.Muted),
			ui.Text(tr.Websites(), 13, ui.Regular, colors.Text),
		},
	}
}

func (a *app) detail(cx *core.Scope, s *site) core.Node { return a.detailView(cx, s, false) }

// detailView is a site's page: in the panel one column, in the window (wide) its breakdowns side by side, wrapping as the window narrows.
func (a *app) detailView(cx *core.Scope, s *site, wide bool) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	breakdowns := []core.Node{
		breakdown(cx, tr.Pages(), tr.Views(), "path", s.pages, a.liveOf(s, s.livePages), s.detailGen == a.gen && s.pages == nil && s.detailErr == nil, nil),
		breakdown(cx, tr.Referrers(), tr.Visitors(), "referrer", s.referrers, nil, s.pages == nil && s.detailErr == nil,
			func(x string) core.Node { return favicon(cx, x, 14) }),
		breakdown(cx, tr.Countries(), tr.Visitors(), "country", s.countries, a.liveOf(s, s.liveCountries), s.pages == nil && s.detailErr == nil, flag),
	}
	children := []core.Node{
		row(10, favicon(cx, s.Domain, 28),
			row(8, oneLine(ui.Text(s.Name, 17, ui.Semibold, colors.Text)), a.liveBadge(cx, s.active, true))),
		a.tagFilter(cx, s),
		a.metricsGrid(cx, a.shown(s)),
		a.chart(cx, a.shown(s)),
		a.detailError(cx, s),
	}
	if wide {
		for i, b := range breakdowns {
			breakdowns[i] = core.View{Style: core.Style{FlexGrow: 1, FlexBasis: core.Pt(240), MinWidth: core.Pt(240)}, Children: []core.Node{b}}
		}
		children = append(children, core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 18, AlignItems: core.AlignStart}, Children: breakdowns})
	} else {
		children = append(children, breakdowns...)
	}
	return ui.ScrollView{Style: ui.FillRest, TestID: "detail-" + s.ID, EdgeColor: edgeColor(cx),
		ContentStyle: core.Style{Padding: core.All(gutter), Gap: 18},
		Children:     children}
}

// liveOf is who is on the site now of a breakdown, while it is of the tag
// the page filters by.
func (a *app) liveOf(s *site, live []metric) []metric {
	if s.liveTag != a.tag {
		return nil
	}
	return live
}

func (a *app) detailError(cx *core.Scope, s *site) core.Node {
	if s.detailErr == nil {
		return core.Group(nil)
	}
	colors := ui.ColorsOf(cx)
	return row(6, ui.Icon(lucide.CircleAlert, 14, colors.Danger),
		core.Text{Content: s.detailErr.Error(), Style: core.Style{FontSize: 12, FlexGrow: 1}, StyleFunc: ui.Colored(colors.Muted)})
}

// tagFilter lists the site's tags, each with its visitors, of which
// pressing one shows only its events, where the site has any.
func (a *app) tagFilter(cx *core.Scope, s *site) core.Node {
	tags := slices.Clone(s.tags)
	if a.tag != "" && !slices.ContainsFunc(tags, func(m metric) bool { return m.X == a.tag }) {
		tags = append(tags, metric{X: a.tag, Y: -1})
	}
	if len(tags) == 0 {
		return core.Group(nil)
	}
	all := num(-1)
	if s.loaded {
		all = s.cur.Visitors
	}
	chips := []core.Node{a.tagChip(cx, msgsOf(cx).AllTags(), "", all, s.active)}
	for _, m := range tags {
		chips = append(chips, a.tagChip(cx, m.X, m.X, m.Y, online(s.liveTags, m.X)))
	}
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 6},
		Children: chips}
}

// tagChip is a small tag the site's page filters by, the empty one for all
// events, with its visitors when known (n >= 0), after those online now
// where there are any. Pressing the chosen one shows all events again.
func (a *app) tagChip(cx *core.Scope, label, tag string, n num, now int) core.Node {
	colors := ui.ColorsOf(cx)
	on := live(a, func() bool { return a.tag == tag })
	ink := func() core.Color {
		if on() {
			return colors.OnAccent()
		}
		return colors.Text()
	}
	children := []core.Node{oneLine(ui.Text(label, 12, ui.Regular, ink))}
	quiet := func() core.Color {
		if on() {
			return ui.Alpha(colors.OnAccent(), 0.75)
		}
		return colors.Muted()
	}
	if n >= 0 || now > 0 {
		live := func() core.Color {
			if on() {
				return colors.OnAccent()
			}
			return colors.Up()
		}
		children = append(children, onlineCount(now, n, 11, live, quiet))
	}
	return base.Toggle{Label: label, TestID: "tag-" + cmp.Or(tag, "all"), Pressed: on,
		OnChange: func(pressed bool) {
			if pressed {
				a.setTag(tag)
			} else {
				a.setTag("")
			}
		},
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5, MinWidth: core.Pt(0),
			Height: core.Pt(22), Padding: core.Symmetric(0, 8), CornerRadius: 11, BorderWidth: 1},
		StyleFunc: func() core.Style {
			if on() {
				return core.Style{Background: colors.Accent(), BorderColor: colors.Accent()}
			}
			return core.Style{BorderColor: colors.Line(),
				Hover:  &core.Style{Background: colors.Hover()},
				Active: &core.Style{Background: colors.Pressed()}}
		},
		Children: children}
}

func (a *app) metricsGrid(cx *core.Scope, s figures) core.Node {
	tr := msgsOf(cx)
	perVisit := func(st stats) float64 {
		if st.Visits == 0 {
			return 0
		}
		return float64(st.Pageviews) / float64(st.Visits)
	}
	cells := []struct {
		label     string
		value     string
		cur, prev float64
		worse     bool
	}{
		{tr.Visitors(), count(float64(s.cur.Visitors)), float64(s.cur.Visitors), float64(s.prev.Visitors), false},
		{tr.Visits(), count(float64(s.cur.Visits)), float64(s.cur.Visits), float64(s.prev.Visits), false},
		{tr.Views(), count(float64(s.cur.Pageviews)), float64(s.cur.Pageviews), float64(s.prev.Pageviews), false},
		{tr.ViewsPerVisit(), fmt.Sprintf("%.1f", perVisit(s.cur)), perVisit(s.cur), perVisit(s.prev), false},
		{tr.BounceRate(), percent(s.cur.bounceRate()), s.cur.bounceRate(), s.prev.bounceRate(), true},
		{tr.VisitDuration(), duration(s.cur.visitDuration()), s.cur.visitDuration().Seconds(), s.prev.visitDuration().Seconds(), false},
	}
	children := make([]core.Node, 0, len(cells))
	for _, m := range cells {
		children = append(children, a.metricCell(cx, s.loaded, m.label, m.value, m.cur, m.prev, m.worse))
	}
	return core.View{Style: core.Style{Display: core.DisplayGrid, GridTemplateColumns: core.GridTracks("1fr 1fr 1fr"), Gap: 12},
		Children: children}
}

func (a *app) metricCell(cx *core.Scope, loaded bool, label, value string, cur, prev float64, worse bool) core.Node {
	colors := ui.ColorsOf(cx)
	inner := []core.Node{ui.Text(label, 11, ui.Regular, colors.Muted)}
	if !loaded {
		inner = append(inner,
			ui.SkeletonBlock{Width: core.Pt(48), Height: core.Pt(16), Style: core.Style{Margin: core.Symmetric(2, 0)}},
			ui.SkeletonBlock{Width: core.Pt(28), Height: core.Pt(9)},
		)
	} else {
		delta := a.delta(cx, 0, 0, worse)
		if cur != 0 || prev != 0 {
			delta = a.delta(cx, cur, prev, worse)
		}
		inner = append(inner, ui.Tabular(ui.Text(value, 17, ui.Semibold, colors.Text)), delta)
	}
	return column(3, inner...)
}

func (a *app) chart(cx *core.Scope, s figures) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	n := len(a.buckets)
	if n == 0 || len(s.views) != n || len(s.visitors) != n {
		return core.View{Style: core.Style{Height: core.Pt(150), CornerRadius: 8}, StyleFunc: ui.Filled(colors.Hover)}
	}
	views, visitors := append([]float64(nil), s.views...), append([]float64(nil), s.visitors...)
	hi := 4.0
	for _, v := range views {
		if v > hi {
			hi = v
		}
	}
	label := tr.ChartByDay()
	if a.span().hours {
		label = tr.ChartByHour()
	}
	hours := a.span().hours
	ticks := axisTicks(n, hours, a.buckets)
	buckets := append([]time.Time(nil), a.buckets...)
	tip := core.NewSignal(cx, 0)
	fin := ui.FinOf(cx)
	return column(8,
		row(12,
			legend(cx, colors.Accent, tr.Visitors()),
			legend(cx, func() core.Color { return ui.Alpha(colors.Accent(), 0.28) }, tr.Views()),
		),
		plot.Plot{
			Count:  func() int { return n },
			YScale: func() plot.Scale { return plot.LinearScale(0, hi) },
			XScale: func() plot.Scale { return plot.BandScale(plot.Band{PaddingInner: 0.28, PaddingOuter: 0.06}) },
			Label:  label,
			AccessibilityValue: func() core.AccessibilityValue {
				i := tip.Get()
				return core.AccessibilityValue{Text: fmt.Sprintf("%s: %s %s, %s %s", bucketLabel(buckets[i], hours),
					count(visitors[i]), tr.Visitors(), count(views[i]), tr.Views())}
			},
			OnHoverChange: func(i int) {
				if i >= 0 && i < n {
					tip.Set(i)
				}
			},
			TooltipOffset: 4,
			Tooltip: core.View{StyleFunc: func() core.Style {
				return fin.Menu().Over(core.Style{Gap: 3, Padding: core.Symmetric(6, 8), CornerRadius: 8, BorderWidth: 1})
			},
				Children: []core.Node{
					ui.Textf(func() string { return bucketLabel(buckets[tip.Get()], hours) }, 11, ui.Regular, colors.Muted),
					tipValue(cx, colors.Accent, func() string { return count(visitors[tip.Get()]) }, tr.Visitors()),
					tipValue(cx, func() core.Color { return ui.Alpha(colors.Accent(), 0.28) }, func() string { return count(views[tip.Get()]) }, tr.Views()),
				}},
			TestID: "chart",
			Style:  core.Style{Height: core.Pt(150)},
			Children: []core.Node{
				plot.Grid{StyleFunc: func() core.Style { return core.Style{Stroke: core.PaintColor(colors.Line())} }},
				plot.Bar{Value: func() []float64 { return views }, HoverDim: 0.6,
					Corners: plot.Corners{TopLeft: 2, TopRight: 2},
					StyleFunc: func() core.Style {
						return core.Style{Fill: core.PaintColor(ui.Alpha(colors.Accent(), 0.28))}
					}},
				plot.Bar{Value: func() []float64 { return visitors }, HoverDim: 0.4,
					Corners:   plot.Corners{TopLeft: 2, TopRight: 2},
					StyleFunc: func() core.Style { return core.Style{Fill: core.PaintColor(colors.Accent())} }},
				plot.Axis{Placement: plot.AxisBottom, TickValues: func() []float64 { return ticks.at },
					Format: func(_ float64, i int) string {
						if i >= 0 && i < len(ticks.labels) {
							return ticks.labels[i]
						}
						return ""
					},
					LabelStyleFunc: func() core.Style {
						return core.Style{FontSize: 10, Color: colors.Faint()}
					}},
			},
		},
	)
}

type axisLabels struct {
	at     []float64
	labels []string
}

func axisTicks(n int, hours bool, buckets []time.Time) axisLabels {
	every := map[int]int{24: 6, 7: 1, 30: 7, 90: 30}[n]
	if every == 0 {
		every = max(1, n/4)
	}
	var out axisLabels
	for i := 0; i < n; i += every {
		if n-i < every/2 && i != 0 {
			break
		}
		l := buckets[i].Format("Jan 2")
		if hours {
			l = buckets[i].Format("15:04")
		} else if n == 7 {
			l = buckets[i].Format("Mon")
		}
		out.at = append(out.at, float64(i))
		out.labels = append(out.labels, l)
	}
	return out
}

// tipValue is a row of the chart's tooltip: a series' key, its value at
// the bucket under the pointer and its name.
func tipValue(cx *core.Scope, color func() core.Color, value func() string, label string) core.Node {
	colors := ui.ColorsOf(cx)
	return row(5,
		core.View{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8), CornerRadius: 2}, StyleFunc: ui.Filled(color)},
		ui.Tabular(ui.Textf(value, 12, ui.Semibold, colors.Text)),
		muted(cx, label, 11),
	)
}

func legend(cx *core.Scope, color func() core.Color, label string) core.Node {
	return row(5,
		core.View{Style: core.Style{Width: core.Pt(8), Height: core.Pt(8), CornerRadius: 2}, StyleFunc: ui.Filled(color)},
		muted(cx, label, 11),
	)
}

// breakdown lists the top values of a kind, each with what icon draws for
// it when icon is set, and with the visitors on it now of live, those it
// misses after them.
func breakdown(cx *core.Scope, title, unit, kind string, rows, live []metric, loading bool, icon func(x string) core.Node) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	head := core.View{Style: core.Style{Direction: core.Row},
		Children: []core.Node{
			core.Text{Content: title, Style: core.Style{FontSize: 11, FontWeight: ui.Medium, FlexGrow: 1}, StyleFunc: ui.Colored(colors.Faint)},
			ui.Text(unit, 11, ui.Medium, colors.Faint),
		}}
	var body core.Node
	switch {
	case loading:
		sk := make([]core.Node, 3)
		for i := range sk {
			sk[i] = ui.SkeletonBlock{Width: core.Pt(core.Points(160 - i*36)), Height: core.Pt(10),
				Style: core.Style{Height: core.Pt(28), AlignItems: core.AlignCenter}}
		}
		body = column(0, sk...)
	case len(rows) == 0:
		body = ui.Text(tr.NothingInRange(), 12, ui.Regular, colors.Faint)
	default:
		top := math.Max(float64(rows[0].Y), 1)
		rows = withOnline(rows, live)
		items := make([]core.Node, 0, len(rows))
		for _, m := range rows {
			share := float32(max(float64(m.Y), 0) / top * 100)
			label := pathLabel(kind, m.X)
			cells := []core.Node{oneLine(ui.Text(label, 12, ui.Regular, colors.Text))}
			if icon != nil {
				cells = append([]core.Node{icon(m.X)}, cells...)
			}
			cells = append(cells, ui.Spacer{}, onlineCount(online(live, m.X), m.Y, 12, colors.Up, colors.Muted))
			items = append(items, core.View{Style: core.Style{Height: core.Pt(28)}, Children: []core.Node{
				core.View{Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0),
					Width: core.Pct(share), CornerRadius: 5},
					StyleFunc: func() core.Style { return core.Style{Background: ui.Alpha(colors.Accent(), 0.1)} }},
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, FlexGrow: 1,
					Padding: core.All(6), Height: core.Pt(28)},
					Children: cells},
			}})
		}
		body = column(0, items...)
	}
	return column(4, head, body)
}

// onlineCount is a count over the range, n, after the visitors on its
// value now where there are any, as "2/40"; n < 0 is unknown.
func onlineCount(now int, n num, size core.Points, live, quiet func() core.Color) core.Node {
	var children []core.Node
	if now > 0 {
		children = append(children, ui.Tabular(ui.Text(count(float64(now)), size, ui.Medium, live)))
	}
	if n >= 0 {
		total := count(float64(n))
		if now > 0 {
			total = "/" + total
		}
		children = append(children, ui.Tabular(ui.Text(total, size, ui.Regular, quiet)))
	}
	return row(0, children...)
}

// favicon is a website's icon from the service Umami shows them from, at a
// size, or a globe where it has none.
func favicon(cx *core.Scope, domain string, size core.Points) core.Node {
	colors := ui.ColorsOf(cx)
	host := faviconHost(domain)
	failed := core.NewSignal(cx, host == "")
	return core.View{Style: core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0,
		AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		Children: []core.Node{core.Show{When: failed.Get,
			Then: func(*core.Scope) core.Node { return ui.Icon(lucide.Globe, size*0.85, colors.Faint) },
			Else: func(*core.Scope) core.Node {
				return core.Image{Source: core.ImageSource{URI: "https://icons.duckduckgo.com/ip3/" + host + ".ico"},
					ResizeMode: core.Contain, Style: core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					OnError: func(error) { failed.Set(true) }}
			}}}}
}

// faviconHost is the host of a website's domain or a referrer, which may
// come with a scheme or a path; empty for none.
func faviconHost(domain string) string {
	domain = strings.TrimSpace(domain)
	if _, rest, ok := strings.Cut(domain, "://"); ok {
		domain = rest
	}
	domain, _, _ = strings.Cut(domain, "/")
	if !strings.Contains(domain, ".") {
		return ""
	}
	return strings.ToLower(domain)
}

// flag is a country's flag by its ISO code, or the room of one where the
// code is unknown.
func flag(code string) core.Node {
	box := core.Style{Width: core.Pt(16), Height: core.Pt(12), FlexShrink: 0, CornerRadius: 2, Overflow: core.Hidden}
	if len(code) != 2 {
		return core.View{Style: box}
	}
	return core.Image{Source: core.ImageSource{URI: "https://flagcdn.com/32x24/" + strings.ToLower(code) + ".png"},
		ResizeMode: core.Cover, Style: box}
}
