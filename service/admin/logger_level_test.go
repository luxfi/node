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

// TestSetLoggerLevel_MovesTheLevelAndGetReportsIt is the regression: set, then read
// back through the API. Before the fix the set was discarded and the get returned an
// empty map, so both halves silently lied.
func TestSetLoggerLevel_MovesTheLevelAndGetReportsIt(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	svc := newLevelTestService(t, "C")

	_, err := svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName:   "C",
		LogLevel:     "debug",
		DisplayLevel: "error",
	})
	require.NoError(err)

	reply, err := svc.GetLoggerLevel(ctx, &apiadmin.GetLoggerLevelArgs{LoggerName: "C"})
	require.NoError(err)
	require.Equal(
		map[string]apiadmin.LogAndDisplayLevels{"C": {
			LogLevel:     log.DebugLevel.String(),
			DisplayLevel: log.ErrorLevel.String(),
		}},
		reply.LoggerLevels,
		"setLoggerLevel must move the live logger's level and getLoggerLevel must report it",
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

	_, err := svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName: "node", LogLevel: "trace", DisplayLevel: "warn",
	})
	require.NoError(err)

	// Only displayLevel this time — logLevel must survive.
	_, err = svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{
		LoggerName: "node", DisplayLevel: "fatal",
	})
	require.NoError(err)

	reply, err := svc.GetLoggerLevel(ctx, &apiadmin.GetLoggerLevelArgs{LoggerName: "node"})
	require.NoError(err)
	require.Equal(log.TraceLevel.String(), reply.LoggerLevels["node"].LogLevel)
	require.Equal(log.FatalLevel.String(), reply.LoggerLevels["node"].DisplayLevel)
}

// TestLoggerLevel_RejectsUnservableAndInvalidArgs — every refusal is explicit. log.Factory
// addresses loggers BY NAME and exposes no enumeration, so an empty name cannot be served;
// returning 200 OK for it is the bug, not the contract.
func TestLoggerLevel_RejectsUnservableAndInvalidArgs(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	svc := newLevelTestService(t, "C")

	_, err := svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{LogLevel: "debug"})
	require.ErrorIs(err, errNoLoggerName)

	_, err = svc.GetLoggerLevel(ctx, &apiadmin.GetLoggerLevelArgs{})
	require.ErrorIs(err, errNoLoggerName)

	_, err = svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{LoggerName: "C"})
	require.ErrorIs(err, errNoLogLevel)

	// An unparseable level must be refused BEFORE anything is mutated.
	before, err := svc.LogFactory.GetLogLevel("C")
	require.NoError(err)
	_, err = svc.SetLoggerLevel(ctx, &apiadmin.SetLoggerLevelArgs{LoggerName: "C", LogLevel: "loud"})
	require.Error(err)
	after, err := svc.LogFactory.GetLogLevel("C")
	require.NoError(err)
	require.Equal(before, after, "a rejected level must leave the logger untouched")
}
