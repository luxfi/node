// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLog(t *testing.T) {
	// Test that logger can be created and used without panicking
	log := NewLogger("", NewWrappedCore(Info, Discard, Plain.ConsoleEncoder()))

	// Test various log levels
	log.Debug("debug message", zap.String("key", "value"))
	log.Info("info message", zap.String("key", "value"))
	log.Warn("warn message", zap.String("key", "value"))
	log.Error("error message", zap.String("key", "value"))
	
	// Test that we got here without panicking
	require.NotNil(t, log)
}
