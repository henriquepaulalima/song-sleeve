//go:build linux

package tray

import (
	_ "embed"
	"sync"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

//go:embed icon.png
var icon []byte

var (
	mu      sync.Mutex
	started bool
	items   struct{ show, toggle, next, previous, quit *systray.MenuItem }
	end     func()
	// last is the newest state from the UI; it may arrive before the menu exists.
	last State
)

// Start publishes the icon. With no tray host running (GNOME without the
// AppIndicator extension) the item waits and registers when one appears.
func Start(h Handlers) {
	mu.Lock()
	defer mu.Unlock()
	if started {
		return
	}
	started = true
	ready := make(chan struct{})
	start, stop := systray.RunWithExternalLoop(func() {
		mu.Lock()
		defer mu.Unlock()
		systray.SetIcon(icon)
		systray.SetTitle(appName)
		systray.SetTooltip(appName)
		items.show = systray.AddMenuItem("Show Song Sleeve", "")
		systray.AddSeparator()
		items.toggle = systray.AddMenuItem("Play", "")
		items.previous = systray.AddMenuItem("Previous song", "")
		items.next = systray.AddMenuItem("Next song", "")
		systray.AddSeparator()
		items.quit = systray.AddMenuItem("Quit Song Sleeve", "")
		apply(last)
		systray.SetOnTapped(h.Show)
		close(ready)
	}, nil)
	end = stop
	start()
	go func() {
		<-ready
		for {
			select {
			case <-items.show.ClickedCh:
				h.Show()
			case <-items.toggle.ClickedCh:
				h.Action("toggle")
			case <-items.next.ClickedCh:
				h.Action("next")
			case <-items.previous.ClickedCh:
				h.Action("previous")
			case <-items.quit.ClickedCh:
				h.Quit()
				return
			}
		}
	}()
}

// Update relabels the menu. Before the menu is built, the state is kept and
// applied when it is.
func Update(st State) {
	mu.Lock()
	defer mu.Unlock()
	last = st
	if items.quit == nil {
		return
	}
	apply(st)
}

func apply(st State) {
	items.show.SetTitle(label(st, "show", "Show Song Sleeve"))
	if st.Playing {
		items.toggle.SetTitle(label(st, "pause", "Pause"))
	} else {
		items.toggle.SetTitle(label(st, "play", "Play"))
	}
	items.next.SetTitle(label(st, "next", "Next song"))
	items.previous.SetTitle(label(st, "previous", "Previous song"))
	items.quit.SetTitle(label(st, "quit", "Quit Song Sleeve"))
	for _, it := range []*systray.MenuItem{items.toggle, items.next, items.previous} {
		if st.HasTrack {
			it.Enable()
		} else {
			it.Disable()
		}
	}
	if st.Title != "" {
		systray.SetTooltip(st.Title + " · " + appName)
	} else {
		systray.SetTooltip(appName)
	}
}

// Visible reports whether a tray host is showing the icon right now, which is
// when closing the window may hide it instead of quitting.
func Visible() bool {
	mu.Lock()
	ok := started
	mu.Unlock()
	if !ok {
		return false
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var owned bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owned); err != nil || !owned {
		return false
	}
	v, err := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher").GetProperty("org.kde.StatusNotifierWatcher.IsStatusNotifierHostRegistered")
	if err != nil {
		return false
	}
	host, _ := v.Value().(bool)
	return host
}

// Stop removes the icon.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if end != nil {
		end()
		end = nil
	}
}
