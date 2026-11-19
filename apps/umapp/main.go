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
	a      *app
	tray   *desktop.Tray
	panel  *desktop.Window
	window *desktop.Window // the main window while it is open
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
	a.tracked = -1
	a.sendView = startAnalytics(func() string { return core.Untracked(cx, cx.Env().Language) })
	a.trackPage()
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
	if !a.noTray {
		a.openWindow = func() { s.openWindow(cx) }
		// The Dock icon shows while the window is open; clicking it brings the window back.
		desktop.OnReopen(cx, a.openWindow)
		s.installMenu(cx)
	}
	// umapp://connect links, as npm run deploy opens, fill in the settings for the user to confirm.
	desktop.OnOpenLinks(cx, func(links []string) {
		if len(links) > 0 && a.openLink(links[len(links)-1]) {
			s.showPanel()
		}
	})
	a.refresh()
	go s.tick()
}

func (s *shell) installMenu(cx *core.Scope) {
	tr := s.a.msgs()
	app := []desktop.MenuItem{}
	if s.a.openWindow != nil {
		app = append(app, desktop.MenuItem{Label: tr.OpenWindow(), Shortcut: "mod+0", OnSelect: s.a.openWindow})
	}
	app = append(app, []desktop.MenuItem{
		{Label: tr.Settings(), Shortcut: "mod+,", OnSelect: func() {
			s.a.resetSettings()
			s.a.page = pageSettings
			s.a.invalidate()
			s.showPanel()
		}},
	}...)
	if s.a.checkUpdates != nil {
		app = append(app, desktop.MenuItem{Label: tr.CheckForUpdates(), OnSelect: s.a.checkUpdates})
	}
	app = append(app, desktop.MenuItem{Separator: true}, desktop.MenuItem{Role: desktop.MenuQuit})
	bar := []desktop.MenuItem{
		{Label: tr.AppName(), Items: app},
		{Label: menus.For(cx).Edit(), Items: []desktop.MenuItem{
			{Role: desktop.MenuUndo}, {Role: desktop.MenuRedo},
			{Separator: true},
			{Role: desktop.MenuCut}, {Role: desktop.MenuCopy}, {Role: desktop.MenuPaste}, {Role: desktop.MenuSelectAll},
		}},
	}
	if s.a.openWindow != nil {
		// The main window's own menu, as macOS apps have one.
		bar = append(bar, desktop.MenuItem{Label: menus.For(cx).Window(), Items: []desktop.MenuItem{
			{Role: desktop.MenuMinimize}, {Role: desktop.MenuZoom}, {Role: desktop.MenuToggleFullScreen},
			{Separator: true},
			{Role: desktop.MenuClose},
		}})
	}
	desktop.SetApplicationMenu(cx, bar)
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
	if s.a.openWindow != nil {
		items = append(items, desktop.MenuItem{Label: tr.OpenWindow(), OnSelect: s.a.openWindow}, desktop.MenuItem{Separator: true})
	}
	if s.a.checkUpdates != nil {
		items = append(items, desktop.MenuItem{Label: tr.CheckForUpdates(), OnSelect: s.a.checkUpdates}, desktop.MenuItem{Separator: true})
	}
	return append(items, desktop.MenuItem{Label: tr.Quit(), OnSelect: s.a.quit})
}

// openWindow shows the main window, opening it the first time, and the app's icon in the Dock while it is open, as the app is a menu bar app otherwise.
func (s *shell) openWindow(cx *core.Scope) {
	if s.window != nil && !s.window.Closed() {
		s.window.Focus()
		return
	}
	// The app's scope owns the window: opened from the panel's bar, the page's region running then would close it as it builds again.
	cx.Run(func() {
		s.window = desktop.OpenWindow(cx, desktop.WindowOptions{Title: s.a.msgs().AppName(), Key: "main",
			Width: windowWidth, Height: windowHeight,
			Constraints: desktop.WindowConstraints{MinWidth: 860, MinHeight: 480}}, s.a.windowRoot)
	})
	if s.window == nil {
		return
	}
	desktop.SetDockVisible(cx, true)
	s.window.OnClosing(cx, func(*desktop.WindowClosing) { desktop.SetDockVisible(cx, false) })
	if s.panel != nil {
		s.panel.Hide()
	}
	s.window.Focus()
}

// showPanel shows the page the app's state is on: in the main window while it is open, else in the panel.
func (s *shell) showPanel() {
	if s.window != nil && !s.window.Closed() {
		s.window.Focus()
		return
	}
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
