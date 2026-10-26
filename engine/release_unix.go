//go:build unix

package engine

import (
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
)

// releaseDeviceFD closes the descriptor an fd:// device names. It runs only
// for a start that failed before a device took the descriptor over.
func releaseDeviceFD(device string) {
	u, err := url.Parse(device)
	if err != nil || !strings.EqualFold(u.Scheme, fdbased.Driver) {
		return
	}
	if fd, err := strconv.Atoi(u.Host); err == nil && fd >= 0 {
		_ = unix.Close(fd)
	}
}
