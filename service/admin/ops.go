// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// What an operator may do to this node.
//
// A handler here IS the operation: registering it yields the REST route, the
// OpenAPI document, the MCP tool, the CLI command, the generated SDK and the
// by-name call plane, from this one registration and the doc comment above it.
//
// # The verb IS the authorization
//
// The node's rule reads the method and nothing else (server/http/authorize.go):
// GET and HEAD answer anyone, everything else answers the operator. So the verb
// on each line below is a decision about who may call it, not a matter of REST
// taste. Every operation that changes the node is a POST. The reads are GET,
// because a read of what a node IS may be answered to whoever asks — what may be
// disclosed is governed by the TYPE, with `json:"-"` on the fields that hold key
// material (config/node/config.go), and that is the one home for it.
//
// db/value is the exception and the reason this paragraph exists. It changes
// nothing, but its input names ANY key in the node's database, so what it
// discloses is chosen by the caller and no type can mark it. A value nothing can
// govern is one the operator answers for, and a POST is how this node says so.
//
// # aliasChain edits the live route table
//
// chain/alias adds a name to a running chain and mounts /v1/chain/<alias> for it
// while the node serves. That is an operator's authority over their own node,
// held by the same rule as every other change here.

package admin

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	apiadmin "github.com/luxfi/api/admin"
	"github.com/luxfi/constants"
	"github.com/luxfi/filesystem/perms"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	nodeconfig "github.com/luxfi/node/config/node"
	"github.com/luxfi/node/utils"
	"github.com/zap-proto/zip"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// Ops is this service's typed operations. The paths are relative to where the
// app is mounted, which the node decides — a service does not name its own
// address.
func (a *Service) Ops() *zip.App {
	app := zip.New(zip.Config{
		AppName:               "admin",
		Logger:                a.Log,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux node admin",
			Description: "What the operator of a Lux node may do to it: profile it, alias its chains, move its log levels, change what it tracks, read its database and back it up.",
		},
	})
	zip.Post(app, "/profile/cpu/start", a.startCPU)
	zip.Post(app, "/profile/cpu/stop", a.stopCPU)
	zip.Post(app, "/profile/memory", a.memoryProfile)
	zip.Post(app, "/profile/lock", a.lockProfile)
	zip.Post(app, "/stacktrace", a.stacktrace)
	zip.Post(app, "/route/alias", a.aliasRoute)
	zip.Post(app, "/chain/alias", a.aliasChain)
	zip.Get(app, "/chain/aliases", a.chainAliases)
	zip.Post(app, "/log/level", a.setLogLevel)
	zip.Get(app, "/log/level", a.logLevel)
	zip.Get(app, "/config", a.config)
	zip.Get(app, "/plugins", a.listVMs)
	zip.Post(app, "/plugins/load", a.loadVMs)
	zip.Post(app, "/db/value", a.dbValue)
	zip.Get(app, "/chains/tracked", a.trackedChains)
	zip.Post(app, "/chains/tracked", a.trackChains)
	zip.Post(app, "/snapshot", a.snapshot)
	zip.Post(app, "/snapshot/restore", a.restore)
	return app
}

// StartCPU begins writing a CPU profile into the node's profile directory.
//
// Response: {}
func (a *Service) startCPU(_ context.Context, _ *struct{}) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "startCPUProfiler"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.StartCPUProfiler()
}

// StopCPU ends the CPU profile and closes the file it was written to.
//
// Response: {}
func (a *Service) stopCPU(_ context.Context, _ *struct{}) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "stopCPUProfiler"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.StopCPUProfiler()
}

// MemoryProfile writes a heap profile into the node's profile directory.
//
// Response: {}
func (a *Service) memoryProfile(_ context.Context, _ *struct{}) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "memoryProfile"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.MemoryProfile()
}

// LockProfile writes a mutex-contention profile into the node's profile
// directory.
//
// Response: {}
func (a *Service) lockProfile(_ context.Context, _ *struct{}) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "lockProfile"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.LockProfile()
}

// Stacktrace writes every goroutine's stack to stacktrace.txt beside the node.
//
// Response: {}
func (a *Service) stacktrace(_ context.Context, _ *struct{}) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "stacktrace"))
	stacktrace := []byte(utils.GetStacktrace(true))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, perms.WriteFile(stacktraceFile, stacktrace, perms.ReadWrite)
}

// AliasRoute gives one of this node's API endpoints a second address to answer
// on.
//
// Example: {"endpoint": "chain/X", "alias": "myChain"}
// Response: {}
func (a *Service) aliasRoute(_ context.Context, in *apiadmin.AliasArgs) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "alias"),
		log.String("endpoint", in.Endpoint),
		log.String("alias", in.Alias),
	)
	if len(in.Alias) > maxAliasLength {
		return nil, errAliasTooLong
	}
	return &apiadmin.EmptyReply{}, a.HTTPServer.AddAliasesWithReadLock(in.Endpoint, in.Alias)
}

// AliasChain gives a running chain another name, and mounts that name on this
// node's router while it serves.
//
// Example: {"chain": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM", "alias": "myChain"}
// Response: {}
func (a *Service) aliasChain(_ context.Context, in *apiadmin.AliasChainArgs) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "aliasChain"),
		log.String("chain", in.Chain),
		log.String("alias", in.Alias),
	)
	if len(in.Alias) > maxAliasLength {
		return nil, errAliasTooLong
	}
	chainID, err := a.ChainManager.Lookup(in.Chain)
	if err != nil {
		return nil, err
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	if err := a.ChainManager.Alias(chainID, in.Alias); err != nil {
		return nil, err
	}

	endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
	alias := path.Join(constants.ChainAliasPrefix, in.Alias)
	return &apiadmin.EmptyReply{}, a.HTTPServer.AddAliasesWithReadLock(endpoint, alias)
}

// ChainAliases are the names a chain answers to.
//
// Example: {"chain": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"}
// Response: {"aliases": ["X", "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"]}
func (a *Service) chainAliases(_ context.Context, in *apiadmin.GetChainAliasesArgs) (*apiadmin.GetChainAliasesReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "getChainAliases"),
		log.String("chain", in.Chain),
	)
	id, err := ids.FromString(in.Chain)
	if err != nil {
		return nil, err
	}
	aliases, err := a.ChainManager.Aliases(id)
	if err != nil {
		return nil, err
	}
	return &apiadmin.GetChainAliasesReply{Aliases: aliases}, nil
}

// SetLogLevel moves what a named logger writes to its file, to the display, or
// to both. A level that does not parse leaves every logger untouched.
//
// Example: {"loggerName": "C", "logLevel": "debug", "displayLevel": "info"}
// Response: {}
func (a *Service) setLogLevel(_ context.Context, in *apiadmin.SetLoggerLevelArgs) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "setLoggerLevel"),
		log.String("loggerName", in.LoggerName),
		log.String("logLevel", in.LogLevel),
		log.String("displayLevel", in.DisplayLevel),
	)
	if in.LogLevel == "" && in.DisplayLevel == "" {
		return nil, errNoLogLevel
	}

	// Parse before mutating: a rejected level must leave every logger untouched.
	var logLevel, displayLevel log.Level
	if in.LogLevel != "" {
		var err error
		if logLevel, err = log.ToLevel(in.LogLevel); err != nil {
			return nil, err
		}
	}
	if in.DisplayLevel != "" {
		var err error
		if displayLevel, err = log.ToLevel(in.DisplayLevel); err != nil {
			return nil, err
		}
	}

	names, err := a.loggerNames(in.LoggerName)
	if err != nil {
		return nil, err
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	for _, name := range names {
		// Only the levels the caller supplied — an omitted level keeps its value
		// instead of being reset to the zero Level.
		if in.LogLevel != "" {
			a.LogFactory.SetLogLevel(name, logLevel)
		}
		if in.DisplayLevel != "" {
			a.LogFactory.SetDisplayLevel(name, displayLevel)
		}
	}
	return &apiadmin.EmptyReply{}, nil
}

// LogLevel is what a named logger writes to its file and to the display.
// Loggers are addressed by name and cannot be enumerated, so a name is required.
//
// Example: {"loggerName": "C"}
// Response: {"loggerLevels": {"C": {"logLevel": "DEBUG", "displayLevel": "INFO"}}}
func (a *Service) logLevel(_ context.Context, in *apiadmin.GetLoggerLevelArgs) (*apiadmin.LoggerLevelReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "getLoggerLevel"),
		log.String("loggerName", in.LoggerName),
	)

	names, err := a.loggerNames(in.LoggerName)
	if err != nil {
		return nil, err
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	levels, err := a.logLevels(names)
	if err != nil {
		return nil, err
	}
	return &apiadmin.LoggerLevelReply{LoggerLevels: levels}, nil
}

// Config is the configuration this node booted with. The fields that hold key
// material are marked `json:"-"` on the type and are not in the answer.
func (a *Service) config(_ context.Context, _ *struct{}) (*nodeconfig.Config, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "getConfig"))
	return a.NodeConfig, nil
}

// ListVMs are the VMs installed on this node, the names each answers to, and
// where each one's plugin was found. It is addressed as the plugins because
// that is what it adds over info's own list of VMs — an operator asking this
// wants to know which file on this disk a chain is running.
//
// Response: {"vms": {"mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6": {"id": "mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6", "aliases": ["platformvm"]}}}
func (a *Service) listVMs(ctx context.Context, _ *struct{}) (*apiadmin.ListVMsReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "listVMs"))

	a.lock.RLock()
	defer a.lock.RUnlock()

	vmIDs, err := a.VMManager.ListFactories(ctx)
	if err != nil {
		return nil, err
	}

	pluginPaths := make(map[ids.ID]string)
	if a.PluginDir != "" {
		entries, err := os.ReadDir(a.PluginDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				baseName := name[:len(name)-len(filepath.Ext(name))]
				if baseName == "" {
					continue
				}
				vmID, err := ids.FromString(baseName)
				if err == nil {
					pluginPaths[vmID] = filepath.Join(a.PluginDir, name)
				}
			}
		}
	}

	installed := make(apiadmin.InstalledVMs, 0, len(vmIDs))
	for _, vmID := range vmIDs {
		aliases, err := a.VMManager.Aliases(ctx, vmID)
		if err != nil {
			return nil, err
		}

		vmIDStr := vmID.String()
		named := make([]string, 0, len(aliases))
		for _, alias := range aliases {
			if alias != vmIDStr {
				named = append(named, alias)
			}
		}

		info := apiadmin.VMInfo{ID: vmIDStr, Aliases: named}
		if where, ok := pluginPaths[vmID]; ok {
			info.Path = where
		}
		installed = append(installed, info)
	}
	slices.SortFunc(installed, func(x, y apiadmin.VMInfo) int { return strings.Compare(x.ID, y.ID) })

	return &apiadmin.ListVMsReply{VMs: installed}, nil
}

// LoadVMs rescans the plugin directory and brings in any VM that was not there
// at boot, then retries the chains that were waiting for one.
//
// Response: {"newVMs": {}, "failedVMs": {}, "chainsRetried": 0}
func (a *Service) loadVMs(ctx context.Context, _ *struct{}) (*apiadmin.LoadVMsReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "loadVMs"))

	a.lock.Lock()
	defer a.lock.Unlock()

	loaded, failed, err := a.VMRegistry.Reload(ctx)
	if err != nil {
		return nil, err
	}

	refused := make(apiadmin.FailedVMs, 0, len(failed))
	for vmID, err := range failed {
		refused = append(refused, apiadmin.FailedVM{VM: vmID, Error: err.Error()})
	}
	slices.SortFunc(refused, func(x, y apiadmin.FailedVM) int { return x.VM.Compare(y.VM) })

	brought := make(apiadmin.LoadedVMs, 0, len(loaded))
	retried := 0
	for _, vmID := range loaded {
		aliases, err := a.VMManager.Aliases(ctx, vmID)
		if err != nil {
			return nil, err
		}
		brought = append(brought, apiadmin.LoadedVM{VM: vmID, Aliases: aliases})
		retried += a.ChainManager.RetryPendingChains(vmID)
	}
	slices.SortFunc(brought, func(x, y apiadmin.LoadedVM) int { return x.VM.Compare(y.VM) })

	return &apiadmin.LoadVMsReply{
		NewVMs:        brought,
		FailedVMs:     refused,
		ChainsRetried: retried,
	}, nil
}

// DBValue is what the node's database holds at a key, hex for hex. It is a POST
// because the key names anything: what it discloses is chosen by the caller, so
// the operator answers for it.
//
// Example: {"key": "0x68656c6c6f"}
// Response: {"value": "0x776f726c64"}
func (a *Service) dbValue(_ context.Context, in *apiadmin.DBGetArgs) (*apiadmin.DBGetReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "dbGet"),
		log.String("key", in.Key),
	)

	key, err := formatting.Decode(formatting.HexNC, in.Key)
	if err != nil {
		return nil, err
	}

	value, err := a.DB.Get(key)
	if err != nil {
		return nil, err
	}

	val, err := formatting.Encode(formatting.HexNC, value)
	if err != nil {
		return nil, err
	}
	return &apiadmin.DBGetReply{Value: val}, nil
}

// TrackedChains are the chains this node follows.
//
// Response: {"trackedChains": ["2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"]}
func (a *Service) trackedChains(_ context.Context, _ *struct{}) (*apiadmin.GetTrackedChainsReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "getTrackedChains"))
	tracked := a.Network.TrackedChains().List()
	slices.SortFunc(tracked, func(x, y ids.ID) int { return x.Compare(y) })
	names := make([]string, len(tracked))
	for i, id := range tracked {
		names[i] = id.String()
	}
	return &apiadmin.GetTrackedChainsReply{TrackedChains: names}, nil
}

// TrackChains adds chains to the set this node follows.
//
// Example: {"chains": ["2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"]}
// Response: {"trackedChains": ["2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"]}
func (a *Service) trackChains(_ context.Context, in *apiadmin.SetTrackedChainsArgs) (*apiadmin.SetTrackedChainsReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "setTrackedChains"))

	asked := set.Set[ids.ID]{}
	for _, name := range in.Chains {
		chainID, err := ids.FromString(name)
		if err != nil {
			return nil, err
		}
		asked.Add(chainID)
	}

	for _, chainID := range asked.List() {
		if err := a.Network.TrackChain(chainID); err != nil {
			return nil, err
		}
	}

	return &apiadmin.SetTrackedChainsReply{TrackedChains: in.Chains}, nil
}

// Snapshot writes the node's database to a file, whole or since a version a
// previous snapshot returned. A path ending .zst or .zstd is compressed.
//
// Example: {"path": "/var/backups/node.zst", "since": 0}
// Response: {"version": 42}
func (a *Service) snapshot(_ context.Context, in *apiadmin.SnapshotArgs) (*apiadmin.SnapshotReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "snapshot"),
		log.String("path", in.Path),
		log.Uint64("since", in.Since),
	)

	if a.backupService == nil {
		return nil, errNoBackup
	}
	if in.Path == "" {
		return nil, errNoPath
	}

	compress := strings.HasSuffix(in.Path, ".zst") || strings.HasSuffix(in.Path, ".zstd")
	version, err := a.backupService.BackupToFile(in.Path, in.Since, compress)
	if err != nil {
		return nil, err
	}
	return &apiadmin.SnapshotReply{Version: version}, nil
}

// Restore reads a snapshot back into the node's database, overwriting what is
// there. A path ending .zst or .zstd is decompressed.
//
// Example: {"path": "/var/backups/node.zst"}
// Response: {}
func (a *Service) restore(_ context.Context, in *apiadmin.LoadArgs) (*apiadmin.EmptyReply, error) {
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "load"),
		log.String("path", in.Path),
	)

	if a.backupService == nil {
		return nil, errNoBackup
	}
	if in.Path == "" {
		return nil, errNoPath
	}

	compressed := strings.HasSuffix(in.Path, ".zst") || strings.HasSuffix(in.Path, ".zstd")
	if err := a.backupService.RestoreFromFile(in.Path, compressed); err != nil {
		return nil, err
	}
	return &apiadmin.EmptyReply{}, nil
}

var (
	errNoBackup = errors.New("backup service not initialized")
	errNoPath   = errors.New("backup path is required")
)
