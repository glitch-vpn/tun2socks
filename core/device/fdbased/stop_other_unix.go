//go:build unix && !linux

package fdbased

import "gvisor.dev/gvisor/pkg/tcpip/stack"

// stopInbound does nothing: the iobased endpoint has no eventfd to close.
func stopInbound(stack.LinkEndpoint) {}
