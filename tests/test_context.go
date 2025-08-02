// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"time"

	"github.com/stretchr/testify/require"

	log "github.com/luxfi/log"
)

type TestContext interface {
	// Ensures the context can be used to instantiate a require instance
	require.TestingT

	// Ensures compatibility with ginkgo.By
	By(text string, callback ...func())

	// Provides a simple alternative to ginkgo.DeferCleanup
	DeferCleanup(cleanup func())

	// Returns a logger that can be used to log test output
	Log() log.Logger

	// Context helpers requiring cleanup with DeferCleanup
	ContextWithTimeout(duration time.Duration) context.Context
	DefaultContext() context.Context

	// Ensures compatibility with require.Eventually
	Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string)
}
