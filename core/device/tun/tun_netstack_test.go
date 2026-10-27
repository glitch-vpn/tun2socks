//go:build linux

package tun

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func countEventFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err == nil && target == "anon_inode:[eventfd]" {
			count++
		}
	}
	return count
}

// TestCloseDoesNotLeakEventFDs builds the endpoint Open builds, over a
// socketpair in place of a tun device.
func TestCloseDoesNotLeakEventFDs(t *testing.T) {
	const rounds = 20
	before := countEventFDs(t)
	for range rounds {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		device := &TUN{fd: pair[0], mtu: 1500, name: "test"}
		if err := device.attachEndpoint(); err != nil {
			t.Fatal(err)
		}
		device.Close()
		device.Close()
		_ = unix.Close(pair[1])
	}
	if got := countEventFDs(t); got != before {
		t.Fatalf("eventfds went from %d to %d over %d rounds", before, got, rounds)
	}
}
