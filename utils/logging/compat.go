// Package logging compatibility layer - this is temporary for gradual migration
package logging

import (
	luxlog "github.com/luxfi/log"
)

// This file provides type aliases for gradual migration to luxfi/log
// The actual implementations remain in the existing files for now

// Re-export some types from luxfi/log for easier migration
var (
	// Re-export functions for compatibility
	_ = luxlog.NewLogger
)
