package log

import (
	"time"

	glog "gvisor.dev/gvisor/pkg/log"
)

var _globalE = &emitter{}

func init() {
	glog.SetTarget(_globalE)
}

type emitter struct {
	logger *SugaredLogger
}

func (e *emitter) setLogger(logger *SugaredLogger) {
	e.logger = logger.WithOptions(pkgCallerSkip)
	glog.SetLevel(1 - glog.Level(e.logger.Level()))
}

// The stack logs from its own goroutines, which can outlive the engine that
// started them and run into the next Start's SetLogger.
func (e *emitter) logf(level glog.Level, format string, args ...any) {
	_globalMu.RLock()
	logger := e.logger
	_globalMu.RUnlock()
	logger.Logf(1-Level(level), "[STACK] "+format, args...)
}

func (e *emitter) Emit(_ int, level glog.Level, _ time.Time, format string, args ...any) {
	e.logf(level, format, args...)
}
