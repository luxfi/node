// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"context"
	"crypto/ecdsa"
	"math/big"

	"github.com/luxfi/ids"
)

// ChainType represents different blockchain architectures
type ChainType string

const (
	ChainTypeLuxPrimary ChainType = "lux-primary"    // P-Chain, X-Chain, C-Chain
	ChainTypeLuxL2      ChainType = "lux-l2"         // SubnetEVM
	ChainTypeOPStack    ChainType = "op-stack"       // OP Stack on Lux
	ChainTypeBasedRollup ChainType = "based-rollup"  // Based Rollup settling on Lux
	ChainTypeBitcoin    ChainType = "bitcoin"        // Bitcoin network
	ChainTypeXRPL       ChainType = "xrpl"           // XRP Ledger
	ChainTypeEVM        ChainType = "evm"            // Generic EVM chain
)

// WalletType represents the wallet model
type WalletType string

const (
	WalletTypeUTXO    WalletType = "utxo"
	WalletTypeAccount WalletType = "account"
)

// Wallet provides a unified interface for different blockchain wallets
type Wallet interface {
	// GetChainType returns the type of blockchain this wallet manages
	GetChainType() ChainType
	
	// GetWalletType returns whether this is UTXO or account-based
	GetWalletType() WalletType
	
	// GetAddress returns the primary address for this wallet
	GetAddress() string
	
	// GetBalance returns the balance for a specific asset
	GetBalance(ctx context.Context, assetID ids.ID) (*big.Int, error)
	
	// CreateTransaction creates a new transaction
	CreateTransaction(ctx context.Context, req *TransactionRequest) (UnsignedTransaction, error)
	
	// SignTransaction signs a transaction
	SignTransaction(ctx context.Context, tx UnsignedTransaction) (SignedTransaction, error)
	
	// BroadcastTransaction broadcasts a signed transaction
	BroadcastTransaction(ctx context.Context, tx SignedTransaction) (ids.ID, error)
	
	// GetTransactionStatus returns the status of a transaction
	GetTransactionStatus(ctx context.Context, txID ids.ID) (TransactionStatus, error)
}

// BridgeWallet extends Wallet with cross-chain capabilities
type BridgeWallet interface {
	Wallet
	
	// CreateBridgeTransaction creates a cross-chain transaction
	CreateBridgeTransaction(ctx context.Context, req *BridgeRequest) (UnsignedTransaction, error)
	
	// VerifyBridgeAttestation verifies a bridge attestation with Ringtail signatures
	VerifyBridgeAttestation(ctx context.Context, attestation *BridgeAttestation) error
}

// TransactionRequest represents a generic transaction request
type TransactionRequest struct {
	To          string
	AssetID     ids.ID
	Amount      *big.Int
	Fee         *big.Int
	Data        []byte        // For contract calls
	Memo        []byte        // For UTXO chains
	Extra       interface{}   // Chain-specific data
}

// BridgeRequest represents a cross-chain transaction request
type BridgeRequest struct {
	SourceChain      ChainType
	DestinationChain ChainType
	AssetID          ids.ID
	Amount           *big.Int
	Recipient        string
	Data             []byte
}

// UnsignedTransaction represents an unsigned transaction
type UnsignedTransaction interface {
	// GetID returns the transaction ID
	GetID() ids.ID
	
	// GetChainType returns the chain type
	GetChainType() ChainType
	
	// Bytes returns the serialized transaction
	Bytes() []byte
}

// SignedTransaction represents a signed transaction
type SignedTransaction interface {
	UnsignedTransaction
	
	// GetSignatures returns the signatures
	GetSignatures() [][]byte
	
	// GetRingtailSignature returns the Ringtail signature if present
	GetRingtailSignature() []byte
}

// TransactionStatus represents the status of a transaction
type TransactionStatus struct {
	ID          ids.ID
	Status      string // pending, confirmed, failed
	BlockNumber uint64
	BlockHash   ids.ID
	Timestamp   uint64
}

// BridgeAttestation represents a cross-chain message attestation
type BridgeAttestation struct {
	SourceChain      ChainType
	DestinationChain ChainType
	MessageID        ids.ID
	Payload          []byte
	Validators       []ids.NodeID
	Signatures       [][]byte          // Classical signatures
	RingtailSig      []byte           // Aggregated Ringtail signature
}

// KeyManager manages keys for different wallet types
type KeyManager interface {
	// GetPrivateKey returns the ECDSA private key
	GetPrivateKey() *ecdsa.PrivateKey
	
	// GetPublicKey returns the ECDSA public key
	GetPublicKey() *ecdsa.PublicKey
	
	// GetRingtailShare returns the Ringtail key share if available
	GetRingtailShare() []byte
	
	// Sign signs a message
	Sign(message []byte) ([]byte, error)
	
	// SignWithRingtail signs with Ringtail if available
	SignWithRingtail(message []byte) ([]byte, error)
}

// Factory creates wallets for different chain types
type Factory interface {
	// CreateWallet creates a wallet for the specified chain
	CreateWallet(ctx context.Context, chainType ChainType, keyManager KeyManager, config interface{}) (Wallet, error)
	
	// CreateBridgeWallet creates a bridge-enabled wallet
	CreateBridgeWallet(ctx context.Context, chainType ChainType, keyManager KeyManager, config interface{}) (BridgeWallet, error)
}