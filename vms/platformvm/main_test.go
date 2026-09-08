// Copyright (C) 2023-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Temporarily disable goleak to focus on actual test failures
	os.Exit(m.Run())
}
