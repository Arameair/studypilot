//go:build !windows

package backend

func validDiscoveredExecutable(path string) bool {
	return path != ""
}
