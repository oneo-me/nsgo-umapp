package main

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// The main window's size as it first opens, and its sidebar's, which shows the panel's list at the panel's fixed width.
const (
	windowWidth  = 1200
	windowHeight = 760
	sidebarWidth = panelWidth
)

// windowRoot is the main window: the panel's list in a sidebar beside the page of the website chosen there, wider. Both follow the same state, so a website chosen here is the panel's too.
func (a *app) windowRoot(cx *core.Scope) core.Node {
	a.bindKeys(cx)
	tr := msgsOf(cx)
	open := core.NewSignal(cx, true)
	return ui.SidebarShell{TestID: "window",
		Open: open.Get, OnOpenChange: open.Set, ToggleLabel: tr.ToggleSidebar(),
		DefaultWidth: sidebarWidth, MinWidth: sidebarWidth, Fixed: true,
		SidebarActions: []core.Node{a.region(a.windowRangeKey, a.windowRange)},
		Sidebar:        []core.Node{a.region(a.sidebarKey, a.windowSidebar)},
		SidebarFooter: []core.Node{
			a.toolButton(cx, lucide.Plus, tr.AddWebsite(), live(a, func() bool { return !a.configured() }), a.startAdd, "window-add"),
			ui.Spacer{},
			a.toolButton(cx, lucide.Settings, tr.Settings(), nil, a.showSettings, "window-settings"),
		},
		Header:  []core.Node{a.region(a.pageKey, a.windowTitle)},
		Actions: []core.Node{a.region(a.windowActionsKey, a.windowActions)},
		Content: []core.Node{a.region(a.pageKey, a.windowContent)},
	}
}

// sidebarKey changes as what the sidebar shows does: the list as the panel shows it, the website open, and each website's name and domain.
func (a *app) sidebarKey() string {
	var b strings.Builder
	fmt.Fprint(&b, a.page, a.open, a.configured(), a.listContentKey(), ";")
	for _, s := range a.ordered() {
		fmt.Fprintf(&b, "%s:%s:%s;", s.ID, s.Name, s.Domain)
	}
	return b.String()
}

// windowSidebar is the panel's list: the totals, then every website with its trend and counts, the one open selected.
func (a *app) windowSidebar(cx *core.Scope) core.Node {
	if !a.configured() {
		return core.View{}
	}
	if len(a.sites) == 0 {
		return core.View{Style: core.Style{FlexGrow: 1, Padding: core.Symmetric(0, gutter-inset)}, Children: []core.Node{a.list(cx)}}
	}
	rows := []core.Node{a.summary(cx), a.listHeading(cx)}
	for _, s := range a.ordered() {
		rows = append(rows, a.sidebarSite(cx, s))
	}
	return core.View{Style: core.Style{Gap: 1, Padding: core.Symmetric(0, gutter-inset)}, Children: rows}
}

func (a *app) sidebarSite(cx *core.Scope, s *site) core.Node {
	id := s.ID
	row := a.siteRow(cx, s, "window-site-"+id, func() bool { return a.page == pageSite && a.open == id })
	row.Style = row.Style.Merge(core.Style{FlexShrink: 0, MinWidth: core.Pt(0), Cursor: core.CursorDefault})
	return ui.ContextMenu{Trigger: row, Children: a.siteMenu(cx, s), TestID: "window-site-menu-" + id}
}

// siteMenu is what a secondary click on a website in the sidebar offers: its settings, the website itself, its Umami page when the server has one, and deleting it.
func (a *app) siteMenu(cx *core.Scope, s *site) []core.Node {
	tr := msgsOf(cx)
	id, domain := s.ID, s.Domain
	items := []core.Node{
		ui.MenuItem{Text: tr.WebsiteSettings(), Icon: lucide.SlidersHorizontal, OnSelect: func() { a.manageSite(id) }},
	}
	if domain != "" {
		items = append(items, ui.MenuItem{Text: tr.OpenWebsite(), Icon: lucide.Globe, OnSelect: func() { cx.OpenURL("https://"+domain, nil) }})
	}
	if u := dashboardURL(a.cfg.Endpoint, id); u != "" && a.src != nil && a.src.hasDashboard() {
		items = append(items, ui.MenuItem{Text: tr.OpenUmami(), Icon: lucide.ExternalLink, OnSelect: func() { cx.OpenURL(u, nil) }})
	}
	return append(items, ui.MenuSeparator{},
		ui.MenuItem{Text: tr.DeleteWebsite(), Icon: lucide.Trash, Variant: ui.MenuItemDanger,
			Disabled: live(a, func() bool { return a.deleting }), OnSelect: func() { a.confirmDeleteSite(cx, id) }})
}

// showOverview shows the list alone: the panel's, and in the window the sidebar beside no website's page.
func (a *app) showOverview() {
	if a.page == pageSettings {
		a.resetSettings()
	}
	a.page, a.selected, a.copied = pageSites, -1, ""
	a.invalidate()
}

// windowTitle names the page the window shows.
func (a *app) windowTitle(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	title := tr.Websites()
	switch {
	case !a.configured():
		title = tr.ConnectTitle()
	case a.page == pageSettings:
		title = tr.SettingsTitle()
	case a.page == pageAdd:
		title = tr.AddWebsite()
	case a.page == pageManage:
		title = a.managed.Name
	case a.page == pageSite && a.site(a.open) != nil:
		title = a.site(a.open).Name
	}
	text := oneLine(ui.Text(title, 13, ui.Semibold, colors.Text))
	text.Role = core.RoleHeader
	return text
}

func (a *app) windowRangeKey() string { return fmt.Sprint(a.configured()) }

// windowRange is the range the sidebar's list and the website's page show, at the end of the sidebar's bar.
func (a *app) windowRange(cx *core.Scope) core.Node {
	if !a.configured() {
		return core.View{}
	}
	return row(0, a.rangeControl(cx))
}

func (a *app) windowActionsKey() string {
	return fmt.Sprint(a.pageKey(), a.src != nil && a.src.hasDashboard())
}

// windowActions are the content's tools: refresh for the lists and charts, and a website's settings and Umami page on its own.
func (a *app) windowActions(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	if !a.configured() || a.page != pageSites && a.page != pageSite {
		return core.View{}
	}
	var nodes []core.Node
	if s := a.site(a.open); a.page == pageSite && s != nil {
		id := s.ID
		nodes = append(nodes, a.toolButton(cx, lucide.SlidersHorizontal, tr.WebsiteSettings(), nil, func() { a.manageSite(id) }, "window-manage"))
		if u := dashboardURL(a.cfg.Endpoint, id); u != "" && a.src.hasDashboard() {
			nodes = append(nodes, a.toolButton(cx, lucide.ExternalLink, tr.OpenUmami(), nil, func() { cx.OpenURL(u, nil) }, "window-open"))
		}
	}
	nodes = append(nodes, a.refreshButton(cx))
	return row(4, nodes...)
}

// windowContent is the page the panel would show, without the panel's own bars, and a site's page laid out wide.
func (a *app) windowContent(cx *core.Scope) core.Node {
	switch {
	case !a.configured() || a.page == pageSettings:
		return formColumn(a.settingsContent(cx))
	case a.page == pageAdd:
		return formColumn(a.addContent(cx))
	case a.page == pageManage:
		return formColumn(a.manageContent(cx))
	case a.page == pageSite && a.site(a.open) != nil:
		return a.region(a.detailKey, func(cx *core.Scope) core.Node { return a.detailView(cx, a.site(a.open), true) })
	}
	return a.chooseWebsite(cx)
}

// chooseWebsite fills the window's page while no website's is open.
func (a *app) chooseWebsite(cx *core.Scope) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 10, Padding: core.All(24)},
		TestID: "choose-website",
		Children: []core.Node{
			ui.Icon(lucide.MousePointerClick, 28, colors.Faint),
			core.Text{Content: msgsOf(cx).ChooseWebsite(), Style: core.Style{FontSize: 13, TextAlign: core.TextAlignCenter},
				StyleFunc: ui.Colored(colors.Muted)},
		}}
}

// formMaxWidth keeps a form's labels near their values in a wide window.
const formMaxWidth = 680

// formColumn centers a form page in the window at a width it reads well at.
func formColumn(page core.Node) core.Node {
	return core.View{Style: ui.FillRest.Merge(core.Style{AlignItems: core.AlignCenter}), Children: []core.Node{
		core.View{Style: core.Style{FlexGrow: 1, Width: core.Pct(100), MaxWidth: core.Pt(formMaxWidth), MinHeight: core.Pt(0)}, Children: []core.Node{page}},
	}}
}
