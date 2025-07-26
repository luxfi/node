// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"errors"
	"math/big"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/codec/linearcodec"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/ethereum/go-ethereum/common"
)

// Tx represents a transaction on the C-Chain
// TODO: Implement proper C-chain transaction types
type Tx struct {
	ID ids.ID
	UnsignedAtomicTx UnsignedAtomicTx
	Creds []verify.Verifiable
	signedBytes []byte
}

// SignedBytes returns the signed bytes of the transaction
func (tx *Tx) SignedBytes() []byte {
	return tx.signedBytes
}

// UnsignedImportTx is an unsigned import transaction
// TODO: Implement proper import transaction
type UnsignedImportTx struct {
	BaseTx
	SourceChain ids.ID
	ImportedInputs []*lux.TransferableInput
	Outs []*EVMOutput
}

// InputUTXOs implements UnsignedAtomicTx
func (tx *UnsignedImportTx) InputUTXOs() []ids.ID {
	utxos := make([]ids.ID, len(tx.ImportedInputs))
	for i, input := range tx.ImportedInputs {
		utxos[i] = input.InputID()
	}
	return utxos
}

// GasUsed returns the gas used by this transaction
// TODO: Implement proper gas calculation
func (tx *UnsignedImportTx) GasUsed(fixedFee bool) (uint64, error) {
	return 100000, nil // placeholder gas value
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

// Compare implements utils.Sortable
func (e *EVMInput) Compare(other *EVMInput) int {
	addrCmp := e.Address.Cmp(other.Address)
	if addrCmp != 0 {
		return addrCmp
	}
	
	if e.Amount < other.Amount {
		return -1
	}
	if e.Amount > other.Amount {
		return 1
	}
	
	return e.AssetID.Compare(other.AssetID)
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

// InputUTXOs implements UnsignedAtomicTx
func (tx *UnsignedExportTx) InputUTXOs() []ids.ID {
	// Export transactions don't consume UTXOs directly
	return nil
}

// GasUsed returns the gas used by this transaction
// TODO: Implement proper gas calculation
func (tx *UnsignedExportTx) GasUsed(fixedFee bool) (uint64, error) {
	return 100000, nil // placeholder gas value
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
	GetAtomicTxStatus(txID ids.ID) (Status, error)
}

// Status represents the status of a transaction
type Status string

// Constants
const (
	codecVersion = 0
	EVMOutputGas = 100 // TODO: Set proper gas value
	EVMInputGas = 100  // TODO: Set proper gas value
	
	// Transaction status
	Accepted = "Accepted"
)

// Variables
var (
	// Codec is the codec used for serialization
	Codec codec.Manager
	
	// Errors
	errInsufficientFunds = errors.New("insufficient funds")
)

func init() {
	// Initialize codec
	// TODO: Register proper types
	Codec = codec.NewDefaultManager()
	lcodec := linearcodec.NewDefault()
	Codec.RegisterCodec(codecVersion, lcodec)
}

// CalculateDynamicFee calculates the dynamic fee
// TODO: Implement proper fee calculation
func CalculateDynamicFee(gasUsed uint64, baseFee *big.Int) (uint64, error) {
	fee := new(big.Int).Mul(baseFee, new(big.Int).SetUint64(gasUsed))
	if !fee.IsUint64() {
		return 0, errInsufficientFunds
	}
	return fee.Uint64(), nil
}

// Sign signs the transaction
// TODO: Implement proper signing
func (tx *Tx) Sign(codec codec.Manager, signers [][]*secp256k1.PrivateKey) error {
	return nil
}

// Initialize initializes the transaction
// TODO: Implement proper initialization
func (tx *Tx) Initialize(unsignedBytes, signedBytes []byte) {
	// Calculate transaction ID from signed bytes using hash
	tx.ID = ids.ID(hashing.ComputeHash256(signedBytes))
	tx.signedBytes = signedBytes
}

// UnsignedAtomicTx field for Tx
type UnsignedAtomicTxWrapper struct {
	UnsignedAtomicTx UnsignedAtomicTx
}

// errInsufficientFunds is defined in builder.go