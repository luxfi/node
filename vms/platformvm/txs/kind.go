// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/luxfi/zap"

// kind is the 1-byte tx discriminator at object offset 0 of every P-chain tx
// buffer. It is the whole dispatch: Parse reads it and returns the typed
// accessor. There is no codec, no version, no slot map.
type kind uint8

// Every number here is the wire, so a slot that stops naming a type stays a
// hole rather than letting the ones after it shift down. Slot 0 names nothing
// so a zeroed buffer decodes to no type; slot 1 named a time-advance tx that no
// executor would run.
const (
	_ kind = iota
	_
	kindRewardValidator
	kindBase
	kindImport
	kindExport
	kindCreateNetwork
	kindCreateChain
	kindTransferChainOwnership
	kindRemoveChainValidator
	kindTransformChain
	kindAddValidator
	kindAddChainValidator
	kindAddDelegator
	kindAddPermissionlessValidator
	kindAddPermissionlessDelegator
	kindRegisterL1Validator
	kindSetL1ValidatorWeight
	kindIncreaseL1ValidatorBalance
	kindDisableL1Validator
	kindConvertNetwork // promote an existing network: inherited → sovereign, re-anchor parent (L2→L1, L3→L1)
)

// offKind is the fixed wire position of the discriminator (object offset 0).
const offKind = 0

// kindOf reads the discriminator from a parsed tx buffer.
func kindOf(msg *zap.Message) kind {
	return kind(msg.Root().Uint8(offKind))
}
