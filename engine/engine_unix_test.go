//go:build unix

package engine

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStartFailureBeforeDeviceClosesItsFD(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	dupFd, err := syscall.Dup(int(r.Fd()))
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	defer unix.Close(dupFd)

	Insert(&Key{Device: fmt.Sprintf("fd://%d", dupFd), Proxy: "direct://", LogLevel: "no-such-level"})
	if err := Start(); err == nil {
		t.Fatal("Start with an invalid log level returned nil")
	}

	if _, err := unix.FcntlInt(uintptr(dupFd), unix.F_GETFD, 0); err == nil {
		t.Fatalf("fd %d is still open after a start that failed before its device", dupFd)
	}
}
