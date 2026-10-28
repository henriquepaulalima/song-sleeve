package update

import "errors"

// Relaunch is part of the Windows updater, which isn't built yet.
func Relaunch(string) error { return errors.New("not supported on Windows yet") }
