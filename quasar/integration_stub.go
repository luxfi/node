// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !integration
// +build !integration

package quasar

import "fmt"

// Engine represents the Lux consensus engine
// This is a stub implementation when the external consensus package is not available
type Engine struct{}

// NewEngine creates a new Lux consensus engine
func NewEngine(network string) (*Engine, error) {
	return nil, fmt.Errorf("consensus engine not available in this build")
}

// NewEngineWithParams creates a new engine with custom parameters
func NewEngineWithParams(params interface{}) (*Engine, error) {
	return nil, fmt.Errorf("consensus engine not available in this build")
}

// NewTestEngine creates a new engine for testing
func NewTestEngine(params interface{}) (*Engine, error) {
	return nil, fmt.Errorf("consensus engine not available in this build")
}
