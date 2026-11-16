// Umapp shows the websites of an Umami account, and their visitors, in a
// panel of the menu bar.
package main

import (
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	menus "github.com/nsgo-dev/nsgo/desktop/locales"
	"github.com/nsgo-dev/nsgo/updater"
)

// shell is what lives as long as the app: the tray icon, the panel while
// it is open, and the state the panel shows.
type shell struct {
	a     *app
	tray  *desktop.Tray
	panel *desktop.Window
}

func main() {
	s := &shell{}
	nsgo.Run(s.ready, nsgo.HideDock(), nsgo.WindowSize(panelWidth, panelHeight))
}

func (s *shell) ready(cx *core.Scope) {
	p, err := openPrefs(cx)
	if err != nil {
		log.Fatal(err)
	}
	cfg := p.load()
	var src source
	if cfg.Key != "" {
		src = newClient(cfg.Endpoint, cfg.Key)
	}
	a := newApp(cfg, src)
	s.a = a
	a.quit = cx.Quit
	a.onLive = func(n int) { s.updateTray() }
	a.changed = func(cfg config) {
		s.updateTray()
		p.save(cfg)
	}
	cx.SetLocale(cfg.Language)
	a.attach(cx)
	if runtime.GOOS == "darwin" {
		a.checkUpdates = func() { updater.Check(cx) }
		a.canLaunchAtLogin = true
		a.readLogin = func(done func(bool)) { desktop.LaunchAtLogin(cx, done) }
		a.setLogin = func(on bool) { desktop.SetLaunchAtLogin(cx, on) }
	}

	desktop.SetShutdownMode(cx, desktop.ShutdownOnExplicit)
	// The menus and the tray's tooltip say their words again as the
	// language changes.
	cx.Effect(func() {
		cx.Env().Language()
		cx.Untrack(func() {
			s.installMenu(cx)
			s.updateTray()
		})
	})
	s.createTray(cx)
	a.refresh()
	go s.tick()
}

func (s *shell) installMenu(cx *core.Scope) {
	tr := s.a.msgs()
	app := []desktop.MenuItem{
		{Label: tr.Settings(), Shortcut: "mod+,", OnSelect: func() {
			s.a.resetSettings()
			s.a.page = pageSettings
			s.a.invalidate()
			s.showPanel()
		}},
	}
	if s.a.checkUpdates != nil {
		app = append(app, desktop.MenuItem{Label: tr.CheckForUpdates(), OnSelect: s.a.checkUpdates})
	}
	app = append(app, desktop.MenuItem{Separator: true}, desktop.MenuItem{Role: desktop.MenuQuit})
	desktop.SetApplicationMenu(cx, []desktop.MenuItem{
		{Label: tr.AppName(), Items: app},
		{Label: menus.For(cx).Edit(), Items: []desktop.MenuItem{
			{Role: desktop.MenuUndo}, {Role: desktop.MenuRedo},
			{Separator: true},
			{Role: desktop.MenuCut}, {Role: desktop.MenuCopy}, {Role: desktop.MenuPaste}, {Role: desktop.MenuSelectAll},
		}},
	})
}

func (s *shell) createTray(cx *core.Scope) {
	s.panel = desktop.OpenPanel(cx, desktop.PanelOptions{Width: panelWidth, Height: panelHeight, HideOnBlur: true,
		Material: desktop.WindowMaterialPopover}, s.a.root)
	if s.panel == nil {
		// No panels here, as on iOS: the panel's content fills the app,
		// and loads again as the app comes back on numbers gone stale.
		s.a.noTray = true
		nsgo.SetMainView(cx, s.a.root)
		active := true
		cx.Effect(func() {
			was := active
			active = cx.Env().Lifecycle() == core.LifecycleActive
			if active && !was {
				cx.Untrack(func() {
					if s.a.stale() && s.a.loading == 0 {
						s.a.refresh()
					}
				})
			}
		})
		return
	}
	s.tray = desktop.NewTray(cx, desktop.TrayOptions{
		Icon:     trayIcon(),
		Template: true,
		Tooltip:  s.a.msgs().Title(),
		Panel:    s.panel,
		Menu:     s.trayMenu,
	})
	panel := s.panel
	shown := false
	cx.Effect(func() {
		visible := panel.Visible()
		if visible && !shown {
			cx.Untrack(func() {
				if s.a.stale() && s.a.loading == 0 {
					s.a.refresh()
				}
			})
		}
		if shown && !visible {
			cx.Untrack(func() {
				s.a.selected = -1
				s.a.invalidate()
			})
		}
		shown = visible
	})
	s.updateTray()
}

func (s *shell) trayMenu() []desktop.MenuItem {
	tr := s.a.msgs()
	var items []desktop.MenuItem
	if s.a.checkUpdates != nil {
		items = append(items, desktop.MenuItem{Label: tr.CheckForUpdates(), OnSelect: s.a.checkUpdates}, desktop.MenuItem{Separator: true})
	}
	return append(items, desktop.MenuItem{Label: tr.Quit(), OnSelect: s.a.quit})
}

func (s *shell) showPanel() {
	if s.panel == nil {
		if s.a.cx != nil {
			desktop.OrderFront(s.a.cx)
		}
		return
	}
	s.panel.Focus()
}

func (s *shell) updateTray() {
	if s.tray == nil || s.a == nil {
		return
	}
	tr := s.a.msgs()
	title, tip := "", tr.Title()
	live := s.a.liveTotal()
	if s.a.configured() && !s.a.cfg.HideLive && live > 0 {
		title = fmt.Sprint(live)
	}
	if s.a.configured() {
		tip = tr.TrayTooltip(live, s.a.unitWord(live, tr.Visitor(), tr.VisitorsUnit()))
	}
	s.tray.SetTitle(title)
	s.tray.SetTooltip(tip)
}

func (s *shell) tick() {
	n := 0
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for range tick.C {
		n++
		s.a.post(func() {
			if n%5 == 0 {
				s.a.refresh()
			} else {
				s.a.refreshActive()
			}
		})
	}
}
