//go:build linux

package fdbased

import (
	"errors"
	"fmt"
	"reflect"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/log"
)

// StopInbound stops the endpoint's inbound dispatchers and closes the
// eventfd each one signals its stop through.
//
// gVisor (v0.0.0-20260701204157-69c2d17aea96) creates that eventfd, a
// stopfd.StopFD embedded in every fdbased dispatcher, and never closes it:
// Attach(nil) writes to it and the dispatch goroutine returns, but the
// descriptor stays open, one per stack start. The dispatchers are unexported,
// so the descriptors are read through reflect, read-only. The test next to
// this file fails when a gVisor update moves them.
//
// The descriptors close only after Attach(nil) and Wait have returned, when no
// goroutine polls or writes them; a dispatcher that was never attached has no
// goroutine either. A device calls it once, from its Close under a sync.Once;
// a second call would close the descriptors twice. The device is dead after
// that: attaching it again would restart dispatchers on closed descriptors.
//
// Only the dispatchers in inboundDispatchers are reached. In RecvMMsg or
// PacketMMap mode on a socket fd gVisor builds a readv dispatcher first and
// replaces it, leaving that one's eventfd unreachable; the devices here use
// Readv on a tun fd, where the first dispatcher is the one kept.
func StopInbound(ep stack.LinkEndpoint) {
	ep.Attach(nil)
	ep.Wait()

	eventFDs, err := dispatcherEventFDs(ep)
	if err != nil {
		log.Warnf("[FD] eventfd of the inbound dispatchers is left open: %s", err)
		return
	}
	for _, eventFD := range eventFDs {
		_ = unix.Close(eventFD)
	}
}

// dispatcherEventFDs returns the stop eventfd of every inbound dispatcher of
// an fdbased endpoint.
func dispatcherEventFDs(ep stack.LinkEndpoint) ([]int, error) {
	endpoint := reflect.ValueOf(ep)
	if endpoint.Kind() != reflect.Pointer || endpoint.IsNil() || endpoint.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("endpoint is %T, not a pointer to a struct", ep)
	}
	dispatchers := endpoint.Elem().FieldByName("inboundDispatchers")
	if !dispatchers.IsValid() || dispatchers.Kind() != reflect.Slice {
		return nil, errors.New("endpoint has no inboundDispatchers slice")
	}
	eventFDs := make([]int, 0, dispatchers.Len())
	for i := 0; i < dispatchers.Len(); i++ {
		dispatcher := dispatchers.Index(i)
		if dispatcher.Kind() == reflect.Interface {
			dispatcher = dispatcher.Elem()
		}
		if dispatcher.Kind() != reflect.Pointer || dispatcher.IsNil() || dispatcher.Elem().Kind() != reflect.Struct {
			return nil, fmt.Errorf("inbound dispatcher %d is not a pointer to a struct", i)
		}
		stopFD := dispatcher.Elem().FieldByName("StopFD")
		if !stopFD.IsValid() || stopFD.Kind() != reflect.Struct {
			return nil, fmt.Errorf("inbound dispatcher %d (%s) has no StopFD", i, dispatcher.Type())
		}
		eventFD := stopFD.FieldByName("EFD")
		if !eventFD.IsValid() || eventFD.Kind() != reflect.Int {
			return nil, fmt.Errorf("inbound dispatcher %d (%s) has no StopFD.EFD", i, dispatcher.Type())
		}
		eventFDs = append(eventFDs, int(eventFD.Int()))
	}
	return eventFDs, nil
}
