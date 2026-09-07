//go:build !metrics

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package resource

import (
	"math"
	"time"

	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/utils/storage"
)

type CPUUser interface {
	CPUUsage() float64
}

type DiskUser interface {
	DiskUsage() (read float64, write float64)
	AvailableDiskBytes() uint64
}

type User interface {
	CPUUser
	DiskUser
}

type ProcessTracker interface {
	TrackProcess(pid int)
	UntrackProcess(pid int)
}

type Manager interface {
	User
	ProcessTracker
	Shutdown()
}

// noopManager is a minimal resource manager that doesn't track CPU/process usage.
// Build with -tags=metrics for full resource monitoring.
type noopManager struct {
	diskPath           string
	availableDiskBytes uint64
}

func NewManager(
	_ log.Logger,
	diskPath string,
	_, _, _ time.Duration,
	_ metric.Registerer,
) (Manager, error) {
	m := &noopManager{
		diskPath:           diskPath,
		availableDiskBytes: math.MaxUint64,
	}
	// Get initial disk space
	if bytes, err := storage.AvailableBytes(diskPath); err == nil {
		m.availableDiskBytes = bytes
	}
	return m, nil
}

func (m *noopManager) CPUUsage() float64                      { return 0 }
func (m *noopManager) DiskUsage() (float64, float64)          { return 0, 0 }
func (m *noopManager) AvailableDiskBytes() uint64             { return m.availableDiskBytes }
func (m *noopManager) TrackProcess(_ int)                     {}
func (m *noopManager) UntrackProcess(_ int)                   {}
func (m *noopManager) Shutdown()                              {}
