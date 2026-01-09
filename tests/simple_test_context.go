// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/log"
	"github.com/luxfi/node/wallet/net/primary/common"
)

const failNowMessage = "SimpleTestContext.FailNow called"

type SimpleTestContext struct {
	log           log.Logger
	cleanupFuncs  []func()
	cleanupCalled bool
	errorfHandler func(format string, args ...any)
	panicHandler  func(r any)
}

func NewTestContext(log log.Logger) *SimpleTestContext {
	return &SimpleTestContext{
		log: log,
	}
}

// NewTestContextWithArgs creates a test context with custom error and panic handlers.
// This is useful for integration with testing frameworks like Antithesis.
func NewTestContextWithArgs(
	_ context.Context,
	log log.Logger,
	errorfHandler func(format string, args ...any),
	panicHandler func(r any),
) *SimpleTestContext {
	return &SimpleTestContext{
		log:           log,
		errorfHandler: errorfHandler,
		panicHandler:  panicHandler,
	}
}

func (tc *SimpleTestContext) Errorf(format string, args ...interface{}) {
	if tc.errorfHandler != nil {
		tc.errorfHandler(format, args...)
	} else {
		tc.log.Error(fmt.Sprintf(format, args...))
	}
}

func (*SimpleTestContext) FailNow() {
	panic(failNowMessage)
}

// Recover should be deferred to recover from panics and call custom panic handler if set.
func (tc *SimpleTestContext) Recover() {
	if r := recover(); r != nil {
		if tc.panicHandler != nil {
			tc.panicHandler(r)
		} else {
			// Re-panic if no custom handler is set
			panic(r)
		}
	}
}

// Cleanup is intended to be deferred by the caller to ensure cleanup is performed even
// in the event that a panic occurs.
func (tc *SimpleTestContext) Cleanup() {
	if tc.cleanupCalled {
		return
	}
	tc.cleanupCalled = true

	// Only exit non-zero if a cleanup caused a panic
	exitNonZero := false

	var panicData any
	if r := recover(); r != nil {
		// Call custom panic handler if set
		if tc.panicHandler != nil {
			tc.panicHandler(r)
		}

		errorString, ok := r.(string)
		if !ok || errorString != failNowMessage {
			// Retain the panic data to raise after cleanup
			panicData = r
		} else {
			exitNonZero = true
		}
	}

	for _, cleanupFunc := range tc.cleanupFuncs {
		func() {
			// Ensure a failed cleanup doesn't prevent subsequent cleanup functions from running
			defer func() {
				if r := recover(); r != nil {
					exitNonZero = true
					fmt.Println("Recovered from panic during cleanup:", r)
				}
			}()
			cleanupFunc()
		}()
	}

	if panicData != nil {
		panic(panicData)
	}
	if exitNonZero {
		os.Exit(1)
	}
}

func (tc *SimpleTestContext) DeferCleanup(cleanup func()) {
	tc.cleanupFuncs = append(tc.cleanupFuncs, cleanup)
}

func (tc *SimpleTestContext) By(_ string, _ ...func()) {
	tc.Errorf("By not yet implemented")
	tc.FailNow()
}

func (tc *SimpleTestContext) Log() log.Logger {
	return tc.log
}

// Helper simplifying use of a timed context by canceling the context on ginkgo teardown.
func (tc *SimpleTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	return ContextWithTimeout(tc, duration)
}

// Helper simplifying use of a timed context configured with the default timeout.
func (tc *SimpleTestContext) DefaultContext() context.Context {
	return DefaultContext(tc)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func (tc *SimpleTestContext) WithDefaultContext() common.Option {
	return WithDefaultContext(tc)
}

func (tc *SimpleTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	require.Eventually(tc, condition, waitFor, tick, msg)
}
