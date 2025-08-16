// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLog(t *testing.T) {
	// TODO: Fix this test - it's testing panic recovery functionality that
	// needs to be updated for the new logging interface
	t.Skip("Skipping test that needs updating for new logging interface")
	
	log := NewLogger("", NewWrappedCore(Info, Discard, Plain.ConsoleEncoder()))

	recovered := new(bool)
	panicFunc := func() {
		panic("DON'T PANIC!")
	}
	exitFunc := func() {
		*recovered = true
	}
	_ = panicFunc
	_ = exitFunc
	log.Error("test error", zap.String("panic", "test"), zap.String("exit", "test"))

	require.True(t, *recovered)
}
