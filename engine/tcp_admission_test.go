package engine

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
)

type admissionTestDevice struct{ *channel.Endpoint }

func (admissionTestDevice) Name() string { return "test" }
func (admissionTestDevice) Type() string { return "test" }

func useChannelDevice(t *testing.T) *channel.Endpoint {
	t.Helper()
	dev := channel.New(16, 1500, "")
	previous := openDevice
	openDevice = func(string, uint32) (device.Device, error) { return admissionTestDevice{dev}, nil }
	t.Cleanup(func() {
		openDevice = previous
		Stop()
	})
	return dev
}

func sendSYN(dev *channel.Endpoint) {
	client := netip.MustParseAddr("fdfe:dcba:9876::2").As16()
	server := netip.MustParseAddr("2607:f8b0::1").As16()
	packet := make([]byte, header.IPv6MinimumSize+header.TCPMinimumSize)
	ip := header.IPv6(packet)
	ip.Encode(&header.IPv6Fields{
		PayloadLength:     header.TCPMinimumSize,
		TransportProtocol: header.TCPProtocolNumber,
		HopLimit:          64,
		SrcAddr:           tcpip.AddrFromSlice(client[:]),
		DstAddr:           tcpip.AddrFromSlice(server[:]),
	})
	segment := header.TCP(ip.Payload())
	segment.Encode(&header.TCPFields{SrcPort: 40000, DstPort: 443, SeqNum: 1000, DataOffset: header.TCPMinimumSize, Flags: header.TCPFlagSyn, WindowSize: 65535})
	segment.SetChecksum(^segment.CalculateChecksum(header.PseudoHeaderChecksum(header.TCPProtocolNumber, ip.SourceAddress(), ip.DestinationAddress(), header.TCPMinimumSize)))
	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(packet)})
	defer pkt.DecRef()
	dev.InjectInbound(ipv6.ProtocolNumber, pkt)
}

func tcpFlagsOfAnswer(dev *channel.Endpoint) (header.TCPFlags, bool) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pkt := dev.Read(); pkt != nil {
			defer pkt.DecRef()
			return header.TCP(pkt.ToView().AsSlice()[header.IPv6MinimumSize:]).Flags(), true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, false
}

func resetEverything(netip.AddrPort) core.TCPVerdict { return core.TCPReset }

func TestStartedEngineAsksItsTCPAdmission(t *testing.T) {
	dev := useChannelDevice(t)
	SetTCPAdmission(resetEverything)
	Insert(&Key{Device: "test://dev", Proxy: "direct://", LogLevel: "silent", MTU: 1500})
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	sendSYN(dev)
	flags, answered := tcpFlagsOfAnswer(dev)
	if !answered || flags&header.TCPFlagRst == 0 {
		t.Fatalf("answer flags = %v (answered %v), want an RST from the engine's stack", flags, answered)
	}
}

func TestStopClearsTCPAdmission(t *testing.T) {
	useChannelDevice(t)
	SetTCPAdmission(resetEverything)
	Insert(&Key{Device: "test://dev", Proxy: "direct://", LogLevel: "silent", MTU: 1500})
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	Stop()
	_engineMu.Lock()
	defer _engineMu.Unlock()
	if _tcpAdmission != nil {
		t.Fatal("Stop left the TCP admission set")
	}
}

func TestFailedStartClearsTCPAdmission(t *testing.T) {
	SetTCPAdmission(resetEverything)
	previous := openDevice
	openDevice = func(string, uint32) (device.Device, error) { return nil, errors.New("no device") }
	t.Cleanup(func() { openDevice = previous })
	Insert(&Key{Device: "test://dev", Proxy: "direct://", LogLevel: "silent", MTU: 1500})
	if err := Start(); err == nil {
		t.Fatal("Start with an unopenable device returned nil")
	}
	_engineMu.Lock()
	defer _engineMu.Unlock()
	if _tcpAdmission != nil {
		t.Fatal("a failed start left the TCP admission set")
	}
}
