// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// logger_level_test.go — admin.setLoggerLevel / admin.getLoggerLevel must actually
// move a live logger's level.
//
// They did not. SetLoggerLevel computed the logger names and threw them away
// (`loggerNames := a.getLoggerNames(...); _ = loggerNames`) and getLogLevels returned
// an empty map unconditionally, so BOTH endpoints answered 200 OK having done nothing.
// That is why the 2026-07-28 devnet/testnet build-loop diagnosis had to be run off boot
// logs: raising a running node's log level was impossible.
//
// The tests drive the REAL log.Factory (the same one the node builds), not a double,
// so a passing SetLoggerLevel means the level the logger actually filters on moved.
package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiadmin "github.com/luxfi/api/admin"
	"github.com/luxfi/log"
)

// newLevelTestService returns an admin service over a real log.Factory that already
// holds one registered logger — the shape the node runs in.
func newLevelTestService(t *testing.T, loggerName string) *Service {
	t.Helper()

	factory := log.NewFactory()
	t.Cleanup(factory.Close)

	_, err := factory.Make(loggerName)
	require.NoError(t, err)

	return &Service{Config: Config{Log: log.Noop(), LogFactory: factory}}
}

// TestSetLoggerLevel_MovesTheLevelAndGetReportsIt: set, then read back through the
// API. A set that is discarded and a get that answers an empty map both look like
// success, so the round trip is the only thing that catches either.
func TestSetLoggerLevel_MovesTheLevelAndGetReportsIt(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	svc := newLevelTestService(t, "C")

	_, err := svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName:   "C",
		LogLevel:     "debug",
		DisplayLevel: "error",
	})
	require.NoError(err)

	reply, err := svc.logLevel(ctx, &apiadmin.GetLoggerLevelArgs{LoggerName: "C"})
	require.NoError(err)
	require.Equal(
		apiadmin.LoggerLevels{{
			Logger: "C",
			Levels: apiadmin.LogAndDisplayLevels{
				LogLevel:     log.DebugLevel.String(),
				DisplayLevel: log.ErrorLevel.String(),
			},
		}},
		reply.LoggerLevels,
		"setting a level must move the live logger's level and reading it back must report it",
	)

	// The factory is the single source of truth — assert against it directly too, so a
	// getLoggerLevel that merely echoed the request back could not pass this test.
	logLevel, err := svc.LogFactory.GetLogLevel("C")
	require.NoError(err)
	require.Equal(log.DebugLevel, logLevel)
}

// TestSetLoggerLevel_OneLevelAtATime pins that omitting a level leaves it alone rather
// than resetting it to the zero Level.
func TestSetLoggerLevel_OneLevelAtATime(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	svc := newLevelTestService(t, "node")

	_, err := svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName: "node", LogLevel: "trace", DisplayLevel: "warn",
	})
	require.NoError(err)

	// Only displayLevel this time — logLevel must survive.
	_, err = svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName: "node", DisplayLevel: "fatal",
	})
	require.NoError(err)

	reply, err := svc.logLevel(ctx, &apiadmin.GetLoggerLevelArgs{LoggerName: "node"})
	require.NoError(err)
	require.Len(reply.LoggerLevels, 1)
	require.Equal("node", reply.LoggerLevels[0].Logger)
	require.Equal(log.TraceLevel.String(), reply.LoggerLevels[0].Levels.LogLevel)
	require.Equal(log.FatalLevel.String(), reply.LoggerLevels[0].Levels.DisplayLevel)
}

// TestLoggerLevel_RejectsUnservableAndInvalidArgs — every refusal is explicit. log.Factory
// addresses loggers BY NAME and exposes no enumeration, so an empty name cannot be served;
// returning 200 OK for it is the bug, not the contract.
func TestLoggerLevel_RejectsUnservableAndInvalidArgs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	svc := newLevelTestService(t, "C")

	_, err := svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{LogLevel: "debug"})
	require.ErrorIs(err, errNoLoggerName)

	_, err = svc.logLevel(ctx, &apiadmin.GetLoggerLevelArgs{})
	require.ErrorIs(err, errNoLoggerName)

	_, err = svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{LoggerName: "C"})
	require.ErrorIs(err, errNoLogLevel)

	// An unparseable level must be refused BEFORE anything is mutated.
	before, err := svc.LogFactory.GetLogLevel("C")
	require.NoError(err)
	_, err = svc.setLogLevel(ctx, &apiadmin.SetLoggerLevelArgs{LoggerName: "C", LogLevel: "loud"})
	require.Error(err)
	after, err := svc.LogFactory.GetLogLevel("C")
	require.NoError(err)
	require.Equal(before, after, "a rejected level must leave the logger untouched")
}
