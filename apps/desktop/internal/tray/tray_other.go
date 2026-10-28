//go:build !linux

package tray

// Windows gets its notification-area icon with the rest of the Windows work.

func Start(Handlers) {}

func Update(State) {}

func Visible() bool { return false }

func Stop() {}
