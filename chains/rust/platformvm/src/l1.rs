// SPDX-License-Identifier: BSD-3-Clause-Eco

//! A validator of a sovereign network, and the fee it pays to stay one.
//!
//! Ported from Go `vms/platformvm/state/l1_validator.go`, `state/expiry.go` and
//! `vms/platformvm/validators/fee` — the LP-77 continuous fee.
//!
//! An L1 validator is not a staker, and the difference is which way the money
//! goes. A staker bonds a stake and is paid for the time it stood; an L1
//! validator *pays*, continuously, for the P-Chain's trouble in tracking it. So
//! its lifetime is a balance rather than an end time, and it leaves when that
//! balance runs out rather than when a clock strikes.
//!
//! **The order is by when the money runs out.** Validators are walked in
//! increasing `end_accumulated_fee`, so advancing the clock deactivates exactly
//! the prefix that can no longer pay and stops at the first one that can. That
//! is why the balance is stored as an *absolute* accrued-fee mark rather than
//! as a remaining amount: a remaining amount would have to be decremented for
//! every validator on every tick.
//!
//! A weight of zero means removed and an `end_accumulated_fee` of zero means
//! inactive. Those are different things: an inactive validator is still in the
//! set and still weighs on it, it simply cannot vote and is not charged.

use crate::gas::{self, Gas, Price};
use crate::ids::{Id, NodeId};

/// Go: `state.L1Validator`.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Validator {
    /// The name of the registration message that created it — the hash of that
    /// message, so two registrations differing anywhere are two validators.
    pub validation_id: Id,
    /// The network it validates.
    pub chain_id: Id,
    pub node_id: NodeId,
    /// The **uncompressed** BLS key, always populated. The compressed form is
    /// what a proof of possession signs; the uncompressed form is what the set
    /// commitment hashes, and using one where the other belongs produces a root
    /// that verifies against nothing.
    pub public_key: Vec<u8>,
    /// Who gets the remaining balance back, in the standalone owner encoding.
    pub remaining_balance_owner: Vec<u8>,
    /// Who may switch this validator off.
    pub deactivation_owner: Vec<u8>,
    pub start_time: u64,
    /// Zero means removed.
    pub weight: u64,
    /// The smallest nonce that may change the weight next. `u64::MAX` is valid
    /// only for the change that removes the validator.
    pub min_nonce: u64,
    /// The accrued-fee mark this validator can pay up to. Zero means inactive.
    pub end_accumulated_fee: u64,
}

impl Validator {
    pub fn is_deleted(&self) -> bool {
        self.weight == 0
    }

    pub fn is_active(&self) -> bool {
        self.weight != 0 && self.end_accumulated_fee != 0
    }

    /// What the validator set sees. An inactive validator has no node there: it
    /// holds weight but cannot be sampled, so surfacing its node would let a
    /// quorum wait for a vote that can never come.
    pub fn effective_node_id(&self) -> NodeId {
        if self.is_active() {
            self.node_id
        } else {
            NodeId::EMPTY
        }
    }

    pub fn effective_public_key(&self) -> &[u8] {
        if self.is_active() {
            &self.public_key
        } else {
            &[]
        }
    }

    /// Everything except weight, nonce and balance is fixed for the life of a
    /// validation id. A write that changes any of it is not an update to this
    /// validator; it is a different validator wearing its name.
    pub fn immutable_fields_unmodified(&self, other: &Validator) -> bool {
        if self.validation_id != other.validation_id {
            return true;
        }
        self.chain_id == other.chain_id
            && self.node_id == other.node_id
            && self.public_key == other.public_key
            && self.remaining_balance_owner == other.remaining_balance_owner
            && self.deactivation_owner == other.deactivation_owner
            && self.start_time == other.start_time
    }
}

impl Ord for Validator {
    /// Go: `L1Validator.Compare`. By when the money runs out, then by name.
    fn cmp(&self, other: &Validator) -> std::cmp::Ordering {
        self.end_accumulated_fee
            .cmp(&other.end_accumulated_fee)
            .then_with(|| self.validation_id.cmp(&other.validation_id))
    }
}

impl PartialOrd for Validator {
    fn partial_cmp(&self, other: &Validator) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

/// Go: `state.ExpiryEntry`. A registration message that may still be issued,
/// and the moment after which it may not.
///
/// Ordered by that moment, then by name, so advancing the clock drops exactly
/// the prefix that has passed.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord)]
pub struct Expiry {
    pub timestamp: u64,
    pub validation_id: Id,
}

impl Expiry {
    /// Go: `ExpiryEntry.Marshal`. Big-endian timestamp then the id, so sorting
    /// the bytes sorts the timestamps — the database walks these in key order
    /// and the executor needs them in time order, and making those one order is
    /// the whole point of the endianness.
    pub fn marshal(&self) -> [u8; 40] {
        let mut out = [0u8; 40];
        out[..8].copy_from_slice(&self.timestamp.to_be_bytes());
        out[8..].copy_from_slice(&self.validation_id);
        out
    }

    pub fn unmarshal(bytes: &[u8]) -> Option<Expiry> {
        if bytes.len() != 40 {
            return None;
        }
        let mut validation_id = [0u8; 32];
        validation_id.copy_from_slice(&bytes[8..]);
        Some(Expiry {
            timestamp: u64::from_be_bytes(bytes[..8].try_into().ok()?),
            validation_id,
        })
    }
}

/// The shape of the continuous fee: how many validators the chain is built for,
/// how many it wants, the floor price, and how fast the price answers demand.
///
/// Go: `validators/fee.Config`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct FeeConfig {
    pub capacity: Gas,
    pub target: Gas,
    pub min_price: Price,
    pub excess_conversion_constant: Gas,
}

/// Go: `validators/fee.State`. How many active validators there are, and how
/// far above target their number has been running.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct FeeState {
    pub current: Gas,
    pub excess: Gas,
}

impl FeeState {
    /// Go: `fee.State.AdvanceTime`. Excess moves toward zero below target and
    /// away from it above, saturating rather than wrapping at either end.
    pub fn advance_time(&self, target: Gas, seconds: u64) -> FeeState {
        let excess = match self.current.cmp(&target) {
            std::cmp::Ordering::Less => gas::sub_per_second(self.excess, target - self.current, seconds),
            std::cmp::Ordering::Greater => {
                gas::add_per_second(self.excess, self.current - target, seconds)
            }
            std::cmp::Ordering::Equal => self.excess,
        };
        FeeState {
            current: self.current,
            excess,
        }
    }

    /// What `seconds` of being a validator costs from here.
    ///
    /// Saturates at `u64::MAX` rather than wrapping: an unpayable price is
    /// unpayable, not free.
    pub fn cost_of(&self, config: &FeeConfig, seconds: u64) -> u64 {
        // At target the price does not move, so the whole span is one multiply.
        if self.current == config.target {
            let price = gas::calculate_price(
                config.min_price,
                self.excess,
                config.excess_conversion_constant,
            );
            return seconds.saturating_mul(price);
        }

        let mut cost: u64 = 0;
        let mut state = *self;
        for i in 0..seconds {
            state = state.advance_time(config.target, 1);

            // Advancing holds the excess, raises it, or lowers it — never
            // mixes. So once it reaches zero it stays there, and the rest of
            // the span is the floor price times the remaining seconds.
            if state.excess == 0 {
                let Some(rest) = config.min_price.checked_mul(seconds - i) else {
                    return u64::MAX;
                };
                return cost.saturating_add(rest);
            }

            let price = gas::calculate_price(
                config.min_price,
                state.excess,
                config.excess_conversion_constant,
            );
            match cost.checked_add(price) {
                Some(total) => cost = total,
                None => return u64::MAX,
            }
        }
        cost
    }

    /// How long `funds` buys, capped at `max_seconds`. The inverse of
    /// [`FeeState::cost_of`], and what lets a validator be told when it will
    /// run out.
    pub fn seconds_remaining(&self, config: &FeeConfig, max_seconds: u64, funds: u64) -> u64 {
        // A floor price of zero buys forever, and dividing by it would not.
        if config.min_price == 0 {
            return max_seconds;
        }

        if self.current == config.target {
            let price = gas::calculate_price(
                config.min_price,
                self.excess,
                config.excess_conversion_constant,
            );
            return (funds / price).min(max_seconds);
        }

        let mut state = *self;
        let mut remaining = funds;
        for seconds in 0..max_seconds {
            state = state.advance_time(config.target, 1);
            if state.excess == 0 {
                let at_floor = remaining / config.min_price;
                return seconds.checked_add(at_floor).unwrap_or(max_seconds).min(max_seconds);
            }
            let price = gas::calculate_price(
                config.min_price,
                state.excess,
                config.excess_conversion_constant,
            );
            if price > remaining {
                return seconds;
            }
            remaining -= price;
        }
        max_seconds
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config() -> FeeConfig {
        FeeConfig {
            capacity: 20_000,
            target: 10_000,
            min_price: 512,
            excess_conversion_constant: 1_000_000,
        }
    }

    #[test]
    fn a_validator_with_no_weight_is_removed_and_one_with_no_balance_is_merely_off() {
        // The two zeros mean different things, and confusing them either
        // resurrects a removed validator or evicts a paying one.
        let removed = Validator {
            weight: 0,
            end_accumulated_fee: 5,
            ..Default::default()
        };
        assert!(removed.is_deleted());
        assert!(!removed.is_active());

        let inactive = Validator {
            weight: 7,
            end_accumulated_fee: 0,
            ..Default::default()
        };
        assert!(!inactive.is_deleted());
        assert!(!inactive.is_active());

        let live = Validator {
            weight: 7,
            end_accumulated_fee: 5,
            node_id: NodeId([3; 20]),
            public_key: vec![1; 96],
            ..Default::default()
        };
        assert!(live.is_active());
        assert_eq!(live.effective_node_id(), NodeId([3; 20]));
        assert_eq!(live.effective_public_key().len(), 96);
    }

    #[test]
    fn an_inactive_validator_shows_the_set_no_node_and_no_key() {
        // Its weight still counts in the quorum denominator; surfacing its node
        // would let a quorum wait for a vote nobody can cast.
        let off = Validator {
            weight: 7,
            end_accumulated_fee: 0,
            node_id: NodeId([3; 20]),
            public_key: vec![1; 96],
            ..Default::default()
        };
        assert_eq!(off.effective_node_id(), NodeId::EMPTY);
        assert!(off.effective_public_key().is_empty());
    }

    #[test]
    fn validators_are_ordered_by_when_the_money_runs_out() {
        let poor = Validator {
            end_accumulated_fee: 10,
            validation_id: [9; 32],
            ..Default::default()
        };
        let rich = Validator {
            end_accumulated_fee: 20,
            validation_id: [1; 32],
            ..Default::default()
        };
        assert!(poor < rich, "a larger balance must not sort first");

        // Ties break on the name, so the order is total.
        let a = Validator {
            end_accumulated_fee: 10,
            validation_id: [1; 32],
            ..Default::default()
        };
        assert!(a < poor);
    }

    #[test]
    fn only_weight_nonce_and_balance_may_change_under_one_name() {
        let base = Validator {
            validation_id: [1; 32],
            chain_id: [2; 32],
            node_id: NodeId([3; 20]),
            public_key: vec![4; 96],
            start_time: 100,
            weight: 5,
            min_nonce: 0,
            end_accumulated_fee: 6,
            ..Default::default()
        };
        let mut moved = base.clone();
        moved.weight = 50;
        moved.min_nonce = 9;
        moved.end_accumulated_fee = 60;
        assert!(base.immutable_fields_unmodified(&moved));

        let mut impostor = base.clone();
        impostor.node_id = NodeId([7; 20]);
        assert!(!base.immutable_fields_unmodified(&impostor));

        // A different name is a different validator, so nothing is compared.
        let other = Validator {
            validation_id: [8; 32],
            ..Default::default()
        };
        assert!(base.immutable_fields_unmodified(&other));
    }

    #[test]
    fn an_expiry_sorts_the_same_way_its_bytes_do() {
        let early = Expiry {
            timestamp: 1,
            validation_id: [255; 32],
        };
        let late = Expiry {
            timestamp: 2,
            validation_id: [0; 32],
        };
        assert!(early < late);
        assert!(early.marshal() < late.marshal(), "the key order must be the value order");
        assert_eq!(Expiry::unmarshal(&early.marshal()), Some(early));
        assert_eq!(Expiry::unmarshal(&[0u8; 39]), None);
        assert_eq!(Expiry::unmarshal(&[0u8; 41]), None);
    }

    #[test]
    fn at_target_the_price_holds_and_the_cost_is_one_multiply() {
        let c = config();
        let s = FeeState {
            current: c.target,
            excess: 0,
        };
        assert_eq!(s.advance_time(c.target, 1_000), s, "at target nothing moves");
        assert_eq!(s.cost_of(&c, 10), 5_120);
        assert_eq!(s.seconds_remaining(&c, 100, 5_120), 10);
    }

    #[test]
    fn above_target_the_excess_climbs_and_below_it_drains() {
        let c = config();
        let hot = FeeState {
            current: c.target + 100,
            excess: 0,
        };
        assert_eq!(hot.advance_time(c.target, 3).excess, 300);
        let cold = FeeState {
            current: c.target - 100,
            excess: 1_000,
        };
        assert_eq!(cold.advance_time(c.target, 3).excess, 700);
        // And it stops at zero rather than going negative.
        assert_eq!(cold.advance_time(c.target, 1_000).excess, 0);
    }

    #[test]
    fn below_target_the_span_after_the_excess_empties_is_charged_at_the_floor() {
        let c = FeeConfig {
            target: 2_000_000,
            ..config()
        };
        // Two seconds to drain two e-folds of excess, then eight at the floor.
        let s = FeeState {
            current: c.target - 1_000_000,
            excess: 2_000_000,
        };
        let cost = s.cost_of(&c, 10);
        assert!(
            cost > 10 * c.min_price,
            "the seconds before the excess emptied must be charged above the floor"
        );
        assert!(
            cost < 10 * gas::calculate_price(c.min_price, 2_000_000, c.excess_conversion_constant),
            "and the price must fall as the excess drains, not hold at its peak"
        );
        // And the inverse agrees: that money buys exactly that span.
        assert_eq!(s.seconds_remaining(&c, 10, cost), 10);
    }

    #[test]
    fn an_unpayable_span_saturates_rather_than_wrapping() {
        let c = FeeConfig {
            capacity: 1,
            target: 1,
            min_price: u64::MAX,
            excess_conversion_constant: 1_000_000,
        };
        let s = FeeState {
            current: 1,
            excess: 0,
        };
        assert_eq!(s.cost_of(&c, 2), u64::MAX);
    }

    #[test]
    fn a_free_chain_buys_the_whole_span() {
        let c = FeeConfig {
            capacity: 10,
            target: 5,
            min_price: 0,
            excess_conversion_constant: 1_000_000,
        };
        let s = FeeState {
            current: 9,
            excess: 3,
        };
        assert_eq!(s.seconds_remaining(&c, 42, 0), 42);
    }
}
