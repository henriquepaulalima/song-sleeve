//go:build !windows

package update

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// Relaunch starts path once this process has exited, so the new copy doesn't
// hand itself to the old one through the single-instance lock. The caller quits
// right after.
func Relaunch(path string) error {
	closeInherited()
	cmd := exec.Command("/bin/sh", "-c", `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; exec "$2"`, "sh", strconv.Itoa(os.Getpid()), path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The new AppImage runtime sets these for itself.
	env := []string{}
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "APPDIR", "APPIMAGE", "ARGV0", "OWD":
		default:
			env = append(env, kv)
		}
	}
	cmd.Env = env
	return cmd.Start()
}

// closeInherited marks every descriptor above stderr close-on-exec. The AppImage
// runtime hands the app a keepalive pipe and a handle on its mount without that
// flag; passed on to the new copy, they keep the old runtime and its mount of the
// replaced file alive. Go's own descriptors already have the flag.
func closeInherited() {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
}
