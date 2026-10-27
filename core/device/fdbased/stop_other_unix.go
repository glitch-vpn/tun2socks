//go:build unix && !linux

package fdbased

import "gvisor.dev/gvisor/pkg/tcpip/stack"

// StopInbound does nothing: the iobased endpoint has no eventfd to close.
func StopInbound(stack.LinkEndpoint) {}
