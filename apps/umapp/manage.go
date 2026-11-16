package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// openManage shows website w's page of its own: its name and domain to edit, the tracking code to embed, whether it sends data, and its deletion. Back returns to from.
func (a *app) openManage(w website, from page) {
	a.managed, a.manageFrom = w, from
	a.siteName, a.siteDomain = w.Name, w.Domain
	a.saving, a.saveErr, a.deleting, a.copied = false, "", false, ""
	a.checked, a.checkErr = false, ""
	a.page = pageManage
	a.invalidate()
	a.checkInstall()
}

// manageSite opens the page of the site with id, from the page showing now.
func (a *app) manageSite(id string) {
	if s := a.site(id); s != nil {
		a.openManage(s.website, a.page)
	}
}

// checkInstall looks for the managed website's views of the last day, which show its tracking code at work.
func (a *app) checkInstall() {
	if a.checking || a.src == nil {
		return
	}
	id, src := a.managed.ID, a.src
	to := a.now()
	a.checking, a.checkErr = true, ""
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cur, _, err := src.stats(ctx, id, to.Add(-24*time.Hour), to, "")
		a.post(func() {
			a.checking = false
			if id != a.managed.ID {
				return
			}
			a.checked = err == nil
			a.checkViews = float64(cur.Pageviews)
			if err != nil {
				a.checkErr = err.Error()
			}
		})
	})
}

// siteDirty reports whether the manage page's name or domain differs from the website's.
func (a *app) siteDirty() bool {
	return strings.TrimSpace(a.siteName) != a.managed.Name || bareDomain(a.siteDomain) != a.managed.Domain
}

// saveWebsite saves the manage page's name and domain.
func (a *app) saveWebsite() {
	domain := bareDomain(a.siteDomain)
	if a.saving || domain == "" || !a.siteDirty() || a.src == nil {
		return
	}
	name := strings.TrimSpace(a.siteName)
	if name == "" {
		name = domain
	}
	id, src := a.managed.ID, a.src
	a.saving, a.saveErr = true, ""
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		w, err := src.updateWebsite(ctx, id, name, domain)
		a.post(func() {
			a.saving = false
			if id != a.managed.ID {
				return
			}
			if err != nil {
				a.saveErr = err.Error()
				return
			}
			a.managed = w
			a.siteName, a.siteDomain = w.Name, w.Domain
			if s := a.site(id); s != nil {
				s.website = w
			}
		})
	})
}

// warnDeleted tells, when there is a warning, what deleting w left on Cloudflare, which still sends its site's requests through a Worker. It shows before the page w was on goes, from the scope that asked to delete it.
func (a *app) warnDeleted(cx *core.Scope, w website, warning string) {
	if warning == "" {
		return
	}
	tr := msgsOf(cx)
	base.ShowAlertDialog(cx, base.AlertDialog{
		Title:       tr.DeletedWithWarning(w.Name),
		Description: warning,
		Actions:     []core.AlertAction{{Label: tr.Ok(), Role: core.AlertCancel}},
	}, nil)
}

// confirmDelete asks before deleting the managed website, then deletes it and returns to where its page was opened from, or the list for its own page.
func (a *app) confirmDelete(cx *core.Scope) {
	tr := msgsOf(cx)
	del := tr.Delete()
	base.ShowAlertDialog(cx, base.AlertDialog{
		Title:       tr.DeleteTitle(a.managed.Name),
		Description: tr.DeleteBody(),
		Actions: []core.AlertAction{
			{Label: tr.Cancel(), Role: core.AlertCancel},
			{Label: del, Role: core.AlertDestructive},
		},
	}, func(label string) {
		if label == del {
			a.deleteWebsite(cx)
		}
	})
}

// confirmDeleteSite asks before deleting the website with id from where it is listed, then deletes it, showing the list if its page was open. When deleting fails, its settings show why, with the button to try again.
func (a *app) confirmDeleteSite(cx *core.Scope, id string) {
	s := a.site(id)
	if s == nil {
		return
	}
	tr := msgsOf(cx)
	del := tr.Delete()
	w := s.website
	base.ShowAlertDialog(cx, base.AlertDialog{
		Title:       tr.DeleteTitle(w.Name),
		Description: tr.DeleteBody(),
		Actions: []core.AlertAction{
			{Label: tr.Cancel(), Role: core.AlertCancel},
			{Label: del, Role: core.AlertDestructive},
		},
	}, func(label string) {
		if label == del {
			a.deleteSite(cx, w)
		}
	})
}

func (a *app) deleteSite(cx *core.Scope, w website) {
	if a.deleting || a.src == nil {
		return
	}
	src := a.src
	a.deleting = true
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		warning, err := src.deleteWebsite(ctx, w.ID)
		a.post(func() {
			a.deleting = false
			if err != nil {
				a.openManage(w, a.page)
				a.saveErr = err.Error()
				return
			}
			a.warnDeleted(cx, w, warning)
			a.sites = slices.DeleteFunc(a.sites, func(s *site) bool { return s.ID == w.ID })
			if a.open == w.ID || a.page == pageManage && a.managed.ID == w.ID {
				a.open = ""
				a.showOverview()
			}
			a.refresh()
		})
	})
}

func (a *app) deleteWebsite(cx *core.Scope) {
	if a.deleting || a.src == nil {
		return
	}
	id, src := a.managed.ID, a.src
	a.deleting, a.saveErr = true, ""
	a.invalidate()
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		warning, err := src.deleteWebsite(ctx, id)
		a.post(func() {
			a.deleting = false
			if id != a.managed.ID {
				return
			}
			if err != nil {
				a.saveErr = err.Error()
				return
			}
			a.warnDeleted(cx, a.managed, warning)
			a.sites = slices.DeleteFunc(a.sites, func(s *site) bool { return s.ID == id })
			if a.open == id {
				a.open = ""
			}
			// The website's own page is gone with it: return to the list instead.
			if a.manageFrom == pageSite {
				a.manageFrom = pageSites
			}
			a.back()
			a.refresh()
		})
	})
}

// managePage builds its sections apart, so that typing in the fields rebuilds none of them.
func (a *app) managePage(cx *core.Scope) core.Node {
	return core.View{Style: ui.FillRest, Children: []core.Node{
		a.region(func() string { return a.managed.Name }, func(cx *core.Scope) core.Node {
			return a.pageHeader(cx, a.managed.Name)
		}),
		a.manageContent(cx),
	}}
}

// manageContent is the website's page without its bar, which the window shows under its own.
func (a *app) manageContent(cx *core.Scope) core.Node {
	body := a.region(func() string { return a.managed.ID }, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Padding: core.All(gutter), Gap: sectionGap,
			PaddingEnv: core.EnvEdges{Bottom: core.EnvKeyboard}},
			Children: []core.Node{
				column(0, a.siteFields(cx), a.region(func() string {
					return fmt.Sprint(a.saving, a.saveErr, a.siteDirty())
				}, a.siteStatus)),
				a.region(func() string { return a.copied }, func(cx *core.Scope) core.Node { return a.codeSection(cx, a.managed.ID) }),
				a.region(func() string {
					return fmt.Sprint(a.checking, a.checked, a.checkViews, a.checkErr)
				}, a.installSection),
				a.region(func() string { return fmt.Sprint(a.deleting) }, a.deleteSection),
			}}
	})
	return core.Scroll{Style: ui.FillRest, TestID: "manage", Children: []core.Node{body}}
}

// siteFields edit the website's name and domain.
func (a *app) siteFields(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	busy := live(a, func() bool { return a.saving || a.deleting })
	return ui.FormSection{Children: []core.Node{
		ui.FormField{Title: tr.WebsiteName(), Input: base.Input{
			Value: live(a, func() string { return a.siteName }), Placeholder: a.managed.Domain,
			Disabled: busy, TestID: "site-name",
			OnChange: func(s string) { a.siteName = s; a.invalidate() },
			OnSubmit: func(string) { a.saveWebsite() },
		}},
		ui.FormField{Title: tr.Domain(), Input: base.Input{
			Value: live(a, func() string { return a.siteDomain }), Placeholder: "example.com",
			Disabled: busy, TestID: "site-domain",
			OnChange: func(s string) { a.siteDomain = s; a.invalidate() },
			OnSubmit: func(string) { a.saveWebsite() },
		}},
	}}
}

// siteStatus is what went wrong saving or deleting, and Save while the fields differ from the saved name and domain.
func (a *app) siteStatus(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	var rows []core.Node
	if a.saveErr != "" {
		rows = append(rows, ui.FormRow{Icon: lucide.CircleAlert, Title: a.saveErr, Variant: ui.FormRowDanger})
	}
	if a.siteDirty() || a.saving {
		row := ui.FormRow{Title: tr.Save(), Variant: ui.FormRowAccent, TestID: "save-site", OnPress: a.saveWebsite,
			Disabled: live(a, func() bool { return a.saving || bareDomain(a.siteDomain) == "" })}
		if a.saving {
			row.Title, row.Control = tr.Saving(), ui.Spinner{Size: 14}
		}
		rows = append(rows, row)
	}
	if rows == nil {
		return core.View{}
	}
	return ui.FormSection{Style: core.Style{Margin: core.Edges{Top: sectionGap}}, Children: rows}
}

// codeSection shows website id's tracking code, to copy.
func (a *app) codeSection(cx *core.Scope, id string) core.Node {
	tr := msgsOf(cx)
	colors := ui.ColorsOf(cx)
	if a.managed.AutoTrack {
		return ui.FormSection{Title: tr.TrackingCode(), Footer: tr.AutoTrackedHint(), Children: []core.Node{
			ui.FormRow{Icon: lucide.CircleCheck, Title: tr.AutoTracked(), TestID: "auto-tracked"},
		}}
	}
	code := core.Text{Content: trackingCode(a.cfg.Endpoint, id), Selectable: core.SelectableOn, TestID: "tracking-code",
		Style:     core.Style{FontFamily: ui.Mono, FontSize: 11, LineHeight: 16, Padding: core.All(inset)},
		StyleFunc: ui.Colored(colors.Text)}
	return ui.FormSection{Title: tr.TrackingCode(), Footer: tr.TrackingCodeHint(), Children: []core.Node{code, a.copyRow(cx, id)}}
}

func (a *app) copyRow(cx *core.Scope, id string) core.Node {
	tr := msgsOf(cx)
	if a.copied == id {
		return ui.FormRow{Icon: lucide.Check, Title: tr.Copied(), TestID: "copy"}
	}
	return ui.FormRow{Icon: lucide.Copy, Title: tr.Copy(), Variant: ui.FormRowAccent, TestID: "copy",
		OnPress: func() { a.copyTrackingCode(cx, id) }}
}

// copyTrackingCode puts website id's tracking code on the clipboard.
func (a *app) copyTrackingCode(cx *core.Scope, id string) {
	cx.SetClipboard(trackingCode(a.cfg.Endpoint, id))
	a.copied = id
	a.invalidate()
}

// installSection says whether the website sent data in the last day, which shows its tracking code at work.
func (a *app) installSection(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	var status core.Node
	switch {
	case a.checking:
		status = ui.FormRow{Title: tr.Checking(), Control: ui.Spinner{Size: 14}, TestID: "install-status"}
	case a.checkErr != "":
		status = ui.FormRow{Icon: lucide.CircleAlert, Title: a.checkErr, Variant: ui.FormRowDanger, TestID: "install-status"}
	case a.checked && a.checkViews > 0:
		status = ui.FormRow{Icon: lucide.CircleCheck, Title: tr.Receiving(), Detail: tr.ViewsLastDay(int(a.checkViews)), TestID: "install-status"}
	default:
		hint := tr.NoDataYetHint()
		if a.managed.AutoTrack {
			hint = tr.NoDataYetAutoHint()
		}
		status = ui.FormRow{Icon: lucide.CircleDashed, Title: tr.NoDataYet(), Detail: hint, TestID: "install-status"}
	}
	return ui.FormSection{Title: tr.Installation(), Children: []core.Node{
		status,
		ui.FormRow{Icon: lucide.RotateCw, Title: tr.CheckAgain(), Variant: ui.FormRowAccent, TestID: "check-install",
			OnPress: a.checkInstall, Disabled: live(a, func() bool { return a.checking })},
	}}
}

func (a *app) deleteSection(cx *core.Scope) core.Node {
	tr := msgsOf(cx)
	row := ui.FormRow{Icon: lucide.Trash, Title: tr.DeleteWebsite(), Variant: ui.FormRowDanger, TestID: "delete-site",
		OnPress: func() { a.confirmDelete(cx) }, Disabled: live(a, func() bool { return a.saving || a.deleting })}
	if a.deleting {
		row.Title, row.Control = tr.Deleting(), ui.Spinner{Size: 14}
	}
	return ui.FormSection{Children: []core.Node{row}}
}
