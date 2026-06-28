// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import "errors"

// Typed, fail-closed errors. Every one is returned (never swallowed) and, when
// surfaced from the accept hook post-activation, halts finalization rather than
// accepting a checkpoint without valid post-quantum evidence.
var (
	// ErrFinalityCertMissing — a checkpoint was finalized post-activation but no
	// QuasarCert is available for it (the producer has not delivered one).
	ErrFinalityCertMissing = errors.New("quasar: finality cert missing for checkpoint")

	// ErrFinalityCertMismatch — a cert exists but does not bind the finalized
	// block (chain/height/block/state mismatch). Anti-replay.
	ErrFinalityCertMismatch = errors.New("quasar: finality cert does not bind the finalized block")

	// ErrFinalityCertInvalid — the cert is bound correctly but failed consensus
	// verification (policy, validator-set root, or a leg signature).
	ErrFinalityCertInvalid = errors.New("quasar: finality cert failed verification")

	// ErrValidatorSetUnavailable — no committed validator set for the cert's
	// epoch (the verifier cannot resolve the per-leg verification keys).
	ErrValidatorSetUnavailable = errors.New("quasar: validator set unavailable for epoch")

	// ErrPolicyUnavailable — the gate has no configured policy.
	ErrPolicyUnavailable = errors.New("quasar: policy unavailable")

	// ErrPolicyMismatch — the cert's PolicyID is not the configured policy. A
	// cert cannot select its own (weaker) posture.
	ErrPolicyMismatch = errors.New("quasar: cert policy id does not match configured policy")
)
