//go:build !unix && !windows

package daemon

// processAlive cannot check processes on this platform, so a pid file is
// never taken as proof of a running daemon.
func processAlive(int) bool {
	return false
}
