// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// Logger defines the logging interface
type Logger interface {
	// Debug logs at debug level
	Debug(msg string, args ...interface{})

	// Info logs at info level
	Info(msg string, args ...interface{})

	// Warn logs at warn level
	Warn(msg string, args ...interface{})

	// Error logs at error level
	Error(msg string, args ...interface{})

	// Fatal logs at fatal level and exits
	Fatal(msg string, args ...interface{})

	// With returns a logger with additional context
	With(args ...interface{}) Logger

	// WithContext returns a logger with additional context
	WithContext(key string, value interface{}) Logger
}

// logger wraps slog.Logger
type logger struct {
	log *slog.Logger
}

// NewLogger creates a new logger
func NewLogger(name string) Logger {
	return &logger{
		log: slog.New(slog.NewTextHandler(os.Stdout, nil)).With("name", name),
	}
}

// NewLoggerWithOutput creates a new logger with custom output
func NewLoggerWithOutput(name string, w io.Writer) Logger {
	return &logger{
		log: slog.New(slog.NewTextHandler(w, nil)).With("name", name),
	}
}

// Debug implements Logger
func (l *logger) Debug(msg string, args ...interface{}) {
	l.log.Debug(msg, toSlogArgs(args)...)
}

// Info implements Logger
func (l *logger) Info(msg string, args ...interface{}) {
	l.log.Info(msg, toSlogArgs(args)...)
}

// Warn implements Logger
func (l *logger) Warn(msg string, args ...interface{}) {
	l.log.Warn(msg, toSlogArgs(args)...)
}

// Error implements Logger
func (l *logger) Error(msg string, args ...interface{}) {
	l.log.Error(msg, toSlogArgs(args)...)
}

// Fatal implements Logger
func (l *logger) Fatal(msg string, args ...interface{}) {
	l.log.Error(fmt.Sprintf("FATAL: %s", msg), toSlogArgs(args)...)
	os.Exit(1)
}

// With implements Logger
func (l *logger) With(args ...interface{}) Logger {
	return &logger{
		log: l.log.With(toSlogArgs(args)...),
	}
}

// WithContext implements Logger
func (l *logger) WithContext(key string, value interface{}) Logger {
	return &logger{
		log: l.log.With(key, value),
	}
}

// toSlogArgs converts variadic args to slog args
func toSlogArgs(args []interface{}) []interface{} {
	// Ensure even number of args for key-value pairs
	if len(args)%2 != 0 {
		args = append(args, "MISSING_VALUE")
	}
	return args
}

// NoLog is a logger that doesn't log anything
type NoLog struct{}

// Debug implements Logger
func (NoLog) Debug(string, ...interface{}) {}

// Info implements Logger
func (NoLog) Info(string, ...interface{}) {}

// Warn implements Logger
func (NoLog) Warn(string, ...interface{}) {}

// Error implements Logger
func (NoLog) Error(string, ...interface{}) {}

// Fatal implements Logger
func (NoLog) Fatal(string, ...interface{}) { os.Exit(1) }

// With implements Logger
func (n NoLog) With(...interface{}) Logger { return n }

// WithContext implements Logger
func (n NoLog) WithContext(string, interface{}) Logger { return n }
