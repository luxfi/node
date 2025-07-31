// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"go.uber.org/zap"
	"github.com/luxfi/log"
)

// GetZapLogger extracts the underlying *zap.Logger from a luxfi/log.Logger
// This is a temporary workaround for API compatibility issues
func GetZapLogger(logger log.Logger) *zap.Logger {
	// Try to extract the zap logger using reflection or type assertion
	// For now, return a new production logger as a fallback
	zapLogger, _ := zap.NewProduction()
	return zapLogger
}