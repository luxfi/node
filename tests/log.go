// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"github.com/luxfi/log"
)

func NewDefaultLogger(prefix string) log.Logger {
	// Create a logger with default settings
	return log.NewLogger(prefix)
}

// LoggerForFormat creates a logger with the specified format
func LoggerForFormat(prefix string, rawLogFormat string) (log.Logger, error) {
	// For now, just return a default logger since the logging package
	// has changed its API
	return log.NewLogger(prefix), nil
}
