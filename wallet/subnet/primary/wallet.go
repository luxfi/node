// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	gethcommon "github.com/luxfi/geth/common"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/keychain" // input keychain
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/c"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/x"
	walletkeychain "github.com/luxfi/node/wallet/keychain"
	"github.com/luxfi/node/wallet/net/primary/common"

	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	psigner "github.com/luxfi/node/wallet/chain/p/signer"
	pwallet "github.com/luxfi/node/wallet/chain/p/wallet"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
	xsigner "github.com/luxfi/node/wallet/chain/x/signer"
)

// EthKeychainAdapter adapts secp256k1fx.Keychain to c.EthKeychain interface.
// This allows secp256k1fx.Keychain to be used as the ethKeychain parameter in MakeWallet.
type EthKeychainAdapter struct {
	*secp256k1fx.Keychain
}

// GetEth implements c.EthKeychain by type-casting the signer
func (kc *EthKeychainAdapter) GetEth(addr gethcommon.Address) (walletkeychain.Signer, bool) {
	signer, ok := kc.Keychain.GetEth(addr)
	if !ok {
		return nil, false
	}
	// secp256k1fx.luxSigner already implements wallet/keychain.Signer
	return signer.(walletkeychain.Signer), true
}

// EthAddresses implements c.EthKeychain
func (kc *EthKeychainAdapter) EthAddresses() set.Set[gethcommon.Address] {
	return kc.Keychain.EthAddrs
}

// NewEthKeychainAdapter creates an EthKeychainAdapter from a secp256k1fx.Keychain
func NewEthKeychainAdapter(kc *secp256k1fx.Keychain) *EthKeychainAdapter {
	return &EthKeychainAdapter{Keychain: kc}
}

// keychainAdapter adapts utils/crypto keychain to wallet keychain interface
type keychainAdapter struct {
	keychain.Keychain
}

func (k *keychainAdapter) Get(addr ids.ShortID) (walletkeychain.Signer, bool) {
	utilsSigner, ok := k.Keychain.Get(addr)
	if !ok {
		return nil, false
	}
	return utilsSigner, true
}

// Wallet provides chain wallets for the primary network.
type Wallet struct {
	p pwallet.Wallet
	x x.Wallet
	c c.Wallet
}

func (w *Wallet) P() pwallet.Wallet {
	return w.p
}

func (w *Wallet) X() x.Wallet {
	return w.x
}

func (w *Wallet) C() c.Wallet {
	return w.c
}

// Creates a new default wallet
func NewWallet(p pwallet.Wallet, x x.Wallet, c c.Wallet) *Wallet {
	return &Wallet{
		p: p,
		x: x,
		c: c,
	}
}

// Creates a Wallet with the given set of options
func NewWalletWithOptions(w *Wallet, options ...common.Option) *Wallet {
	return NewWallet(
		pwallet.WithOptions(w.p, options...),
		x.NewWalletWithOptions(w.x, options...),
		c.NewWalletWithOptions(w.c, options...),
	)
}

type WalletConfig struct {
	// Net IDs that the wallet should know about to be able to generate
	// transactions.
	NetIDs []ids.ID // optional
	// Validation IDs that the wallet should know about to be able to generate
	// transactions.
	ValidationIDs []ids.ID // optional
}

// MakeWallet returns a wallet that supports issuing transactions to the chains
// living in the primary network.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// owners.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakeWallet(
	ctx context.Context,
	uri string,
	luxKeychain keychain.Keychain,
	ethKeychain c.EthKeychain,
	config WalletConfig,
) (*Wallet, error) {
	luxAddrs := luxKeychain.Addresses()
	luxState, err := FetchState(ctx, uri, luxAddrs)
	if err != nil {
		return nil, err
	}

	ethAddrs := ethKeychain.EthAddresses()
	ethState, err := FetchEthState(ctx, uri, ethAddrs)
	if err != nil {
		return nil, err
	}

	owners, err := platformvm.GetOwners(luxState.PClient, ctx, config.NetIDs, config.ValidationIDs)
	if err != nil {
		return nil, err
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(luxState.PClient, pBackend)
	pBuilder := pbuilder.New(luxAddrs, luxState.PCTX, pBackend)
	pSigner := psigner.New(&keychainAdapter{Keychain: luxKeychain}, pBackend)

	xChainID := luxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, luxState.UTXOs)
	xBackend := x.NewBackend(luxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(luxAddrs, luxState.XCTX, xBackend)
	xSigner := xsigner.New(&keychainAdapter{Keychain: luxKeychain}, xBackend)

	cChainID := luxState.CCTX.BlockchainID
	cUTXOs := common.NewChainUTXOs(cChainID, luxState.UTXOs)
	cBackend := c.NewBackend(cUTXOs, ethState.Accounts)
	cBuilder := c.NewBuilder(luxAddrs, ethAddrs, luxState.CCTX, cBackend)
	cSigner := c.NewSigner(&keychainAdapter{Keychain: luxKeychain}, ethKeychain, cBackend)

	return NewWallet(
		pwallet.New(pClient, pBuilder, pSigner),
		x.NewWallet(xBuilder, xSigner, xBackend),
		c.NewWallet(cBuilder, cSigner, luxState.CClient, ethState.Client, cBackend),
	), nil
}

// MakePWallet returns a P-chain wallet that supports issuing transactions.
//
// On creation, the wallet attaches to the provided uri and fetches all UTXOs
// that reference any of the provided keys. If the UTXOs are modified through an
// external issuance process, such as another instance of the wallet, the UTXOs
// may become out of sync. The wallet will also fetch all requested P-chain
// owners.
//
// The wallet manages all state locally, and performs all tx signing locally.
func MakePWallet(
	ctx context.Context,
	uri string,
	keychain keychain.Keychain,
	config WalletConfig,
) (pwallet.Wallet, error) {
	addrs := keychain.Addresses()
	client, context, utxos, err := FetchPState(ctx, uri, addrs)
	if err != nil {
		return nil, err
	}

	owners, err := platformvm.GetOwners(client, ctx, config.NetIDs, config.ValidationIDs)
	if err != nil {
		return nil, err
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(client, pBackend)
	pBuilder := pbuilder.New(addrs, context, pBackend)
	pSigner := psigner.New(&keychainAdapter{Keychain: keychain}, pBackend)
	return pwallet.New(pClient, pBuilder, pSigner), nil
}
