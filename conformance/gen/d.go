// SPDX-License-Identifier: BSD-3-Clause-Eco

// The D-chain half of the corpus, and the Go reference's answers for it.
//
// D is not a block chain in this corpus's sense: `chains/dexvm` is a REGISTRY,
// and what it decides is identity and admission — what an asset IS, what a
// market IS, which asset kinds may be registered, and whether native value may
// activate at all. Those are consensus decisions with no wire of their own: two
// implementations deriving different bytes for one asset have forked the value
// plane without ever disagreeing about a transaction.
//
// So a D vector's WIRE column carries the inputs, not a serialization. The op
// says which function is being asked, and the inputs are pipe-separated ASCII —
// one line, no format library, readable by a `split` in any of the three
// languages. A derivation's argument list is an argument list; encoding it as a
// block would be inventing a wire that no chain writes.
//
//	assetid   networkID | sourceChainHex | kindToken | refHex
//	marketid  networkID | baseHex | quoteHex | venueHex
//	kind      token
//	mode      token
//	class     networkID
//	label     text
//	guard     valueEnabled | modeToken | capsOn | realAssetsOnly | haltReady
//
// The result fields carry what the reference answered:
//
//	kind       the function's own name for what it produced
//	id         the derived identity, where there is one
//	syntactic  whether the inputs are admissible
//	exec       what the function returned, refusal class or OK
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/luxfi/chains/dexvm/registry"
	"github.com/luxfi/ids"
)

// The chain ids the D vectors derive against: a C-chain for the EVM kinds and
// an X-chain for the UTXO kind, which is the pairing the reference documents.
const (
	dCChain = 3
	dXChain = 2
)

func dArgs(parts ...string) []byte { return []byte(strings.Join(parts, "|")) }

func hexID32(b byte) string { i := id(b); return hex.EncodeToString(i[:]) }

func dVectors() []Vector {
	// The three admissible kinds, each with the reference it is defined over:
	// the native marker, a 20-byte contract address, a 32-byte source asset id.
	native := hex.EncodeToString(make([]byte, 20)) // address zero, the native marker
	erc20 := hex.EncodeToString(zBytes(0xC0, 20))
	utxoRef := hexID32(0x50)

	v := []Vector{
		vec("D_ASSETID_EVM_NATIVE", "D", "assetid",
			dArgs("1", hexID32(dCChain), "EVM_NATIVE", native)),
		vec("D_ASSETID_ERC20", "D", "assetid",
			dArgs("1", hexID32(dCChain), "ERC20", erc20)),
		vec("D_ASSETID_UTXO", "D", "assetid",
			dArgs("1", hexID32(dXChain), "UTXO", utxoRef)),

		// The same asset on a different network, and on a different source
		// chain. Both must derive a DIFFERENT id: an identity that ignored
		// either would let one registration name two assets.
		vec("D_ASSETID_OTHER_NETWORK", "D", "assetid",
			dArgs("2", hexID32(dCChain), "ERC20", erc20)),
		vec("D_ASSETID_OTHER_CHAIN", "D", "assetid",
			dArgs("1", hexID32(dCChain+1), "ERC20", erc20)),

		// Refusals. Each is a correct call with one thing wrong.
		vec("D_ASSETID_EMPTY_CHAIN", "D", "assetid",
			dArgs("1", hexID32(0), "ERC20", erc20)),
		vec("D_ASSETID_INVALID_KIND", "D", "assetid",
			dArgs("1", hexID32(dCChain), "INVALID", erc20)),
		vec("D_ASSETID_SHORT_ERC20_REF", "D", "assetid",
			dArgs("1", hexID32(dCChain), "ERC20", hex.EncodeToString(zBytes(0xC0, 19)))),
		// The native coin's reference is address zero and nothing else. A
		// non-zero one here would let a token masquerade as the coin.
		vec("D_ASSETID_NATIVE_WITH_REF", "D", "assetid",
			dArgs("1", hexID32(dCChain), "EVM_NATIVE", erc20)),
		vec("D_ASSETID_ZERO_ERC20_REF", "D", "assetid",
			dArgs("1", hexID32(dCChain), "ERC20", hex.EncodeToString(make([]byte, 20)))),
		vec("D_ASSETID_SHORT_UTXO_REF", "D", "assetid",
			dArgs("1", hexID32(dXChain), "UTXO", hex.EncodeToString(zBytes(0x50, 31)))),

		vec("D_MARKETID", "D", "marketid",
			dArgs("1", hexID32(0x10), hexID32(0x11), hex.EncodeToString([]byte("tick=1,lot=1")))),
		// The same pair, the other way round. A market identity that folded
		// its two assets symmetrically would name one market for both sides.
		vec("D_MARKETID_REVERSED", "D", "marketid",
			dArgs("1", hexID32(0x11), hexID32(0x10), hex.EncodeToString([]byte("tick=1,lot=1")))),
		// The same pair at a different venue.
		vec("D_MARKETID_OTHER_VENUE", "D", "marketid",
			dArgs("1", hexID32(0x10), hexID32(0x11), hex.EncodeToString([]byte("tick=2,lot=1")))),
		vec("D_MARKETID_OTHER_NETWORK", "D", "marketid",
			dArgs("2", hexID32(0x10), hexID32(0x11), hex.EncodeToString([]byte("tick=1,lot=1")))),

		vec("D_KIND_EVM_NATIVE", "D", "kind", dArgs("EVM_NATIVE")),
		vec("D_KIND_ERC20", "D", "kind", dArgs("ERC20")),
		vec("D_KIND_UTXO", "D", "kind", dArgs("UTXO")),
		vec("D_KIND_INVALID", "D", "kind", dArgs("INVALID")),
		vec("D_KIND_TICKER", "D", "kind", dArgs("LUX")),
		vec("D_KIND_EMPTY", "D", "kind", dArgs("")),

		vec("D_MODE_QUORUM_FINALITY", "D", "mode", dArgs("QUORUM_FINALITY")),
		vec("D_MODE_HONEST_VALIDATOR_LABELED", "D", "mode", dArgs("HONEST_VALIDATOR_LABELED")),
		vec("D_MODE_UNSET", "D", "mode", dArgs("UNSET")),
		vec("D_MODE_UNKNOWN", "D", "mode", dArgs("BFT")),

		vec("D_CLASS_MAINNET", "D", "class", dArgs("1")),
		vec("D_CLASS_TESTNET", "D", "class", dArgs("2")),
		vec("D_CLASS_LOCAL", "D", "class", dArgs("3")),
		vec("D_CLASS_LOCALNET", "D", "class", dArgs("1337")),

		// Value activation. The default arm refuses, so an unmodelled mode can
		// never accidentally authorise value — which is the one decision on
		// this chain that moves real money.
		vec("D_GUARD_OFF", "D", "guard", dArgs("false", "UNSET", "false", "false", "false")),
		vec("D_GUARD_QUORUM", "D", "guard", dArgs("true", "QUORUM_FINALITY", "false", "false", "false")),
		vec("D_GUARD_LABELED_FULL", "D", "guard", dArgs("true", "HONEST_VALIDATOR_LABELED", "true", "true", "true")),
		vec("D_GUARD_LABELED_NO_CAPS", "D", "guard", dArgs("true", "HONEST_VALIDATOR_LABELED", "false", "true", "true")),
		vec("D_GUARD_LABELED_NO_HALT", "D", "guard", dArgs("true", "HONEST_VALIDATOR_LABELED", "true", "true", "false")),
		vec("D_GUARD_UNSET", "D", "guard", dArgs("true", "UNSET", "true", "true", "true")),
	}

	dAssert(v)
	return v
}

// dAssert holds the emit to the claims the D vectors rest on: the identities
// are distinct where they must be, and the value guard refuses by default.
func dAssert(v []Vector) {
	by := map[string]Result{}
	for _, vec := range v {
		by[vec.ID] = evalD(vec)
	}
	distinct := map[string]string{}
	for _, name := range []string{
		"D_ASSETID_EVM_NATIVE", "D_ASSETID_ERC20", "D_ASSETID_UTXO",
		"D_ASSETID_OTHER_NETWORK", "D_ASSETID_OTHER_CHAIN",
		"D_MARKETID", "D_MARKETID_REVERSED", "D_MARKETID_OTHER_VENUE",
		"D_MARKETID_OTHER_NETWORK",
	} {
		got := by[name]
		if got.Exec != VOK {
			panic(fmt.Sprintf("%s was expected to derive an identity: %s", name, got.Note))
		}
		if prev, dup := distinct[got.Hash]; dup {
			panic(fmt.Sprintf("%s and %s derive the same identity", prev, name))
		}
		distinct[got.Hash] = name
	}
	if by["D_GUARD_UNSET"].Exec == VOK {
		panic("D_GUARD_UNSET authorised value under an unset consensus mode")
	}
	if by["D_GUARD_LABELED_FULL"].Exec != VOK {
		panic("D_GUARD_LABELED_FULL was refused with the whole bundle asserted: " +
			by["D_GUARD_LABELED_FULL"].Note)
	}
}

// dInput reads a vector's argument list back out of the wire column.
func dInput(v Vector) ([]string, error) {
	b, ok := wireOf(v)
	if !ok {
		return nil, errors.New("corpus wire is not hex")
	}
	return strings.Split(string(b), "|"), nil
}

func dU32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	return uint32(n), err
}

func dID(s string) (ids.ID, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ids.Empty, err
	}
	if len(b) != 32 {
		return ids.Empty, fmt.Errorf("%d bytes, want 32", len(b))
	}
	return ids.ID(b), nil
}

func dBool(s string) (bool, error) { return strconv.ParseBool(s) }

func evalD(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	args, err := dInput(v)
	if err != nil {
		r.Parse = VInternal
		r.Note = err.Error()
		return r
	}
	r.Parse = "ok"

	switch v.Op {
	case "assetid":
		return dAssetID(r, args)
	case "marketid":
		return dMarketID(r, args)
	case "kind":
		return dKind(r, args)
	case "mode":
		return dMode(r, args)
	case "class":
		return dClass(r, args)
	case "guard":
		return dGuard(r, args)
	}
	r.Parse = VInternal
	r.Syntactic = VInternal
	r.Exec = VInternal
	r.Note = "unknown op " + v.Op
	return r
}

// dBadInput is the answer for a vector whose ARGUMENTS do not read. It is the
// evaluator's own failure, not the chain's, and is reported as such rather than
// as a refusal the chain never made.
func dBadInput(r Result, why string) Result {
	r.Parse = VInternal
	r.Syntactic = VInternal
	r.Exec = VInternal
	r.Note = why
	return r
}

func dAssetID(r Result, args []string) Result {
	r.Kind = "AssetID"
	if len(args) != 4 {
		return dBadInput(r, fmt.Sprintf("assetid wants 4 arguments, got %d", len(args)))
	}
	network, err := dU32(args[0])
	if err != nil {
		return dBadInput(r, "network id: "+err.Error())
	}
	chain, err := dID(args[1])
	if err != nil {
		return dBadInput(r, "source chain: "+err.Error())
	}
	ref, err := hex.DecodeString(args[3])
	if err != nil {
		return dBadInput(r, "reference: "+err.Error())
	}

	// The kind token is the registry's own parser, so a token it refuses is a
	// refusal at the same layer a manifest would meet.
	kind, err := registry.ParseAssetKind(args[2])
	if err != nil {
		r.Syntactic = classify(err)
		r.Exec = r.Syntactic
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK

	assetID, err := registry.DeriveAssetID(network, chain, kind, ref)
	if err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Hash = hex.EncodeToString(assetID[:])
	r.Exec = VOK
	r.Note = "derived"
	return r
}

func dMarketID(r Result, args []string) Result {
	r.Kind = "MarketID"
	if len(args) != 4 {
		return dBadInput(r, fmt.Sprintf("marketid wants 4 arguments, got %d", len(args)))
	}
	network, err := dU32(args[0])
	if err != nil {
		return dBadInput(r, "network id: "+err.Error())
	}
	base, err := dID(args[1])
	if err != nil {
		return dBadInput(r, "base asset: "+err.Error())
	}
	quote, err := dID(args[2])
	if err != nil {
		return dBadInput(r, "quote asset: "+err.Error())
	}
	venue, err := hex.DecodeString(args[3])
	if err != nil {
		return dBadInput(r, "venue: "+err.Error())
	}
	r.Syntactic = VOK
	market := registry.MarketID(network, base, quote, venue)
	r.Hash = hex.EncodeToString(market[:])
	r.Exec = VOK
	r.Note = "derived"
	return r
}

func dKind(r Result, args []string) Result {
	r.Kind = "AssetKind"
	if len(args) != 1 {
		return dBadInput(r, fmt.Sprintf("kind wants 1 argument, got %d", len(args)))
	}
	kind, err := registry.ParseAssetKind(args[0])
	if err != nil {
		r.Syntactic = classify(err)
		r.Exec = r.Syntactic
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = kind.String()
	return r
}

func dMode(r Result, args []string) Result {
	r.Kind = "ConsensusMode"
	if len(args) != 1 {
		return dBadInput(r, fmt.Sprintf("mode wants 1 argument, got %d", len(args)))
	}
	mode, err := registry.ParseConsensusMode(args[0])
	if err != nil {
		r.Syntactic = classify(err)
		r.Exec = r.Syntactic
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = mode.String()
	return r
}

func dClass(r Result, args []string) Result {
	r.Kind = "NetworkClass"
	if len(args) != 1 {
		return dBadInput(r, fmt.Sprintf("class wants 1 argument, got %d", len(args)))
	}
	network, err := dU32(args[0])
	if err != nil {
		return dBadInput(r, "network id: "+err.Error())
	}
	r.Syntactic = VOK
	r.Exec = VOK
	r.Note = registry.NetworkClassFor(network).String()
	return r
}

func dGuard(r Result, args []string) Result {
	r.Kind = "ValueActivation"
	if len(args) != 5 {
		return dBadInput(r, fmt.Sprintf("guard wants 5 arguments, got %d", len(args)))
	}
	enabled, err := dBool(args[0])
	if err != nil {
		return dBadInput(r, "value enabled: "+err.Error())
	}
	caps, err := dBool(args[2])
	if err != nil {
		return dBadInput(r, "caps: "+err.Error())
	}
	real, err := dBool(args[3])
	if err != nil {
		return dBadInput(r, "real assets: "+err.Error())
	}
	halt, err := dBool(args[4])
	if err != nil {
		return dBadInput(r, "halt ready: "+err.Error())
	}

	// An unparseable mode token is UNSET, because that is what a caller who
	// declared nothing legible has declared — and UNSET is refused. Reading it
	// as an error here would report the parse rather than the guard.
	mode, err := registry.ParseConsensusMode(args[1])
	if err != nil {
		mode = registry.ConsensusModeUnset
	}
	r.Syntactic = VOK

	status, err := registry.GuardValueActivation(enabled, mode,
		registry.LaunchAssertions{CapsOn: caps, RealAssetsOnly: real, HaltReady: halt})
	if err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Exec = VOK
	r.Note = status.Mode.String() + " " + status.Status
	if status.Status == "" {
		r.Note = status.Mode.String() + " (no disclaimer)"
	}
	return r
}
