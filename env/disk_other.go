//go:build !windows

package env

import "fmt"

// DiskInfo contains disk usage information for a filesystem path.
type DiskInfo struct {
	FreeBytesAvailable uint64
	TotalBytes         uint64
	TotalFreeBytes     uint64
}

// GetDiskInfo is not yet implemented on non-Windows platforms in this env package.
func GetDiskInfo(path string) (*DiskInfo, error) {
	_ = path
	return nil, fmt.Errorf("GetDiskInfo is not implemented on this platform")
}
