// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/proto/p2p"
)

// chain.go answers, at handshake time, a question the handshake never asked:
// not "are we on the same network" — NetworkId already settles that — but "when
// we both say chain D, do we mean the same chain".
//
// Genesis comes only from the creation record. Local storage and roots commit to
// it. Peer handshakes report it. Absence means unknown, for backwards
// compatibility; explicit disagreement isolates only that peer's traffic for that
// one chain. Peers can reveal a configuration error, and can never redefine chain
// identity.
//
// A VM can prove its own three views of genesis agree (the creation record it was
// handed, what its binary derives, what its database holds) and still be alone.
// That check is node-local, and node-local consistency cannot see a peer. Two
// validators handed different creation documents each pass their own check, each
// look healthy, and neither learns anything is wrong until one builds a block the
// other cannot accept — at which point the fork already exists and the logs of
// both nodes say nothing.
//
// So each node states, per blockchain, which chain it is running, and a peer
// whose statement differs is excluded FROM THAT BLOCKCHAIN. Not disconnected:
// a peer on a different D-Chain is still a perfectly good peer for P, C, X and Q,
// and dropping the connection would let one misconfigured validator remove itself
// from every chain at once. Not halting either — the local chain keeps running
// with the peers that do agree, because a node that stops when it meets a
// stranger hands any stranger the ability to stop it.
//
// WHAT THIS IS NOT. This is not Byzantine-proof identity enforcement, and no
// decision should be built on reading it as one. The field is optional, so a
// malicious peer simply omits it and presents as Unknown — which is permitted, by
// design, because Unknown is also what every not-yet-upgraded node looks like.
// The guarantee is exactly:
//
//	a peer that DECLARES a different chain is isolated early, on that chain
//
// and specifically NOT:
//
//	every peer running a different chain is identified by the handshake
//
// Consensus is still the safety boundary — nodes on different histories do not
// agree whatever they say here. What this buys is that honest misconfiguration
// fails early, on the handshake, naming both digests and the peer, instead of
// late and silently as a fork nobody can explain.
//
// FUTURE WORK, deliberately not built. A second phase could make the field
// mandatory, so that a validator declaring no digest is Incompatible rather than
// Unknown, which would close the omission above. That is a compatibility change
// and must be scheduled as one — announced, dated, and applied by every node at
// the same point. It must never be inferred from "surely everyone has upgraded by
// now", and there is no switch here to flip early precisely so that nobody can.

// GenesisDigest is a chain's identity, taken over the creation record exactly as
// delivered.
//
//	keccak256( "lux/chain-genesis/v1" ‖ record )
//
// Opaque end to end: never parsed, normalized, or re-encoded. The live D-Chain
// records prove why — they differ in whether they end with a newline, and they
// spell the em dash as six ASCII characters rather than as UTF-8. Anything that
// decodes and re-encodes the JSON produces different bytes, a different digest,
// and quarantines exactly the fleets this is meant to protect.
//
// The tag names what is hashed, not who hashes it: the same construction covers
// P, C, X, Q and D, and which VM the chain runs is carried by VMID, separately.
func GenesisDigest(record []byte) [32]byte {
	return hash.ComputeKeccak256Array([]byte("lux/chain-genesis/v1"), record)
}

// RulesDigest is the rule generation a chain is configured with:
//
//	keccak256( "lux/chain-rules/v1" ‖ canonical(upgrade) )
//
// upgrade is the chain's upgrade bytes — which consensus rules it applies and
// when. It is NOT a binary version and NOT a build id: two different binaries can
// implement the same rules and must be able to run together, which is the whole
// reason this is separate from the genesis digest and is never enforced.
//
// Unlike the genesis digest this hashes a canonical form, and the difference is
// the point. A genesis digest answers "are these the same bytes", so the bytes
// are hashed exactly as they arrived. A rules digest answers "do these schedule
// the same thing", so two files that schedule the same activations must agree
// however they were written — key order, indentation and line endings are not
// consensus. Hashing them raw would report a fork every time someone reformatted
// a config.
//
// Canonical form is the JSON re-encoded from parsed values, which sorts object
// keys and drops all whitespace. Numbers keep their exact written form rather
// than passing through float64: an activation height silently rounded is a
// scheduled fork this would then fail to report, and that is a worse failure than
// reporting a cosmetic 1 vs 1.0. Bytes that are not JSON are hashed as they are —
// the value is still stable per node, which is all a reported signal needs.
//
// The chain's config bytes are deliberately excluded. They legitimately differ
// between nodes — listen addresses, log levels, cache sizes — and a signal that
// fires on log level is noise wearing a signal's clothes.
//
// Zero when a chain schedules no rules. That is a real state, not a missing one.
func RulesDigest(upgrade []byte) [32]byte {
	if len(upgrade) == 0 {
		return [32]byte{}
	}
	return hash.ComputeKeccak256Array([]byte("lux/chain-rules/v1"), canonicalJSON(upgrade))
}

// canonicalJSON re-encodes JSON from its parsed values, which sorts object keys
// and removes insignificant whitespace. Returns the input unchanged when it does
// not parse.
func canonicalJSON(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep numeric literals exact; see RulesDigest
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	// Trailing content means this was not one JSON value; hash what arrived.
	if dec.More() {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// ChainState is what a peer said about ONE chain. It is always read as a pair,
// (peer, chainID) — never as a property of the peer — because the whole value of
// excluding at the chain is that a D-Chain misconfiguration stays a D-Chain
// problem instead of becoming a change in who is connected to whom.
type ChainState uint8

const (
	// ChainUnknown — the peer said nothing about this chain: it does not run
	// the chain, or it predates the field. Traffic is PERMITTED. Observable,
	// never grounds for exclusion; see the note on rolling upgrades in
	// (*peer).compareChains.
	ChainUnknown ChainState = iota
	// ChainCompatible — the peer stated the same chain we run. Normal traffic.
	ChainCompatible
	// ChainIncompatible — the peer stated a DIFFERENT chain. Traffic for this
	// chain is dropped in both directions; every other chain on the connection
	// is untouched.
	ChainIncompatible
)

func (s ChainState) String() string {
	switch s {
	case ChainCompatible:
		return "compatible"
	case ChainIncompatible:
		return "incompatible"
	default:
		return "unknown"
	}
}

// ChainIdentity is everything two nodes must agree about before they can run a
// blockchain together, plus the one thing they need only compare.
type ChainIdentity struct {
	NetworkID uint32
	ChainID   ids.ID
	VMID      ids.ID
	Genesis   [32]byte
	Rules     [32]byte
}

// Agrees reports whether two nodes are running the same chain.
//
// Every field here is creation data: it comes from the network's genesis or from
// the CreateChainTx that created the blockchain, so two honest nodes on one chain
// derive identical values with no coordination. A difference in any of them means
// the two are not on the same chain whatever their chain ids match.
//
// Rules is NOT part of this. It is a claim about the peer's binary that the
// receiver cannot verify, and acting on an unverifiable claim would let a peer
// choose whether we participate.
func (c ChainIdentity) Agrees(o ChainIdentity) bool {
	return c.NetworkID == o.NetworkID &&
		c.ChainID == o.ChainID &&
		c.VMID == o.VMID &&
		c.Genesis == o.Genesis
}

// Disagreement names the first field that differs, for an operator who has to
// find which of two nodes is the misconfigured one. Empty when they agree.
func (c ChainIdentity) Disagreement(o ChainIdentity) string {
	switch {
	case c.NetworkID != o.NetworkID:
		return fmt.Sprintf("network %d vs %d", c.NetworkID, o.NetworkID)
	case c.ChainID != o.ChainID:
		return fmt.Sprintf("chain %s vs %s", c.ChainID, o.ChainID)
	case c.VMID != o.VMID:
		return fmt.Sprintf("vm %s vs %s", c.VMID, o.VMID)
	case c.Genesis != o.Genesis:
		return fmt.Sprintf("genesis %x vs %x", c.Genesis, o.Genesis)
	}
	return ""
}

// wire converts to the handshake representation.
func (c ChainIdentity) wire() *p2p.ChainIdentity {
	w := &p2p.ChainIdentity{
		NetworkId:     c.NetworkID,
		ChainId:       c.ChainID[:],
		VmId:          c.VMID[:],
		GenesisDigest: c.Genesis[:],
	}
	if c.Rules != ([32]byte{}) {
		w.RulesId = c.Rules[:]
	}
	return w
}

// parseChainIdentity reads one identity off the handshake. Widths are checked
// because they arrive from a peer: a short chain id would otherwise be padded
// into a different chain that happens to compare equal to nothing.
func parseChainIdentity(w *p2p.ChainIdentity) (ChainIdentity, error) {
	if w == nil {
		return ChainIdentity{}, fmt.Errorf("chain identity is absent")
	}
	chainID, err := ids.ToID(w.GetChainId())
	if err != nil {
		return ChainIdentity{}, fmt.Errorf("chain id: %w", err)
	}
	vmID, err := ids.ToID(w.GetVmId())
	if err != nil {
		return ChainIdentity{}, fmt.Errorf("vm id: %w", err)
	}
	c := ChainIdentity{NetworkID: w.GetNetworkId(), ChainID: chainID, VMID: vmID}
	if d := w.GetGenesisDigest(); len(d) != len(c.Genesis) {
		return ChainIdentity{}, fmt.Errorf("genesis digest is %d bytes, want %d", len(d), len(c.Genesis))
	} else {
		copy(c.Genesis[:], d)
	}
	// Rules is optional: a chain that schedules none sends nothing.
	if r := w.GetRulesId(); len(r) > 0 {
		if len(r) != len(c.Rules) {
			return ChainIdentity{}, fmt.Errorf("rules id is %d bytes, want %d", len(r), len(c.Rules))
		}
		copy(c.Rules[:], r)
	}
	return c, nil
}

// Chains is the identity of every blockchain this node runs.
//
// Written by the chain manager as chains start — including long after boot, when
// a CreateChainTx is accepted — and read by every peer goroutine as it builds its
// handshake, so it carries its own lock rather than borrowing the network's.
type Chains struct {
	lock sync.RWMutex
	m    map[ids.ID]ChainIdentity
}

// Add records a chain this node is running. Called once per chain, as it starts.
func (c *Chains) Add(id ChainIdentity) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.m == nil {
		c.m = make(map[ids.ID]ChainIdentity)
	}
	c.m[id.ChainID] = id
}

// Get returns this node's identity for a chain, and whether it runs it at all.
func (c *Chains) Get(chainID ids.ID) (ChainIdentity, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	id, ok := c.m[chainID]
	return id, ok
}

// List returns every chain this node runs, for the handshake.
func (c *Chains) List() []ChainIdentity {
	c.lock.RLock()
	defer c.lock.RUnlock()
	out := make([]ChainIdentity, 0, len(c.m))
	for _, id := range c.m {
		out = append(out, id)
	}
	return out
}
