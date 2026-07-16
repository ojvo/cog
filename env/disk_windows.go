//go:build windows

package env

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DiskInfo contains disk usage information for a filesystem path.
type DiskInfo struct {
	FreeBytesAvailable uint64
	TotalBytes         uint64
	TotalFreeBytes     uint64
}

// GetDiskInfo returns disk usage information for the specified path.
func GetDiskInfo(path string) (*DiskInfo, error) {
	h, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return nil, fmt.Errorf("failed to load kernel32.dll: %w", err)
	}
	c, err := h.FindProc("GetDiskFreeSpaceExW")
	if err != nil {
		return nil, fmt.Errorf("failed to find GetDiskFreeSpaceExW: %w", err)
	}
	freeAvail := uint64(0)
	total := uint64(0)
	totalFree := uint64(0)
	if len(path) == 2 && path[1] == ':' {
		path += "\\"
	}
	ret, _, _ := c.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDiskFreeSpaceExW failed")
	}
	return &DiskInfo{FreeBytesAvailable: freeAvail, TotalBytes: total, TotalFreeBytes: totalFree}, nil
}
