package core

import (
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/log"
)

// strayEndpointAbortInterval paces the aborts of endpoints registered after
// the first abort pass.
const strayEndpointAbortInterval = 50 * time.Millisecond

// CloseStack tears s down and waits for it, closing the link endpoints of
// its NICs on the way.
//
// The NICs go first: that drops new packets and the routes a handshake
// needs, and stops an fd dispatcher before its fd closes. A forwarder that
// found its route before then still registers its endpoint after the abort
// pass, and Wait would sit out that handshake's retransmits, about two
// minutes; such endpoints are aborted until Wait returns.
func CloseStack(s *stack.Stack) {
	for id := range s.NICInfo() {
		if err := s.RemoveNIC(id); err != nil {
			log.Warnf("[STACK] remove NIC %d: %s", id, err)
		}
	}
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	ticker := time.NewTicker(strayEndpointAbortInterval)
	defer ticker.Stop()
	for {
		select {
		case <-waited:
			return
		case <-ticker.C:
			for _, e := range s.RegisteredEndpoints() {
				e.Abort()
			}
		}
	}
}
