// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// quorum.go — the node-layer wiring that makes the consensus engine's α-of-K
// quorum-cert finality LIVE (HIGH-4). The consensus engine (luxfi/consensus
// engine/chain) defines the quorum RULE and the vote/cert topology interfaces;
// THIS file supplies the concrete node implementations the engine needs:
//
//   - blsVoteVerifier  — verifies a validator's BLS signature over the canonical
//     vote message against the chain's validator set (the engine is scheme-
//     agnostic; the node injects BLS).
//   - blsVoteSigner    — signs THIS node's accept votes with its staking BLS key
//     so its signature can be collected into a cert.
//   - validatorStakeSource — supplies per-validator stake so finality is a
//     ⅔-by-STAKE supermajority (HIGH-3), not a raw voter count.
//   - networkGossiper.BroadcastVote / GossipCert — the QuorumGossiper transport:
//     a follower broadcasts its signed vote to ALL validators and any node that
//     collects α distinct signed votes gossips the assembled cert, so finality
//     never hinges on one node's inbound Chits (liveness; no proposer-freeze).
//
// Inbound votes/certs arrive as app-gossip and are demuxed in blockHandler.Gossip
// (see manager.go) into engine.HandleIncomingVote / HandleIncomingCert.
package chains

import (
	"errors"

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
)

// selectConsensusParams picks the consensus parameters for a chain.
//
//   - sybilProtection == false (--dev / single-node): K=1, the sole validator's
//     accept is the 1-of-1 quorum (no peer signatures).
//   - sybilProtection == true (multi-node): a BYZANTINE-fault-tolerant param set
//     selected by network — NEVER LocalParams() (K=3/α=2, f=0, which a single
//     Byzantine validator forks; this was CRITICAL-2). Mainnet→K=21, Testnet→
//     K=11, every other multi-node net→Default K=20. All satisfy f≥1 and the
//     2α−K ≥ f+1 overlap bound (ValidateForValueNetwork is also asserted at the
//     call site as a fail-closed backstop).
func selectConsensusParams(sybilProtection bool, networkID uint32) consensusconfig.Parameters {
	if !sybilProtection {
		return consensusconfig.SingleValidatorParams()
	}
	switch networkID {
	case constants.MainnetID:
		return consensusconfig.MainnetParams()
	case constants.TestnetID:
		return consensusconfig.TestnetParams()
	default:
		return consensusconfig.DefaultParams()
	}
}

// --- BLS vote verifier -------------------------------------------------------

// blsVoteVerifier verifies a validator's BLS signature over the canonical vote
// message. The validator's BLS public key is looked up in the chain's validator
// set; an unknown validator, a validator with no BLS key, or a bad signature all
// yield false (never an error/panic) — a cert with such a voter is simply
// invalid, the fail-closed contract the engine requires.
type blsVoteVerifier struct {
	vdrs      validators.Manager
	networkID ids.ID
}

func newBLSVoteVerifier(vdrs validators.Manager, networkID ids.ID) *blsVoteVerifier {
	return &blsVoteVerifier{vdrs: vdrs, networkID: networkID}
}

// VerifyVote implements consensuschain.VoteVerifier.
func (v *blsVoteVerifier) VerifyVote(nodeID ids.NodeID, message []byte, sig []byte) bool {
	if v.vdrs == nil {
		return false
	}
	out, ok := v.vdrs.GetValidator(v.networkID, nodeID)
	if !ok || out == nil || len(out.PublicKey) == 0 {
		return false
	}
	pk, err := bls.PublicKeyFromCompressedBytes(out.PublicKey)
	if err != nil || pk == nil {
		return false
	}
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil || signature == nil {
		return false
	}
	return bls.Verify(pk, signature, message)
}

var _ consensuschain.VoteVerifier = (*blsVoteVerifier)(nil)

// --- BLS vote signer ---------------------------------------------------------

// blsVoteSigner signs this node's accept votes with its staking BLS key.
type blsVoteSigner struct {
	signer bls.Signer
}

func newBLSVoteSigner(signer bls.Signer) *blsVoteSigner {
	if signer == nil {
		return nil
	}
	return &blsVoteSigner{signer: signer}
}

// SignVote implements consensuschain.VoteSigner.
func (s *blsVoteSigner) SignVote(message []byte) ([]byte, error) {
	sig, err := s.signer.Sign(message)
	if err != nil {
		return nil, err
	}
	return bls.SignatureToBytes(sig), nil
}

var _ consensuschain.VoteSigner = (*blsVoteSigner)(nil)

// --- stake source (HIGH-3) ---------------------------------------------------

// validatorStakeSource supplies validator voting weights so the engine can
// require a ⅔-by-stake supermajority for finality (HIGH-3). Weights are read
// from the chain's validator set. (The set here is the current set; for a chain
// with epoch-versioned stake the height argument selects the epoch — the current
// node validator manager is single-epoch, so height is advisory.)
type validatorStakeSource struct {
	vdrs      validators.Manager
	networkID ids.ID
}

func newValidatorStakeSource(vdrs validators.Manager, networkID ids.ID) *validatorStakeSource {
	return &validatorStakeSource{vdrs: vdrs, networkID: networkID}
}

// Weight implements consensuschain.StakeSource.
func (s *validatorStakeSource) Weight(nodeID ids.NodeID, _ uint64) uint64 {
	if s.vdrs == nil {
		return 0
	}
	return s.vdrs.GetLight(s.networkID, nodeID)
}

// TotalStake implements consensuschain.StakeSource.
func (s *validatorStakeSource) TotalStake(_ uint64) uint64 {
	if s.vdrs == nil {
		return 0
	}
	total, err := s.vdrs.TotalLight(s.networkID)
	if err != nil {
		return 0
	}
	return total
}

var _ consensuschain.StakeSource = (*validatorStakeSource)(nil)

// --- app-gossip envelope for votes/certs -------------------------------------

// The QuorumGossiper transport rides on app-gossip. A single framed envelope
// carries either a signed vote or an assembled cert. The receiver (blockHandler
// .Gossip) demuxes on the magic + kind and routes to the engine. A payload
// without the magic is a plain block gossip (legacy Put path), so the demux is
// backward-compatible.
//
// Layout (big-endian):
//
//	magic:4 ("LXQ\x01")  kind:1  blockID:32  payload:...
//
// kind 1 = signed vote (payload = engine encodeSignedVote: nodeID+sig)
// kind 2 = finality cert (payload = engine cert MarshalBinary)
var quorumGossipMagic = [4]byte{'L', 'X', 'Q', 0x01}

const (
	quorumKindVote byte = 1
	quorumKindCert byte = 2
)

// ErrNotQuorumGossip signals a payload is not a quorum envelope (so the caller
// falls through to the block-gossip path).
var ErrNotQuorumGossip = errors.New("chains: not a quorum gossip envelope")

// encodeQuorumGossip frames a vote/cert payload for app-gossip.
func encodeQuorumGossip(kind byte, blockID ids.ID, payload []byte) []byte {
	buf := make([]byte, 0, 4+1+32+len(payload))
	buf = append(buf, quorumGossipMagic[:]...)
	buf = append(buf, kind)
	buf = append(buf, blockID[:]...)
	buf = append(buf, payload...)
	return buf
}

// decodeQuorumGossip parses an envelope. Returns ErrNotQuorumGossip if the magic
// is absent (a normal block gossip) — fail-soft so the legacy path is preserved.
func decodeQuorumGossip(data []byte) (kind byte, blockID ids.ID, payload []byte, err error) {
	if len(data) < 4+1+32 || [4]byte{data[0], data[1], data[2], data[3]} != quorumGossipMagic {
		return 0, ids.Empty, nil, ErrNotQuorumGossip
	}
	kind = data[4]
	copy(blockID[:], data[5:5+32])
	payload = data[5+32:]
	if kind != quorumKindVote && kind != quorumKindCert {
		return 0, ids.Empty, nil, ErrNotQuorumGossip
	}
	return kind, blockID, payload, nil
}
