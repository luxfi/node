// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/keychain"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/wallet/chain/c"
	"github.com/luxfi/node/wallet/chain/p"
	"github.com/luxfi/node/wallet/chain/x"
	"github.com/luxfi/node/wallet/subnet/primary/common"

	pbuilder "github.com/luxfi/node/wallet/chain/p/builder"
	psigner "github.com/luxfi/node/wallet/chain/p/signer"
	pwallet "github.com/luxfi/node/wallet/chain/p/wallet"
	xbuilder "github.com/luxfi/node/wallet/chain/x/builder"
	xsigner "github.com/luxfi/node/wallet/chain/x/signer"
)

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
	// Subnet IDs that the wallet should know about to be able to generate
	// transactions.
	SubnetIDs []ids.ID // optional
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

	owners, err := platformvm.GetOwners(luxState.PClient, ctx, config.SubnetIDs, config.ValidationIDs)
	if err != nil {
		return nil, err
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, luxState.UTXOs)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(luxState.PClient, pBackend)
	pBuilder := pbuilder.New(luxAddrs, luxState.PCTX, pBackend)
	pSigner := psigner.New(luxKeychain, pBackend)

	xChainID := luxState.XCTX.BlockchainID
	xUTXOs := common.NewChainUTXOs(xChainID, luxState.UTXOs)
	xBackend := x.NewBackend(luxState.XCTX, xUTXOs)
	xBuilder := xbuilder.New(luxAddrs, luxState.XCTX, xBackend)
	xSigner := xsigner.New(luxKeychain, xBackend)

	cChainID := luxState.CCTX.BlockchainID
	cUTXOs := common.NewChainUTXOs(cChainID, luxState.UTXOs)
	cBackend := c.NewBackend(cUTXOs, ethState.Accounts)
	cBuilder := c.NewBuilder(luxAddrs, ethAddrs, luxState.CCTX, cBackend)
	cSigner := c.NewSigner(luxKeychain, ethKeychain, cBackend)

	return NewWallet(
		pwallet.New(pClient, pBuilder, pSigner),
		x.NewWallet(xBuilder, xSigner, luxState.XClient, xBackend),
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

	owners, err := platformvm.GetOwners(client, ctx, config.SubnetIDs, config.ValidationIDs)
	if err != nil {
		return nil, err
	}

	pUTXOs := common.NewChainUTXOs(constants.PlatformChainID, utxos)
	pBackend := pwallet.NewBackend(pUTXOs, owners)
	pClient := p.NewClient(client, pBackend)
	pBuilder := pbuilder.New(addrs, context, pBackend)
	pSigner := psigner.New(keychain, pBackend)
	return pwallet.New(pClient, pBuilder, pSigner), nil
}
