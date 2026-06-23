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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"

	consensusconfig "github.com/luxfi/consensus/config"
	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	validators "github.com/luxfi/validators"
)

// isLocalDevNetwork reports whether networkID is an explicitly-local developer
// network: devnet (3) or localnet (1337). These are EXACT IDs (per luxfi/constants
// convention), NOT a range — a custom value L1 with a high networkID (e.g. an
// L1 whose chainID == networkID) is a VALUE network and must NOT match here.
// Local dev networks are the only IDs that run a committee SIZED TO THE LIVE
// VALIDATOR SET (localBFTParamsForN) instead of the large-committee Default
// (K=20), because a handful of localhost validators cannot reach an α=14-of-K=20
// quorum.
func isLocalDevNetwork(networkID uint32) bool {
	return networkID == constants.DevnetID || networkID == constants.LocalID
}

// bftSafeAlpha returns the smallest accept-quorum α that is Byzantine-safe for a
// committee of K: the integer ceil((K + f + 1) / 2) with f = ⌊(K-1)/3⌋. This is
// the SAME bound the consensus engine enforces (config.Parameters.Valid asserts
// 2·α − K ≥ f + 1) and the SAME formula the operator computes (api/v1
// BFTSafeAlpha) — one definition of "safe quorum", so the node and the operator
// can never disagree on it. For K=5 → f=1 → α=4; K=4 → f=1 → α=3; K=1 → α=1.
func bftSafeAlpha(k int) int {
	if k < 1 {
		return 1
	}
	f := (k - 1) / 3
	alpha := (k + f + 2) / 2 // == ceil((k + f + 1) / 2)
	if alpha < 1 {
		alpha = 1
	}
	if alpha > k {
		alpha = k
	}
	return alpha
}

// localBFTParamsForN returns the minimal Byzantine-fault-tolerant parameter set
// for a LOCAL DEV network of n validators: K = n (every validator samples every
// other — the only K that lets the proposer poll the whole set, since the
// proposer FILTERS ITSELF out of its own K-sample), α = bftSafeAlpha(K).
//
// Why K MUST equal n (the liveness fix): the gossiper proposer samples K
// validators then drops itself from the sample (engine/chain RequestVotes), so a
// proposer in its own sample queries only K−1 distinct peers. With a fixed K=4
// on a 5-validator set the proposer queried just 3 peers yet α=3 demanded a
// UNANIMOUS reply from those 3 — a single lagging validator made the α-quorum
// unreachable and the first block hung forever (D-Chain + C-Chain frozen at
// height 0). Setting K = n makes the proposer poll all n−1 peers, so α = ⌈2n/3⌉-ish
// (4 of 5) has real slack: it tolerates f = ⌊(n-1)/3⌋ non-responsive/Byzantine
// validators instead of zero. This is what the operator already emits as
// --consensus-sample-size/--consensus-quorum-size; the node now derives the
// identical committee from the live set instead of a hardcoded K=4.
//
// Clamped to K≥4 so a 2- or 3-validator local net still clears the value-network
// BFT floor (ValidateForValueNetwork requires K≥4, f≥1); such a tiny set degrades
// to near-unanimous fault tolerance but remains safe. Keeps the localhost timing
// (1ms blocks / 5ms rounds) from LocalBFTParams.
func localBFTParamsForN(n int) consensusconfig.Parameters {
	k := n
	if k < 4 {
		k = 4
	}
	p := consensusconfig.LocalBFTParams() // localhost timing + BetaVirtuous/Rogue
	alpha := bftSafeAlpha(k)
	p.K = k
	p.AlphaPreference = alpha
	p.AlphaConfidence = alpha
	if p.BetaRogue < k {
		p.BetaRogue = k // rogue confidence stays above committee size
	}
	return p
}

// selectConsensusParams picks the consensus parameters for a chain.
//
//   - sybilProtection == false (--dev / single-node): K=1, the sole validator's
//     accept is the 1-of-1 quorum (no peer signatures).
//   - sybilProtection == true, VALUE network: a large BYZANTINE-fault-tolerant
//     param set — NEVER LocalParams() (K=3/α=2, f=0, which a single Byzantine
//     validator forks; this was CRITICAL-2). Mainnet→K=21, Testnet→K=11, every
//     other value net→Default K=20.
//   - sybilProtection == true, LOCAL DEV network (devnet 3 / localnet 1337):
//     a committee SIZED TO THE LIVE VALIDATOR SET — K = numValidators,
//     α = bftSafeAlpha(K) (localBFTParamsForN). Default K=20 is unsatisfiable on a
//     few localhost validators (α=14 unreachable → P-Chain frozen at height 0). A
//     hardcoded K=4 on a 5-validator set was ALSO unsatisfiable in practice: the
//     proposer self-filters its K-sample so it polled only 3 peers while α=3
//     required all 3 — one lagging validator wedged finality (this is the bug this
//     fixes). K = N makes the proposer poll the whole set so α has BFT slack.
//
// numValidators is the live count of the primary-network validator set (the set
// every native chain — P/C/D — samples from); 0 means "set not yet known", in
// which case we fall back to the minimal K=4 committee. All branches satisfy the
// 2α−K ≥ f+1 overlap bound; the manager call site also asserts
// ValidateForValueNetwork as a fail-closed backstop.
func selectConsensusParams(sybilProtection bool, networkID uint32, numValidators int) consensusconfig.Parameters {
	if !sybilProtection {
		return consensusconfig.SingleValidatorParams()
	}
	switch networkID {
	case constants.MainnetID:
		return consensusconfig.MainnetParams()
	case constants.TestnetID:
		return consensusconfig.TestnetParams()
	default:
		if isLocalDevNetwork(networkID) {
			return localBFTParamsForN(numValidators)
		}
		return consensusconfig.DefaultParams()
	}
}

// --- BLS vote verifier -------------------------------------------------------

// blsVoteVerifier verifies a validator's BLS signature over the canonical vote
// message. The validator's BLS public key is resolved FROM THE HEIGHT-INDEXED
// validators.State AT THE BLOCK'S P-CHAIN EPOCH HEIGHT (RESIDUAL-B) — the SAME
// height-pinned source the set-root and the ⅔-by-stake tally read from
// (validatorSetAtHeight). It is NOT resolved from the CURRENT validator map.
//
// Why the epoch, not the current map: during an async validator-set change a
// validator can be present in the set@H (it legitimately signed the block being
// voted on at height H) yet ALREADY GONE from the current map. Resolving its
// pubkey from the current map drops its valid vote, and if that validator holds
// >⅓ of the stake-at-H the stable set falls below ⅔ and block H NEVER finalizes
// (this is not self-healing for that block — the skew is permanent for H). Pinning
// pubkey resolution to set@H — alongside membership, set-root, and stake — makes
// the cert internally consistent at exactly one epoch.
//
// An unknown validator at the epoch, a validator with no BLS key, or a bad
// signature all yield false (never an error/panic) — a cert with such a voter is
// simply invalid, the fail-closed contract the engine requires. A nil state (the
// no-op on a non-quorum node) yields false for every voter; a K>1 chain is
// guarded against ever reaching here with a nil/no-op state (manager.go).
type blsVoteVerifier struct {
	state     validators.State
	networkID ids.ID
}

func newBLSVoteVerifier(state validators.State, networkID ids.ID) *blsVoteVerifier {
	return &blsVoteVerifier{state: state, networkID: networkID}
}

// VerifyVote implements consensuschain.VoteVerifier. epochHeight is the block's
// P-chain height; the voter's pubkey is read from the set IN FORCE AT that height.
func (v *blsVoteVerifier) VerifyVote(nodeID ids.NodeID, message []byte, sig []byte, epochHeight uint64) bool {
	out, ok := validatorSetAtHeight(v.state, v.networkID, epochHeight)[nodeID]
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

// --- height-pinned epoch read (MEDIUM-1) -------------------------------------

// validatorSetAtHeight reads the validator set IN FORCE AT a value-chain height
// from the height-indexed validators.State. This is the SINGLE source of epoch
// truth shared by the stake source and the set-root source: both membership and
// weights are read at the SAME height H so a cert's signed set-root and its
// ⅔-by-stake tally are measured against the identical set.
//
// Determinism across nodes is the whole point. validators.State.GetValidatorSet
// returns the set the network already agreed on at height H (P-chain / L1-staking
// consensus), so every honest node computes the same set — and therefore the
// same set-root and the same tally — for a given H, INDEPENDENT of async
// current-map skew during a validator-set change. (The previous Manager.GetMap()
// read hashed the CURRENT map, which diverges between the signer and the
// assembler across that skew window → mismatched canonical messages → dropped
// votes → finality stall at every staking change. That was MEDIUM-1.)
//
// A nil state, a lookup error, or an empty set yields a nil map, which the
// callers fold into their fail-soft answers (0 weight / Empty root). An error is
// SYMMETRIC across nodes (a committed height H reads the same on every node, or
// fails the same way), so the degraded answer is uniform — it never makes one
// node's view disagree with another's, which is the property that matters here.
func validatorSetAtHeight(state validators.State, networkID ids.ID, height uint64) map[ids.NodeID]*validators.GetValidatorOutput {
	if state == nil {
		return nil
	}
	set, err := state.GetValidatorSet(context.Background(), height, networkID)
	if err != nil || len(set) == 0 {
		return nil
	}
	return set
}

// --- stake source (HIGH-3, height-pinned by MEDIUM-1) ------------------------

// validatorStakeSource supplies validator voting weights so the engine can
// require a ⅔-by-stake supermajority for finality (HIGH-3). Weights are read
// from the HEIGHT-INDEXED validators.State at the cert-position height, the same
// height the set-root commits to (MEDIUM-1). Reading the tally at the same epoch
// as the signed membership means a validator whose vote is in the cert (its
// signature verifies against the height-H set-root) also contributes its height-H
// weight to the tally — eliminating the second skew (a current-map weight read
// could drop a legitimately-signed quorum when membership changed between sign
// and tally).
type validatorStakeSource struct {
	state     validators.State
	networkID ids.ID
}

func newValidatorStakeSource(state validators.State, networkID ids.ID) *validatorStakeSource {
	return &validatorStakeSource{state: state, networkID: networkID}
}

// Weight implements consensuschain.StakeSource. Returns the validator's stake in
// the set IN FORCE AT height — deterministic across nodes for a given height. An
// unknown validator (or a fail-soft empty read) yields 0, which cannot inflate
// the numerator.
func (s *validatorStakeSource) Weight(nodeID ids.NodeID, height uint64) uint64 {
	out, ok := validatorSetAtHeight(s.state, s.networkID, height)[nodeID]
	if !ok || out == nil {
		return 0
	}
	return out.Light
}

// TotalStake implements consensuschain.StakeSource. Total active stake of the set
// IN FORCE AT height (the denominator of the ⅔ predicate), measured at the same
// epoch as Weight and the set-root.
func (s *validatorStakeSource) TotalStake(height uint64) uint64 {
	var total uint64
	for _, out := range validatorSetAtHeight(s.state, s.networkID, height) {
		if out != nil {
			total += out.Light
		}
	}
	return total
}

var _ consensuschain.StakeSource = (*validatorStakeSource)(nil)

// --- validator-set-root source (MEDIUM: epoch binding) -----------------------

// validatorSetRootSource computes the deterministic commitment to the validator
// set IN FORCE AT a value-chain height — the value the engine stamps into every
// vote's VotePosition.ValidatorSetRoot so a cert is pinned to the exact set it
// was certified under. It is the node side of the MEDIUM fix: it turns the
// "⅔-by-stake measured at the cert-position epoch" property into an ENFORCED
// invariant (a cross-epoch cert fails verification because every signature was
// over this root).
//
// HEIGHT-PINNED (MEDIUM-1). The root is read from the HEIGHT-INDEXED
// validators.State at the value-chain block height, NOT from the Manager's
// CURRENT map. At a given height H, GetValidatorSet returns the set the network
// already agreed on, so every honest node — signer and assembler alike — computes
// the IDENTICAL root for H, independent of async current-map skew during a
// validator-set change. Reading the current map (the prior bug) let the signer
// and the assembler hold different maps across that skew window → different roots
// → the canonical signed message differed → signatures failed verification →
// votes dropped → finality stalled at every staking change.
//
// The commitment is a SHA-256 over the set serialized in a canonical order
// (validators sorted by NodeID, each as nodeID || light || len(pubkey) ||
// pubkey) — see hashValidatorSet. Sorting by NodeID + length-prefixing the
// pubkey makes the encoding canonical and unambiguous; the byte layout is
// UNCHANGED from the prior implementation, so the wire format and the engine's
// epoch-binding contract are preserved (only the SOURCE of the set changed from
// the current map to the height-indexed set).
type validatorSetRootSource struct {
	state     validators.State
	networkID ids.ID
}

func newValidatorSetRootSource(state validators.State, networkID ids.ID) *validatorSetRootSource {
	return &validatorSetRootSource{state: state, networkID: networkID}
}

// ValidatorSetRoot implements consensuschain.ValidatorSetRootSource. Returns the
// commitment to the weighted set IN FORCE AT height (deterministic across nodes).
// Returns ids.Empty when the state is absent or the set is empty (the explicit
// "unbound" answer, consistent with the engine default); a height-read error is
// symmetric across nodes, so the Empty fallback is uniform and never creates a
// cross-node root disagreement.
func (s *validatorSetRootSource) ValidatorSetRoot(height uint64) ids.ID {
	return hashValidatorSet(validatorSetAtHeight(s.state, s.networkID, height))
}

var _ consensuschain.ValidatorSetRootSource = (*validatorSetRootSource)(nil)

// hashValidatorSet computes the canonical SHA-256 commitment to a weighted
// validator set: validators sorted by NodeID, each serialized as
// nodeID || light(8,BE) || len(pubkey)(8,BE) || pubkey. An empty/nil set commits
// to ids.Empty (the "unbound" answer). This is the SINGLE definition of the
// set-root encoding (DRY) — both the live source and its tests hash through here,
// so the wire format cannot drift between them.
func hashValidatorSet(set map[ids.NodeID]*validators.GetValidatorOutput) ids.ID {
	if len(set) == 0 {
		return ids.Empty
	}
	nodeIDs := make([]ids.NodeID, 0, len(set))
	for nodeID := range set {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool {
		return bytes.Compare(nodeIDs[i][:], nodeIDs[j][:]) < 0
	})
	h := sha256.New()
	var u64 [8]byte
	for _, nodeID := range nodeIDs {
		v := set[nodeID]
		h.Write(nodeID[:])
		binary.BigEndian.PutUint64(u64[:], v.Light)
		h.Write(u64[:])
		binary.BigEndian.PutUint64(u64[:], uint64(len(v.PublicKey)))
		h.Write(u64[:])
		h.Write(v.PublicKey)
	}
	var root ids.ID
	copy(root[:], h.Sum(nil))
	return root
}

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
