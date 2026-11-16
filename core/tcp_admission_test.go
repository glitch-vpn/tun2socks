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

// answerTo returns the TCP flags of the stack's reply to a SYN, and false when
// it sent none.
func answerTo(t *testing.T, admit TCPAdmission) (header.TCPFlags, bool) {
	t.Helper()
	dev := channel.New(16, 1500, "")
	s, err := CreateStack(&Config{LinkEndpoint: dev, TransportHandler: nopHandler{}, TCPAdmission: admit})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStack(s)

	injectSYN(dev)
	return readAnswer(dev, time.Second)
}

func readAnswer(dev *channel.Endpoint, wait time.Duration) (header.TCPFlags, bool) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if pkt := dev.Read(); pkt != nil {
			defer pkt.DecRef()
			view := pkt.ToView().AsSlice()
			return header.TCP(view[header.IPv6MinimumSize:]).Flags(), true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, false
}

func TestTCPAdmissionRefusedSYNIsResetBeforeAnyHandshake(t *testing.T) {
	var asked netip.AddrPort
	flags, answered := answerTo(t, func(destination netip.AddrPort) TCPVerdict {
		asked = destination
		return TCPReset
	})
	if !answered || flags&header.TCPFlagRst == 0 || flags&header.TCPFlagSyn != 0 {
		t.Fatalf("answer flags = %v (answered %v), want an RST and no SYN-ACK", flags, answered)
	}
	if want := netip.MustParseAddrPort("[2607:f8b0::1]:443"); asked != want {
		t.Fatalf("asked about %v, want %v", asked, want)
	}
}

func TestTCPAdmissionDroppedSYNGetsNoAnswerAndIsJudgedAgainOnRetransmit(t *testing.T) {
	var asked int
	verdicts := []TCPVerdict{TCPDrop, TCPAdmit}
	dev := channel.New(16, 1500, "")
	s, err := CreateStack(&Config{LinkEndpoint: dev, TransportHandler: nopHandler{}, TCPAdmission: func(netip.AddrPort) TCPVerdict {
		v := verdicts[asked]
		asked++
		return v
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStack(s)

	injectSYN(dev)
	if flags, answered := readAnswer(dev, 300*time.Millisecond); answered {
		t.Fatalf("a dropped SYN was answered with flags %v", flags)
	}
	injectSYN(dev)
	flags, answered := readAnswer(dev, time.Second)
	if !answered || flags&header.TCPFlagSyn == 0 || flags&header.TCPFlagAck == 0 {
		t.Fatalf("the retransmitted SYN: flags %v (answered %v), want SYN+ACK", flags, answered)
	}
}

func TestTCPAdmissionAdmittedSYNGetsASYNACK(t *testing.T) {
	flags, answered := answerTo(t, func(netip.AddrPort) TCPVerdict { return TCPAdmit })
	if !answered || flags&header.TCPFlagSyn == 0 || flags&header.TCPFlagAck == 0 {
		t.Fatalf("answer flags = %v, want SYN+ACK", flags)
	}
}

func TestNoTCPAdmissionAnswersEverySYN(t *testing.T) {
	flags, answered := answerTo(t, nil)
	if !answered || flags&header.TCPFlagSyn == 0 {
		t.Fatalf("answer flags = %v, want SYN+ACK", flags)
	}
}
