// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import "errors"

// TxKind is the 1-byte tx-type discriminator stored at offset 0 of every
// zap_native tx's fixed section. Wrap*Tx functions verify the kind matches
// the expected value before returning a typed accessor; constructors write
// the kind unconditionally. This closes the cross-tx-type confusion surface
// where an AdvanceTimeTx buffer could be Wrap'd as a BaseTx and return
// garbage-but-deterministic field reads.
//
// Schema v3 lays out every fixed section with TxKind at offset 0; all other
// fields shift by +1 byte vs v2. TxKind values are dense, never reused, and
// 0 is reserved (rejected by Wrap*).
type TxKind uint8

const (
	TxKindReserved                   TxKind = 0
	TxKindAdvanceTime                TxKind = 1
	TxKindRewardValidator            TxKind = 2
	TxKindSetL1ValidatorWeight       TxKind = 3
	TxKindIncreaseL1ValidatorBalance TxKind = 4
	TxKindDisableL1Validator         TxKind = 5
	TxKindBase                       TxKind = 6
	TxKindRegisterL1Validator        TxKind = 7
	TxKindSlashValidator             TxKind = 8
	TxKindTransferChainOwnership     TxKind = 9
	TxKindRemoveChainValidator       TxKind = 10
	// Batch 3 — tx types that compose batch-3 list/object primitives:
	TxKindBaseFull                   TxKind = 11 // BaseTx with Outs+Ins+Credentials
	TxKindAddPermissionlessValidator TxKind = 12
	TxKindImport                     TxKind = 13
	TxKindExport                     TxKind = 14
	TxKindCreateChain                TxKind = 15
)

// OffsetTxKind is the fixed wire position of the discriminator. Every
// zap_native fixed section reserves byte 0 for TxKind.
const OffsetTxKind = 0

// ErrWrongTxKind is returned by Wrap*Tx when the buffer's TxKind discriminator
// does not match the expected tx type. Caller passed the wrong buffer to the
// wrong wrapper — a cross-type confusion attempt or a dispatch bug.
var ErrWrongTxKind = errors.New("zap_native: tx kind discriminator does not match expected tx type")
