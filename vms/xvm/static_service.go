// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/go-json-experiment/json"
	jsonv1 "github.com/go-json-experiment/json/v1"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/luxfi/address"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"

	avajson "github.com/luxfi/node/utils/json"
)

var (
	errUnknownAssetType = errors.New("unknown asset type")

	_ lux.TransferableIn  = (*secp256k1fx.TransferInput)(nil)
	_ verify.State        = (*secp256k1fx.MintOutput)(nil)
	_ lux.TransferableOut = (*secp256k1fx.TransferOutput)(nil)
	_ fxs.FxOperation     = (*secp256k1fx.MintOperation)(nil)
	_ verify.Verifiable   = (*secp256k1fx.Credential)(nil)

	_ verify.State      = (*nftfx.MintOutput)(nil)
	_ verify.State      = (*nftfx.TransferOutput)(nil)
	_ fxs.FxOperation   = (*nftfx.MintOperation)(nil)
	_ fxs.FxOperation   = (*nftfx.TransferOperation)(nil)
	_ verify.Verifiable = (*nftfx.Credential)(nil)

	_ verify.State      = (*propertyfx.MintOutput)(nil)
	_ verify.State      = (*propertyfx.OwnedOutput)(nil)
	_ fxs.FxOperation   = (*propertyfx.MintOperation)(nil)
	_ fxs.FxOperation   = (*propertyfx.BurnOperation)(nil)
	_ verify.Verifiable = (*propertyfx.Credential)(nil)
)

// StaticService defines the base service for the asset vm
type StaticService struct{}

func CreateStaticService() *StaticService {
	return &StaticService{}
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	NetworkID   avajson.Uint32      `json:"networkID"`
	GenesisData Assets              `json:"genesisData"`
	Encoding    formatting.Encoding `json:"encoding"`
}

// Assets is the set of assets a genesis defines. The wire spells it as an
// object keyed by the alias each asset is known by; here it is a list, because
// a field on the plane is an offset and a map has no fixed layout. The alias
// lives inside each entry, so nothing is lost, and the entries are ordered by
// it so the same request always encodes to the same bytes.
type Assets []AssetDefinition

func (a Assets) MarshalJSON() ([]byte, error) {
	if a == nil {
		return []byte(avajson.Null), nil
	}
	m := make(map[string]AssetDefinition, len(a))
	for _, def := range a {
		m[def.Alias] = def
	}
	return json.Marshal(m, jsonv1.DefaultOptionsV1())
}

func (a *Assets) UnmarshalJSON(b []byte) error {
	var m map[string]AssetDefinition
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m == nil {
		*a = nil
		return nil
	}
	out := make(Assets, 0, len(m))
	for alias, def := range m {
		def.Alias = alias
		out = append(out, def)
	}
	slices.SortFunc(out, func(x, y AssetDefinition) int { return cmp.Compare(x.Alias, y.Alias) })
	*a = out
	return nil
}

type AssetDefinition struct {
	// Alias is the name the asset is known by. On the wire it is the key the
	// definition hangs under rather than one of its fields, which is what
	// `json:"-"` says; Assets supplies it.
	Alias        string              `json:"-"`
	Name         string              `json:"name"`
	Symbol       string              `json:"symbol"`
	Denomination avajson.Uint8       `json:"denomination"`
	InitialState InitialState   `json:"initialState"`
	Memo         string              `json:"memo"`
}

// InitialState is what an asset holds at genesis.
//
// The wire spells it as an object, but its keys are not data: fixedCap carries
// holders, variableCap carries minters, and any other key has always been
// refused. A closed vocabulary of two is a struct with two fields, which is
// also what has a layout — and it removes the round trip through `any` that
// re-marshalled every element just to read it back into the type it already
// was.
type InitialState struct {
	FixedCap    []Holder `json:"fixedCap,omitempty"`
	VariableCap []Owners `json:"variableCap,omitempty"`
}

func (s InitialState) empty() bool {
	return len(s.FixedCap) == 0 && len(s.VariableCap) == 0
}

// UnmarshalJSON refuses a key outside the vocabulary, which is where a
// malformed request belongs — the alternative is a genesis quietly built
// without the state that was asked for.
func (s *InitialState) UnmarshalJSON(b []byte) error {
	var raw map[string]jsontext.Value
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for kind, states := range raw {
		var err error
		switch kind {
		case "fixedCap":
			err = json.Unmarshal(states, &s.FixedCap)
		case "variableCap":
			err = json.Unmarshal(states, &s.VariableCap)
		default:
			return fmt.Errorf("%w: %q", errUnknownAssetType, kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes    string              `json:"bytes"`
	Encoding formatting.Encoding `json:"encoding"`
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (*StaticService) BuildGenesis(_ *http.Request, args *BuildGenesisArgs, reply *BuildGenesisReply) error {
	// Validate the fx set is well-formed (native genesis marshals without a
	// codec; the parser is only constructed here to reject a bad fx set).
	if _, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
			&nftfx.Fx{},
			&propertyfx.Fx{},
		},
	); err != nil {
		return err
	}

	g := Genesis{}
	for _, assetDefinition := range args.GenesisData {
		assetMemo, err := formatting.Decode(args.Encoding, assetDefinition.Memo)
		if err != nil {
			return fmt.Errorf("problem formatting asset definition memo due to: %w", err)
		}
		asset := GenesisAsset{
			Alias: assetDefinition.Alias,
			CreateAssetTx: txs.CreateAssetTx{
				BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
					NetworkID:    uint32(args.NetworkID),
					BlockchainID: ids.Empty,
					Memo:         assetMemo,
				}},
				Name:         assetDefinition.Name,
				Symbol:       assetDefinition.Symbol,
				Denomination: byte(assetDefinition.Denomination),
			},
		}
		if !assetDefinition.InitialState.empty() {
			initialState := &txs.InitialState{
				FxIndex: 0, // Implementation note
			}
			for _, holder := range assetDefinition.InitialState.FixedCap {
				_, addrbuff, err := address.ParseBech32(holder.Address)
				if err != nil {
					return fmt.Errorf("problem parsing holder address: %w", err)
				}
				addr, err := ids.ToShortID(addrbuff)
				if err != nil {
					return fmt.Errorf("problem parsing holder address: %w", err)
				}
				initialState.Outs = append(initialState.Outs, &secp256k1fx.TransferOutput{
					Amt: uint64(holder.Amount),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{addr},
					},
				})
			}
			for _, owners := range assetDefinition.InitialState.VariableCap {
				out := &secp256k1fx.MintOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
					},
				}
				for _, addrStr := range owners.Minters {
					_, addrBytes, err := address.ParseBech32(addrStr)
					if err != nil {
						return fmt.Errorf("problem parsing minters address: %w", err)
					}
					addr, err := ids.ToShortID(addrBytes)
					if err != nil {
						return fmt.Errorf("problem parsing minters address: %w", err)
					}
					out.Addrs = append(out.Addrs, addr)
				}
				out.Sort()

				initialState.Outs = append(initialState.Outs, out)
			}
			initialState.Sort()
			asset.States = append(asset.States, initialState)
		}
		utils.Sort(asset.States)
		g.Txs = append(g.Txs, &asset)
	}
	utils.Sort(g.Txs)

	b, err := g.Bytes()
	if err != nil {
		return fmt.Errorf("problem marshaling genesis: %w", err)
	}

	reply.Bytes, err = formatting.Encode(args.Encoding, b)
	if err != nil {
		return fmt.Errorf("couldn't encode genesis as string: %w", err)
	}
	reply.Encoding = args.Encoding
	return nil
}
