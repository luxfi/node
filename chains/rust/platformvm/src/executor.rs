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

use crate::components::{Credential, Output, Owners, Utxo, UtxoId};
use crate::flow;
use crate::ids::{Id, NodeId, EMPTY, PRIMARY_NETWORK_ID};
use crate::reward;
use crate::state::{Error as StateError, Staker, State};
use crate::txs::{bounded_by, Kind, NetworkValidator, Priority, Tx, Unsigned};

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
    /// How much of its term a validator must have been reachable for to be
    /// paid, in millionths.
    pub uptime_requirement: u32,
}

/// Everything the executor needs that is not state.
#[derive(Clone, Debug)]
pub struct Config {
    /// Which network this is. A warp message names the network it was signed
    /// for, and one signed for another network proves nothing here.
    pub network_id: u32,
    /// This chain's own id. A transaction addressed to another chain of the
    /// same network is refused for the same reason one addressed to another
    /// network is: it was signed for somewhere else.
    pub blockchain_id: Id,
    /// The asset stake and fees are denominated in.
    pub native_asset: Id,
    /// What an L1 validator pays, continuously, for the P-Chain's trouble in
    /// tracking it — and how many of them the chain has room for at once.
    pub validator_fee: crate::l1::FeeConfig,
    pub staking: StakingPolicy,
    pub reward: reward::Config,
    /// Every staking policy that has been in force, oldest first.
    ///
    /// A validator is judged on the terms it agreed to when it bonded, so the
    /// uptime a reward is measured against is read out of this at the
    /// validator's start time rather than at the present. Without it,
    /// [`StakingPolicy::uptime_requirement`] binds always, which is what an
    /// ungoverned chain has.
    pub staking_history: Option<crate::stakingparams::History>,
    /// False while the node is still catching up. Go skips the checks that
    /// need a complete view of the world until this is true, because a node
    /// that has not finished syncing would refuse valid transactions.
    pub bootstrapped: bool,
}

/// What a transaction costs.
///
/// Injected, not computed: Go passes a `fee.Calculator` into every execution
/// for the same reason, and this is the seam the two chains differ across.
///
/// **They do differ.** Go's live P-Chain charges by gas, always — its
/// `state.PickFeeCalculator` returns `txfee.NewDynamicCalculator` on every
/// verify and every build, and says of the flat schedule that it "is
/// unreachable". This chain charges [`FlatFees`]. So for the same transaction
/// the two answer different amounts, and a node of each would disagree about
/// whether its fee was paid.
///
/// It is written here, at the seam, because this is where it would be fixed:
/// the dynamic calculator is complexity → gas → cost, and it arrives as another
/// implementation of this trait, not as a change to any caller. Porting it means
/// porting Go's per-kind complexity tables (`vms/platformvm/txs/fee/complexity.go`),
/// which is a body of arithmetic that has to be checked against Go vector by
/// vector before it is trusted — an unchecked port of it would not close this
/// divergence, it would move it somewhere harder to see.
pub trait Fees {
    fn fee(&self, tx: &Unsigned) -> u64;
}

/// The flat per-kind fee schedule — what this chain actually charges.
///
/// Go states it as `fee.StaticConfig` and it is reproduced field for field. Go
/// no longer reaches it; see [`Fees`] for what Go charges instead and why that
/// is not here.
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

/// How much of its term a validator was reachable for.
///
/// The chain cannot measure this: reachability is something each node observes
/// for itself, and two honest nodes will observe slightly different numbers.
/// That is why the reward is a *proposal* — every node answers it from its own
/// measurement and the stake-weighted vote settles it — and why this is a seam
/// the node fills rather than something state holds.
pub trait Uptime: Send + Sync {
    /// The fraction of the time since `since` that `node` was reachable on
    /// `chain`, between 0 and 1, or nothing when it cannot be said.
    fn fraction_since(&self, node: &NodeId, chain: &Id, since: u64) -> Option<f64>;
}

/// What another chain has already handed to this one.
///
/// Go: `atomic.SharedMemory`. An import spends an output that was made
/// somewhere else, so the P-chain cannot check it alone — only the shared half
/// both chains write to can say the export happened. The node holds that half
/// and the chain asks it, which is why this is a seam and not something state
/// holds: an import of something nobody exported has nothing to find.
///
/// Removing what an import consumed from the shared half is the node's, on
/// accept, reading the accepted block. Go arranges the same split: its executor
/// returns `AtomicRequests` and the node applies them when the block is
/// accepted, because an import that vanished from the shared half while its
/// block was still undecided would be money nobody could spend.
pub trait Atomic: Send + Sync {
    /// The outputs `ids` name on `source`, in the order they were asked for. A
    /// name the shared half does not hold answers `None`.
    fn imported(&self, source: &Id, ids: &[UtxoId]) -> Vec<Option<Utxo>>;
}

/// A node with no shared half: every import finds nothing.
///
/// Not a stub standing in for the real thing — it is the honest answer for a
/// node that is not connected to another chain, and it is the answer Go gives
/// from an empty shared memory. Every import against it is refused for want of
/// what it names, never accepted against nothing.
pub struct NoImports;

impl Atomic for NoImports {
    fn imported(&self, _source: &Id, ids: &[UtxoId]) -> Vec<Option<Utxo>> {
        vec![None; ids.len()]
    }
}

/// Whether this node would pay the staker the transaction retires.
///
/// This is the whole reward gate. A validator is judged against the uptime
/// rule that was in force when it BONDED, not the one in force now — which is
/// what stops a governed uptime requirement from being retroactive. Without
/// it, a stake majority could raise the bar the day before a rival's stake
/// matures and take its reward, and that is expropriation rather than
/// governance.
///
/// A refusal here is not "do not pay": it is "this node cannot say", and the
/// caller answers that by paying. Go does the same and says why — err on the
/// side of over-rewarding rather than under-rewarding, because the alternative
/// is that an unusual case or a hostile block proposer costs an honest
/// validator its reward.
pub fn prefers_reward(
    state: &State,
    tx: &Tx,
    config: &Config,
    uptime: &dyn Uptime,
) -> Result<bool, Error> {
    let Unsigned::RewardValidator { staker_tx_id } = &tx.unsigned else {
        return Err(Error::WrongTxType(tx.unsigned.kind()));
    };
    let staker_tx = state.tx(staker_tx_id)?;
    let staker = staker_tx
        .unsigned
        .staker()
        .ok_or(Error::ShouldBePermissionlessStaker)?;
    let node = staker.validator.node_id;

    // The bond is on the primary network even when the stake is not: a
    // network validator is one only for as long as it validates the primary
    // network, and that is the term its reachability is measured over.
    let primary = state.current_validator(&PRIMARY_NETWORK_ID, &node)?;

    let required = required_uptime(state, config, staker.chain, primary.start_time)?;
    let measured = uptime
        .fraction_since(&node, &staker.chain, primary.start_time)
        .ok_or(Error::UptimeUnknown)?;
    Ok(measured >= required)
}

/// The fraction of its term a validator bonding at `bonded_at` must have been
/// reachable for.
fn required_uptime(
    state: &State,
    config: &Config,
    chain: Id,
    bonded_at: u64,
) -> Result<f64, Error> {
    // A network states its own requirement in the transformation that made it
    // staked, and that requirement is read as it was when the validator bonded
    // — a transformation is written once, at genesis, and never rewritten.
    let millionths = if chain == PRIMARY_NETWORK_ID {
        policy_at(config, bonded_at).uptime_requirement
    } else {
        transformation_of(state, chain)?.uptime_requirement
    };
    Ok(millionths as f64 / crate::reward::PERCENT_DENOMINATOR as f64)
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
    /// A reward that names nothing.
    InvalidId,
    /// Nothing could say how reachable the validator had been.
    UptimeUnknown,
    /// A network whose staking terms nobody ever stated. Its validators are
    /// admitted through the L1 plane instead.
    NoNetworkTerms,
    /// A network that has no owner: it was never created here.
    NoSuchNetwork,
    /// The modification is not signed for by the owner it names.
    NotAuthorized(flow::CredentialError),
    /// A network that has converted or transformed answers to its own rules,
    /// not to the P-Chain owner that made it.
    NetworkIsImmutable,
    /// A contract-managed set that names no contract to manage it.
    ManagerNeedsAddress,
    /// There is no room for another active L1 validator.
    MaxActiveL1Validators,
    /// The warp message this transaction carries is not one.
    Warp(crate::warp::Error),
    /// The payload inside the warp message is not what this transaction needs.
    WarpPayload(crate::warpmsg::Error),
    /// A registration message whose moment has passed.
    WarpMessageExpired { expiry: u64, now: u64 },
    /// A registration message issued so far ahead that remembering it until it
    /// expired would be a way to fill the chain's memory.
    WarpMessageNotYetAllowed { seconds: u64, limit: u64 },
    /// A registration already issued once. The expiry set is the whole replay
    /// defence.
    WarpMessageAlreadyIssued(Id),
    /// The message did not come from the chain and address the conversion
    /// recorded, so whoever sent it does not speak for this L1.
    WrongWarpSource,
    /// A conversion this chain never recorded.
    NoConversion(Id),
    /// A weight message older than one already applied.
    StaleNonce { given: u64, least: u64 },
    /// The largest nonce is reserved for the change that removes a validator.
    NonceReservedForRemoval,
    /// Removing the last validator of a converted chain, which would leave a
    /// chain nobody can ever speak for again.
    RemovingLastValidator,
    /// A validation id nothing is registered under.
    NoSuchL1Validator(Id),
    /// A record that cannot be read back. Refused rather than trusted, because
    /// the alternative is minting from a corrupt row.
    CorruptState(&'static str),
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
            Error::InvalidId => write!(f, "invalid ID"),
            Error::UptimeUnknown => write!(f, "nothing can say how reachable the validator was"),
            Error::NoNetworkTerms => {
                write!(f, "this network never stated staking terms of its own")
            }
            Error::NoSuchNetwork => write!(f, "no such network"),
            Error::NotAuthorized(e) => write!(f, "unauthorized modification: {e}"),
            Error::NetworkIsImmutable => {
                write!(f, "this network answers to its own rules now, not to its owner")
            }
            Error::ManagerNeedsAddress => {
                write!(f, "a contract-managed set must name the contract that manages it")
            }
            Error::MaxActiveL1Validators => {
                write!(f, "already at the max number of active validators")
            }
            Error::Warp(e) => write!(f, "{e}"),
            Error::WarpPayload(e) => write!(f, "{e}"),
            Error::WarpMessageExpired { expiry, now } => {
                write!(f, "the warp message expired at {expiry} and it is now {now}")
            }
            Error::WarpMessageNotYetAllowed { seconds, limit } => write!(
                f,
                "the warp message is {seconds} seconds in the future but the limit is {limit}"
            ),
            Error::WarpMessageAlreadyIssued(id) => {
                write!(f, "the warp message for {} was already issued", hex(id))
            }
            Error::WrongWarpSource => {
                write!(f, "the warp message did not come from this L1's manager")
            }
            Error::NoConversion(id) => write!(f, "{} has no recorded conversion", hex(id)),
            Error::StaleNonce { given, least } => {
                write!(f, "nonce {given} must be at least {least}")
            }
            Error::NonceReservedForRemoval => {
                write!(f, "the largest nonce may only remove a validator")
            }
            Error::RemovingLastValidator => {
                write!(f, "attempting to remove the last L1 validator from a converted chain")
            }
            Error::NoSuchL1Validator(id) => {
                write!(f, "no L1 validator is registered as {}", hex(id))
            }
            Error::CorruptState(what) => write!(f, "state corruption: {what}"),
        }
    }
}

impl std::error::Error for Error {}

/// An id as it is written in a refusal. Short enough to read, long enough to
/// find in a log.
fn hex(id: &Id) -> String {
    id.iter().map(|b| format!("{b:02x}")).collect()
}

impl From<crate::txs::Error> for Error {
    fn from(e: crate::txs::Error) -> Self {
        Error::Syntactic(e)
    }
}

impl From<crate::warp::Error> for Error {
    fn from(e: crate::warp::Error) -> Self {
        Error::Warp(e)
    }
}

impl From<crate::warpmsg::Error> for Error {
    fn from(e: crate::warpmsg::Error) -> Self {
        Error::WarpPayload(e)
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
    atomic: &dyn Atomic,
) -> Result<(), Error> {
    tx.syntactic_verify(config.chain())?;

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

        Unsigned::Import {
            base,
            source_chain,
            imported,
        } => {
            // What the source chain exported, from the half both chains write
            // to. A name it does not hold is refused here rather than trusted:
            // an import is a spend of value made somewhere else, and this is
            // the only place that can say the export happened.
            let names: Vec<UtxoId> = imported.iter().map(|i| i.utxo).collect();
            let found = atomic.imported(source_chain, &names);
            let mut utxos = utxos_of(state, &base.ins)?;
            for utxo in found {
                utxos.push(utxo.ok_or(Error::State(StateError::NotFound))?);
            }

            // One spend, over this chain's inputs and the imported ones
            // together: they buy the same outputs and pay the same fee, so
            // checking them apart would let either half fund the other.
            let mut ins = base.ins.clone();
            ins.extend_from_slice(imported);
            charge_utxos(
                state, tx, &utxos, &ins, &base.outs, &tx.creds, 0, config, fees,
            )?;

            // Only this chain's inputs are consumed here. The imported ones
            // are removed from the shared half by the node when the block is
            // accepted — see [`Atomic`].
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
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

        Unsigned::CreateChain {
            base,
            chain,
            name,
            chain_auth,
            ..
        } => {
            if !name.is_empty() && state.is_chain_name_taken(name) {
                return Err(Error::ChainNameTaken);
            }
            // A chain runs on a network, and only that network's owner may put
            // one there. Without this check anyone could add a chain to
            // anybody's network.
            let base_creds =
                verify_poa_chain_authorization(state, tx, chain, chain_auth)?.to_vec();
            charge_creds(state, tx, &base.ins, &base.outs, &base_creds, 0, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.add_blockchain(tx.id(), name);
            state.add_tx(tx.clone());
            Ok(())
        }

        Unsigned::AddChainValidator {
            base,
            validator,
            chain,
            chain_auth,
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
                // And the network's owner must have admitted this one by name:
                // that is what "permissioned" means, and without the check the
                // set is open to anyone who can pay the fee.
                let base_creds =
                    verify_poa_chain_authorization(state, tx, chain, chain_auth)?.to_vec();
                charge_creds(state, tx, &base.ins, &base.outs, &base_creds, 0, config, fees)?;
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
            // The terms are the network's own, and a network states them in
            // the transformation that made it staked. Judging a joiner against
            // the primary network's terms instead would admit it on rules
            // nobody there agreed to, so a network that stated none refuses by
            // name rather than borrowing somebody else's.
            let rules = network_rules(state, config, *chain)?;

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
            let rules = network_rules(state, config, *chain)?;

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
            chain_auth,
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
                // A validator admitted by name is removed by the same owner
                // that named it, and by nobody else.
                let base_creds = verify_chain_authorization(state, tx, chain, chain_auth)?.to_vec();
                charge_creds(state, tx, &base.ins, &base.outs, &base_creds, 0, config, fees)?;
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
            base,
            chain,
            chain_auth,
            owner,
        } => {
            // Handing a network on is the most consequential thing its owner
            // can do, so it is the owner that has to sign for it.
            let base_creds = verify_chain_authorization(state, tx, chain, chain_auth)?.to_vec();
            charge_creds(state, tx, &base.ins, &base.outs, &base_creds, 0, config, fees)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.set_chain_owner(*chain, owner.clone());
            Ok(())
        }

        Unsigned::IncreaseL1ValidatorBalance {
            base,
            validation_id,
            balance,
        } => {
            // Anyone may top up an L1 validator's balance: nobody has to be
            // authorised to pay someone else's fees.
            charge_creds(
                state,
                tx,
                &base.ins,
                &base.outs,
                &tx.creds,
                *balance,
                config,
                fees,
            )?;
            let mut validator = state
                .l1_validator(validation_id)
                .map_err(|_| Error::NoSuchL1Validator(*validation_id))?
                .clone();

            // A top-up of an inactive validator activates it, and there is only
            // so much room for active ones.
            if validator.end_accumulated_fee == 0 {
                if state.num_active_l1_validators() as u64 >= config.validator_fee.capacity {
                    return Err(Error::MaxActiveL1Validators);
                }
                validator.end_accumulated_fee = state.accrued_fees();
            }
            validator.end_accumulated_fee = validator
                .end_accumulated_fee
                .checked_add(*balance)
                .ok_or(Error::Overflow)?;

            state.put_l1_validator(validator)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
        }

        Unsigned::DisableL1Validator {
            base,
            validation_id,
            auth,
        } => {
            // Switching a validator off is the one L1 operation the P-Chain
            // authorises itself, against the deactivation owner the
            // registration named.
            let validator = state
                .l1_validator(validation_id)
                .map_err(|_| Error::NoSuchL1Validator(*validation_id))?
                .clone();
            let owner = Owners::unmarshal(&validator.deactivation_owner)
                .map_err(|_| Error::CorruptState("the deactivation owner is malformed"))?;
            let base_creds = verify_authorization(tx, &owner, auth, state.timestamp())?.to_vec();
            charge_creds(
                state,
                tx,
                &base.ins,
                &base.outs,
                &base_creds,
                0,
                config,
                fees,
            )?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);

            // Already off: nothing to refund and nothing to change.
            if validator.end_accumulated_fee == 0 {
                return Ok(());
            }
            refund_remaining_balance(state, config, tx.id(), base.outs.len(), &validator)?;
            let mut off = validator;
            off.end_accumulated_fee = 0;
            state.put_l1_validator(off)?;
            Ok(())
        }

        Unsigned::CreateNetwork {
            base,
            owner,
            security,
            validators,
            manager_chain_id,
            manager_address,
            ..
        } => {
            // The network's id IS this transaction's id, which is what lets
            // every later transaction that names the network name it without
            // anyone having chosen a name — and it is what the seeded set is
            // keyed under.
            let network_id = tx.id();

            // Every activated validator's balance is charged on top of the base
            // fee: it funds that validator's continuously-charged mark, so the
            // backing LUX must be spent here or it would be minted.
            let mut prepaid: u64 = 0;
            if security.sovereign() {
                for v in validators {
                    prepaid = prepaid.checked_add(v.balance).ok_or(Error::Overflow)?;
                }
            }
            charge_creds(
                state,
                tx,
                &base.ins,
                &base.outs,
                &tx.creds,
                prepaid,
                config,
                fees,
            )?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.add_chain(network_id, owner.clone());

            // A network that runs a set of its own has that set seeded now.
            // Chains themselves are added by CreateChain.
            if security.sovereign() {
                register_own_set(
                    state,
                    config,
                    network_id,
                    validators,
                    security,
                    *manager_chain_id,
                    manager_address,
                )?;
            }
            Ok(())
        }

        Unsigned::ConvertNetwork {
            base,
            network,
            manager_chain_id,
            manager_address,
            validators,
            auth,
            security,
            ..
        } => {
            // The existing network owner must authorise the promotion.
            let base_creds =
                verify_poa_chain_authorization(state, tx, network, auth)?.to_vec();

            let mut prepaid: u64 = 0;
            for v in validators {
                prepaid = prepaid.checked_add(v.balance).ok_or(Error::Overflow)?;
            }

            // The set is established before the spend is checked, exactly as Go
            // orders it: the capacity refusal is about the set, not the money.
            register_own_set(
                state,
                config,
                *network,
                validators,
                security,
                *manager_chain_id,
                manager_address,
            )?;
            charge_creds(
                state,
                tx,
                &base.ins,
                &base.outs,
                &base_creds,
                prepaid,
                config,
                fees,
            )?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
        }

        Unsigned::TransformChain { .. } => Err(Error::TransformChainNotPermitted),

        Unsigned::RegisterL1Validator {
            base,
            balance,
            proof_of_possession,
            message,
        } => {
            let now = state.timestamp();
            charge_creds(
                state,
                tx,
                &base.ins,
                &base.outs,
                &tx.creds,
                *balance,
                config,
                fees,
            )?;

            let (warp_message, call) = open_warp_call(message)?;
            let crate::warpmsg::Message::Register(msg) =
                crate::warpmsg::parse_message(&call.payload)?
            else {
                return Err(Error::WarpPayload(crate::warpmsg::Error::WrongKind));
            };
            msg.verify()?;

            verify_l1_conversion(
                state,
                msg.chain_id,
                warp_message.unsigned.source_chain_id,
                &call.source_address,
            )?;

            // The expiry bounds how long the chain has to remember this message
            // in order to refuse a replay of it.
            if msg.expiry <= now {
                return Err(Error::WarpMessageExpired {
                    expiry: msg.expiry,
                    now,
                });
            }
            let until = msg.expiry - now;
            if until > REGISTER_EXPIRY_WINDOW {
                return Err(Error::WarpMessageNotYetAllowed {
                    seconds: until,
                    limit: REGISTER_EXPIRY_WINDOW,
                });
            }

            let validation_id = msg.validation_id();
            let expiry = crate::l1::Expiry {
                timestamp: msg.expiry,
                validation_id,
            };
            // The whole replay defence: the chain remembers every registration
            // it has seen until the moment that registration could no longer be
            // issued.
            if state.has_expiry(&expiry) {
                return Err(Error::WarpMessageAlreadyIssued(validation_id));
            }

            // The message says which key; the transaction proves whoever sent
            // it holds that key. Neither alone is enough.
            crate::signer::Signer::ProofOfPossession {
                public_key: msg.bls_public_key,
                proof: *proof_of_possession,
            }
            .verify()
            .map_err(|e| Error::Syntactic(crate::txs::Error::Signer(e)))?;

            let mut node_id = [0u8; crate::ids::NODE_ID_LEN];
            node_id.copy_from_slice(&msg.node_id);
            let public_key = crate::signer::uncompress(&msg.bls_public_key)
                .map_err(|e| Error::Syntactic(crate::txs::Error::Signer(e)))?;

            let mut validator = crate::l1::Validator {
                validation_id,
                chain_id: msg.chain_id,
                node_id: NodeId(node_id),
                public_key,
                remaining_balance_owner: msg.remaining_balance_owner.as_owners().marshal(),
                deactivation_owner: msg.disable_owner.as_owners().marshal(),
                start_time: now,
                weight: msg.weight,
                min_nonce: 0,
                // A zero balance leaves it inactive.
                end_accumulated_fee: 0,
            };
            if *balance != 0 {
                if state.num_active_l1_validators() as u64 >= config.validator_fee.capacity {
                    return Err(Error::MaxActiveL1Validators);
                }
                // The balance is stored as the accrued-fee mark it can pay up
                // to, so deactivation is a comparison rather than a
                // per-validator decrement.
                validator.end_accumulated_fee = balance
                    .checked_add(state.accrued_fees())
                    .ok_or(Error::Overflow)?;
            }

            state.put_l1_validator(validator)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            state.put_expiry(expiry);
            Ok(())
        }

        Unsigned::SetL1ValidatorWeight { base, message } => {
            charge(state, tx, &base.ins, &base.outs, config, fees)?;

            let (warp_message, call) = open_warp_call(message)?;
            let crate::warpmsg::Message::Weight(msg) =
                crate::warpmsg::parse_message(&call.payload)?
            else {
                return Err(Error::WarpPayload(crate::warpmsg::Error::WrongKind));
            };
            // The largest nonce is reserved for the change that removes a
            // validator, so the increment below can never overflow for one that
            // stays.
            if msg.nonce == u64::MAX && msg.weight != 0 {
                return Err(Error::NonceReservedForRemoval);
            }

            let mut validator = state
                .l1_validator(&msg.validation_id)
                .map_err(|_| Error::NoSuchL1Validator(msg.validation_id))?
                .clone();

            // The nonce is the whole replay defence for weight: an old message
            // cannot be re-sent over a newer one.
            if msg.nonce < validator.min_nonce {
                return Err(Error::StaleNonce {
                    given: msg.nonce,
                    least: validator.min_nonce,
                });
            }

            verify_l1_conversion(
                state,
                validator.chain_id,
                warp_message.unsigned.source_chain_id,
                &call.source_address,
            )?;

            if msg.weight == 0 {
                // A chain with no validators is a chain nobody can ever speak
                // for again, so the last one cannot be removed.
                if state.weight_of_l1_validators(&validator.chain_id)? == validator.weight {
                    return Err(Error::RemovingLastValidator);
                }
                if validator.end_accumulated_fee != 0 {
                    refund_remaining_balance(
                        state,
                        config,
                        tx.id(),
                        base.outs.len(),
                        &validator,
                    )?;
                }
            }

            validator.min_nonce = msg.nonce.wrapping_add(1);
            validator.weight = msg.weight;
            state.put_l1_validator(validator)?;
            state.consume_and_produce(tx.id(), &base.ins, &base.outs);
            Ok(())
        }
    }
}

/// How long a registration message may sit ahead of the clock. The chain
/// remembers every registration until it expires in order to refuse a replay,
/// so this bounds how much it has to remember. Go:
/// `RegisterL1ValidatorTxExpiryWindow`.
pub const REGISTER_EXPIRY_WINDOW: u64 = 24 * 60 * 60;

/// The share of a source chain's weight a warp message must carry. Go:
/// `WarpQuorumNumerator` / `WarpQuorumDenominator`.
pub const WARP_QUORUM_NUMERATOR: u64 = 67;
pub const WARP_QUORUM_DENOMINATOR: u64 = 100;

/// The staking terms a network admits on.
///
/// The primary network's are the compiled-in policy in force at the moment the
/// joiner joins. Every other network states its own in the transformation that
/// made it staked; a network that never stated any admits validators through
/// the L1 plane instead, and is told so by name rather than judged on the
/// primary network's rules — which nobody there agreed to.
fn network_rules(state: &State, config: &Config, chain: Id) -> Result<StakingPolicy, Error> {
    if chain == PRIMARY_NETWORK_ID {
        return Ok(policy_at(config, state.timestamp()));
    }
    let terms = transformation_of(state, chain)?;
    Ok(StakingPolicy {
        min_validator_stake: terms.min_validator_stake,
        max_validator_stake: terms.max_validator_stake,
        min_delegator_stake: terms.min_delegator_stake,
        min_stake_duration: terms.min_stake_duration as u64,
        max_stake_duration: terms.max_stake_duration as u64,
        min_delegation_fee: terms.min_delegation_fee,
        uptime_requirement: terms.uptime_requirement,
    })
}

/// What a network's own transformation says, as a value rather than as a
/// transaction. Everything a joiner is judged against lives here.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Terms {
    pub asset_id: Id,
    pub min_validator_stake: u64,
    pub max_validator_stake: u64,
    pub min_delegator_stake: u64,
    pub min_stake_duration: u32,
    pub max_stake_duration: u32,
    pub min_delegation_fee: u32,
    pub max_validator_weight_factor: u8,
    pub uptime_requirement: u32,
    pub min_consumption_rate: u64,
    pub max_consumption_rate: u64,
    pub maximum_supply: u64,
}

/// Go: `GetTransformChainTx`. A network's terms, out of the transformation it
/// stated them in.
fn transformation_of(state: &State, chain: Id) -> Result<Terms, Error> {
    let tx = state.transformation(&chain).map_err(|_| Error::NoNetworkTerms)?;
    let Unsigned::TransformChain {
        asset_id,
        maximum_supply,
        min_consumption_rate,
        max_consumption_rate,
        min_validator_stake,
        max_validator_stake,
        min_stake_duration,
        max_stake_duration,
        min_delegation_fee,
        min_delegator_stake,
        max_validator_weight_factor,
        uptime_requirement,
        ..
    } = &tx.unsigned
    else {
        // The row is keyed by network and written only from genesis, so
        // anything else there is a corrupt record rather than a wrong caller.
        return Err(Error::CorruptState("a network's terms are not a transformation"));
    };
    Ok(Terms {
        asset_id: *asset_id,
        min_validator_stake: *min_validator_stake,
        max_validator_stake: *max_validator_stake,
        min_delegator_stake: *min_delegator_stake,
        min_stake_duration: *min_stake_duration,
        max_stake_duration: *max_stake_duration,
        min_delegation_fee: *min_delegation_fee,
        max_validator_weight_factor: *max_validator_weight_factor,
        uptime_requirement: *uptime_requirement,
        min_consumption_rate: *min_consumption_rate,
        max_consumption_rate: *max_consumption_rate,
        maximum_supply: *maximum_supply,
    })
}

/// The compiled-in policy in force at `at`. A joiner accepts the terms in force
/// at the moment it joins, so that instant is the chain's own clock.
fn policy_at(config: &Config, at: u64) -> StakingPolicy {
    let Some(history) = &config.staking_history else {
        return config.staking;
    };
    let Some(governed) = history.at(at as i64) else {
        return config.staking;
    };
    StakingPolicy {
        min_validator_stake: governed.min_validator_stake,
        max_validator_stake: governed.max_validator_stake,
        // Deliberately not governed: it is the floor on who may delegate at
        // all, and delegators are the one constituency that cannot defend
        // itself by voting, because the vote belongs to the validator.
        min_delegator_stake: config.staking.min_delegator_stake,
        min_stake_duration: governed.min_stake_duration as u64,
        max_stake_duration: governed.max_stake_duration as u64,
        min_delegation_fee: governed.min_delegation_fee,
        uptime_requirement: governed.uptime_requirement,
    }
}

/// What a network pays its stakers. Go: `GetRewardsCalculator`. A network that
/// never transformed mints on the primary network's schedule.
fn rewards_for(state: &State, config: &Config, chain: Id) -> reward::Config {
    match transformation_of(state, chain) {
        Err(_) => config.reward,
        Ok(terms) => reward::Config {
            max_consumption_rate: terms.max_consumption_rate,
            min_consumption_rate: terms.min_consumption_rate,
            minting_period: config.reward.minting_period,
            supply_cap: terms.maximum_supply,
        },
    }
}

impl Config {
    /// The chain a transaction has to be addressed to, out of the chain's own
    /// configuration. One place, so the syntactic pass and the executor cannot
    /// disagree about which chain this is.
    pub fn chain(&self) -> crate::txs::Chain {
        crate::txs::Chain {
            network_id: self.network_id,
            blockchain_id: self.blockchain_id,
            native_asset: self.native_asset,
        }
    }
}

/// Go: `verifyAuthorization`. The **last** credential authorises the
/// modification; the rest authorise the spending.
///
/// The split matters twice over. A transaction that modifies something and
/// spends something carries signatures for both, and running the spend check
/// over the authorisation credential would either accept a spend nobody signed
/// for or refuse one that was signed for correctly.
fn verify_authorization<'a>(
    tx: &'a Tx,
    owner: &Owners,
    auth: &[u32],
    now: u64,
) -> Result<&'a [Credential], Error> {
    if tx.creds.is_empty() {
        return Err(Error::WrongNumberOfCredentials);
    }
    let base_len = tx.creds.len() - 1;
    flow::verify_permission(owner, auth, &tx.creds[base_len], &tx.sighash(), now)
        .map_err(Error::NotAuthorized)?;
    Ok(&tx.creds[..base_len])
}

/// Go: `verifyChainAuthorization`. The same, against the owner of a network.
fn verify_chain_authorization<'a>(
    state: &State,
    tx: &'a Tx,
    chain: &Id,
    auth: &[u32],
) -> Result<&'a [Credential], Error> {
    let owner = state.chain_owner(chain).map_err(|_| Error::NoSuchNetwork)?.clone();
    verify_authorization(tx, &owner, auth, state.timestamp())
}

/// Go: `verifyPoAChainAuthorization`. A network that has transformed or
/// converted is immutable: its own rules govern it now, and the owner that made
/// it no longer speaks for it.
fn verify_poa_chain_authorization<'a>(
    state: &State,
    tx: &'a Tx,
    chain: &Id,
    auth: &[u32],
) -> Result<&'a [Credential], Error> {
    let creds = verify_chain_authorization(state, tx, chain, auth)?;
    if state.transformation(chain).is_ok() || state.conversion(chain).is_ok() {
        return Err(Error::NetworkIsImmutable);
    }
    Ok(creds)
}

/// Go: `registerOwnSet`. Seeds a network's own validator set and records the
/// authority that may change it.
///
/// The one primitive behind both the ∅ → network constructor and the
/// network → network promotion, so a sovereign set is established exactly one
/// way. It only mutates state: the LUX backing every activated validator's
/// balance is spent by the caller's flow check.
fn register_own_set(
    state: &mut State,
    config: &Config,
    network_id: Id,
    validators: &[NetworkValidator],
    security: &crate::security::Mode,
    manager_chain_id: Id,
    manager_address: &[u8],
) -> Result<(), Error> {
    // A contract-governed set must name its manager. `syntactic_verify` already
    // enforces this; stating it here keeps the primitive self-contained.
    if security.manager == crate::security::Manager::Contract
        && (manager_chain_id == EMPTY || manager_address.is_empty())
    {
        return Err(Error::ManagerNeedsAddress);
    }

    let start_time = state.timestamp();
    let accrued = state.accrued_fees();
    let mut data = crate::warpmsg::ConversionData {
        chain_id: network_id,
        manager_chain_id,
        manager_address: manager_address.to_vec(),
        validators: Vec::with_capacity(validators.len()),
    };

    for (i, v) in validators.iter().enumerate() {
        if v.node_id.len() != crate::ids::NODE_ID_LEN {
            return Err(Error::Syntactic(crate::txs::Error::BadNodeIdLength(
                v.node_id.len(),
            )));
        }
        let mut node_id = [0u8; crate::ids::NODE_ID_LEN];
        node_id.copy_from_slice(&v.node_id);

        // The possession pairing was already run by `syntactic_verify` over
        // these same bytes, so this is the decode alone. Still fail-closed: a
        // key that does not parse is refused, because a validator stored
        // keyless carries weight in the quorum denominator with no way for
        // anyone to vote toward it.
        let compressed = v.signer.public_key().ok_or(Error::Syntactic(
            crate::txs::Error::Signer(crate::signer::Error::MalformedPublicKey),
        ))?;
        let public_key = crate::signer::uncompress(&compressed)
            .map_err(|e| Error::Syntactic(crate::txs::Error::Signer(e)))?;

        let mut record = crate::l1::Validator {
            // The name is DERIVED — the network's id with the index appended —
            // rather than assigned, so genesis validators need nothing agreed.
            validation_id: append_index(&network_id, i as u32),
            chain_id: network_id,
            node_id: NodeId(node_id),
            public_key,
            remaining_balance_owner: v.remaining_balance_owner.as_owners().marshal(),
            deactivation_owner: v.deactivation_owner.as_owners().marshal(),
            start_time,
            weight: v.weight,
            min_nonce: 0,
            // A zero balance leaves it inactive.
            end_accumulated_fee: 0,
        };

        if v.balance != 0 {
            // Activating a validator consumes active-set capacity and prepays
            // its fee out of the accrued-fee clock.
            if state.num_active_l1_validators() as u64 >= config.validator_fee.capacity {
                return Err(Error::MaxActiveL1Validators);
            }
            record.end_accumulated_fee = v.balance.checked_add(accrued).ok_or(Error::Overflow)?;
        }
        state.put_l1_validator(record)?;

        data.validators.push(crate::warpmsg::ConversionValidator {
            node_id: v.node_id.clone(),
            bls_public_key: compressed,
            weight: v.weight,
        });
    }

    // The conversion id is the hash of the set as it was established, and it is
    // what every later message about this L1 refers to.
    state.set_conversion(
        network_id,
        crate::state::Conversion {
            conversion_id: data.conversion_id(),
            chain_id: manager_chain_id,
            address: manager_address.to_vec(),
        },
    );
    Ok(())
}

/// Go: `ids.ID.Append`. A validation id derived from the network's id and the
/// validator's position in the set it was born with.
fn append_index(id: &Id, index: u32) -> Id {
    let mut preimage = Vec::with_capacity(36);
    preimage.extend_from_slice(id);
    preimage.extend_from_slice(&index.to_be_bytes());
    crate::ids::hash256(&preimage)
}

/// Go: `verifyL1Conversion`. The message must have come from the chain and the
/// address the conversion recorded — otherwise anyone with a chain could speak
/// for this L1.
fn verify_l1_conversion(
    state: &State,
    chain_id: Id,
    source_chain: Id,
    source_address: &[u8],
) -> Result<(), Error> {
    let conversion = state
        .conversion(&chain_id)
        .map_err(|_| Error::NoConversion(chain_id))?;
    if conversion.chain_id != source_chain || conversion.address != source_address {
        return Err(Error::WrongWarpSource);
    }
    Ok(())
}

/// The three layers a warp-carrying transaction wraps its message in: the
/// signed envelope, the addressed call that says who sent it, and the L1's own
/// message.
fn open_warp_call(raw: &[u8]) -> Result<(crate::warp::Message, crate::warpmsg::Call), Error> {
    let message = crate::warp::Message::parse(raw)?;
    match crate::warpmsg::parse_envelope(&message.unsigned.payload)? {
        crate::warpmsg::Envelope::Call(call) => Ok((message, call)),
        crate::warpmsg::Envelope::Hash(_) => Err(Error::WarpPayload(
            crate::warpmsg::Error::WrongKind,
        )),
    }
}

/// What an L1 validator prepaid and did not spend, back to the owner the
/// registration named, as the output after the transaction's own.
fn refund_remaining_balance(
    state: &mut State,
    config: &Config,
    tx_id: Id,
    own_outputs: usize,
    validator: &crate::l1::Validator,
) -> Result<(), Error> {
    let owner = Owners::unmarshal(&validator.remaining_balance_owner)
        .map_err(|_| Error::CorruptState("the remaining-balance owner is malformed"))?;
    let accrued = state.accrued_fees();
    // Unreachable if the fee state is sound. Kept because the alternative to an
    // impossible refusal here is minting LUX out of a corrupt record.
    if validator.end_accumulated_fee <= accrued {
        return Err(Error::CorruptState(
            "the validator should already have been disabled",
        ));
    }
    state.add_utxo(Utxo {
        id: UtxoId {
            tx_id,
            output_index: own_outputs as u32,
        },
        output: Output {
            asset: config.native_asset,
            stake_lock: 0,
            amount: validator.end_accumulated_fee - accrued,
            owners: owner,
        },
    });
    Ok(())
}

/// Check the aggregate proof on every warp message a transaction carries.
///
/// Go runs this as its own pass (`VerifyWarpMessages`) at the P-Chain height
/// the block is being verified against, because the set that signed has to be
/// the set as it stood then — which is what [`crate::validators::History`]
/// answers. Only the two transactions that carry a message have one to check.
pub fn verify_warp_messages(
    tx: &Unsigned,
    network_id: u32,
    source_set: &crate::warp::Canonical,
) -> Result<(), Error> {
    let raw = match tx {
        Unsigned::RegisterL1Validator { message, .. }
        | Unsigned::SetL1ValidatorWeight { message, .. } => message,
        _ => return Ok(()),
    };
    let message = crate::warp::Message::parse(raw)?;
    crate::warp::verify(
        &message.signature,
        &message.unsigned,
        network_id,
        source_set,
        WARP_QUORUM_NUMERATOR,
        WARP_QUORUM_DENOMINATOR,
    )
    .map_err(Error::Warp)
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
    charge_creds(state, tx, ins, outs, &tx.creds, 0, config, fees)
}

/// The same, with the credentials named explicitly and an amount burnt on top
/// of the fee.
///
/// The credentials are named because a transaction that also authorises a
/// modification carries one more credential than it has inputs, and the last of
/// them is the authorisation rather than a spend. `extra` is what a transaction
/// must burn beyond its fee — an L1 validator's prepaid balance, which is real
/// money that must leave circulation or it would be minted.
#[allow(clippy::too_many_arguments)]
fn charge_creds(
    state: &State,
    tx: &Tx,
    ins: &[crate::components::Input],
    outs: &[Output],
    creds: &[Credential],
    extra: u64,
    config: &Config,
    fees: &dyn Fees,
) -> Result<(), Error> {
    let utxos = utxos_of(state, ins)?;
    charge_utxos(state, tx, &utxos, ins, outs, creds, extra, config, fees)
}

/// The outputs a set of inputs names, from this chain.
fn utxos_of(state: &State, ins: &[crate::components::Input]) -> Result<Vec<Utxo>, Error> {
    let mut utxos = Vec::with_capacity(ins.len());
    for input in ins {
        utxos.push(state.utxo(&input.utxo.input_id())?.clone());
    }
    Ok(utxos)
}

/// The charge itself, over outputs already in hand.
///
/// Separated from the fetch because an import's outputs come from the shared
/// half rather than from this chain, and the arithmetic and the signatures must
/// be the same either way — two spend checks would be two answers to whether
/// value was created.
#[allow(clippy::too_many_arguments)]
fn charge_utxos(
    state: &State,
    tx: &Tx,
    utxos: &[Utxo],
    ins: &[crate::components::Input],
    outs: &[Output],
    creds: &[Credential],
    extra: u64,
    config: &Config,
    fees: &dyn Fees,
) -> Result<(), Error> {
    let fee = fees
        .fee(&tx.unsigned)
        .checked_add(extra)
        .ok_or(Error::Overflow)?;
    let mut fee_map = HashMap::new();
    fee_map.insert(config.native_asset, fee);
    flow::verify_spend(utxos, ins, outs, creds, &fee_map, state.timestamp())?;
    // The arithmetic says the value adds up; this says whose value it was.
    // Both, always, and in one place — an execution path that ran one without
    // the other would let anyone spend anyone's output.
    flow::verify_credentials(utxos, ins, creds, &tx.sighash(), state.timestamp())?;
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
    if staker_tx_id == [0u8; 32] {
        // No transaction is named by the zero id — an id is a hash — so a
        // reward naming it names nothing.
        return Err(Error::InvalidId);
    }
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
        // What a network mints is the network's own schedule, written in the
        // transformation that made it staked. A network that never transformed
        // has no schedule of its own and mints on the primary network's, which
        // is what Go's `GetRewardsCalculator` answers.
        let calculator = reward::Calculator::new(rewards_for(state, config, old.chain));
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

    // A registration nobody issued stops being issuable once its own expiry
    // has passed, and the chain stops having to remember it. Go:
    // `removeStaleExpiries`. The set is in time order, so this is a prefix.
    let stale: Vec<crate::l1::Expiry> = state
        .expiries()
        .take_while(|e| e.timestamp <= new_time)
        .cloned()
        .collect();
    for expiry in &stale {
        state.delete_expiry(expiry);
    }

    // And the clock charges the L1 validators for the time it moved. Go:
    // `advanceValidatorFeeState`.
    let seconds = new_time.saturating_sub(state.timestamp());
    if advance_validator_fees(state, config, seconds)? {
        changed = true;
    }

    state.set_timestamp(new_time);
    Ok(changed)
}

/// Charge every active L1 validator for `seconds`, and drop the ones that can
/// no longer pay.
///
/// This is what makes the L1 plane's balance a *lifetime* rather than a number
/// sitting in a record. A validator pays continuously for the P-chain's trouble
/// in tracking it, and it leaves when the money runs out rather than when a
/// clock strikes — so a chain that never charged would keep every registration
/// alive for ever, free, and the fee that bounds how many there can be would
/// bound nothing.
///
/// The order is the whole trick: validators are walked in increasing
/// `end_accumulated_fee`, so the prefix that can no longer pay is exactly the
/// prefix this deactivates, and the walk stops at the first one that can. That
/// is why a balance is stored as an absolute accrued-fee mark and not as a
/// remaining amount — a remaining amount would have to be decremented for every
/// validator on every tick.
///
/// A deactivated validator is not refunded: it is deactivated *because* there
/// is nothing left. It stays in the set and keeps its weight, because weight
/// nobody can vote toward is still weight a quorum has to beat.
fn advance_validator_fees(
    state: &mut State,
    config: &Config,
    seconds: u64,
) -> Result<bool, Error> {
    let fee_state = crate::l1::FeeState {
        current: state.num_active_l1_validators() as u64,
        excess: state.l1_excess(),
    };
    let cost = fee_state.cost_of(&config.validator_fee, seconds);
    let accrued = state
        .accrued_fees()
        .checked_add(cost)
        .ok_or(Error::Overflow)?;

    let spent: Vec<crate::l1::Validator> = state
        .active_l1_validators()
        .into_iter()
        .take_while(|v| v.end_accumulated_fee <= accrued)
        .cloned()
        .collect();
    let changed = !spent.is_empty();
    for mut validator in spent {
        validator.end_accumulated_fee = 0;
        state.put_l1_validator(validator)?;
    }

    let moved = fee_state.advance_time(config.validator_fee.target, seconds);
    state.set_l1_excess(moved.excess);
    state.set_accrued_fees(accrued);
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
    use crate::txs::{Envelope, NetworkValidator, PChainOwner, Validator};

    const ASSET: Id = [9u8; 32];
    const DAY: u64 = 24 * 60 * 60;
    const YEAR: u64 = 365 * DAY;
    const MEGA: u64 = 1_000_000_000_000;

    fn config() -> Config {
        Config {
            network_id: 1,
            blockchain_id: [3; 32],
            native_asset: ASSET,
            validator_fee: crate::l1::FeeConfig {
                capacity: 20_000,
                target: 10_000,
                min_price: 512,
                excess_conversion_constant: 1_246_488,
            },
            staking: StakingPolicy {
                min_validator_stake: 2 * MEGA,
                max_validator_stake: 3_000 * MEGA,
                min_delegator_stake: 25 * MEGA / 1000,
                min_stake_duration: 2 * 7 * DAY,
                max_stake_duration: YEAR,
                min_delegation_fee: 20_000,
                uptime_requirement: 800_000,
            },
            staking_history: None,
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

    /// The same transaction with its LAST credential made by a stranger.
    ///
    /// The last credential is the one an authorisation is checked against, and
    /// the others are the spend's; forging it is how every "nobody authorised
    /// this" test below is written, so each of them exercises a real recovery
    /// rather than a missing signature.
    fn forge_last_credential(tx: &Tx) -> Tx {
        let stranger = k256::ecdsa::SigningKey::from_bytes(&[0x22u8; 32].into()).unwrap();
        let sighash = tx.sighash();
        let mut creds = tx.creds.clone();
        let last = creds.len() - 1;
        creds[last] = Credential {
            sigs: vec![crate::sign::sign(&stranger, &sighash)],
        };
        Tx::new(tx.unsigned.clone(), creds)
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

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
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

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
            execute_standard(&mut state, &second, &config(), &fees(), &NoImports),
            Err(Error::DuplicateValidator)
        );
    }

    #[test]
    fn a_stake_below_the_floor_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, MEGA);
        let tx = signed(add_validator(5, MEGA, now + YEAR, input, 0), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::WeightTooLarge)
        );
    }

    #[test]
    fn a_term_outside_the_bounds_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let short = signed(add_validator(5, 10 * MEGA, now + DAY, input.clone(), 0), 1);
        assert_eq!(
            execute_standard(&mut state, &short, &config(), &fees(), &NoImports),
            Err(Error::StakeTooShort)
        );

        let long = signed(add_validator(5, 10 * MEGA, now + 2 * YEAR, input, 0), 1);
        assert_eq!(
            execute_standard(&mut state, &long, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &signed(u, 1), &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &signed(v, 1), &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &signed(d, 1), &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &signed(v, 1), &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::WrongTxType(Kind::RewardValidator))
        );
    }

    // ---- delegation ----

    fn with_validator(now: u64, weight: u64, end: u64) -> (State, Tx) {
        let (mut state, input) = funded(now, weight);
        let tx = signed(add_validator(5, weight, end, input, 0), 1);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::PeriodMismatch)
        );
    }

    #[test]
    fn a_delegation_to_nobody_is_refused() {
        let now = 1000;
        let (mut state, input) = funded(now, MEGA);
        let tx = signed(delegate(7, MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &ok, &config(), &fees(), &NoImports),
            Ok(())
        );

        let b = fund_more(&mut state, 3, MEGA);
        let over = signed(delegate(5, MEGA, now + YEAR, b), 1);
        assert_eq!(
            execute_standard(&mut state, &over, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

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
        execute_standard(&mut state, &delegator_tx, &config(), &fees(), &NoImports).unwrap();
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::State(StateError::NotFound))
        );
    }

    #[test]
    fn a_chain_name_may_not_be_taken_twice() {
        let now = 1000;
        let (mut state, input) = funded(now, 100);
        // The network the chains go on, owned by the spender — the one who
        // signs the authorisation credential below.
        state.add_chain([6; 32], spend_owner());
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
                // One credential per input, plus the network owner's.
                2,
            )
        };
        let first = mk(input, "MyChain");
        assert_eq!(
            execute_standard(&mut state, &first, &config(), &fees(), &NoImports),
            Ok(())
        );

        let second_input = fund_more(&mut state, 2, 100);
        let second = mk(second_input, "mychain");
        assert_eq!(
            execute_standard(&mut state, &second, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
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
            execute_standard(&mut state, &over, &config(), &fees(), &NoImports),
            Err(Error::OverDelegated)
        );

        // And a 5 is 50 exactly, which is the ceiling and not past it.
        let input = fund_more(&mut state, 5, 5 * MEGA);
        let ok = signed(delegate(5, 5 * MEGA, now + YEAR, input), 1);
        assert_eq!(
            execute_standard(&mut state, &ok, &config(), &fees(), &NoImports),
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
        assert_eq!(unsigned.syntactic_verify(config().chain()), Ok(()));

        let tx = signed(unsigned, 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::WrongStakedAsset)
        );
    }

    /// A network's staking terms are that network's own.
    ///
    /// They are read out of the transformation the network stated them in.
    /// A network that never stated any admits validators through the L1 plane
    /// instead, and is told so — judging its joiners on the primary network's
    /// terms would admit somebody on rules nobody there agreed to.
    #[test]
    fn a_network_is_not_staked_on_the_primary_networks_terms() {
        let now = 1000;
        let (mut state, input) = funded(now, 10 * MEGA);
        let mut unsigned = add_validator(5, 10 * MEGA, now + YEAR, input, 0);
        if let Unsigned::AddPermissionlessValidator { chain, signer, .. } = &mut unsigned {
            *chain = [6; 32];
            // A network validator registers no key, as the wire requires.
            *signer = crate::signer::Signer::Empty;
        }
        let tx = signed(unsigned, 1);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::NoNetworkTerms)
        );
    }

    /// A reward naming nothing rewards nobody.
    #[test]
    fn a_reward_naming_the_zero_id_is_refused() {
        let now = 1000;
        let (mut state, _) = with_validator(now, 10 * MEGA, now + YEAR);
        let mut on_abort = state.clone();
        let tx = Tx::new(
            Unsigned::RewardValidator {
                staker_tx_id: [0; 32],
            },
            Vec::new(),
        );
        assert_eq!(
            execute_proposal(&tx, &mut state, &mut on_abort),
            Err(Error::InvalidId)
        );
    }

    // ── the authorisation paths, each with a real stranger's signature
    //
    // Every credential this chain checks goes through `flow::verify_permission`,
    // and every "nobody authorised this" test forges the LAST credential with a
    // stranger's real secp256k1 signature rather than removing it — so each
    // exercises a genuine recovery that names the wrong address, which is the
    // failure the original missing-recoverer bug could not have produced.

    #[test]
    fn handing_a_network_on_needs_the_owner_that_holds_it() {
        // The most consequential thing a network's owner can do, so it is the
        // owner that has to sign for it.
        let now = 1000;
        let chain = [6u8; 32];
        let (mut state, input) = funded(now, 100);
        state.add_chain(chain, spend_owner());

        let tx = signed(
            Unsigned::TransferChainOwnership {
                base: envelope(vec![input], vec![]),
                chain,
                chain_auth: vec![0],
                owner: owner(9),
            },
            2,
        );
        assert_eq!(
            execute_standard(&mut state, &forge_last_credential(&tx), &config(), &fees(), &NoImports),
            Err(Error::NotAuthorized(flow::CredentialError::WrongSigner))
        );
        assert_eq!(state.chain_owner(&chain), Ok(&spend_owner()), "and it did not move");

        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Ok(())
        );
        assert_eq!(state.chain_owner(&chain), Ok(&owner(9)));
    }

    #[test]
    fn removing_a_named_validator_needs_the_owner_that_named_it() {
        // A validator admitted by name is removed by the same owner that named
        // it, and by nobody else.
        let now = 1000;
        let chain = [6u8; 32];
        let (mut state, input) = funded(now, 100);
        state.add_chain(chain, spend_owner());
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
                priority: Priority::ChainPermissionedValidatorCurrent,
            })
            .unwrap();

        let tx = signed(
            Unsigned::RemoveChainValidator {
                base: envelope(vec![input], vec![]),
                node_id: NodeId([5; 20]),
                chain,
                chain_auth: vec![0],
            },
            2,
        );
        assert_eq!(
            execute_standard(&mut state, &forge_last_credential(&tx), &config(), &fees(), &NoImports),
            Err(Error::NotAuthorized(flow::CredentialError::WrongSigner))
        );
        assert!(
            state.current_validator(&chain, &NodeId([5; 20])).is_ok(),
            "and it is still validating"
        );

        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Ok(())
        );
        assert!(state.current_validator(&chain, &NodeId([5; 20])).is_err());
    }

    #[test]
    fn making_a_chain_on_a_network_needs_that_networks_owner() {
        let now = 1000;
        let network = [6u8; 32];
        let (mut state, input) = funded(now, 100);
        state.add_chain(network, spend_owner());

        let tx = signed(
            Unsigned::CreateChain {
                base: envelope(vec![input], vec![]),
                chain: network,
                name: "a chain".into(),
                vm_id: [8; 32],
                fx_ids: Vec::new(),
                genesis: vec![1, 2, 3],
                chain_auth: vec![0],
            },
            2,
        );
        assert_eq!(
            execute_standard(&mut state, &forge_last_credential(&tx), &config(), &fees(), &NoImports),
            Err(Error::NotAuthorized(flow::CredentialError::WrongSigner))
        );
        assert!(state.blockchains().is_empty(), "and no chain was made");

        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Ok(())
        );
        assert_eq!(state.blockchains(), &[tx.id()]);
    }

    // ── imports
    //
    // An import spends value made on another chain, so the P-chain cannot check
    // it alone. These exercise both halves of that: what happens when the
    // shared half holds the export, and what happens when it does not.

    /// A shared half holding exactly what was handed over.
    struct Handed(Vec<(UtxoId, Utxo)>);

    impl Atomic for Handed {
        fn imported(&self, _source: &Id, ids: &[UtxoId]) -> Vec<Option<Utxo>> {
            ids.iter()
                .map(|id| {
                    self.0
                        .iter()
                        .find(|(held, _)| held == id)
                        .map(|(_, u)| u.clone())
                })
                .collect()
        }
    }

    fn an_export_from(chain: Id, amount: u64) -> (Input, Utxo) {
        let id = UtxoId {
            tx_id: chain,
            output_index: 0,
        };
        (
            Input {
                utxo: id,
                asset: ASSET,
                stake_lock: 0,
                amount,
                sig_indices: vec![0],
            },
            Utxo {
                id,
                output: funds(amount),
            },
        )
    }

    #[test]
    fn an_import_spends_what_the_other_chain_handed_over() {
        let now = 1000;
        let source = [0x11u8; 32];
        let mut state = State::new();
        state.set_timestamp(now);
        let (input, utxo) = an_export_from(source, 500);

        let tx = signed(
            Unsigned::Import {
                base: envelope(vec![], vec![funds_out(499)]),
                source_chain: source,
                imported: vec![input.clone()],
            },
            1,
        );
        assert_eq!(
            execute_standard(
                &mut state,
                &tx,
                &config(),
                &fees(),
                &Handed(vec![(input.utxo, utxo)])
            ),
            Ok(())
        );
        // The output it made is here. The one it consumed was never here: the
        // node removes that from the shared half when the block is accepted.
        assert!(state
            .utxo(
                &UtxoId {
                    tx_id: tx.id(),
                    output_index: 0
                }
                .input_id()
            )
            .is_ok());
    }

    #[test]
    fn an_import_of_something_nobody_exported_is_refused() {
        // The whole point of asking the shared half. Without it this chain
        // would be minting: the input names value that was never made.
        let now = 1000;
        let source = [0x11u8; 32];
        let mut state = State::new();
        state.set_timestamp(now);
        let (input, _) = an_export_from(source, 500);

        let tx = signed(
            Unsigned::Import {
                base: envelope(vec![], vec![funds_out(499)]),
                source_chain: source,
                imported: vec![input],
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::State(StateError::NotFound))
        );
    }

    #[test]
    fn an_import_nobody_signed_for_is_refused() {
        // The imported inputs go through the same credential check as any
        // other spend — the value was made elsewhere, but whose it is still
        // has to be answered here.
        let now = 1000;
        let source = [0x11u8; 32];
        let mut state = State::new();
        state.set_timestamp(now);
        let (input, utxo) = an_export_from(source, 500);

        let tx = signed(
            Unsigned::Import {
                base: envelope(vec![], vec![funds_out(499)]),
                source_chain: source,
                imported: vec![input.clone()],
            },
            1,
        );
        assert_eq!(
            execute_standard(
                &mut state,
                &forge_last_credential(&tx),
                &config(),
                &fees(),
                &Handed(vec![(input.utxo, utxo)])
            ),
            Err(Error::Credential(flow::CredentialError::WrongSigner))
        );
    }

    #[test]
    fn an_import_may_not_take_out_more_than_was_handed_over() {
        // One spend over both halves: the imported value and this chain's
        // inputs buy the same outputs and pay the same fee, so checking them
        // apart would let either half fund the other.
        let now = 1000;
        let source = [0x11u8; 32];
        let mut state = State::new();
        state.set_timestamp(now);
        let (input, utxo) = an_export_from(source, 500);

        let tx = signed(
            Unsigned::Import {
                base: envelope(vec![], vec![funds_out(600)]),
                source_chain: source,
                imported: vec![input.clone()],
            },
            1,
        );
        assert!(matches!(
            execute_standard(
                &mut state,
                &tx,
                &config(),
                &fees(),
                &Handed(vec![(input.utxo, utxo)])
            ),
            Err(Error::Flow(_))
        ));
    }

    // ── the clock, over the L1 plane

    #[test]
    fn the_clock_charges_an_l1_validator_and_drops_it_when_the_money_runs_out() {
        // What makes the balance a lifetime rather than a number sitting in a
        // record. A chain that never charged would keep every registration
        // alive for ever, free, and the fee that bounds how many there can be
        // would bound nothing.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, tx) = converted(
            now,
            network,
            vec![network_validator(1, 100, 4_096), network_validator(2, 50, 1_000_000)],
            2_000_000,
        );
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        assert_eq!(state.num_active_l1_validators(), 2);

        // Two active validators against a target of 10_000 is far below it, so
        // the price sits at the floor: 512 per second.
        let cheap = state.l1_validators(&network)[0].validation_id;
        let _ = cheap;

        // Eight seconds at the floor is 4_096 — exactly what the first one
        // prepaid, and it is dropped at the mark it can pay UP TO.
        assert_eq!(advance_time_to(&mut state, now + 8, &config()), Ok(true));
        assert_eq!(state.accrued_fees(), 4_096);
        assert_eq!(state.num_active_l1_validators(), 1);

        // The spent one is still in the set, still weighing on it, and cannot
        // be sampled.
        let by_node: std::collections::HashMap<NodeId, crate::l1::Validator> = state
            .l1_validators(&network)
            .into_iter()
            .cloned()
            .map(|v| (v.node_id, v))
            .collect();
        let spent = &by_node[&NodeId([1; 20])];
        assert!(!spent.is_active());
        assert_eq!(spent.weight, 100);
        assert_eq!(spent.effective_node_id(), NodeId::EMPTY);
        // And it is not refunded: it was dropped BECAUSE there is nothing left.
        assert_eq!(spent.end_accumulated_fee, 0);
        // The one that can still pay is untouched.
        assert!(by_node[&NodeId([2; 20])].is_active());
    }

    #[test]
    fn a_clock_that_does_not_move_charges_nothing() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, tx) = converted(now, network, vec![network_validator(1, 100, 4_096)], 10_000);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        advance_time_to(&mut state, now, &config()).unwrap();
        assert_eq!(state.accrued_fees(), 0);
        assert_eq!(state.num_active_l1_validators(), 1);
    }

    #[test]
    fn a_registration_stops_being_remembered_once_it_can_no_longer_be_issued() {
        // The chain remembers a registration only to refuse a replay of it, and
        // a message past its own expiry can never be issued again. Go:
        // `removeStaleExpiries`.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 0, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

        let expiry = crate::l1::Expiry {
            timestamp: now + 60,
            validation_id: msg.validation_id(),
        };
        assert!(state.has_expiry(&expiry));
        advance_time_to(&mut state, now + 59, &config()).unwrap();
        assert!(state.has_expiry(&expiry), "not yet past it");
        advance_time_to(&mut state, now + 60, &config()).unwrap();
        assert!(!state.has_expiry(&expiry), "the moment it passes, it is dropped");
    }

    // ── the sovereign-L1 plane
    //
    // A network is promoted with a set of its own, and from then on that set is
    // changed by messages its own manager signs rather than by the P-Chain
    // owner that made it. This is the birth path every downstream L1 depends
    // on, so each step is exercised end to end: the promotion, a registration,
    // a top-up, a weight change, a removal, and switching one off.

    /// The owner every authorisation in these tests is checked against — the
    /// same key `signed()` signs with, so an authorisation that is checked at
    /// all passes, and one that is checked against a stranger does not.
    fn spendable_l1_owner() -> PChainOwner {
        PChainOwner {
            threshold: 1,
            addresses: spend_owner().addrs.clone(),
        }
    }

    fn network_validator(node: u8, weight: u64, balance: u64) -> NetworkValidator {
        NetworkValidator {
            node_id: vec![node; crate::ids::NODE_ID_LEN],
            weight,
            balance,
            signer: Signer::prove(&bls(node)),
            remaining_balance_owner: spendable_l1_owner(),
            deactivation_owner: spendable_l1_owner(),
        }
    }

    fn contract_managed() -> crate::security::Mode {
        crate::security::Mode {
            restake_parent: false,
            admission: crate::security::Admission::Gated,
            threshold: 0,
            manager: crate::security::Manager::Contract,
        }
    }

    /// The chain and address an L1's manager speaks from.
    const MANAGER_CHAIN: Id = [0x5c; 32];
    const MANAGER_ADDRESS: [u8; 20] = [0x5a; 20];

    /// An empty aggregate. `execute_standard` does not check the aggregate --
    /// Go runs that as its own pass (`VerifyWarpMessages`) against the set as
    /// it stood at the block's height, which is `verify_warp_messages` here --
    /// so these tests carry the envelope's shape and check the executor's own
    /// refusals. The aggregate has its own tests in `warp`.
    fn unsigned_by_nobody() -> crate::warp::BitSet {
        crate::warp::BitSet {
            signers: Vec::new(),
            signature: [0u8; crate::signer::SIGNATURE_LEN],
        }
    }

    /// A message from the L1's manager, wrapped in the two layers the executor
    /// unwraps: the addressed call that says who sent it, and the signed
    /// envelope that says which chain it crossed from.
    fn from_the_manager(payload: &[u8]) -> Vec<u8> {
        let call = crate::warpmsg::Call::build(&MANAGER_ADDRESS, payload);
        let unsigned = crate::warp::Unsigned::build(1, MANAGER_CHAIN, &call.bytes);
        crate::warp::Message::build(&unsigned, &unsigned_by_nobody()).bytes
    }

    /// A network promoted to run its own set, with `validators` in it.
    ///
    /// This is `ConvertNetwork` executed for real, not a state fixture: every
    /// test below starts from a promotion that actually happened.
    fn converted(
        now: u64,
        network: Id,
        validators: Vec<NetworkValidator>,
        funds: u64,
    ) -> (State, Tx) {
        let (mut state, input) = funded(now, funds);
        state.add_chain(network, spend_owner());
        let prepaid: u64 = validators.iter().map(|v| v.balance).sum();
        let change = funds - prepaid - 1;
        let tx = signed(
            Unsigned::ConvertNetwork {
                base: envelope(vec![input], vec![funds_out(change)]),
                network,
                parent: PRIMARY_NETWORK_ID,
                manager_chain_id: MANAGER_CHAIN,
                manager_address: MANAGER_ADDRESS.to_vec(),
                validators,
                auth: vec![0],
                security: contract_managed(),
            },
            2,
        );
        (state, tx)
    }

    /// An output back to the spender, so a transaction's change is spendable
    /// again by the same key.
    fn funds_out(amount: u64) -> Output {
        Output {
            asset: ASSET,
            stake_lock: 0,
            amount,
            owners: spend_owner(),
        }
    }

    #[test]
    fn a_network_is_promoted_to_run_a_set_of_its_own() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, tx) = converted(
            now,
            network,
            vec![network_validator(1, 100, 500), network_validator(2, 50, 0)],
            10_000,
        );
        assert_eq!(execute_standard(&mut state, &tx, &config(), &fees(), &NoImports), Ok(()));

        // Both validators are in the network's own set, named by ids derived
        // from the network's — nothing had to be agreed for them to have names.
        let set = state.l1_validators(&network);
        assert_eq!(set.len(), 2);
        assert_eq!(set[0].chain_id, network);
        let by_node: std::collections::HashMap<NodeId, &crate::l1::Validator> =
            set.iter().map(|v| (v.node_id, *v)).collect();

        // The one that prepaid is active and can be sampled; the one that did
        // not is in the set, weighs on it, and cannot vote.
        let paid = by_node[&NodeId([1; 20])];
        assert!(paid.is_active());
        assert_eq!(paid.weight, 100);
        assert_eq!(paid.end_accumulated_fee, 500);
        let unpaid = by_node[&NodeId([2; 20])];
        assert!(!unpaid.is_active());
        assert_eq!(unpaid.weight, 50);
        assert_eq!(unpaid.effective_node_id(), NodeId::EMPTY);

        // And the authority that may change the set from here on is recorded.
        let conversion = state.conversion(&network).unwrap();
        assert_eq!(conversion.chain_id, MANAGER_CHAIN);
        assert_eq!(conversion.address, MANAGER_ADDRESS.to_vec());
        assert_ne!(conversion.conversion_id, EMPTY);
    }

    #[test]
    fn a_promotion_nobody_authorised_is_refused() {
        // The credential path for ConvertNetwork: the network's existing owner
        // signs, or the promotion does not happen.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, tx) = converted(now, network, vec![network_validator(1, 100, 0)], 10_000);
        let forged = forge_last_credential(&tx);
        assert_eq!(
            execute_standard(&mut state, &forged, &config(), &fees(), &NoImports),
            Err(Error::NotAuthorized(flow::CredentialError::WrongSigner))
        );
        assert!(state.conversion(&network).is_err(), "and nothing was promoted");
    }

    #[test]
    fn a_network_that_has_already_gone_sovereign_is_not_promoted_again() {
        // Go: `verifyPoAChainAuthorization`. Once a network runs its own set,
        // its own rules govern it and the owner that made it no longer speaks
        // for it.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, tx) = converted(now, network, vec![network_validator(1, 100, 0)], 10_000);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

        let (_, again) = converted(now, network, vec![network_validator(3, 100, 0)], 10_000);
        assert_eq!(
            execute_standard(&mut state, &again, &config(), &fees(), &NoImports),
            Err(Error::NetworkIsImmutable)
        );
    }

    /// A network already promoted, with one active validator, and an input to
    /// spend.
    fn an_l1(now: u64, network: Id, funds: u64) -> (State, Input) {
        let (mut state, tx) = converted(now, network, vec![network_validator(1, 100, 500)], funds);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let change = funds - 500 - 1;
        let id = UtxoId {
            tx_id: tx.id(),
            output_index: 0,
        };
        (
            state,
            Input {
                utxo: id,
                asset: ASSET,
                stake_lock: 0,
                amount: change,
                sig_indices: vec![0],
            },
        )
    }

    fn a_registration(now: u64, network: Id, node: u8, weight: u64) -> crate::warpmsg::Register {
        crate::warpmsg::Register::build(
            network,
            &NodeId([node; 20]),
            &Signer::prove(&bls(node)).public_key().unwrap(),
            now + 60,
            &spendable_l1_owner(),
            &spendable_l1_owner(),
            weight,
        )
    }

    fn register_tx(
        now: u64,
        network: Id,
        node: u8,
        weight: u64,
        balance: u64,
        input: Input,
    ) -> (crate::warpmsg::Register, Tx) {
        let msg = a_registration(now, network, node, weight);
        let change = input.amount - balance - 1;
        let proof = match Signer::prove(&bls(node)) {
            Signer::ProofOfPossession { proof, .. } => proof,
            Signer::Empty => unreachable!("a proven signer carries a proof"),
        };
        let tx = signed(
            Unsigned::RegisterL1Validator {
                base: envelope(vec![input], vec![funds_out(change)]),
                balance,
                proof_of_possession: proof,
                message: from_the_manager(&msg.bytes),
            },
            1,
        );
        (msg, tx)
    }

    #[test]
    fn an_l1_admits_a_validator_its_manager_registered() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 300, input);

        assert_eq!(execute_standard(&mut state, &tx, &config(), &fees(), &NoImports), Ok(()));
        let held = state.l1_validator(&msg.validation_id()).unwrap();
        assert_eq!(held.chain_id, network);
        assert_eq!(held.node_id, NodeId([9; 20]));
        assert_eq!(held.weight, 42);
        assert_eq!(held.start_time, now);
        // The balance is stored as the accrued-fee mark it can pay up to.
        assert_eq!(held.end_accumulated_fee, 300);
        assert!(held.is_active());
        // The key it will sign with is the uncompressed one.
        assert_eq!(held.public_key.len(), 96);
    }

    #[test]
    fn the_same_registration_is_not_issued_twice() {
        // The whole replay defence: the chain remembers a registration until
        // the moment it could no longer be issued.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 0, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();

        let replay = Input {
            utxo: UtxoId {
                tx_id: tx.id(),
                output_index: 0,
            },
            asset: ASSET,
            stake_lock: 0,
            amount: 10_000 - 500 - 1 - 1,
            sig_indices: vec![0],
        };
        let (_, again) = register_tx(now, network, 9, 42, 0, replay);
        assert_eq!(
            execute_standard(&mut state, &again, &config(), &fees(), &NoImports),
            Err(Error::WarpMessageAlreadyIssued(msg.validation_id()))
        );
    }

    #[test]
    fn a_registration_from_a_chain_that_does_not_manage_this_l1_is_refused() {
        // Otherwise anyone with a chain of their own could speak for this one.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let msg = a_registration(now, network, 9, 42);
        let call = crate::warpmsg::Call::build(&[0xffu8; 20], &msg.bytes);
        let unsigned = crate::warp::Unsigned::build(1, MANAGER_CHAIN, &call.bytes);
        let tx = signed(
            Unsigned::RegisterL1Validator {
                base: envelope(vec![input], vec![funds_out(9_000)]),
                balance: 0,
                proof_of_possession: match Signer::prove(&bls(9)) {
                    Signer::ProofOfPossession { proof, .. } => proof,
                    Signer::Empty => unreachable!(),
                },
                message: crate::warp::Message::build(
                    &unsigned,
                    &unsigned_by_nobody(),
                )
                .bytes,
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::WrongWarpSource)
        );
    }

    #[test]
    fn a_registration_of_a_key_the_sender_cannot_prove_is_refused() {
        // The message says which key; the transaction proves whoever sent it
        // holds that key. Neither alone is enough — a registration that needed
        // only the message would let anyone enrol somebody else's key and
        // collect the weight against it.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let msg = a_registration(now, network, 9, 42);
        let someone_elses = match Signer::prove(&bls(4)) {
            Signer::ProofOfPossession { proof, .. } => proof,
            Signer::Empty => unreachable!(),
        };
        let tx = signed(
            Unsigned::RegisterL1Validator {
                base: envelope(vec![input], vec![funds_out(9_000)]),
                balance: 0,
                proof_of_possession: someone_elses,
                message: from_the_manager(&msg.bytes),
            },
            1,
        );
        assert!(matches!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::Syntactic(crate::txs::Error::Signer(_)))
        ));
    }

    #[test]
    fn a_registration_nobody_paid_for_is_refused() {
        // The credential path for RegisterL1Validator: the balance and the fee
        // are spent from somewhere, and that spend is signed for.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (_, tx) = register_tx(now, network, 9, 42, 300, input);
        let forged = forge_last_credential(&tx);
        assert_eq!(
            execute_standard(&mut state, &forged, &config(), &fees(), &NoImports),
            Err(Error::Credential(flow::CredentialError::WrongSigner))
        );
    }

    #[test]
    fn a_registration_that_could_be_issued_tomorrow_is_refused() {
        // The chain has to remember a registration until it expires, so how
        // far ahead one may be dated is how much it has to remember.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let msg = crate::warpmsg::Register::build(
            network,
            &NodeId([9; 20]),
            &Signer::prove(&bls(9)).public_key().unwrap(),
            now + REGISTER_EXPIRY_WINDOW + 1,
            &spendable_l1_owner(),
            &spendable_l1_owner(),
            42,
        );
        let tx = signed(
            Unsigned::RegisterL1Validator {
                base: envelope(vec![input], vec![funds_out(9_000)]),
                balance: 0,
                proof_of_possession: match Signer::prove(&bls(9)) {
                    Signer::ProofOfPossession { proof, .. } => proof,
                    Signer::Empty => unreachable!(),
                },
                message: from_the_manager(&msg.bytes),
            },
            1,
        );
        assert!(matches!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::WarpMessageNotYetAllowed { .. })
        ));
    }

    #[test]
    fn topping_up_an_inactive_validator_puts_it_back_in_the_set() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        // Registered with no balance: in the set, weighing on it, unable to
        // vote.
        let (msg, tx) = register_tx(now, network, 9, 42, 0, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let id = msg.validation_id();
        assert!(!state.l1_validator(&id).unwrap().is_active());

        let top_up = signed(
            Unsigned::IncreaseL1ValidatorBalance {
                base: envelope(
                    vec![Input {
                        utxo: UtxoId {
                            tx_id: tx.id(),
                            output_index: 0,
                        },
                        asset: ASSET,
                        stake_lock: 0,
                        amount: 10_000 - 500 - 1 - 1,
                        sig_indices: vec![0],
                    }],
                    vec![funds_out(8_000)],
                ),
                validation_id: id,
                balance: 700,
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &top_up, &config(), &fees(), &NoImports),
            Ok(())
        );
        let held = state.l1_validator(&id).unwrap();
        assert!(held.is_active());
        assert_eq!(held.end_accumulated_fee, 700);
        assert_eq!(held.effective_node_id(), NodeId([9; 20]));
    }

    #[test]
    fn a_top_up_of_a_validator_that_does_not_exist_is_refused() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let tx = signed(
            Unsigned::IncreaseL1ValidatorBalance {
                base: envelope(vec![input], vec![funds_out(9_000)]),
                validation_id: [0xab; 32],
                balance: 100,
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::NoSuchL1Validator([0xab; 32]))
        );
    }

    fn weight_tx(validation_id: Id, nonce: u64, weight: u64, input: Input, change: u64) -> Tx {
        let msg = crate::warpmsg::Weight::build(validation_id, nonce, weight);
        signed(
            Unsigned::SetL1ValidatorWeight {
                base: envelope(vec![input], vec![funds_out(change)]),
                message: from_the_manager(&msg.bytes),
            },
            1,
        )
    }

    #[test]
    fn an_l1_restates_one_of_its_validators_weights() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 300, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let id = msg.validation_id();

        let change = signed(
            Unsigned::SetL1ValidatorWeight {
                base: envelope(
                    vec![Input {
                        utxo: UtxoId {
                            tx_id: tx.id(),
                            output_index: 0,
                        },
                        asset: ASSET,
                        stake_lock: 0,
                        amount: 10_000 - 500 - 1 - 300 - 1,
                        sig_indices: vec![0],
                    }],
                    vec![funds_out(8_000)],
                ),
                message: from_the_manager(&crate::warpmsg::Weight::build(id, 0, 99).bytes),
            },
            1,
        );
        assert_eq!(
            execute_standard(&mut state, &change, &config(), &fees(), &NoImports),
            Ok(())
        );
        let held = state.l1_validator(&id).unwrap();
        assert_eq!(held.weight, 99);
        // The nonce is the replay defence: the next message must carry a
        // larger one.
        assert_eq!(held.min_nonce, 1);
    }

    #[test]
    fn a_weight_message_replayed_over_a_newer_one_is_refused() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 300, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let id = msg.validation_id();

        let spend = |tx_id: Id, amount: u64| Input {
            utxo: UtxoId {
                tx_id,
                output_index: 0,
            },
            asset: ASSET,
            stake_lock: 0,
            amount,
            sig_indices: vec![0],
        };
        let first = weight_tx(id, 5, 99, spend(tx.id(), 10_000 - 500 - 1 - 300 - 1), 8_000);
        execute_standard(&mut state, &first, &config(), &fees(), &NoImports).unwrap();
        assert_eq!(state.l1_validator(&id).unwrap().min_nonce, 6);

        let stale = weight_tx(id, 5, 1, spend(first.id(), 8_000), 7_000);
        assert_eq!(
            execute_standard(&mut state, &stale, &config(), &fees(), &NoImports),
            Err(Error::StaleNonce { given: 5, least: 6 })
        );
    }

    #[test]
    fn the_last_validator_of_an_l1_may_not_be_removed() {
        // A chain with no validators is a chain nobody can ever speak for
        // again.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let only = state.l1_validators(&network)[0].validation_id;
        let tx = weight_tx(only, 0, 0, input, 9_000);
        assert_eq!(
            execute_standard(&mut state, &tx, &config(), &fees(), &NoImports),
            Err(Error::RemovingLastValidator)
        );
    }

    #[test]
    fn removing_a_validator_gives_its_unspent_balance_back() {
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 300, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let id = msg.validation_id();

        let removal = weight_tx(
            id,
            0,
            0,
            Input {
                utxo: UtxoId {
                    tx_id: tx.id(),
                    output_index: 0,
                },
                asset: ASSET,
                stake_lock: 0,
                amount: 10_000 - 500 - 1 - 300 - 1,
                sig_indices: vec![0],
            },
            8_000,
        );
        assert_eq!(
            execute_standard(&mut state, &removal, &config(), &fees(), &NoImports),
            Ok(())
        );
        // Gone from the set...
        assert!(state.l1_validator(&id).is_err());
        // ...and what it prepaid and did not spend came back, as the output
        // after the transaction's own.
        let refund = state
            .utxo(
                &UtxoId {
                    tx_id: removal.id(),
                    output_index: 1,
                }
                .input_id(),
            )
            .expect("the refund");
        assert_eq!(refund.output.amount, 300);
        assert_eq!(refund.output.owners, spend_owner());
    }

    #[test]
    fn switching_a_validator_off_needs_the_owner_that_may_switch_it_off() {
        // The one L1 operation the P-Chain authorises itself, against the
        // deactivation owner the registration named.
        let now = 1000;
        let network = [0x77u8; 32];
        let (mut state, input) = an_l1(now, network, 10_000);
        let (msg, tx) = register_tx(now, network, 9, 42, 300, input);
        execute_standard(&mut state, &tx, &config(), &fees(), &NoImports).unwrap();
        let id = msg.validation_id();

        let disable = signed(
            Unsigned::DisableL1Validator {
                base: envelope(
                    vec![Input {
                        utxo: UtxoId {
                            tx_id: tx.id(),
                            output_index: 0,
                        },
                        asset: ASSET,
                        stake_lock: 0,
                        amount: 10_000 - 500 - 1 - 300 - 1,
                        sig_indices: vec![0],
                    }],
                    vec![funds_out(8_000)],
                ),
                validation_id: id,
                auth: vec![0],
            },
            2,
        );

        // Signed by someone who is not the deactivation owner: refused.
        assert_eq!(
            execute_standard(&mut state, &forge_last_credential(&disable), &config(), &fees(), &NoImports),
            Err(Error::NotAuthorized(flow::CredentialError::WrongSigner))
        );
        assert!(state.l1_validator(&id).unwrap().is_active(), "and it is still on");

        assert_eq!(
            execute_standard(&mut state, &disable, &config(), &fees(), &NoImports),
            Ok(())
        );
        let held = state.l1_validator(&id).unwrap();
        // Off, but still in the set and still weighing on it.
        assert!(!held.is_active());
        assert_eq!(held.weight, 42);
        assert_eq!(held.effective_node_id(), NodeId::EMPTY);
        // And its unspent balance came back.
        let refund = state
            .utxo(
                &UtxoId {
                    tx_id: disable.id(),
                    output_index: 1,
                }
                .input_id(),
            )
            .expect("the refund");
        assert_eq!(refund.output.amount, 300);
    }

}
