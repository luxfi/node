// SPDX-License-Identifier: BSD-3-Clause-Eco

//! How much of its term a validator was reachable for.
//!
//! This is the measurement behind the reward gate, and it is deliberately NOT
//! chain state. Two honest nodes watching the same validator see slightly
//! different numbers — they connect at different moments, and their clocks are
//! their own — so a measurement can never be something every node has to agree
//! about. That is exactly why the reward is a *proposal*: each node answers it
//! from what it saw, and the stake-weighted vote settles which answer the chain
//! takes. A ledger of measurements is therefore per node, persisted beside the
//! chain rather than inside it.
//!
//! Go states this as `platformvm.uptimeTracker`, and the model is one sentence:
//! each connected peer's session accrues into a per-validator up-duration that
//! is folded forward to now on read, and written down on flush.
//!
//! - [`Tracker::connect`] records when a peer connected — including during
//!   bootstrap, before tracking begins, so a set that was already stable is
//!   fully observed the moment it starts being measured.
//! - [`Tracker::start_tracking`] baselines every validator, crediting the
//!   window nobody was watching as online, and switches to live measurement.
//!   Without that credit a validator that has been staked for a month reads as
//!   zero, and the reward gate takes a reward it earned.
//! - [`Tracker::fraction`] folds the currently-connected session forward, so a
//!   validator that stays connected accrues without ever disconnecting.
//! - [`Tracker::disconnect`] and [`Tracker::stop_tracking`] write the accrued
//!   session down.
//!
//! Everything here is in seconds, because that is the resolution the record is
//! kept at; working at one resolution keeps the interval arithmetic exact.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use crate::executor::Uptime;
use crate::ids::{Id, NodeId};

/// What has been measured of one validator: how long it has been up, and the
/// moment that was last worked out.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Measure {
    /// Seconds the validator has been reachable.
    pub up: u64,
    /// The unix second `up` was last brought forward to.
    pub last_updated: u64,
}

/// Where measurements are kept.
///
/// A trait because the node decides how they survive a restart; the chain has
/// no opinion, and must not, because these numbers are not agreed.
pub trait Record: Send + Sync {
    fn measure(&self, node: &NodeId, chain: &Id) -> Option<Measure>;
    fn set_measure(&self, node: &NodeId, chain: &Id, m: Measure);
    /// When the validator's term began. `None` for a peer that is not one.
    fn start_time(&self, node: &NodeId, chain: &Id) -> Option<u64>;
}

/// A ledger held in memory.
///
/// The one this crate ships. A node that wants its measurements to survive a
/// restart writes its own [`Record`] over whatever it already persists.
#[derive(Default)]
pub struct Ledger {
    inner: Mutex<HashMap<(Id, NodeId), (Measure, u64)>>,
}

impl Ledger {
    pub fn new() -> Ledger {
        Ledger::default()
    }

    /// Open a record for a validator that has just entered the set.
    ///
    /// Nothing is measured for a peer that is not a validator: an unopened
    /// record is how the tracker tells the two apart, and it is why connecting
    /// to the rest of the network costs nothing.
    pub fn note_validator(&self, node: NodeId, chain: Id, start_time: u64) {
        self.inner.lock().unwrap().insert(
            (chain, node),
            (
                Measure {
                    up: 0,
                    last_updated: start_time,
                },
                start_time,
            ),
        );
    }

    /// Forget a validator that has left.
    pub fn forget(&self, node: &NodeId, chain: &Id) {
        self.inner.lock().unwrap().remove(&(*chain, *node));
    }

    /// Open a record for everyone now validating `chain`, and close the ones
    /// who have left.
    ///
    /// A node calls this after accepting a block. The set is the chain's to
    /// say and the measurements are the node's to keep, and this is the one
    /// place the two meet — which is why it takes the state rather than the
    /// state holding a ledger.
    ///
    /// A validator already being measured keeps what it has: re-opening its
    /// record would forget everything this node had seen of it and hand it a
    /// clean slate every block.
    pub fn follow(&self, state: &crate::state::State, chain: &Id) {
        let mut inner = self.inner.lock().unwrap();
        let mut still_here = std::collections::HashSet::new();
        for staker in state.current_stakers() {
            if staker.chain != *chain || !staker.priority.is_current_validator() {
                continue;
            }
            still_here.insert(staker.node_id);
            inner.entry((*chain, staker.node_id)).or_insert((
                Measure {
                    up: 0,
                    last_updated: staker.start_time,
                },
                staker.start_time,
            ));
        }
        inner.retain(|(c, node), _| *c != *chain || still_here.contains(node));
    }
}

impl Record for Ledger {
    fn measure(&self, node: &NodeId, chain: &Id) -> Option<Measure> {
        self.inner
            .lock()
            .unwrap()
            .get(&(*chain, *node))
            .map(|(m, _)| *m)
    }

    fn set_measure(&self, node: &NodeId, chain: &Id, m: Measure) {
        if let Some(entry) = self.inner.lock().unwrap().get_mut(&(*chain, *node)) {
            entry.0 = m;
        }
    }

    fn start_time(&self, node: &NodeId, chain: &Id) -> Option<u64> {
        self.inner
            .lock()
            .unwrap()
            .get(&(*chain, *node))
            .map(|(_, start)| *start)
    }
}

/// Why the tracker refused.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    /// Tracking was already under way.
    AlreadyTracking,
    /// Tracking had not begun.
    NotTracking,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::AlreadyTracking => write!(f, "already tracking"),
            Error::NotTracking => write!(f, "not tracking"),
        }
    }
}

impl std::error::Error for Error {}

/// What this node saw of the validators it watches.
pub struct Tracker {
    chain: Id,
    record: Arc<dyn Record>,
    clock: Box<dyn Fn() -> u64 + Send + Sync>,
    inner: Mutex<Sessions>,
}

#[derive(Default)]
struct Sessions {
    /// Each connected peer, and the second it connected.
    connected: HashMap<NodeId, u64>,
    /// Whether live sessions are being counted. Before this, a validator is
    /// assumed to have been up since its record was last brought forward — the
    /// window nobody was watching is not evidence of absence.
    tracking: bool,
}

impl Tracker {
    /// A tracker for one network, reading a clock the caller owns.
    ///
    /// The clock is injected because a node with a wrong clock should be a
    /// configuration problem, and because a test should not be at the mercy of
    /// the wall.
    pub fn new(
        record: Arc<dyn Record>,
        chain: Id,
        clock: Box<dyn Fn() -> u64 + Send + Sync>,
    ) -> Tracker {
        Tracker {
            chain,
            record,
            clock,
            inner: Mutex::new(Sessions::default()),
        }
    }

    fn now(&self) -> u64 {
        (self.clock)()
    }

    /// Note that a peer connected.
    ///
    /// Repeating it keeps the original moment, so a router that dispatches the
    /// same live connection twice cannot restart the session clock — and so
    /// cannot be used to inflate anyone's uptime.
    pub fn connect(&self, node: NodeId) {
        let now = self.now();
        self.inner
            .lock()
            .unwrap()
            .connected
            .entry(node)
            .or_insert(now);
    }

    pub fn is_connected(&self, node: &NodeId) -> bool {
        self.inner.lock().unwrap().connected.contains_key(node)
    }

    /// Note that a peer disconnected, writing down what its session earned.
    ///
    /// Answers whether anything was written: nothing is, before tracking
    /// begins or for a peer that is not a validator, and a caller that
    /// persists on every write can skip the ones that wrote nothing.
    pub fn disconnect(&self, node: &NodeId) -> bool {
        let mut inner = self.inner.lock().unwrap();
        let tracking = inner.tracking;
        let wrote = if tracking {
            self.fold_forward(&inner, node)
        } else {
            false
        };
        inner.connected.remove(node);
        wrote
    }

    /// Begin measuring, crediting every validator with the window nobody was
    /// watching.
    pub fn start_tracking(&self, nodes: &[NodeId]) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        if inner.tracking {
            return Err(Error::AlreadyTracking);
        }
        for node in nodes {
            self.fold_forward(&inner, node);
        }
        inner.tracking = true;
        Ok(())
    }

    /// Stop measuring, writing down every session first.
    pub fn stop_tracking(&self, nodes: &[NodeId]) -> Result<(), Error> {
        let mut inner = self.inner.lock().unwrap();
        if !inner.tracking {
            return Err(Error::NotTracking);
        }
        for node in nodes {
            self.fold_forward(&inner, node);
        }
        inner.tracking = false;
        Ok(())
    }

    pub fn tracking(&self) -> bool {
        self.inner.lock().unwrap().tracking
    }

    /// What a validator has been up for, and the longest it could have been.
    pub fn measured(&self, node: &NodeId, chain: &Id) -> Option<(u64, u64)> {
        if *chain != self.chain {
            return Some((0, 0));
        }
        let start = self.record.start_time(node, chain)?;
        let inner = self.inner.lock().unwrap();
        let (up, _) = self.brought_forward(&inner, node)?;
        Some((up, self.now().saturating_sub(start)))
    }

    /// The fraction of its whole term a validator has been up for.
    pub fn fraction(&self, node: &NodeId, chain: &Id) -> Option<f64> {
        if *chain != self.chain {
            return Some(0.0);
        }
        let start = self.record.start_time(node, chain)?;
        self.fraction_from(node, chain, start)
    }

    /// The fraction of the time since `from` a validator has been up for.
    pub fn fraction_from(&self, node: &NodeId, chain: &Id, from: u64) -> Option<f64> {
        if *chain != self.chain {
            return Some(0.0);
        }
        let inner = self.inner.lock().unwrap();
        let (up, _) = self.brought_forward(&inner, node)?;
        let best = self.now().saturating_sub(from);
        if best == 0 {
            // Nothing has passed, so nothing was missed.
            return Some(1.0);
        }
        Some((up as f64 / best as f64).min(1.0))
    }

    /// A validator's up-duration brought forward to now, and the moment that
    /// is. This is the whole measurement, in five cases.
    fn brought_forward(&self, inner: &Sessions, node: &NodeId) -> Option<(u64, u64)> {
        let m = self.record.measure(node, &self.chain)?;
        let now = self.now();

        // A clock that has gone backwards is never allowed to subtract time or
        // to count an interval twice.
        if now < m.last_updated {
            return Some((m.up, m.last_updated));
        }
        // Not measuring yet: assume it was up since the record was last
        // brought forward. The window nobody watched is not evidence.
        if !inner.tracking {
            return Some((m.up + (now - m.last_updated), now));
        }
        // Measuring, and it is not there: it has been down since then.
        let Some(&connected_at) = inner.connected.get(node) else {
            return Some((m.up, now));
        };
        // Measuring, and it is there: credit from the later of when it
        // connected and when the record was last brought forward, so no second
        // is counted twice.
        let from = connected_at.max(m.last_updated);
        if now < from {
            return Some((m.up, now));
        }
        Some((m.up + (now - from), now))
    }

    /// Write a validator's up-duration down. Answers whether anything was
    /// written — a peer with no record is not a validator, and is skipped.
    fn fold_forward(&self, inner: &Sessions, node: &NodeId) -> bool {
        match self.brought_forward(inner, node) {
            Some((up, last_updated)) => {
                self.record
                    .set_measure(node, &self.chain, Measure { up, last_updated });
                true
            }
            None => false,
        }
    }
}

impl Uptime for Tracker {
    fn fraction_since(&self, node: &NodeId, chain: &Id, since: u64) -> Option<f64> {
        self.fraction_from(node, chain, since)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};
    use std::sync::Arc;

    const HOUR: u64 = 60 * 60;
    const DAY: u64 = 24 * HOUR;

    struct Clock(Arc<AtomicU64>);

    fn at(start: u64) -> (Arc<AtomicU64>, Box<dyn Fn() -> u64 + Send + Sync>) {
        let now = Arc::new(AtomicU64::new(start));
        let handle = Clock(now.clone());
        (now, Box::new(move || handle.0.load(Ordering::SeqCst)))
    }

    fn node(n: u8) -> NodeId {
        NodeId([n; 20])
    }

    const CHAIN: Id = [1; 32];
    const OTHER_CHAIN: Id = [2; 32];

    /// A validator staked a month ago must read ~100%, not zero.
    ///
    /// Go names this the regression test for the reward gate: the tracker it
    /// replaced never started tracking, so a stable validator's stored
    /// up-duration was zero and the gate took a reward it had earned.
    #[test]
    fn a_validator_that_has_been_staked_a_month_reads_as_up() {
        let now = 1_700_000_000;
        let (_clock, read) = at(now);
        let ledger = Ledger::new();
        let start = now - 30 * DAY;
        ledger.note_validator(node(5), CHAIN, start);

        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);
        let fraction = tracker
            .fraction_from(&node(5), &CHAIN, start)
            .expect("a validator");
        assert!(
            (fraction - 1.0).abs() < 0.001,
            "a continuously staked validator reads {fraction}, not ~1.0"
        );
    }

    /// Beginning to measure credits the window nobody was watching.
    #[test]
    fn beginning_to_measure_credits_the_unwatched_window() {
        let now = 1_700_000_000;
        let (_clock, read) = at(now);
        let ledger = Ledger::new();
        let start = now - HOUR;
        ledger.note_validator(node(5), CHAIN, start);
        let ledger = Arc::new(ledger);
        let peek = ledger.clone();

        let tracker = Tracker::new(ledger, CHAIN, read);
        assert!(!tracker.tracking());
        assert_eq!(tracker.start_tracking(&[node(5)]), Ok(()));
        assert!(tracker.tracking());

        let m = peek.measure(&node(5), &CHAIN).unwrap();
        assert_eq!(m.up, HOUR);
        assert_eq!(m.last_updated, now);
    }

    /// A validator that connects and stays connected climbs, without ever
    /// disconnecting. That is the case a tracker that only wrote on disconnect
    /// could never see.
    #[test]
    fn a_validator_that_stays_connected_climbs() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);

        // It connects while the node is still bootstrapping.
        tracker.connect(node(5));
        assert!(tracker.is_connected(&node(5)));
        assert_eq!(tracker.start_tracking(&[node(5)]), Ok(()));

        clock.store(start + HOUR, Ordering::SeqCst);
        let one = tracker.fraction(&node(5), &CHAIN).unwrap();
        assert!((one - 1.0).abs() < 0.001, "after an hour: {one}");
        assert!(tracker.is_connected(&node(5)));

        clock.store(start + 2 * HOUR, Ordering::SeqCst);
        let two = tracker.fraction(&node(5), &CHAIN).unwrap();
        assert!((two - 1.0).abs() < 0.001, "after two hours: {two}");
    }

    /// A session is written down when the peer goes, and nothing accrues after.
    #[test]
    fn a_session_is_written_down_when_the_peer_goes() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();

        tracker.connect(node(5));
        clock.store(start + 30 * 60, Ordering::SeqCst);
        assert!(tracker.disconnect(&node(5)), "a tracked session is written");
        assert!(!tracker.is_connected(&node(5)));

        // Half an hour up out of an hour.
        clock.store(start + HOUR, Ordering::SeqCst);
        let half = tracker.fraction(&node(5), &CHAIN).unwrap();
        assert!((half - 0.5).abs() < 0.001, "{half}");
    }

    /// A peer that goes before measurement began writes nothing, so a caller
    /// that persists on every write is not made to persist nothing.
    #[test]
    fn a_peer_that_goes_before_measurement_began_writes_nothing() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let ledger = Arc::new(ledger);
        let peek = ledger.clone();
        let tracker = Tracker::new(ledger, CHAIN, read);
        // Deliberately not tracking: the node is still catching up.

        tracker.connect(node(5));
        clock.store(start + 30 * 60, Ordering::SeqCst);
        assert!(!tracker.disconnect(&node(5)));
        assert!(!tracker.is_connected(&node(5)));
        assert_eq!(peek.measure(&node(5), &CHAIN).unwrap().up, 0);
    }

    /// A validator this node never sees earns nothing from it. This is the
    /// property that lets the reward gate withhold a reward at all.
    #[test]
    fn a_validator_this_node_never_sees_earns_nothing_from_it() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();

        clock.store(start + HOUR, Ordering::SeqCst);
        assert_eq!(tracker.fraction(&node(5), &CHAIN), Some(0.0));
        assert!(!tracker.is_connected(&node(5)));
    }

    /// Stopping writes every session down first.
    #[test]
    fn stopping_writes_every_session_down() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        let nodes: Vec<NodeId> = (0..10).map(node).collect();
        for n in &nodes {
            ledger.note_validator(*n, CHAIN, start);
        }
        let ledger = Arc::new(ledger);
        let peek = ledger.clone();
        let tracker = Tracker::new(ledger, CHAIN, read);
        tracker.start_tracking(&nodes).unwrap();
        for n in &nodes {
            tracker.connect(*n);
        }

        clock.store(start + 3 * 60, Ordering::SeqCst);
        assert_eq!(tracker.stop_tracking(&nodes), Ok(()));
        assert!(!tracker.tracking());
        for n in &nodes {
            assert_eq!(peek.measure(n, &CHAIN).unwrap().up, 3 * 60);
        }
    }

    #[test]
    fn measurement_starts_once_and_stops_only_after_it_started() {
        let start = 1_700_000_000;
        let (_clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);

        assert_eq!(tracker.stop_tracking(&[]), Err(Error::NotTracking));
        assert_eq!(tracker.start_tracking(&[node(5)]), Ok(()));
        assert_eq!(
            tracker.start_tracking(&[node(5)]),
            Err(Error::AlreadyTracking)
        );
    }

    /// A peer that is not a validator costs nothing and is not an error.
    #[test]
    fn a_peer_that_is_not_a_validator_is_watched_for_free() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let tracker = Tracker::new(Arc::new(Ledger::new()), CHAIN, read);

        assert_eq!(tracker.start_tracking(&[node(9)]), Ok(()));
        tracker.connect(node(9));
        clock.store(start + 60, Ordering::SeqCst);
        assert!(!tracker.disconnect(&node(9)));
        // And asking about one says so, rather than answering zero.
        assert_eq!(tracker.fraction(&node(9), &CHAIN), None);
    }

    /// A question about another network is not this tracker's to answer.
    #[test]
    fn another_networks_question_is_answered_with_nothing() {
        let start = 1_700_000_000;
        let (_clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start - HOUR);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);

        assert_eq!(tracker.fraction(&node(5), &OTHER_CHAIN), Some(0.0));
        assert_eq!(tracker.measured(&node(5), &OTHER_CHAIN), Some((0, 0)));
    }

    /// Connecting twice keeps the first moment, so a repeated dispatch of one
    /// live connection cannot inflate anyone's uptime.
    #[test]
    fn connecting_twice_keeps_the_first_moment() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let ledger = Arc::new(ledger);
        let peek = ledger.clone();
        let tracker = Tracker::new(ledger, CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();

        tracker.connect(node(5));
        clock.store(start + 5 * 60, Ordering::SeqCst);
        tracker.connect(node(5)); // the same connection, dispatched again
        clock.store(start + 10 * 60, Ordering::SeqCst);
        assert!(tracker.disconnect(&node(5)));

        // Ten minutes, not five.
        assert_eq!(peek.measure(&node(5), &CHAIN).unwrap().up, 600);
    }

    /// Cycling never fabricates a second beyond the intervals actually held.
    #[test]
    fn cycling_never_fabricates_a_second() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let ledger = Arc::new(ledger);
        let peek = ledger.clone();
        let tracker = Tracker::new(ledger, CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();

        // A hundred cycles with no time passing earns nothing.
        for _ in 0..100 {
            tracker.connect(node(5));
            tracker.disconnect(&node(5));
        }
        assert_eq!(peek.measure(&node(5), &CHAIN).unwrap().up, 0);

        // Fifty cycles of one second each earns fifty.
        for i in 0..50 {
            tracker.connect(node(5));
            clock.store(start + i + 1, Ordering::SeqCst);
            tracker.disconnect(&node(5));
        }
        assert_eq!(peek.measure(&node(5), &CHAIN).unwrap().up, 50);
    }

    /// A clock that goes backwards subtracts nothing.
    #[test]
    fn a_clock_that_goes_backwards_subtracts_nothing() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();
        tracker.connect(node(5));

        clock.store(start - HOUR, Ordering::SeqCst);
        assert_eq!(tracker.measured(&node(5), &CHAIN), Some((0, 0)));
        assert_eq!(tracker.fraction_from(&node(5), &CHAIN, start), Some(1.0));
    }

    /// Many threads asking and answering at once, which is what a running node
    /// does.
    #[test]
    fn many_threads_may_ask_and_answer_at_once() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Arc::new(Tracker::new(Arc::new(ledger), CHAIN, read));
        tracker.start_tracking(&[node(5)]).unwrap();

        let mut threads = Vec::new();
        for i in 0..100 {
            let t = tracker.clone();
            threads.push(std::thread::spawn(move || match i % 4 {
                0 => {
                    t.connect(node(5));
                }
                1 => {
                    t.disconnect(&node(5));
                }
                2 => {
                    t.fraction(&node(5), &CHAIN);
                }
                _ => {
                    t.is_connected(&node(5));
                }
            }));
        }
        for t in threads {
            t.join().unwrap();
        }
        clock.store(start + 60, Ordering::SeqCst);
        assert!(tracker.fraction(&node(5), &CHAIN).is_some());
    }

    /// And the tracker is what the reward gate asks.
    #[test]
    fn the_tracker_answers_the_reward_gate() {
        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Ledger::new();
        ledger.note_validator(node(5), CHAIN, start);
        let tracker = Tracker::new(Arc::new(ledger), CHAIN, read);
        tracker.start_tracking(&[node(5)]).unwrap();
        tracker.connect(node(5));
        clock.store(start + HOUR, Ordering::SeqCst);

        let asked: &dyn Uptime = &tracker;
        let fraction = asked.fraction_since(&node(5), &CHAIN, start).unwrap();
        assert!((fraction - 1.0).abs() < 0.001);
    }

    /// The set is the chain's to say; the measurements are the node's to keep.
    #[test]
    fn a_node_follows_the_set_the_chain_agreed_on() {
        use crate::state::{Staker, State};
        use crate::txs::Priority;

        let start = 1_700_000_000;
        let (clock, read) = at(start);
        let ledger = Arc::new(Ledger::new());
        let tracker = Tracker::new(ledger.clone(), CHAIN, read);

        let mut state = State::new();
        state.set_timestamp(start);
        let vdr = |n: u8, start_time: u64| Staker {
            tx_id: [n; 32],
            node_id: node(n),
            public_key: None,
            chain: CHAIN,
            weight: 1,
            start_time,
            end_time: start_time + DAY,
            potential_reward: 0,
            next_time: start_time + DAY,
            priority: Priority::PrimaryNetworkValidatorCurrent,
        };
        state.put_current_validator(vdr(5, start)).unwrap();
        ledger.follow(&state, &CHAIN);

        // Now it is watched, and being connected earns it time.
        tracker.start_tracking(&[node(5)]).unwrap();
        tracker.connect(node(5));
        clock.store(start + HOUR, Ordering::SeqCst);
        let seen = tracker.fraction(&node(5), &CHAIN).unwrap();
        assert!((seen - 1.0).abs() < 0.001);

        // Following again does not hand it a clean slate.
        ledger.follow(&state, &CHAIN);
        assert_eq!(
            ledger.measure(&node(5), &CHAIN).unwrap().last_updated,
            start
        );
        assert!(tracker.fraction(&node(5), &CHAIN).unwrap() > 0.99);

        // And a validator that has left is no longer watched.
        state.delete_current_validator(&vdr(5, start));
        ledger.follow(&state, &CHAIN);
        assert_eq!(ledger.measure(&node(5), &CHAIN), None);
        assert_eq!(tracker.fraction(&node(5), &CHAIN), None);
    }
}
