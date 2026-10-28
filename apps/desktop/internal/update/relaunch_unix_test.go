//go:build linux

package update

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// The AppImage runtime's keepalive descriptors must not reach the relaunched copy.
func TestCloseInherited(t *testing.T) {
	fd, err := syscall.Open("/dev/null", syscall.O_RDONLY, 0) // no O_CLOEXEC, like the runtime's
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	seen := func() bool {
		out, err := exec.Command("/bin/sh", "-c", "ls /proc/$$/fd").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Contains(" "+strings.Join(strings.Fields(string(out)), " ")+" ", " "+strconv.Itoa(fd)+" ")
	}
	if !seen() {
		t.Fatal("test setup: the child should inherit the descriptor")
	}
	closeInherited()
	if seen() {
		t.Fatal("the descriptor still reaches child processes")
	}
}
