//go:build linux

package fdbased

import (
	"os"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type discardDispatcher struct{}

func (discardDispatcher) DeliverNetworkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer) {}
func (discardDispatcher) DeliverLinkPacket(tcpip.NetworkProtocolNumber, *stack.PacketBuffer)    {}

// countEventFDs returns how many eventfds the process holds open.
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

func openSocketPair(t *testing.T) (*FD, int) {
	t.Helper()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	device, err := Open(strconv.Itoa(pair[0]), 1500, 0)
	if err != nil {
		t.Fatal(err)
	}
	return device.(*FD), pair[1]
}

func TestDispatcherEventFDsResolve(t *testing.T) {
	before := countEventFDs(t)
	device, peer := openSocketPair(t)
	defer unix.Close(peer)
	defer device.Close()

	eventFDs, err := dispatcherEventFDs(device.LinkEndpoint)
	if err != nil {
		t.Fatalf("the gVisor field path moved: %v", err)
	}
	if len(eventFDs) != 1 {
		t.Fatalf("got %d inbound dispatchers, want 1", len(eventFDs))
	}
	if _, err := unix.FcntlInt(uintptr(eventFDs[0]), unix.F_GETFD, 0); err != nil {
		t.Fatalf("EFD %d is not an open descriptor: %v", eventFDs[0], err)
	}
	if got := countEventFDs(t); got != before+1 {
		t.Fatalf("eventfds went from %d to %d, want one more", before, got)
	}
}

func TestCloseDoesNotLeakEventFDs(t *testing.T) {
	const rounds = 20
	cases := map[string]func(*FD){
		"attached": func(f *FD) {
			f.Attach(discardDispatcher{})
			f.Close()
		},
		"attached and detached first": func(f *FD) {
			f.Attach(discardDispatcher{})
			f.Attach(nil)
			f.Close()
		},
		"never attached": func(f *FD) {
			f.Close()
		},
		"closed twice": func(f *FD) {
			f.Attach(discardDispatcher{})
			f.Close()
			f.Close()
		},
	}
	for name, closeDevice := range cases {
		t.Run(name, func(t *testing.T) {
			before := countEventFDs(t)
			for range rounds {
				device, peer := openSocketPair(t)
				closeDevice(device)
				_ = unix.Close(peer)
			}
			if got := countEventFDs(t); got != before {
				t.Fatalf("eventfds went from %d to %d over %d rounds", before, got, rounds)
			}
		})
	}
}

type dispatcherWithoutStopFD struct{ fd int }

type endpointWithoutStopFD struct {
	stack.LinkEndpoint
	inboundDispatchers []any
}

func TestDispatcherEventFDsReturnsErrorWhenFieldsAreMissing(t *testing.T) {
	cases := map[string]stack.LinkEndpoint{
		"no StopFD":         &endpointWithoutStopFD{inboundDispatchers: []any{&dispatcherWithoutStopFD{}}},
		"dispatcher is int": &endpointWithoutStopFD{inboundDispatchers: []any{1}},
		"nil dispatcher":    &endpointWithoutStopFD{inboundDispatchers: []any{(*dispatcherWithoutStopFD)(nil)}},
		"no slice":          &discardEndpoint{},
		"not a pointer":     discardEndpoint{},
	}
	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := dispatcherEventFDs(endpoint); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

type discardEndpoint struct{ stack.LinkEndpoint }
