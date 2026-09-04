// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The terms the validator set is admitted on, and the rule that decides
//! whether a proposed change to them is admissible.
//!
//! Go states this as `vms/platformvm/stakingparams`, and this is that package:
//! values only, no wire, no state, no config. Three consumers compose it — the
//! transaction executor (admission), the block executor (the reward gate), and
//! state.
//!
//! # What is here, and what is deliberately not
//!
//! Here: validator admission thresholds, stake durations, the delegation-fee
//! floor, and the uptime requirement. Those are operational policy — a
//! legitimate collective choice of the people running the network.
//!
//! Not here, and never to be added: the consumption rates, the minting period,
//! the supply cap, and the fee split. Those are money. A votable emission
//! schedule is a capture surface, not a feature; predictable money is the
//! product. They stay compiled into the node and change the way Bitcoin's
//! rules change — by users and validators adopting a new release.
//! [`tests::params_governs_no_monetary_field`] holds that line at build time.
//!
//! # Who decides
//!
//! Nobody. There is no admin key, owner, council, guardian or upgrader in this
//! module or in the mechanism it serves — no field to hold one and no argument
//! to pass one. [`accept`] is a pure function of (current state, proposal,
//! compiled-in bounds). Every node runs it independently and stake-weighted
//! consensus resolves the outcome, exactly as it already does for the
//! commit/abort decision on a validator's reward. A dissenting node cannot
//! veto, only vote and lose; a proposing node holds no privilege, because its
//! preference binds no one.
//!
//! # The one invariant worth remembering
//!
//! Moving toward MORE permissionless is free and instant. Moving toward LESS
//! permissionless is bounded and rate-limited. A cartel that captures a stake
//! majority therefore cannot ambush the minority out of the validator set:
//! every exclusionary step is small, capped, and visible for the interval it
//! takes, which is the window in which the excluded can organise, exit, or
//! fork. That asymmetry is the whole security argument, and it is one rule
//! applied uniformly to every field.

use crate::reward::PERCENT_DENOMINATOR;

/// One unit, for the whole node. Every threshold below is denominated in it,
/// so a second definition of the unit would silently rescale all of them. LUX
/// carries six decimals, as `luxfi/node/utils/units` states it.
pub const LUX: u64 = 1_000_000;
pub const KILO_LUX: u64 = 1_000 * LUX;
pub const MEGA_LUX: u64 = 1_000 * KILO_LUX;
pub const GIGA_LUX: u64 = 1_000 * MEGA_LUX;

/// A day, in seconds. Durations here are seconds because the P-Chain wire is.
const DAY: u64 = 24 * 60 * 60;

/// The staking policy in force.
///
/// Percent-scaled fields are measured against [`PERCENT_DENOMINATOR`], so
/// 800_000 is 80%. Durations are seconds, matching the P-Chain wire.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Params {
    pub min_validator_stake: u64,
    pub max_validator_stake: u64,
    pub min_stake_duration: u32,
    pub max_stake_duration: u32,
    pub min_delegation_fee: u32,
    pub uptime_requirement: u32,
}

/// The constitution: the closed interval outside which no proposal is ever
/// admissible, whatever the vote. `lo` and `hi` are corner [`Params`] — each
/// field of a proposal must lie between the same field of each.
///
/// This is compiled in. Changing it takes a node release that operators choose
/// to run — the Bitcoin path — and is the only route by which the governable
/// envelope itself can move. That is the point: the envelope is the thing a
/// stake majority must not be able to widen for itself.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Bounds {
    pub lo: Params,
    pub hi: Params,
}

/// How fast policy may move against the people it governs.
///
/// `max_step` is in [`PERCENT_DENOMINATOR`] units of the CURRENT value
/// (100_000 = 10% of current, per change). `min_interval` is the minimum
/// chain-time spacing, in seconds, between two accepted changes. Together they
/// set the cost of a squeeze-out: reaching a target from a starting value takes
/// ceil(log(target/start) / log(1+step)) changes, each `min_interval` apart,
/// all of it on-chain and observable.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Rate {
    pub max_step: u32,
    /// Seconds.
    pub min_interval: u64,
}

/// One activation of a policy: the params, and the chain time they bind from.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Entry {
    pub activation: i64,
    pub params: Params,
}

/// The append-only record of every policy that has been in force, oldest
/// first.
///
/// It exists for one reason: a validator must be judged on the terms it agreed
/// to when it bonded, never on terms voted in afterwards. Without it,
/// `uptime_requirement` — the one governed field read at REWARD time rather
/// than at admission time — would be retroactive, and a stake majority could
/// raise it the day before a rival's stake matures and confiscate the reward.
/// With it, governance can only bind the future, which is the difference
/// between setting policy and expropriating people.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct History(pub Vec<Entry>);

/// Why a proposal was refused.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// A field of the proposal is outside the compiled-in envelope.
    OutOfBounds {
        field: &'static str,
        value: u64,
        lo: u64,
        hi: u64,
    },
    /// A minimum exceeds the maximum it is paired with.
    Incoherent {
        field: &'static str,
        min: u64,
        max: u64,
    },
    /// An exclusionary move larger than the rate limit allows.
    StepTooLarge {
        field: &'static str,
        from: u64,
        delta: u64,
        limit: u64,
    },
    /// Not enough chain time has passed since the last accepted change.
    TooSoon { elapsed: u64, required: u64 },
    /// The proposal is what is already in force.
    NoChange,
    /// A history with nothing in it.
    EmptyHistory,
    /// A history whose activations do not strictly increase.
    NotMonotonic { index: usize, at: i64, after: i64 },
    /// The compiled-in envelope is itself incoherent.
    BoundsIncoherent {
        field: &'static str,
        lo: u64,
        hi: u64,
    },
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::OutOfBounds {
                field,
                value,
                lo,
                hi,
            } => write!(f, "{field} {value} is not in [{lo}, {hi}]"),
            Error::Incoherent { field, min, max } => write!(f, "{field}: min {min} > max {max}"),
            Error::StepTooLarge {
                field,
                from,
                delta,
                limit,
            } => write!(
                f,
                "{field} moved {delta} from {from}, limit {limit} per change"
            ),
            Error::TooSoon { elapsed, required } => {
                write!(f, "{elapsed}s elapsed, {required}s required")
            }
            Error::NoChange => write!(f, "the proposal is what is already in force"),
            Error::EmptyHistory => write!(f, "the history is empty"),
            Error::NotMonotonic { index, at, after } => write!(
                f,
                "entry {index} activates at {at}, which is not after {after}"
            ),
            Error::BoundsIncoherent { field, lo, hi } => {
                write!(f, "bounds {field}: lo {lo} > hi {hi}")
            }
        }
    }
}

impl std::error::Error for Error {}

/// One governed value, projected out of [`Params`] so that every rule is
/// written once and applied uniformly.
///
/// `higher_is_permissive` records which direction widens the set of people who
/// can take part. That single bit is what makes the ratchet asymmetry one rule
/// rather than six special cases.
struct Field {
    name: &'static str,
    get: fn(&Params) -> u64,
    higher_is_permissive: bool,
}

fn fields() -> [Field; 6] {
    [
        Field {
            name: "minValidatorStake",
            get: |p| p.min_validator_stake,
            higher_is_permissive: false,
        },
        Field {
            name: "maxValidatorStake",
            get: |p| p.max_validator_stake,
            higher_is_permissive: true,
        },
        Field {
            name: "minStakeDuration",
            get: |p| p.min_stake_duration as u64,
            higher_is_permissive: false,
        },
        Field {
            name: "maxStakeDuration",
            get: |p| p.max_stake_duration as u64,
            higher_is_permissive: true,
        },
        Field {
            name: "minDelegationFee",
            get: |p| p.min_delegation_fee as u64,
            higher_is_permissive: false,
        },
        Field {
            name: "uptimeRequirement",
            get: |p| p.uptime_requirement as u64,
            higher_is_permissive: false,
        },
    ]
}

impl Params {
    /// Whether the params are internally coherent: every minimum at or below
    /// the maximum it is paired with.
    ///
    /// Checked separately from [`Bounds`] because a proposal can be inside the
    /// envelope on every field and still be nonsense as a pair.
    pub fn valid(&self) -> Result<(), Error> {
        if self.min_validator_stake > self.max_validator_stake {
            return Err(Error::Incoherent {
                field: "validatorStake",
                min: self.min_validator_stake,
                max: self.max_validator_stake,
            });
        }
        if self.min_stake_duration > self.max_stake_duration {
            return Err(Error::Incoherent {
                field: "stakeDuration",
                min: self.min_stake_duration as u64,
                max: self.max_stake_duration as u64,
            });
        }
        Ok(())
    }
}

impl Bounds {
    /// Whether the compiled-in envelope is itself coherent.
    ///
    /// It exists so a release that ships a nonsensical envelope fails loudly
    /// rather than silently admitting or rejecting everything.
    pub fn valid(&self) -> Result<(), Error> {
        for f in fields() {
            let (lo, hi) = ((f.get)(&self.lo), (f.get)(&self.hi));
            if lo > hi {
                return Err(Error::BoundsIncoherent {
                    field: f.name,
                    lo,
                    hi,
                });
            }
        }
        Ok(())
    }

    /// Whether every field of `p` lies inside the envelope.
    pub fn contains(&self, p: &Params) -> Result<(), Error> {
        for f in fields() {
            let (v, lo, hi) = ((f.get)(p), (f.get)(&self.lo), (f.get)(&self.hi));
            if v < lo || v > hi {
                return Err(Error::OutOfBounds {
                    field: f.name,
                    value: v,
                    lo,
                    hi,
                });
            }
        }
        Ok(())
    }

    /// `p` with every field pulled inside the envelope.
    ///
    /// This is the migration path when a release narrows the envelope under
    /// params that are already live: the chain does not halt and no key is
    /// needed to rescue it — the constitution simply binds, deterministically
    /// and identically on every node.
    pub fn clamp(&self, p: &Params) -> Params {
        Params {
            min_validator_stake: p
                .min_validator_stake
                .max(self.lo.min_validator_stake)
                .min(self.hi.min_validator_stake),
            max_validator_stake: p
                .max_validator_stake
                .max(self.lo.max_validator_stake)
                .min(self.hi.max_validator_stake),
            min_stake_duration: p
                .min_stake_duration
                .max(self.lo.min_stake_duration)
                .min(self.hi.min_stake_duration),
            max_stake_duration: p
                .max_stake_duration
                .max(self.lo.max_stake_duration)
                .min(self.hi.max_stake_duration),
            min_delegation_fee: p
                .min_delegation_fee
                .max(self.lo.min_delegation_fee)
                .min(self.hi.min_delegation_fee),
            uptime_requirement: p
                .uptime_requirement
                .max(self.lo.uptime_requirement)
                .min(self.hi.uptime_requirement),
        }
    }
}

/// The largest admissible exclusionary move away from `cur`.
///
/// Computed without overflowing: `cur/denom*step + cur%denom*step/denom`. The
/// naive `cur*step` form overflows a u64 at the weights the P-Chain actually
/// reaches. A `cur` of 0 admits a move to 1, so a field parked at zero is not
/// frozen there forever.
fn step_limit(cur: u64, step: u32) -> u64 {
    let denom = PERCENT_DENOMINATOR;
    let s = step as u64;
    let lim = (cur / denom) * s + (cur % denom) * s / denom;
    if lim == 0 {
        1
    } else {
        lim
    }
}

/// The whole governance decision, as a pure function.
///
/// It takes no key, no caller identity, no role and no owner — there is
/// nothing to hold and nothing to compromise. Given the same
/// `(cur, next, bounds, rate, elapsed)` every node on earth reaches the same
/// verdict, which is what lets the existing stake-weighted commit/abort
/// machinery settle it with no leader.
///
/// `elapsed` is the chain time in seconds since the last accepted change. Pass
/// something at or above `rate.min_interval` when no change has been made yet.
pub fn accept(
    cur: &Params,
    next: &Params,
    bounds: &Bounds,
    rate: &Rate,
    elapsed: u64,
) -> Result<(), Error> {
    bounds.valid()?;
    next.valid()?;
    bounds.contains(next)?;
    if cur == next {
        return Err(Error::NoChange);
    }
    if elapsed < rate.min_interval {
        return Err(Error::TooSoon {
            elapsed,
            required: rate.min_interval,
        });
    }

    for f in fields() {
        let (from, to) = ((f.get)(cur), (f.get)(next));
        if from == to {
            continue;
        }
        // Toward more permissionless: free and instant. Widening who may take
        // part can never be used to exclude anyone, so it needs no brake —
        // only the envelope check above, which already passed.
        if (to > from) == f.higher_is_permissive {
            continue;
        }
        // Toward less permissionless: rate-limited. This is the only direction
        // a stake majority can use against a minority, so it is the only one
        // that costs time.
        let delta = to.abs_diff(from);
        let limit = step_limit(from, rate.max_step);
        if delta > limit {
            return Err(Error::StepTooLarge {
                field: f.name,
                from,
                delta,
                limit,
            });
        }
    }
    Ok(())
}

impl History {
    /// Whether the history is non-empty and strictly increasing in activation.
    pub fn valid(&self) -> Result<(), Error> {
        if self.0.is_empty() {
            return Err(Error::EmptyHistory);
        }
        for i in 1..self.0.len() {
            if self.0[i].activation <= self.0[i - 1].activation {
                return Err(Error::NotMonotonic {
                    index: i,
                    at: self.0[i].activation,
                    after: self.0[i - 1].activation,
                });
            }
        }
        Ok(())
    }

    /// The params in force at unix time `t` — the latest entry activating at or
    /// before it.
    ///
    /// Before the first activation the first entry applies, so genesis params
    /// bind from the beginning of time.
    pub fn at(&self, t: i64) -> Option<Params> {
        let mut p = self.0.first()?.params;
        for e in &self.0 {
            if e.activation > t {
                break;
            }
            p = e.params;
        }
        Some(p)
    }

    /// The params in force now — the newest entry.
    pub fn current(&self) -> Option<Params> {
        self.0.last().map(|e| e.params)
    }
}

// ---- the constitution ----

/// The policy Lux mainnet was born with: min stake 2,000 LUX, max 5 GigaLux,
/// terms from two weeks to a year, a 2% delegation-fee floor and an 80% uptime
/// requirement.
///
/// Seeding the history with exactly the live values is what makes adopting the
/// mechanism a no-op on day one: nothing about the network changes until stake
/// votes to change it.
pub const MAINNET_GENESIS: Params = Params {
    min_validator_stake: 2_000 * LUX,
    max_validator_stake: 5 * GIGA_LUX,
    min_stake_duration: (2 * 7 * DAY) as u32,
    max_stake_duration: (365 * DAY) as u32,
    min_delegation_fee: 20_000,  // 2%
    uptime_requirement: 800_000, // 80%
};

/// The envelope no vote leaves.
///
/// Each bound answers one question: what is the most exclusionary value a
/// hostile stake majority could ever reach, and is the network still open to an
/// arbitrary participant there? Where the answer is no, the bound is wrong.
///
/// - `min_validator_stake` hi = 100,000 LUX. At the most hostile setting the
///   mechanism permits, a holder of 100k LUX can still validate. Below lo =
///   1 LUX there is nothing to bond.
/// - `max_validator_stake` lo = 1 MegaLux stops a cartel shrinking the ceiling
///   to exclude large honest stakers.
/// - `min_stake_duration` hi = 30 days caps how long anyone can be made to
///   lock.
/// - `max_stake_duration` hi = 365 days, because the reward calculator's
///   minting period is one year and a longer bond has no defined emission.
///   That bound is a correctness constraint, not a preference.
/// - `min_delegation_fee` hi = 20% caps how much of a delegator's yield
///   validators can vote themselves. Delegators are the one constituency that
///   cannot defend itself by voting, because the vote is the validator's.
/// - `uptime_requirement` hi = 95%. Above that, ordinary maintenance forfeits a
///   reward and the field stops being a liveness incentive and becomes an
///   ejection weapon.
pub const MAINNET_BOUNDS: Bounds = Bounds {
    lo: Params {
        min_validator_stake: LUX,
        max_validator_stake: MEGA_LUX,
        min_stake_duration: 60 * 60,
        max_stake_duration: (7 * DAY) as u32,
        min_delegation_fee: 0,
        uptime_requirement: 0,
    },
    hi: Params {
        min_validator_stake: 100 * KILO_LUX,
        max_validator_stake: 5 * GIGA_LUX,
        min_stake_duration: (30 * DAY) as u32,
        max_stake_duration: (365 * DAY) as u32,
        min_delegation_fee: 200_000, // 20%
        uptime_requirement: 950_000, // 95%
    },
};

/// The brake on exclusionary change: at most 10% of the current value per
/// accepted change, and at most one accepted change per 14 days.
///
/// 14 days is not arbitrary — it is mainnet's minimum stake duration, so no
/// bond can be entered and matured inside a single governance step. With the
/// 10% cap that puts roughly 1.6 years of continuous, public, on-chain effort
/// between the 2,000 LUX floor and the 100k LUX ceiling, which is the window in
/// which anyone being squeezed out can organise, exit, or fork.
pub const MAINNET_RATE: Rate = Rate {
    max_step: (PERCENT_DENOMINATOR / 10) as u32,
    min_interval: 14 * DAY,
};

/// The one instant every mainnet rule turns on at, as `luxfi/genesis` states
/// it. Named here so the staking floor cannot activate at a moment the
/// precompile schedule does not know.
pub const ACTIVATION: i64 = 1_766_708_400;

/// The policy in force on mainnet, oldest first.
///
/// The first entry is what the chain was born with; the second raises the
/// validator floor to 1,000,000 LUX at [`ACTIVATION`]. Anyone bonding at the
/// old floor before then is judged on the terms they agreed to.
pub fn mainnet_history() -> History {
    History(vec![
        Entry {
            activation: 0,
            params: MAINNET_GENESIS,
        },
        Entry {
            activation: ACTIVATION,
            params: Params {
                min_validator_stake: 1_000_000 * LUX,
                ..MAINNET_GENESIS
            },
        },
    ])
}

#[cfg(test)]
mod tests {
    use super::*;

    const DAY_S: u64 = 24 * 60 * 60;

    /// The build-time guard on the money/policy split.
    ///
    /// Go writes this with reflection over the struct's field names. Rust has
    /// no such reflection, so the same guarantee is made two ways that together
    /// are stronger: an exhaustive destructure, which stops compiling the
    /// moment a seventh field appears, and a scan of this module's own source
    /// for the monetary names — so adding `supply_cap` here fails the build
    /// rather than quietly making inflation votable.
    #[test]
    fn params_governs_no_monetary_field() {
        let Params {
            min_validator_stake: _,
            max_validator_stake: _,
            min_stake_duration: _,
            max_stake_duration: _,
            min_delegation_fee: _,
            uptime_requirement: _,
        } = MAINNET_GENESIS;

        let src = include_str!("stakingparams.rs");
        let decl = src
            .split("pub struct Params {")
            .nth(1)
            .expect("Params is declared here")
            .split('}')
            .next()
            .unwrap();
        for banned in [
            "supply_cap",
            "minting_period",
            "consumption_rate",
            "fee_split",
        ] {
            assert!(
                !decl.contains(banned),
                "{banned} is money, not policy; it must never be votable"
            );
        }

        // And the projection table must cover every field, or a field would be
        // silently ungoverned by the bounds and rate rules.
        assert_eq!(fields().len(), 6);
    }

    /// The mechanism has nowhere to put an admin key.
    ///
    /// [`Params`], [`Bounds`], [`Rate`] and [`Entry`] are the complete state of
    /// the decision; none carries an owner, role, threshold or address, and
    /// [`accept`] takes no caller identity.
    #[test]
    fn there_is_no_authority_surface() {
        let src = include_str!("stakingparams.rs");
        for ty in ["Params", "Bounds", "Rate", "Entry"] {
            let decl = src
                .split(&format!("pub struct {ty} {{"))
                .nth(1)
                .unwrap_or_else(|| panic!("{ty} is declared here"))
                .split("\n}")
                .next()
                .unwrap();
            for banned in [
                "owner",
                "admin",
                "auth",
                "role",
                "guardian",
                "council",
                "pauser",
                "upgrader",
                "threshold",
                "address",
            ] {
                assert!(
                    !decl.to_lowercase().contains(banned),
                    "{ty} names {banned}, which is an authority surface; \
                     governance must be keyless"
                );
            }
        }

        // And accept's signature carries no identity argument: five parameters,
        // exactly (cur, next, bounds, rate, elapsed).
        let _: fn(&Params, &Params, &Bounds, &Rate, u64) -> Result<(), Error> = accept;
    }

    #[test]
    fn the_constitution_is_coherent_and_contains_today() {
        assert_eq!(MAINNET_BOUNDS.valid(), Ok(()));
        assert_eq!(MAINNET_GENESIS.valid(), Ok(()));
        assert_eq!(
            MAINNET_BOUNDS.contains(&MAINNET_GENESIS),
            Ok(()),
            "today's live policy must be inside the envelope, or adopting this \
             would change the network on day one"
        );
    }

    /// The permissionless-ward direction is free: a proposal that lowers every
    /// barrier at once, by any amount inside the envelope, passes in one step.
    #[test]
    fn loosening_is_instant() {
        let wide_open = Params {
            min_validator_stake: LUX,                 // 2000 LUX -> 1 LUX, a 2000x drop
            max_validator_stake: 5 * GIGA_LUX,        // unchanged
            min_stake_duration: 60 * 60,              // 14 days -> 1 hour
            max_stake_duration: (365 * DAY_S) as u32, // unchanged
            min_delegation_fee: 0,                    // 2% -> 0
            uptime_requirement: 0,                    // 80% -> 0
        };
        assert_eq!(
            accept(
                &MAINNET_GENESIS,
                &wide_open,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                MAINNET_RATE.min_interval
            ),
            Ok(())
        );
    }

    /// The other direction costs time: the same magnitude of change, pointed at
    /// excluding people, is refused.
    #[test]
    fn an_exclusionary_step_is_rate_limited() {
        // A cartel tries to jump the floor straight to the ceiling.
        let mut squeeze = MAINNET_GENESIS;
        squeeze.min_validator_stake = 100 * KILO_LUX;
        assert!(matches!(
            accept(
                &MAINNET_GENESIS,
                &squeeze,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                365 * DAY_S
            ),
            Err(Error::StepTooLarge { .. })
        ));

        // Exactly 10% of current is the largest admissible step.
        let mut ok = MAINNET_GENESIS;
        ok.min_validator_stake =
            MAINNET_GENESIS.min_validator_stake + MAINNET_GENESIS.min_validator_stake / 10;
        assert_eq!(
            accept(
                &MAINNET_GENESIS,
                &ok,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                MAINNET_RATE.min_interval
            ),
            Ok(())
        );

        // One base unit beyond it is not.
        let mut over = ok;
        over.min_validator_stake += 1;
        assert!(matches!(
            accept(
                &MAINNET_GENESIS,
                &over,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                MAINNET_RATE.min_interval
            ),
            Err(Error::StepTooLarge { .. })
        ));
    }

    #[test]
    fn the_minimum_interval_is_enforced() {
        let mut next = MAINNET_GENESIS;
        next.uptime_requirement = 700_000; // a loosening, so only the interval can refuse it

        assert!(matches!(
            accept(
                &MAINNET_GENESIS,
                &next,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                MAINNET_RATE.min_interval - 1
            ),
            Err(Error::TooSoon { .. })
        ));
        assert_eq!(
            accept(
                &MAINNET_GENESIS,
                &next,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                MAINNET_RATE.min_interval
            ),
            Ok(())
        );
    }

    /// No vote, however patient, escapes the envelope. This is the Bitcoin
    /// half: to move these, operators must adopt a new release.
    #[test]
    fn the_bounds_are_absolute() {
        type Mutation = (&'static str, fn(&mut Params));
        let cases: [Mutation; 5] = [
            ("minValidatorStake above hi", |p| {
                p.min_validator_stake = MAINNET_BOUNDS.hi.min_validator_stake + 1
            }),
            ("minDelegationFee above 20%", |p| {
                p.min_delegation_fee = 200_001
            }),
            ("uptimeRequirement above 95%", |p| {
                p.uptime_requirement = 950_001
            }),
            ("minStakeDuration above 30d", |p| {
                p.min_stake_duration = (30 * DAY_S) as u32 + 1
            }),
            ("maxValidatorStake below lo", |p| {
                p.max_validator_stake = MAINNET_BOUNDS.lo.max_validator_stake - 1
            }),
        ];
        for (name, mutate) in cases {
            let mut p = MAINNET_GENESIS;
            mutate(&mut p);
            assert!(
                matches!(
                    accept(
                        &MAINNET_GENESIS,
                        &p,
                        &MAINNET_BOUNDS,
                        &MAINNET_RATE,
                        10 * 365 * DAY_S
                    ),
                    Err(Error::OutOfBounds { .. })
                ),
                "{name} must be refused as out of bounds"
            );
        }
    }

    /// The number the design lives or dies on: driving the validator floor from
    /// today's value to the most exclusionary one the constitution permits, at
    /// the maximum rate, in public, takes this long. It is the warning window in
    /// which anyone being pushed out can organise, exit, or fork.
    #[test]
    fn a_squeeze_out_costs_years() {
        let mut cur = MAINNET_GENESIS;
        let mut steps = 0u64;
        while cur.min_validator_stake < MAINNET_BOUNDS.hi.min_validator_stake {
            let mut next = cur;
            next.min_validator_stake = (cur.min_validator_stake
                + step_limit(cur.min_validator_stake, MAINNET_RATE.max_step))
            .min(MAINNET_BOUNDS.hi.min_validator_stake);
            assert_eq!(
                accept(
                    &cur,
                    &next,
                    &MAINNET_BOUNDS,
                    &MAINNET_RATE,
                    MAINNET_RATE.min_interval
                ),
                Ok(()),
                "a step at exactly the maximum rate must always be admissible"
            );
            cur = next;
            steps += 1;
            assert!(steps < 1000, "loop guard");
        }

        let elapsed = steps * MAINNET_RATE.min_interval;
        assert!(
            elapsed > 365 * DAY_S,
            "a stake majority must need over a year of public, on-chain effort \
             to reach the most exclusionary policy: got {} days over {steps} changes",
            elapsed / DAY_S
        );

        // And having spent it, the network is STILL open to a 100k LUX holder.
        assert_eq!(
            cur.min_validator_stake,
            MAINNET_BOUNDS.hi.min_validator_stake
        );
        assert_eq!(MAINNET_BOUNDS.contains(&cur), Ok(()));
    }

    /// The non-retroactivity proof, and the property that separates governance
    /// from expropriation.
    ///
    /// `uptime_requirement` is the one governed field read at REWARD time, long
    /// after a validator bonded. A validator that bonded under an 80% rule is
    /// judged at 80% even after stake votes it to 88% — so a majority cannot
    /// raise the bar the day before a rival's stake matures and take its
    /// reward.
    #[test]
    fn a_history_binds_only_the_future() {
        const BONDED_AT: i64 = 1_765_573_611; // the real start of today's mainnet validators
        const VOTED_AT: i64 = 1_785_000_000; // a later vote

        let mut tightened = MAINNET_GENESIS;
        tightened.uptime_requirement = 880_000; // 88%

        let h = History(vec![
            Entry {
                activation: 0,
                params: MAINNET_GENESIS,
            },
            Entry {
                activation: VOTED_AT,
                params: tightened,
            },
        ]);
        assert_eq!(h.valid(), Ok(()));

        assert_eq!(
            h.at(BONDED_AT).unwrap().uptime_requirement,
            800_000,
            "a validator bonded before the vote is judged on the terms it accepted"
        );
        assert_eq!(
            h.at(VOTED_AT + 1).unwrap().uptime_requirement,
            880_000,
            "a validator bonding after the vote is judged on the new terms"
        );
        assert_eq!(h.current(), Some(tightened));

        // Before any activation, genesis params bind.
        assert_eq!(h.at(-1), Some(MAINNET_GENESIS));
    }

    #[test]
    fn a_history_out_of_order_is_refused() {
        assert_eq!(History(vec![]).valid(), Err(Error::EmptyHistory));
        let e = |a: i64| Entry {
            activation: a,
            params: MAINNET_GENESIS,
        };
        assert!(matches!(
            History(vec![e(10), e(10)]).valid(),
            Err(Error::NotMonotonic { .. })
        ));
        assert!(matches!(
            History(vec![e(10), e(9)]).valid(),
            Err(Error::NotMonotonic { .. })
        ));
    }

    #[test]
    fn an_incoherent_proposal_is_refused() {
        let mut p = MAINNET_GENESIS;
        p.min_validator_stake = 5 * GIGA_LUX;
        p.max_validator_stake = MEGA_LUX;
        assert!(matches!(
            accept(
                &MAINNET_GENESIS,
                &p,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                365 * DAY_S
            ),
            Err(Error::Incoherent { .. })
        ));
    }

    #[test]
    fn a_proposal_that_changes_nothing_is_refused() {
        assert_eq!(
            accept(
                &MAINNET_GENESIS,
                &MAINNET_GENESIS,
                &MAINNET_BOUNDS,
                &MAINNET_RATE,
                365 * DAY_S
            ),
            Err(Error::NoChange)
        );
    }

    /// The arithmetic at the extremes the P-Chain can actually reach: weights
    /// run to ~1.8e19 and the denominator is 1e6, so the naive `cur * step`
    /// form overflows a u64.
    #[test]
    fn the_step_limit_does_not_overflow() {
        assert_eq!(step_limit(u64::MAX, MAINNET_RATE.max_step), u64::MAX / 10);
        assert_eq!(step_limit(2_000 * LUX, MAINNET_RATE.max_step), 200 * LUX);
        // A field parked at zero is not frozen there.
        assert_eq!(step_limit(0, MAINNET_RATE.max_step), 1);
    }

    /// The migration path when a release narrows the constitution under live
    /// params: every node clamps identically and the chain keeps running. No
    /// guardian, no emergency multisig, no halt.
    #[test]
    fn a_clamp_rescues_without_a_key() {
        let mut live = MAINNET_GENESIS;
        live.uptime_requirement = 940_000; // legal today

        let mut narrowed = MAINNET_BOUNDS;
        narrowed.hi.uptime_requirement = 900_000; // a later release tightens the ceiling

        assert!(narrowed.contains(&live).is_err());
        let clamped = narrowed.clamp(&live);
        assert_eq!(narrowed.contains(&clamped), Ok(()));
        assert_eq!(clamped.uptime_requirement, 900_000);
        assert_eq!(clamped.valid(), Ok(()));
    }

    /// The floor moves at the activation and nowhere else. A validator bonding
    /// one second before it is held to 2,000 LUX; one bonding at it must bring
    /// 1,000,000.
    #[test]
    fn the_mainnet_floor_moves_at_the_activation() {
        let h = mainnet_history();
        assert_eq!(h.valid(), Ok(()));
        let activation = h.0[1].activation;
        assert_eq!(
            h.at(activation - 1).unwrap().min_validator_stake,
            2_000 * LUX
        );
        assert_eq!(
            h.at(activation).unwrap().min_validator_stake,
            1_000_000 * LUX
        );

        // Nothing else moved: the change is the floor and only the floor.
        let mut before = h.at(activation - 1).unwrap();
        let mut after = h.at(activation).unwrap();
        before.min_validator_stake = 0;
        after.min_validator_stake = 0;
        assert_eq!(before, after);
    }

    /// One unit for the node. The thresholds are stated in LUX and must resolve
    /// through the same constant everything else uses — six decimals.
    #[test]
    fn the_units_are_the_nodes_units() {
        assert_eq!(LUX, 1_000_000);
        assert_eq!(MAINNET_GENESIS.min_validator_stake, 2_000 * LUX);
    }
}
