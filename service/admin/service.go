// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	apiadmin "github.com/luxfi/api/admin"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/filesystem/perms"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/service/backup"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/profiler"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/registry"
)

const (
	maxAliasLength = 512

	// Name of file that stacktraces are written to.
	stacktraceFile = "stacktrace.txt"
)

var (
	errAliasTooLong = errors.New("alias length is too long")
	errNoLogLevel   = errors.New("need to specify either displayLevel or logLevel")
	errNoLoggerName = errors.New("need to specify loggerName: loggers are addressed by name and cannot be enumerated")
)

// ChainTracker is the interface for tracking chains at runtime.
type ChainTracker interface {
	TrackChain(chainID ids.ID) error
	TrackedChains() set.Set[ids.ID]
}

type Config struct {
	Log          log.Logger
	ProfileDir   string
	LogFactory   log.Factory
	NodeConfig   interface{}
	DB           database.Database
	ChainManager chains.Manager
	HTTPServer   server.PathAdderWithReadLock
	VMRegistry   registry.VMRegistry
	VMManager    vms.Manager
	PluginDir    string
	Network      ChainTracker
	// DataDir is the node's data directory, used for backup metadata.
	DataDir string
}

// Service implements api/admin.Service.
type Service struct {
	Config
	lock          sync.RWMutex
	profiler      profiler.Profiler
	backupService *backup.Service
}

func New(config Config) *Service {
	svc := &Service{
		Config:   config,
		profiler: profiler.New(config.ProfileDir),
	}

	// Initialize backup service if data directory is configured
	if config.DataDir != "" && config.DB != nil {
		backupSvc, err := backup.New(backup.Config{
			DB:          config.DB,
			MetadataDir: filepath.Join(config.DataDir, "backup"),
			Log:         config.Log,
		})
		if err != nil {
			config.Log.Warn("failed to initialize backup service", "error", err)
		} else {
			svc.backupService = backupSvc
		}
	}

	return svc
}

func (a *Service) StartCPUProfiler(ctx context.Context) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "startCPUProfiler"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.StartCPUProfiler()
}

func (a *Service) StopCPUProfiler(ctx context.Context) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "stopCPUProfiler"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.StopCPUProfiler()
}

func (a *Service) MemoryProfile(ctx context.Context) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "memoryProfile"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.MemoryProfile()
}

func (a *Service) LockProfile(ctx context.Context) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "lockProfile"))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, a.profiler.LockProfile()
}

func (a *Service) Alias(ctx context.Context, args *apiadmin.AliasArgs) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "alias"),
		log.String("endpoint", args.Endpoint),
		log.String("alias", args.Alias),
	)
	if len(args.Alias) > maxAliasLength {
		return nil, errAliasTooLong
	}
	return &apiadmin.EmptyReply{}, a.HTTPServer.AddAliasesWithReadLock(args.Endpoint, args.Alias)
}

func (a *Service) AliasChain(ctx context.Context, args *apiadmin.AliasChainArgs) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "aliasChain"),
		log.String("chain", args.Chain),
		log.String("alias", args.Alias),
	)
	if len(args.Alias) > maxAliasLength {
		return nil, errAliasTooLong
	}
	chainID, err := a.ChainManager.Lookup(args.Chain)
	if err != nil {
		return nil, err
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	if err := a.ChainManager.Alias(chainID, args.Alias); err != nil {
		return nil, err
	}

	endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
	alias := path.Join(constants.ChainAliasPrefix, args.Alias)
	return &apiadmin.EmptyReply{}, a.HTTPServer.AddAliasesWithReadLock(endpoint, alias)
}

func (a *Service) GetChainAliases(ctx context.Context, args *apiadmin.GetChainAliasesArgs) (*apiadmin.GetChainAliasesReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "getChainAliases"),
		log.String("chain", args.Chain),
	)
	id, err := ids.FromString(args.Chain)
	if err != nil {
		return nil, err
	}
	aliases, err := a.ChainManager.Aliases(id)
	if err != nil {
		return nil, err
	}
	return &apiadmin.GetChainAliasesReply{Aliases: aliases}, nil
}

func (a *Service) Stacktrace(ctx context.Context) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "stacktrace"))
	stacktrace := []byte(utils.GetStacktrace(true))
	a.lock.Lock()
	defer a.lock.Unlock()
	return &apiadmin.EmptyReply{}, perms.WriteFile(stacktraceFile, stacktrace, perms.ReadWrite)
}

func (a *Service) SetLoggerLevel(ctx context.Context, args *apiadmin.SetLoggerLevelArgs) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "setLoggerLevel"),
		log.String("loggerName", args.LoggerName),
		log.String("logLevel", args.LogLevel),
		log.String("displayLevel", args.DisplayLevel),
	)
	if args.LogLevel == "" && args.DisplayLevel == "" {
		return nil, errNoLogLevel
	}

	// Parse before mutating: a rejected level must leave every logger untouched.
	var logLevel, displayLevel log.Level
	if args.LogLevel != "" {
		var err error
		if logLevel, err = log.ToLevel(args.LogLevel); err != nil {
			return nil, err
		}
	}
	if args.DisplayLevel != "" {
		var err error
		if displayLevel, err = log.ToLevel(args.DisplayLevel); err != nil {
			return nil, err
		}
	}

	loggerNames, err := a.getLoggerNames(args.LoggerName)
	if err != nil {
		return nil, err
	}

	a.lock.Lock()
	defer a.lock.Unlock()

	for _, name := range loggerNames {
		// Only the levels the caller supplied — an omitted level keeps its value
		// instead of being reset to the zero Level.
		if args.LogLevel != "" {
			a.LogFactory.SetLogLevel(name, logLevel)
		}
		if args.DisplayLevel != "" {
			a.LogFactory.SetDisplayLevel(name, displayLevel)
		}
	}
	return &apiadmin.EmptyReply{}, nil
}

func (a *Service) GetLoggerLevel(ctx context.Context, args *apiadmin.GetLoggerLevelArgs) (*apiadmin.LoggerLevelReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "getLoggerLevel"),
		log.String("loggerName", args.LoggerName),
	)

	loggerNames, err := a.getLoggerNames(args.LoggerName)
	if err != nil {
		return nil, err
	}

	a.lock.RLock()
	defer a.lock.RUnlock()

	loggerLevels, err := a.getLogLevels(loggerNames)
	if err != nil {
		return nil, err
	}
	return &apiadmin.LoggerLevelReply{LoggerLevels: loggerLevels}, nil
}

func (a *Service) GetConfig(ctx context.Context) (any, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "getConfig"))
	return a.NodeConfig, nil
}

func (a *Service) LoadVMs(ctx context.Context) (*apiadmin.LoadVMsReply, error) {
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "loadVMs"))

	a.lock.Lock()
	defer a.lock.Unlock()

	loadedVMs, failedVMs, err := a.VMRegistry.Reload(ctx)
	if err != nil {
		return nil, err
	}

	failedVMsParsed := make(map[ids.ID]string)
	for vmID, err := range failedVMs {
		failedVMsParsed[vmID] = err.Error()
	}

	newVMs := make(map[ids.ID][]string, len(loadedVMs))
	for _, vmID := range loadedVMs {
		aliases, err := a.VMManager.Aliases(ctx, vmID)
		if err != nil {
			return nil, err
		}
		newVMs[vmID] = aliases
	}

	totalRetried := 0
	for _, vmID := range loadedVMs {
		retried := a.ChainManager.RetryPendingChains(vmID)
		totalRetried += retried
	}

	return &apiadmin.LoadVMsReply{
		NewVMs:        newVMs,
		FailedVMs:     failedVMsParsed,
		ChainsRetried: totalRetried,
	}, nil
}

func (a *Service) DbGet(ctx context.Context, args *apiadmin.DBGetArgs) (*apiadmin.DBGetReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "dbGet"),
		log.String("key", args.Key),
	)

	key, err := formatting.Decode(formatting.HexNC, args.Key)
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

func (a *Service) ListVMs(ctx context.Context) (*apiadmin.ListVMsReply, error) {
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

	reply := &apiadmin.ListVMsReply{
		VMs: make(map[string]apiadmin.VMInfo, len(vmIDs)),
	}
	for _, vmID := range vmIDs {
		aliases, err := a.VMManager.Aliases(ctx, vmID)
		if err != nil {
			return nil, err
		}

		vmIDStr := vmID.String()
		filteredAliases := make([]string, 0, len(aliases))
		for _, alias := range aliases {
			if alias != vmIDStr {
				filteredAliases = append(filteredAliases, alias)
			}
		}

		info := apiadmin.VMInfo{
			ID:      vmIDStr,
			Aliases: filteredAliases,
		}
		if path, ok := pluginPaths[vmID]; ok {
			info.Path = path
		}

		reply.VMs[vmIDStr] = info
	}

	return reply, nil
}

func (a *Service) SetTrackedChains(ctx context.Context, args *apiadmin.SetTrackedChainsArgs) (*apiadmin.SetTrackedChainsReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "setTrackedChains"))

	chains := set.Set[ids.ID]{}
	for _, chainStr := range args.Chains {
		chainID, err := ids.FromString(chainStr)
		if err != nil {
			return nil, err
		}
		chains.Add(chainID)
	}

	for _, chainID := range chains.List() {
		if err := a.Network.TrackChain(chainID); err != nil {
			return nil, err
		}
	}

	return &apiadmin.SetTrackedChainsReply{TrackedChains: args.Chains}, nil
}

func (a *Service) GetTrackedChains(ctx context.Context) (*apiadmin.GetTrackedChainsReply, error) {
	_ = ctx
	a.Log.Debug("API called", log.String("service", "admin"), log.String("method", "getTrackedChains"))
	trackedIDs := a.Network.TrackedChains().List()
	trackedChains := make([]string, len(trackedIDs))
	for i, id := range trackedIDs {
		trackedChains[i] = id.String()
	}
	return &apiadmin.GetTrackedChainsReply{TrackedChains: trackedChains}, nil
}

// getLoggerNames resolves the loggerName argument to the loggers to act on — the one
// place either logger-level endpoint decides what it is addressing.
//
// log.Factory addresses loggers BY NAME and exposes no enumeration, so the "every
// logger" form (an empty name) cannot be served. Refuse it explicitly: answering 200 OK
// while doing nothing is what made these endpoints unusable.
func (a *Service) getLoggerNames(loggerName string) ([]string, error) {
	if loggerName == "" {
		return nil, errNoLoggerName
	}
	return []string{loggerName}, nil
}

func (a *Service) getLogLevels(loggerNames []string) (map[string]apiadmin.LogAndDisplayLevels, error) {
	loggerLevels := make(map[string]apiadmin.LogAndDisplayLevels, len(loggerNames))
	for _, name := range loggerNames {
		logLevel, err := a.LogFactory.GetLogLevel(name)
		if err != nil {
			return nil, err
		}
		displayLevel, err := a.LogFactory.GetDisplayLevel(name)
		if err != nil {
			return nil, err
		}
		loggerLevels[name] = apiadmin.LogAndDisplayLevels{
			LogLevel:     logLevel.String(),
			DisplayLevel: displayLevel.String(),
		}
	}
	return loggerLevels, nil
}

// Snapshot creates a backup of the database to the specified path.
// Args:
//   - Path: file path to write the backup (supports .zst extension for compression)
//   - Since: version for incremental backup (0 for full backup)
//
// Returns the version number for use in future incremental backups.
func (a *Service) Snapshot(ctx context.Context, args *apiadmin.SnapshotArgs) (*apiadmin.SnapshotReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "snapshot"),
		log.String("path", args.Path),
		log.Uint64("since", args.Since),
	)

	if a.backupService == nil {
		return nil, errors.New("backup service not initialized")
	}

	if args.Path == "" {
		return nil, errors.New("backup path is required")
	}

	// Determine if compression is requested based on file extension
	compress := strings.HasSuffix(args.Path, ".zst") || strings.HasSuffix(args.Path, ".zstd")

	version, err := a.backupService.BackupToFile(args.Path, args.Since, compress)
	if err != nil {
		return nil, err
	}

	return &apiadmin.SnapshotReply{Version: version}, nil
}

// Load restores the database from a backup file.
// Args:
//   - Path: file path to read the backup from (detects .zst extension for decompression)
//
// Warning: This will overwrite the current database state.
func (a *Service) Load(ctx context.Context, args *apiadmin.LoadArgs) (*apiadmin.EmptyReply, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "load"),
		log.String("path", args.Path),
	)

	if a.backupService == nil {
		return nil, errors.New("backup service not initialized")
	}

	if args.Path == "" {
		return nil, errors.New("backup path is required")
	}

	// Determine if decompression is needed based on file extension
	compressed := strings.HasSuffix(args.Path, ".zst") || strings.HasSuffix(args.Path, ".zstd")

	if err := a.backupService.RestoreFromFile(args.Path, compressed); err != nil {
		return nil, err
	}

	return &apiadmin.EmptyReply{}, nil
}

// GetBackupMetadata returns information about the last backup.
func (a *Service) GetBackupMetadata(ctx context.Context) (*backup.Metadata, error) {
	_ = ctx
	a.Log.Debug("API called",
		log.String("service", "admin"),
		log.String("method", "getBackupMetadata"),
	)

	if a.backupService == nil {
		return nil, errors.New("backup service not initialized")
	}

	return a.backupService.GetMetadata(), nil
}

var _ apiadmin.Service = (*Service)(nil)
var _ = apitypes.EmptyReply{}
