// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package genesis builds X-Chain (XVM) genesis bytes from a small,
// caller-supplied descriptor. It exists so callers (e.g. the node's
// primary-network genesis builder) do not have to import the xvm
// package directly to construct primary-asset genesis blobs — the
// asset shape lives here, in the same module tree as the codec that
// serializes it.
//
// The package is intentionally narrow: it owns the
// AssetDescriptor + Holder + BuildBytes contract, and nothing else.
// Bech32 formatting, allocation sorting, memo composition, and the
// "is X-Chain opt-in for this network?" policy all stay in the
// caller — those are network-level concerns, not XVM concerns.
package genesis

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm"
)

// AssetDescriptor is the JSON-shaped primary-asset descriptor that
// callers carry on their network config (e.g. genesiscfg.XChainGenesis).
// The JSON tags mirror the existing on-disk xChainGenesis shard shape:
//
//	{"symbol":"LUX","name":"Lux","denomination":9}
type AssetDescriptor struct {
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	Denomination byte   `json:"denomination"`
}

// Holder is one (bech32-address, amount) initial fixed-cap holder.
// Address is the already-formatted bech32 string for the target
// network's HRP — this package does no HRP resolution.
type Holder struct {
	Amount  uint64
	Address string
}

// BuildBytes constructs the canonical XVM genesis bytes for a network
// whose primary asset is `asset`, with the supplied initial fixed-cap
// holders and an optional opaque memo. The asset is registered in the
// genesis under `asset.Symbol` (the same key callers use today).
//
// The returned bytes are stable across calls with identical inputs
// because xvm.NewGenesis sorts both the asset set and each asset's
// states deterministically.
func BuildBytes(
	networkID uint32,
	asset AssetDescriptor,
	holders []Holder,
	memo []byte,
) ([]byte, error) {
	def := xvm.GenesisAssetDefinition{
		Name:         asset.Name,
		Symbol:       asset.Symbol,
		Denomination: asset.Denomination,
		InitialState: xvm.AssetInitialState{},
		Memo:         memo,
	}
	for _, h := range holders {
		def.InitialState.FixedCap = append(def.InitialState.FixedCap, xvm.GenesisHolder{
			Amount:  h.Amount,
			Address: h.Address,
		})
	}

	g, err := xvm.NewGenesis(networkID, map[string]xvm.GenesisAssetDefinition{
		asset.Symbol: def,
	})
	if err != nil {
		return nil, err
	}
	b, err := g.Bytes()
	if err != nil {
		return nil, fmt.Errorf("couldn't serialize xvm genesis: %w", err)
	}
	return b, nil
}

// AssetIDFromBytes returns the runtime X-Chain native asset ID derived
// from the canonical XVM genesis bytes (the same blob BuildBytes
// produces). This is the value vm.initGenesis assigns to the first
// genesis CreateAssetTx — the ID under which the X-Chain's fee asset is
// indexed in state.
//
// Callers (genesis/builder, config.getGenesisData) use this to derive
// the X-Chain native asset ID from genesis content rather than
// constants.UTXOAssetIDFor(networkID). On sovereign L1s the two values
// differ: the constant is network-id-keyed and identical across every
// L1 sharing a primary-network ID, while the genesis-derived ID
// captures the asset's actual on-chain identity (different per L1).
func AssetIDFromBytes(genesisBytes []byte) (ids.ID, error) {
	return xvm.AssetIDFromGenesisBytes(genesisBytes)
}
