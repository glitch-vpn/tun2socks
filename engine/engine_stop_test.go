package engine

import (
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

type channelDevice struct{ *channel.Endpoint }

func (channelDevice) Name() string { return "test" }
func (channelDevice) Type() string { return "channel" }

func synPacket(srcPort uint16) *stack.PacketBuffer {
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
		SrcPort:    srcPort,
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

// Apps keep opening connections while the engine stops. A handshake that
// registers after the abort pass must not hold Stop for its SYN-ACK
// retransmits, about two minutes.
func TestStopWhileAppsKeepConnecting(t *testing.T) {
	const rounds, connectors = 10, 32
	for round := range rounds {
		dev := channelDevice{channel.New(1024, 1500, "")}
		s, err := core.CreateStack(&core.Config{LinkEndpoint: dev, TransportHandler: holdingHandler{}})
		if err != nil {
			t.Fatalf("create stack: %v", err)
		}
		_engineMu.Lock()
		_defaultStack, _defaultDevice = s, dev
		_engineMu.Unlock()

		quit := make(chan struct{})
		var apps sync.WaitGroup
		for c := range connectors {
			apps.Add(1)
			go func() {
				defer apps.Done()
				for port := uint16(1024 + c*1000); ; port++ {
					select {
					case <-quit:
						return
					default:
					}
					dev.InjectInbound(header.IPv4ProtocolNumber, synPacket(port))
					if synAck := dev.Read(); synAck != nil {
						synAck.DecRef()
					}
				}
			}()
		}
		time.Sleep(20 * time.Millisecond)

		stopped := make(chan struct{})
		go func() {
			Stop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatalf("round %d: Stop still waiting 10s with apps connecting", round)
		}
		close(quit)
		apps.Wait()
	}
}
