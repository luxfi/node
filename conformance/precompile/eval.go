// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/vm"
	luxcontract "github.com/luxfi/precompile/contract"
	"github.com/luxfi/precompile/modules"

	_ "github.com/luxfi/precompile/registry" // the Lux modules register themselves
)

// eval reads a corpus and prints what this implementation says about it.
func eval(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	out := bufio.NewWriter(w)
	defer out.Flush()

	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		v, err := ParseVector(line)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, run(v).Line())
	}
	return s.Err()
}

// enabled is the C-chain's precompileUpgrades — the ConfigKeys named in
// lux/genesis/configs/mainnet/upgrade.json, which is the set a mainnet node
// activates. Every Lux network's upgrade.json agrees with it on every key this
// corpus reaches.
//
// It is here because registration is not activation, and the distance between
// the two is what this evaluator used to answer with. Importing the registry
// links all fifty-six modules into this binary; a module reaches a call only
// where the chain names its key. LuxPrecompileOverrider.PrecompileOverride
// reads GetExtrasRules(...).Precompiles and returns nothing for an address
// whose key is unnamed — it never asks the registry whether the code is
// linked, and neither can an evaluator that means to answer as the chain does.
var enabled = map[string]bool{
	"aiMiningConfig":           true,
	"anchorConfig":             true,
	"attestationConfig":        true,
	"babyjubjubConfig":         true,
	"blake3Config":             true,
	"bls12381G1AddConfig":      true,
	"bls12381G1MSMConfig":      true,
	"bls12381G1MulConfig":      true,
	"bls12381G2AddConfig":      true,
	"bls12381G2MSMConfig":      true,
	"bls12381G2MulConfig":      true,
	"bls12381PairingConfig":    true,
	"bridgeRegistrarConfig":    true,
	"computeMarketConfig":      true,
	"curve25519Config":         true,
	"deadConfig":               true,
	"deadFullConfig":           true,
	"deadZeroConfig":           true,
	"dexPositionManagerConfig": true,
	"dexQuoterConfig":          true,
	"dexSettleConfig":          true,
	"dexStateViewConfig":       true,
	"fheConfig":                true,
	"fixedPointMathConfig":     true,
	"graphConfig":              true,
	"hpkeConfig":               true,
	"mlkemConfig":              true,
	"pastaConfig":              true,
	"pedersenConfig":           true,
	"poseidonConfig":           true,
	"rewardManagerConfig":      true,
	"ringConfig":               true,
	"routerConfig":             true,
	"stableSwapConfig":         true,
	"vrfConfig":                true,
	"warpConfig":               true,
	"x25519Config":             true,
	"xwingConfig":              true,
	"zkConfig":                 true,
}

// run answers one vector the way the chain would answer the call.
//
// The order is the chain's order, not a convenience: a Lux module the chain
// ENABLES shadows the stock precompile at its address, because that is what
// LuxPrecompileOverrider does before the standard table is consulted. Asking
// the stock table first would report a gas price no chain charges — and so
// would asking the registry, which holds modules no chain has turned on.
func run(v Vector) Result {
	address := common.HexToAddress("0x" + v.Address)

	if m, ok := modules.GetPrecompileModuleByAddress(address); ok && m.Contract != nil &&
		(m.AlwaysOn || enabled[m.ConfigKey]) {
		// The modules this corpus reaches deduct gas and delegate, and read
		// nothing, so there is no state to give them. One that does read state
		// cannot be compared against implementations that have no chain under
		// them either, and it says so instead of being answered for — and
		// instead of taking the run down, which is what a nil state does to
		// the dead-address module at 0x0.
		return runModule(v, address, m.ConfigKey)
	}

	// The stock set at the revision the C-chain runs. luxfi/evm maps its
	// Quasar fork to CancunTime and knows no fork after it — its config carries
	// no OsakaTime for a network to set, and none does — so Cancun is where
	// every Lux chain ends. A later table would serve addresses no validator
	// serves and price calls no validator prices.
	p, ok := vm.PrecompiledContractsCancun[address]
	if !ok {
		return Result{ID: v.ID, Status: ABSENT, Note: "no precompile at this address"}
	}
	output, remaining, err := vm.RunPrecompiledContract(p, v.Input, v.Gas, nil)
	return verdict(v, output, remaining, err, "geth:"+p.Name())
}

// runModule calls a Lux module, and survives one that wanted a chain.
func runModule(v Vector, address common.Address, key string) (r Result) {
	m, _ := modules.GetPrecompileModuleByAddress(address)
	defer func() {
		if p := recover(); p != nil {
			r = Result{ID: v.ID, Status: STATE, Note: "lux:" + key + ": reads chain state"}
		}
	}()
	output, remaining, err := m.Contract.Run(nil, address, address, v.Input, v.Gas, false)
	return verdict(v, output, remaining, err, "lux:"+key)
}

func verdict(v Vector, output []byte, remaining uint64, err error, who string) Result {
	r := Result{ID: v.ID, Output: output, Note: who}
	switch {
	// Two sentinels mean the same thing here. The stock precompiles return
	// geth's and the Lux modules return their own, and an evaluator that knew
	// only one would report a refusal where the other two implementations
	// report a price it could not pay — a disagreement invented by the
	// harness rather than found by it.
	case errors.Is(err, vm.ErrOutOfGas), errors.Is(err, luxcontract.ErrOutOfGas):
		r.Status, r.Gas, r.Output = OOG, 0, nil
		r.Note = who + ": out of gas"
	case err != nil:
		// The charge stands. A precompile that read the input and refused it
		// has done the work of reading it, and every implementation here
		// charges for that — so the refusal is compared with its price, not
		// instead of it.
		r.Status, r.Gas, r.Output = FAILED, v.Gas-remaining, nil
		r.Note = who + ": " + err.Error()
	default:
		r.Status, r.Gas = OK, v.Gas-remaining
	}
	return r
}
