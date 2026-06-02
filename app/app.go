// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/luxfi/filesystem/perms"
	"github.com/luxfi/log"
	"github.com/luxfi/node/node"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/sys/ulimit"

	nodeconfig "github.com/luxfi/node/config/node"
)

const Header = `
▼▼▼▼▼
 ▼▼▼
  ▼

`

var _ App = (*app)(nil)

type App interface {
	// Start kicks off the application and returns immediately.
	// Start should only be called once.
	Start()

	// Stop notifies the application to exit and returns immediately.
	// Stop should only be called after [Start].
	// It is safe to call Stop multiple times.
	Stop()

	// ExitCode should only be called after [Start] returns. It
	// should block until the application finishes
	ExitCode() int
}

func New(config nodeconfig.Config) (App, error) {
	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(config.DatabaseConfig.Path, true, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to restrict the permissions of the database directory with: %w", err)
	}
	// LoggingConfig removed from node.Config
	// if err := perms.ChmodR(config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
	// 	return nil, fmt.Errorf("failed to restrict the permissions of the log directory with: %w", err)
	// }

	// Create a logger and log factory
	// Use info level by default to avoid excessive logging (debug level causes massive log spam)
	infoLevel, _ := log.ToLevel("info")
	logFactory := log.NewFactoryWithConfig(log.Config{
		RotatingWriterConfig: log.RotatingWriterConfig{
			MaxSize:   8, // 8MB per log file (reasonable for standard profile)
			MaxFiles:  5, // Keep 5 rotated files
			MaxAge:    7, // 7 days retention
			Directory: config.DatabaseConfig.Path,
		},
		DisplayLevel: infoLevel,
		LogLevel:     infoLevel, // Use info level, not debug (prevents log spam)
	})
	logger, err := logFactory.Make("main")
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// update fd limit
	fdLimit := config.FdLimit
	if err := ulimit.Set(fdLimit, logger); err != nil {
		logger.Error("failed to set fd-limit",
			"error", err,
		)
		return nil, err
	}

	n, err := node.New(&config, logFactory, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize node: %w", err)
	}

	return &app{
		node:       n,
		log:        logger,
		logFactory: logFactory,
	}, nil
}

func Run(app App) int {
	// start running the application
	app.Start()

	// register terminationSignals to kill the application
	terminationSignals := make(chan os.Signal, 1)
	signal.Notify(terminationSignals, syscall.SIGINT, syscall.SIGTERM)

	stackTraceSignal := make(chan os.Signal, 1)
	signal.Notify(stackTraceSignal, syscall.SIGABRT)

	// start up a new go routine to handle attempts to kill the application
	go func() {
		for range terminationSignals {
			app.Stop()
			return
		}
	}()

	// start a goroutine to listen on SIGABRT signals,
	// to print the stack trace to standard error.
	go func() {
		for range stackTraceSignal {
			fmt.Fprint(os.Stderr, utils.GetStacktrace(true))
		}
	}()

	// wait for the app to exit and get the exit code response
	exitCode := app.ExitCode()

	// shut down the termination signal go routine
	signal.Stop(terminationSignals)
	close(terminationSignals)

	// shut down the stack trace go routine
	signal.Stop(stackTraceSignal)
	close(stackTraceSignal)

	// return the exit code that the application reported
	return exitCode
}

// app is a wrapper around a node that runs in this process
type app struct {
	node       *node.Node
	log        log.Logger
	logFactory log.Factory
	exitWG     sync.WaitGroup
}

// Start the business logic of the node (as opposed to config reading, etc).
// Does not block until the node is done.
func (a *app) Start() {
	// [p.ExitCode] will block until [p.exitWG.Done] is called
	a.exitWG.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.log.Error("caught panic", "panic", r)
			}
			// a.log.Stop() // Not available in new log module
			// a.logFactory.Close() // Not available
			a.exitWG.Done()
		}()
		defer func() {
			// If [p.node.Dispatch()] panics, then we should log the panic and
			// then re-raise the panic. This is why the above defer is broken
			// into two parts.
			// a.log.StopOnPanic() // Not available
		}()

		err := a.node.Dispatch()
		a.log.Debug("dispatch returned",
			log.Reflect("error", err),
		)
	}()
}

// Stop attempts to shutdown the currently running node. This function will
// block until Shutdown returns.
func (a *app) Stop() {
	a.node.Shutdown(0)
}

// ExitCode returns the exit code that the node is reporting. This function
// blocks until the node has been shut down.
func (a *app) ExitCode() int {
	a.exitWG.Wait()
	return a.node.ExitCode()
}
