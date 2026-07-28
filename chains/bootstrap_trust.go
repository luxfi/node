// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// bootstrap_trust.go — the SEPARATE trust object for INITIAL SYNC, decomplected from
// consensus finality.
//
// The mass-recovery DEADLOCK this fixes: the prior FrontierTip required a ⅔-by-stake quorum
// of the CURRENT total validator set to be CONNECTED before it would name a sync frontier.
// When the recovery TARGETS are themselves validators (a node that crashed IS one of the 5),
// taking them down drops connected stake below ⅔ of the whole set — so on a network of 5
// equal-weight validators, losing 2 leaves 3 (60% < ⅔) and NO node can ever name a frontier
// to recover from. Bootstrap trust was braided into consensus finality, and finality's ⅔ rule
// is mathematically unsatisfiable during a mass outage.
//
// The fix is a type split, NOT a renamed threshold. FinalityQuorum decides FINALITY
// (> ⅔ of CURRENT stake — UNCHANGED). BootstrapTrust decides whether a fetched frontier is
// SAFE TO BEGIN SYNC FROM: a quorum of AUTHENTICATED CONFIGURED beacons that RESPOND, gated by
// a response FLOOR (MinResponses) and an agreement threshold over the RESPONDERS (not over the
// whole set). 3 of 5 reachable beacons all agreeing is a valid sync anchor even though 3 of 5
// stake is not a finalizing supermajority. The two decisions have different threat models and
// are different objects.
package chains

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"time"

	consensusconfig "github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
)

// BootstrapTrust is not a consensus-finality oracle.
// It selects a weak-subjective sync frontier from authenticated configured beacons.
// Live block acceptance remains governed exclusively by FinalityQuorum.
type BootstrapTrust interface {
	// AcceptsFrontier returns the block an empty/behind node may BEGIN SYNCING FROM, selected
	// from the authenticated configured beacons' frontier replies — or an error
	// (ErrInsufficientBootstrapResponses / ErrNoBootstrapQuorum) when no trusted frontier can be
	// named this round. The returned Frontier is a sync ANCHOR, never a consensus certificate
	// (see the type comment): the node must still re-execute every block it descends to before
	// re-entering live consensus, where FinalityQuorum alone governs acceptance.
	AcceptsFrontier(ctx context.Context, replies []BeaconReply) (*Frontier, error)
}

// FinalityQuorum decides FINALITY: whether a weight is a finalizing supermajority (> ⅔) of the
// CURRENT validator set. This is the live-consensus rule; bootstrap does NOT change it. It is a
// SEPARATE named type from BootstrapTrust precisely so the distinction is explicit and testable:
// a frontier that AcceptsFrontier admits is "safe to sync from", and in general it does NOT
// satisfy HasFinality (3 of 5 responders is a valid sync anchor; 3 of 5 stake is not finality).
type FinalityQuorum interface {
	HasFinality(weight, total StakeWeight) bool
}

// StakeWeight is validator stake in the units the validator manager reports (Weight/Light).
type StakeWeight = uint64

// twoThirdsFinality is the production FinalityQuorum: > ⅔ of the CURRENT total stake, exactly
// the rule the live cert-gate uses (consensusconfig.TwoThirdsStakeFloor). Defined here only to
// give the live rule a name to CONTRAST bootstrap trust against — it is not wired into the live
// path (that already enforces ⅔ inside consensus), and bootstrap never calls it to ACCEPT.
type twoThirdsFinality struct{}

func (twoThirdsFinality) HasFinality(weight, total StakeWeight) bool {
	return weight > consensusconfig.TwoThirdsStakeFloor(total)
}

// DefaultFinalityQuorum returns the live ⅔-of-current-stake finality rule — the thing bootstrap
// trust is explicitly NOT. Used by the test suite to prove a bootstrap-accepted frontier does
// not constitute finality.
func DefaultFinalityQuorum() FinalityQuorum { return twoThirdsFinality{} }

var (
	// ErrInsufficientBootstrapResponses: fewer than MinResponses configured beacons answered.
	// Not a partition-capture-safe quorum — the node must keep waiting for more beacons (or use
	// an operator checkpoint), never sync from the captured few. INVARIANT 2's response floor.
	ErrInsufficientBootstrapResponses = errors.New("bootstrap: insufficient configured-beacon responses")
	// ErrNoBootstrapQuorum: enough beacons responded, but no block clears the agreement threshold
	// over the responders (a genuine partition, or a transient bleeding-edge split the loop retries).
	ErrNoBootstrapQuorum = errors.New("bootstrap: no responder-agreed frontier")
)

// BeaconReply is one authenticated configured beacon's report of its accepted frontier tip
// during initial sync. NodeID is authenticated at the transport handshake (a peer cannot forge
// another's identity); Weight is the beacon's CONFIGURED stake from the trust anchor — NOT a
// self-reported value. A reply whose NodeID is not in the policy's TrustedBeacons is ignored.
type BeaconReply struct {
	NodeID ids.NodeID
	Tip    ids.ID
	Weight StakeWeight
}

// Frontier is the weak-subjective sync anchor BootstrapTrust selects: the block a node descends
// to and re-executes. It is NOT a consensus certificate (see BootstrapTrust). Height is the
// tallied height when named via the ancestor-tolerant path, and 0 (unknown) when named via the
// exact fast path before any ancestry fetch — the sync loop uses ID; Height is diagnostic.
type Frontier struct {
	ID             ids.ID
	Height         uint64
	Weight         StakeWeight // responder stake whose accepted chain contains this block
	Responders     int         // distinct configured beacons that backed it
	FromCheckpoint bool        // selected from an operator checkpoint (too few beacons responded)
}

// BlockRef is a parsed block's CONTENT-ADDRESSED identity (id, height, parent) — the only thing
// the ancestor-tolerant tally needs. Decouples the policy from the VM/block types.
type BlockRef struct {
	ID     ids.ID
	Height uint64
	Parent ids.ID
}

// AncestrySource resolves a tip's CONTENT-ADDRESSED ancestry for the ancestor-tolerant tally —
// the SAME parent-linked descent the sync loop trusts. Injected so the trust DECISION (which
// beacons count, the response floor, the agreement threshold) stays separate from the transport.
type AncestrySource interface {
	// Ancestry returns up to max blocks ending at tip, parsed to (id, height, parent). An empty
	// result (no error) means the tip's ancestry was not served — that anchor contributes nothing.
	Ancestry(ctx context.Context, tip ids.ID, max int) ([]BlockRef, error)
}

// Checkpoint is an operator-pinned (id, height) the recovering node may anchor to when too few
// beacons respond to form a quorum — the EXPLICIT override for INVARIANT 2's "1 of N reachable"
// case. Absent (nil) ⇒ the default policy REJECTS rather than trusting a captured minority.
//
// INVARIANT 4 (a checkpoint is a SIGNED weak-subjectivity anchor, not a bare config value): the
// checkpoint carries a cryptographic Signature by the configured checkpoint AUTHORITY over its
// (id, height), and AcceptsFrontier trusts it ONLY when CheckpointVerifier authenticates that
// signature. A (id,height) present in a flag/config but UNSIGNED — or signed by a non-authority key
// — is REJECTED (fail closed). This is the crucial hardening: the checkpoint is the one path that
// bypasses the beacon quorum, so a compromised flag must NOT be able to inject a false sync anchor
// without ALSO forging the authority's signature. It is a cryptographic vouch, NEVER a ⅔-live-stake
// tally (that conflation is the very deadlock BootstrapTrust exists to avoid).
type Checkpoint struct {
	ID     ids.ID
	Height uint64
	// Signature is the checkpoint authority's signature over this checkpoint's canonical (id,height)
	// bytes. Verified by CheckpointVerifier before the anchor is trusted; an empty signature is
	// never accepted.
	Signature []byte
}

// CheckpointVerifier authenticates a Checkpoint's Signature against the configured checkpoint
// AUTHORITY key(s). The node injects a real implementation backed by a PROVEN primitive (Ed25519 /
// BLS — never custom crypto); the policy stays free of any crypto dependency, exactly like
// AncestrySource and heightOf. A nil verifier means no signed anchor is configured, so any
// Checkpoint is untrusted and the below-floor case fails closed.
type CheckpointVerifier interface {
	// VerifyCheckpoint reports whether sig is a valid signature over (id, height) by the configured
	// checkpoint authority. It MUST reject an empty signature and be signature-safe (constant-time
	// compare on the primitive). It is the sole authority on whether a pinned anchor may be trusted.
	VerifyCheckpoint(id ids.ID, height uint64, sig []byte) bool
}

// CanonicalCheckpointMessage is the exact byte string a checkpoint authority signs and
// CheckpointVerifier authenticates: a domain-separated, fixed-layout encoding of (id, height) so a
// signature can never be transplanted from another context. 8-byte big-endian height after the
// 32-byte id, under a distinct domain tag.
func CanonicalCheckpointMessage(id ids.ID, height uint64) []byte {
	const domain = "lux-bootstrap-checkpoint-v1\x00"
	msg := make([]byte, 0, len(domain)+len(id)+8)
	msg = append(msg, domain...)
	msg = append(msg, id[:]...)
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], height)
	return append(msg, h[:]...)
}

// Ratio is an exact rational threshold (e.g. 2/3, 3/4). A value clears it iff
// value > floorOf(whole) — strictly greater, matching the consensus ⅔ floor's semantics.
type Ratio struct{ Num, Den uint64 }

// floorOf returns ⌊whole · Num / Den⌋ without floating point, overflow-safe for the sub-unity
// thresholds used here. Ratio{2,3}.floorOf(w) == consensusconfig.TwoThirdsStakeFloor(w) exactly,
// so the responder-⅔ agreement reuses the same strict-greater floor the live rule uses.
func (r Ratio) floorOf(whole uint64) uint64 {
	if r.Den == 0 {
		return whole // degenerate guard; constructors always set a real ratio
	}
	hi, lo := bits.Mul64(whole, r.Num)
	if hi >= r.Den {
		return math.MaxUint64 // Num ≥ Den: not a sub-unity threshold — nothing can exceed it
	}
	q, _ := bits.Div64(hi, lo, r.Den)
	return q
}

// BootstrapPolicy is the default BootstrapTrust: a CONFIGURED-BEACON quorum with a response
// FLOOR and an agreement threshold over the RESPONDERS — a SEPARATE object from FinalityQuorum
// with a SEPARATE threat model. It does NOT pass "reachable stake" into the ⅔-of-current-stake
// finality rule (that conflation IS the mass-recovery deadlock). It reuses the ancestor-tolerant
// common-ancestor tally only for HOW to find the agreed frontier; the ACCEPTANCE gate is the
// response floor + responder agreement here.
//
// The three invariants:
//   - INVARIANT 1 (non-circular beacon eligibility): only NodeIDs in TrustedBeacons count, and
//     TrustedBeacons comes from the configured/checkpointed/genesis anchor — NEVER peer
//     self-report. A recovering node never lets arbitrary peers define who is a beacon.
//   - INVARIANT 2 (a floor prevents partition-capture): MinResponses authenticated beacons must
//     respond before any frontier is named; an attacker who partitions the node down to a few
//     beacons cannot capture the frontier. Below the floor, REJECT (or use Checkpoint).
//   - INVARIANT 3 (acceptance ≠ finality): the named Frontier is "safe to begin sync from", not
//     finalized. The node independently re-executes the descent before re-entering consensus.
type BootstrapPolicy struct {
	// TrustedBeacons is the trust anchor: configured-beacon NodeID → configured stake (INVARIANT
	// 1). Resolved from --bootstrap-nodes / a finalized P-chain checkpoint / the genesis set —
	// never from peer self-report.
	TrustedBeacons map[ids.NodeID]StakeWeight
	// AgreementThreshold is the fraction of the RESPONDER weight a named block must exceed
	// (default 2/3). Over RESPONDERS, not the whole set — that is what permits mass recovery.
	AgreementThreshold Ratio
	// MinResponses is the FLOOR on distinct configured-beacon responders (INVARIANT 2). Default:
	// a MAJORITY of the configured set (the largest floor that still lets a node recover when a
	// minority of validators is down). Capped at the set size.
	MinResponses int
	// MinResponseWeight is an OPTIONAL floor on the total responder weight (0 ⇒ disabled).
	MinResponseWeight StakeWeight
	// MinResponders is the minimum DISTINCT beacons that must back a NAMED block (default 2), so a
	// single beacon cannot alone name the frontier. Capped at the responder count.
	MinResponders int
	// MinFrontierHeight is the node's current last-accepted height. The ANCESTOR-TOLERANT path
	// names only a block STRICTLY ABOVE it — a frontier genuinely AHEAD. A common ancestor BELOW it
	// is history the node has (a partition above, not a frontier ahead). A block AT exactly this
	// height is ALSO not named here (the M1 eclipse-stale fix): an eclipse can throttle the honest
	// ahead-tips below the ⅔ naming threshold while the node's OWN height accrues ⅔ as their shared
	// ANCESTOR — naming it would go Ready stale. Excluding own height routes that case to CaughtUp,
	// which distinguishes a legit all-at-N fleet from an eclipse with ahead-tips the node lacks. So
	// nothing at or below own height is named (→ ErrNoBootstrapQuorum, fail safe), never a
	// false-complete at the stale height. The exact fast path is exempt: a tip a responder
	// supermajority ACTIVELY reports is a real frontier even at own height (a genuinely fresh
	// network, or a fleet unanimously AT the tip).
	MinFrontierHeight uint64
	// Checkpoint is the OPTIONAL operator override for the below-floor case (INVARIANT 2). nil ⇒
	// reject below the floor. When set, it is trusted ONLY if CheckpointVerifier authenticates its
	// signature (INVARIANT 4).
	Checkpoint *Checkpoint
	// CheckpointVerifier authenticates the Checkpoint's authority signature (INVARIANT 4). nil ⇒ a
	// configured Checkpoint is NOT trusted (fail closed) — a bare (id,height) is never enough.
	CheckpointVerifier CheckpointVerifier
	// NamingWindow is the per-FETCH ancestry chunk (Ancestry returns FULL blocks, so one fetch
	// must stay inside a network message); MaxNamingDepth is the TOTAL ancestry a single anchor
	// may be walked down in NamingWindow-sized chunks; MaxAnchors bounds how many distinct
	// reported tips are resolved. All default to the package constants when zero.
	NamingWindow   int
	MaxNamingDepth int
	MaxAnchors     int
	// NamingTimeout TOTAL-bounds the ancestor-tolerant resolution (all anchor fetches combined) so
	// a partition that ANSWERS the frontier query but WITHHOLDS ancestry cannot make the decision
	// hang — it returns what it found (or nothing → ErrNoBootstrapQuorum) and the caller's bounded
	// retry tries a fresh sample next round. Zero ⇒ the package default.
	NamingTimeout time.Duration
	// Source resolves content-addressed ancestry for the ancestor-tolerant tally. When nil, the
	// policy decides on the exact fast path alone (no split resolution).
	Source AncestrySource
}

// compile-time: the default policy IS a BootstrapTrust.
var _ BootstrapTrust = (*BootstrapPolicy)(nil)

func (p *BootstrapPolicy) effectiveMinResponses() int {
	n := len(p.TrustedBeacons)
	if p.MinResponses > 0 {
		if p.MinResponses > n {
			return n
		}
		return p.MinResponses
	}
	return n/2 + 1 // default: a MAJORITY of the configured beacon set
}

func (p *BootstrapPolicy) effectiveAgreement() Ratio {
	if p.AgreementThreshold.Den == 0 {
		return Ratio{Num: 2, Den: 3} // default: ⅔ of the RESPONDERS
	}
	return p.AgreementThreshold
}

func (p *BootstrapPolicy) effectiveMinResponders(responders int) int {
	r := p.MinResponders
	if r <= 0 {
		r = bootstrapMinAgreeingBeacons // default 2
	}
	if r > responders {
		r = responders
	}
	if r < 1 {
		r = 1
	}
	return r
}

func (p *BootstrapPolicy) namingWindow() int {
	if p.NamingWindow > 0 {
		return p.NamingWindow
	}
	return bootstrapNamingWindow
}

// maxNamingDepth is the TOTAL ancestry one anchor may be walked down, in namingWindow-sized
// chunks. It is a RESOURCE bound and nothing else. Safety comes from hash-verified ancestry
// (a forged chain cannot link), MinResponders distinct voters, the ⅔-of-RESPONDER-stake floor,
// and the full re-Verify every block gets on the descent — none of which depend on this number.
func (p *BootstrapPolicy) maxNamingDepth() int {
	if p.MaxNamingDepth > 0 {
		return p.MaxNamingDepth
	}
	return bootstrapMaxNamingDepth
}

func (p *BootstrapPolicy) maxAnchors() int {
	if p.MaxAnchors > 0 {
		return p.MaxAnchors
	}
	return maxNamingAnchors
}

func (p *BootstrapPolicy) namingTimeout() time.Duration {
	if p.NamingTimeout > 0 {
		return p.NamingTimeout
	}
	return bootstrapNamingTimeout
}

// tallyResponders applies INVARIANT 1 (only CONFIGURED beacons count, deduplicated by NodeID — a
// reply from a peer not in TrustedBeacons, a repeat, or an empty tip is dropped) and returns the
// distinct responder count + total responder stake plus the per-tip stake / voter maps the naming
// tally walks. The authenticated NodeID (transport handshake) is what makes "configured"
// unforgeable. Shared by AcceptsFrontier (which names a frontier AHEAD) and CaughtUp (which
// concludes NONE is ahead) so both judge the IDENTICAL responder set under the SAME eligibility
// rule — the eligibility decision lives in exactly one place.
func (p *BootstrapPolicy) tallyResponders(replies []BeaconReply) (responders int, responderWeight StakeWeight, stakeOnTip map[ids.ID]StakeWeight, votersOf map[ids.ID]map[ids.NodeID]struct{}) {
	seen := make(map[ids.NodeID]struct{}, len(replies))
	stakeOnTip = make(map[ids.ID]StakeWeight)
	votersOf = make(map[ids.ID]map[ids.NodeID]struct{})
	for _, r := range replies {
		w, ok := p.TrustedBeacons[r.NodeID]
		if !ok || r.Tip == ids.Empty {
			continue
		}
		if _, dup := seen[r.NodeID]; dup {
			continue
		}
		seen[r.NodeID] = struct{}{}
		responders++
		responderWeight += w
		stakeOnTip[r.Tip] += w
		if votersOf[r.Tip] == nil {
			votersOf[r.Tip] = make(map[ids.NodeID]struct{})
		}
		votersOf[r.Tip][r.NodeID] = struct{}{}
	}
	return responders, responderWeight, stakeOnTip, votersOf
}

// floorMet reports whether the responder set clears INVARIANT 2's partition-capture FLOOR: at
// least MinResponses distinct configured beacons AND (when MinResponseWeight is configured) at
// least that much total responder stake. AcceptsFrontier gates NAMING a frontier on it and
// CaughtUp gates concluding NONE-AHEAD on the SAME floor — so an eclipse that suppresses the
// honest ahead-nodes to fake EITHER outcome must drop the responder set below it and fail safe.
func (p *BootstrapPolicy) floorMet(responders int, responderWeight StakeWeight) bool {
	if responders < p.effectiveMinResponses() {
		return false
	}
	if p.MinResponseWeight > 0 && responderWeight < p.MinResponseWeight {
		return false
	}
	return true
}

// AcceptsFrontier implements BootstrapTrust. It (1) keeps ONLY configured-beacon replies
// (INVARIANT 1, tallyResponders), (2) enforces the MinResponses / MinResponseWeight floor or falls
// back to the operator checkpoint (INVARIANT 2, floorMet), then (3) names the highest block a
// responder supermajority shares via the ancestor-tolerant tally. It never consults the
// ⅔-of-current-stake finality rule (INVARIANT 3): the decision is the response floor + responder
// agreement, a separate threat model.
func (p *BootstrapPolicy) AcceptsFrontier(ctx context.Context, replies []BeaconReply) (*Frontier, error) {
	responders, responderWeight, stakeOnTip, votersOf := p.tallyResponders(replies)

	// INVARIANT 2: the response FLOOR prevents partition-capture. Below MinResponses (or below
	// MinResponseWeight) the node has not heard from enough authenticated beacons to trust ANY
	// frontier — an attacker may have partitioned it down to a captured few. REJECT, unless the
	// operator explicitly pinned a checkpoint to anchor from.
	if !p.floorMet(responders, responderWeight) {
		if p.Checkpoint != nil {
			// INVARIANT 4: the checkpoint bypasses the beacon quorum, so trust it ONLY when the
			// configured authority SIGNED this exact (id,height). A checkpoint present in config but
			// unsigned, or signed by a non-authority key, is REJECTED (fail closed) — a compromised
			// flag cannot inject a false sync anchor without also forging the authority's signature.
			if p.CheckpointVerifier == nil || len(p.Checkpoint.Signature) == 0 ||
				!p.CheckpointVerifier.VerifyCheckpoint(p.Checkpoint.ID, p.Checkpoint.Height, p.Checkpoint.Signature) {
				return nil, fmt.Errorf("%w: a checkpoint is pinned but its authority signature did not verify",
					ErrInsufficientBootstrapResponses)
			}
			return &Frontier{
				ID:             p.Checkpoint.ID,
				Height:         p.Checkpoint.Height,
				Responders:     responders,
				FromCheckpoint: true,
			}, nil
		}
		return nil, fmt.Errorf("%w: %d configured beacons responded (weight %d), need %d",
			ErrInsufficientBootstrapResponses, responders, responderWeight, p.effectiveMinResponses())
	}

	// The agreement threshold is over the RESPONDERS, not the whole configured set — this is what
	// lets a node recover when validators are down (3 of 5 reachable, all 3 agreeing, is a valid
	// sync anchor). The ⅔-of-current-stake finality rule is never used here (INVARIANT 3).
	floor := p.effectiveAgreement().floorOf(responderWeight)
	required := p.effectiveMinResponders(responders)

	id, height, weight, ok := p.nameFrontier(ctx, stakeOnTip, votersOf, floor, required)
	if !ok {
		return nil, ErrNoBootstrapQuorum
	}
	return &Frontier{ID: id, Height: height, Weight: weight, Responders: responders}, nil
}

// CaughtUp reports whether the responder set PROVES the node is already AT OR ABOVE the network
// frontier — the dual of AcceptsFrontier ("nobody is ahead" vs "here is the block ahead to sync
// to"). It is the go-live path for a TIP-HOLDER on a mixed-height co-restart: when producers
// restart together the responder set SPLITS (the tip-holders are exactly half — below the ⅔
// naming threshold), so AcceptsFrontier names NOTHING (ErrNoBootstrapQuorum), yet the node is
// plainly not behind. Without this determination such a producer fails safe DOWN at its own tip —
// the exact OPPOSITE of the stale-go-live bug, and just as wrong. THREE conditions, ALL required:
//
//   - (a) the SAME response FLOOR AcceptsFrontier uses is met (floorMet: MinResponses distinct
//     beacons AND MinResponseWeight stake-majority). An eclipse that hides the higher (real) tips
//     to fake caught-up must SUPPRESS the ahead-nodes' replies, dropping the responder set below
//     the floor → NOT caught up, fail safe. No partition-capture: faking caught-up costs the same
//     stake-majority of honest beacons that faking a NAMED frontier does.
//   - (b) every responder's reported ACCEPTED tip is at height ≤ lastAccepted. A genuinely STALE
//     node has at least one honest responder AHEAD (height > lastAccepted) → NOT caught up: it
//     still syncs, so the stale-go-live bug stays fixed. (GetAcceptedFrontier reports a beacon's
//     last-ACCEPTED block, so an un-finalized N+1 a producer is merely processing is never reported
//     — the ±1 pending-tip skew cannot fake "ahead", and a producer one ACCEPTED block ahead
//     correctly defeats caught-up so the node syncs that block.)
//   - (c) the node has ACCEPTED every reported tip — heightOf returns ok ONLY for a block on the
//     node's FINALIZED chain, so a tip the node lacks OR merely holds-in-store-but-has-not-accepted
//     (someone genuinely ahead, a gossiped-ahead block, or a same-height sibling/fork it never
//     finalized) makes the conclusion fail. The node declares caught-up only to blocks it ACCEPTED.
//
// heightOf resolves a tip's height from the node's ACCEPTED chain (ok=false when the tip is not
// accepted — including a block merely PRESENT in the store but unaccepted, the luxd-2 freeze case),
// injected so the trust DECISION stays free of any VM/block dependency — the same separation as
// AncestrySource. It is NEVER a network fetch: an unaccepted/absent tip simply makes the node
// not-caught-up (the safe direction — it syncs). Because (c) requires the node to have ACCEPTED
// every reported tip, the heights (b) compares are the blocks' canonical (content-addressed)
// heights read from the finalized chain — store presence can never fake "caught up".
func (p *BootstrapPolicy) CaughtUp(replies []BeaconReply, lastAccepted uint64, heightOf func(ids.ID) (uint64, bool)) bool {
	responders, responderWeight, stakeOnTip, _ := p.tallyResponders(replies)
	if !p.floorMet(responders, responderWeight) {
		return false // (a) below the floor — an eclipse/partition can never fake caught-up
	}
	sawTip := false
	for tip := range stakeOnTip {
		sawTip = true
		h, held := heightOf(tip)
		if !held || h > lastAccepted {
			return false // (c) a tip we do not hold, or (b) a responder ahead → NOT caught up
		}
	}
	return sawTip // ≥1 responder tip evaluated (floor already implies this; guards an empty set)
}

// nameFrontier finds the block a responder supermajority shares — by CONTENT, reusing the
// parent-link descent the sync loop trusts (HOW to find the agreed frontier; the ACCEPTANCE gate
// already passed in AcceptsFrontier). A beacon reporting tip T vouches for every ANCESTOR of T,
// so the named frontier is the HIGHEST block whose backing stake exceeds floor (the responder
// agreement threshold) with ≥ required distinct voters.
//
//   - EXACT FAST PATH: if a single reported tip clears the floor outright, name it with NO
//     ancestry fetch (the whole responding quorum already agrees on the same tip). Exempt from
//     MinFrontierHeight: an actively-reported tip is a real frontier even when low.
//   - ANCESTOR-TOLERANT PATH: otherwise, fetch the distinct tips' ancestries into ONE union index
//     and globally credit each tip's stake to every block on its content-addressed chain. The
//     highest block clearing the floor AND at a height STRICTLY ABOVE MinFrontierHeight (a frontier
//     genuinely ahead — never the node's own height, which an eclipse could over-credit as a shared
//     ancestor; that routes to CaughtUp) is named. A sibling split converges to the common committed
//     ancestor; a partition that shares nothing ⅔-backed names nothing (→ fail safe).
//
// C1 (a forged chain finalizes ZERO) is preserved: a block is credited a beacon's stake only when
// that beacon's tip lies on the block's CONTENT-ADDRESSED descendant chain (parent ids are bound
// to block content), so a peer cannot fake linkage to over-credit; a block is named only with
// backing > ⅔ of the responder weight; a minority (< ⅓) forged tip can only RATIFY real ancestors
// it builds on, never name itself or raise the named height above the honest common block.
func (p *BootstrapPolicy) nameFrontier(ctx context.Context, stakeOnTip map[ids.ID]StakeWeight, votersOf map[ids.ID]map[ids.NodeID]struct{}, floor StakeWeight, required int) (ids.ID, uint64, StakeWeight, bool) {
	// EXACT fast path: a single reported tip already clears the floor — name it, no fetch.
	for tip, st := range stakeOnTip {
		if st > floor && len(votersOf[tip]) >= required {
			return tip, 0, st, true
		}
	}
	if p.Source == nil {
		return ids.Empty, 0, 0, false
	}

	// TOTAL-bound all anchor fetches so a partition that answers the frontier query but withholds
	// ancestry cannot hang the decision — the caller's bounded retry handles it next round.
	ctx, cancel := context.WithTimeout(ctx, p.namingTimeout())
	defer cancel()

	// Build ONE union index from the distinct reported tips' ancestries (most stake first; skip a
	// tip already present from an earlier fetch — a nested tip covers its ancestors). Bounded by
	// MaxAnchors × NamingWindow blocks, so a Byzantine swarm reporting many forged tips cannot
	// induce unbounded work.
	index := make(map[ids.ID]BlockRef)
	fetches := 0
	for _, tip := range sortedByStakeDesc(stakeOnTip) {
		if _, have := index[tip]; have {
			continue
		}
		if fetches >= p.maxAnchors() {
			break
		}
		fetches++
		// CHUNKED DESCENT. namingWindow is the per-FETCH size (Ancestry returns FULL blocks, so
		// one fetch must fit a network message) — NOT the total ancestry worth walking. Keep
		// following the parent chain in window-sized chunks up to maxNamingDepth, so a fleet
		// that HALTED with a straggler far below the highest tip still resolves its ⅔-backed
		// common ancestor. A single un-chunked window silently capped this at 256 and wedged
		// mainnet at a 535-block gap. ctx already TOTAL-bounds every fetch (namingTimeout).
		cur := tip
		for depth := 0; depth < p.maxNamingDepth(); {
			// The deadline must bind the WALK, not just each fetch: a Byzantine peer serving a
			// long fabricated chain cheaply (or an in-process Source) would otherwise run the
			// full depth budget before anyone checked the clock.
			if ctx.Err() != nil {
				break
			}
			refs, err := p.Source.Ancestry(ctx, cur, p.namingWindow())
			if err != nil || len(refs) == 0 {
				break
			}
			deepest, first := BlockRef{}, true
			for _, ref := range refs {
				if _, ok := index[ref.ID]; !ok {
					index[ref.ID] = ref
				}
				if first || ref.Height < deepest.Height {
					deepest, first = ref, false
				}
			}
			depth += len(refs) // len(refs) >= 1 here, so depth strictly advances -> terminates
			if deepest.Parent == ids.Empty {
				break // reached genesis
			}
			if _, covered := index[deepest.Parent]; covered {
				break // an earlier anchor's chain already covers everything below
			}
			cur = deepest.Parent
		}
	}
	if len(index) == 0 {
		return ids.Empty, 0, 0, false
	}

	// Global credit: each reported tip vouches for every block on its content-addressed ancestry.
	// The running backing at block B = the responder stake whose accepted chain contains B.
	backing := make(map[ids.ID]StakeWeight)
	voters := make(map[ids.ID]map[ids.NodeID]struct{})
	for tip, st := range stakeOnTip {
		cur := tip
		for {
			ref, ok := index[cur]
			if !ok {
				break // the served ancestry does not extend further down (or this tip was unserved)
			}
			backing[cur] += st
			if voters[cur] == nil {
				voters[cur] = make(map[ids.NodeID]struct{})
			}
			for v := range votersOf[tip] {
				voters[cur][v] = struct{}{}
			}
			if ref.Parent == ids.Empty {
				break
			}
			cur = ref.Parent
		}
	}

	// Name the HIGHEST block clearing the floor with ≥ required distinct voters, at a height
	// STRICTLY ABOVE MinFrontierHeight — a genuine frontier AHEAD. A block AT the node's own
	// last-accepted height is NOT named here (it is history the node already holds, reachable as a
	// ⅔-backed ANCESTOR of higher tips an eclipse can suppress below the naming threshold — the M1
	// stale-go-live path): that case routes to CaughtUp, which alone can distinguish a legit
	// all-at-N fleet (→ Ready at N) from an eclipse with ahead-tips the node lacks (→ sync). A block
	// BELOW own height is a partition diverged beneath the node. Both fail safe, never false-complete.
	var bestID ids.ID
	var bestHeight, bestStake uint64
	found := false
	for id, st := range backing {
		ref := index[id]
		if st <= floor || len(voters[id]) < required || ref.Height <= p.MinFrontierHeight {
			continue
		}
		if !found || ref.Height > bestHeight || (ref.Height == bestHeight && st > bestStake) {
			bestID, bestHeight, bestStake, found = id, ref.Height, st, true
		}
	}
	return bestID, bestHeight, bestStake, found
}

// sortedByStakeDesc returns the reported tips most-stake-first (stable id tiebreak) — the order
// the ancestor-tolerant tally fetches anchors in, so the well-supported honest tips are covered
// first and a forged low-stake outlier swarm falls outside the anchor cap.
func sortedByStakeDesc(stakeOnTip map[ids.ID]StakeWeight) []ids.ID {
	tips := make([]ids.ID, 0, len(stakeOnTip))
	for t := range stakeOnTip {
		tips = append(tips, t)
	}
	sort.Slice(tips, func(i, j int) bool {
		if stakeOnTip[tips[i]] != stakeOnTip[tips[j]] {
			return stakeOnTip[tips[i]] > stakeOnTip[tips[j]]
		}
		return bytes.Compare(tips[i][:], tips[j][:]) < 0
	})
	return tips
}
