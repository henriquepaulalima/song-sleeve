//go:build (dev || production) && linux

package main

/*
#cgo pkg-config: glib-2.0
#include <glib.h>
*/
import "C"

// setAppName names the app for the desktop: WebKitGTK reports it as the MPRIS
// Identity, so system media controls say "Song Sleeve" (§9.4).
func setAppName() {
	C.g_set_application_name(C.CString("Song Sleeve"))
}
