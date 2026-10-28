//go:build linux && integration

package tray

import (
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// watcher is a stand-in StatusNotifierWatcher with a host, like KDE's panel or
// GNOME's AppIndicator extension.
type watcher struct{ registered chan string }

func (w *watcher) RegisterStatusNotifierItem(sender dbus.Sender, path string) *dbus.Error {
	w.registered <- string(sender) + path
	return nil
}

type item struct {
	id      int32
	label   string
	enabled bool
}

// menuItems flattens a com.canonical.dbusmenu layout.
func menuItems(v []any) []item {
	var out []item
	props, _ := v[1].(map[string]dbus.Variant)
	if l, ok := props["label"]; ok {
		en := true
		if e, ok := props["enabled"]; ok {
			en, _ = e.Value().(bool)
		}
		lbl, _ := l.Value().(string)
		out = append(out, item{id: v[0].(int32), label: lbl, enabled: en})
	}
	children, _ := v[2].([]dbus.Variant)
	for _, c := range children {
		out = append(out, menuItems(c.Value().([]any))...)
	}
	return out
}

// Runs on a private bus: make e2e starts it through dbus-run-session.
func TestTrayOverDBus(t *testing.T) {
	if os.Getenv("SONG_SLEEVE_PRIVATE_BUS") != "1" {
		t.Skip("needs a private session bus (dbus-run-session)")
	}
	shown, quit, actions := make(chan struct{}, 4), make(chan struct{}, 1), make(chan string, 4)
	// The page can send its translated labels before the menu exists.
	pt := map[string]string{"show": "Mostrar o Song Sleeve", "play": "Tocar", "pause": "Pausar", "next": "Próxima música", "previous": "Música anterior", "quit": "Sair do Song Sleeve"}
	Update(State{Labels: pt})
	Start(Handlers{
		Show:   func() { shown <- struct{}{} },
		Quit:   func() { quit <- struct{}{} },
		Action: func(name string) { actions <- name },
	})
	defer Stop()
	time.Sleep(200 * time.Millisecond)
	if Visible() {
		t.Fatal("visible with no tray host")
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	w := &watcher{registered: make(chan string, 2)}
	if err := conn.Export(w, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	if _, err := prop.Export(conn, "/StatusNotifierWatcher", prop.Map{"org.kde.StatusNotifierWatcher": {
		"IsStatusNotifierHostRegistered": {Value: true},
		"RegisteredStatusNotifierItems":  {Value: []string{}},
	}}); err != nil {
		t.Fatal(err)
	}
	if r, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own the watcher name: %v %v", r, err)
	}
	var owner string
	select {
	case owner = <-w.registered:
	case <-time.After(3 * time.Second):
		t.Fatal("the icon didn't register when a tray host appeared")
	}
	if !Visible() {
		t.Fatal("not visible with a tray host")
	}
	sender := owner[:len(owner)-len("/StatusNotifierItem")]

	layout := func() []item {
		var rev uint32
		var l []any
		call := conn.Object(sender, "/StatusNotifierItem/menu").Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{})
		if err := call.Store(&rev, &l); err != nil {
			t.Fatal(err)
		}
		return menuItems(l)
	}
	find := func(items []item, label string) item {
		for _, it := range items {
			if it.label == label {
				return it
			}
		}
		t.Fatalf("no %q in %+v", label, items)
		return item{}
	}

	// Nothing loaded: playback items are off, and the early labels were kept.
	if it := find(layout(), "Tocar"); it.enabled {
		t.Fatal("Play is enabled with nothing loaded")
	}

	Update(State{Labels: pt, Playing: true, HasTrack: true, Title: "Song · Artist"})
	items := layout()
	toggle := find(items, "Pausar")
	if !toggle.enabled || !find(items, "Próxima música").enabled {
		t.Fatalf("playback items should be on: %+v", items)
	}
	find(items, "Mostrar o Song Sleeve")
	tip, err := conn.Object(sender, "/StatusNotifierItem").GetProperty("org.kde.StatusNotifierItem.ToolTip")
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := tip.Value().([]any); len(s) < 3 || s[2] != "Song · Artist · Song Sleeve" {
		t.Fatalf("tooltip = %v", tip.Value())
	}

	click := func(id int32) {
		if err := conn.Object(sender, "/StatusNotifierItem/menu").Call("com.canonical.dbusmenu.Event", 0, id, "clicked", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
			t.Fatal(err)
		}
	}
	click(toggle.id)
	select {
	case a := <-actions:
		if a != "toggle" {
			t.Fatalf("action = %q", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("play/pause didn't reach the app")
	}

	if err := conn.Object(sender, "/StatusNotifierItem").Call("org.kde.StatusNotifierItem.Activate", 0, int32(0), int32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-shown:
	case <-time.After(2 * time.Second):
		t.Fatal("clicking the icon didn't show the window")
	}

	click(find(items, "Sair do Song Sleeve").id)
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("Quit didn't reach the app")
	}
}
