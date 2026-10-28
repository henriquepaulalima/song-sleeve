//go:build dev

package main

import (
	"log"
	"os"
	"time"
)

// devSyncDelay reads SONG_SLEEVE_DEV_SYNC_DELAY (e.g. 1m): every sync waits that
// long before uploading, so a sync can be caught in progress on a fast local
// network. Production builds ignore it.
func devSyncDelay() time.Duration {
	v := os.Getenv("SONG_SLEEVE_DEV_SYNC_DELAY")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("SONG_SLEEVE_DEV_SYNC_DELAY=%q: %v", v, err)
		return 0
	}
	log.Printf("development: every sync waits %s before uploading", d)
	return d
}
