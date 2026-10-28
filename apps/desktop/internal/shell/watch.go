package shell

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/henriquepaulalima/song-sleeve/desktop/internal/songsdir"
)

// watchDelay coalesces a burst of changes (a file manager deleting many files, a
// copy in progress) into one check.
var watchDelay = 2 * time.Second

// watchFolder runs the quick folder check soon after anything changes in the
// Song Sleeve folder or this account's folder while the app runs (§7.13): a song
// deleted or edited by hand is restored, a file added by hand is removed. The
// check is part of reconciliation, so it also waits for catching up before
// deleting anything.
func (s *Shell) watchFolder(ctx context.Context, a *account) {
	defer a.done.Done()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("folder watcher unavailable", "error", err)
		return
	}
	defer w.Close()
	dirs := []string{a.songs.Root, a.songs.AccountDir}
	watching := map[string]bool{}
	// Folders can be deleted and recreated by hand (and by the check), so the
	// watches are re-added whenever they're missing.
	ensure := func() {
		for _, d := range dirs {
			if !watching[d] && w.Add(d) == nil {
				watching[d] = true
			}
		}
	}
	ensure()

	var timer <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				for _, d := range dirs {
					if ev.Name == d {
						watching[d] = false
					}
				}
			}
			if ignoredChange(ev.Name) {
				continue
			}
			if timer == nil {
				timer = time.After(watchDelay)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("folder watcher", "error", err)
		case <-timer:
			timer = nil
			ensure()
			a.reconcileSoon()
		}
	}
}

// ignoredChange skips the app's own bookkeeping: every check rewrites the manifest,
// and reacting to that would loop.
func ignoredChange(path string) bool {
	switch filepath.Base(path) {
	case songsdir.ManifestName, songsdir.ManifestName + ".tmp":
		return true
	}
	return false
}
