// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	gethcommon "github.com/luxfi/geth/common"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/keychain"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/x"
	"github.com/luxfi/node/wallet/network/primary/common"
	"github.com/luxfi/utxo/secp256k1fx"

	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	psigner "github.com/luxfi/node/wallet/chain/p/signer"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
	xsigner "github.com/luxfi/node/wallet/chain/x/signer"
)

var _ Wallet = (*wallet)(nil)

// EVMKeychain is the canonical interface for keychains that expose
// 20-byte account addresses used by EVM-runtime chains (Lux C-Chain,
// , Hanzo EVM, and every EVM-compatible chain).
//
// Naming: the value IS "EVM-runtime account address". The internal
// derivation uses Keccak256 of the secp256k1 pubkey, but that's HOW
// the value is computed — WHAT the value is is determined by the data
// model that consumes it (EVM account). The relevant contrast in a
// multi-chain keychain is UTXO data model vs EVM data model — not
// "what hash produced it".
type EVMKeychain interface {
	GetByEVM(addr gethcommon.Address) (keychain.Signer, bool)
	EVMAddresses() set.Set[gethcommon.Address]
}

// KeccakKeychain is the deprecated alias of EVMKeychain.
//
// Deprecated: use EVMKeychain.
type KeccakKeychain interface {
	GetByKeccak(addr gethcommon.Address) (keychain.Signer, bool)
	KeccakAddresses() set.Set[gethcommon.Address]
}

// EthKeychain is the deprecated alias of EVMKeychain.
//
// Deprecated: use EVMKeychain.
type EthKeychain interface {
	GetEth(addr gethcommon.Address) (keychain.Signer, bool)
	EthAddresses() set.Set[gethcommon.Address]
}

// KeychainAdapter adapts secp256k1fx.Keychain to wallet/keychain.Keychain
// AND all three interfaces (canonical EVMKeychain, deprecated
// KeccakKeychain, deprecated EthKeychain). This lets secp256k1fx.Keychain
// be used with MakeWallet today while downstream callers migrate.
type KeychainAdapter struct {
	*secp256k1fx.Keychain
}

// Addresses implements wallet/keychain.Keychain
func (kc *KeychainAdapter) Addresses() set.Set[ids.ShortID] {
	return kc.Keychain.Addrs
}

// Get implements keychain.Keychain
func (kc *KeychainAdapter) Get(addr ids.ShortID) (keychain.Signer, bool) {
	return kc.Keychain.Get(addr)
}

// GetByEVM implements EVMKeychain — canonical name.
func (kc *KeychainAdapter) GetByEVM(addr gethcommon.Address) (keychain.Signer, bool) {
	signer, ok := kc.Keychain.GetByEVM(addr)
	if !ok {
		return nil, false
	}
	// secp256k1fx.luxSigner already implements wallet/keychain.Signer
	return signer.(keychain.Signer), true
}

// EVMAddresses implements EVMKeychain — canonical name.
func (kc *KeychainAdapter) EVMAddresses() set.Set[gethcommon.Address] {
	return kc.Keychain.EVMAddrs
}

// GetByKeccak implements KeccakKeychain.
//
// Deprecated: use GetByEVM.
func (kc *KeychainAdapter) GetByKeccak(addr gethcommon.Address) (keychain.Signer, bool) {
	return kc.GetByEVM(addr)
}

// KeccakAddresses implements KeccakKeychain.
//
// Deprecated: use EVMAddresses.
func (kc *KeychainAdapter) KeccakAddresses() set.Set[gethcommon.Address] {
	return kc.EVMAddresses()
}

// GetEth implements EthKeychain.
//
// Deprecated: use GetByEVM.
func (kc *KeychainAdapter) GetEth(addr gethcommon.Address) (keychain.Signer, bool) {
	return kc.GetByEVM(addr)
}

// EthAddresses implements EthKeychain.
//
// Deprecated: use EVMAddresses.
func (kc *KeychainAdapter) EthAddresses() set.Set[gethcommon.Address] {
	return kc.EVMAddresses()
}

// NewKeychainAdapter creates a KeychainAdapter from a secp256k1fx.Keychain
func NewKeychainAdapter(kc *secp256k1fx.Keychain) *KeychainAdapter {
	return &KeychainAdapter{Keychain: kc}
}

// Wallet provides chain wallets for the primary network.
// NOTE: C-Chain wallet is disabled - use github.com/luxfi/evm/ethclient directly
type Wallet interface {
	P() p.Wallet
	X() x.Wallet
}

type wallet struct {
	p p.Wallet
	x x.Wallet
}

func (w *wallet) P() p.Wallet {
	return w.p
}

func (w *wallet) X() x.Wallet {
	return w.x
}

// Creates a new default wallet
func NewWallet(pWallet p.Wallet, xWallet x.Wallet) Wallet {
	return &wallet{
		p: pWallet,
		x: xWallet,
	}
}

// Creates a Wallet with the given set of options
func NewWalletWithOptions(w Wallet, options ...common.Option) Wallet {
	return NewWallet(
		p.NewWalletWithOptions(w.P(), options...),
		x.NewWalletWithOptions(w.X(), options...),
	)
}

type WalletConfig struct {
	// Base URI to use for all node requests.
	URI string // required
	// Keys to use for signing all transactions.
	LUXKeychain keychain.Keychain // required
	EthKeychain EthKeychain       // optional - for future C-Chain support
	// Set of P-chain transactions that the wallet should know about to be able
	// to generate transactions.
	PChainTxs map[ids.ID]*txs.Tx // optional
	// Set of P-chain transactions that the wallet should fetch to be able to
	// generate transactions.
	PChainTxsToFetch set.Set[ids.ID] // optional
}

// MakeWallet returns a wallet that supports issuing transactions to the chains
// living in the primary network.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// transactions.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakeWallet(ctx context.Context, config *WalletConfig) (Wallet, error) {
	luxAddrs := config.LUXKeychain.Addresses()
	luxState, err := FetchState(ctx, config.URI, luxAddrs)
	if err != nil {
		return nil, err
	}

	// ethAddrs := config.EthKeychain.EthAddresses()
	// ethState, err := FetchEthState(ctx, config.URI, ethAddrs)
	// if err != nil {
	// 	return nil, err
	// }

	pChainTxs := config.PChainTxs
	if pChainTxs == nil {
		pChainTxs = make(map[ids.ID]*txs.Tx)
	}

	for txID := range config.PChainTxsToFetch {
		txBytes, err := luxState.PClient.GetTx(ctx, txID)
		if err != nil {
			return nil, err
		}
		tx, err := txs.Parse(txs.Codec, txBytes)
		if err != nil {
			return nil, err
		}
		pChainTxs[txID] = tx
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	pBackend := p.NewBackend(luxState.PCTX, pUTXOs, pChainTxs)
	pBuilder := pbuilder.New(luxAddrs, luxState.PCTX, pBackend)
	pSigner := psigner.New(config.LUXKeychain, pBackend)

	xChainID := luxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, luxState.UTXOs)
	xBackend := x.NewBackend(luxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(luxAddrs, luxState.XCTX, xBackend)
	xSigner := xsigner.New(config.LUXKeychain, xBackend)

	// cChainID := luxState.CCTX.BlockchainID
	// cUTXOs := common.NewChainUTXOs(cChainID, luxState.UTXOs)
	// cBackend := c.NewBackend(cUTXOs, ethState.Accounts)
	// cBuilder := c.NewBuilder(luxAddrs, ethAddrs, luxState.CCTX, cBackend)
	// cSigner := c.NewSigner(config.LUXKeychain, config.EthKeychain, cBackend)

	pClient := p.NewClient(luxState.PClient, pBackend)

	return NewWallet(
		p.NewWallet(pClient, pBuilder, pSigner),
		x.NewWallet(xBuilder, xSigner, xBackend),
	), nil
}
