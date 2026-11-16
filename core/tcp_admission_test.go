package core

import (
	"net/netip"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

var (
	clientV6 = tcpip.AddrFromSlice(netip.MustParseAddr("fdfe:dcba:9876::2").AsSlice())
	serverV6 = tcpip.AddrFromSlice(netip.MustParseAddr("2607:f8b0::1").AsSlice())
)

func injectSYN(dev *channel.Endpoint) {
	const tcpLen = header.TCPMinimumSize
	hdr := make([]byte, header.IPv6MinimumSize+tcpLen)
	ip := header.IPv6(hdr)
	ip.Encode(&header.IPv6Fields{
		PayloadLength:     tcpLen,
		TransportProtocol: header.TCPProtocolNumber,
		HopLimit:          64,
		SrcAddr:           clientV6,
		DstAddr:           serverV6,
	})
	tcpHdr := header.TCP(hdr[header.IPv6MinimumSize:])
	tcpHdr.Encode(&header.TCPFields{
		SrcPort:    40000,
		DstPort:    443,
		SeqNum:     1000,
		DataOffset: tcpLen,
		Flags:      header.TCPFlagSyn,
		WindowSize: 65535,
	})
	tcpHdr.SetChecksum(^tcpHdr.CalculateChecksum(header.PseudoHeaderChecksum(
		header.TCPProtocolNumber, clientV6, serverV6, tcpLen)))
	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(hdr)})
	defer pkt.DecRef()
	dev.InjectInbound(ipv6.ProtocolNumber, pkt)
}

// answerTo returns the TCP flags of the stack's reply to a SYN.
func answerTo(t *testing.T, admit TCPAdmission) header.TCPFlags {
	t.Helper()
	dev := channel.New(16, 1500, "")
	s, err := CreateStack(&Config{LinkEndpoint: dev, TransportHandler: nopHandler{}, TCPAdmission: admit})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStack(s)

	injectSYN(dev)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pkt := dev.Read(); pkt != nil {
			defer pkt.DecRef()
			view := pkt.ToView().AsSlice()
			return header.TCP(view[header.IPv6MinimumSize:]).Flags()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the stack never answered the SYN")
	return 0
}

func TestTCPAdmissionRefusedSYNIsResetBeforeAnyHandshake(t *testing.T) {
	var asked netip.AddrPort
	flags := answerTo(t, func(destination netip.AddrPort) bool {
		asked = destination
		return false
	})
	if flags&header.TCPFlagRst == 0 || flags&header.TCPFlagSyn != 0 {
		t.Fatalf("answer flags = %v, want an RST and no SYN-ACK", flags)
	}
	if want := netip.MustParseAddrPort("[2607:f8b0::1]:443"); asked != want {
		t.Fatalf("asked about %v, want %v", asked, want)
	}
}

func TestTCPAdmissionAdmittedSYNGetsASYNACK(t *testing.T) {
	flags := answerTo(t, func(netip.AddrPort) bool { return true })
	if flags&header.TCPFlagSyn == 0 || flags&header.TCPFlagAck == 0 {
		t.Fatalf("answer flags = %v, want SYN+ACK", flags)
	}
}

func TestNoTCPAdmissionAnswersEverySYN(t *testing.T) {
	flags := answerTo(t, nil)
	if flags&header.TCPFlagSyn == 0 {
		t.Fatalf("answer flags = %v, want SYN+ACK", flags)
	}
}
