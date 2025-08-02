// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package bitcoin provides Bitcoin wallet functionality for the bridge chain
package bitcoin

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utxo"
)

// Wallet provides Bitcoin wallet functionality
type Wallet interface {
	// GetAddress returns the Bitcoin address for this wallet
	GetAddress() string

	// GetBalance returns the balance for a specific asset (BTC)
	GetBalance(ctx context.Context) (*big.Int, error)

	// CreateTransaction creates a new Bitcoin transaction
	CreateTransaction(ctx context.Context, to string, amount *big.Int) (*Transaction, error)

	// SignTransaction signs a transaction
	SignTransaction(ctx context.Context, tx *Transaction) (*SignedTransaction, error)

	// BroadcastTransaction broadcasts a signed transaction
	BroadcastTransaction(ctx context.Context, tx *SignedTransaction) (ids.ID, error)
}

// Transaction represents an unsigned Bitcoin transaction
type Transaction struct {
	Version  int32
	Inputs   []Input
	Outputs  []Output
	Locktime uint32
}

// Input represents a transaction input
type Input struct {
	PreviousTxID ids.ID
	OutputIndex  uint32
	ScriptSig    []byte
	Sequence     uint32
}

// Output represents a transaction output
type Output struct {
	Value        int64
	ScriptPubKey []byte
}

// SignedTransaction represents a signed Bitcoin transaction
type SignedTransaction struct {
	Transaction
	Witnesses [][]byte
}

// wallet implements the Wallet interface
type wallet struct {
	privateKey *ecdsa.PrivateKey
	address    string
	utxoMgr    utxo.Manager
	client     Client
}

// Client defines the interface for interacting with a Bitcoin node
type Client interface {
	GetUTXOs(ctx context.Context, address string) ([]*utxo.UTXO, error)
	BroadcastTransaction(ctx context.Context, txHex string) (string, error)
	GetTransaction(ctx context.Context, txID string) (*Transaction, error)
}

// NewWallet creates a new Bitcoin wallet
func NewWallet(privateKey *ecdsa.PrivateKey, client Client) (Wallet, error) {
	// TODO: Derive Bitcoin address from private key
	// This requires implementing Bitcoin address generation
	address := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa" // placeholder

	return &wallet{
		privateKey: privateKey,
		address:    address,
		utxoMgr:    utxo.NewBaseManager(),
		client:     client,
	}, nil
}

func (w *wallet) GetAddress() string {
	return w.address
}

func (w *wallet) GetBalance(ctx context.Context) (*big.Int, error) {
	// Bitcoin uses satoshis, we'll use the zero ID for BTC asset
	return w.utxoMgr.GetBalance(ctx, w.address, ids.Empty)
}

func (w *wallet) CreateTransaction(ctx context.Context, to string, amount *big.Int) (*Transaction, error) {
	// Select UTXOs to meet the amount
	selectedUTXOs, total, err := w.utxoMgr.SelectUTXOs(ctx, w.address, ids.Empty, amount)
	if err != nil {
		return nil, err
	}

	// Create transaction inputs
	inputs := make([]Input, len(selectedUTXOs))
	for i, utxo := range selectedUTXOs {
		inputs[i] = Input{
			PreviousTxID: utxo.TxID,
			OutputIndex:  utxo.OutputIndex,
			Sequence:     0xffffffff,
		}
	}

	// Create outputs
	outputs := []Output{
		{
			Value: amount.Int64(),
			// TODO: Generate scriptPubKey for recipient
		},
	}

	// Add change output if necessary
	change := new(big.Int).Sub(total, amount)
	if change.Sign() > 0 {
		outputs = append(outputs, Output{
			Value: change.Int64(),
			// TODO: Generate scriptPubKey for change
		})
	}

	return &Transaction{
		Version:  2,
		Inputs:   inputs,
		Outputs:  outputs,
		Locktime: 0,
	}, nil
}

func (w *wallet) SignTransaction(ctx context.Context, tx *Transaction) (*SignedTransaction, error) {
	// TODO: Implement Bitcoin transaction signing
	// This requires implementing Bitcoin signature generation
	return &SignedTransaction{
		Transaction: *tx,
		Witnesses:   make([][]byte, len(tx.Inputs)),
	}, nil
}

func (w *wallet) BroadcastTransaction(ctx context.Context, tx *SignedTransaction) (ids.ID, error) {
	// TODO: Serialize transaction to hex
	txHex := hex.EncodeToString([]byte{}) // placeholder

	txID, err := w.client.BroadcastTransaction(ctx, txHex)
	if err != nil {
		return ids.Empty, err
	}

	// Convert Bitcoin txID to ids.ID
	return ids.FromString(txID)
}
