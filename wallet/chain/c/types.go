// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/ethereum/go-ethereum/common"
)

// Tx represents a transaction on the C-Chain
// TODO: Implement proper C-chain transaction types
type Tx struct {
	ID ids.ID
}

// UnsignedImportTx is an unsigned import transaction
// TODO: Implement proper import transaction
type UnsignedImportTx struct {
	BaseTx
	SourceChain ids.ID
	ImportedInputs []*lux.TransferableInput
	Outs []*EVMOutput
}

// EVMOutput represents an output on the C-chain
type EVMOutput struct {
	Address common.Address
	Amount  uint64
}

// EVMInput represents an input on the C-chain  
type EVMInput struct {
	Address common.Address
	Amount  uint64
	AssetID ids.ID
	Nonce   uint64
}

// UnsignedExportTx is an unsigned export transaction  
// TODO: Implement proper export transaction
type UnsignedExportTx struct {
	BaseTx
	DestinationChain ids.ID
	ExportedOutputs []*lux.TransferableOutput
	Ins []*EVMInput
}

// ID returns the transaction ID
func (tx *UnsignedExportTx) ID() ids.ID {
	// TODO: Implement proper ID calculation
	return ids.Empty
}

// UnsignedAtomicTx is the interface for unsigned atomic transactions
// TODO: Implement proper atomic transaction interface
type UnsignedAtomicTx interface {
	InputUTXOs() []ids.ID
}

// BaseTx contains common transaction fields
type BaseTx struct {
	NetworkID    uint32
	BlockchainID ids.ID
	Outs         []*lux.TransferableOutput
	Ins          []*lux.TransferableInput
	Memo         []byte
}

// Client represents the C-chain client interface
// TODO: Implement proper C-chain client
type Client interface {
	IssueTx(tx *Tx) (ids.ID, error)
}