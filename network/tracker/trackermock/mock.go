// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package trackermock provides mock implementations for testing
package trackermock

import (
	"github.com/golang/mock/gomock"
	"github.com/luxfi/node/network/tracker"
)

// NewTracker creates a new mock tracker
func NewTracker(ctrl *gomock.Controller) *tracker.MockTracker {
	return tracker.NewMockTracker(ctrl)
}

// NewTargeter creates a new mock targeter
func NewTargeter(ctrl *gomock.Controller) *tracker.MockTargeter {
	return tracker.NewMockTargeter(ctrl)
}