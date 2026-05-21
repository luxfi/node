// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/utils/filesystem"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/rpcchainvm"
	"github.com/luxfi/node/vms/rpcchainvm/runtime"
	"github.com/luxfi/resource"
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
	ProcessTracker  resource.ProcessTracker
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

// Reload scans the plugin directory and registers any VM that isn't yet
// known to the manager. Files whose name doesn't parse as an ids.ID are
// skipped silently; a VM that is already registered is skipped without
// error. Per-VM load failures are collected into failedVMs so the caller
// can report them. Only fundamental issues (unreadable plugin dir) bubble
// up as the third return value.
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
		return nil, nil, fmt.Errorf("read plugin directory %s: %w", pluginDir, err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		vmID, err := ids.FromString(file.Name())
		if err != nil {
			continue
		}
		if _, getErr := r.config.VMManager.GetFactory(ctx, vmID); getErr == nil {
			continue
		}
		factory, err := r.config.VMGetter.Get(filepath.Join(pluginDir, file.Name()))
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

// Get returns an rpcchainvm-backed factory for the plugin at pluginPath.
// The plugin binary is launched lazily on the first factory.New call.
func (g *vmGetter) Get(pluginPath string) (vms.Factory, error) {
	return rpcchainvm.NewFactory(
		pluginPath,
		g.config.ProcessTracker,
		g.config.RuntimeTracker,
		g.config.MetricsGatherer,
	), nil
}
