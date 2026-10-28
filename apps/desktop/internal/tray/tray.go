// Package tray is the desktop app's notification-area icon (§9.4): it reopens the
// window, controls playback and quits, so closing the window can leave the app
// running with its listener. Linux uses StatusNotifierItem over D-Bus; other systems
// have no tray yet and closing the window quits.
package tray

// State is what the menu shows. The UI sends it translated (§8.8).
type State struct {
	Labels   map[string]string `json:"labels"` // show, play, pause, next, previous, quit
	Playing  bool              `json:"playing"`
	HasTrack bool              `json:"has_track"`
	Title    string            `json:"title"` // the playing song, for the tooltip
}

// Handlers run on the tray's goroutine.
type Handlers struct {
	Show   func()
	Quit   func()
	Action func(name string) // "toggle", "next" or "previous"
}

const appName = "Song Sleeve"

func label(st State, key, def string) string {
	if v := st.Labels[key]; v != "" {
		return v
	}
	return def
}
