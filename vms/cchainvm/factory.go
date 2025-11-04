// (c) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchainvm

import (
	"log/slog"
	"os"

	"github.com/luxfi/log"
	"github.com/luxfi/node/vms"
)

var _ vms.Factory = (*Factory)(nil)

// Factory creates new instances of the C-Chain VM
type Factory struct{}

// New creates a new C-Chain VM instance
func (f *Factory) New(logger log.Logger) (interface{}, error) {
	// Convert log.Logger to slog.Logger
	// For now, create a basic slog logger
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slogLogger := slog.New(handler)

	return &VM{
		log: slogLogger.With("module", "cchainvm"),
	}, nil
}
