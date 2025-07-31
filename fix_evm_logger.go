// +build ignore

// This file documents the fix needed for the EVM logger compatibility issue.
// The issue is in github.com/luxfi/evm@v0.8.2/plugin/evm/vm_database.go:187
// where it passes a log.Logger to factory.New which expects *zap.Logger.
//
// The fix would be to either:
// 1. Update the EVM module to convert the logger properly
// 2. Update the database factory to accept log.Logger interface
// 3. Use a build tag to exclude the EVM module temporarily