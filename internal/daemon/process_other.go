//go:build !unix && !windows

package daemon

// No process check here, so a pid file never proves a running daemon.
func processAlive(int) bool {
	return false
}
