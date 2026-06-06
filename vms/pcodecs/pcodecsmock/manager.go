// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pcodecsmock re-exports luxfi/codec/codecmock so node/vms test
// files can mock codec.Manager via pcodecsmock.NewManager(ctrl) without
// importing luxfi/codec directly. Wave 2D of the codec rip (#101).
package pcodecsmock

import (
	"go.uber.org/mock/gomock"

	"github.com/luxfi/codec/codecmock"
)

// Manager is the codecmock.Manager mock — a gomock-generated double for
// codec.Manager. Re-exported as pcodecsmock.Manager so test files don't
// pull in luxfi/codec/codecmock directly.
type Manager = codecmock.Manager

// NewManager builds a fresh Manager mock against the supplied controller.
// Mirrors codecmock.NewManager.
func NewManager(ctrl *gomock.Controller) *Manager {
	return codecmock.NewManager(ctrl)
}
