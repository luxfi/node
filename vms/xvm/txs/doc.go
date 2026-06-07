// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txs is the X-chain (xvm) transaction package for the
// luxfi/node binary. It owns the parser, signed-tx envelope, and
// per-fx Initial-State / Operation accessors that the X-VM uses to
// build, sign, and parse on-chain transactions.
//
// The wire shapes (BaseTx, CreateAssetTx, OperationTx, ImportTx,
// ExportTx, InitialState) are declared by the canonical schema at
// github.com/luxfi/proto/schemas/xvm/txs.zap. Hand-rolled accessor
// files in this package are the current source of truth for offsets;
// the schema-driven *_zap.go output replaces them incrementally.
//
// Regenerate the *_zap.go files after editing the schema:
//
//	go generate ./...
//
// See proto/schemas/README.md for the schema-layout convention.
package txs

//go:generate zapgen ../../../../proto/schemas/xvm/txs.zap
