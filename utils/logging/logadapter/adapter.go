// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logadapter

import (
	"context"
	"log/slog"
	"os"

	"github.com/luxfi/log"
	luxlog "github.com/luxfi/log"
	"go.uber.org/zap"
)

// ToLogLogger adapts a luxlog.Logger to a log.Logger
func ToLogLogger(logger luxlog.Logger) log.Logger {
	return &adapter{logger: logger}
}

type adapter struct {
	logger luxlog.Logger
}

func (a *adapter) With(ctx ...interface{}) log.Logger {
	// Convert ctx to zap fields
	fields := make([]zap.Field, 0, len(ctx)/2)
	for i := 0; i < len(ctx)-1; i += 2 {
		if key, ok := ctx[i].(string); ok {
			fields = append(fields, zap.Any(key, ctx[i+1]))
		}
	}
	// Create a new logger with the fields
	return &adapter{logger: a.logger}
}

func (a *adapter) New(ctx ...interface{}) log.Logger {
	return a.With(ctx...)
}

func (a *adapter) Log(level slog.Level, msg string, ctx ...interface{}) {
	fields := ctxToFields(ctx)
	switch level {
	case log.LevelTrace:
		a.logger.Trace(msg, fields...)
	case slog.LevelDebug:
		a.logger.Debug(msg, fields...)
	case slog.LevelInfo:
		a.logger.Info(msg, fields...)
	case slog.LevelWarn:
		a.logger.Warn(msg, fields...)
	case slog.LevelError:
		a.logger.Error(msg, fields...)
	case log.LevelCrit:
		a.logger.Fatal(msg, fields...)
	}
}

func (a *adapter) Trace(msg string, ctx ...interface{}) {
	a.logger.Trace(msg, ctxToFields(ctx)...)
}

func (a *adapter) Debug(msg string, ctx ...interface{}) {
	a.logger.Debug(msg, ctxToFields(ctx)...)
}

func (a *adapter) Info(msg string, ctx ...interface{}) {
	a.logger.Info(msg, ctxToFields(ctx)...)
}

func (a *adapter) Warn(msg string, ctx ...interface{}) {
	a.logger.Warn(msg, ctxToFields(ctx)...)
}

func (a *adapter) Error(msg string, ctx ...interface{}) {
	a.logger.Error(msg, ctxToFields(ctx)...)
}

func (a *adapter) Crit(msg string, ctx ...interface{}) {
	a.logger.Fatal(msg, ctxToFields(ctx)...)
	os.Exit(1)
}

func (a *adapter) Write(level slog.Level, msg string, attrs ...any) {
	a.Log(level, msg, attrs...)
}

func (a *adapter) Enabled(ctx context.Context, level slog.Level) bool {
	// Assume all levels are enabled
	return true
}

func (a *adapter) Handler() slog.Handler {
	// Return a discard handler
	return slog.NewTextHandler(os.Stderr, nil)
}

// ctxToFields converts context key/value pairs to zap fields
func ctxToFields(ctx []interface{}) []zap.Field {
	fields := make([]zap.Field, 0, len(ctx)/2)
	for i := 0; i < len(ctx)-1; i += 2 {
		if key, ok := ctx[i].(string); ok {
			fields = append(fields, zap.Any(key, ctx[i+1]))
		}
	}
	return fields
}
