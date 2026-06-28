// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	qcert "github.com/luxfi/consensus/protocol/quasar"
)

// ValidatorSetProvider resolves the committed validator set the verifier pins a
// cert against, for a (chain, epoch). Post-activation the gate calls this once
// per verified checkpoint.
type ValidatorSetProvider interface {
	ValidatorSet(chainID uint32, epoch uint64) (qcert.ConsensusValidatorSet, error)
}

// ValidatorSet is a concrete ConsensusValidatorSet for one epoch: the committed
// weighted-validator-set root plus the per-leg verification keys the cert legs
// verify against (the classical BLS aggregate key for the Beam leg and the
// Pulsar ML-DSA threshold group key for the Pulsar leg — the HYBRID_PQ pair).
//
// Production wiring (the activation seam): in production these fields are
// populated from the P-Chain-pinned validator set (Root, Epoch) and the active
// KeyEra group keys for the epoch. That population is era/rotation-coupled and
// lands with the producer + KeyEra-registry wiring (see producer.go). Corona
// (STRICT_DUAL_PQ) and Magnetar/P3Q (POLARIS / RECOVERY) group keys + the
// weighted-sig-set config are populated by that same follow-on; until then this
// set serves the HYBRID_PQ pair and reports "no key" for the other lanes.
type ValidatorSet struct {
	root        [48]byte
	epoch       uint64
	blsAggKey   []byte // classical BLS-12-381 aggregate pubkey (Beam leg)
	pulsarGroup []byte // Pulsar ML-DSA threshold group pubkey (Pulsar leg)
}

var _ qcert.ConsensusValidatorSet = (*ValidatorSet)(nil)

// NewValidatorSet builds a committed validator set for one epoch from its root
// and the HYBRID_PQ verification keys.
func NewValidatorSet(root [48]byte, epoch uint64, blsAggKey, pulsarGroup []byte) *ValidatorSet {
	return &ValidatorSet{root: root, epoch: epoch, blsAggKey: blsAggKey, pulsarGroup: pulsarGroup}
}

// Root returns the 48-byte weighted-validator-set commitment.
func (v *ValidatorSet) Root() [48]byte { return v.root }

// Epoch returns the epoch this set was committed under.
func (v *ValidatorSet) Epoch() uint64 { return v.epoch }

// WeightedConfig returns the QuorumVerifierConfig for the WeightedSigSet
// evidence mode. HYBRID_PQ does not use weighted-sig-set legs; the zero config
// is correct here and is populated by the POLARIS / RECOVERY follow-on.
func (v *ValidatorSet) WeightedConfig() qcert.QuorumVerifierConfig {
	return qcert.QuorumVerifierConfig{}
}

// WeightedEnvelope returns the round-digest posture axes for the inner
// WeightedQuorumCert. Zero for HYBRID_PQ (no weighted-sig-set leg); populated by
// the POLARIS / RECOVERY follow-on.
func (v *ValidatorSet) WeightedEnvelope() qcert.QuorumMessageEnvelope {
	return qcert.QuorumMessageEnvelope{}
}

// ThresholdGroupKey returns the threshold-signature group public key for a leg
// kind. Serves the Pulsar (ML-DSA) lane; reports (zero, false) for the others
// until their group keys are wired by the follow-on.
func (v *ValidatorSet) ThresholdGroupKey(kind qcert.LegKind) (qcert.ThresholdGroupKey, bool) {
	if kind == qcert.LegPulsarMLDSA && len(v.pulsarGroup) > 0 {
		return qcert.ThresholdGroupKey{Kind: qcert.LegPulsarMLDSA, PulsarGroupKey: v.pulsarGroup}, true
	}
	return qcert.ThresholdGroupKey{}, false
}

// ClassicalAggregateKey returns the classical aggregate verification key for a
// scheme. Serves the BLS-12-381 Beam leg.
func (v *ValidatorSet) ClassicalAggregateKey(scheme qcert.ClassicalScheme) ([]byte, bool) {
	if scheme == qcert.ClassicalSchemeBLS12381 && len(v.blsAggKey) > 0 {
		return v.blsAggKey, true
	}
	return nil, false
}

// StaticValidatorSetProvider returns the same committed set for every (chain,
// epoch). It is the single-era / test provider; the production provider resolves
// per-epoch sets from the P-Chain validator manager + KeyEra registry.
type StaticValidatorSetProvider struct{ Set qcert.ConsensusValidatorSet }

// ValidatorSet implements ValidatorSetProvider.
func (p StaticValidatorSetProvider) ValidatorSet(_ uint32, _ uint64) (qcert.ConsensusValidatorSet, error) {
	if p.Set == nil {
		return nil, ErrValidatorSetUnavailable
	}
	return p.Set, nil
}
