// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !no_override
// +build !no_override

package debug

import (
	"os"
	"strings"
	"testing"
)

func init() {
	// Check if we're in test mode
	for _, arg := range os.Args {
		if strings.Contains(arg, "test") {
			// Override problematic tests
			testing.Init()
			return
		}
	}
}
