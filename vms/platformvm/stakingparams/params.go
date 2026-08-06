// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package stakingparams is the node-policy the validator set governs, plus the
// pure rule that decides whether a proposed change is admissible. It is one
// small, orthogonal definition — values only, no wire, state or config
// dependencies — composed by the three consumers that need it: the P-Chain tx
// executor (admission), the block executor (reward gate), and state.
//
// It is the sibling of package security: security says how a network's set is
// ADMITTED, stakingparams says on what TERMS.
//
// # What is here, and what is deliberately not
//
// Here: validator admission thresholds, stake durations, the delegation-fee
// floor, and the uptime requirement. These are operational policy — a
// legitimate collective choice of the people running the network.
//
// NOT here, and never to be added: MinConsumptionRate, MaxConsumptionRate,
// MintingPeriod, SupplyCap, and the fee-split ratio. Those are money. A votable
// emission schedule is a capture surface, not a feature; predictable money is
// the product. They stay compiled into the node and change only the way
// Bitcoin's rules change — by users and validators voluntarily adopting a new
// release. TestParamsGovernsNoMonetaryField enforces this at build time.
//
// # Who decides
//
// Nobody. There is no admin key, owner, council, guardian or upgrader in this
// package or in the mechanism it serves — no field to hold one and no argument
// to pass one. Accept is a pure function of (current state, proposal,
// compiled-in bounds). Every node runs it independently and the stake-weighted
// consensus resolves the outcome, exactly as it already does for the
// commit/abort decision on a validator's reward. A dissenting node cannot veto,
// only vote and lose; a proposing node holds no privilege, because its
// preference binds no one.
//
// # The one invariant worth remembering
//
// Moving toward MORE permissionless is free and instant. Moving toward LESS
// permissionless is bounded and rate-limited. A cartel that captures a stake
// majority therefore cannot ambush the minority out of the validator set: every
// exclusionary step is small, capped, and visible for the interval it takes,
// which is the window in which the excluded fork. That asymmetry is the whole
// security argument, and it is one rule applied uniformly to every field.
package stakingparams

import (
	"errors"
	"fmt"
	"time"

	"github.com/luxfi/node/vms/platformvm/reward"
)

// Params is the node-level staking policy in force.
//
// Percent-scaled fields use reward.PercentDenominator (1_000_000), so 800_000
// is 80%. Durations are seconds, matching the P-Chain wire.
type Params struct {
	MinValidatorStake uint64 `json:"minValidatorStake"`
	MaxValidatorStake uint64 `json:"maxValidatorStake"`
	MinStakeDuration  uint32 `json:"minStakeDuration"`
	MaxStakeDuration  uint32 `json:"maxStakeDuration"`
	MinDelegationFee  uint32 `json:"minDelegationFee"`
	UptimeRequirement uint32 `json:"uptimeRequirement"`
}

// Bounds is the constitution: the closed interval outside which no proposal is
// ever admissible, whatever the vote. Lo and Hi are corner Params — each field
// of a proposal must lie between the same field of Lo and of Hi.
//
// Bounds is compiled in. Changing it requires a node release that operators
// choose to run — the Bitcoin path — and is the only route by which the
// governable envelope itself can move. That is intentional: the envelope is the
// thing a stake majority must not be able to widen for itself.
type Bounds struct {
	Lo Params
	Hi Params
}

// Rate limits how fast policy may move against the participants it governs.
//
// MaxStep is expressed in reward.PercentDenominator units of the CURRENT value
// (100_000 = 10% of current per change). MinInterval is the minimum chain-time
// spacing between two accepted changes. Together they set the cost of a
// squeeze-out: reaching a target from a starting value takes
// ceil(log(target/start) / log(1+step)) changes, each MinInterval apart, all of
// it on-chain and observable.
type Rate struct {
	MaxStep     uint32
	MinInterval time.Duration
}

var (
	ErrOutOfBounds      = errors.New("stakingparams: proposal outside compiled-in bounds")
	ErrIncoherent       = errors.New("stakingparams: min exceeds max")
	ErrStepTooLarge     = errors.New("stakingparams: exclusionary step exceeds rate limit")
	ErrTooSoon          = errors.New("stakingparams: minimum interval between changes not elapsed")
	ErrNoChange         = errors.New("stakingparams: proposal is identical to current params")
	ErrEmptyHistory     = errors.New("stakingparams: history is empty")
	ErrNotMonotonic     = errors.New("stakingparams: history activations must strictly increase")
	ErrBoundsIncoherent = errors.New("stakingparams: bounds Lo exceeds Hi")
)

// field is one governed value, projected out of Params so that every rule is
// written once and applied uniformly. higherIsPermissive records which
// direction widens the set of parties who can participate; that single bit is
// what makes the ratchet asymmetry a rule rather than six special cases.
type field struct {
	name               string
	get                func(Params) uint64
	higherIsPermissive bool
}

func fields() []field {
	return []field{
		{"minValidatorStake", func(p Params) uint64 { return p.MinValidatorStake }, false},
		{"maxValidatorStake", func(p Params) uint64 { return p.MaxValidatorStake }, true},
		{"minStakeDuration", func(p Params) uint64 { return uint64(p.MinStakeDuration) }, false},
		{"maxStakeDuration", func(p Params) uint64 { return uint64(p.MaxStakeDuration) }, true},
		{"minDelegationFee", func(p Params) uint64 { return uint64(p.MinDelegationFee) }, false},
		{"uptimeRequirement", func(p Params) uint64 { return uint64(p.UptimeRequirement) }, false},
	}
}

// Valid reports whether p is internally coherent: every min at or below its
// paired max. Coherence is checked separately from Bounds because a proposal
// can be inside the envelope on every field and still be nonsense as a pair.
func (p Params) Valid() error {
	if p.MinValidatorStake > p.MaxValidatorStake {
		return fmt.Errorf("%w: minValidatorStake %d > maxValidatorStake %d",
			ErrIncoherent, p.MinValidatorStake, p.MaxValidatorStake)
	}
	if p.MinStakeDuration > p.MaxStakeDuration {
		return fmt.Errorf("%w: minStakeDuration %d > maxStakeDuration %d",
			ErrIncoherent, p.MinStakeDuration, p.MaxStakeDuration)
	}
	return nil
}

// Valid reports whether the compiled-in envelope is itself coherent. It exists
// so a node release that ships a nonsensical Bounds fails loudly at startup
// rather than silently admitting or rejecting everything.
func (b Bounds) Valid() error {
	for _, f := range fields() {
		if f.get(b.Lo) > f.get(b.Hi) {
			return fmt.Errorf("%w: %s Lo %d > Hi %d", ErrBoundsIncoherent, f.name, f.get(b.Lo), f.get(b.Hi))
		}
	}
	return nil
}

// Contains reports whether every field of p lies inside the envelope.
func (b Bounds) Contains(p Params) error {
	for _, f := range fields() {
		v, lo, hi := f.get(p), f.get(b.Lo), f.get(b.Hi)
		if v < lo || v > hi {
			return fmt.Errorf("%w: %s %d not in [%d, %d]", ErrOutOfBounds, f.name, v, lo, hi)
		}
	}
	return nil
}

// Clamp returns p with every field pulled inside the envelope. It is the
// migration path when a node release narrows Bounds under params that are
// already live: the chain does not halt and no key is needed to rescue it —
// the constitution simply binds, deterministically and identically on every
// node.
func (b Bounds) Clamp(p Params) Params {
	return Params{
		MinValidatorStake: min(max(p.MinValidatorStake, b.Lo.MinValidatorStake), b.Hi.MinValidatorStake),
		MaxValidatorStake: min(max(p.MaxValidatorStake, b.Lo.MaxValidatorStake), b.Hi.MaxValidatorStake),
		MinStakeDuration:  min(max(p.MinStakeDuration, b.Lo.MinStakeDuration), b.Hi.MinStakeDuration),
		MaxStakeDuration:  min(max(p.MaxStakeDuration, b.Lo.MaxStakeDuration), b.Hi.MaxStakeDuration),
		MinDelegationFee:  min(max(p.MinDelegationFee, b.Lo.MinDelegationFee), b.Hi.MinDelegationFee),
		UptimeRequirement: min(max(p.UptimeRequirement, b.Lo.UptimeRequirement), b.Hi.UptimeRequirement),
	}
}

// stepLimit returns the largest admissible exclusionary move away from cur.
// It is computed without overflowing: cur/denom*step + cur%denom*step/denom.
// A cur of 0 admits any move to 1, so a field parked at zero is not frozen
// there forever.
func stepLimit(cur uint64, step uint32) uint64 {
	const denom = uint64(reward.PercentDenominator)
	s := uint64(step)
	lim := (cur/denom)*s + (cur%denom)*s/denom
	if lim == 0 {
		lim = 1
	}
	return lim
}

// Accept is the entire governance decision, as a pure function.
//
// It takes no key, no caller identity, no role and no owner — there is nothing
// to hold and nothing to compromise. Given the same (cur, next, b, r, elapsed)
// every node on earth reaches the same verdict, which is what lets the existing
// stake-weighted commit/abort machinery settle it with no leader.
//
// elapsed is the chain time since the last accepted change. Pass a duration at
// or above r.MinInterval when no change has been made yet.
func Accept(cur, next Params, b Bounds, r Rate, elapsed time.Duration) error {
	if err := b.Valid(); err != nil {
		return err
	}
	if err := next.Valid(); err != nil {
		return err
	}
	if err := b.Contains(next); err != nil {
		return err
	}
	if cur == next {
		return ErrNoChange
	}
	if elapsed < r.MinInterval {
		return fmt.Errorf("%w: %s elapsed, %s required", ErrTooSoon, elapsed, r.MinInterval)
	}

	for _, f := range fields() {
		from, to := f.get(cur), f.get(next)
		if from == to {
			continue
		}
		// Toward more permissionless: free and instant. Widening who may
		// participate can never be used to exclude anyone, so it needs no
		// brake — only the Bounds check above, which already passed.
		if (to > from) == f.higherIsPermissive {
			continue
		}
		// Toward less permissionless: rate-limited. This is the only direction
		// a stake majority can use against a minority, so it is the only one
		// that costs time.
		var delta uint64
		if to > from {
			delta = to - from
		} else {
			delta = from - to
		}
		if lim := stepLimit(from, r.MaxStep); delta > lim {
			return fmt.Errorf("%w: %s moved %d from %d, limit %d per change",
				ErrStepTooLarge, f.name, delta, from, lim)
		}
	}
	return nil
}

// Entry is one activation of a policy: the params, and the chain time from
// which they bind.
type Entry struct {
	Activation int64  `json:"activation"`
	Params     Params `json:"params"`
}

// History is the append-only record of every policy that has been in force,
// oldest first. It exists for one reason: a validator must be judged on the
// terms it agreed to when it bonded, never on terms voted in afterwards.
//
// Without it, UptimeRequirement — the one governed field read at REWARD time
// rather than at admission time — would be retroactive, and a stake majority
// could raise it the day before a rival's stake matures and confiscate the
// reward. With it, governance can only bind the future, which is the difference
// between setting policy and expropriating people.
type History []Entry

// Valid reports whether h is non-empty and strictly increasing in activation.
func (h History) Valid() error {
	if len(h) == 0 {
		return ErrEmptyHistory
	}
	for i := 1; i < len(h); i++ {
		if h[i].Activation <= h[i-1].Activation {
			return fmt.Errorf("%w: entry %d activates at %d, not after %d",
				ErrNotMonotonic, i, h[i].Activation, h[i-1].Activation)
		}
	}
	return nil
}

// At returns the params in force at unix time t — the latest entry activating
// at or before t. Before the first activation the first entry applies, so
// genesis params bind from the beginning of time.
func (h History) At(t int64) Params {
	if len(h) == 0 {
		return Params{}
	}
	p := h[0].Params
	for _, e := range h {
		if e.Activation > t {
			break
		}
		p = e.Params
	}
	return p
}

// Current returns the params in force now, i.e. the newest entry.
func (h History) Current() Params {
	if len(h) == 0 {
		return Params{}
	}
	return h[len(h)-1].Params
}
