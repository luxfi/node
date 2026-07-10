// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/luxfi/zap"

// kind is the 1-byte tx discriminator at object offset 0 of every P-chain tx
// buffer. It is the whole dispatch: Parse reads it and returns the typed
// accessor. There is no codec, no version, no slot map.
type kind uint8

const (
	kindReserved kind = iota
	kindAdvanceTime
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
	kindConvertNetworkToL1
	kindCreateSovereignL1
	kindRegisterL1Validator
	kindSetL1ValidatorWeight
	kindIncreaseL1ValidatorBalance
	kindDisableL1Validator
)

// offKind is the fixed wire position of the discriminator (object offset 0).
const offKind = 0

// kindOf reads the discriminator from a parsed tx buffer.
func kindOf(msg *zap.Message) kind {
	return kind(msg.Root().Uint8(offKind))
}
