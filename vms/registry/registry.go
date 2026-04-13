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

func (r *vmRegistry) Reload(ctx context.Context) ([]ids.ID, map[ids.ID]error, error) {
	var newVMs []ids.ID
	failedVMs := make(map[ids.ID]error)

	getter, ok := r.config.VMGetter.(*vmGetter)
	if !ok {
		fmt.Printf("[Registry] VMGetter type assertion failed\n")
		return newVMs, failedVMs, nil
	}

	pluginDir := getter.config.PluginDirectory
	if pluginDir == "" {
		fmt.Printf("[Registry] No plugin directory configured\n")
		return newVMs, failedVMs, nil
	}
	fmt.Printf("[Registry] Loading plugins from: %s\n", pluginDir)

	files, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("[Registry] Plugin directory does not exist: %s\n", pluginDir)
			return newVMs, failedVMs, nil
		}
		return nil, nil, fmt.Errorf("failed to read plugin directory: %w", err)
	}

	fmt.Printf("[Registry] Found %d files in plugin directory\n", len(files))
	for _, file := range files {
		if file.IsDir() {
			fmt.Printf("[Registry] Skipping directory: %s\n", file.Name())
			continue
		}

		pluginPath := filepath.Join(pluginDir, file.Name())
		fmt.Printf("[Registry] Checking file: %s\n", file.Name())
		vmID, err := ids.FromString(file.Name())
		if err != nil {
			// Skip files that aren't valid VM IDs
			fmt.Printf("[Registry] Invalid VM ID format: %s (err: %v)\n", file.Name(), err)
			continue
		}
		fmt.Printf("[Registry] Parsed VM ID: %s\n", vmID)

		// Check if already registered
		fmt.Printf("[Registry] Checking if VM %s is already registered...\n", vmID)
		existingFactory, getErr := r.config.VMManager.GetFactory(ctx, vmID)
		fmt.Printf("[Registry] GetFactory returned: factory=%v, err=%v\n", existingFactory, getErr)
		if getErr == nil {
			// Already registered
			fmt.Printf("[Registry] VM already registered: %s\n", vmID)
			continue
		}
		fmt.Printf("[Registry] VM not registered, will load from plugin\n")

		fmt.Printf("[Registry] Loading factory from plugin: %s\n", pluginPath)
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
	fmt.Printf("[VMGetter] Creating rpcchainvm factory for: %s\n", pluginPath)
	// VM binaries are executable files that run as gRPC plugins via rpcchainvm
	// This is the standard approach for EVM and other VM binaries
	factory := rpcchainvm.NewFactory(
		pluginPath,
		g.config.ProcessTracker,
		g.config.RuntimeTracker,
		g.config.MetricsGatherer,
	)
	fmt.Printf("[VMGetter] Factory created successfully\n")
	return factory, nil
}
