package core

import (
	"net/netip"
	"testing"

	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
)

// detachRecorder notes when the stack lets go of it, which is when its
// dispatchers stop reading the device.
type detachRecorder struct {
	*channel.Endpoint
	detached bool
}

func (d *detachRecorder) Attach(dispatcher stack.NetworkDispatcher) {
	if dispatcher == nil {
		d.detached = true
	}
	d.Endpoint.Attach(dispatcher)
}

type nopHandler struct{}

func (nopHandler) HandleTCP(adapter.TCPConn) {}
func (nopHandler) HandleUDP(adapter.UDPConn) {}

func TestCreateStackFailingAfterTheNICLetsGoOfTheDevice(t *testing.T) {
	dev := &detachRecorder{Endpoint: channel.New(16, 1500, "")}
	_, err := CreateStack(&Config{
		LinkEndpoint:     dev,
		TransportHandler: nopHandler{},
		MulticastGroups:  []netip.Addr{netip.MustParseAddr("192.0.2.1")},
	})
	if err == nil {
		t.Fatal("joining a unicast address as a multicast group succeeded")
	}
	if !dev.detached {
		t.Fatal("a failed CreateStack left the device attached to its NIC")
	}
}
