package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
)

// synOnCloseDevice hands the stack one more SYN as it is closed, and closes
// once the stack has answered it: a connection an app opened while the engine
// was stopping.
type synOnCloseDevice struct {
	*channel.Endpoint
	closeOnce sync.Once
}

func (*synOnCloseDevice) Name() string { return "test" }
func (*synOnCloseDevice) Type() string { return "channel" }

func (d *synOnCloseDevice) Close() {
	d.closeOnce.Do(func() {
		d.InjectInbound(header.IPv4ProtocolNumber, synPacket())
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if synAck := d.ReadContext(ctx); synAck != nil {
			synAck.DecRef()
		}
		d.Endpoint.Close()
	})
}

func synPacket() *stack.PacketBuffer {
	b := make([]byte, header.IPv4MinimumSize+header.TCPMinimumSize)
	ip := header.IPv4(b)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(len(b)),
		TTL:         64,
		Protocol:    uint8(header.TCPProtocolNumber),
		SrcAddr:     tcpip.AddrFrom4([4]byte{10, 0, 0, 2}),
		DstAddr:     tcpip.AddrFrom4([4]byte{192, 0, 2, 1}),
	})
	ip.SetChecksum(^ip.CalculateChecksum())
	tcp := header.TCP(b[header.IPv4MinimumSize:])
	tcp.Encode(&header.TCPFields{
		SrcPort:    40000,
		DstPort:    443,
		SeqNum:     1,
		DataOffset: header.TCPMinimumSize,
		Flags:      header.TCPFlagSyn,
		WindowSize: 65535,
	})
	sum := header.PseudoHeaderChecksum(header.TCPProtocolNumber, ip.SourceAddress(), ip.DestinationAddress(), header.TCPMinimumSize)
	tcp.SetChecksum(^tcp.CalculateChecksum(sum))
	return stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(b)})
}

type holdingHandler struct{}

func (holdingHandler) HandleTCP(adapter.TCPConn) {}
func (holdingHandler) HandleUDP(adapter.UDPConn) {}

// Stop never waits out the handshake of a connection opened while it runs,
// about two minutes of SYN-ACK retransmits.
func TestStopDoesNotWaitOnAConnectionOpenedWhileStopping(t *testing.T) {
	dev := &synOnCloseDevice{Endpoint: channel.New(64, 1500, "")}
	s, err := core.CreateStack(&core.Config{LinkEndpoint: dev, TransportHandler: holdingHandler{}})
	if err != nil {
		t.Fatalf("create stack: %v", err)
	}
	_engineMu.Lock()
	_defaultStack, _defaultDevice = s, dev
	_engineMu.Unlock()

	stopped := make(chan struct{})
	go func() {
		Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop still waiting 10s after a SYN arrived during it")
	}
}
