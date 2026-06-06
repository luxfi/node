// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"cmp"
	"fmt"

	"github.com/luxfi/address"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"
)

// Genesis represents the genesis state of the XVM
type Genesis struct {
	Txs []*GenesisAsset `serialize:"true"`
}

// GenesisAsset represents an asset in the genesis block
type GenesisAsset struct {
	Alias             string `serialize:"true"`
	txs.CreateAssetTx `serialize:"true"`
}

// Compare implements utils.Sortable for GenesisAsset
func (g *GenesisAsset) Compare(other *GenesisAsset) int {
	return cmp.Compare(g.Alias, other.Alias)
}

// AssetInitialState describes the initial state of an asset
type AssetInitialState struct {
	FixedCap    []GenesisHolder
	VariableCap []GenesisOwners
}

// GenesisAssetDefinition describes a genesis asset and its initial state
type GenesisAssetDefinition struct {
	Name         string
	Symbol       string
	Denomination byte
	InitialState AssetInitialState
	Memo         []byte
}

// GenesisHolder describes how much asset is owned by an address
type GenesisHolder struct {
	Amount  uint64
	Address string
}

// GenesisOwners describes who can perform an action
type GenesisOwners struct {
	Threshold uint32
	Minters   []string
}

// NewGenesis creates a new Genesis from genesis data
func NewGenesis(
	networkID uint32,
	genesisData map[string]GenesisAssetDefinition,
) (*Genesis, error) {
	g := &Genesis{}
	for assetAlias, assetDefinition := range genesisData {
		asset := GenesisAsset{
			Alias: assetAlias,
			CreateAssetTx: txs.CreateAssetTx{
				BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
					NetworkID:    networkID,
					BlockchainID: ids.Empty,
					Memo:         assetDefinition.Memo,
				}},
				Name:         assetDefinition.Name,
				Symbol:       assetDefinition.Symbol,
				Denomination: assetDefinition.Denomination,
			},
		}

		initialState := &txs.InitialState{
			FxIndex: 0, // secp256k1fx
		}
		for _, holder := range assetDefinition.InitialState.FixedCap {
			_, addrbuff, err := address.ParseBech32(holder.Address)
			if err != nil {
				return nil, fmt.Errorf("problem parsing holder address: %w", err)
			}
			addr, err := ids.ToShortID(addrbuff)
			if err != nil {
				return nil, fmt.Errorf("problem parsing holder address: %w", err)
			}
			initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
				Amt: holder.Amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addr},
				},
			})
		}
		for _, owners := range assetDefinition.InitialState.VariableCap {
			out := &secp256k1fx.MintOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: owners.Threshold,
				},
			}
			for _, addrStr := range owners.Minters {
				_, addrBytes, err := address.ParseBech32(addrStr)
				if err != nil {
					return nil, fmt.Errorf("problem parsing minters address: %w", err)
				}
				addr, err := ids.ToShortID(addrBytes)
				if err != nil {
					return nil, fmt.Errorf("problem parsing minters address: %w", err)
				}
				out.Addrs = append(out.Addrs, addr)
			}
			out.Sort()

			initialState.Outs = append(initialState.Outs, out)
		}

		if len(initialState.Outs) > 0 {
			codec, err := newGenesisCodec()
			if err != nil {
				return nil, err
			}
			initialState.Sort(codec)
			asset.States = append(asset.States, initialState)
		}

		utils.Sort(asset.States)
		g.Txs = append(g.Txs, &asset)
	}
	utils.Sort(g.Txs)

	return g, nil
}

// Bytes serializes the Genesis to bytes using the XVM genesis codec
func (g *Genesis) Bytes() ([]byte, error) {
	codec, err := newGenesisCodec()
	if err != nil {
		return nil, err
	}
	return codec.Marshal(txs.CodecVersion, g)
}

func newGenesisCodec() (pcodecs.Manager, error) {
	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
			&mldsafx.Fx{},
			&slhdsafx.Fx{},
			&ed25519fx.Fx{},
			&secp256r1fx.Fx{},
			&schnorrfx.Fx{},
			&bls12381fx.Fx{},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("problem creating parser: %w", err)
	}
	return parser.GenesisCodec(), nil
}

// ParseGenesisBytes decodes the canonical XVM genesis bytes produced by
// (*Genesis).Bytes() back into a *Genesis with each GenesisAsset's
// embedded CreateAssetTx initialised against the genesis codec. After
// Initialize, each tx's deterministic ID (tx.ID()) is the runtime asset
// ID of that genesis-minted asset — i.e. the same value vm.initGenesis
// computes when bootstrapping the X-Chain.
//
// Callers (genesis/builder, config/getGenesisData) use this to derive
// the X-Chain native asset ID from genesis content rather than the
// network-id-keyed constants.UTXOAssetIDFor(networkID). On sovereign
// L1s those two values DIFFER — the wallet
// builder context's UTXOAssetID must be the genesis-derived one or every
// fee-paying tx fails with "insufficient funds, needs N more nLUX".
func ParseGenesisBytes(genesisBytes []byte) (*Genesis, error) {
	codec, err := newGenesisCodec()
	if err != nil {
		return nil, err
	}
	g := &Genesis{}
	if _, err := codec.Unmarshal(genesisBytes, g); err != nil {
		return nil, fmt.Errorf("unmarshal xvm genesis: %w", err)
	}
	for i := range g.Txs {
		tx := &txs.Tx{Unsigned: &g.Txs[i].CreateAssetTx}
		if err := tx.Initialize(codec); err != nil {
			return nil, fmt.Errorf("initialize genesis asset %d (%s): %w", i, g.Txs[i].Alias, err)
		}
	}
	return g, nil
}

// AssetIDFromGenesisBytes returns the first genesis asset's runtime
// asset ID — the ID vm.initGenesis assigns to genesis.Txs[0]. This is
// the X-Chain native fee asset by convention (the same asset the
// platform-vm reports via platform.getStakingAssetID and the wallet
// builder context's UTXOAssetID).
//
// Returns an error when genesisBytes is malformed or contains zero
// assets — both are unrecoverable on a primary-network bootstrap.
func AssetIDFromGenesisBytes(genesisBytes []byte) (ids.ID, error) {
	g, err := ParseGenesisBytes(genesisBytes)
	if err != nil {
		return ids.Empty, err
	}
	if len(g.Txs) == 0 {
		return ids.Empty, fmt.Errorf("xvm genesis has zero asset txs")
	}
	tx := &txs.Tx{Unsigned: &g.Txs[0].CreateAssetTx}
	codec, err := newGenesisCodec()
	if err != nil {
		return ids.Empty, err
	}
	if err := tx.Initialize(codec); err != nil {
		return ids.Empty, fmt.Errorf("initialize first genesis asset (%s): %w", g.Txs[0].Alias, err)
	}
	return tx.ID(), nil
}
