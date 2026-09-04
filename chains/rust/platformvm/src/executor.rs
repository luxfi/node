// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What a transaction does to the chain.
//!
//! Two executors, because the P-Chain has two kinds of transaction and they
//! answer different questions.
//!
//! A **standard** transaction is one somebody submitted. It is checked against
//! the state, it pays a fee, and it either applies or it does not. Adding a
//! validator is one of these, which is the sentence that makes this chain a
//! public good: [`Unsigned::AddPermissionlessValidator`] consults no owner and
//! no allowlist. It checks that the stake is real, that it is large enough,
//! that the node is not already in the set, and that the money adds up — and
//! then anyone is in.
//!
//! A **proposal** transaction is one the chain emits about itself, and it has
//! two futures. Retiring a validator either pays it or does not, and which one
//! happens is decided by consensus after the transaction is already agreed on.
//! So a proposal is executed twice, onto two states, and the block that
//! follows picks one.
//!
//! The fee is asked for rather than computed here. What a transaction costs is
//! a policy — Go injects a `fee.Calculator` for exactly this reason — and an
//! executor that priced its own transactions would be two decisions in one
//! place.

use std::collections::HashMap;

use crate::components::{Output, Utxo};
use crate::flow;
use crate::ids::{Id, NodeId, PRIMARY_NETWORK_ID};
use crate::reward;
use crate::state::{Error as StateError, Staker, State};
use crate::txs::{bounded_by, Kind, Priority, Tx, Unsigned};

/// How much stake a validator may have behind it relative to its own.
///
/// Go's `MaxValidatorWeightFactor`. It is what stops one node from renting the
/// whole network's weight: a validator's delegations may not exceed four times
/// its own stake, so influence stays anchored to skin in the game.
pub const MAX_VALIDATOR_WEIGHT_FACTOR: u64 = 5;

/// How far ahead of local time a block's timestamp may be.
pub const SYNC_BOUND: u64 = 10;

/// The rules the primary network stakes under.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct StakingPolicy {
    pub min_validator_stake: u64,
    pub max_validator_stake: u64,
    pub min_delegator_stake: u64,
    /// Seconds.
    pub min_stake_duration: u64,
    pub max_stake_duration: u64,
    /// The smallest cut a validator may take from its delegators, in
    /// millionths.
    pub min_delegation_fee: u32,
}

/// Everything the executor needs that is not state.
#[derive(Clone, Debug)]
pub struct Config {
    /// The asset stake and fees are denominated in.
    pub native_asset: Id,
    pub staking: StakingPolicy,
    pub reward: reward::Config,
    /// False while the node is still catching up. Go skips the checks that
    /// need a complete view of the world until this is true, because a node
    /// that has not finished syncing would refuse valid transactions.
    pub bootstrapped: bool,
}

/// What a transaction costs.
///
/// Injected, not computed: Go passes a `fee.Calculator` into every execution
/// for the same reason.
pub trait Fees {
    fn fee(&self, tx: &Unsigned) -> u64;
}

/// The flat per-kind fee schedule.
///
/// Go states it as `fee.StaticConfig` and it is reproduced field for field.
/// The gas-metered alternative — complexity times weights times a price that
/// moves with demand — is not in this port; see LLM.md.
#[derive(Clone, Copy, Debug, Default)]
pub struct FlatFees {
    pub tx: u64,
    pub create_chain: u64,
    pub create_network: u64,
    pub transform_chain: u64,
    pub add_primary_validator: u64,
    pub add_primary_delegator: u64,
    pub add_chain_validator: u64,
    pub add_chain_delegator: u64,
}

impl Fees for FlatFees {
    fn fee(&self, tx: &Unsigned) -> u64 {
        match tx {
            // The chain's own transaction. Nobody submitted it, so nobody pays.
            Unsigned::RewardValidator { .. } => 0,
            Unsigned::CreateChain { .. } => self.create_chain,
            Unsigned::AddValidator { .. } => self.add_primary_validator,
            Unsigned::AddDelegator { .. } => self.add_primary_delegator,
            Unsigned::AddChainValidator { .. } => self.add_chain_validator,
            Unsigned::AddPermissionlessValidator { chain, .. } => {
                if *chain == PRIMARY_NETWORK_ID {
                    self.add_primary_validator
                } else {
                    self.add_chain_validator
                }
            }
            Unsigned::AddPermissionlessDelegator { chain, .. } => {
                if *chain == PRIMARY_NETWORK_ID {
                    self.add_primary_delegator
                } else {
                    self.add_chain_delegator
                }
            }
            _ => self.tx,
        }
    }
}

/// Why a transaction did not execute.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Syntactic(crate::txs::Error),
    Flow(flow::Error),
    /// The signatures do not authorise the spend.
    Credential(flow::CredentialError),
    State(StateError),
    /// This executor does not run this kind of transaction.
    WrongTxType(Kind),
    /// A validator with no node.
    EmptyNodeId,
    /// The legacy scheduled-staker transactions. Go refuses them permanently:
    /// stakers now enter the current set immediately, so a transaction that
    /// schedules one for later has no meaning left.
    AddValidatorNotPermitted,
    AddDelegatorNotPermitted,
    /// The node is already validating this network.
    DuplicateValidator,
    /// Not a current or pending validator.
    NotValidator,
    /// Only a validator admitted by name may be removed by name.
    RemovePermissionlessValidator,
    WeightTooSmall,
    WeightTooLarge,
    InsufficientDelegationFee,
    StakeTooShort,
    StakeTooLong,
    WrongStakedAsset,
    /// The delegation would take the validator past its weight limit.
    OverDelegated,
    /// The staking period does not sit inside the period it depends on.
    PeriodMismatch,
    /// A delegator pointed at a validator that was admitted by name.
    DelegateToPermissionedValidator,
    /// A reward transaction naming a staker that is not the next to leave.
    RemoveWrongStaker,
    /// A reward transaction run before its staker's end time.
    RemoveStakerTooEarly,
    /// A reward transaction carrying credentials. It authorises nothing, so it
    /// signs nothing.
    WrongNumberOfCredentials,
    /// A staker in the current set that should not be reachable there.
    ShouldBePermissionlessStaker,
    /// A chain name already in use.
    ChainNameTaken,
    /// The block's time runs backwards.
    ChildBlockEarlierThanParent,
    /// The block's time is past the next staker change, which would skip it.
    ChildBlockAfterStakerChangeTime,
    /// The block's time is further ahead of local time than the bound allows.
    ChildBlockBeyondSyncBound,
    Overflow,
    /// Turning a network into a staked one. Go refuses it permanently — it has
    /// no role now that every staker enters immediately, and any historical
    /// one is already applied in genesis.
    TransformChainNotPermitted,
    /// A network asking for a validator set of its own.
    ///
    /// A set of one's own is held in the L1-validator plane — registrations,
    /// their fee balances, the expiry of a registration that was never paid
    /// for — and this port does not carry that plane. Refused by name rather
    /// than recorded as a network whose set silently has nobody in it. See
    /// LLM.md.
    OwnSetNotHeld,
    /// A transaction that acts on a validator registered on an L1.
    L1ValidatorPlaneNotHeld(Kind),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Syntactic(e) => write!(f, "{e}"),
            Error::Flow(e) => write!(f, "flow check failed: {e}"),
            Error::Credential(e) => write!(f, "the spend is not authorised: {e}"),
            Error::State(e) => write!(f, "{e}"),
            Error::WrongTxType(k) => write!(f, "wrong transaction type: {k:?}"),
            Error::EmptyNodeId => write!(f, "validator nodeID cannot be empty"),
            Error::AddValidatorNotPermitted => write!(f, "AddValidatorTx is not permitted"),
            Error::AddDelegatorNotPermitted => write!(f, "AddDelegatorTx is not permitted"),
            Error::DuplicateValidator => write!(f, "duplicate validator"),
            Error::NotValidator => write!(f, "isn't a current or pending validator"),
            Error::RemovePermissionlessValidator => {
                write!(f, "attempting to remove permissionless validator")
            }
            Error::WeightTooSmall => write!(f, "weight of this validator is too low"),
            Error::WeightTooLarge => write!(f, "weight of this validator is too large"),
            Error::InsufficientDelegationFee => {
                write!(f, "staker charges an insufficient delegation fee")
            }
            Error::StakeTooShort => write!(f, "staking period is too short"),
            Error::StakeTooLong => write!(f, "staking period is too long"),
            Error::WrongStakedAsset => write!(f, "incorrect staked assetID"),
            Error::OverDelegated => write!(f, "validator would be over delegated"),
            Error::PeriodMismatch => {
                write!(
                    f,
                    "proposed staking period is not inside dependent staking period"
                )
            }
            Error::DelegateToPermissionedValidator => {
                write!(f, "delegation to permissioned validator")
            }
            Error::RemoveWrongStaker => write!(f, "attempting to remove wrong staker"),
            Error::RemoveStakerTooEarly => {
                write!(f, "attempting to remove staker before their end time")
            }
            Error::WrongNumberOfCredentials => write!(f, "wrong number of credentials"),
            Error::ShouldBePermissionlessStaker => write!(f, "expected permissionless staker"),
            Error::ChainNameTaken => write!(f, "chain name is already taken"),
            Error::ChildBlockEarlierThanParent => {
                write!(f, "proposed timestamp before current chain time")
            }
            Error::ChildBlockAfterStakerChangeTime => {
                write!(f, "proposed timestamp later than next staker change time")
            }
            Error::ChildBlockBeyondSyncBound => write!(
                f,
                "proposed timestamp is too far in the future relative to local time"
            ),
            Error::Overflow => write!(f, "overflow"),
            Error::TransformChainNotPermitted => {
                write!(f, "TransformChainTx is not permitted")
            }
            Error::OwnSetNotHeld => {
                write!(f, "a network's own validator set is not held by this port")
            }
            Error::L1ValidatorPlaneNotHeld(k) => write!(
                f,
                "{k:?} acts on an L1 validator, which this port does not hold"
            ),
        }
    }
}

impl std::error::Error for Error {}

impl From<crate::txs::Error> for Error {
    fn from(e: crate::txs::Error) -> Self {
        Error::Syntactic(e)
    }
}

impl From<flow::Error> for Error {
    fn from(e: flow::Error) -> Self {
        Error::Flow(e)
    }
}

impl From<flow::CredentialError> for Error {
    fn from(e: flow::CredentialError) -> Self {
        Error::Credential(e)
    }
}

impl From<StateError> for Error {
    fn from(e: StateError) -> Self {
        Error::State(e)
    }
}

/// Run a submitted transaction against the state.
///
/// The state is modified in place, so the caller hands in the copy the block
/// is being verified on.
pub fn execute_standard(
    state: &mut State,
    tx: &Tx,
    config: &Config,
    fees: &dyn Fees,
) -> Result<(), Error> {
    tx.syntactic_verify(config.native_asset)?;

    match &tx.unsigned {
        // A proposal transaction in a standard block is not a transaction
        // anyone may submit.
        Unsigned::RewardValidator { .. } => Err(Error::WrongTxType(Kind::RewardValidator)),

        Unsigned::AddValidator { validator, .. } => {
            if validator.node_id.is_empty() {
                return Err(Error::EmptyNodeId);
            }
            Err(Error::AddValidatorNotPermitted)
        }
        Unsigned::AddDelegator { .. } => Err(Error::AddDelegatorNotPermitted),

        Unsigned::Base(base) => {
            charge(state, tx, &base.ins, &base.outs, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
        }

        Unsigned::Import { base, imported, .. } => {
            // The imported outputs are proved by the chain they came from;
            // this port does not carry that proof, so an import is refused
            // rather than trusted. See LLM.md.
            let _ = imported;
            let _ = base;
            Err(Error::WrongTxType(Kind::Import))
        }

        Unsigned::Export { base, exported, .. } => {
            let mut all: Vec<Output> = base.outs.clone();
            all.extend_from_slice(exported);
            charge(state, tx, &base.ins, &all, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            // The exported outputs are handed to the destination chain by the
            // caller; they are deliberately not made here, because a UTXO that
            // existed on both chains would be the same money twice.
            Ok(())
        }

        Unsigned::CreateChain { base, name, .. } => {
            if !name.is_empty() && state.is_chain_name_taken(name) {
                return Err(Error::ChainNameTaken);
            }
            charge(state, tx, &base.ins, &base.outs, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.add_blockchain(tx.id(), name);
            state.add_tx(tx.clone());
            Ok(())
        }

        Unsigned::AddChainValidator {
            base,
            validator,
            chain,
            ..
        } => {
            let now = state.timestamp();
            let duration = validator.end.saturating_sub(now);
            if duration < config.staking.min_stake_duration {
                return Err(Error::StakeTooShort);
            }
            if duration > config.staking.max_stake_duration {
                return Err(Error::StakeTooLong);
            }
            if config.bootstrapped {
                if state.validator(chain, &validator.node_id).is_ok() {
                    return Err(Error::DuplicateValidator);
                }
                // A network's validator must be one of the primary network's,
                // for a period inside the one it validates there. Otherwise a
                // network could be secured by nodes with nothing at stake.
                verify_primary_network_requirements(state, &validator.node_id, validator.end)?;
                charge(state, tx, &base.ins, &base.outs, config, fees)?;
            }
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            put_staker(state, tx, config)?;
            state.add_tx(tx.clone());
            Ok(())
        }

        Unsigned::AddPermissionlessValidator {
            base,
            validator,
            chain,
            signer,
            stake,
            delegation_shares,
            ..
        } => {
            // The proof of possession is checked syntactically, above, by
            // `syntactic_verify`; nothing here may weaken it.
            debug_assert!(signer.verify().is_ok());
            if !config.bootstrapped {
                state.consume_and_produce(tx.id(), &base.ins, &base.outs);
                put_staker(state, tx, config)?;
                state.add_tx(tx.clone());
                return Ok(());
            }

            let now = state.timestamp();
            let duration = validator.end.saturating_sub(now);
            let rules = &config.staking;

            if validator.weight < rules.min_validator_stake {
                return Err(Error::WeightTooSmall);
            }
            if validator.weight > rules.max_validator_stake {
                return Err(Error::WeightTooLarge);
            }
            if *delegation_shares < rules.min_delegation_fee {
                return Err(Error::InsufficientDelegationFee);
            }
            if duration < rules.min_stake_duration {
                return Err(Error::StakeTooShort);
            }
            if duration > rules.max_stake_duration {
                return Err(Error::StakeTooLong);
            }
            if stake.first().map(|o| o.asset) != Some(config.native_asset) {
                return Err(Error::WrongStakedAsset);
            }
            if state.validator(chain, &validator.node_id).is_ok() {
                return Err(Error::DuplicateValidator);
            }
            if *chain != PRIMARY_NETWORK_ID {
                verify_primary_network_requirements(state, &validator.node_id, validator.end)?;
            }

            let mut all: Vec<Output> = base.outs.clone();
            all.extend_from_slice(stake);
            charge(state, tx, &base.ins, &all, config, fees)?;

            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            put_staker(state, tx, config)?;
            state.add_tx(tx.clone());
            Ok(())
        }

        Unsigned::AddPermissionlessDelegator {
            base,
            validator,
            chain,
            stake,
            ..
        } => {
            if !config.bootstrapped {
                state.consume_and_produce(tx.id(), &base.ins, &base.outs);
                put_staker(state, tx, config)?;
                state.add_tx(tx.clone());
                return Ok(());
            }

            let now = state.timestamp();
            let duration = validator.end.saturating_sub(now);
            let rules = &config.staking;

            if validator.weight < rules.min_delegator_stake {
                return Err(Error::WeightTooSmall);
            }
            if duration < rules.min_stake_duration {
                return Err(Error::StakeTooShort);
            }
            if duration > rules.max_stake_duration {
                return Err(Error::StakeTooLong);
            }
            if stake.first().map(|o| o.asset) != Some(config.native_asset) {
                return Err(Error::WrongStakedAsset);
            }

            let delegatee = state
                .validator(chain, &validator.node_id)
                .map_err(|_| Error::NotValidator)?
                .clone();

            let limit = MAX_VALIDATOR_WEIGHT_FACTOR
                .saturating_mul(delegatee.weight)
                .min(rules.max_validator_stake);

            if !bounded_by(now, validator.end, delegatee.start_time, delegatee.end_time) {
                return Err(Error::PeriodMismatch);
            }
            if over_delegated(
                state,
                &delegatee,
                limit,
                validator.weight,
                now,
                validator.end,
            )? {
                return Err(Error::OverDelegated);
            }
            if *chain != PRIMARY_NETWORK_ID && delegatee.priority.is_permissioned_validator() {
                return Err(Error::DelegateToPermissionedValidator);
            }

            let mut all: Vec<Output> = base.outs.clone();
            all.extend_from_slice(stake);
            charge(state, tx, &base.ins, &all, config, fees)?;

            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            put_staker(state, tx, config)?;
            state.add_tx(tx.clone());
            Ok(())
        }

        Unsigned::RemoveChainValidator {
            base,
            node_id,
            chain,
            ..
        } => {
            let (staker, is_current) = match state.current_validator(chain, node_id) {
                Ok(v) => (v.clone(), true),
                Err(_) => match state.pending_validator(chain, node_id) {
                    Ok(v) => (v.clone(), false),
                    Err(_) => return Err(Error::NotValidator),
                },
            };
            // Only a validator admitted by name may be removed by name. A
            // permissionless one bought its place and leaves when it says so.
            if !staker.priority.is_permissioned_validator() {
                return Err(Error::RemovePermissionlessValidator);
            }
            if config.bootstrapped {
                charge(state, tx, &base.ins, &base.outs, config, fees)?;
            }
            if is_current {
                state.delete_current_validator(&staker);
            } else {
                state.delete_pending_validator(&staker);
            }
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
        }

        Unsigned::TransferChainOwnership {
            base, chain, owner, ..
        } => {
            charge(state, tx, &base.ins, &base.outs, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.set_chain_owner(*chain, owner.clone());
            Ok(())
        }

        Unsigned::IncreaseL1ValidatorBalance { .. } | Unsigned::DisableL1Validator { .. } => {
            // Both act on an L1 validator's fee balance, which this port does
            // not hold. Refused by name rather than applied to nothing.
            Err(Error::WrongTxType(tx.unsigned.kind()))
        }

        Unsigned::CreateNetwork {
            base,
            owner,
            security,
            ..
        } => {
            // A network that runs a set of its own has that set seeded here,
            // in the L1-validator plane. This port does not hold that plane,
            // so it refuses rather than recording a network whose set is
            // silently empty — an empty set is a network nothing secures.
            if security.sovereign() {
                return Err(Error::OwnSetNotHeld);
            }
            charge(state, tx, &base.ins, &base.outs, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            // The network's id IS this transaction's id, which is what lets
            // every later transaction that names the network name it without
            // anyone having chosen a name.
            state.add_chain(tx.id(), owner.clone());
            Ok(())
        }

        Unsigned::ConvertNetwork { .. } => {
            // Promotion establishes a set of the network's own, which is the
            // same plane CreateNetwork's sovereign path needs.
            Err(Error::OwnSetNotHeld)
        }

        Unsigned::TransformChain { .. } => Err(Error::TransformChainNotPermitted),

        Unsigned::RegisterL1Validator { .. } | Unsigned::SetL1ValidatorWeight { .. } => {
            Err(Error::L1ValidatorPlaneNotHeld(tx.unsigned.kind()))
        }
    }
}

/// Read the UTXOs an input names and check the arithmetic.
fn charge(
    state: &State,
    tx: &Tx,
    ins: &[crate::components::Input],
    outs: &[Output],
    config: &Config,
    fees: &dyn Fees,
) -> Result<(), Error> {
    let mut utxos = Vec::with_capacity(ins.len());
    for input in ins {
        utxos.push(state.utxo(&input.utxo.input_id())?.clone());
    }
    let mut fee_map = HashMap::new();
    fee_map.insert(config.native_asset, fees.fee(&tx.unsigned));
    flow::verify_spend(&utxos, ins, outs, &tx.creds, &fee_map, state.timestamp())?;
    // The arithmetic says the value adds up; this says whose value it was.
    // Both, always, and in one place — an execution path that ran one without
    // the other would let anyone spend anyone's output.
    flow::verify_credentials(&utxos, ins, &tx.creds, &tx.sighash(), state.timestamp())?;
    Ok(())
}

/// Every network validator must also be validating the primary network, for a
/// period that contains the one it is asking for.
///
/// This is what keeps a network's security anchored: without it a network
/// could be validated by nodes with nothing staked on the primary network at
/// all, and its validator set would be free to assemble.
fn verify_primary_network_requirements(
    state: &State,
    node: &NodeId,
    end: u64,
) -> Result<(), Error> {
    let primary = state
        .validator(&PRIMARY_NETWORK_ID, node)
        .map_err(|_| Error::NotValidator)?;
    if !bounded_by(state.timestamp(), end, primary.start_time, primary.end_time) {
        return Err(Error::PeriodMismatch);
    }
    Ok(())
}

/// Place the staker a transaction creates into the current set.
///
/// Stakers enter immediately: their start time is the chain's current time.
/// The reward is computed here, at entry, and added to the supply before it is
/// paid — which is why the calculator may never return more than what is left
/// to mint.
fn put_staker(state: &mut State, tx: &Tx, config: &Config) -> Result<(), Error> {
    let view = match tx.unsigned.staker() {
        Some(v) => v,
        None => return Ok(()),
    };
    let chain_time = state.timestamp();

    let mut potential_reward = 0;
    if !view.priority_current.is_permissioned_validator() {
        let supply = state.current_supply(&view.chain)?;
        let calculator = reward::Calculator::new(config.reward);
        potential_reward = calculator.calculate(
            std::time::Duration::from_secs(view.validator.end.saturating_sub(chain_time)),
            view.validator.weight,
            supply,
        );
        state.set_current_supply(
            view.chain,
            supply
                .checked_add(potential_reward)
                .ok_or(Error::Overflow)?,
        );
    }

    let staker = Staker {
        tx_id: tx.id(),
        node_id: view.validator.node_id,
        public_key: view.signer,
        chain: view.chain,
        weight: view.validator.weight,
        start_time: chain_time,
        end_time: view.validator.end,
        potential_reward,
        next_time: view.validator.end,
        priority: view.priority_current,
    };

    if staker.priority.is_current_validator() {
        state.put_current_validator(staker)?;
    } else if staker.priority.is_current_delegator() {
        state.put_current_delegator(staker);
    } else if staker.priority.is_pending_validator() {
        state.put_pending_validator(staker)?;
    } else {
        state.put_pending_delegator(staker);
    }
    Ok(())
}

/// The most weight this validator will carry between two times, plus what is
/// being added.
///
/// The maximum over the window is what matters, not the weight right now: a
/// delegation that fits today but not next week would push the validator over
/// its limit for the rest of its term.
fn over_delegated(
    state: &State,
    validator: &Staker,
    limit: u64,
    added: u64,
    start: u64,
    end: u64,
) -> Result<bool, Error> {
    let max = max_weight(state, validator, start, end)?;
    Ok(max.checked_add(added).ok_or(Error::Overflow)? > limit)
}

/// The largest total weight on `validator` at any moment in `[start, end]`.
///
/// Go walks the delegation changes in the order the clock will apply them,
/// adding a pending delegator's weight when it starts and removing a current
/// one's when it ends, and takes the running maximum inside the window.
fn max_weight(state: &State, validator: &Staker, start: u64, end: u64) -> Result<u64, Error> {
    let current: Vec<Staker> = state
        .current_delegators(&validator.chain, &validator.node_id)
        .cloned()
        .collect();
    let pending: Vec<Staker> = state
        .pending_delegators(&validator.chain, &validator.node_id)
        .cloned()
        .collect();

    // What is on the validator right now: its own weight and every delegation
    // already backing it.
    let mut current_weight = validator.weight;
    for d in &current {
        current_weight = current_weight
            .checked_add(d.weight)
            .ok_or(Error::Overflow)?;
    }

    // Then walk the changes in the order time will apply them, taking the
    // weight before each one — the weight before an arrival is the weight the
    // arrival is being added to, and the weight before a departure is the
    // weight that was actually carried.
    let mut changes = crate::state::StakerDiff::new(current, pending);
    let mut current_max = 0u64;
    while let Some((delegator, arriving)) = changes.next() {
        if delegator.next_time > end {
            break;
        }
        if delegator.next_time >= start {
            current_max = current_max.max(current_weight);
        }
        current_weight = if arriving {
            current_weight
                .checked_add(delegator.weight)
                .ok_or(Error::Overflow)?
        } else {
            current_weight.saturating_sub(delegator.weight)
        };
    }
    Ok(current_max.max(current_weight))
}

/// Run the chain's own transaction, onto both of its futures.
///
/// `on_commit` is the state if the reward is paid; `on_abort` is the state if
/// it is not. They must be equal on the way in — the whole point is that they
/// differ only by what this decides.
pub fn execute_proposal(tx: &Tx, on_commit: &mut State, on_abort: &mut State) -> Result<(), Error> {
    let staker_tx_id = match &tx.unsigned {
        Unsigned::RewardValidator { staker_tx_id } => *staker_tx_id,
        other => return Err(Error::WrongTxType(other.kind())),
    };
    if !tx.creds.is_empty() {
        return Err(Error::WrongNumberOfCredentials);
    }

    let staker = on_commit
        .next_current_staker()
        .cloned()
        .ok_or(Error::State(StateError::NotFound))?;

    // It must be the staker that is actually next. Otherwise a block could
    // reward whichever staker it liked.
    if staker.tx_id != staker_tx_id {
        return Err(Error::RemoveWrongStaker);
    }
    if staker.end_time != on_commit.timestamp() {
        return Err(Error::RemoveStakerTooEarly);
    }

    let staker_tx = on_commit.tx(&staker.tx_id)?.clone();

    if staker_tx.unsigned.validation_rewards_owner().is_some() {
        reward_validator(&staker_tx, &staker, on_commit, on_abort)?;
        on_commit.delete_current_validator(&staker);
        on_abort.delete_current_validator(&staker);
    } else if staker_tx.unsigned.delegator_rewards_owner().is_some() {
        reward_delegator(&staker_tx, &staker, on_commit, on_abort)?;
        on_commit.delete_current_delegator(&staker);
        on_abort.delete_current_delegator(&staker);
    } else {
        // Permissioned stakers leave when the clock passes them, so one still
        // here at its end time means the sets are not what they should be.
        return Err(Error::ShouldBePermissionlessStaker);
    }

    // If the reward is refused, the supply never grew.
    let supply = on_abort.current_supply(&staker.chain)?;
    on_abort.set_current_supply(
        staker.chain,
        supply
            .checked_sub(staker.potential_reward)
            .ok_or(Error::Overflow)?,
    );
    Ok(())
}

fn reward_validator(
    staker_tx: &Tx,
    validator: &Staker,
    on_commit: &mut State,
    on_abort: &mut State,
) -> Result<(), Error> {
    let tx_id = validator.tx_id;
    let stake = staker_tx.unsigned.stake();
    let outputs = staker_tx.unsigned.outputs();
    let asset = stake.first().map(|o| o.asset).ok_or(Error::Overflow)?;

    // The stake comes back either way. A validator that finished its term is
    // paid or not paid; it is never confiscated.
    for (i, out) in stake.iter().enumerate() {
        let utxo = Utxo {
            id: crate::components::UtxoId {
                tx_id,
                output_index: (outputs.len() + i) as u32,
            },
            output: out.clone(),
        };
        on_commit.add_utxo(utxo.clone());
        on_abort.add_utxo(utxo);
    }

    let mut offset = 0;
    if validator.potential_reward > 0 {
        let owner = staker_tx
            .unsigned
            .validation_rewards_owner()
            .ok_or(Error::ShouldBePermissionlessStaker)?
            .clone();
        let utxo = Utxo {
            id: crate::components::UtxoId {
                tx_id,
                output_index: (outputs.len() + stake.len()) as u32,
            },
            output: Output {
                asset,
                stake_lock: 0,
                amount: validator.potential_reward,
                owners: owner,
            },
        };
        on_commit.add_utxo(utxo.clone());
        on_commit.add_reward_utxo(tx_id, utxo);
        offset = 1;
    }

    // The fees its delegators paid it, accrued over the term and paid now.
    let delegatee_reward = on_commit.delegatee_reward(&validator.chain, &validator.node_id);
    if delegatee_reward == 0 {
        return Ok(());
    }
    let owner = staker_tx
        .unsigned
        .delegation_rewards_owner()
        .ok_or(Error::ShouldBePermissionlessStaker)?
        .clone();

    let commit_utxo = Utxo {
        id: crate::components::UtxoId {
            tx_id,
            output_index: (outputs.len() + stake.len() + offset) as u32,
        },
        output: Output {
            asset,
            stake_lock: 0,
            amount: delegatee_reward,
            owners: owner.clone(),
        },
    };
    on_commit.add_utxo(commit_utxo.clone());
    on_commit.add_reward_utxo(tx_id, commit_utxo);

    // On the abort side there is no validation reward, so the delegatee reward
    // takes the index the validation reward would have had.
    let abort_utxo = Utxo {
        id: crate::components::UtxoId {
            tx_id,
            output_index: (outputs.len() + stake.len()) as u32,
        },
        output: Output {
            asset,
            stake_lock: 0,
            amount: delegatee_reward,
            owners: owner,
        },
    };
    on_abort.add_utxo(abort_utxo.clone());
    on_abort.add_reward_utxo(tx_id, abort_utxo);
    Ok(())
}

fn reward_delegator(
    staker_tx: &Tx,
    delegator: &Staker,
    on_commit: &mut State,
    on_abort: &mut State,
) -> Result<(), Error> {
    let tx_id = delegator.tx_id;
    let stake = staker_tx.unsigned.stake();
    let outputs = staker_tx.unsigned.outputs();
    let asset = stake.first().map(|o| o.asset).ok_or(Error::Overflow)?;

    for (i, out) in stake.iter().enumerate() {
        let utxo = Utxo {
            id: crate::components::UtxoId {
                tx_id,
                output_index: (outputs.len() + i) as u32,
            },
            output: out.clone(),
        };
        on_commit.add_utxo(utxo.clone());
        on_abort.add_utxo(utxo);
    }

    let validator = on_commit
        .current_validator(&delegator.chain, &delegator.node_id)?
        .clone();
    let validator_tx = on_commit.tx(&validator.tx_id)?.clone();
    let shares = validator_tx
        .unsigned
        .shares()
        .ok_or(Error::WrongTxType(validator_tx.unsigned.kind()))?;

    // The validator's cut is deferred until its own term ends; the delegator's
    // half is paid now.
    let (delegatee_reward, delegator_reward) = reward::split(delegator.potential_reward, shares);

    if delegator_reward > 0 {
        let owner = staker_tx
            .unsigned
            .delegator_rewards_owner()
            .ok_or(Error::ShouldBePermissionlessStaker)?
            .clone();
        let utxo = Utxo {
            id: crate::components::UtxoId {
                tx_id,
                output_index: (outputs.len() + stake.len()) as u32,
            },
            output: Output {
                asset,
                stake_lock: 0,
                amount: delegator_reward,
                owners: owner,
            },
        };
        on_commit.add_utxo(utxo.clone());
        on_commit.add_reward_utxo(tx_id, utxo);
    }

    if delegatee_reward == 0 {
        return Ok(());
    }
    let previous = on_commit.delegatee_reward(&validator.chain, &validator.node_id);
    on_commit.set_delegatee_reward(
        validator.chain,
        validator.node_id,
        previous
            .checked_add(delegatee_reward)
            .ok_or(Error::Overflow)?,
    );
    Ok(())
}

/// Whether a block may claim this timestamp.
pub fn verify_new_chain_time(state: &State, new_time: u64, now: u64) -> Result<(), Error> {
    if new_time < state.timestamp() {
        return Err(Error::ChildBlockEarlierThanParent);
    }
    if new_time > now + SYNC_BOUND {
        return Err(Error::ChildBlockBeyondSyncBound);
    }
    if new_time > state.next_staker_change_time(new_time.max(u64::MAX / 2)) {
        return Err(Error::ChildBlockAfterStakerChangeTime);
    }
    Ok(())
}

/// Move the clock, and everything the clock moves.
///
/// Pending stakers whose start time has arrived become current, and their
/// reward is computed and added to the supply as they enter. Permissioned
/// validators whose term has ended are dropped. A permissionless staker whose
/// term has ended is *not* dropped here — it leaves by a reward transaction,
/// which pays it, and advancing the clock alone must never drop it unpaid.
///
/// Answers whether the validator set changed.
pub fn advance_time_to(state: &mut State, new_time: u64, config: &Config) -> Result<bool, Error> {
    let mut changed = false;

    let promoting: Vec<Staker> = state
        .pending_stakers()
        .take_while(|s| s.start_time <= new_time)
        .cloned()
        .collect();

    for old in promoting {
        let mut promoted = old.clone();
        promoted.next_time = old.end_time;
        promoted.priority = old
            .priority
            .to_current()
            .ok_or(Error::ShouldBePermissionlessStaker)?;

        if old.priority == Priority::ChainPermissionedValidatorPending {
            state.put_current_validator(promoted)?;
            state.delete_pending_validator(&old);
            changed = true;
            continue;
        }

        let supply = state.current_supply(&old.chain)?;
        let calculator = reward::Calculator::new(config.reward);
        promoted.potential_reward = calculator.calculate(
            std::time::Duration::from_secs(old.end_time.saturating_sub(old.start_time)),
            old.weight,
            supply,
        );
        state.set_current_supply(
            old.chain,
            supply
                .checked_add(promoted.potential_reward)
                .ok_or(Error::Overflow)?,
        );

        if promoted.priority.is_current_validator() {
            state.put_current_validator(promoted)?;
            state.delete_pending_validator(&old);
        } else {
            state.put_current_delegator(promoted);
            state.delete_pending_delegator(&old);
        }
        changed = true;
    }

    let leaving: Vec<Staker> = state
        .current_stakers()
        .take_while(|s| {
            s.end_time <= new_time && s.priority == Priority::ChainPermissionedValidatorCurrent
        })
        .cloned()
        .collect();
    for staker in leaving {
        state.delete_current_validator(&staker);
        changed = true;
    }

    state.set_timestamp(new_time);
    Ok(changed)
}

/// The set consensus samples for one network.
pub fn validator_set(state: &State, chain: &Id) -> Vec<(NodeId, u64, Option<[u8; 48]>)> {
    state.validator_set(chain)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Credential, Input, Owners, Utxo, UtxoId};
    use crate::ids::ShortId;
    use crate::signer::Signer;
    use crate::txs::{Envelope, Validator};

    const ASSET: Id = [9u8; 32];
    const DAY: u64 = 24 * 60 * 60;
    const YEAR: u64 = 365 * DAY;
    const MEGA: u64 = 1_000_000_000_000;

    fn config() -> Config {
        Config {
            native_asset: ASSET,
            staking: StakingPolicy {
                min_validator_stake: 2 * MEGA,
                max_validator_stake: 3_000 * MEGA,
                min_delegator_stake: 25 * MEGA / 1000,
                min_stake_duration: 2 * 7 * DAY,
                max_stake_duration: YEAR,
                min_delegation_fee: 20_000,
            },
            reward: reward::Config {
                max_consumption_rate: 120_000,
                min_consumption_rate: 100_000,
                minting_period: std::time::Duration::from_secs(YEAR),
                supply_cap: 720 * 1_000_000 * 1_000_000,
            },
            bootstrapped: true,
        }
    }

    fn fees() -> FlatFees {
        FlatFees::default()
    }

    fn owner(n: u8) -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![ShortId([n; 20])],
        }
    }

    fn output(amount: u64, who: u8) -> Output {
        Output {
            asset: ASSET,
            stake_lock: 0,
            amount,
            owners: owner(who),
        }
    }

    /// The key everything in these tests is funded to, and signs with.
    ///
    /// The signatures here are real. A test that spent with a made-up
    /// credential would prove the bookkeeping around signatures and not the
    /// thing that matters — that an output moves only for the person who owns
    /// it.
    fn spender() -> k256::ecdsa::SigningKey {
        k256::ecdsa::SigningKey::from_bytes(&[0x11u8; 32].into()).expect("a key")
    }

    fn spend_owner() -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![crate::sign::address(
                spender().verifying_key().to_encoded_point(true).as_bytes(),
            )],
        }
    }

    /// An output the spender can move.
    fn funds(amount: u64) -> Output {
        Output {
            asset: ASSET,
            stake_lock: 0,
            amount,
            owners: spend_owner(),
        }
    }

    /// A funded state at time `now` with one spendable output of `amount`.
    fn funded(now: u64, amount: u64) -> (State, Input) {
        let mut state = State::new();
        state.set_timestamp(now);
        state.set_current_supply(PRIMARY_NETWORK_ID, 400 * 1_000_000 * 1_000_000);
        let id = UtxoId {
            tx_id: [1; 32],
            output_index: 0,
        };
        state.add_utxo(Utxo {
            id,
            output: funds(amount),
        });
        (
            state,
            Input {
                utxo: id,
                asset: ASSET,
                stake_lock: 0,
                amount,
                sig_indices: vec![0],
            },
        )
    }

    fn envelope(ins: Vec<Input>, outs: Vec<Output>) -> Envelope {
        Envelope {
            network_id: 1,
            blockchain_id: [3; 32],
            outs,
            ins,
            memo: Vec::new(),
        }
    }

    /// A transaction with one real signature per input, made by the spender.
    fn signed(unsigned: Unsigned, n_creds: usize) -> Tx {
        let sighash = crate::ids::hash256(&unsigned.to_bytes());
        let cred = Credential {
            sigs: vec![crate::sign::sign(&spender(), &sighash)],
        };
        Tx::new(unsigned, vec![cred; n_creds])
    }

    /// A validator's BLS key. Derived from the node byte so one node keeps one
    /// key across a test.
    fn bls(node: u8) -> blst::min_pk::SecretKey {
        let mut ikm = [0u8; 32];
        ikm[0] = node;
        ikm[1] = 0xa5;
        blst::min_pk::SecretKey::key_gen(&ikm, &[]).unwrap()
    }

    fn add_validator(node: u8, weight: u64, end: u64, input: Input, change: u64) -> Unsigned {
        Unsigned::AddPermissionlessValidator {
            base: envelope(
                vec![input],
                if change > 0 {
                    vec![output(change, 1)]
                } else {
                    vec![]
                },
            ),
            validator: Validator {
                node_id: NodeId([node; 20]),
                start: 0,
                end,
                weight,
            },
            chain: PRIMARY_NETWORK_ID,
            signer: Signer::prove(&bls(node)),
            stake: vec![Output {
                asset: ASSET,
                stake_lock: 0,
                amount: weight,
                owners: owner(2),
            }],
            validator_rewards_owner: owner(3),
            delegator_rewards_owner: owner(4),
            delegation_shares: 20_000,
        }
    }

    #[test]
    fn anyone_with_enough_stake_becomes_a_validator() {
        // The sentence this chain exists for: no owner is consulted, no
        // allowlist is read, and the transaction is accepted.
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let tx = signed(add_validator(5, 10 * MEGA, now + YEAR, input, 0), 1);

        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Ok(())
        );

        let v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .unwrap();
        assert_eq!(v.weight, 10 * MEGA);
        assert_eq!(v.start_time, now, "a staker enters at the current time");
        assert_eq!(v.end_time, now + YEAR);
        assert!(v.potential_reward > 0, "and is owed a reward for the term");
    }

    #[test]
    fn a_validators_reward_is_added_to_the_supply_when_it_enters() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let before = state.current_supply(&PRIMARY_NETWORK_ID).unwrap();
        let tx = signed(add_validator(5, 10 * MEGA, now + YEAR, input, 0), 1);
        execute_standard(&mut state, &tx, &config(), &fees()).unwrap();

        let v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .unwrap();
        assert_eq!(
            state.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            before + v.potential_reward
        );
    }

    #[test]
    fn a_node_may_not_validate_the_same_network_twice() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let tx = signed(add_validator(5, 10 * MEGA, now + YEAR, input, 0), 1);
        execute_standard(&mut state, &tx, &config(), &fees()).unwrap();

        // A second transaction from a second output, same node.
        let id = UtxoId {
            tx_id: [2; 32],
            output_index: 0,
        };
        state.add_utxo(Utxo {
            id,
            output: output(10 * MEGA, 1),
        });
        let second = signed(
            add_validator(
                5,
                10 * MEGA,
                now + YEAR,
                Input {
                    utxo: id,
                    asset: ASSET,
                    stake_lock: 0,
                    amount: 10 * MEGA,
                    sig_indices: vec![0],
                },
                0,
            ),
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &second, &config(), &fees()),
            Err(Error::DuplicateValidator)
        );
    }

    #[test]
    fn a_stake_below_the_floor_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, MEGA);
        let tx = signed(add_validator(5, MEGA, now + YEAR, input, 0), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::WeightTooSmall)
        );
    }

    #[test]
    fn a_stake_above_the_ceiling_is_refused() {
        let now = 1000;
        let big = 4_000 * MEGA;
        let (mut state, input) = funded(now, big);
        let tx = signed(add_validator(5, big, now + YEAR, input, 0), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::WeightTooLarge)
        );
    }

    #[test]
    fn a_term_outside_the_bounds_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let short = signed(add_validator(5, 10 * MEGA, now + DAY, input.clone(), 0), 1);
        assert_eq!(
            execute_standard(&mut state, &short, &config(), &fees()),
            Err(Error::StakeTooShort)
        );

        let long = signed(add_validator(5, 10 * MEGA, now + 2 * YEAR, input, 0), 1);
        assert_eq!(
            execute_standard(&mut state, &long, &config(), &fees()),
            Err(Error::StakeTooLong)
        );
    }

    #[test]
    fn a_delegation_fee_below_the_floor_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let mut u = add_validator(5, 10 * MEGA, now + YEAR, input, 0);
        if let Unsigned::AddPermissionlessValidator {
            delegation_shares, ..
        } = &mut u
        {
            *delegation_shares = 19_999;
        }
        assert_eq!(
            execute_standard(&mut state, &signed(u, 1), &config(), &fees()),
            Err(Error::InsufficientDelegationFee)
        );
    }

    #[test]
    fn a_validator_may_not_stake_more_than_it_spends() {
        // The flow check is what stops weight from being conjured.
        let now = 1000;
        let (mut state, mut input) = funded(now, 10 * MEGA);
        input.amount = 10 * MEGA;
        // Declare and stake 10 MEGA but also keep 10 MEGA as change.
        let tx = signed(add_validator(5, 10 * MEGA, now + YEAR, input, 10 * MEGA), 1);
        assert!(matches!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::Flow(flow::Error::InsufficientUnlockedFunds { .. }))
        ));
    }

    #[test]
    fn the_legacy_scheduled_staker_transactions_are_refused() {
        // Go rejects both permanently; a port that quietly executed them would
        // admit stakers by a path nobody reviews.
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let v = Unsigned::AddValidator {
            base: envelope(vec![input.clone()], vec![]),
            validator: Validator {
                node_id: NodeId([5; 20]),
                start: now,
                end: now + YEAR,
                weight: 10 * MEGA,
            },
            stake: vec![output(10 * MEGA, 2)],
            rewards_owner: owner(3),
            delegation_shares: 20_000,
        };
        assert_eq!(
            execute_standard(&mut state, &signed(v, 1), &config(), &fees()),
            Err(Error::AddValidatorNotPermitted)
        );

        let d = Unsigned::AddDelegator {
            base: envelope(vec![input], vec![]),
            validator: Validator {
                node_id: NodeId([5; 20]),
                start: now,
                end: now + YEAR,
                weight: 10 * MEGA,
            },
            stake: vec![output(10 * MEGA, 2)],
            rewards_owner: owner(3),
        };
        assert_eq!(
            execute_standard(&mut state, &signed(d, 1), &config(), &fees()),
            Err(Error::AddDelegatorNotPermitted)
        );
    }

    #[test]
    fn a_validator_with_no_node_is_refused_before_anything_else() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let v = Unsigned::AddValidator {
            base: envelope(vec![input], vec![]),
            validator: Validator {
                node_id: NodeId::EMPTY,
                start: now,
                end: now + YEAR,
                weight: 10 * MEGA,
            },
            stake: vec![output(10 * MEGA, 2)],
            rewards_owner: owner(3),
            delegation_shares: 20_000,
        };
        assert_eq!(
            execute_standard(&mut state, &signed(v, 1), &config(), &fees()),
            Err(Error::EmptyNodeId)
        );
    }

    #[test]
    fn a_reward_transaction_may_not_be_submitted_as_a_standard_one() {
        let mut state = State::new();
        let tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: [1; 32],
            },
            0,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::WrongTxType(Kind::RewardValidator))
        );
    }

    // ---- delegation ----

    fn with_validator(now: u64, weight: u64, end: u64) -> (State, Tx) {
        let (mut state, input) = funded(now, weight);
        let tx = signed(add_validator(5, weight, end, input, 0), 1);
        execute_standard(&mut state, &tx, &config(), &fees()).unwrap();
        (state, tx)
    }

    fn delegate(node: u8, weight: u64, end: u64, input: Input) -> Unsigned {
        Unsigned::AddPermissionlessDelegator {
            base: envelope(vec![input], vec![]),
            validator: Validator {
                node_id: NodeId([node; 20]),
                start: 0,
                end,
                weight,
            },
            chain: PRIMARY_NETWORK_ID,
            stake: vec![Output {
                asset: ASSET,
                stake_lock: 0,
                amount: weight,
                owners: owner(2),
            }],
            rewards_owner: owner(6),
        }
    }

    fn fund_more(state: &mut State, tx: u8, amount: u64) -> Input {
        let id = UtxoId {
            tx_id: [tx; 32],
            output_index: 0,
        };
        state.add_utxo(Utxo {
            id,
            output: funds(amount),
        });
        Input {
            utxo: id,
            asset: ASSET,
            stake_lock: 0,
            amount,
            sig_indices: vec![0],
        }
    }

    #[test]
    fn a_delegation_inside_the_validators_term_is_accepted() {
        let now = 1000;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + YEAR);
        let input = fund_more(&mut state, 2, MEGA);
        let tx = signed(delegate(5, MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Ok(())
        );
        assert_eq!(
            state
                .current_delegators(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
                .count(),
            1
        );
    }

    #[test]
    fn a_delegation_outliving_its_validator_is_refused() {
        // Go: ErrPeriodMismatch. A delegation that outlasts the validator would
        // be stake behind nobody.
        let now = 1000;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + 30 * DAY);
        let input = fund_more(&mut state, 2, MEGA);
        let tx = signed(delegate(5, MEGA, now + 60 * DAY, input), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::PeriodMismatch)
        );
    }

    #[test]
    fn a_delegation_to_nobody_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, MEGA);
        let tx = signed(delegate(7, MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::NotValidator)
        );
    }

    #[test]
    fn a_validator_may_not_be_delegated_past_its_weight_limit() {
        // MaxValidatorWeightFactor: the validator's own stake times five,
        // including its own weight. 10 own + 40 delegated is the ceiling.
        let now = 1000;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + YEAR);

        let a = fund_more(&mut state, 2, 40 * MEGA);
        let ok = signed(delegate(5, 40 * MEGA, now + YEAR, a), 1);
        assert_eq!(
            execute_standard(&mut state, &ok, &config(), &fees()),
            Ok(())
        );

        let b = fund_more(&mut state, 3, MEGA);
        let over = signed(delegate(5, MEGA, now + YEAR, b), 1);
        assert_eq!(
            execute_standard(&mut state, &over, &config(), &fees()),
            Err(Error::OverDelegated)
        );
    }

    #[test]
    fn a_delegation_below_the_floor_is_refused() {
        let now = 1000;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + YEAR);
        let input = fund_more(&mut state, 2, 1);
        let tx = signed(delegate(5, 1, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::WeightTooSmall)
        );
    }

    // ---- the clock ----

    #[test]
    fn time_may_not_run_backwards() {
        let mut state = State::new();
        state.set_timestamp(500);
        assert_eq!(
            verify_new_chain_time(&state, 499, 1000),
            Err(Error::ChildBlockEarlierThanParent)
        );
        assert_eq!(verify_new_chain_time(&state, 500, 1000), Ok(()));
    }

    #[test]
    fn time_may_not_run_far_ahead_of_the_wall_clock() {
        let mut state = State::new();
        state.set_timestamp(500);
        assert_eq!(verify_new_chain_time(&state, 1010, 1000), Ok(()));
        assert_eq!(
            verify_new_chain_time(&state, 1011, 1000),
            Err(Error::ChildBlockBeyondSyncBound)
        );
    }

    #[test]
    fn time_may_not_skip_over_a_staker_change() {
        let mut state = State::new();
        state.set_timestamp(500);
        state
            .put_current_validator(Staker {
                tx_id: [1; 32],
                node_id: NodeId([1; 20]),
                public_key: None,
                chain: PRIMARY_NETWORK_ID,
                weight: 1,
                start_time: 0,
                end_time: 600,
                potential_reward: 0,
                next_time: 600,
                priority: Priority::ChainPermissionedValidatorCurrent,
            })
            .unwrap();
        assert_eq!(verify_new_chain_time(&state, 600, 10_000), Ok(()));
        assert_eq!(
            verify_new_chain_time(&state, 601, 10_000),
            Err(Error::ChildBlockAfterStakerChangeTime)
        );
    }

    #[test]
    fn a_permissioned_validator_leaves_when_the_clock_passes_it() {
        let mut state = State::new();
        state.set_timestamp(500);
        state.set_current_supply(PRIMARY_NETWORK_ID, 100);
        let staker = Staker {
            tx_id: [1; 32],
            node_id: NodeId([1; 20]),
            public_key: None,
            chain: [7; 32],
            weight: 1,
            start_time: 0,
            end_time: 600,
            potential_reward: 0,
            next_time: 600,
            priority: Priority::ChainPermissionedValidatorCurrent,
        };
        state.put_current_validator(staker).unwrap();

        assert_eq!(advance_time_to(&mut state, 600, &config()), Ok(true));
        assert_eq!(state.timestamp(), 600);
        assert!(state.current_validator(&[7; 32], &NodeId([1; 20])).is_err());
    }

    #[test]
    fn a_permissionless_validator_is_not_dropped_by_the_clock() {
        // It leaves by a reward transaction, which pays it. Dropping it here
        // would confiscate a term that was served.
        let mut state = State::new();
        state.set_timestamp(500);
        state.set_current_supply(PRIMARY_NETWORK_ID, 100);
        state
            .put_current_validator(Staker {
                tx_id: [1; 32],
                node_id: NodeId([1; 20]),
                public_key: None,
                chain: PRIMARY_NETWORK_ID,
                weight: 1,
                start_time: 0,
                end_time: 600,
                potential_reward: 7,
                next_time: 600,
                priority: Priority::PrimaryNetworkValidatorCurrent,
            })
            .unwrap();

        assert_eq!(advance_time_to(&mut state, 600, &config()), Ok(false));
        assert!(state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([1; 20]))
            .is_ok());
    }

    #[test]
    fn a_pending_staker_becomes_current_when_its_time_arrives() {
        let mut state = State::new();
        state.set_timestamp(500);
        state.set_current_supply(PRIMARY_NETWORK_ID, 400 * 1_000_000 * 1_000_000);
        state
            .put_pending_validator(Staker {
                tx_id: [1; 32],
                node_id: NodeId([1; 20]),
                public_key: None,
                chain: PRIMARY_NETWORK_ID,
                weight: 10 * MEGA,
                start_time: 600,
                end_time: 600 + YEAR,
                potential_reward: 0,
                next_time: 600,
                priority: Priority::PrimaryNetworkValidatorPending,
            })
            .unwrap();

        let before = state.current_supply(&PRIMARY_NETWORK_ID).unwrap();
        assert_eq!(advance_time_to(&mut state, 600, &config()), Ok(true));

        let v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([1; 20]))
            .unwrap();
        assert_eq!(v.priority, Priority::PrimaryNetworkValidatorCurrent);
        assert_eq!(v.next_time, v.end_time);
        assert!(v.potential_reward > 0);
        assert_eq!(
            state.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            before + v.potential_reward
        );
    }

    #[test]
    fn a_pending_staker_whose_time_has_not_come_stays_pending() {
        let mut state = State::new();
        state.set_timestamp(500);
        state.set_current_supply(PRIMARY_NETWORK_ID, 100);
        state
            .put_pending_validator(Staker {
                tx_id: [1; 32],
                node_id: NodeId([1; 20]),
                public_key: None,
                chain: PRIMARY_NETWORK_ID,
                weight: 1,
                start_time: 700,
                end_time: 900,
                potential_reward: 0,
                next_time: 700,
                priority: Priority::PrimaryNetworkValidatorPending,
            })
            .unwrap();
        assert_eq!(advance_time_to(&mut state, 600, &config()), Ok(false));
        assert!(state
            .pending_validator(&PRIMARY_NETWORK_ID, &NodeId([1; 20]))
            .is_ok());
    }

    // ---- the two futures of a reward ----

    /// A validator that has served its term, with the chain clock at its end.
    fn served(now: u64, potential_reward: u64) -> (State, Tx) {
        let start = 1000;
        let (mut state, input) = funded(start, 10 * MEGA);
        let tx = signed(add_validator(5, 10 * MEGA, now, input, 0), 1);
        execute_standard(&mut state, &tx, &config(), &fees()).unwrap();

        // Pin the reward so the test asserts a number rather than a formula.
        let mut v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .unwrap()
            .clone();
        state.delete_current_validator(&v);
        let supply = state.current_supply(&PRIMARY_NETWORK_ID).unwrap();
        state.set_current_supply(
            PRIMARY_NETWORK_ID,
            supply - v.potential_reward + potential_reward,
        );
        v.potential_reward = potential_reward;
        state.put_current_validator(v).unwrap();
        state.set_timestamp(now);
        (state, tx)
    }

    #[test]
    fn a_rewarded_validator_gets_its_stake_back_and_its_reward() {
        let end = 1000 + YEAR;
        let (state, staker_tx) = served(end, 500);
        let mut on_commit = state.clone();
        let mut on_abort = state.clone();

        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: staker_tx.id(),
            },
            0,
        );
        assert_eq!(
            execute_proposal(&reward_tx, &mut on_commit, &mut on_abort),
            Ok(())
        );

        // Gone from the set on both futures.
        assert!(on_commit
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .is_err());
        assert!(on_abort
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .is_err());

        // The stake comes back either way.
        let stake_utxo = UtxoId {
            tx_id: staker_tx.id(),
            output_index: 0,
        };
        assert_eq!(
            on_commit
                .utxo(&stake_utxo.input_id())
                .unwrap()
                .output
                .amount,
            10 * MEGA
        );
        assert_eq!(
            on_abort.utxo(&stake_utxo.input_id()).unwrap().output.amount,
            10 * MEGA
        );

        // The reward exists only on the committed future.
        assert_eq!(on_commit.reward_utxos(&staker_tx.id()).len(), 1);
        assert_eq!(
            on_commit.reward_utxos(&staker_tx.id())[0].output.amount,
            500
        );
        assert_eq!(on_abort.reward_utxos(&staker_tx.id()).len(), 0);
    }

    #[test]
    fn an_aborted_reward_gives_the_supply_back() {
        let end = 1000 + YEAR;
        let (state, staker_tx) = served(end, 500);
        let supply_before = state.current_supply(&PRIMARY_NETWORK_ID).unwrap();
        let mut on_commit = state.clone();
        let mut on_abort = state.clone();

        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: staker_tx.id(),
            },
            0,
        );
        execute_proposal(&reward_tx, &mut on_commit, &mut on_abort).unwrap();

        assert_eq!(
            on_commit.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            supply_before,
            "a paid reward was already counted in the supply"
        );
        assert_eq!(
            on_abort.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            supply_before - 500,
            "an unpaid one is given back"
        );
    }

    #[test]
    fn a_reward_transaction_naming_the_wrong_staker_is_refused() {
        let end = 1000 + YEAR;
        let (state, _) = served(end, 500);
        let mut on_commit = state.clone();
        let mut on_abort = state.clone();
        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: [99; 32],
            },
            0,
        );
        assert_eq!(
            execute_proposal(&reward_tx, &mut on_commit, &mut on_abort),
            Err(Error::RemoveWrongStaker)
        );
    }

    #[test]
    fn a_reward_transaction_run_early_is_refused() {
        let end = 1000 + YEAR;
        let (mut state, staker_tx) = served(end, 500);
        state.set_timestamp(end - 1);
        let mut on_commit = state.clone();
        let mut on_abort = state.clone();
        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: staker_tx.id(),
            },
            0,
        );
        assert_eq!(
            execute_proposal(&reward_tx, &mut on_commit, &mut on_abort),
            Err(Error::RemoveStakerTooEarly)
        );
    }

    #[test]
    fn a_reward_transaction_carrying_a_credential_is_refused() {
        // It authorises nothing, so a signature on it is somebody claiming a
        // say in a decision that is the chain's.
        let end = 1000 + YEAR;
        let (state, staker_tx) = served(end, 500);
        let mut on_commit = state.clone();
        let mut on_abort = state.clone();
        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: staker_tx.id(),
            },
            1,
        );
        assert_eq!(
            execute_proposal(&reward_tx, &mut on_commit, &mut on_abort),
            Err(Error::WrongNumberOfCredentials)
        );
    }

    #[test]
    fn a_standard_transaction_is_not_a_proposal() {
        let mut on_commit = State::new();
        let mut on_abort = State::new();
        let tx = signed(Unsigned::Base(envelope(vec![], vec![])), 0);
        assert_eq!(
            execute_proposal(&tx, &mut on_commit, &mut on_abort),
            Err(Error::WrongTxType(Kind::Base))
        );
    }

    #[test]
    fn a_delegators_reward_is_split_with_its_validator() {
        let now = 1000;
        let (mut state, validator_tx) = with_validator(now, 10 * MEGA, now + YEAR);
        let input = fund_more(&mut state, 2, MEGA);
        let delegator_tx = signed(delegate(5, MEGA, now + YEAR, input), 1);
        execute_standard(&mut state, &delegator_tx, &config(), &fees()).unwrap();
        state.add_tx(delegator_tx.clone());

        // Pin the delegator's reward and put the clock at its end.
        let mut d = state
            .current_delegators(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .next()
            .unwrap()
            .clone();
        state.delete_current_delegator(&d);
        d.potential_reward = 1_000_000;
        d.end_time = now + YEAR;
        d.next_time = now + YEAR;
        state.put_current_delegator(d);
        state.set_timestamp(now + YEAR);
        state.add_tx(validator_tx);

        let mut on_commit = state.clone();
        let mut on_abort = state.clone();
        let reward_tx = signed(
            Unsigned::RewardValidator {
                staker_tx_id: delegator_tx.id(),
            },
            0,
        );
        execute_proposal(&reward_tx, &mut on_commit, &mut on_abort).unwrap();

        // 2% of a million to the validator, the rest to the delegator.
        let (delegatee, delegator) = reward::split(1_000_000, 20_000);
        assert_eq!(delegatee, 20_000);
        assert_eq!(
            on_commit.delegatee_reward(&PRIMARY_NETWORK_ID, &NodeId([5; 20])),
            delegatee,
            "the validator's cut is accrued, not paid yet"
        );
        assert_eq!(
            on_commit.reward_utxos(&delegator_tx.id())[0].output.amount,
            delegator
        );
    }

    #[test]
    fn a_transaction_spends_what_it_names_and_makes_what_it_declares() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        let spent = input.utxo;
        let tx = signed(
            Unsigned::Base(envelope(vec![input], vec![output(60, 1)])),
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Ok(())
        );
        assert!(state.utxo(&spent.input_id()).is_err());
        let made = UtxoId {
            tx_id: tx.id(),
            output_index: 0,
        };
        assert_eq!(state.utxo(&made.input_id()).unwrap().output.amount, 60);
    }

    #[test]
    fn a_transaction_spending_an_output_that_does_not_exist_is_refused() {
        let mut state = State::new();
        state.set_timestamp(1000);
        let tx = signed(
            Unsigned::Base(envelope(
                vec![Input {
                    utxo: UtxoId {
                        tx_id: [42; 32],
                        output_index: 0,
                    },
                    asset: ASSET,
                    stake_lock: 0,
                    amount: 100,
                    sig_indices: vec![0],
                }],
                vec![output(100, 1)],
            )),
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::State(StateError::NotFound))
        );
    }

    #[test]
    fn a_chain_name_may_not_be_taken_twice() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        let mk = |input: Input, name: &str| {
            signed(
                Unsigned::CreateChain {
                    base: envelope(vec![input], vec![]),
                    chain: [6; 32],
                    vm_id: [8; 32],
                    name: name.to_string(),
                    fx_ids: vec![],
                    genesis: vec![],
                    chain_auth: vec![0],
                },
                1,
            )
        };
        let first = mk(input, "MyChain");
        assert_eq!(
            execute_standard(&mut state, &first, &config(), &fees()),
            Ok(())
        );

        let second_input = fund_more(&mut state, 2, 100);
        let second = mk(second_input, "mychain");
        assert_eq!(
            execute_standard(&mut state, &second, &config(), &fees()),
            Err(Error::ChainNameTaken)
        );
    }

    #[test]
    fn only_a_named_validator_may_be_removed_by_name() {
        let now = 1000;
        let chain = [6u8; 32];
        let mut state = State::new();
        state.set_timestamp(now);
        state.set_current_supply(chain, 0);
        // A validator that bought its place on a network, rather than one the
        // network's owner admitted by name.
        state
            .put_current_validator(crate::state::Staker {
                tx_id: [7; 32],
                node_id: NodeId([5; 20]),
                public_key: None,
                chain,
                weight: 10 * MEGA,
                start_time: now,
                end_time: now + YEAR,
                potential_reward: 0,
                next_time: now + YEAR,
                priority: Priority::ChainPermissionlessValidatorCurrent,
            })
            .unwrap();

        let input = fund_more(&mut state, 2, 100);
        let tx = signed(
            Unsigned::RemoveChainValidator {
                base: envelope(vec![input], vec![]),
                node_id: NodeId([5; 20]),
                chain,
                chain_auth: vec![0],
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::RemovePermissionlessValidator)
        );
    }

    /// The primary network has no owner to authorise a removal, so a removal
    /// naming it is refused where it is read rather than where it is run.
    #[test]
    fn a_removal_naming_the_primary_network_is_refused_before_it_runs() {
        let mut state = State::new();
        state.set_timestamp(1000);
        let input = fund_more(&mut state, 2, 100);
        let tx = signed(
            Unsigned::RemoveChainValidator {
                base: envelope(vec![input], vec![]),
                node_id: NodeId([5; 20]),
                chain: PRIMARY_NETWORK_ID,
                chain_auth: vec![0],
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::Syntactic(
                crate::txs::Error::RemovePrimaryNetworkValidator
            ))
        );
    }

    #[test]
    fn removing_a_node_that_validates_nothing_is_refused() {
        let mut state = State::new();
        state.set_timestamp(1000);
        let input = fund_more(&mut state, 2, 100);
        let tx = signed(
            Unsigned::RemoveChainValidator {
                base: envelope(vec![input], vec![]),
                node_id: NodeId([5; 20]),
                chain: [7; 32],
                chain_auth: vec![0],
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::NotValidator)
        );
    }

    /// The check the whole chain rests on: an output moves only for the person
    /// who owns it.
    ///
    /// The transaction below is well formed, its arithmetic balances, and its
    /// credential is the right shape — it is only signed by the wrong key.
    /// Nothing else in this file would catch that.
    #[test]
    fn a_spend_signed_by_someone_else_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        let unsigned = Unsigned::Base(envelope(vec![input], vec![output(90, 2)]));
        let sighash = crate::ids::hash256(&unsigned.to_bytes());
        let stranger = k256::ecdsa::SigningKey::from_bytes(&[0x22u8; 32].into()).unwrap();
        let tx = Tx::new(
            unsigned,
            vec![Credential {
                sigs: vec![crate::sign::sign(&stranger, &sighash)],
            }],
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::Credential(flow::CredentialError::WrongSigner))
        );
    }

    /// And a signature over a different transaction does not authorise this
    /// one, so an authorisation cannot be lifted off one spend onto another.
    #[test]
    fn a_signature_lifted_from_another_transaction_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        let elsewhere = Unsigned::Base(envelope(vec![input.clone()], vec![output(10, 2)]));
        let borrowed = crate::sign::sign(&spender(), &crate::ids::hash256(&elsewhere.to_bytes()));
        let tx = Tx::new(
            Unsigned::Base(envelope(vec![input], vec![output(90, 2)])),
            vec![Credential {
                sigs: vec![borrowed],
            }],
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::Credential(flow::CredentialError::WrongSigner))
        );
    }

    /// A credential with no signature in it authorises nothing either.
    #[test]
    fn a_spend_with_an_empty_credential_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        let tx = Tx::new(
            Unsigned::Base(envelope(vec![input], vec![output(90, 2)])),
            vec![Credential::default()],
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::Credential(
                flow::CredentialError::WrongNumberOfSignatures {
                    given: 0,
                    needed: 1
                }
            ))
        );
    }

    /// A delegation that leaves as another arrives is two delegations at once
    /// for an instant, and the limit is measured over that instant.
    ///
    /// The validator carries 10 of its own and 20 delegated, and its ceiling
    /// is 50. At one instant a 20 leaves and a 15 arrives: for that instant
    /// the validator carries 45, not 30. A 6 on top of that is 51 and is
    /// refused. Reading the departure first would report 30, admit the 6, and
    /// put the validator past its ceiling — which is why the order of changes
    /// at one instant is a rule and not a detail.
    #[test]
    fn a_delegation_leaving_as_another_arrives_counts_at_its_peak() {
        let now = 1000;
        let change = now + 100;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + YEAR);

        state.put_current_delegator(crate::state::Staker {
            tx_id: [0xd1; 32],
            node_id: NodeId([5; 20]),
            public_key: None,
            chain: PRIMARY_NETWORK_ID,
            weight: 20 * MEGA,
            start_time: now,
            end_time: change,
            potential_reward: 0,
            next_time: change,
            priority: Priority::PrimaryNetworkDelegatorCurrent,
        });
        state.put_pending_delegator(crate::state::Staker {
            tx_id: [0xd2; 32],
            node_id: NodeId([5; 20]),
            public_key: None,
            chain: PRIMARY_NETWORK_ID,
            weight: 15 * MEGA,
            start_time: change,
            end_time: now + YEAR,
            potential_reward: 0,
            next_time: change,
            priority: Priority::PrimaryNetworkDelegatorPermissionlessPending,
        });

        // 10 own + 20 leaving + 15 arriving = 45 at the instant they overlap,
        // so 6 more is 51 against a ceiling of 50.
        let input = fund_more(&mut state, 4, 6 * MEGA);
        let over = signed(delegate(5, 6 * MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &over, &config(), &fees()),
            Err(Error::OverDelegated)
        );

        // And a 5 is 50 exactly, which is the ceiling and not past it.
        let input = fund_more(&mut state, 5, 5 * MEGA);
        let ok = signed(delegate(5, 5 * MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &ok, &config(), &fees()),
            Ok(())
        );
    }

    /// Which asset a network stakes is the executor's question, not the
    /// bytes'.
    ///
    /// The transaction below is well formed — Go's syntactic check compares a
    /// stake output only to the other stake outputs — and it stakes something
    /// that is not the chain's own asset on the primary network, where the
    /// asset is fixed. It is the executor that knows that, and the executor
    /// that refuses it.
    #[test]
    fn the_primary_network_is_staked_in_its_own_asset_and_nothing_else() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let mut unsigned = add_validator(5, 10 * MEGA, now + YEAR, input, 0);
        if let Unsigned::AddPermissionlessValidator { stake, .. } = &mut unsigned {
            for out in stake.iter_mut() {
                out.asset = [0xee; 32];
            }
        }
        // Well formed: the stake is one asset and it adds to the weight.
        assert_eq!(unsigned.syntactic_verify(config().native_asset), Ok(()));

        let tx = signed(unsigned, 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees()),
            Err(Error::WrongStakedAsset)
        );
    }
}
