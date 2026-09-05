// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What consumption costs, and how the price answers demand.
//!
//! Ported from Go `vms/components/gas` (`gas.go`, `state.go`), the LP-103
//! mechanism. The price is `min_price · e^(excess / K)`, approximated by the
//! EIP-4844 fake-exponential series, so a chain running above its target gets
//! dearer smoothly rather than in steps and nobody has to be asked.
//!
//! The P-Chain uses this twice for different things. The transaction fee is one
//! (not ported — see LLM.md). The other is the LP-77 continuous fee an L1
//! validator pays for the P-Chain's trouble in tracking it, in [`crate::l1`],
//! and that one *is* here, because without a price there is no way to say when
//! a validator has stopped paying.
//!
//! The arithmetic saturates rather than wrapping. These are clock quantities,
//! not money: a capacity that would overflow is simply the largest capacity
//! there is, and an unpayable price is unpayable rather than free.

/// Gas, and the price of it. Two names for a `u64` because multiplying one by
/// the other is money and multiplying two of either is nothing.
pub type Gas = u64;
pub type Price = u64;

/// `g + per_second · seconds`, saturating at `u64::MAX`.
pub fn add_per_second(g: Gas, per_second: Gas, seconds: u64) -> Gas {
    per_second
        .checked_mul(seconds)
        .and_then(|added| g.checked_add(added))
        .unwrap_or(u64::MAX)
}

/// `g - per_second · seconds`, saturating at zero.
pub fn sub_per_second(g: Gas, per_second: Gas, seconds: u64) -> Gas {
    match per_second.checked_mul(seconds) {
        Some(removed) => g.saturating_sub(removed),
        // A product that overflows is larger than anything `g` can be, so the
        // whole of `g` is removed. Go returns 0 here for the same reason.
        None => 0,
    }
}

/// The chain's fee position: what it may still spend, and how far above target
/// it has been running.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct State {
    pub capacity: Gas,
    pub excess: Gas,
}

impl State {
    /// Go: `State.AdvanceTime`. Capacity grows toward its ceiling and excess
    /// falls toward zero, both at a fixed rate per second.
    pub fn advance_time(
        &self,
        max_capacity: Gas,
        max_per_second: Gas,
        target_per_second: Gas,
        duration: u64,
    ) -> State {
        State {
            capacity: add_per_second(self.capacity, max_per_second, duration).min(max_capacity),
            excess: sub_per_second(self.excess, target_per_second, duration),
        }
    }

    /// Go: `State.ConsumeGas`. Spending costs capacity and raises the excess,
    /// which raises the price. Asking for more than the chain has is refused;
    /// an excess that would overflow saturates, because it is a signal and not
    /// a balance.
    pub fn consume(&self, gas: Gas) -> Option<State> {
        let capacity = self.capacity.checked_sub(gas)?;
        Some(State {
            capacity,
            excess: self.excess.saturating_add(gas),
        })
    }
}

/// `min_price · e^(excess / k)`, by the EIP-4844 fake exponential.
///
/// Go computes this in `uint256` because every intermediate is bounded by
/// 2^193; [`Wide`] is that headroom here. A `k` of zero is a chain with no
/// demand response at all, and the series would divide by it, so the floor
/// price is the whole answer.
pub fn calculate_price(min_price: Price, excess: Gas, k: Gas) -> Price {
    if k == 0 {
        return min_price;
    }
    let mut accum = Wide::from_u64(min_price);
    accum.mul_u64(k);

    // The output is divided by `k` at the end, so anything reaching `k · 2^64`
    // is already the largest price there is.
    let mut max_output = Wide::from_u64(k);
    max_output.mul_u64(u64::MAX);

    let mut out = Wide::ZERO;
    let mut i: u64 = 1;
    while !accum.is_zero() {
        out.add(&accum);
        if out.cmp(&max_output) != std::cmp::Ordering::Less {
            return u64::MAX;
        }
        accum.mul_u64(excess);
        accum.div_u64(k);
        accum.div_u64(i);
        i += 1;
    }
    out.div_u64(k);
    out.to_u64()
}

/// A 512-bit unsigned integer, in eight little-endian limbs.
///
/// Only what the price curve and the quorum comparison need: add, multiply by a
/// `u64`, divide by a `u64`, compare. Go reaches for `uint256`; this is the same
/// headroom without a dependency that would have to agree with it about
/// rounding.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Wide {
    limbs: [u64; 8],
}

impl Wide {
    pub const ZERO: Wide = Wide { limbs: [0; 8] };

    pub fn from_u64(v: u64) -> Wide {
        let mut w = Wide::ZERO;
        w.limbs[0] = v;
        w
    }

    pub fn is_zero(&self) -> bool {
        self.limbs.iter().all(|l| *l == 0)
    }

    pub fn add(&mut self, other: &Wide) {
        let mut carry = 0u128;
        for i in 0..8 {
            let sum = self.limbs[i] as u128 + other.limbs[i] as u128 + carry;
            self.limbs[i] = sum as u64;
            carry = sum >> 64;
        }
    }

    pub fn mul_u64(&mut self, v: u64) {
        let mut carry = 0u128;
        for limb in self.limbs.iter_mut() {
            let product = *limb as u128 * v as u128 + carry;
            *limb = product as u64;
            carry = product >> 64;
        }
    }

    /// Long division, most significant limb first. A divisor of zero leaves the
    /// value alone rather than trapping: the callers here all guard it, and a
    /// panic in a block executor is a way to stop a node from a distance.
    pub fn div_u64(&mut self, v: u64) {
        if v == 0 {
            return;
        }
        let mut remainder = 0u128;
        for i in (0..8).rev() {
            let cur = (remainder << 64) | self.limbs[i] as u128;
            self.limbs[i] = (cur / v as u128) as u64;
            remainder = cur % v as u128;
        }
    }

    #[allow(clippy::should_implement_trait)]
    pub fn cmp(&self, other: &Wide) -> std::cmp::Ordering {
        for i in (0..8).rev() {
            match self.limbs[i].cmp(&other.limbs[i]) {
                std::cmp::Ordering::Equal => continue,
                other => return other,
            }
        }
        std::cmp::Ordering::Equal
    }

    /// The low limb. Callers use this only where the value is known to fit,
    /// which for the price curve is guaranteed by the `max_output` cutoff.
    pub fn to_u64(&self) -> u64 {
        self.limbs[0]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_zero_conversion_constant_is_the_floor_price() {
        // Go would divide by it. The floor is the only defined answer.
        assert_eq!(calculate_price(100, 1_000, 0), 100);
    }

    #[test]
    fn no_excess_is_the_floor_price() {
        // e^0 = 1, so the series is one term.
        assert_eq!(calculate_price(2_000, 0, 1_000_000), 2_000);
    }

    #[test]
    fn the_price_rises_with_the_excess_and_never_falls() {
        let mut last = 0;
        for excess in (0..2_000_000).step_by(100_000) {
            let price = calculate_price(2_000, excess, 1_000_000);
            assert!(price >= last, "the price fell at excess {excess}");
            last = price;
        }
        // One K of excess is one e-fold: 2000·e ≈ 5436.
        assert_eq!(calculate_price(2_000, 1_000_000, 1_000_000), 5_436);
    }

    #[test]
    fn an_unpayable_price_saturates_rather_than_wrapping() {
        // Go returns MaxUint64 once the series passes k·2^64. A wrap here would
        // make an unpayable validator free.
        assert_eq!(calculate_price(u64::MAX, u64::MAX, 1), u64::MAX);
    }

    #[test]
    fn per_second_arithmetic_saturates_at_both_ends() {
        assert_eq!(add_per_second(5, 2, 3), 11);
        assert_eq!(add_per_second(u64::MAX, 1, 1), u64::MAX);
        assert_eq!(add_per_second(1, u64::MAX, u64::MAX), u64::MAX);
        assert_eq!(sub_per_second(11, 2, 3), 5);
        assert_eq!(sub_per_second(1, 2, 3), 0);
        assert_eq!(sub_per_second(u64::MAX, u64::MAX, u64::MAX), 0);
    }

    #[test]
    fn advancing_the_clock_fills_capacity_to_its_ceiling_and_drains_excess() {
        let s = State {
            capacity: 10,
            excess: 100,
        };
        let out = s.advance_time(50, 30, 40, 2);
        assert_eq!(out.capacity, 50, "capacity is capped, not overrun");
        assert_eq!(out.excess, 20);
        // Draining below zero is zero, not a wrap.
        assert_eq!(s.advance_time(50, 0, 1_000, 1).excess, 0);
    }

    #[test]
    fn consuming_more_than_the_chain_holds_is_refused() {
        let s = State {
            capacity: 10,
            excess: 0,
        };
        assert_eq!(
            s.consume(4),
            Some(State {
                capacity: 6,
                excess: 4
            })
        );
        assert_eq!(s.consume(11), None);
        // The excess saturates; only the capacity is a balance.
        let hot = State {
            capacity: 10,
            excess: u64::MAX,
        };
        assert_eq!(hot.consume(1).unwrap().excess, u64::MAX);
    }

    #[test]
    fn the_wide_integer_carries_across_limbs() {
        let mut w = Wide::from_u64(u64::MAX);
        w.mul_u64(u64::MAX);
        // (2^64-1)^2 = 2^128 - 2^65 + 1.
        let mut expected = Wide::ZERO;
        expected.limbs[0] = 1;
        expected.limbs[1] = u64::MAX - 1;
        assert_eq!(w, expected);

        // And dividing it back is exact.
        w.div_u64(u64::MAX);
        assert_eq!(w, Wide::from_u64(u64::MAX));
    }

    #[test]
    fn the_wide_integer_orders_by_its_high_limb_first() {
        let mut big = Wide::from_u64(1);
        big.mul_u64(u64::MAX);
        big.mul_u64(u64::MAX);
        assert_eq!(Wide::from_u64(u64::MAX).cmp(&big), std::cmp::Ordering::Less);
        assert_eq!(big.cmp(&big), std::cmp::Ordering::Equal);
    }
}
