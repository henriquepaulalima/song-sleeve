//go:build production

package main

import "time"

func devSyncDelay() time.Duration { return 0 }
