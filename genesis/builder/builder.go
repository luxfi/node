// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package builder provides genesis byte generation for Lux networks.
// This package depends on node types and is responsible for building
// the actual genesis state from genesis config.
package builder

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/const"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/sampler"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/exchangevm"
	"github.com/luxfi/node/vms/exchangevm/fxs"
	xchaintxs "github.com/luxfi/node/vms/exchangevm/txs"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/reward"
	pchaintxs "github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/secp256k1fx"

	genesiscfg "github.com/luxfi/genesis/pkg/genesis"
)

var (
	// PChainAliases are the default aliases for the P-Chain
	PChainAliases = []string{"P", "platform"}
	// XChainAliases are the default aliases for the X-Chain
	XChainAliases = []string{"X", "xvm"}
	// CChainAliases are the default aliases for the C-Chain
	CChainAliases = []string{"C", "evm"}

	// VMAliases are the default aliases for VMs
	VMAliases = map[ids.ID][]string{
		constants.PlatformVMID: {"platform"},
		constants.XVMID:        {"xvm"},
		constants.EVMID:        {"evm"},
		secp256k1fx.ID:         {"secp256k1fx"},
		nftfx.ID:               {"nftfx"},
		propertyfx.ID:          {"propertyfx"},
	}

	errNoTxs                          = errors.New("genesis creates no transactions")
	errOverridesStandardNetworkConfig = errors.New("overrides standard network genesis config")
)

// Bootstrapper represents a network bootstrap node with parsed types
type Bootstrapper struct {
	ID ids.NodeID
	IP netip.AddrPort
}

// ParseBootstrapper converts a genesis config bootstrapper to a parsed Bootstrapper
func ParseBootstrapper(b genesiscfg.Bootstrapper) (Bootstrapper, error) {
	nodeID, err := ids.NodeIDFromString(b.ID)
	if err != nil {
		return Bootstrapper{}, fmt.Errorf("invalid bootstrapper ID %q: %w", b.ID, err)
	}
	ip, err := netip.ParseAddrPort(b.IP)
	if err != nil {
		return Bootstrapper{}, fmt.Errorf("invalid bootstrapper IP %q: %w", b.IP, err)
	}
	return Bootstrapper{ID: nodeID, IP: ip}, nil
}

// GetBootstrappers returns parsed bootstrappers for the network
func GetBootstrappers(networkID uint32) ([]Bootstrapper, error) {
	cfgBootstrappers := genesiscfg.GetBootstrappers(networkID)
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
	return genesiscfg.GetConfig(networkID)
}

// FromConfig builds genesis bytes from a config
func FromConfig(config *genesiscfg.Config) ([]byte, ids.ID, error) {
	// Build XVM (X-Chain) genesis
	lux := exchangevm.GenesisAssetDefinition{
		Name:         "Lux",
		Symbol:       "LUX",
		Denomination: 9,
		InitialState: exchangevm.AssetInitialState{},
	}
	memoBytes := []byte{}

	// Sort allocations for deterministic output
	type allocation struct {
		ETHAddr       ids.ShortID
		LUXAddr       ids.ShortID
		InitialAmount uint64
	}
	xAllocations := []allocation{}
	for _, a := range config.Allocations {
		if a.InitialAmount > 0 {
			xAllocations = append(xAllocations, allocation{
				ETHAddr:       a.ETHAddr,
				LUXAddr:       a.LUXAddr,
				InitialAmount: a.InitialAmount,
			})
		}
	}

	// Get HRP for this network to format bech32 addresses
	hrp := constants.GetHRP(config.NetworkID)

	for _, a := range xAllocations {
		// Format address as bech32 for the X-Chain
		bech32Addr, err := address.FormatBech32(hrp, a.LUXAddr[:])
		if err != nil {
			return nil, ids.Empty, fmt.Errorf("failed to format bech32 address: %w", err)
		}
		lux.InitialState.FixedCap = append(lux.InitialState.FixedCap, exchangevm.GenesisHolder{
			Amount:  a.InitialAmount,
			Address: bech32Addr,
		})
		// Add ETH address to memo for reference
		ethAddrStr := a.ETHAddr.Hex()
		if len(ethAddrStr) > 2 { // "0x" prefix
			memoBytes = append(memoBytes, []byte(ethAddrStr[2:])...)
		}
	}
	lux.Memo = memoBytes

	xvmGenesis, err := exchangevm.NewGenesis(
		config.NetworkID,
		map[string]exchangevm.GenesisAssetDefinition{
			"LUX": lux,
		},
	)
	if err != nil {
		return nil, ids.Empty, err
	}
	xvmGenesisBytes, err := xvmGenesis.Bytes()
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't serialize xvm genesis: %w", err)
	}

	luxAssetID, err := XAssetID(xvmGenesisBytes)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("couldn't generate LUX asset ID: %w", err)
	}

	genesisTime := time.Unix(int64(config.StartTime), 0)

	// Calculate initial supply
	initialSupply := uint64(0)
	for _, a := range config.Allocations {
		initialSupply += a.InitialAmount
		for _, unlock := range a.UnlockSchedule {
			initialSupply += unlock.Amount
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
		if initiallyStaked.Contains(a.LUXAddr) {
			skippedAllocations = append(skippedAllocations, a)
			continue
		}
		for _, unlock := range a.UnlockSchedule {
			if unlock.Amount > 0 {
				// Format address as bech32 for the P-Chain
				bech32Addr, err := address.FormatBech32(hrp, a.LUXAddr[:])
				if err != nil {
					return nil, ids.Empty, fmt.Errorf("failed to format bech32 address for P-chain: %w", err)
				}
				platformAllocations = append(platformAllocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  bech32Addr,
					Message:  a.ETHAddr.Bytes(),
				})
			}
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
				bech32Addr, err := address.FormatBech32(hrp, a.LUXAddr[:])
				if err != nil {
					return nil, ids.Empty, fmt.Errorf("failed to format bech32 address for staker allocation: %w", err)
				}
				allocations = append(allocations, genesis.Allocation{
					Locktime: unlock.Locktime,
					Amount:   unlock.Amount,
					Address:  bech32Addr,
					Message:  a.ETHAddr.Bytes(),
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
		})
	}

	// Specify the chains
	chains := []genesis.Chain{
		{
			GenesisData: xvmGenesisBytes,
			ChainID:       constants.PrimaryNetworkID,
			VMID:        constants.XVMID,
			FxIDs: []ids.ID{
				secp256k1fx.ID,
				nftfx.ID,
				propertyfx.ID,
			},
			Name: "X-Chain",
		},
		{
			GenesisData: []byte(config.CChainGenesis),
			ChainID:       constants.PrimaryNetworkID,
			VMID:        constants.EVMID,
			Name:        "C-Chain",
		},
	}

	pChainGenesis, err := genesis.New(
		luxAssetID,
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
	return pChainGenesisBytes, luxAssetID, nil
}

// FromFile loads genesis config from file and builds genesis bytes
func FromFile(networkID uint32, filepath string, stakingCfg *StakingConfig, allowCustomGenesis bool) ([]byte, ids.ID, error) {
	// Protect standard networks from custom genesis unless explicitly allowed
	if !allowCustomGenesis {
		switch networkID {
		case constants.MainnetID, constants.TestnetID:
			return nil, ids.Empty, fmt.Errorf(
				"%w: %s",
				errOverridesStandardNetworkConfig,
				constants.NetworkName(networkID),
			)
		}
	}

	config, err := genesiscfg.GetConfigFile(filepath)
	if err != nil {
		return nil, ids.Empty, fmt.Errorf("failed to load genesis file %s: %w", filepath, err)
	}
	if err := validateConfig(networkID, config, stakingCfg); err != nil {
		return nil, ids.Empty, fmt.Errorf("genesis config validation failed: %w", err)
	}
	return FromConfig(config)
}

// FromFlag parses base64-encoded genesis content and builds genesis bytes
func FromFlag(networkID uint32, genesisContent string, stakingCfg *StakingConfig, allowCustomGenesis bool) ([]byte, ids.ID, error) {
	// Protect standard networks from custom genesis unless explicitly allowed
	if !allowCustomGenesis {
		switch networkID {
		case constants.MainnetID, constants.TestnetID:
			return nil, ids.Empty, fmt.Errorf(
				"%w: %s",
				errOverridesStandardNetworkConfig,
				constants.NetworkName(networkID),
			)
		}
	}

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
	config := genesiscfg.GetConfig(constants.CustomID)
	config.NetworkID = networkID
	config.Message = "DATABASE_REPLAY_MODE"
	return FromConfig(config)
}

// VMGenesis returns the genesis tx for a specific VM
func VMGenesis(genesisBytes []byte, vmID ids.ID) (*pchaintxs.Tx, error) {
	gen, err := genesis.Parse(genesisBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse genesis: %w", err)
	}
	for _, chain := range gen.Chains {
		uChain := chain.Unsigned.(*pchaintxs.CreateChainTx)
		if uChain.VMID == vmID {
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
		switch uChain.VMID {
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
		}
	}
	return apiAliases, chainAliases, nil
}

// XAssetID returns the LUX asset ID from XVM genesis bytes
func XAssetID(xvmGenesisBytes []byte) (ids.ID, error) {
	parser, err := xchaintxs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	if err != nil {
		return ids.Empty, err
	}

	genesisCodec := parser.GenesisCodec()
	gen := exchangevm.Genesis{}
	if _, err := genesisCodec.Unmarshal(xvmGenesisBytes, &gen); err != nil {
		return ids.Empty, err
	}

	if len(gen.Txs) == 0 {
		return ids.Empty, errNoTxs
	}
	genesisTx := gen.Txs[0]

	tx := xchaintxs.Tx{Unsigned: &genesisTx.CreateAssetTx}
	if err := tx.Initialize(genesisCodec); err != nil {
		return ids.Empty, err
	}
	return tx.ID(), nil
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
	NodeID        ids.NodeID        // The validator node ID
	BLSPublicKey  string            // BLS public key hex
	BLSPopProof   string            // BLS proof of possession hex
	RewardAddress ids.ShortID       // Reward/allocation address
	CChainGenesis string            // C-Chain genesis JSON
}

// ForDevMode creates a genesis configuration suitable for single-node development mode.
// It creates a single validator with far-future stake time and funds the treasury address.
func ForDevMode(cfg DevModeConfig, stakingCfg *StakingConfig) ([]byte, ids.ID, error) {
	// Genesis start time: now
	startTime := uint64(time.Now().Unix())
	
	// Far-future stake duration: 100 years in seconds
	// This ensures the validator never expires during development
	const hundredYears = 100 * 365 * 24 * 60 * 60
	
	// Create allocation for the reward address
	// Initial staked amount: 1B LUX (enough to be a validator)
	const oneMillionLUX = 1_000_000_000_000_000 // 1M LUX in nLUX
	const oneBillionLUX = 1_000_000_000_000_000_000 // 1B LUX in nLUX
	
	allocation := genesiscfg.Allocation{
		ETHAddr:       cfg.RewardAddress, // Same as LUX addr for simplicity
		LUXAddr:       cfg.RewardAddress,
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
		NetworkID:                  constants.CustomID,
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
