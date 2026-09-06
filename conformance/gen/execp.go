// SPDX-License-Identifier: BSD-3-Clause-Eco

// Executing a P-chain transaction, in Go, against a chain that holds nothing.
//
// The state here is the Go node's OWN state — `state.New` over an in-memory
// database, seeded by a genesis the Go genesis package built. Nothing about it
// is a stand-in: it is the same type the running node executes against, which
// is the only reason its verdict is worth comparing to anyone else's.
//
// It holds no UTXO the corpus spends and no validator the corpus names, and it
// is meant to. A funded state would have to be built three times, once per
// language, and the differential would then be measuring three state builders
// instead of three chains. What an empty chain still separates is the verdict
// CLASS — and a chain that refuses a whole transaction kind by name answers
// UNSUPPORTED where a chain that tries to execute it answers FUNDS. That
// difference is exactly the shape of a fork.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/address"
	"github.com/luxfi/constants"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/platformvm/utxo"
	pvalidators "github.com/luxfi/node/vms/platformvm/validators"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/util"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
)

// chainOnce builds the state and backend once; every vector is executed on a
// fresh diff over it, so no vector can see another's writes.
var (
	chainOnce sync.Once
	chainErr  error
	chainSt   state.State
	chainBk   *executor.Backend
	chainFee  fee.Calculator
)

// The staking terms the corpus is judged under. They are the same numbers the
// Rust and C++ evaluators are configured with, so a stake the corpus offers is
// weighed on one set of rules rather than three.
const (
	minValidatorStake = 1
	maxValidatorStake = uint64(1) << 60
	minDelegatorStake = 1
	minDelegationFee  = 20_000
	minStakeDuration  = 24 * time.Hour
	maxStakeDuration  = 365 * 24 * time.Hour
	uptimePercentage  = 0.8
)

func buildChain() {
	genesisBytes, err := emptyGenesis()
	if err != nil {
		chainErr = fmt.Errorf("genesis: %w", err)
		return
	}

	reg := metric.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		chainErr = fmt.Errorf("metrics: %w", err)
		return
	}
	execCfg, err := config.GetConfig(nil)
	if err != nil {
		chainErr = fmt.Errorf("execution config: %w", err)
		return
	}
	rewards := reward.NewCalculator(reward.Config{
		MaxConsumptionRate: 120_000,
		MinConsumptionRate: 100_000,
		MintingPeriod:      365 * 24 * time.Hour,
		SupplyCap:          720 * constants.MegaLux,
	})

	st, err := state.New(
		memdb.New(),
		genesisBytes,
		reg,
		validators.NewManager(),
		upgrade.GetConfig(constants.UnitTestID),
		execCfg,
		rt(),
		m,
		rewards,
	)
	if err != nil {
		chainErr = fmt.Errorf("state: %w", err)
		return
	}

	clk := &mockable.Clock{}
	clk.Set(time.Unix(genesisTime, 0))

	// secp256k1fx's own minimal host: the fx needs a clock and a log and
	// nothing else, and the library names that shape itself.
	fx := &secp256k1fx.Fx{}
	if err := fx.Initialize(&secp256k1fx.TestVM{Clk: clk, Log: log.Noop()}); err != nil {
		chainErr = fmt.Errorf("fx: %w", err)
		return
	}
	if err := fx.Bootstrapped(); err != nil {
		chainErr = fmt.Errorf("fx bootstrapped: %w", err)
		return
	}

	bootstrapped := &utils.Atomic[bool]{}
	bootstrapped.Set(true)

	internal := &config.Internal{
		Validators:         validators.NewManager(),
		DynamicFeeConfig:   builder.LocalDynamicFeeConfig,
		ValidatorFeeConfig: builder.LocalValidatorFeeConfig,
		MinValidatorStake:  minValidatorStake,
		MaxValidatorStake:  maxValidatorStake,
		MinDelegatorStake:  minDelegatorStake,
		MinDelegationFee:   minDelegationFee,
		UptimePercentage:   uptimePercentage,
		MinStakeDuration:   minStakeDuration,
		MaxStakeDuration:   maxStakeDuration,
		RewardConfig: reward.Config{
			MaxConsumptionRate: 120_000,
			MinConsumptionRate: 100_000,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * constants.MegaLux,
		},
		UpgradeConfig:          upgrade.GetConfig(constants.UnitTestID),
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
	}

	// The chain identity the executor reads, with the P-chain's OWN validator
	// manager bound to it. Import and Export ask it which network a peer chain
	// belongs to; without it those two vectors would report the evaluator's
	// missing wiring as if it were the chain's verdict.
	chainRuntime := rt()
	chainRuntime.ValidatorState = pvalidators.NewManager(*internal, st, m, clk)

	chainSt = st
	chainBk = &executor.Backend{
		Config:       internal,
		Runtime:      chainRuntime,
		Clk:          clk,
		Fx:           fx,
		FlowChecker:  utxo.NewHandler(context.Background(), clk, fx),
		Uptimes:      internal.UptimeLockedCalculator,
		Rewards:      rewards,
		Bootstrapped: bootstrapped,
		Log:          log.Noop(),
	}
	chainFee = fee.NewDynamicCalculator(
		internal.DynamicFeeConfig.Weights,
		internal.DynamicFeeConfig.MinPrice,
	)
}

// emptyGenesis is a P-chain genesis with one allocation and one validator: the
// least a genesis may hold and still be one. It funds nothing the corpus
// spends — the corpus's inputs name UTXOs of their own — so every vector meets
// the same empty ledger.
func emptyGenesis() ([]byte, error) {
	addr, err := address.FormatBech32(constants.GetHRP(networkID), short(1).Bytes())
	if err != nil {
		return nil, err
	}
	pop, err := signer.NewProofOfPossession(blsKey(0x31))
	if err != nil {
		return nil, err
	}
	g, err := genesis.New(
		id(stakeAsset),
		networkID,
		[]genesis.Allocation{{Amount: 1_000_000, Address: addr}},
		[]genesis.PermissionlessValidator{{
			Validator: genesis.Validator{
				NodeID:    nodeID(200),
				StartTime: genesisTime,
				EndTime:   genesisTime + uint64(maxStakeDuration/time.Second),
				Weight:    1_000_000,
			},
			RewardOwner:        &genesis.Owner{Threshold: 1, Addresses: []string{addr}},
			ExactDelegationFee: minDelegationFee,
			Staked:             []genesis.Allocation{{Amount: 1_000_000, Address: addr}},
			Signer:             pop,
		}},
		nil,
		genesisTime,
		2_000_000,
		"conformance",
	)
	if err != nil {
		return nil, err
	}
	return g.Bytes()
}

// execPTx executes one transaction on a fresh diff over the empty chain and
// reports the verdict class the Go executor produced.
func execPTx(tx *txs.Tx) (string, string) {
	chainOnce.Do(buildChain)
	if chainErr != nil {
		return VInternal, trim(chainErr.Error())
	}

	diff, err := state.NewDiffOn(chainSt)
	if err != nil {
		return VInternal, trim(err.Error())
	}
	if _, _, _, err := executor.StandardTx(chainBk, chainFee, tx, diff); err != nil {
		return classify(err), trim(root(err).Error())
	}
	return VOK, "executed on the empty chain"
}
