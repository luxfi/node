// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/stakingparams"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
	"github.com/luxfi/node/vms/txs/auth"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
)

// Internal contains all of the parameters for the PlatformVM that are
// internally set by the node.
type Internal struct {
	// The node's chain manager
	Chains chains.Manager

	// Node's validator set maps netID -> validators of the net
	//
	// Invariant: The primary network's validator set should have been added to
	//            the manager before calling VM.Initialize.
	// Invariant: The primary network's validator set should be empty before
	//            calling VM.Initialize.
	Validators validators.Manager

	DynamicFeeConfig gas.Config

	ValidatorFeeConfig fee.Config

	// Provides access to the uptime manager as a thread safe data structure
	UptimeLockedCalculator uptime.LockedCalculator

	// True if the node is being run with staking enabled
	SybilProtectionEnabled bool

	// If true, only the P-chain will be instantiated on the primary network.
	PartialSyncPrimaryNetwork bool

	// Set of chains that this node is validating
	TrackedChains set.Set[ids.ID]

	// If true, track all chains automatically (useful for dev/test networks)
	TrackAllChains bool

	// The minimum amount of tokens one must bond to be a validator
	MinValidatorStake uint64

	// The maximum amount of tokens that can be bonded on a validator
	MaxValidatorStake uint64

	// Minimum stake, in µLUX, that can be delegated on the primary network
	MinDelegatorStake uint64

	// Minimum fee that can be charged for delegation
	MinDelegationFee uint32

	// UptimePercentage is the minimum uptime required to be rewarded for staking
	UptimePercentage float64

	// Minimum amount of time to allow a staker to stake
	MinStakeDuration time.Duration

	// Maximum amount of time to allow a staker to stake
	MaxStakeDuration time.Duration

	// Config for the minting function
	RewardConfig reward.Config

	// All network upgrade timestamps
	UpgradeConfig upgrade.Config

	// UseCurrentHeight forces [GetMinimumHeight] to return the current height
	// of the P-Chain instead of the oldest block in the [recentlyAccepted]
	// window.
	//
	// This config is particularly useful for triggering proposervm activation
	// on recently created chains (without this, users need to wait for
	// [recentlyAcceptedWindowTTL] to pass for activation to occur).
	UseCurrentHeight bool

	// SecurityProfile pins the chain-wide credential-admission policy
	// resolved at node bootstrap from the genesis SecurityProfile block
	// (F102). The platformvm threads this into its mempool builder via
	// SetAuthPolicy so strict-PQ chains refuse classical secp256k1
	// credentials at gossip time. Nil for legacy (classical-compat)
	// networks that pre-date the locked-profile pin.
	SecurityProfile *consensusconfig.ChainSecurityProfile

	// ClassicalCompatRegistry names the allow-list of legacy operators
	// that may still post classical secp256k1 credentials under a
	// classical-compat fork profile. Nil for strict-PQ (the empty
	// registry refuses everything classical) and for legacy networks
	// (the mempool gate is bypassed entirely).
	ClassicalCompatRegistry auth.ClassicalCompatRegistry

	// StakingParams is the governed primary-network staking policy, as an
	// append-only history of activations. Empty means ungoverned: the
	// compiled-in fields above are used verbatim, which is exactly today's
	// behaviour — so a node that carries no history behaves identically to one
	// built before this field existed.
	//
	// Every primary-network staking rule is read through StakingPolicyAt, so
	// replacing the source of this history — config today, P-Chain state once
	// the proposal tx lands — changes nothing else.
	StakingParams stakingparams.History
}

// StakingPolicyAt returns the primary-network staking policy that binds a party
// which committed at unix time t.
//
// The time argument is the whole non-retroactivity guarantee, and callers must
// pass the moment the party bound itself, not the moment of the check:
//
//   - Admission (getValidatorRules, getDelegatorRules) passes the current chain
//     time, because a joiner is choosing to accept the rules in force now.
//   - The reward gate (block/executor prefersCommit) passes the staker's
//     StartTime, because a validator that bonded under an 80% uptime rule must
//     be judged at 80% however stake votes afterwards.
//
// Getting that second call wrong would make the uptime requirement retroactive
// and hand a stake majority the power to raise the bar the day before a rival's
// stake matures and take its reward.
func (c *Internal) StakingPolicyAt(t int64) stakingparams.Params {
	if len(c.StakingParams) == 0 {
		return stakingparams.Params{
			MinValidatorStake: c.MinValidatorStake,
			MaxValidatorStake: c.MaxValidatorStake,
			MinStakeDuration:  uint32(c.MinStakeDuration / time.Second),
			MaxStakeDuration:  uint32(c.MaxStakeDuration / time.Second),
			MinDelegationFee:  c.MinDelegationFee,
			UptimeRequirement: uint32(c.UptimePercentage * reward.PercentDenominator),
		}
	}
	return c.StakingParams.At(t)
}

// Create the blockchain described in [tx], but only if this node is a member of
// the chain that validates the blockchain.
//
// P-only primary network: P-Chain is the sole mandatory chain (created out
// of band by the chain manager at startup). EVERY other chain — X-Chain
// (XVM), C-Chain (EVM), Q-Chain (Quantum), D-Chain (DEX), and any
// per-chain blockchain — opt-in via --track-chains / --track-all
// or by being present in platform genesis. Operators bring up exactly what
// their workload needs, nothing more.
func (c *Internal) CreateChain(blockchainID ids.ID, tx *txs.CreateChainTx) {
	if c.SybilProtectionEnabled &&
		!c.TrackAllChains && // Not tracking all chains automatically
		!c.TrackedChains.Contains(tx.ChainID()) && // Check if chain ID is tracked
		!c.TrackedChains.Contains(blockchainID) { // Check if blockchain ID is tracked
		return
	}

	chainParams := chains.ChainParameters{
		ID:          blockchainID,
		ChainID:     tx.ChainID(),
		GenesisData: tx.GenesisData(),
		VMID:        tx.VMID(),
		FxIDs:       tx.FxIDs(),
		Name:        tx.BlockchainName(),
	}

	c.Chains.QueueChainCreation(chainParams)
}
