// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	wkeychain "github.com/luxfi/keychain"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/wallet/chain/x/builder"
	"github.com/luxfi/node/wallet/chain/x/signer"
	"github.com/luxfi/node/wallet/network/primary/common"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/chains/atomic"
)

type Builder struct {
	utxos     *utxos
	ctx       *builder.Context
	networkID uint32
	chainID   ids.ID
}

func New(
	ctx context.Context,
	cfg *config.Config,
	feeAssetID ids.ID,
	state state.State,
	sharedMemory atomic.SharedMemory,
) *Builder {
	utxos := newUTXOs(ctx, state, sharedMemory)
	return &Builder{
		utxos:     utxos,
		ctx:       newContext(ctx, cfg, feeAssetID),
		networkID: 0, // Will be set from VM context
		chainID:   ids.Empty,
	}
}

// SetContextIDs sets the network ID and chain ID from the VM's Runtime
func (b *Builder) SetContextIDs(networkID uint32, chainID ids.ID) {
	b.networkID = networkID
	b.chainID = chainID
	// Update the builder context as well
	b.ctx.NetworkID = networkID
	b.ctx.BlockchainID = chainID
	// Update the utxos chain ID so it looks up UTXOs from the correct source
	b.utxos.SetChainID(chainID)
}

func (b *Builder) CreateAssetTx(
	name, symbol string,
	denomination byte,
	initialStates map[uint32][]verify.State,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	utx, err := xBuilder.NewCreateAssetTx(
		name,
		symbol,
		denomination,
		initialStates,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building base tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) BaseTx(
	outs []*lux.TransferableOutput,
	memo []byte,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	utx, err := xBuilder.NewBaseTx(
		outs,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building base tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) MintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	utx, err := xBuilder.NewOperationTxMintNFT(
		assetID,
		payload,
		owners,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting NFTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) MintFTs(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	utx, err := xBuilder.NewOperationTxMintFT(
		outputs,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed minting FTs: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) Operation(
	ops []*txs.Operation,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	utx, err := xBuilder.NewOperationTx(
		ops,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building operation tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) ImportTx(
	sourceChain ids.ID,
	to ids.ShortID,
	kc *secp256k1fx.Keychain,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	outOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{to},
	}

	utx, err := xBuilder.NewImportTx(
		sourceChain,
		outOwner,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

func (b *Builder) ExportTx(
	destinationChain ids.ID,
	to ids.ShortID,
	exportedAssetID ids.ID,
	exportedAmt uint64,
	kc *secp256k1fx.Keychain,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	xBuilder, xSigner := b.builders(kc)

	outputs := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: exportedAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: exportedAmt,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	utx, err := xBuilder.NewExportTx(
		destinationChain,
		outputs,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building export tx: %w", err)
	}

	return signer.SignUnsigned(context.Background(), xSigner, utx)
}

// keychainAdapter adapts secp256k1fx.Keychain (utils/crypto keychain) to wallet keychain
type keychainAdapter struct {
	kc *secp256k1fx.Keychain
}

func (k *keychainAdapter) Get(addr ids.ShortID) (wkeychain.Signer, bool) {
	utilsSigner, ok := k.kc.Get(addr)
	if !ok {
		return nil, false
	}
	return utilsSigner.(wkeychain.Signer), true
}

func (k *keychainAdapter) Addresses() set.Set[ids.ShortID] {
	return k.kc.Addresses()
}

func (b *Builder) builders(kc *secp256k1fx.Keychain) (builder.Builder, signer.Signer) {
	var (
		addrs = kc.Addresses()
		wa    = &walletUTXOsAdapter{
			utxos: b.utxos,
			addrs: addrs,
		}
		builder   = builder.New(addrs, b.ctx, wa)
		kcAdapter = &keychainAdapter{kc: kc}
		signer    = signer.New(kcAdapter, wa)
	)
	return builder, signer
}

// TransferNFT moves the NFT of [assetID] in [groupID] to [to], drawing the
// operation from whichever of [utxos] the keychain can spend.
func (b *Builder) TransferNFT(
	utxos []*lux.UTXO,
	kc *secp256k1fx.Keychain,
	assetID ids.ID,
	groupID uint32,
	to ids.ShortID,
	changeAddr ids.ShortID,
) (*txs.Tx, error) {
	now := uint64(time.Now().Unix())
	for _, utxo := range utxos {
		if utxo.AssetID() != assetID {
			continue
		}
		out, ok := utxo.Out.(*nftfx.TransferOutput)
		if !ok || out.GroupID != groupID {
			continue
		}
		indices, _, ok := kc.Match(&out.OutputOwners, now)
		if !ok {
			continue
		}
		return b.Operation([]*txs.Operation{{
			Asset:   utxo.Asset,
			UTXOIDs: []*lux.UTXOID{&utxo.UTXOID},
			Op: &nftfx.TransferOperation{
				Input: secp256k1fx.Input{SigIndices: indices},
				Output: nftfx.TransferOutput{
					GroupID: out.GroupID,
					Payload: out.Payload,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			},
		}}, kc, changeAddr)
	}
	return nil, fmt.Errorf("no spendable NFT of asset %s in group %d", assetID, groupID)
}
