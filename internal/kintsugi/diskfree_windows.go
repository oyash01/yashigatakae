//go:build windows

package kintsugi

import (
	"syscall"
	"unsafe"
)

// diskFree returns (free, total) bytes for the filesystem holding `path`,
// using the Win32 GetDiskFreeSpaceExW API.
func diskFree(path string) (uint64, uint64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	r1, _, e1 := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, 0, e1
	}
	return freeAvail, totalBytes, nil
}
