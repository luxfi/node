// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package builder provides genesis byte generation for Lux networks.
// This package depends on node types and is responsible for building
// the actual genesis state from genesis config.
package builder

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/go-json-experiment/json"
	"github.com/luxfi/node/utils/units"
	"path"
	"time"

	"github.com/luxfi/address"
	"github.com/luxfi/constants"
	"github.com/luxfi/container/sampler"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	pchaintxs "github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
	xvmgenesis "github.com/luxfi/node/vms/xvm/genesis"
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"

	genesisconfigs "github.com/luxfi/genesis/configs"
	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
)

// Chain alias vars are derived from the single source of truth (Registry
// in registry.go). Rebranding a chain — adding/removing aliases or
// renaming the letter — edits one row in Registry; these vars and the
// VMAliases map re-derive at package init.
var (
	PChainAliases = AliasesFor("P")
	XChainAliases = AliasesFor("X")
	CChainAliases = AliasesFor("C")
	DChainAliases = AliasesFor("D")
	QChainAliases = AliasesFor("Q")
	AChainAliases = AliasesFor("A")
	BChainAliases = AliasesFor("B")
	MChainAliases = AliasesFor("M")
	FChainAliases = AliasesFor("F")
	ZChainAliases = AliasesFor("Z")
	GChainAliases = AliasesFor("G")
	KChainAliases = AliasesFor("K")
	IChainAliases = AliasesFor("I")
	OChainAliases = AliasesFor("O")
	RChainAliases = AliasesFor("R")

	// Network-specific genesis messages (Latin for mainnet, descriptive for others)
	// Mainnet: "Lux et Libertas" - Light and Liberty
	MainnetChainGenesis = `{"version":1,"message":"Lux et Libertas"}`
	// Testnet: "Per Aspera ad Astra" - Through hardships to the stars
	TestnetChainGenesis = `{"version":1,"message":"Per Aspera ad Astra"}`
	// Devnet: "In Silico Veritas" - Truth in silicon
	DevnetChainGenesis = `{"version":1,"message":"In Silico Veritas"}`
	// Local/Custom: "Carpe Diem" - Seize the day
	LocalChainGenesis = `{"version":1,"message":"Carpe Diem"}`

	// VMAliases is the default (VMID -> aliases) map for the node's VM
	// manager. Chain entries are derived from Registry; fx entries are
	// the static feature-extension aliases (secp256k1fx, nftfx, ...).
	VMAliases = VMAliasesMap()

	errOverridesStandardNetworkConfig = errors.New("overrides standard network genesis config")
)

// Bootstrapper represents a network bootstrap node with parsed types.
// Supports both IP addresses and hostnames for the endpoint.
type Bootstrapper struct {
	ID       ids.NodeID
	Endpoint endpoints.Endpoint
}

// IP returns the IP address if this is an IP-based endpoint.
// For hostname endpoints, this returns an invalid AddrPort.
// Deprecated: Use Endpoint directly for new code.
func (b Bootstrapper) IP() endpoints.Endpoint {
	return b.Endpoint
}

// ParseBootstrapper converts a genesis config bootstrapper to a parsed Bootstrapper.
// The IP field can be either an IP:port (e.g., "1.2.3.4:9631") or a hostname:port
// (e.g., "node-0.validators.example.com:9631") — a hostname keeps a bootstrapper
// addressable when its address is assigned at start time rather than pinned.
func ParseBootstrapper(b genesiscfg.Bootstrapper) (Bootstrapper, error) {
	nodeID, err := ids.NodeIDFromString(b.ID)
	if err != nil {
		return Bootstrapper{}, fmt.Errorf("invalid bootstrapper ID %q: %w", b.ID, err)
	}
	endpoint, err := endpoints.ParseEndpoint(b.IP)
	if err != nil {
		return Bootstrapper{}, fmt.Errorf("invalid bootstrapper endpoint %q: %w", b.IP, err)
	}
	return Bootstrapper{ID: nodeID, Endpoint: endpoint}, nil
}

// parseProofOfPossession converts a genesis config ProofOfPossession (with hex strings)
// to a node signer.ProofOfPossession (with byte arrays).
func parseProofOfPossession(pop *genesiscfg.ProofOfPossession) (*signer.ProofOfPossession, error) {
	if pop == nil {
		return nil, nil
	}

	// Decode the public key from hex string (may have 0x prefix)
	pkBytes, err := formatting.Decode(formatting.HexNC, pop.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode publicKey: %w", err)
	}

	// Decode the proof of possession from hex string (may have 0x prefix)
	popBytes, err := formatting.Decode(formatting.HexNC, pop.ProofOfPossession)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proofOfPossession: %w", err)
	}

	result := &signer.ProofOfPossession{}
	copy(result.PublicKey[:], pkBytes)
	copy(result.ProofOfPossession[:], popBytes)

	// Verify the proof of possession is valid
	if err := result.Verify(); err != nil {
		return nil, fmt.Errorf("invalid proof of possession: %w", err)
	}

	return result, nil
}

// GetBootstrappers returns parsed bootstrappers for the network
func GetBootstrappers(networkID uint32) ([]Bootstrapper, error) {
	cfgBootstrappers := genesiscfg.GetBootstrappers(networkID)
	if len(cfgBootstrappers) == 0 {
		// The disk paths genesiscfg checks (~/work/lux/genesis, /etc/lux) are
		// empty inside a node container, so fall back to the bootstrappers
		// embedded in the genesis module — a node then finds peers with zero
		// on-disk config.
		cfgBootstrappers = genesisconfigs.GetBootstrappers(networkID)
	}
	result := make([]Bootstrapper, 0, len(cfgBootstrappers))
	for _, b := range cfgBootstrappers {
		parsed, err := ParseBootstrapper(b)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// SampleBootstrappers returns a random sample of bootstrappers for the network
func SampleBootstrappers(networkID uint32, count int) ([]Bootstrapper, error) {
	allBootstrappers, err := GetBootstrappers(networkID)
	if err != nil {
		return nil, err
	}
	count = min(count, len(allBootstrappers))
	if count <= 0 {
		return nil, nil
	}

	s := sampler.NewUniform()
	s.Initialize(uint64(len(allBootstrappers)))
	indices, _ := s.Sample(count)

	sampled := make([]Bootstrapper, 0, len(indices))
	for _, index := range indices {
		sampled = append(sampled, allBootstrappers[int(index)])
	}
	return sampled, nil
}

// StakingConfig is the staking configuration with time.Duration types
type StakingConfig struct {
	UptimeRequirement float64
	MinValidatorStake uint64
	MaxValidatorStake uint64
	MinDelegatorStake uint64
	MinDelegationFee  uint32
	MinStakeDuration  time.Duration
	MaxStakeDuration  time.Duration
	RewardConfig      reward.Config

	// BLS key information for genesis replay
	NodeID               string `json:"nodeID"`
	BLSPublicKey         []byte `json:"blsPublicKey"`
	BLSProofOfPossession []byte `json:"blsProofOfPossession"`
}

// GetStakingConfig returns the staking config with time.Duration types
func GetStakingConfig(networkID uint32) StakingConfig {
	cfg := genesiscfg.GetStakingConfig(networkID)
	return StakingConfig{
		UptimeRequirement: cfg.UptimeRequirement,
		MinValidatorStake: cfg.MinValidatorStake,
		MaxValidatorStake: cfg.MaxValidatorStake,
		MinDelegatorStake: cfg.MinDelegatorStake,
		MinDelegationFee:  cfg.MinDelegationFee,
		MinStakeDuration:  time.Duration(cfg.MinStakeDuration) * time.Second,
		MaxStakeDuration:  time.Duration(cfg.MaxStakeDuration) * time.Second,
		RewardConfig: reward.Config{
			MaxConsumptionRate: cfg.RewardConfig.MaxConsumptionRate,
			MinConsumptionRate: cfg.RewardConfig.MinConsumptionRate,
			MintingPeriod:      time.Duration(cfg.RewardConfig.MintingPeriod) * time.Second,
			SupplyCap:          cfg.RewardConfig.SupplyCap,
		},
	}
}

// TxFeeConfig contains transaction fee configuration
// This includes the basic fee config from genesis plus dynamic/validator fees
type TxFeeConfig struct {
	TxFee              uint64     `json:"txFee"`
	CreateAssetTxFee   uint64     `json:"createAssetTxFee"`
	DynamicFeeConfig   gas.Config `json:"dynamicFeeConfig"`
	ValidatorFeeConfig fee.Config `json:"validatorFeeConfig"`
}

// Default dynamic fee parameters
var (
	MainnetDynamicFeeConfig = gas.Config{
		Weights: gas.Dimensions{
			gas.Bandwidth: 1,
			gas.DBRead:    1,
			gas.DBWrite:   1,
			gas.Compute:   1,
		},
		MaxCapacity:              1_000_000,
		MaxPerSecond:             100_000,
		TargetPerSecond:          50_000,
		MinPrice:                 1,
		ExcessConversionConstant: 5_000,
	}

	TestnetDynamicFeeConfig = gas.Config{
		Weights: gas.Dimensions{
			gas.Bandwidth: 1,
			gas.DBRead:    1,
			gas.DBWrite:   1,
			gas.Compute:   1,
		},
		MaxCapacity:              1_000_000,
		MaxPerSecond:             100_000,
		TargetPerSecond:          50_000,
		MinPrice:                 1,
		ExcessConversionConstant: 5_000,
	}

	LocalDynamicFeeConfig = gas.Config{
		Weights: gas.Dimensions{
			gas.Bandwidth: 1,
			gas.DBRead:    1,
			gas.DBWrite:   1,
			gas.Compute:   1,
		},
		MaxCapacity:              1_000_000,
		MaxPerSecond:             100_000,
		TargetPerSecond:          50_000,
		MinPrice:                 1,
		ExcessConversionConstant: 5_000,
	}

	MainnetValidatorFeeConfig = fee.Config{
		Capacity:                 20_000,
		Target:                   10_000,
		MinPrice:                 512,
		ExcessConversionConstant: 1_587,
	}

	TestnetValidatorFeeConfig = fee.Config{
		Capacity:                 20_000,
		Target:                   10_000,
		MinPrice:                 512,
		ExcessConversionConstant: 1_587,
	}

	LocalValidatorFeeConfig = fee.Config{
		Capacity:                 20_000,
		Target:                   10_000,
		MinPrice:                 512,
		ExcessConversionConstant: 1_587,
	}
)

// GetTxFeeConfig returns the tx fee config
func GetTxFeeConfig(networkID uint32) TxFeeConfig {
	cfg := genesiscfg.GetTxFeeConfig(networkID)

	var dynamicCfg gas.Config
	var validatorCfg fee.Config

	switch networkID {
	case constants.MainnetID:
		dynamicCfg = MainnetDynamicFeeConfig
		validatorCfg = MainnetValidatorFeeConfig
	case constants.TestnetID:
		dynamicCfg = TestnetDynamicFeeConfig
		validatorCfg = TestnetValidatorFeeConfig
	default:
		dynamicCfg = LocalDynamicFeeConfig
		validatorCfg = LocalValidatorFeeConfig
	}

	return TxFeeConfig{
		TxFee:              cfg.TxFee,
		CreateAssetTxFee:   cfg.CreateAssetTxFee,
		DynamicFeeConfig:   dynamicCfg,
		ValidatorFeeConfig: validatorCfg,
	}
}

// GetConfig returns the genesis config for the given network ID
func GetConfig(networkID uint32) *genesiscfg.Config {
	// Use embedded genesis configs (//go:embed) first.
	// The genesis/pkg/genesis.GetConfig() checks filesystem paths which
	// don't exist in Docker containers, returning an empty config.
	if cfg, err := genesisconfigs.GetConfig(networkID); err == nil && len(cfg.Allocations) > 0 {
		return cfg
	}
	// Fallback to filesystem-based config (local dev)
	return genesiscfg.GetConfig(networkID)
}

// FromConfig builds genesis bytes from a config.
//
// X-Chain is opt-in via config.XChainGenesis (a small JSON descriptor like
//
//	{"symbol":"LUX","name":"Lux","denomination":9}
//
// ). When set, the builder constructs an XVM genesis whose primary asset
// is that descriptor (initial holders sourced from config.Allocations) and
// emits an X-Chain entry in the primary-network CreateChainTx set. When
// empty, no XVM genesis is built and X-Chain is omitted from the chain
// set — the path used by P-only L2s whose value capture lives on a
// downstream EVM.
//
// The LUX asset ID returned is always the network-wide constant
// (constants.UTXO_ASSET_ID), independent of whether X-Chain is baked.
// This is what decouples the asset from any specific chain's genesis
// bytes — P-Chain stake/UTXO references stay byte-stable across both
// shapes of primary network.
func FromConfig(config *genesiscfg.Config) ([]byte, ids.ID, error) {
	// HRP is needed for X-Chain holder addresses, P-Chain protocol
	// allocations, staker allocations, and reward addresses — define once,
	// before the X-Chain conditional, so it's available on both paths.
	hrp := constants.GetHRP(config.NetworkID)

	// X-Chain is the first opt-in chain. Build its XVM genesis bytes only
	// when config.XChainGenesis is non-empty; otherwise leave the slot
	// empty and the optIn loop below skips emitting an X-Chain CreateChainTx.
	var xvmGenesisBytes []byte
	if config.XChainGenesis != "" {
		var asset xvmgenesis.AssetDescriptor
		if err := json.Unmarshal([]byte(config.XChainGenesis), &asset); err != nil {
			return nil, ids.Empty, fmt.Errorf("invalid xChainGenesis shard: %w", err)
		}

		holders := []xvmgenesis.Holder{}
		memoBytes := []byte{}
		for _, a := range config.Allocations {
			if a.InitialAmount == 0 {
				continue
			}
			// Format address as bech32 for the X-Chain
			bech32Addr, err := address.FormatBech32(hrp, a.UTXOAddr[:])
			if err != nil {
				return nil, ids.Empty, fmt.Errorf("failed to format bech32 address: %w", err)
			}
			holders = append(holders, xvmgenesis.Holder{
				Amount:  a.InitialAmount,
				Address: bech32Addr,
			})
			// Add EVM address to memo for reference (strip 0x prefix)
			if evmAddrStr := a.EVMAddr.Hex(); len(evmAddrStr) > 2 {
				memoBytes = append(memoBytes, []byte(evmAddrStr[2:])...)
			}
		}

		var err error
		xvmGenesisBytes, err = xvmgenesis.BuildBytes(config.NetworkID, asset, holders, memoBytes)
		if err != nil {
			return nil, ids.Empty, err
		}
	}

	// X-Chain native asset ID is derived from the actual XVM genesis bytes
	// (the runtime ID of the first GenesisAsset.CreateAssetTx) so the value
	// the wallet builder reads back via platform.getStakingAssetID matches
	// the asset the X-Chain genuinely mints. On a sovereign L1 every primary
	// network has its own X-Chain genesis content (different validator set,
	// different initial holders, different denomination/name) so the
	// genesis-derived asset ID is distinct from any
	// constants.UTXOAssetIDFor(networkID) value — those are network-id-keyed
	// constants and identical across every L1 sharing the same
	// primary-network ID, which would let two sovereign networks collide.
	//
	// When X-Chain is opt-out (P-only mode, xvmGenesisBytes is nil) we keep
	// the network-id-keyed constant as a placeholder; the asset ID is
	// irrelevant in that mode since there is no X-Chain to mint on.
	var utxoAssetID ids.ID
	if len(xvmGenesisBytes) > 0 {
		var err error
		utxoAssetID, err = xvmgenesis.AssetIDFromBytes(xvmGenesisBytes)
		if err != nil {
			return nil, ids.Empty, fmt.Errorf("derive X-Chain asset ID from genesis: %w", err)
		}
	} else {
		utxoAssetID = constants.UTXOAssetIDFor(config.NetworkID)
	}

	genesisTime := time.Unix(int64(config.StartTime), 0)

	// Calculate initial supply. Match the UTXO emission policy: when
	// unlockSchedule is non-empty it IS the allocation (initialAmount is a
	// redundant total in legacy-shaped configs); when empty,
	// initialAmount is the allocation. Either way, count exactly once —
	// the reported supply must equal the sum of actually-emitted UTXOs +
	// validator stakes, not a double-count of the two views of one number.
	initialSupply := uint64(0)
	for _, a := range config.Allocations {
		if len(a.UnlockSchedule) > 0 {
			for _, unlock := range a.UnlockSchedule {
				initialSupply += unlock.Amount
			}
		} else {
			initialSupply += a.InitialAmount
		}
	}

	// Build platform allocations
	initiallyStaked := set.Set[ids.ShortID]{}
	for _, addr := range config.InitialStakedFunds {
		initiallyStaked.Add(addr)
	}

	platformAllocations := []genesis.Allocation{}
	skippedAllocations := []genesiscfg.Allocation{}
	for _, a := range config.Allocations {
		if initiallyStaked.Contains(a.UTXOAddr) {
			skippedAllocations = append(skippedAllocations, a)
			continue
		}
		for _, unlock := range a.UnlockSchedule {
			if unlock.Amount > 0 {
				// Format address as bech32 for the P-Chain
				bech32Addr, err := address.FormatBech32(hrp, a.UTXOAddr[:])
				if err != nil {
					return nil, ids.Empty, fmt.Errorf("failed to format bech32 address for P-chain: %w", err)
				}
				platformAllocations = append(platformAllocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  bech32Addr,
					Message:  a.EVMAddr.Bytes(),
				})
			}
		}

		// Also create P-Chain UTXO for initialAmount (spendable, no locktime)
		// when unlockSchedule is empty. Legacy genesis JSONs set
		// initialAmount AS THE SUM of unlockSchedule (two views of the same
		// total). Emitting both would double-mint that total. Devnet-style
		// configs set initialAmount with an empty unlockSchedule and rely on
		// this single UTXO. One semantic, one UTXO, no double-mint.
		if a.InitialAmount > 0 && len(a.UnlockSchedule) == 0 {
			bech32Addr, err := address.FormatBech32(hrp, a.UTXOAddr[:])
			if err != nil {
				return nil, ids.Empty, fmt.Errorf("failed to format bech32 address for P-chain initialAmount: %w", err)
			}
			platformAllocations = append(platformAllocations, genesis.Allocation{
				Locktime: 0, // immediately spendable
				Amount:   a.InitialAmount,
				Address:  bech32Addr,
				Message:  a.EVMAddr.Bytes(),
			})
		}
	}

	// Build validators
	validators := []genesis.PermissionlessValidator{}
	allNodeAllocations := splitAllocations(skippedAllocations, len(config.InitialStakers))
	endStakingTime := genesisTime.Add(time.Duration(config.InitialStakeDuration) * time.Second)
	stakingOffset := time.Duration(0)

	for i, staker := range config.InitialStakers {
		// Safely get node allocations (may be empty if no initialStakedFunds)
		var nodeAllocations []genesiscfg.Allocation
		if i < len(allNodeAllocations) {
			nodeAllocations = allNodeAllocations[i]
		}

		// Use explicit staker times if provided, otherwise use calculated values
		startTime := uint64(genesisTime.Unix())
		if staker.StartTime > 0 {
			startTime = staker.StartTime
		}

		endTime := endStakingTime.Add(-stakingOffset)
		stakingOffset += time.Duration(config.InitialStakeDurationOffset) * time.Second
		endTimeUnix := uint64(endTime.Unix())
		if staker.EndTime > 0 {
			endTimeUnix = staker.EndTime
		}

		allocations := []genesis.Allocation{}
		for _, a := range nodeAllocations {
			for _, unlock := range a.UnlockSchedule {
				// Format address as bech32 for staker allocations
				bech32Addr, err := address.FormatBech32(hrp, a.UTXOAddr[:])
				if err != nil {
					return nil, ids.Empty, fmt.Errorf("failed to format bech32 address for staker allocation: %w", err)
				}
				allocations = append(allocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  bech32Addr,
					Message:  a.EVMAddr.Bytes(),
				})
			}
		}

		// Calculate weight from allocations or use explicit weight
		weight := staker.Weight
		if weight == 0 {
			for _, a := range allocations {
				weight += a.Amount
			}
		}

		// Format reward address as bech32
		rewardBech32, err := address.FormatBech32(hrp, staker.RewardAddress[:])
		if err != nil {
			return nil, ids.Empty, fmt.Errorf("failed to format bech32 reward address: %w", err)
		}

		// Parse the BLS proof of possession if present
		var blsSigner *signer.ProofOfPossession
		if staker.Signer != nil {
			blsSigner, err = parseProofOfPossession(staker.Signer)
			if err != nil {
				return nil, ids.Empty, fmt.Errorf("failed to parse proof of possession for staker %d: %w", i, err)
			}
		}

		validators = append(validators, genesis.PermissionlessValidator{
			Validator: genesis.Validator{
				StartTime: startTime,
				EndTime:   endTimeUnix,
				Weight:    weight,
				NodeID:    staker.NodeID,
			},
			RewardOwner: &genesis.Owner{
				Threshold: 1,
				Addresses: []string{rewardBech32},
			},
			Staked:             allocations,
			ExactDelegationFee: staker.DelegationFee,
			Signer:             blsSigner,
		})
	}

	// Primary-network chain set is data-driven.
	//
	// P-Chain is implicit (the platform genesis itself). Every other
	// primary-network chain — including X-Chain — is opt-in via a
	// non-empty genesis string in the resolved Config. Entries with
	// empty GenesisData are SKIPPED — luxd will not start a chain
	// manager for them. Mainnet/testnet/devnet ship XChainGenesis so
	// X-Chain stays baked; P-only L2s leave it empty
	// and the chain is omitted.
	//
	// Order is fixed (X→C→D→B→T→Q→Z) and preserved across
	// presence/absence so byte-identical genesis holds when the same
	// shard set is supplied. Append-only — reordering shifts the
	// P-Chain genesis byte layout.
	//
	// Runtime tracking is separate: an operator decides which of the
	// baked chains this node actually runs via --track-chains /
	// --track-all-chains. Baked-but-untracked chains stay dormant.
	optIn := []struct {
		genesisData []byte
		vmID        ids.ID
		name        string
		fxIDs       []ids.ID // X-Chain only; nil for everything else.
	}{
		{xvmGenesisBytes, constants.XVMID, "X-Chain", []ids.ID{
			secp256k1fx.ID,
			nftfx.ID,
			propertyfx.ID,
			mldsafx.ID,
			slhdsafx.ID,
			ed25519fx.ID,
			secp256r1fx.ID,
			schnorrfx.ID,
			bls12381fx.ID,
		}},
		{[]byte(config.CChainGenesis), constants.EVMID, "C-Chain", nil},
		{[]byte(config.DChainGenesis), constants.DexVMID, "D-Chain", nil},
		{[]byte(config.BChainGenesis), constants.BridgeVMID, "B-Chain", nil},
		{[]byte(config.MChainGenesis), constants.MPCVMID, "M-Chain", nil},
		{[]byte(config.FChainGenesis), constants.FHEVMID, "F-Chain", nil},
		{[]byte(config.QChainGenesis), constants.QuantumVMID, "Q-Chain", nil},
		{[]byte(config.ZChainGenesis), constants.ZKVMID, "Z-Chain", nil},
		// A/G/K carry genesis blobs and are deterministic genesis chains
		// (per the no-CreateChainTx model). Appended AFTER Z so the existing
		// X→C→D→B→T→Q→Z blockchain IDs are preserved; A/G/K take fresh tail IDs.
		{[]byte(config.AChainGenesis), constants.AIVMID, "A-Chain", nil},
		{[]byte(config.GChainGenesis), constants.GraphVMID, "G-Chain", nil},
		{[]byte(config.KChainGenesis), constants.KeyVMID, "K-Chain", nil},
	}
	chains := []genesis.Chain{}
	for _, c := range optIn {
		if len(c.genesisData) == 0 {
			continue
		}
		chains = append(chains, genesis.Chain{
			GenesisData: c.genesisData,
			ChainID:     constants.PrimaryNetworkID,
			VMID:        c.vmID,
			FxIDs:       c.fxIDs,
			Name:        c.name,
		})
	}

	// I/O/R chains carry no genesis blob; they load as plugins and are
	// instantiated via CreateChainTx post-genesis on their own chains.

	pChainGenesis, err := genesis.New(
		utxoAssetID,
		config.NetworkID,
		platformAllocations,
		validators,
		chains,
		config.StartTime,
		initialSupply,
		config.Message,
	)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("problem while building platform chain's genesis state: %w", err)
	}
	pChainGenesisBytes, err := pChainGenesis.Bytes()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("problem while serializing platform chain's genesis state: %w", err)
	}
	return pChainGenesisBytes, utxoAssetID, nil
}

// FromFile loads genesis config from file and builds genesis bytes.
// Caller owns the choice — if a genesis file is set, we use it. No
// "is this networkID standard?" guard, no allow-flag. Operator-owned
// chainset.
func FromFile(networkID uint32, filepath string, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	config, err := genesiscfg.GetConfigFile(filepath)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("failed to load genesis file %s: %w", filepath, err)
	}
	if err := validateConfig(networkID, config, stakingCfg); err != nil {
		return nil, ids.Empty, fmt.Errorf("genesis config validation failed: %w", err)
	}
	return FromConfig(config)
}

// FromFlag parses base64-encoded genesis content and builds genesis bytes.
// Same contract as FromFile — caller owns the override.
func FromFlag(networkID uint32, genesisContent string, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	data, err := base64.StdEncoding.DecodeString(genesisContent)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("failed to decode base64 genesis content: %w", err)
	}
	var config genesiscfg.Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, ids.Empty, fmt.Errorf("failed to parse genesis config: %w", err)
	}
	if err := validateConfig(networkID, &config, stakingCfg); err != nil {
		return nil, ids.Empty, fmt.Errorf("genesis config validation failed: %w", err)
	}
	return FromConfig(&config)
}

// FromDatabase returns genesis data for database replay mode
func FromDatabase(networkID uint32, dbPath string, dbType string, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	config := genesiscfg.GetConfig(constants.LocalID)
	config.NetworkID = networkID
	config.Message = "DATABASE_REPLAY_MODE"
	return FromConfig(config)
}

// UTXOAssetIDFromGenesisBytes returns the X-Chain native asset ID encoded
// in a platform-genesis blob. It parses the platform genesis, finds the
// X-Chain CreateChainTx (vmID == constants.XVMID), then decodes that
// chain's embedded XVM genesis bytes to recover the runtime asset ID
// (the same value vm.initGenesis assigns to the first GenesisAsset).
//
// Returns (ids.Empty, false, nil) when the platform genesis contains
// no X-Chain (P-only mode) — callers fall back to whatever default
// they prefer (e.g. constants.UTXOAssetIDFor(networkID)).
//
// Returns a non-nil error when the platform genesis is unparseable or
// the X-Chain genesis bytes embedded inside it are malformed; both are
// unrecoverable on a primary-network bootstrap.
//
// Used by config.getGenesisData when it loads genesis via the cached /
// raw paths that skip FromConfig — those paths historically returned
// constants.UTXOAssetIDFor(networkID), but on sovereign L1s that value
// disagrees with what's actually in the genesis bytes. Wiring this
// helper through getGenesisData means the node always reports the
// genesis-derived asset ID via platform.getStakingAssetID regardless
// of which load path was taken.
func UTXOAssetIDFromGenesisBytes(genesisBytes []byte) (ids.ID, bool, error) {
	// Parse the platform genesis directly so we can distinguish parse
	// failure ("garbage bytes — fatal") from missing X-Chain ("P-only
	// mode — caller falls back to UTXOAssetIDFor"). VMGenesis collapses
	// both into one error, which is the wrong shape here.
	gen, err := genesis.Parse(genesisBytes)
	if err != nil {
		return ids.Empty, false, fmt.Errorf("parse platform genesis: %w", err)
	}
	for _, chain := range gen.Chains {
		uChain, ok := chain.Unsigned.(*pchaintxs.CreateChainTx)
		if !ok {
			continue
		}
		if uChain.VMID() != constants.XVMID {
			continue
		}
		id, err := xvmgenesis.AssetIDFromBytes(uChain.GenesisData())
		if err != nil {
			return ids.Empty, false, fmt.Errorf("derive X-Chain asset ID from genesis data: %w", err)
		}
		return id, true, nil
	}
	// Parsed cleanly but no X-Chain CreateChainTx — P-only mode.
	return ids.Empty, false, nil
}

// VMGenesis returns the genesis tx for a specific VM
func VMGenesis(genesisBytes []byte, vmID ids.ID) (*pchaintxs.Tx, error) {
	gen, err := genesis.Parse(genesisBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse genesis: %w", err)
	}
	for _, chain := range gen.Chains {
		uChain := chain.Unsigned.(*pchaintxs.CreateChainTx)
		if uChain.VMID() == vmID {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("couldn't find blockchain with VM ID %s", vmID)
}

// Aliases returns the default aliases for chains and APIs
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := map[string][]string{
		path.Join(constants.ChainAliasPrefix, constants.PlatformChainID.String()): {
			"P",
			"platform",
			path.Join(constants.ChainAliasPrefix, "P"),
			path.Join(constants.ChainAliasPrefix, "platform"),
		},
	}
	chainAliases := map[ids.ID][]string{
		constants.PlatformChainID: PChainAliases,
	}

	gen, err := genesis.Parse(genesisBytes)
	if err != nil {
		return nil, nil, err
	}
	for _, chain := range gen.Chains {
		uChain := chain.Unsigned.(*pchaintxs.CreateChainTx)
		chainID := chain.ID()
		endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
		switch uChain.VMID() {
		case constants.XVMID:
			apiAliases[endpoint] = []string{
				"X",
				"xvm",
				path.Join(constants.ChainAliasPrefix, "X"),
				path.Join(constants.ChainAliasPrefix, "xvm"),
			}
			chainAliases[chainID] = XChainAliases
		case constants.EVMID:
			apiAliases[endpoint] = []string{
				"C",
				"evm",
				path.Join(constants.ChainAliasPrefix, "C"),
				path.Join(constants.ChainAliasPrefix, "evm"),
			}
			chainAliases[chainID] = CChainAliases
		case constants.DexVMID:
			apiAliases[endpoint] = []string{
				"D",
				"dex",
				"dexvm",
				path.Join(constants.ChainAliasPrefix, "D"),
				path.Join(constants.ChainAliasPrefix, "dex"),
				path.Join(constants.ChainAliasPrefix, "dexvm"),
			}
			chainAliases[chainID] = DChainAliases
		case constants.QuantumVMID:
			apiAliases[endpoint] = []string{
				"Q",
				"quantum",
				"quantumvm",
				"pq",
				path.Join(constants.ChainAliasPrefix, "Q"),
				path.Join(constants.ChainAliasPrefix, "quantum"),
			}
			chainAliases[chainID] = QChainAliases
		case constants.AIVMID:
			apiAliases[endpoint] = []string{
				"A",
				"attest",
				"ai",
				"aivm",
				path.Join(constants.ChainAliasPrefix, "A"),
				path.Join(constants.ChainAliasPrefix, "attest"),
			}
			chainAliases[chainID] = AChainAliases
		case constants.BridgeVMID:
			apiAliases[endpoint] = []string{
				"B",
				"bridge",
				"bridgevm",
				path.Join(constants.ChainAliasPrefix, "B"),
				path.Join(constants.ChainAliasPrefix, "bridge"),
			}
			chainAliases[chainID] = BChainAliases
		case constants.MPCVMID:
			apiAliases[endpoint] = []string{
				"M",
				"mpc",
				"mpcvm",
				path.Join(constants.ChainAliasPrefix, "M"),
				path.Join(constants.ChainAliasPrefix, "mpc"),
			}
			chainAliases[chainID] = MChainAliases
		case constants.FHEVMID:
			apiAliases[endpoint] = []string{
				"F",
				"fhe",
				"fhevm",
				path.Join(constants.ChainAliasPrefix, "F"),
				path.Join(constants.ChainAliasPrefix, "fhe"),
			}
			chainAliases[chainID] = FChainAliases
		case constants.ZKVMID:
			apiAliases[endpoint] = []string{
				"Z",
				"zk",
				"zkvm",
				path.Join(constants.ChainAliasPrefix, "Z"),
				path.Join(constants.ChainAliasPrefix, "zk"),
			}
			chainAliases[chainID] = ZChainAliases
		case constants.GraphVMID:
			apiAliases[endpoint] = []string{
				"G",
				"graph",
				"graphvm",
				"dgraph",
				path.Join(constants.ChainAliasPrefix, "G"),
				path.Join(constants.ChainAliasPrefix, "graph"),
			}
			chainAliases[chainID] = GChainAliases
		case constants.KeyVMID:
			apiAliases[endpoint] = []string{
				"K",
				"kms",
				"keyvm",
				path.Join(constants.ChainAliasPrefix, "K"),
				path.Join(constants.ChainAliasPrefix, "kms"),
			}
			chainAliases[chainID] = KChainAliases
		case constants.IdentityVMID:
			apiAliases[endpoint] = []string{
				"I",
				"identity",
				"identityvm",
				"id",
				path.Join(constants.ChainAliasPrefix, "I"),
				path.Join(constants.ChainAliasPrefix, "identity"),
			}
			chainAliases[chainID] = IChainAliases
		case constants.OracleVMID:
			apiAliases[endpoint] = []string{
				"O",
				"oracle",
				"oraclevm",
				"feed",
				path.Join(constants.ChainAliasPrefix, "O"),
				path.Join(constants.ChainAliasPrefix, "oracle"),
			}
			chainAliases[chainID] = OChainAliases
		case constants.RelayVMID:
			apiAliases[endpoint] = []string{
				"R",
				"relay",
				"relayvm",
				"msg",
				path.Join(constants.ChainAliasPrefix, "R"),
				path.Join(constants.ChainAliasPrefix, "relay"),
			}
			chainAliases[chainID] = RChainAliases
		}
	}
	return apiAliases, chainAliases, nil
}

// validateConfig validates the genesis config
func validateConfig(networkID uint32, config *genesiscfg.Config, stakingCfg *StakingConfig) error {
	if config.NetworkID != networkID {
		return fmt.Errorf("network ID mismatch: expected %d, got %d", networkID, config.NetworkID)
	}
	if config.InitialStakeDuration > uint64(stakingCfg.MaxStakeDuration.Seconds()) {
		return fmt.Errorf("initial stake duration %d exceeds max %d", config.InitialStakeDuration, uint64(stakingCfg.MaxStakeDuration.Seconds()))
	}
	return nil
}

// DevModeConfig holds configuration for dev mode genesis
type DevModeConfig struct {
	NodeID        ids.NodeID  // The validator node ID
	BLSPublicKey  string      // BLS public key hex
	BLSPopProof   string      // BLS proof of possession hex
	RewardAddress ids.ShortID // Reward/allocation address
	CChainGenesis string      // C-Chain genesis JSON
	StartTime     uint64      // Genesis start time (if 0, uses time.Now())
}

// ForDevMode creates a genesis configuration suitable for single-node development mode.
// It creates a single validator with far-future stake time and funds the treasury address.
func ForDevMode(cfg DevModeConfig, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	// Genesis start time: use provided time or fall back to now
	startTime := cfg.StartTime
	if startTime == 0 {
		startTime = uint64(time.Now().Unix())
	}

	// Far-future stake duration: 100 years in seconds
	// This ensures the validator never expires during development
	const hundredYears = 100 * 365 * 24 * 60 * 60

	// Create allocation for the reward address
	// Initial staked amount: 1B LUX (enough to be a validator)
	const oneMillionLUX = 1_000_000 * units.Lux     // 1M LUX
	const oneBillionLUX = 1_000_000_000 * units.Lux // 1B LUX

	allocation := genesiscfg.Allocation{
		EVMAddr:       cfg.RewardAddress, // Same as UTXO addr for simplicity
		UTXOAddr:      cfg.RewardAddress,
		InitialAmount: oneMillionLUX, // Initial unlocked amount
		UnlockSchedule: []genesiscfg.LockedAmount{
			{
				Amount:   oneBillionLUX, // Staked amount
				Locktime: 0,             // No lock time
			},
		},
	}

	// Create the single staker
	var signer *genesiscfg.ProofOfPossession
	if cfg.BLSPublicKey != "" && cfg.BLSPopProof != "" {
		signer = &genesiscfg.ProofOfPossession{
			PublicKey:         cfg.BLSPublicKey,
			ProofOfPossession: cfg.BLSPopProof,
		}
	}

	staker := genesiscfg.Staker{
		NodeID:        cfg.NodeID,
		RewardAddress: cfg.RewardAddress,
		DelegationFee: 1000000, // 100% delegation fee (no delegators in dev mode)
		Signer:        signer,
		Weight:        oneBillionLUX,
		StartTime:     startTime,
		EndTime:       startTime + hundredYears,
	}

	// Build the genesis config
	config := &genesiscfg.Config{
		NetworkID:                  constants.LocalID,
		Allocations:                []genesiscfg.Allocation{allocation},
		StartTime:                  startTime,
		InitialStakeDuration:       hundredYears,
		InitialStakeDurationOffset: 0,
		InitialStakedFunds:         []ids.ShortID{cfg.RewardAddress},
		InitialStakers:             []genesiscfg.Staker{staker},
		CChainGenesis:              cfg.CChainGenesis,
		Message:                    "Lux Development Mode Genesis",
	}

	return FromConfig(config)
}

// splitAllocations splits allocations across multiple stakers
func splitAllocations(allocations []genesiscfg.Allocation, numSplits int) [][]genesiscfg.Allocation {
	if numSplits == 0 {
		return [][]genesiscfg.Allocation{}
	}

	totalAmount := uint64(0)
	for _, a := range allocations {
		for _, unlock := range a.UnlockSchedule {
			totalAmount += unlock.Amount
		}
	}

	nodeWeight := totalAmount / uint64(numSplits)
	allNodeAllocations := make([][]genesiscfg.Allocation, 0, numSplits)

	currentNodeAllocation := []genesiscfg.Allocation{}
	currentNodeAmount := uint64(0)

	for _, allocation := range allocations {
		currentAllocation := allocation
		currentAllocation.InitialAmount = 0
		currentAllocation.UnlockSchedule = nil

		for _, unlock := range allocation.UnlockSchedule {
			for currentNodeAmount+unlock.Amount > nodeWeight && len(allNodeAllocations) < numSplits-1 {
				amountToAdd := nodeWeight - currentNodeAmount
				currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, genesiscfg.LockedAmount{
					Amount:   amountToAdd,
					Locktime: unlock.Locktime,
				})
				unlock.Amount -= amountToAdd

				currentNodeAllocation = append(currentNodeAllocation, currentAllocation)
				allNodeAllocations = append(allNodeAllocations, currentNodeAllocation)

				currentNodeAllocation = nil
				currentNodeAmount = 0

				currentAllocation = allocation
				currentAllocation.InitialAmount = 0
				currentAllocation.UnlockSchedule = nil
			}

			if unlock.Amount == 0 {
				continue
			}

			currentAllocation.UnlockSchedule = append(currentAllocation.UnlockSchedule, genesiscfg.LockedAmount{
				Amount:   unlock.Amount,
				Locktime: unlock.Locktime,
			})
			currentNodeAmount += unlock.Amount
		}

		if len(currentAllocation.UnlockSchedule) > 0 {
			currentNodeAllocation = append(currentNodeAllocation, currentAllocation)
		}
	}

	return append(allNodeAllocations, currentNodeAllocation)
}
