// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"plugin"

	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/utils/filesystem"
	"github.com/luxfi/node/utils/resource"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
)

var (
	ErrNotPlugin = errors.New("not a plugin")
	ErrNotVM     = errors.New("does not implement VM interface")
)

// VMRegistry provides functionality to load VMs from plugins
type VMRegistry interface {
	// Reload causes all VM plugins in the plugin directory to be loaded
	// Returns:
	// 1) slice of newly loaded VM IDs
	// 2) map of vmID -> error for VMs that failed to load
	// 3) general error if reload failed
	Reload(ctx context.Context) ([]ids.ID, map[ids.ID]error, error)
}

// VMRegistryConfig configures the VMRegistry
type VMRegistryConfig struct {
	VMGetter  VMGetter
	VMManager vms.Manager
}

// VMGetter provides a method to get VMs from plugins
type VMGetter interface {
	// Get returns the VM factory for the given plugin path
	Get(pluginPath string) (vms.Factory, error)
}

// VMGetterConfig configures the VMGetter
type VMGetterConfig struct {
	FileReader      filesystem.Reader
	Manager         vms.Manager
	PluginDirectory string
	CPUTracker      resource.Manager
	RuntimeTracker  runtime.Tracker
	MetricsGatherer metric.MultiGatherer
}

// vmRegistry implements VMRegistry
type vmRegistry struct {
	config VMRegistryConfig
}

// NewVMRegistry returns a new VMRegistry
func NewVMRegistry(config VMRegistryConfig) VMRegistry {
	return &vmRegistry{
		config: config,
	}
}

// vmGetter implements VMGetter
type vmGetter struct {
	config VMGetterConfig
}

// NewVMGetter returns a new VMGetter
func NewVMGetter(config VMGetterConfig) VMGetter {
	return &vmGetter{
		config: config,
	}
}

func (r *vmRegistry) Reload(ctx context.Context) ([]ids.ID, map[ids.ID]error, error) {
	var newVMs []ids.ID
	failedVMs := make(map[ids.ID]error)

	getter, ok := r.config.VMGetter.(*vmGetter)
	if !ok {
		return newVMs, failedVMs, nil
	}

	pluginDir := getter.config.PluginDirectory
	if pluginDir == "" {
		return newVMs, failedVMs, nil
	}

	files, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return newVMs, failedVMs, nil
		}
		return nil, nil, fmt.Errorf("failed to read plugin directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		pluginPath := filepath.Join(pluginDir, file.Name())
		vmID, err := ids.FromString(file.Name())
		if err != nil {
			// Skip files that aren't valid VM IDs
			continue
		}

		// Check if already registered
		if _, err := r.config.VMManager.GetFactory(vmID); err == nil {
			// Already registered
			continue
		}

		factory, err := r.config.VMGetter.Get(pluginPath)
		if err != nil {
			failedVMs[vmID] = err
			continue
		}

		if err := r.config.VMManager.RegisterFactory(ctx, vmID, factory); err != nil {
			failedVMs[vmID] = err
			continue
		}

		newVMs = append(newVMs, vmID)
	}

	return newVMs, failedVMs, nil
}

func (g *vmGetter) Get(pluginPath string) (vms.Factory, error) {
	// Try to load as a Go plugin first
	p, err := plugin.Open(pluginPath)
	if err != nil {
		// Not a Go plugin, might be a standalone binary
		return nil, fmt.Errorf("%w: %v", ErrNotPlugin, err)
	}

	sym, err := p.Lookup("Factory")
	if err != nil {
		return nil, fmt.Errorf("plugin does not export Factory: %w", err)
	}

	factory, ok := sym.(vms.Factory)
	if !ok {
		return nil, ErrNotVM
	}

	return factory, nil
}
