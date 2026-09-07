// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Operating a node.
//
// The service is a value with a registry of typed operations on it — see ops.go,
// which is where every operation and its doc live. This file is the value: what
// the service is built from, what it holds, and the two things more than one
// operation needs.
package admin

import (
	"errors"
	"path/filepath"
	"sync"

	apiadmin "github.com/luxfi/api/admin"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	nodeconfig "github.com/luxfi/node/config/node"
	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/service/backup"
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
	Log        log.Logger
	ProfileDir string
	LogFactory log.Factory
	// NodeConfig is the configuration this node booted with. It is the node's
	// own type rather than an `any` because an operation's reply is its type:
	// an untyped one has no schema, no MCP shape and no generated client. What
	// may be DISCLOSED of it is a separate question with its own answer —
	// `json:"-"` on the fields that hold key material, in config/node.
	NodeConfig   *nodeconfig.Config
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

// Service is the node's operator API. Its operations are registered by
// [Service.Ops].
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

// loggerNames resolves the loggerName argument to the loggers to act on — the one
// place either logger-level operation decides what it is addressing.
//
// log.Factory addresses loggers BY NAME and exposes no enumeration, so the "every
// logger" form (an empty name) cannot be served. Refuse it explicitly: answering 200 OK
// while doing nothing is what made these endpoints unusable.
func (a *Service) loggerNames(loggerName string) ([]string, error) {
	if loggerName == "" {
		return nil, errNoLoggerName
	}
	return []string{loggerName}, nil
}

// logLevels reads the two levels each named logger is set to.
func (a *Service) logLevels(loggerNames []string) (apiadmin.LoggerLevels, error) {
	levels := make(apiadmin.LoggerLevels, 0, len(loggerNames))
	for _, name := range loggerNames {
		logLevel, err := a.LogFactory.GetLogLevel(name)
		if err != nil {
			return nil, err
		}
		displayLevel, err := a.LogFactory.GetDisplayLevel(name)
		if err != nil {
			return nil, err
		}
		levels = append(levels, apiadmin.LoggerLevel{
			Logger: name,
			Levels: apiadmin.LogAndDisplayLevels{
				LogLevel:     logLevel.String(),
				DisplayLevel: displayLevel.String(),
			},
		})
	}
	return levels, nil
}
