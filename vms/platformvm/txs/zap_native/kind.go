// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"

	"github.com/luxfi/zap"
)

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
	// Batch 4 — remaining P-chain tx types:
	TxKindAddValidator                TxKind = 16 // pre-Etna legacy validator add
	TxKindAddDelegator                TxKind = 17 // pre-Etna legacy delegator add
	TxKindAddPermissionlessDelegator  TxKind = 18 // Etna+ permissionless delegator
	TxKindAddChainValidator           TxKind = 19 // chain/subnet validator add (rebranded from AddSubnetValidator)
	TxKindCreateNetwork               TxKind = 20 // create a network (rebranded from CreateSubnet)
	TxKindTransformChain              TxKind = 21 // transform chain config (rebranded from TransformSubnet)
	TxKindConvertNetworkToL1          TxKind = 22 // convert network → L1 (rebranded from ConvertSubnetToL1)
	TxKindCreateSovereignL1           TxKind = 23 // create sovereign L1
)

// OffsetTxKind is the fixed wire position of the discriminator. Every
// zap_native fixed section reserves byte 0 for TxKind.
const OffsetTxKind = 0

// ErrWrongTxKind is returned by Wrap*Tx when the buffer's TxKind discriminator
// does not match the expected tx type. Caller passed the wrong buffer to the
// wrong wrapper — a cross-type confusion attempt or a dispatch bug.
var ErrWrongTxKind = errors.New("zap_native: tx kind discriminator does not match expected tx type")

// ErrWrongSchemaVersion is returned by Wrap*Tx when the buffer's ZAP wire
// header version is not Version2. v3 platformvm tx schemas live exclusively
// under Version2; v1-header buffers (legacy v2 platformvm schema) collide
// with the v3 TxKind discriminator (e.g. NetworkID=11 byte 0 == TxKindBaseFull)
// and must be rejected before any field interpretation. RED-MEDIUM-1
// (LP-023 v3.1 round 2).
var ErrWrongSchemaVersion = errors.New("zap_native: wire version does not match v3 schema (Version2)")

// parseAndCheckKind is the shared Wrap*Tx prologue. It parses the ZAP buffer,
// gates the wire-header version on Version2 (RED-MEDIUM-1), and confirms the
// TxKind discriminator at offset 0 matches the expected kind (V14
// cross-type confusion defense). Returns the parsed message + root object
// on success, or a typed error.
func parseAndCheckKind(b []byte, want TxKind) (*zap.Message, zap.Object, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, zap.Object{}, err
	}
	if msg.Version() != zap.Version2 {
		return nil, zap.Object{}, ErrWrongSchemaVersion
	}
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != want {
		return nil, zap.Object{}, ErrWrongTxKind
	}
	return msg, obj, nil
}
