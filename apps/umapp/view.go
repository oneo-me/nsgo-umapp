package main

import (
	"fmt"
	"math"
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	panelWidth  = 360
	panelHeight = 560
	// gutter pads bars and pages on every side; inset pads rows, which
	// sit gutter-inset from the edge so that their text lines up.
	gutter = 12
	inset  = 8
	// countWidth is the width of a row's counts, so that the sparklines
	// beside them line up.
	countWidth = 90
)

func (a *app) root(cx *core.Scope) core.Node {
	base.SetScrollbarTheme(cx, ui.ScrollbarTheme(cx))
	a.bindKeys(cx)
	colors := ui.ColorsOf(cx)
	style := ui.FillRest.Merge(core.Style{FontSize: 13, Overflow: core.Hidden})
	if a.noTray {
		style.PaddingEnv = core.EnvAll(core.EnvSafeArea)
	}
	return core.View{Style: style,
		StyleFunc: func() core.Style {
			if desktop.ShowsMaterial(cx) {
				return core.Style{Color: colors.Text()}
			}
			return core.Style{Background: colors.Surface(), Color: colors.Text()}
		},
		Children: []core.Node{
			a.region(a.pageKey, func(cx *core.Scope) core.Node {
				switch {
				case a.page == pageSettings:
					return a.settings(cx)
				case a.page == pageAdd:
					return a.addPage(cx)
				case a.page == pageManage:
					return a.managePage(cx)
				case a.page == pageSite && a.site(a.open) != nil:
					return a.region(a.detailKey, func(cx *core.Scope) core.Node {
						s := a.site(a.open)
						return core.View{Style: ui.FillRest, Children: []core.Node{a.detailHeader(cx, s), a.detail(cx, s)}}
					})
				default:
					return a.region(a.listContentKey, func(cx *core.Scope) core.Node {
						return core.View{Style: ui.FillRest, Children: []core.Node{a.header(cx), a.list(cx)}}
					})
				}
			}),
		}}
}

// bindKeys binds the shortcuts the panel and the window share: refresh, the ranges, settings, and Escape back.
func (a *app) bindKeys(cx *core.Scope) {
	cx.OnKey("mod+r", func() {
		if a.configured() {
			a.refresh()
		}
	})
	cx.OnKey("mod+1", func() { a.setRange(0) })
	cx.OnKey("mod+2", func() { a.setRange(1) })
	cx.OnKey("mod+3", func() { a.setRange(2) })
	cx.OnKey("mod+4", func() { a.setRange(3) })
	cx.OnKey("mod+,", a.showSettings)
	cx.OnKey("escape", func() {
		if a.page != pageSites && a.configured() {
			a.back()
		}
	})
}

// showSettings opens the settings, once connected.
func (a *app) showSettings() {
	if !a.configured() {
		return
	}
	a.resetSettings()
	a.page = pageSettings
	a.invalidate()
}

func (a *app) pageKey() string {
	return fmt.Sprint(a.page, a.open, a.configured(), a.fromLink)
}

func (a *app) listContentKey() string {
	return fmt.Sprint(a.cfg.Range, a.cfg.Sort, a.listErr, a.loading, a.gen, a.loadedAt, a.listKey())
}

func (a *app) detailKey() string {
	s := a.site(a.open)
	if s == nil {
		return a.open
	}
	return fmt.Sprint(a.cfg.Range, a.loading, a.gen, s.detailGen, a.tag, s.detailTag, len(s.tags), s.tagged.loaded,
		len(s.pages), len(s.referrers), len(s.countries), s.detailErr, s.loaded, s.active, s.liveTag, s.liveTags, s.livePages, s.liveCountries)
}

func (a *app) header(cx *core.Scope) core.Node {
	tools := []core.Node{
		a.rangeControl(cx),
		ui.Spacer{},
		a.toolButton(cx, lucide.Plus, a.msgs().AddWebsite(), nil, a.startAdd, "add"),
		a.refreshButton(cx),
	}
	if a.openWindow != nil {
		tools = append(tools, a.toolButton(cx, lucide.Maximize2, a.msgs().OpenWindow(), nil, a.openWindow, "open-window"))
	}
	tools = append(tools, a.toolButton(cx, lucide.Settings, a.msgs().Settings(), nil, func() {
		a.resetSettings()
		a.page = pageSettings
		a.invalidate()
	}, "settings"))
	return bar(cx, tools...)
}

// bar is the strip across the top of a page.
func bar(cx *core.Scope, children ...core.Node) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4,
		Padding: core.All(gutter), Borders: core.Edges{Bottom: 1}, FlexShrink: 0},
		StyleFunc: func() core.Style { return core.Style{BorderColor: colors.Line()} },
		Children:  children}
}

// edgeColor is what a scroll view's content fades into at its edges: the
// panel's surface, or nothing over the material, which the page's color
// would show as a glow on.
func edgeColor(cx *core.Scope) func() core.Color {
	if desktop.ShowsMaterial(cx) {
		return ui.Fixed(core.Transparent)
	}
	return ui.ColorsOf(cx).Surface
}

func (a *app) refreshButton(cx *core.Scope) core.Node {
	return a.toolButton(cx, lucide.RefreshCw, a.msgs().Refresh(), live(a, func() bool { return a.loading > 0 }), a.refresh, "refresh")
}

func (a *app) rangeControl(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	return ui.Tabs[int]{
		Options: []ui.Option[int]{
			{Value: 0, Label: tr.Range24h(), TestID: "24H"},
			{Value: 1, Label: tr.Range7d(), TestID: "7D"},
			{Value: 2, Label: tr.Range30d(), TestID: "30D"},
			{Value: 3, Label: tr.Range90d(), TestID: "90D"},
		},
		Value:    live(a, func() int { return a.cfg.Range }),
		OnChange: a.setRange,
		Label:    tr.Order(),
		TestID:   "range",
		Size:     ui.SizeSmall,
	}
}

func (a *app) toolButton(cx *core.Scope, icon func() *core.SVG, label string, disabled func() bool, press func(), id string) core.Node {
	return ui.IconButton{Icon: icon, Label: label, Disabled: disabled, OnPress: press, TestID: id}
}

func (a *app) list(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	switch {
	case a.listErr != nil && len(a.sites) == 0:
		return a.problem(cx, lucide.CloudOff, tr.LoadErrorTitle(), a.listErr.Error())
	case len(a.sites) == 0 && a.loading > 0:
		return core.View{Style: core.Style{Padding: core.All(gutter - inset)},
			Children: []core.Node{a.summary(cx), a.skeletonList(cx)}}
	case len(a.sites) == 0:
		return a.problem(cx, lucide.Globe, tr.NoSitesTitle(), tr.NoSitesBody())
	}
	return a.region(a.listKey, a.siteList)
}

func (a *app) listKey() string {
	var b string
	b += fmt.Sprint(a.cfg.Sort, a.cfg.Range, a.loading, a.gen)
	for _, s := range a.ordered() {
		b += fmt.Sprintf("%s:%v:%d:%g:%g;", s.ID, s.loaded, s.active, s.cur.Visitors, s.cur.Pageviews)
	}
	return b
}

func (a *app) listHeading(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	metrics := column(1, ui.Text(tr.Visitors()+" / "+tr.Views(), 11, ui.Medium, colors.Faint))
	metrics.Style = metrics.Style.Merge(core.Style{Width: core.Pt(countWidth), AlignItems: core.AlignEnd})
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2,
		Padding: core.All(inset), FlexShrink: 0},
		Children: []core.Node{
			ui.Text(tr.Websites(), 11, ui.Medium, colors.Faint),
			a.orderMenu(cx),
			ui.Spacer{},
			metrics,
		}}
}

func (a *app) orderMenu(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	label := tr.ByVisitors()
	switch a.cfg.Sort {
	case sortByViews:
		label = tr.ByViews()
	case sortByName:
		label = tr.ByName()
	}
	return ui.Menu{
		Trigger: ui.Button{Text: label, Variant: ui.ButtonGhost, TestID: "order"},
		Face: row(3,
			muted(cx, label, 11),
			ui.Icon(lucide.ChevronDown, 10, colors.Faint),
		),
		Label:  tr.Order(),
		TestID: "order",
		Children: []core.Node{
			ui.MenuItem{Text: tr.MostVisitorsFirst(), Checked: live(a, func() bool { return a.cfg.Sort == sortByVisitors }),
				OnSelect: func() { a.setSort(sortByVisitors) }},
			ui.MenuItem{Text: tr.MostViewsFirst(), Checked: live(a, func() bool { return a.cfg.Sort == sortByViews }),
				OnSelect: func() { a.setSort(sortByViews) }},
			ui.MenuItem{Text: tr.ByNameFirst(), Checked: live(a, func() bool { return a.cfg.Sort == sortByName }),
				OnSelect: func() { a.setSort(sortByName) }},
		},
	}
}

func (a *app) siteList(cx *core.Scope) core.Node {
	vis := a.ordered()
	children := make([]core.Node, 0, len(vis)+2)
	children = append(children, a.summary(cx), a.listHeading(cx))
	for _, s := range vis {
		children = append(children, a.siteRow(cx, s, s.ID, nil))
	}
	return ui.ScrollView{Style: ui.FillRest, TestID: "sites", EdgeColor: edgeColor(cx),
		ContentStyle: core.Style{Padding: core.All(gutter - inset)},
		Children:     children}
}

// siteRow is a website's row of the list, named testID; selected, when set, marks the one whose page is open.
func (a *app) siteRow(cx *core.Scope, s *site, testID string, selected func() bool) base.Button {
	colors := ui.ColorsOf(cx)
	id := s.ID
	name := s.Name
	left := column(1, row(6, oneLine(ui.Text(name, 13, ui.Medium, colors.Text)), a.liveBadge(cx, s.active, false)))
	if s.Domain != "" {
		left.Children = append(left.Children, caption(cx, s.Domain))
	}
	left.Style = left.Style.Merge(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)})
	var spark core.Node
	switch {
	case !s.loaded:
		spark = ui.SkeletonBlock{Width: core.Pt(56), Height: core.Pt(20)}
	case s.err != nil && s.cur.Visitors == 0:
		spark = ui.Icon(lucide.CircleAlert, 13, colors.Danger)
	default:
		spark = a.sparkline(cx, s.visitors, 20, 56, false)
	}
	right := column(1, a.siteCount(cx, s)...)
	right.Style = right.Style.Merge(core.Style{Width: core.Pt(countWidth), AlignItems: core.AlignEnd, FlexShrink: 0})
	lead := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10,
		FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
		Children: []core.Node{favicon(cx, s.Domain, 20), left}}
	return base.Button{
		Label: name, TestID: testID, Selected: selected,
		OnPress: func(core.PressEvent) { a.openSite(id) },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12,
			Padding: core.All(inset), CornerRadius: 8, Width: core.Pct(100)},
		StyleFunc: func() core.Style {
			return core.Style{
				Hover:        &core.Style{Background: colors.Hover()},
				Active:       &core.Style{Background: colors.Pressed()},
				Selected:     &core.Style{Background: ui.Alpha(colors.Text(), 0.1)},
				FocusVisible: ui.FocusRing(cx, -2),
			}
		},
		Children: []core.Node{lead, spark, right},
	}
}

func (a *app) siteCount(cx *core.Scope, s *site) []core.Node {
	if !s.loaded {
		return []core.Node{
			ui.SkeletonBlock{Width: core.Pt(32), Height: core.Pt(11), Style: core.Style{Margin: core.Symmetric(2, 0)}},
			ui.SkeletonBlock{Width: core.Pt(24), Height: core.Pt(9), Style: core.Style{Margin: core.Symmetric(2, 0)}},
		}
	}
	cur, prev := s.cur.Visitors, s.prev.Visitors
	if a.cfg.Sort == sortByViews {
		cur, prev = s.cur.Pageviews, s.prev.Pageviews
	}
	return []core.Node{
		a.counts(cx, s.cur, 13, 11),
		a.delta(cx, float64(cur), float64(prev), false),
	}
}

// counts shows visitors and views as "visitors / views".
func (a *app) counts(cx *core.Scope, st stats, size, viewsSize core.Points) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline},
		Children: []core.Node{
			ui.Tabular(ui.Text(count(float64(st.Visitors)), size, ui.Semibold, colors.Text)),
			ui.Tabular(ui.Text(" / "+count(float64(st.Pageviews)), viewsSize, ui.Regular, colors.Muted)),
		}}
}

func (a *app) skeletonList(cx *core.Scope) core.Node {
	rows := make([]core.Node, 4)
	for i := range rows {
		rows[i] = a.skeletonRow(cx)
	}
	return column(0, rows...)
}

func (a *app) skeletonRow(cx *core.Scope) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.All(inset)},
		Children: []core.Node{
			column(6, ui.SkeletonBlock{Width: core.Pt(120), Height: core.Pt(11)}, ui.SkeletonBlock{Width: core.Pt(80), Height: core.Pt(9)}),
			ui.SkeletonBlock{Width: core.Pt(56), Height: core.Pt(20)},
			column(6, ui.SkeletonBlock{Width: core.Pt(32), Height: core.Pt(11)}, ui.SkeletonBlock{Width: core.Pt(24), Height: core.Pt(9)}),
		}}
}

func (a *app) summary(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	cur, prev, visitors, active, loaded := a.totals()
	var figure core.Node
	if loaded && len(a.sites) > 0 {
		figure = row(10,
			a.counts(cx, cur, 30, 13),
			a.delta(cx, float64(cur.Visitors), float64(prev.Visitors), false),
		)
	} else {
		figure = ui.SkeletonBlock{Width: core.Pt(96), Height: core.Pt(26), Style: core.Style{Margin: core.Edges{Top: 6, Bottom: 4}}}
	}
	return core.View{Style: core.Style{Padding: core.All(inset), Gap: 2, FlexShrink: 0},
		Children: []core.Node{
			row(6, ui.Text(tr.Visitors()+" / "+tr.Views(), 12, ui.Medium, colors.Muted), ui.Spacer{}, a.liveBadge(cx, active, true)),
			figure,
			a.sparkline(cx, visitors, 40, 0, true),
		}}
}

func (a *app) liveBadge(cx *core.Scope, n int, long bool) core.Node {
	if n <= 0 && !long {
		return core.Group(nil)
	}
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	dotColor := colors.Faint
	textColor := colors.Muted
	weight := ui.Regular
	if n > 0 {
		dotColor = colors.Up
		textColor = colors.Up
		weight = ui.Medium
	}
	label := strconv.Itoa(n)
	if long {
		label = tr.Online(n)
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 5},
		Label: tr.LiveTooltip(n, a.unitWord(n, tr.Visitor(), tr.VisitorsUnit())),
		Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(6), Height: core.Pt(6), CornerRadius: 3}, StyleFunc: ui.Filled(dotColor)},
			ui.Tabular(ui.Text(label, 11, weight, textColor)),
		}}
}

func (a *app) delta(cx *core.Scope, cur, prev float64, worse bool) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	f, ok := change(cur, prev)
	switch {
	case !ok && cur == 0:
		return muted(cx, tr.Dash(), 11)
	case !ok:
		return muted(cx, tr.New(), 11)
	case math.Abs(f) < 0.005:
		return muted(cx, "0%", 11)
	}
	good := f > 0 != worse
	c := colors.Down
	icon := lucide.ArrowDown
	if good {
		c = colors.Up
	}
	if f > 0 {
		icon = lucide.ArrowUp
	}
	return row(1, ui.Icon(icon, 10, c), ui.Tabular(ui.Text(percent(f), 11, ui.Medium, c)))
}

func (a *app) sparkline(cx *core.Scope, values []float64, h, w float32, wide bool) core.Node {
	style := core.Style{Height: core.Pt(core.Points(h)), FlexGrow: 1, MinWidth: core.Pt(0)}
	if w > 0 {
		style.Width, style.FlexGrow = core.Pt(core.Points(w)), 0
	}
	if len(values) < 2 {
		return core.View{Style: style}
	}
	vals := append([]float64(nil), values...)
	colors := ui.ColorsOf(cx)
	width := float32(1.5)
	if wide {
		width = 1.75
	}
	return core.Canvas{
		Style: style,
		Draw: func(d *core.Drawing, size core.Size) {
			a.track()
			hi := 0.0
			for _, v := range vals {
				if v > hi {
					hi = v
				}
			}
			empty := hi == 0
			if empty {
				hi = 1
			}
			accent := colors.Accent()
			if empty {
				accent = colors.Faint()
			}
			n := len(vals)
			w, ht := float32(size.Width), float32(size.Height)-2
			if w <= 0 || ht <= 0 {
				return
			}
			d.SetLineWidth(width)
			d.SetStrokeColor(accent)
			d.SetFillColor(ui.Alpha(accent, 0.2))
			d.BeginPath()
			for i, v := range vals {
				x := float32(i) / float32(n-1) * w
				y := 1 + ht - float32(v/hi*0.92)*ht
				if i == 0 {
					d.MoveTo(x, y)
				} else {
					d.LineTo(x, y)
				}
			}
			d.Stroke()
			d.LineTo(w, 1+ht)
			d.LineTo(0, 1+ht)
			d.ClosePath()
			d.Fill()
		},
	}
}

func (a *app) problem(cx *core.Scope, icon func() *core.SVG, title, detail string) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	actions := []core.Node{}
	if a.listErr != nil {
		actions = append(actions, ui.Button{Text: tr.TryAgain(), TestID: "retry", OnPress: a.refresh})
	} else {
		actions = append(actions, ui.Button{Text: tr.AddWebsite(), TestID: "empty-add", OnPress: a.startAdd})
	}
	actions = append(actions, ui.Button{Text: tr.Settings(), Variant: ui.ButtonSecondary, TestID: "open-settings", OnPress: func() {
		a.resetSettings()
		a.page = pageSettings
		a.invalidate()
	}})
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 6,
		Padding: core.All(24)},
		Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(48), Height: core.Pt(48), CornerRadius: 12, AlignItems: core.AlignCenter,
				JustifyContent: core.JustifyCenter, Margin: core.Edges{Bottom: 6}},
				StyleFunc: ui.Filled(colors.Inset), Children: []core.Node{ui.Icon(icon, 24, colors.Muted)}},
			ui.Text(title, 14, ui.Semibold, colors.Text),
			core.Text{Content: detail, Style: core.Style{FontSize: 12, TextAlign: core.TextAlignCenter, LineHeight: 16},
				StyleFunc: ui.Colored(colors.Muted)},
			core.View{Style: core.Style{Margin: core.Edges{Top: 8}}, Children: []core.Node{row(8, actions...)}},
		}}
}
