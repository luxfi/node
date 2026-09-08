// Copyright (C) 2022-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !windows && !openbsd

package storage

import "syscall"

func AvailableBytes(storagePath string) (uint64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(storagePath, &stat)
	if err != nil {
		return 0, err
	}
	avail := stat.Bavail * uint64(stat.Bsize)
	return avail, nil
}
