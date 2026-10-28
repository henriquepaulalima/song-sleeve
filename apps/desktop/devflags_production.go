//go:build production

package main

import "time"

func devSyncDelay() time.Duration { return 0 }

func devCrashTest() bool { return false }

func devSentryDSN() string { return "" }

// buildEnvironment tags crash reports.
const buildEnvironment = "production"
