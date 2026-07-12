// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txs is the X-chain (xvm) transaction package for the luxfi/node
// binary. It owns the native-ZAP struct-is-wire tx codec: the signed-tx
// envelope (wire.SignedTx), the per-type unsigned bodies keyed by a 1-byte
// xkind discriminator, and the per-fx Initial-State / Operation accessors the
// X-VM uses to build, sign, and parse on-chain transactions.
//
// There is no pcodecs.Manager, no linearcodec, and no reflection-driven slot
// map. Each tx type serializes itself directly onto a github.com/luxfi/zap
// buffer; polymorphic fx primitives (outputs, inputs, operations, credentials)
// name themselves on the wire via their (TypeKind, ShapeKind) envelope and are
// reconstructed by envelope dispatch (fxwire.go).
package txs
