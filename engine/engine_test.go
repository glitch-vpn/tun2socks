package engine

import (
	"testing"
)

func TestStartWithoutKeyReturnsError(t *testing.T) {
	Insert(nil)
	if err := Start(); err == nil {
		t.Fatal("Start with no key returned nil")
	}
}

func TestStartAfterStopReturnsError(t *testing.T) {
	Insert(&Key{Device: "fd://0", Proxy: "direct://"})
	Stop()
	if err := Start(); err == nil {
		t.Fatal("Start after Stop returned nil")
	}
}

func TestStartBadProxyClearsKey(t *testing.T) {
	Insert(&Key{Device: "fd://0", Proxy: "nosuchscheme://x", LogLevel: "silent"})
	if err := Start(); err == nil {
		t.Fatal("Start with an unknown proxy scheme returned nil")
	}
	_engineMu.Lock()
	defer _engineMu.Unlock()
	if _defaultKey != nil {
		t.Fatal("failed start left the key set")
	}
}
