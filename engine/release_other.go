//go:build !unix

package engine

// releaseDeviceFD has nothing to close: a device on this platform is named,
// not a file descriptor.
func releaseDeviceFD(string) {}
