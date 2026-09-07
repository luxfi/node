// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build windows

package node

import (
	"syscall"
	"unsafe"
)

// diskFreeBytes returns available disk space in bytes for the filesystem
// containing the given path.
func diskFreeBytes(path string) (uint64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable uint64
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		0,
		0,
	)
	if ret == 0 {
		return 0, err
	}
	return freeBytesAvailable, nil
}
