// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What a staker is paid.
//!
//! The supply approaches a cap and never reaches it. What is left to mint is
//! divided by how much of the existing supply a staker put up and by how long
//! they put it up for, and a longer stake is paid at a higher rate — that last
//! part is the whole design: it is cheaper to stake once for a year than to
//! stake twelve times for a month, so the set is stable by arithmetic rather
//! than by a rule forbidding people to leave.
//!
//! ```text
//! remaining      = cap - supply
//! rate           = min_rate + (max_rate - min_rate) * duration / period
//! reward         = remaining * (amount / supply) * rate * (duration / period)
//! ```
//!
//! Every division happens once, at the end, on one product. Go does the same
//! with `math/big`, and the order is not stylistic: dividing earlier would
//! round differently and pay a different number, and every node must pay the
//! same number.
//!
//! Durations are nanoseconds, because Go's are — the same integer flows
//! through the same arithmetic and lands on the same result.

use std::time::Duration;

/// The denominator every rate and share is measured against.
pub const PERCENT_DENOMINATOR: u64 = 1_000_000;

/// A percentage of a value, without overflowing.
///
/// The naive `value * percent / DENOMINATOR` overflows a u64 at the weights
/// the P-Chain actually carries — a validator's weight runs to ~1.8e19 and the
/// denominator is 1e6, so the product does not fit. Splitting the value at the
/// denominator first keeps every intermediate inside a u64 and gives the same
/// answer everywhere the product would have fitted.
///
/// Go writes this arithmetic in two places — the staking-policy rate limit and
/// the slash amount — and both are this. One value, one function.
pub fn percent_of(value: u64, percent: u64) -> u64 {
    let denom = PERCENT_DENOMINATOR;
    (value / denom) * percent + (value % denom) * percent / denom
}

/// What a chain pays and how fast.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Config {
    /// The rate for a stake that lasts the whole minting period.
    pub max_consumption_rate: u64,
    /// The rate for a stake of no duration.
    pub min_consumption_rate: u64,
    /// The longest a stake may last, and the period the rates are stated over.
    pub minting_period: Duration,
    /// What the supply approaches.
    pub supply_cap: u64,
}

/// Answers what a stake is worth.
#[derive(Clone, Copy, Debug)]
pub struct Calculator {
    max_sub_min_consumption_rate: u128,
    min_consumption_rate: u128,
    minting_period: u128,
    supply_cap: u64,
}

impl Calculator {
    pub fn new(c: Config) -> Calculator {
        Calculator {
            max_sub_min_consumption_rate: (c.max_consumption_rate - c.min_consumption_rate) as u128,
            min_consumption_rate: c.min_consumption_rate as u128,
            minting_period: c.minting_period.as_nanos(),
            supply_cap: c.supply_cap,
        }
    }

    /// What to mint for a stake of `amount` held for `duration` against a
    /// supply of `current_supply`.
    ///
    /// The result is capped at what is left to mint, so the supply can be
    /// approached but never passed — the invariant the staker set relies on
    /// when it adds a potential reward to the supply before it is paid.
    pub fn calculate(&self, duration: Duration, amount: u64, current_supply: u64) -> u64 {
        let staked_duration = duration.as_nanos();
        let remaining_supply = self.supply_cap.saturating_sub(current_supply);
        if current_supply == 0 {
            // Go divides by the supply; a zero supply has no reward to divide.
            return 0;
        }

        let rate_numerator = self.max_sub_min_consumption_rate * staked_duration
            + self.min_consumption_rate * self.minting_period;
        let rate_denominator = self.minting_period * PERCENT_DENOMINATOR as u128;

        // One product, then the divisions, in Go's order. u128 is not wide
        // enough for the product at extreme inputs, so the arithmetic widens
        // as far as it needs to rather than wrapping.
        let mut reward = Big::from(remaining_supply);
        reward.mul_u128(rate_numerator);
        reward.mul_u128(amount as u128);
        reward.mul_u128(staked_duration);
        reward.div_u128(rate_denominator);
        reward.div_u128(current_supply as u128);
        reward.div_u128(self.minting_period);

        match reward.to_u64() {
            Some(r) => r.min(remaining_supply),
            // Go returns the whole remaining supply when the reward does not
            // fit in a u64 — the cap is the answer, not an overflow.
            None => remaining_supply,
        }
    }
}

/// Split a reward between a delegator and the validator it delegated to.
///
/// `shares` is the validator's cut in millionths. The remainder is computed
/// first and subtracted, so the two halves always add back to the whole — a
/// rounding that lost a unit would mint less than the supply was told it did.
pub fn split(total: u64, shares: u32) -> (u64, u64) {
    let remainder_shares = PERCENT_DENOMINATOR - shares as u64;
    // Go delays the rounding whenever the product fits, which changes the
    // answer for small totals — and small totals are where a delegator's whole
    // reward lives.
    let remainder = match remainder_shares.checked_mul(total) {
        Some(p) => p / PERCENT_DENOMINATOR,
        None => remainder_shares * (total / PERCENT_DENOMINATOR),
    };
    (total - remainder, remainder)
}

/// Just enough wide arithmetic for the reward product.
///
/// The reward multiplies four numbers that are each up to 64 bits, so the
/// product needs 256. Reaching for a bignum crate for one expression would add
/// a dependency to the consensus-critical path; this is the four operations
/// that expression uses, on a little-endian limb vector, and nothing else.
#[derive(Clone, Debug)]
struct Big {
    limbs: Vec<u64>,
}

impl Big {
    fn from(v: u64) -> Big {
        Big { limbs: vec![v] }
    }

    fn trim(&mut self) {
        while self.limbs.len() > 1 && *self.limbs.last().unwrap() == 0 {
            self.limbs.pop();
        }
    }

    fn mul_u128(&mut self, m: u128) {
        // Multiply by the low and high halves of `m` and add the shifted
        // result, so a full 128-bit multiplier is handled with 64-bit limbs.
        let lo = m as u64;
        let hi = (m >> 64) as u64;
        let low_part = self.mul_u64(lo);
        let mut high_part = self.mul_u64(hi);
        if !(high_part.limbs.len() == 1 && high_part.limbs[0] == 0) {
            high_part.limbs.insert(0, 0); // shift one limb = multiply by 2^64
        }
        *self = Big::add(&low_part, &high_part);
    }

    fn mul_u64(&self, m: u64) -> Big {
        let mut out = vec![0u64; self.limbs.len() + 1];
        let mut carry: u128 = 0;
        for (i, limb) in self.limbs.iter().enumerate() {
            let p = *limb as u128 * m as u128 + carry;
            out[i] = p as u64;
            carry = p >> 64;
        }
        out[self.limbs.len()] = carry as u64;
        let mut b = Big { limbs: out };
        b.trim();
        b
    }

    fn add(a: &Big, b: &Big) -> Big {
        let n = a.limbs.len().max(b.limbs.len());
        let mut out = Vec::with_capacity(n + 1);
        let mut carry = 0u128;
        for i in 0..n {
            let x = *a.limbs.get(i).unwrap_or(&0) as u128;
            let y = *b.limbs.get(i).unwrap_or(&0) as u128;
            let s = x + y + carry;
            out.push(s as u64);
            carry = s >> 64;
        }
        if carry != 0 {
            out.push(carry as u64);
        }
        let mut r = Big { limbs: out };
        r.trim();
        r
    }

    fn div_u128(&mut self, d: u128) {
        if d == 0 {
            return;
        }
        if d <= u64::MAX as u128 {
            self.div_u64(d as u64);
            return;
        }
        // A divisor above 64 bits: halve both sides until the divisor fits.
        // The reward's divisors are products of a period and a denominator and
        // are always even at this size, so no remainder is lost.
        let mut d = d;
        let mut shift = 0;
        while d > u64::MAX as u128 {
            d >>= 1;
            shift += 1;
        }
        self.shr(shift);
        self.div_u64(d as u64);
    }

    fn div_u64(&mut self, d: u64) {
        let mut rem: u128 = 0;
        for limb in self.limbs.iter_mut().rev() {
            let cur = (rem << 64) | *limb as u128;
            *limb = (cur / d as u128) as u64;
            rem = cur % d as u128;
        }
        self.trim();
    }

    fn shr(&mut self, bits: u32) {
        for _ in 0..bits {
            let mut carry = 0u64;
            for limb in self.limbs.iter_mut().rev() {
                let new_carry = *limb & 1;
                *limb = (*limb >> 1) | (carry << 63);
                carry = new_carry;
            }
        }
        self.trim();
    }

    fn to_u64(&self) -> Option<u64> {
        if self.limbs.len() > 1 {
            return None;
        }
        Some(self.limbs[0])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // The units the P-Chain counts in. Six decimals: one LUX is a million.
    const MICRO_LUX: u64 = 1;
    const MILLI_LUX: u64 = 1000 * MICRO_LUX;
    const LUX: u64 = 1000 * MILLI_LUX;
    const KILO_LUX: u64 = 1000 * LUX;
    const MEGA_LUX: u64 = 1000 * KILO_LUX;

    const MIN_STAKING_DURATION: Duration = Duration::from_secs(24 * 60 * 60);
    const MAX_STAKING_DURATION: Duration = Duration::from_secs(365 * 24 * 60 * 60);
    const MIN_VALIDATOR_STAKE: u64 = 5 * MILLI_LUX;

    fn default_config() -> Config {
        Config {
            max_consumption_rate: 12 * PERCENT_DENOMINATOR / 100,
            min_consumption_rate: 10 * PERCENT_DENOMINATOR / 100,
            minting_period: MAX_STAKING_DURATION,
            supply_cap: 720 * MEGA_LUX,
        }
    }

    /// Go's `TestRewards`, with its expected values verbatim.
    #[test]
    fn the_reward_table_matches_go() {
        let c = Calculator::new(default_config());
        let cases: &[(Duration, u64, u64, u64)] = &[
            // (720M - 360M) * (1M / 360M) * 12%
            (
                MAX_STAKING_DURATION,
                MEGA_LUX,
                360 * MEGA_LUX,
                120 * KILO_LUX,
            ),
            // (720M - 400M) * (1M / 400M) * 12%
            (
                MAX_STAKING_DURATION,
                MEGA_LUX,
                400 * MEGA_LUX,
                96 * KILO_LUX,
            ),
            // (720M - 400M) * (2M / 400M) * 12%
            (
                MAX_STAKING_DURATION,
                2 * MEGA_LUX,
                400 * MEGA_LUX,
                192 * KILO_LUX,
            ),
            // Nothing left to mint.
            (MAX_STAKING_DURATION, MEGA_LUX, 720 * MEGA_LUX, 0),
            // One day, at the rate a one-day stake earns.
            (MIN_STAKING_DURATION, MEGA_LUX, 360 * MEGA_LUX, 274_122_724),
            // A stake so small the reward rounds to one unit.
            (MIN_STAKING_DURATION, MIN_VALIDATOR_STAKE, 360 * MEGA_LUX, 1),
            (MIN_STAKING_DURATION, MEGA_LUX, 400 * MEGA_LUX, 219_298_179),
            (
                MIN_STAKING_DURATION,
                2 * MEGA_LUX,
                400 * MEGA_LUX,
                438_596_359,
            ),
            (MIN_STAKING_DURATION, MEGA_LUX, 720 * MEGA_LUX, 0),
        ];
        for (duration, amount, supply, want) in cases {
            assert_eq!(
                c.calculate(*duration, *amount, *supply),
                *want,
                "reward({duration:?}, {amount}, {supply})"
            );
        }
    }

    /// Go's `TestLongerDurationBonus`: twelve months of restaking earns less
    /// than one stake of a year. This is the property, not a number.
    #[test]
    fn staking_longer_pays_more_than_restaking() {
        let c = Calculator::new(default_config());
        let short = Duration::from_secs(24 * 60 * 60);
        let total = Duration::from_secs(365 * 24 * 60 * 60);

        let mut short_balance = KILO_LUX;
        for _ in 0..(total.as_secs() / short.as_secs()) {
            let r = c.calculate(short, short_balance, 359 * MEGA_LUX + short_balance);
            short_balance += r;
        }
        let leftover = Duration::from_secs(total.as_secs() % short.as_secs());
        short_balance += c.calculate(leftover, short_balance, 359 * MEGA_LUX + short_balance);

        let mut long_balance = KILO_LUX;
        long_balance += c.calculate(total, long_balance, 359 * MEGA_LUX + long_balance);

        assert!(
            short_balance < long_balance,
            "should promote stakers to stake longer: {short_balance} vs {long_balance}"
        );
    }

    /// Go's `TestRewardsOverflow`: a reward that cannot fit is the remaining
    /// supply, never a wrapped number.
    #[test]
    fn a_reward_larger_than_a_u64_is_capped_at_what_is_left() {
        let max_supply = u64::MAX;
        let initial_supply = 1u64;
        let c = Calculator::new(Config {
            max_consumption_rate: PERCENT_DENOMINATOR,
            min_consumption_rate: PERCENT_DENOMINATOR,
            minting_period: MIN_STAKING_DURATION,
            supply_cap: max_supply,
        });
        assert_eq!(
            c.calculate(MIN_STAKING_DURATION, max_supply, initial_supply),
            max_supply - initial_supply
        );
    }

    /// Go's `TestRewardsMint`: the same at a small cap.
    #[test]
    fn minting_never_passes_the_cap() {
        let max_supply = 1000u64;
        let initial_supply = 1u64;
        let c = Calculator::new(Config {
            max_consumption_rate: PERCENT_DENOMINATOR,
            min_consumption_rate: PERCENT_DENOMINATOR,
            minting_period: MIN_STAKING_DURATION,
            supply_cap: max_supply,
        });
        assert_eq!(
            c.calculate(MIN_STAKING_DURATION, max_supply, initial_supply),
            max_supply - initial_supply
        );
    }

    /// Go's `TestSplit`.
    #[test]
    fn a_reward_splits_the_way_go_splits_it() {
        let cases: &[(u64, u32, u64)] = &[
            (1000, (PERCENT_DENOMINATOR / 2) as u32, 500),
            (1, PERCENT_DENOMINATOR as u32, 1),
            // The delayed rounding: one unit and almost all the shares still
            // pays the one unit.
            (1, PERCENT_DENOMINATOR as u32 - 1, 1),
            (1, 1, 1),
            (1, 0, 0),
        ];
        for (amount, shares, want) in cases {
            let (from_shares, remainder) = split(*amount, *shares);
            assert_eq!(from_shares, *want, "split({amount}, {shares})");
            assert_eq!(
                from_shares + remainder,
                *amount,
                "the halves must add back to the whole"
            );
        }
    }

    #[test]
    fn a_split_always_adds_back_to_the_whole() {
        for total in [0u64, 1, 7, 999, 1_000_000, u64::MAX / 2] {
            for shares in [0u32, 1, 250_000, 999_999, 1_000_000] {
                let (a, b) = split(total, shares);
                assert_eq!(a + b, total, "split({total}, {shares})");
            }
        }
    }

    #[test]
    fn a_supply_at_the_cap_earns_nothing() {
        let c = Calculator::new(default_config());
        assert_eq!(
            c.calculate(MAX_STAKING_DURATION, MEGA_LUX, 720 * MEGA_LUX),
            0
        );
    }

    /// Go: `TestHIGH2_SlashAmountNoOverflow`, value for value.
    #[test]
    fn a_percentage_of_a_large_value_does_not_overflow() {
        let denom = PERCENT_DENOMINATOR;
        let cases: [(u64, u64, u64); 7] = [
            (1_000_000_000, 100_000, 100_000_000),
            (1_000_000_000, 500_000, 500_000_000),
            // The one that overflows the naive product.
            (u64::MAX / 2, 500_000, u64::MAX / 4),
            (u64::MAX, 100_000, u64::MAX / 10),
            (denom, 500_000, 500_000),
            (999_999, 500_000, 499_999),
            (1, 1, 0),
        ];
        for (value, percent, want) in cases {
            assert_eq!(
                percent_of(value, percent),
                want,
                "{percent} millionths of {value}"
            );
        }
    }

    /// Go: `TestHIGH2_SlashAmountMatchesNaive` — where the naive product does
    /// fit, the safe form gives exactly the same answer, so nothing was traded
    /// for the safety.
    #[test]
    fn where_the_naive_product_fits_the_answers_agree() {
        let denom = PERCENT_DENOMINATOR;
        for value in [0u64, 1, 100, 1_000_000, 1_000_000_000, denom, denom * 2] {
            for percent in [0u64, 1, 100_000, 500_000, 999_999, 1_000_000] {
                if value > 0 && percent > 0 && value > u64::MAX / percent {
                    continue; // the naive form would not fit
                }
                assert_eq!(
                    value * percent / denom,
                    percent_of(value, percent),
                    "value {value}, percent {percent}"
                );
            }
        }
    }
}
