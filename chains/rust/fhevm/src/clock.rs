// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Time, and the one clock F is allowed to read.
//!
//! CHAIN time comes from the accepting block and is what every stored timestamp
//! and every expiry is measured against. WALL time is what a node happens to
//! think the hour is, and F reads it in exactly two places: admission, where a
//! wrong answer costs a retry, and the read surface, where it costs a stale
//! reply. Nothing under acceptance reads it, because two validators replaying one
//! block must write the same bytes — `tests/replay.rs` is what holds that line.
//!
//! [`Time`] keeps nanoseconds because Go's `time.Time` does, and the difference is
//! observable: a proposer whose clock reads half a second past its own tip has a
//! time that is AFTER the tip while its whole-second form is the same second. A
//! port that rounded would take the other branch there and build a different
//! block — no fork, since both are valid, but not the same chain either.

use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

/// A point in time, to the nanosecond, as a count from the unix epoch.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Default)]
pub struct Time(i128);

/// A nanosecond count of one second.
const NANOS: i128 = 1_000_000_000;

impl Time {
    /// A whole number of seconds since the epoch — the resolution a block's
    /// timestamp crosses the wire at.
    pub const fn from_unix(secs: i64) -> Self {
        Time(secs as i128 * NANOS)
    }

    pub const fn from_nanos(nanos: i128) -> Self {
        Time(nanos)
    }

    /// The whole seconds, rounding toward negative infinity as Go's `Unix` does.
    pub fn unix(&self) -> i64 {
        self.0.div_euclid(NANOS) as i64
    }

    pub fn nanos(&self) -> i128 {
        self.0
    }

    pub fn add_secs(&self, secs: i64) -> Self {
        Time(self.0 + secs as i128 * NANOS)
    }

    pub fn add_nanos(&self, nanos: i128) -> Self {
        Time(self.0 + nanos)
    }

    pub fn after(&self, other: &Time) -> bool {
        self.0 > other.0
    }

    pub fn before(&self, other: &Time) -> bool {
        self.0 < other.0
    }
}

/// The node's clock, settable so a test can name the hour.
#[derive(Debug, Default)]
pub struct Clock {
    fixed: Mutex<Option<Time>>,
}

impl Clock {
    pub fn new() -> Self {
        Clock { fixed: Mutex::new(None) }
    }

    /// Pins the clock. Every later read answers this until it is set again.
    pub fn set(&self, t: Time) {
        *self.fixed.lock().unwrap() = Some(t);
    }

    /// What this node thinks the hour is.
    pub fn time(&self) -> Time {
        if let Some(t) = *self.fixed.lock().unwrap() {
            return t;
        }
        let d = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("the system clock is not before the unix epoch");
        Time(d.as_nanos() as i128)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn whole_seconds_are_what_crosses_the_wire() {
        let t = Time::from_unix(1_700_000_000);
        assert_eq!(t.unix(), 1_700_000_000);
        assert_eq!(t.add_nanos(999_999_999).unix(), 1_700_000_000, "the sub-second part is dropped");
        assert_eq!(t.add_secs(1).unix(), 1_700_000_001);
    }

    #[test]
    fn a_sub_second_step_is_after_without_being_a_later_second() {
        let tip = Time::from_unix(100);
        let half = tip.add_nanos(500_000_000);
        assert!(half.after(&tip), "half a second later is later");
        assert_eq!(half.unix(), tip.unix(), "and still the same second on the wire");
        assert!(!tip.after(&tip));
        assert!(tip.before(&half));
    }

    #[test]
    fn a_time_before_the_epoch_rounds_the_way_go_rounds_it() {
        assert_eq!(Time::from_unix(-5).unix(), -5);
        assert_eq!(Time::from_unix(-5).add_nanos(1).unix(), -5);
    }

    #[test]
    fn a_pinned_clock_answers_what_it_was_told() {
        let c = Clock::new();
        let before = c.time();
        c.set(Time::from_unix(42));
        assert_eq!(c.time(), Time::from_unix(42));
        assert_ne!(before, Time::from_unix(42));
    }
}
