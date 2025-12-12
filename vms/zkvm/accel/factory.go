// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Factory for ZK accelerator backends
// Build tags control which implementations are available:
//   - Default: Pure Go (always available)
//   - darwin,arm64: MLX (Apple Silicon Metal)
//   - cgo,linux: CGO/C++ with OpenMP
//   - fpga: FPGA acceleration (AMD Versal, AWS F2, Intel Stratix)

package accel

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

var (
	ErrUnsupportedBackend = errors.New("unsupported acceleration backend")
	ErrBackendNotCompiled = errors.New("backend not compiled into binary")
)

// NewAccelerator creates the appropriate ZK accelerator based on configuration
// Priority (if Backend is Auto):
//   1. FPGA (if compiled with -tags fpga and hardware available)
//   2. MLX (if on Apple Silicon)
//   3. CGO (if compiled with CGO_ENABLED=1 on Linux)
//   4. Go (always available)
func NewAccelerator(config Config) (Accelerator, error) {
	if config.Backend == BackendAuto {
		config.Backend = detectBestBackend()
	}

	// Allow environment override
	if envBackend := os.Getenv("LUX_ZK_BACKEND"); envBackend != "" {
		switch strings.ToLower(envBackend) {
		case "go":
			config.Backend = BackendGo
		case "mlx":
			config.Backend = BackendMLX
		case "cgo":
			config.Backend = BackendCGO
		case "fpga":
			config.Backend = BackendFPGA
		case "cuda":
			config.Backend = BackendCUDA
		}
	}

	switch config.Backend {
	case BackendGo:
		return NewGoAccelerator(config)

	case BackendMLX:
		return newMLXAccelerator(config)

	case BackendCGO:
		return newCGOAccelerator(config)

	case BackendFPGA:
		return newFPGAAccelerator(config)

	case BackendCUDA:
		// CUDA backend - future implementation
		return nil, fmt.Errorf("%w: CUDA (coming soon)", ErrBackendNotCompiled)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedBackend, config.Backend)
	}
}

// detectBestBackend automatically selects the best available backend
func detectBestBackend() Backend {
	// Check for FPGA first (highest priority if available)
	if isFPGAAvailable() {
		return BackendFPGA
	}

	// Check for Apple Silicon (MLX)
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if isMLXAvailable() {
			return BackendMLX
		}
	}

	// Check for CGO on Linux
	if runtime.GOOS == "linux" && isCGOAvailable() {
		return BackendCGO
	}

	// Default to pure Go
	return BackendGo
}

// GetAvailableBackends returns all backends compiled into the binary
func GetAvailableBackends() []Backend {
	backends := []Backend{BackendGo} // Go is always available

	if isMLXAvailable() {
		backends = append(backends, BackendMLX)
	}

	if isCGOAvailable() {
		backends = append(backends, BackendCGO)
	}

	if isFPGAAvailable() {
		backends = append(backends, BackendFPGA)
	}

	return backends
}

// BackendInfo returns information about a backend
func BackendInfo(backend Backend) string {
	switch backend {
	case BackendGo:
		return "Pure Go implementation - portable, no dependencies"
	case BackendMLX:
		return "Apple Silicon Metal via MLX - GPU accelerated NTT/MSM"
	case BackendCGO:
		return "C++ with OpenMP - parallel CPU acceleration on Linux"
	case BackendCUDA:
		return "NVIDIA CUDA - GPU accelerated (coming soon)"
	case BackendFPGA:
		return "FPGA acceleration - AMD Versal, AWS F2, Intel Stratix"
	default:
		return "Unknown backend"
	}
}
