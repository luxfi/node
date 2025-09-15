// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"log/slog"

	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/logging"
)

// LoggerWrapper wraps luxfi/log.Logger to implement utils/logging.Logger interface
// This bridges the gap between the luxfi/log package and the utils/logging package
type LoggerWrapper struct {
	logger log.Logger
}

// NewLoggerWrapper creates a new LoggerWrapper
func NewLoggerWrapper(logger log.Logger) logging.Logger {
	return &LoggerWrapper{logger: logger}
}

func (l *LoggerWrapper) Debug(msg string, fields ...log.Field) {
	l.logger.Debug(msg, fields...)
}

func (l *LoggerWrapper) Info(msg string, fields ...log.Field) {
	l.logger.Info(msg, fields...)
}

func (l *LoggerWrapper) Warn(msg string, fields ...log.Field) {
	l.logger.Warn(msg, fields...)
}

func (l *LoggerWrapper) Error(msg string, fields ...log.Field) {
	l.logger.Error(msg, fields...)
}

func (l *LoggerWrapper) Fatal(msg string, fields ...log.Field) {
	l.logger.Fatal(msg, fields...)
}

func (l *LoggerWrapper) Trace(msg string, fields ...log.Field) {
	l.logger.Trace(msg, fields...)
}

func (l *LoggerWrapper) Verbo(msg string, fields ...log.Field) {
	l.logger.Verbo(msg, fields...)
}

func (l *LoggerWrapper) Write(p []byte) (n int, err error) {
	return l.logger.Write(p)
}

// Enabled returns true if the given level is at or above this level.
func (l *LoggerWrapper) Enabled(lvl logging.Level) bool {
	// Convert logging.Level to slog.Level
	var slogLevel slog.Level
	switch lvl {
	case logging.Verbo:
		slogLevel = log.LevelVerbo
	case logging.Debug:
		slogLevel = log.LevelDebug
	case logging.Trace:
		slogLevel = log.LevelTrace
	case logging.Info:
		slogLevel = log.LevelInfo
	case logging.Warn:
		slogLevel = log.LevelWarn
	case logging.Error:
		slogLevel = log.LevelError
	case logging.Fatal:
		slogLevel = log.LevelFatal
	default:
		slogLevel = log.LevelInfo
	}
	return l.logger.EnabledLevel(slogLevel)
}

func (l *LoggerWrapper) SetLevel(level logging.Level) {
	// Convert logging.Level to slog.Level
	var slogLevel slog.Level
	switch level {
	case logging.Verbo:
		slogLevel = log.LevelVerbo
	case logging.Debug:
		slogLevel = log.LevelDebug
	case logging.Trace:
		slogLevel = log.LevelTrace
	case logging.Info:
		slogLevel = log.LevelInfo
	case logging.Warn:
		slogLevel = log.LevelWarn
	case logging.Error:
		slogLevel = log.LevelError
	case logging.Fatal:
		slogLevel = log.LevelFatal
	default:
		slogLevel = log.LevelInfo
	}
	l.logger.SetLevel(slogLevel)
}

func (l *LoggerWrapper) StopOnPanic() {
	l.logger.StopOnPanic()
}

func (l *LoggerWrapper) RecoverAndPanic(f func()) {
	l.logger.RecoverAndPanic(f)
}

func (l *LoggerWrapper) RecoverAndExit(f, exit func()) {
	l.logger.RecoverAndExit(f, exit)
}

func (l *LoggerWrapper) Stop() {
	l.logger.Stop()
}