// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What someone asks the P-Chain to do.
//!
//! Every transaction is one ZAP object whose first byte says which kind it is.
//! That byte is the whole dispatch: there is no codec, no version, no slot
//! map. The kinds are numbered as Go numbers them, and a number that has
//! stopped naming a kind stays a hole rather than letting the ones after it
//! slide down — a renumbering would silently reinterpret every transaction
//! already on disk.
//!
//! Most kinds share one envelope — which network, which chain, what is spent,
//! what is made — and then add their own fields after it at fixed offsets.
//! Those offsets are consensus, so they are stated once, here, next to the
//! kind that owns them.
//!
//! A signed transaction is the unsigned message's bytes followed by the
//! credential message's bytes. Both are self-delimiting, so the bytes a
//! signature covers are a genuine prefix of the bytes that travel, and the
//! split is found by reading the first message's length rather than by
//! re-encoding anything.

use crate::components::{
    is_sorted_outputs, is_sorted_unique_inputs, read_addrs, slice_addrs, slice_sigs, verify_memo,
    Credential, Input, Output, Owners, UtxoId,
};
use crate::ids::{hash256, Id, NodeId, ShortId, SHORT_ID_LEN, PRIMARY_NETWORK_ID};
use crate::pchain_zap as w;
use crate::signer::Signer;
use lux_zap::zap;

/// Bytes in one secp256k1 signature — the width of a credential's element.
const SIG_LEN: usize = 65;

/// How much of a reward a delegator's fee is measured against.
pub const PERCENT_DENOMINATOR: u32 = 1_000_000;

/// Longest a chain's name may be.
pub const MAX_NAME_LEN: usize = 128;

/// Longest a chain's genesis blob may be.
pub const MAX_GENESIS_LEN: usize = 1 << 20;

/// Which transaction this is.
///
/// The numbers are the wire. Slot 0 names nothing, so a zeroed buffer decodes
/// to no kind; slot 1 named a time-advance transaction no executor would run.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Kind {
    RewardValidator = 2,
    Base = 3,
    Import = 4,
    Export = 5,
    CreateNetwork = 6,
    CreateChain = 7,
    TransferChainOwnership = 8,
    RemoveChainValidator = 9,
    TransformChain = 10,
    AddValidator = 11,
    AddChainValidator = 12,
    AddDelegator = 13,
    AddPermissionlessValidator = 14,
    AddPermissionlessDelegator = 15,
    RegisterL1Validator = 16,
    SetL1ValidatorWeight = 17,
    IncreaseL1ValidatorBalance = 18,
    DisableL1Validator = 19,
    ConvertNetwork = 20,
}

impl Kind {
    fn from_u8(v: u8) -> Option<Kind> {
        Some(match v {
            2 => Kind::RewardValidator,
            3 => Kind::Base,
            4 => Kind::Import,
            5 => Kind::Export,
            6 => Kind::CreateNetwork,
            7 => Kind::CreateChain,
            8 => Kind::TransferChainOwnership,
            9 => Kind::RemoveChainValidator,
            10 => Kind::TransformChain,
            11 => Kind::AddValidator,
            12 => Kind::AddChainValidator,
            13 => Kind::AddDelegator,
            14 => Kind::AddPermissionlessValidator,
            15 => Kind::AddPermissionlessDelegator,
            16 => Kind::RegisterL1Validator,
            17 => Kind::SetL1ValidatorWeight,
            18 => Kind::IncreaseL1ValidatorBalance,
            19 => Kind::DisableL1Validator,
            20 => Kind::ConvertNetwork,
            _ => return None,
        })
    }
}

// ---- the shared envelope ----
//
// The offsets were here. They are in `chains/schema/pchain.zap` now, one
// table for three languages, and `pchain_zap` — the module `zapgen` writes
// out of it — is what this file reads and writes through. A number in this
// file would be a second statement of the format, and two statements of a
// format are two formats.

/// What every spending transaction carries.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Envelope {
    pub network_id: u32,
    pub blockchain_id: Id,
    pub outs: Vec<Output>,
    pub ins: Vec<Input>,
    /// Kept on the wire so old bytes still parse; must be empty.
    pub memo: Vec<u8>,
}

/// The chain a transaction has to be addressed to, and the asset it is
/// denominated in.
///
/// Go hands the same three fields down as a whole `runtime.Runtime`; these are
/// the ones a transaction is checked against. They travel together because a
/// transaction that satisfied two of them and not the third is still a
/// transaction for somewhere else.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Chain {
    pub network_id: u32,
    pub blockchain_id: Id,
    pub native_asset: Id,
}

/// Who is staking, from when until when, with what weight.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Validator {
    pub node_id: NodeId,
    pub start: u64,
    pub end: u64,
    pub weight: u64,
}

impl Validator {
    /// Go's `Validator.Verify`: a validator with no weight is not one.
    pub fn verify(&self) -> Result<(), Error> {
        if self.weight == 0 {
            return Err(Error::WeightTooSmall);
        }
        Ok(())
    }
}

/// True when a staking period sits inside a bound, ends after it starts, and
/// does not stick out either side.
pub fn bounded_by(staker_start: u64, staker_end: u64, lower: u64, upper: u64) -> bool {
    staker_start >= lower && staker_end <= upper && staker_end >= staker_start
}

/// Longest a manager address may be.
pub const MAX_CHAIN_ADDRESS_LEN: usize = 4096;

/// An owner as the L1 messages spell it: a threshold and the addresses it is
/// counted against, and no locktime.
///
/// It is a narrower thing than [`Owners`] on purpose — a balance owner is
/// named in a Warp payload that has no notion of a lock — so the two do not
/// share a type and cannot be passed for one another.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct PChainOwner {
    pub threshold: u32,
    pub addresses: Vec<ShortId>,
}

impl PChainOwner {
    /// The same group, as the shape everything else spends against. The
    /// locktime is zero because a balance owner has none — that is the whole
    /// difference between the two types.
    pub fn as_owners(&self) -> Owners {
        Owners {
            locktime: 0,
            threshold: self.threshold,
            addrs: self.addresses.clone(),
        }
    }

    /// The same check [`Owners`] makes, on the fields this shape has.
    fn verify(&self) -> Result<(), Error> {
        self.as_owners().verify().map_err(Error::Owner)
    }
}

/// A validator a network is born with.
///
/// This is the value both network transactions carry: the node, what it is
/// worth, what it has to spend on its own continued registration, the key it
/// signs with, and the two owners that can reclaim its balance or switch it
/// off.
///
/// `node_id` is a variable-length blob on the wire — it is the raw bytes a
/// Warp payload names a node by — so a length that is not a node id is a thing
/// [`NetworkValidator::verify`] refuses rather than something the reader
/// silently pads.
///
/// The signer slot is 144 unconditional bytes: a public key and a proof, with
/// no tag. [`Signer::Empty`] therefore writes zeros, which is exactly the Go
/// zero value, and which `verify` refuses because zeros are not a curve point.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct NetworkValidator {
    pub node_id: Vec<u8>,
    pub weight: u64,
    pub balance: u64,
    pub signer: Signer,
    pub remaining_balance_owner: PChainOwner,
    pub deactivation_owner: PChainOwner,
}

impl NetworkValidator {
    /// Go's `NetworkValidator.Verify`.
    pub fn verify(&self) -> Result<(), Error> {
        if self.weight == 0 {
            return Err(Error::ZeroWeight);
        }
        if self.node_id.len() != crate::ids::SHORT_ID_LEN {
            return Err(Error::BadNodeIdLength(self.node_id.len()));
        }
        if self.node_id.iter().all(|b| *b == 0) {
            return Err(Error::EmptyNodeId);
        }
        // The slot this signer occupies has no tag: it is a key and a proof,
        // always. So there is no "no signer" to be legal here — an empty one is
        // the zero pair, and the zero pair is not a point on the curve. Judging
        // it as what it will be written as is what makes verification agree
        // with the wire.
        let as_written = match self.signer {
            Signer::Empty => Signer::ProofOfPossession {
                public_key: [0; crate::signer::PUBLIC_KEY_LEN],
                proof: [0; crate::signer::SIGNATURE_LEN],
            },
            other => other,
        };
        as_written.verify().map_err(Error::Signer)?;
        self.remaining_balance_owner.verify()?;
        self.deactivation_owner.verify()
    }

    /// The order a genesis set is written in: by node id, as Go's
    /// `Compare` does.
    fn order_key(&self) -> &[u8] {
        &self.node_id
    }
}

/// Why a transaction is not well formed.
///
/// These are syntactic refusals only — nothing here reads state.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Wire(zap::Error),
    /// A first byte that names no kind.
    UnknownKind(u8),
    /// Addressed to another network. Go: `ErrWrongNetworkID`.
    WrongNetwork { addressed: u32, here: u32 },
    /// Addressed to another chain of this network. Go: `ErrWrongChainID`.
    WrongBlockchain,
    OutputsNotSorted,
    InputsNotSortedUnique,
    Output(crate::components::OutputError),
    Input(crate::components::InputError),
    Owner(crate::components::OwnerError),
    MemoCarried(usize),
    WeightTooSmall,
    /// A delegation fee larger than the whole reward.
    TooManyShares,
    /// The staked outputs do not add up to the declared weight.
    WeightMismatch {
        declared: u64,
        staked: u64,
    },
    /// Something other than the chain's own asset was staked.
    StakeMustBeNativeAsset,
    /// A chain validator naming the primary network.
    BadChainId,
    /// A credential claims signatures the transaction does not carry.
    CredentialRangeOutOfBounds,
    /// A name longer than the chain allows.
    NameTooLong(usize),
    /// The transaction was never given its bytes.
    NotInitialized,
    /// A validator with no node.
    EmptyNodeId,
    /// A staking transaction that stakes nothing.
    NoStake,
    /// Stake in more than one asset. Which asset a network stakes is one
    /// question with one answer.
    MultipleStakedAssets,
    /// A sum that does not fit.
    Overflow,
    /// A proof of possession that is not one.
    Signer(crate::signer::Error),
    /// A BLS key where none belongs, or none where one is required.
    InvalidSigner {
        has_key: bool,
        is_primary: bool,
    },
    /// A genesis validator worth nothing.
    ZeroWeight,
    /// A node id blob that is not a node id.
    BadNodeIdLength(usize),
    /// A manager address longer than a network may name.
    AddressTooLong(usize),
    /// A genesis set that is not in canonical order, or names a node twice.
    ValidatorsNotSortedAndUnique,
    /// A network that neither restakes its parent nor ships a validator to
    /// produce its first block.
    OwnSetMustIncludeValidator,
    /// A network with no set of its own, carrying validators for it.
    NoOwnSetButHasValidators,
    /// A contract-managed set with no contract named.
    ContractManagerNeedsAddress,
    /// The two security axes do not make a network.
    Security(crate::security::Error),
    /// An attempt to convert the primary network.
    ConvertPrimaryNetwork,
    /// A conversion that establishes no validator.
    ConvertMustHaveValidators,
    /// A conversion that leaves the network without a set of its own.
    ConvertMustEstablishOwnSet,
    /// An attempt to transform the primary network.
    CantTransformPrimaryNetwork,
    /// A staking asset that names nothing.
    EmptyAssetId,
    /// A network staking asset that is the chain's own.
    AssetIdCantBeNative,
    InitialSupplyZero,
    InitialSupplyAboveMaximum,
    MinConsumptionRateAboveMax,
    MaxConsumptionRateTooLarge,
    MinValidatorStakeZero,
    MinValidatorStakeAboveSupply,
    MinValidatorStakeAboveMax,
    MaxValidatorStakeAboveSupply,
    MinStakeDurationZero,
    MinStakeDurationAboveMax,
    MinDelegationFeeTooLarge,
    MinDelegatorStakeZero,
    MaxValidatorWeightFactorZero,
    UptimeRequirementTooLarge,
    /// An authorization whose signature indices repeat or run out of order.
    /// Either would let one signature be counted twice toward a threshold.
    AuthIndicesNotSortedUnique,
    /// A blockchain asking to be validated by the primary network.
    CantValidatePrimaryNetwork,
    /// A chain with no VM to run it.
    InvalidVmId,
    FxIdsNotSortedAndUnique,
    GenesisTooLong(usize),
    /// A name with a character outside letters, numbers and the space.
    IllegalNameCharacter(char),
    /// An attempt to remove a primary-network validator by name.
    RemovePrimaryNetworkValidator,
    /// An attempt to transfer ownership of the primary network.
    TransferPermissionlessChain,
    /// A balance increase of nothing.
    ZeroBalance,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Wire(e) => write!(f, "{e}"),
            Error::UnknownKind(k) => write!(f, "zap: unknown tx kind {k}"),
            Error::WrongNetwork { addressed, here } => write!(
                f,
                "the transaction has the wrong network ID: addressed to {addressed}, this is {here}"
            ),
            Error::WrongBlockchain => {
                write!(f, "the transaction has the wrong chain ID")
            }
            Error::OutputsNotSorted => write!(f, "outputs not sorted"),
            Error::InputsNotSortedUnique => write!(f, "inputs not sorted and unique"),
            Error::Output(e) => write!(f, "output failed verification: {e}"),
            Error::Input(e) => write!(f, "input failed verification: {e}"),
            Error::Owner(e) => write!(f, "owner failed verification: {e}"),
            Error::MemoCarried(n) => write!(f, "memo length {n} > 0"),
            Error::WeightTooSmall => write!(f, "weight of this validator is too low"),
            Error::TooManyShares => write!(
                f,
                "a staker can only require at most {PERCENT_DENOMINATOR} shares from delegators"
            ),
            Error::WeightMismatch { declared, staked } => {
                write!(f, "weight {declared} != stake {staked}")
            }
            Error::StakeMustBeNativeAsset => write!(f, "stake must be the native asset"),
            Error::BadChainId => write!(f, "chain ID can't be primary network ID"),
            Error::CredentialRangeOutOfBounds => write!(
                f,
                "credential signature range is outside the signature array"
            ),
            Error::NameTooLong(n) => write!(f, "name length {n} > {MAX_NAME_LEN}"),
            Error::NotInitialized => write!(f, "tx was never initialized and is not valid"),
            Error::EmptyNodeId => write!(f, "validator nodeID cannot be empty"),
            Error::NoStake => write!(f, "there is no stake"),
            Error::MultipleStakedAssets => write!(f, "multiple staked assets"),
            Error::Overflow => write!(f, "a sum does not fit"),
            Error::Signer(e) => write!(f, "{e}"),
            Error::InvalidSigner {
                has_key,
                is_primary,
            } => write!(
                f,
                "invalid signer: hasKey={has_key} != isPrimaryNetwork={is_primary}"
            ),
            Error::ZeroWeight => write!(f, "validator weight must be non-zero"),
            Error::BadNodeIdLength(n) => write!(f, "node id is {n} bytes, not a node id"),
            Error::AddressTooLong(n) => {
                write!(f, "address length {n} > {MAX_CHAIN_ADDRESS_LEN}")
            }
            Error::ValidatorsNotSortedAndUnique => {
                write!(f, "validators must be sorted and unique")
            }
            Error::OwnSetMustIncludeValidator => write!(
                f,
                "sovereign (non-restaking) network must include at least one genesis validator"
            ),
            Error::NoOwnSetButHasValidators => {
                write!(f, "network with no own set must not carry validators")
            }
            Error::ContractManagerNeedsAddress => {
                write!(f, "contract-governed own set requires a manager address")
            }
            Error::Security(e) => write!(f, "{e}"),
            Error::ConvertPrimaryNetwork => write!(f, "cannot convert the primary network"),
            Error::ConvertMustHaveValidators => {
                write!(f, "conversion must establish at least one validator")
            }
            Error::ConvertMustEstablishOwnSet => write!(
                f,
                "conversion must establish an own validator set (sovereign mode)"
            ),
            Error::CantTransformPrimaryNetwork => write!(f, "cannot transform primary network"),
            Error::EmptyAssetId => write!(f, "empty asset ID is not valid"),
            Error::AssetIdCantBeNative => write!(f, "asset ID can't be the native asset"),
            Error::InitialSupplyZero => write!(f, "initial supply must be non-0"),
            Error::InitialSupplyAboveMaximum => {
                write!(f, "initial supply can't be greater than maximum supply")
            }
            Error::MinConsumptionRateAboveMax => write!(
                f,
                "min consumption rate must be less than or equal to max consumption rate"
            ),
            Error::MaxConsumptionRateTooLarge => write!(
                f,
                "max consumption rate must be less than or equal to {PERCENT_DENOMINATOR}"
            ),
            Error::MinValidatorStakeZero => write!(f, "min validator stake must be non-0"),
            Error::MinValidatorStakeAboveSupply => write!(
                f,
                "min validator stake must be less than or equal to initial supply"
            ),
            Error::MinValidatorStakeAboveMax => write!(
                f,
                "min validator stake must be less than or equal to max validator stake"
            ),
            Error::MaxValidatorStakeAboveSupply => write!(
                f,
                "max validator stake must be less than or equal to max supply"
            ),
            Error::MinStakeDurationZero => write!(f, "min stake duration must be non-0"),
            Error::MinStakeDurationAboveMax => write!(
                f,
                "min stake duration must be less than or equal to max stake duration"
            ),
            Error::MinDelegationFeeTooLarge => write!(
                f,
                "min delegation fee must be less than or equal to {PERCENT_DENOMINATOR}"
            ),
            Error::MinDelegatorStakeZero => write!(f, "min delegator stake must be non-0"),
            Error::MaxValidatorWeightFactorZero => {
                write!(f, "max validator weight factor must be non-0")
            }
            Error::UptimeRequirementTooLarge => write!(
                f,
                "uptime requirement must be less than or equal to {PERCENT_DENOMINATOR}"
            ),
            Error::AuthIndicesNotSortedUnique => {
                write!(f, "signature indices are not sorted and unique")
            }
            Error::CantValidatePrimaryNetwork => {
                write!(f, "new blockchain can't be validated by primary network")
            }
            Error::InvalidVmId => write!(f, "invalid VM ID"),
            Error::FxIdsNotSortedAndUnique => {
                write!(f, "feature extensions IDs must be sorted and unique")
            }
            Error::GenesisTooLong(n) => write!(f, "genesis length {n} > {MAX_GENESIS_LEN}"),
            Error::IllegalNameCharacter(c) => write!(f, "illegal name character {c:?}"),
            Error::RemovePrimaryNetworkValidator => {
                write!(f, "can't remove primary network validator")
            }
            Error::TransferPermissionlessChain => {
                write!(f, "can't transfer ownership of the primary network")
            }
            Error::ZeroBalance => write!(f, "balance must be non-zero"),
        }
    }
}

impl std::error::Error for Error {}

impl From<zap::Error> for Error {
    fn from(e: zap::Error) -> Self {
        Error::Wire(e)
    }
}

/// A transaction body.
///
/// One arm per wire kind. A sum rather than a trait object because the
/// executor's whole job is to do a different thing per kind, and a `match`
/// that stops covering one is a compile error rather than a missing method.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Unsigned {
    /// The chain asking itself to retire a staker and pay it. Carries no
    /// envelope: nobody submits it and nobody pays for it.
    RewardValidator {
        staker_tx_id: Id,
    },
    Base(Envelope),
    Import {
        base: Envelope,
        source_chain: Id,
        imported: Vec<Input>,
    },
    Export {
        base: Envelope,
        destination_chain: Id,
        exported: Vec<Output>,
    },
    CreateChain {
        base: Envelope,
        /// The network this chain will run on.
        chain: Id,
        vm_id: Id,
        name: String,
        fx_ids: Vec<Id>,
        genesis: Vec<u8>,
        /// Which of the network owner's addresses authorize this.
        chain_auth: Vec<u32>,
    },
    /// The legacy scheduled validator. Its executor refuses it; the wire is
    /// read because transactions already on disk are still parsed.
    AddValidator {
        base: Envelope,
        validator: Validator,
        stake: Vec<Output>,
        rewards_owner: Owners,
        delegation_shares: u32,
    },
    /// The legacy scheduled delegator. Also refused by the executor.
    AddDelegator {
        base: Envelope,
        validator: Validator,
        stake: Vec<Output>,
        rewards_owner: Owners,
    },
    /// A validator a network's owner admits by name.
    AddChainValidator {
        base: Envelope,
        validator: Validator,
        chain: Id,
        chain_auth: Vec<u32>,
    },
    /// Anyone, with a stake. This is the transaction that makes the set
    /// permissionless: no owner is consulted and no allowlist is read.
    AddPermissionlessValidator {
        base: Envelope,
        validator: Validator,
        chain: Id,
        signer: Signer,
        stake: Vec<Output>,
        validator_rewards_owner: Owners,
        delegator_rewards_owner: Owners,
        delegation_shares: u32,
    },
    AddPermissionlessDelegator {
        base: Envelope,
        validator: Validator,
        chain: Id,
        stake: Vec<Output>,
        rewards_owner: Owners,
    },
    RemoveChainValidator {
        base: Envelope,
        node_id: NodeId,
        chain: Id,
        chain_auth: Vec<u32>,
    },
    TransferChainOwnership {
        base: Envelope,
        chain: Id,
        chain_auth: Vec<u32>,
        owner: Owners,
    },
    IncreaseL1ValidatorBalance {
        base: Envelope,
        validation_id: Id,
        balance: u64,
    },
    DisableL1Validator {
        base: Envelope,
        validation_id: Id,
        auth: Vec<u32>,
    },
    /// The sole network constructor: the ∅ → network birth. It makes a network
    /// at any level of the hierarchy, and the bytes are identical at every
    /// level — only `parent` differs. Chains are not made here;
    /// [`Unsigned::CreateChain`] is the sole chain constructor.
    CreateNetwork {
        base: Envelope,
        /// The parent network. The primary network makes an L1; an L1 makes an
        /// L2; and so on down. Depth is derivable and never stored.
        parent: Id,
        /// Who may later administer the network.
        owner: Owners,
        /// On what terms the network's set is admitted.
        security: crate::security::Mode,
        /// The set the network is born with, in node-id order.
        validators: Vec<NetworkValidator>,
        /// The chain hosting a contract-managed set's staking contract; the
        /// zero id means the P-Chain governs it and the owner is the authority.
        manager_chain_id: Id,
        manager_address: Vec<u8>,
    },
    /// Promotion: network → network. It changes a network's security from
    /// inherited to sovereign and re-anchors its parent — an L2 becoming an
    /// L1. Distinct from [`Unsigned::CreateNetwork`], which makes one.
    ConvertNetwork {
        base: Envelope,
        /// The network being promoted.
        network: Id,
        /// Its new parent.
        parent: Id,
        manager_chain_id: Id,
        manager_address: Vec<u8>,
        validators: Vec<NetworkValidator>,
        /// Which of the existing owner's addresses authorize this.
        auth: Vec<u32>,
        security: crate::security::Mode,
    },
    /// Turning a permissioned network into a staked one: it names the asset
    /// stake is denominated in and every threshold that asset is measured
    /// against.
    TransformChain {
        base: Envelope,
        chain: Id,
        asset_id: Id,
        initial_supply: u64,
        maximum_supply: u64,
        min_consumption_rate: u64,
        max_consumption_rate: u64,
        min_validator_stake: u64,
        max_validator_stake: u64,
        min_stake_duration: u32,
        max_stake_duration: u32,
        min_delegation_fee: u32,
        min_delegator_stake: u64,
        max_validator_weight_factor: u8,
        uptime_requirement: u32,
        chain_auth: Vec<u32>,
    },
    /// A validator joining an L1, carrying the signed message the L1 said it
    /// with and the balance that pays for its continued registration.
    RegisterL1Validator {
        base: Envelope,
        balance: u64,
        /// A proof of possession of the key named inside `message`.
        proof_of_possession: [u8; crate::signer::SIGNATURE_LEN],
        message: Vec<u8>,
    },
    /// An L1 restating one of its validators' weights, in the message its own
    /// validators signed.
    SetL1ValidatorWeight {
        base: Envelope,
        message: Vec<u8>,
    },
}

impl Unsigned {
    pub fn kind(&self) -> Kind {
        match self {
            Unsigned::RewardValidator { .. } => Kind::RewardValidator,
            Unsigned::Base(_) => Kind::Base,
            Unsigned::Import { .. } => Kind::Import,
            Unsigned::Export { .. } => Kind::Export,
            Unsigned::CreateChain { .. } => Kind::CreateChain,
            Unsigned::AddValidator { .. } => Kind::AddValidator,
            Unsigned::AddDelegator { .. } => Kind::AddDelegator,
            Unsigned::AddChainValidator { .. } => Kind::AddChainValidator,
            Unsigned::AddPermissionlessValidator { .. } => Kind::AddPermissionlessValidator,
            Unsigned::AddPermissionlessDelegator { .. } => Kind::AddPermissionlessDelegator,
            Unsigned::RemoveChainValidator { .. } => Kind::RemoveChainValidator,
            Unsigned::TransferChainOwnership { .. } => Kind::TransferChainOwnership,
            Unsigned::IncreaseL1ValidatorBalance { .. } => Kind::IncreaseL1ValidatorBalance,
            Unsigned::DisableL1Validator { .. } => Kind::DisableL1Validator,
            Unsigned::CreateNetwork { .. } => Kind::CreateNetwork,
            Unsigned::ConvertNetwork { .. } => Kind::ConvertNetwork,
            Unsigned::TransformChain { .. } => Kind::TransformChain,
            Unsigned::RegisterL1Validator { .. } => Kind::RegisterL1Validator,
            Unsigned::SetL1ValidatorWeight { .. } => Kind::SetL1ValidatorWeight,
        }
    }

    /// The spending envelope, for every kind that has one.
    pub fn envelope(&self) -> Option<&Envelope> {
        match self {
            Unsigned::RewardValidator { .. } => None,
            Unsigned::Base(b)
            | Unsigned::Import { base: b, .. }
            | Unsigned::Export { base: b, .. }
            | Unsigned::CreateChain { base: b, .. }
            | Unsigned::AddValidator { base: b, .. }
            | Unsigned::AddDelegator { base: b, .. }
            | Unsigned::AddChainValidator { base: b, .. }
            | Unsigned::AddPermissionlessValidator { base: b, .. }
            | Unsigned::AddPermissionlessDelegator { base: b, .. }
            | Unsigned::RemoveChainValidator { base: b, .. }
            | Unsigned::TransferChainOwnership { base: b, .. }
            | Unsigned::IncreaseL1ValidatorBalance { base: b, .. }
            | Unsigned::DisableL1Validator { base: b, .. }
            | Unsigned::CreateNetwork { base: b, .. }
            | Unsigned::ConvertNetwork { base: b, .. }
            | Unsigned::TransformChain { base: b, .. }
            | Unsigned::RegisterL1Validator { base: b, .. }
            | Unsigned::SetL1ValidatorWeight { base: b, .. } => Some(b),
        }
    }

    /// What this transaction spends.
    pub fn inputs(&self) -> &[Input] {
        self.envelope().map(|e| e.ins.as_slice()).unwrap_or(&[])
    }

    /// What this transaction makes, not counting stake and exports.
    pub fn outputs(&self) -> &[Output] {
        self.envelope().map(|e| e.outs.as_slice()).unwrap_or(&[])
    }

    /// The staked outputs, for the kinds that stake.
    pub fn stake(&self) -> &[Output] {
        match self {
            Unsigned::AddValidator { stake, .. }
            | Unsigned::AddDelegator { stake, .. }
            | Unsigned::AddPermissionlessValidator { stake, .. }
            | Unsigned::AddPermissionlessDelegator { stake, .. } => stake,
            _ => &[],
        }
    }

    /// The staker this transaction creates, when it creates one.
    pub fn staker(&self) -> Option<StakerView<'_>> {
        match self {
            Unsigned::AddValidator {
                validator, stake, ..
            } => Some(StakerView {
                validator: *validator,
                chain: PRIMARY_NETWORK_ID,
                signer: None,
                stake,
                priority_current: Priority::PrimaryNetworkValidatorCurrent,
                priority_pending: Priority::PrimaryNetworkValidatorPending,
            }),
            Unsigned::AddDelegator {
                validator, stake, ..
            } => Some(StakerView {
                validator: *validator,
                chain: PRIMARY_NETWORK_ID,
                signer: None,
                stake,
                priority_current: Priority::PrimaryNetworkDelegatorCurrent,
                priority_pending: Priority::PrimaryNetworkDelegatorLegacyPending,
            }),
            Unsigned::AddChainValidator {
                validator, chain, ..
            } => Some(StakerView {
                validator: *validator,
                chain: *chain,
                signer: None,
                stake: &[],
                priority_current: Priority::ChainPermissionedValidatorCurrent,
                priority_pending: Priority::ChainPermissionedValidatorPending,
            }),
            Unsigned::AddPermissionlessValidator {
                validator,
                chain,
                signer,
                stake,
                ..
            } => Some(StakerView {
                validator: *validator,
                chain: *chain,
                signer: signer.public_key(),
                stake,
                priority_current: if *chain == PRIMARY_NETWORK_ID {
                    Priority::PrimaryNetworkValidatorCurrent
                } else {
                    Priority::ChainPermissionlessValidatorCurrent
                },
                priority_pending: if *chain == PRIMARY_NETWORK_ID {
                    Priority::PrimaryNetworkValidatorPending
                } else {
                    Priority::ChainPermissionlessValidatorPending
                },
            }),
            Unsigned::AddPermissionlessDelegator {
                validator,
                chain,
                stake,
                ..
            } => Some(StakerView {
                validator: *validator,
                chain: *chain,
                signer: None,
                stake,
                priority_current: if *chain == PRIMARY_NETWORK_ID {
                    Priority::PrimaryNetworkDelegatorCurrent
                } else {
                    Priority::ChainPermissionlessDelegatorCurrent
                },
                priority_pending: if *chain == PRIMARY_NETWORK_ID {
                    Priority::PrimaryNetworkDelegatorPermissionlessPending
                } else {
                    Priority::ChainPermissionlessDelegatorPending
                },
            }),
            _ => None,
        }
    }

    /// The fee a delegator pays this validator, in millionths.
    pub fn shares(&self) -> Option<u32> {
        match self {
            Unsigned::AddValidator {
                delegation_shares, ..
            }
            | Unsigned::AddPermissionlessValidator {
                delegation_shares, ..
            } => Some(*delegation_shares),
            _ => None,
        }
    }

    /// Where a validator's own reward goes.
    pub fn validation_rewards_owner(&self) -> Option<&Owners> {
        match self {
            Unsigned::AddValidator { rewards_owner, .. } => Some(rewards_owner),
            Unsigned::AddPermissionlessValidator {
                validator_rewards_owner,
                ..
            } => Some(validator_rewards_owner),
            _ => None,
        }
    }

    /// Where the fees a validator takes from its delegators go.
    pub fn delegation_rewards_owner(&self) -> Option<&Owners> {
        match self {
            Unsigned::AddValidator { rewards_owner, .. } => Some(rewards_owner),
            Unsigned::AddPermissionlessValidator {
                delegator_rewards_owner,
                ..
            } => Some(delegator_rewards_owner),
            _ => None,
        }
    }

    /// Where a delegator's own reward goes.
    pub fn delegator_rewards_owner(&self) -> Option<&Owners> {
        match self {
            Unsigned::AddDelegator { rewards_owner, .. }
            | Unsigned::AddPermissionlessDelegator { rewards_owner, .. } => Some(rewards_owner),
            _ => None,
        }
    }
}

/// A staker read off a transaction, without deciding yet whether it is
/// current or pending.
#[derive(Clone, Copy, Debug)]
pub struct StakerView<'a> {
    pub validator: Validator,
    pub chain: Id,
    pub signer: Option<[u8; 48]>,
    pub stake: &'a [Output],
    pub priority_current: Priority,
    pub priority_pending: Priority,
}

/// The order stakers move in and out of the set.
///
/// Numbering is the tie-break when two stakers change at the same second, so
/// these values are consensus. Go states the ordering rules in
/// `txs/priorities.go` and they are reproduced exactly.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(u8)]
pub enum Priority {
    PrimaryNetworkDelegatorLegacyPending = 1,
    PrimaryNetworkValidatorPending = 2,
    PrimaryNetworkDelegatorPermissionlessPending = 3,
    ChainPermissionlessValidatorPending = 4,
    ChainPermissionlessDelegatorPending = 5,
    ChainPermissionedValidatorPending = 6,
    ChainPermissionedValidatorCurrent = 7,
    ChainPermissionlessDelegatorCurrent = 8,
    ChainPermissionlessValidatorCurrent = 9,
    PrimaryNetworkDelegatorCurrent = 10,
    PrimaryNetworkValidatorCurrent = 11,
}

impl Priority {
    pub fn is_validator(self) -> bool {
        self.is_current_validator() || self.is_pending_validator()
    }

    pub fn is_permissioned_validator(self) -> bool {
        matches!(
            self,
            Priority::ChainPermissionedValidatorCurrent
                | Priority::ChainPermissionedValidatorPending
        )
    }

    pub fn is_current_validator(self) -> bool {
        matches!(
            self,
            Priority::PrimaryNetworkValidatorCurrent
                | Priority::ChainPermissionedValidatorCurrent
                | Priority::ChainPermissionlessValidatorCurrent
        )
    }

    pub fn is_current_delegator(self) -> bool {
        matches!(
            self,
            Priority::PrimaryNetworkDelegatorCurrent
                | Priority::ChainPermissionlessDelegatorCurrent
        )
    }

    pub fn is_pending_validator(self) -> bool {
        matches!(
            self,
            Priority::PrimaryNetworkValidatorPending
                | Priority::ChainPermissionedValidatorPending
                | Priority::ChainPermissionlessValidatorPending
        )
    }

    pub fn is_pending_delegator(self) -> bool {
        matches!(
            self,
            Priority::PrimaryNetworkDelegatorPermissionlessPending
                | Priority::PrimaryNetworkDelegatorLegacyPending
                | Priority::ChainPermissionlessDelegatorPending
        )
    }

    /// What a pending staker becomes when its start time arrives.
    pub fn to_current(self) -> Option<Priority> {
        Some(match self {
            Priority::PrimaryNetworkDelegatorLegacyPending => {
                Priority::PrimaryNetworkDelegatorCurrent
            }
            Priority::PrimaryNetworkValidatorPending => Priority::PrimaryNetworkValidatorCurrent,
            Priority::PrimaryNetworkDelegatorPermissionlessPending => {
                Priority::PrimaryNetworkDelegatorCurrent
            }
            Priority::ChainPermissionlessValidatorPending => {
                Priority::ChainPermissionlessValidatorCurrent
            }
            Priority::ChainPermissionlessDelegatorPending => {
                Priority::ChainPermissionlessDelegatorCurrent
            }
            Priority::ChainPermissionedValidatorPending => {
                Priority::ChainPermissionedValidatorCurrent
            }
            _ => return None,
        })
    }
}

// ---- the runs a transaction points at ----
//
// Where the offsets used to be. They are in `chains/schema/pchain.zap` now,
// and `pchain_zap` is what came out of it: a view per kind whose accessors are
// those offsets, and a builder per kind that writes them back. Nothing below
// names a number.

/// The four runs an envelope points at, packed as the elements a builder takes.
///
/// A list element is its own bytes, so `pack_out` and `pack_in` lay one out at
/// the offsets the schema states and the builder copies the run in. An output
/// carries the START and COUNT of its owners in a transaction-wide address
/// array rather than the addresses themselves, which is why packing an output
/// list produces two runs and not one; inputs and their signature indices are
/// the same shape.
#[derive(Default)]
struct Runs {
    outs: Vec<[u8; w::OUT_SIZE]>,
    addrs: Vec<ShortId>,
    ins: Vec<[u8; w::IN_SIZE]>,
    sigs: Vec<u32>,
}

impl Runs {
    /// An output list and the address array its owners index into.
    fn outputs(outs: &[Output]) -> Runs {
        let mut r = Runs::default();
        for o in outs {
            r.outs.push(w::pack_out(&w::OutInput {
                asset: &o.asset,
                stake_lock: o.stake_lock,
                amount: o.amount,
                threshold: o.owners.threshold,
                owner_lock: o.owners.locktime,
                addr_start: r.addrs.len() as u32,
                addr_count: o.owners.addrs.len() as u32,
                ..Default::default()
            }));
            r.addrs.extend_from_slice(&o.owners.addrs);
        }
        r
    }

    /// An input list and the signature-index array its inputs slice.
    fn inputs(ins: &[Input]) -> Runs {
        let mut r = Runs::default();
        for i in ins {
            r.ins.push(w::pack_in(&w::InInput {
                tx_id: &i.utxo.tx_id,
                index: i.utxo.output_index,
                asset: &i.asset,
                stake_lock: i.stake_lock,
                amount: i.amount,
                sig_start: r.sigs.len() as u32,
                sig_count: i.sig_indices.len() as u32,
                ..Default::default()
            }));
            r.sigs.extend_from_slice(&i.sig_indices);
        }
        r
    }

    /// Everything the shared envelope points at.
    fn envelope(base: &Envelope) -> Runs {
        let o = Runs::outputs(&base.outs);
        let i = Runs::inputs(&base.ins);
        Runs {
            outs: o.outs,
            addrs: o.addrs,
            ins: i.ins,
            sigs: i.sigs,
        }
    }

    fn out_bytes(&self) -> Vec<&[u8]> {
        self.outs.iter().map(|e| &e[..]).collect()
    }

    fn in_bytes(&self) -> Vec<&[u8]> {
        self.ins.iter().map(|e| &e[..]).collect()
    }
}

/// Addresses as the flat elements a list of them is made of.
fn flat(addrs: &[ShortId]) -> Vec<[u8; SHORT_ID_LEN]> {
    addrs.iter().map(|a| a.0).collect()
}

/// Ids as the flat elements a list of them is made of.
fn flat_ids(ids: &[Id]) -> Vec<[u8; 32]> {
    ids.to_vec()
}

/// A genesis validator set, packed, with the two pools its entries slice into.
fn pack_network_validators(
    vdrs: &[NetworkValidator],
) -> (Vec<[u8; w::NETWORK_VALIDATOR_SIZE]>, Vec<u8>, Vec<ShortId>) {
    let mut packed = Vec::with_capacity(vdrs.len());
    let mut node_ids: Vec<u8> = Vec::new();
    let mut addrs: Vec<ShortId> = Vec::new();
    for v in vdrs {
        let (key, proof) = match v.signer {
            Signer::ProofOfPossession { public_key, proof } => (public_key, proof),
            Signer::Empty => (
                [0u8; crate::signer::PUBLIC_KEY_LEN],
                [0u8; crate::signer::SIGNATURE_LEN],
            ),
        };
        packed.push(w::pack_network_validator(&w::NetworkValidatorInput {
            weight: v.weight,
            balance: v.balance,
            signer_key: &key,
            signer_proof: &proof,
            node_id_start: node_ids.len() as u32,
            node_id_len: v.node_id.len() as u32,
            remove_threshold: v.remaining_balance_owner.threshold,
            remove_addr_start: addrs.len() as u32,
            remove_addr_count: v.remaining_balance_owner.addresses.len() as u32,
            disable_threshold: v.deactivation_owner.threshold,
            disable_addr_start: (addrs.len() + v.remaining_balance_owner.addresses.len()) as u32,
            disable_addr_count: v.deactivation_owner.addresses.len() as u32,
        }));
        node_ids.extend_from_slice(&v.node_id);
        addrs.extend_from_slice(&v.remaining_balance_owner.addresses);
        addrs.extend_from_slice(&v.deactivation_owner.addresses);
    }
    (packed, node_ids, addrs)
}

// ---- reading ----

/// The eight fields every kind opens with.
///
/// Read through `Base`'s accessors whatever kind the byte at 0 names, because
/// the schema gives every kind those eight at those offsets — which is not a
/// convention to remember but a thing
/// [`the_envelope_is_the_same_eight_fields_in_every_kind`] checks, struct by
/// struct, against the emitted constants.
fn read_envelope(o: zap::Object<'_>) -> Envelope {
    let v = w::Base::new(o);
    Envelope {
        network_id: v.network_id(),
        blockchain_id: *v.blockchain_id(),
        outs: read_outputs(v.outs(), v.owner_addrs()),
        ins: read_inputs(v.ins(), v.sig_indices()),
        memo: v.memo().to_vec(),
    }
}

fn read_outputs(list: zap::List<'_>, addrs: zap::List<'_>) -> Vec<Output> {
    (0..list.len())
        .map(|i| {
            let e = w::Out::new(list.object(i, w::OUT_SIZE));
            Output {
                asset: *e.asset(),
                stake_lock: e.stake_lock(),
                amount: e.amount(),
                owners: Owners {
                    locktime: e.owner_lock(),
                    threshold: e.threshold(),
                    addrs: slice_addrs(addrs, e.addr_start(), e.addr_count()),
                },
            }
        })
        .collect()
}

fn read_inputs(list: zap::List<'_>, sigs: zap::List<'_>) -> Vec<Input> {
    (0..list.len())
        .map(|i| {
            let e = w::In::new(list.object(i, w::IN_SIZE));
            Input {
                utxo: UtxoId {
                    tx_id: *e.tx_id(),
                    output_index: e.index(),
                },
                asset: *e.asset(),
                stake_lock: e.stake_lock(),
                amount: e.amount(),
                sig_indices: slice_sigs(sigs, e.sig_start(), e.sig_count()),
            }
        })
        .collect()
}

fn read_owners(threshold: u32, locktime: u64, addrs: zap::List<'_>) -> Owners {
    Owners {
        locktime,
        threshold,
        addrs: read_addrs(addrs),
    }
}

fn read_u32_list(l: zap::List<'_>) -> Vec<u32> {
    (0..l.len()).map(|i| l.u32(i)).collect()
}

fn read_id_list(l: zap::List<'_>) -> Vec<Id> {
    (0..l.len())
        .map(|i| {
            let mut id = [0u8; 32];
            let src = l.object(i, 32).bytes_fixed(0, 32);
            id[..src.len()].copy_from_slice(src);
            id
        })
        .collect()
}

/// A genesis validator set, read back out of the list and its two pools.
fn read_network_validators(
    list: zap::List<'_>,
    node_ids: &[u8],
    addrs: zap::List<'_>,
) -> Vec<NetworkValidator> {
    (0..list.len())
        .map(|i| {
            let e = w::NetworkValidator::new(list.object(i, w::NETWORK_VALIDATOR_SIZE));
            let (start, len) = (e.node_id_start() as usize, e.node_id_len() as usize);
            // A claimed run that is not inside the pool reads as nothing, for
            // the same reason a short buffer reads as zeros: a hostile
            // transaction has to reach the check that refuses it.
            let node_id = match start.checked_add(len) {
                Some(end) if len > 0 && end <= node_ids.len() => node_ids[start..end].to_vec(),
                _ => Vec::new(),
            };
            NetworkValidator {
                node_id,
                weight: e.weight(),
                balance: e.balance(),
                signer: Signer::ProofOfPossession {
                    public_key: *e.signer_key(),
                    proof: *e.signer_proof(),
                },
                remaining_balance_owner: PChainOwner {
                    threshold: e.remove_threshold(),
                    addresses: slice_addrs(addrs, e.remove_addr_start(), e.remove_addr_count()),
                },
                deactivation_owner: PChainOwner {
                    threshold: e.disable_threshold(),
                    addresses: slice_addrs(addrs, e.disable_addr_start(), e.disable_addr_count()),
                },
            }
        })
        .collect()
}

/// The two security axes, out of the four bytes that carry them.
///
/// A byte that names no admission or manager is carried as the refusal it will
/// become: the mode is returned with the closest safe reading and
/// [`crate::security::Mode::valid`] is what refuses it, exactly as Go leaves
/// the check to `Mode.Valid`. Returning an error here instead would make a
/// malformed byte unparseable rather than invalid, and a transaction that
/// cannot be parsed cannot be reported on.
fn read_security(
    restake_parent: u8,
    admission: u8,
    manager: u8,
    threshold: u64,
) -> Result<crate::security::Mode, Error> {
    let admission = crate::security::Admission::from_u8(admission)
        .map_err(|v| Error::Security(crate::security::Error::UnknownAdmission(v)))?;
    let manager = crate::security::Manager::from_u8(manager)
        .map_err(|v| Error::Security(crate::security::Error::UnknownManager(v)))?;
    Ok(crate::security::Mode {
        restake_parent: restake_parent != 0,
        admission,
        manager,
        threshold,
    })
}

impl Unsigned {
    /// The transaction's bytes.
    ///
    /// This is construction, not serialization: nothing is cached and nothing
    /// is re-encoded later. The bytes a signature covers are these.
    ///
    /// One arm per kind, and each arm is the schema's field list read straight
    /// down. What a field points at is written before the fixed section, in
    /// field order, which is the order the Go chain writes it in — so these
    /// bytes and a Go peer's are the same bytes, which is what
    /// `tests/corpus_bytes.rs` checks against the corpus the Go chain
    /// generated.
    pub fn to_bytes(&self) -> Vec<u8> {
        match self {
            Unsigned::RewardValidator { staker_tx_id } => {
                w::new_reward_validator(&w::RewardValidatorInput {
                    kind: Kind::RewardValidator as u8,
                    staker_tx_id,
                })
            }
            Unsigned::Base(base) => {
                let r = Runs::envelope(base);
                w::new_base(&w::BaseInput {
                    kind: Kind::Base as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                })
            }
            Unsigned::Import {
                base,
                source_chain,
                imported,
            } => {
                let r = Runs::envelope(base);
                let i = Runs::inputs(imported);
                w::new_import(&w::ImportInput {
                    kind: Kind::Import as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    source: source_chain,
                    imported: &i.in_bytes(),
                    imported_sigs: &i.sigs,
                })
            }
            Unsigned::Export {
                base,
                destination_chain,
                exported,
            } => {
                let r = Runs::envelope(base);
                let e = Runs::outputs(exported);
                w::new_export(&w::ExportInput {
                    kind: Kind::Export as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    destination: destination_chain,
                    exported: &e.out_bytes(),
                    exported_addrs: &flat(&e.addrs),
                })
            }
            Unsigned::CreateChain {
                base,
                chain,
                vm_id,
                name,
                fx_ids,
                genesis,
                chain_auth,
            } => {
                let r = Runs::envelope(base);
                w::new_create_chain(&w::CreateChainInput {
                    kind: Kind::CreateChain as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    chain,
                    vm_id,
                    name: name.as_bytes(),
                    fx_ids: &flat_ids(fx_ids),
                    genesis,
                    auth: chain_auth,
                })
            }
            Unsigned::AddValidator {
                base,
                validator,
                stake,
                rewards_owner,
                delegation_shares,
            } => {
                let r = Runs::envelope(base);
                let s = Runs::outputs(stake);
                w::new_add_validator(&w::AddValidatorInput {
                    kind: Kind::AddValidator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &validator.node_id.0,
                    start: validator.start,
                    end: validator.end,
                    weight: validator.weight,
                    stake_outs: &s.out_bytes(),
                    stake_addrs: &flat(&s.addrs),
                    rewards_threshold: rewards_owner.threshold,
                    rewards_locktime: rewards_owner.locktime,
                    rewards_addrs: &flat(&rewards_owner.addrs),
                    delegation_shares: *delegation_shares,
                })
            }
            Unsigned::AddDelegator {
                base,
                validator,
                stake,
                rewards_owner,
            } => {
                let r = Runs::envelope(base);
                let s = Runs::outputs(stake);
                w::new_add_delegator(&w::AddDelegatorInput {
                    kind: Kind::AddDelegator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &validator.node_id.0,
                    start: validator.start,
                    end: validator.end,
                    weight: validator.weight,
                    stake_outs: &s.out_bytes(),
                    stake_addrs: &flat(&s.addrs),
                    rewards_threshold: rewards_owner.threshold,
                    rewards_locktime: rewards_owner.locktime,
                    rewards_addrs: &flat(&rewards_owner.addrs),
                })
            }
            Unsigned::AddChainValidator {
                base,
                validator,
                chain,
                chain_auth,
            } => {
                let r = Runs::envelope(base);
                w::new_add_chain_validator(&w::AddChainValidatorInput {
                    kind: Kind::AddChainValidator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &validator.node_id.0,
                    start: validator.start,
                    end: validator.end,
                    weight: validator.weight,
                    chain,
                    auth: chain_auth,
                })
            }
            Unsigned::AddPermissionlessValidator {
                base,
                validator,
                chain,
                signer,
                stake,
                validator_rewards_owner,
                delegator_rewards_owner,
                delegation_shares,
            } => {
                let r = Runs::envelope(base);
                let s = Runs::outputs(stake);
                let (kind, key, proof) = signer.parts();
                w::new_add_permissionless_validator(&w::AddPermissionlessValidatorInput {
                    kind: Kind::AddPermissionlessValidator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &validator.node_id.0,
                    start: validator.start,
                    end: validator.end,
                    weight: validator.weight,
                    chain,
                    signer_kind: kind,
                    signer_key: &key,
                    signer_proof: &proof,
                    stake_outs: &s.out_bytes(),
                    stake_addrs: &flat(&s.addrs),
                    validator_rewards_threshold: validator_rewards_owner.threshold,
                    validator_rewards_locktime: validator_rewards_owner.locktime,
                    validator_rewards_addrs: &flat(&validator_rewards_owner.addrs),
                    delegator_rewards_threshold: delegator_rewards_owner.threshold,
                    delegator_rewards_locktime: delegator_rewards_owner.locktime,
                    delegator_rewards_addrs: &flat(&delegator_rewards_owner.addrs),
                    delegation_shares: *delegation_shares,
                })
            }
            Unsigned::AddPermissionlessDelegator {
                base,
                validator,
                chain,
                stake,
                rewards_owner,
            } => {
                let r = Runs::envelope(base);
                let s = Runs::outputs(stake);
                w::new_add_permissionless_delegator(&w::AddPermissionlessDelegatorInput {
                    kind: Kind::AddPermissionlessDelegator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &validator.node_id.0,
                    start: validator.start,
                    end: validator.end,
                    weight: validator.weight,
                    chain,
                    stake_outs: &s.out_bytes(),
                    stake_addrs: &flat(&s.addrs),
                    rewards_threshold: rewards_owner.threshold,
                    rewards_locktime: rewards_owner.locktime,
                    rewards_addrs: &flat(&rewards_owner.addrs),
                })
            }
            Unsigned::RemoveChainValidator {
                base,
                node_id,
                chain,
                chain_auth,
            } => {
                let r = Runs::envelope(base);
                w::new_remove_chain_validator(&w::RemoveChainValidatorInput {
                    kind: Kind::RemoveChainValidator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    node_id: &node_id.0,
                    chain,
                    auth: chain_auth,
                })
            }
            Unsigned::TransferChainOwnership {
                base,
                chain,
                chain_auth,
                owner,
            } => {
                let r = Runs::envelope(base);
                w::new_transfer_chain_ownership(&w::TransferChainOwnershipInput {
                    kind: Kind::TransferChainOwnership as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    chain,
                    auth: chain_auth,
                    new_owner_threshold: owner.threshold,
                    new_owner_locktime: owner.locktime,
                    new_owner_addrs: &flat(&owner.addrs),
                })
            }
            Unsigned::IncreaseL1ValidatorBalance {
                base,
                validation_id,
                balance,
            } => {
                let r = Runs::envelope(base);
                w::new_increase_l1_validator_balance(&w::IncreaseL1ValidatorBalanceInput {
                    kind: Kind::IncreaseL1ValidatorBalance as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    validation_id,
                    balance: *balance,
                })
            }
            Unsigned::DisableL1Validator {
                base,
                validation_id,
                auth,
            } => {
                let r = Runs::envelope(base);
                w::new_disable_l1_validator(&w::DisableL1ValidatorInput {
                    kind: Kind::DisableL1Validator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    validation_id,
                    auth,
                })
            }
            Unsigned::CreateNetwork {
                base,
                parent,
                owner,
                security,
                validators,
                manager_chain_id,
                manager_address,
            } => {
                let r = Runs::envelope(base);
                let (vdrs, node_ids, pool) = pack_network_validators(validators);
                w::new_create_network(&w::CreateNetworkInput {
                    kind: Kind::CreateNetwork as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    parent,
                    network_owner_threshold: owner.threshold,
                    network_owner_locktime: owner.locktime,
                    network_owner_addrs: &flat(&owner.addrs),
                    restake_parent: u8::from(security.restake_parent),
                    admission: security.admission as u8,
                    manager: security.manager as u8,
                    threshold: security.threshold,
                    validators: &vdrs.iter().map(|e| &e[..]).collect::<Vec<_>>(),
                    node_id_pool: &node_ids,
                    addr_pool: &flat(&pool),
                    manager_chain_id,
                    manager_address,
                })
            }
            Unsigned::ConvertNetwork {
                base,
                network,
                parent,
                manager_chain_id,
                manager_address,
                validators,
                auth,
                security,
            } => {
                let r = Runs::envelope(base);
                let (vdrs, node_ids, pool) = pack_network_validators(validators);
                w::new_convert_network(&w::ConvertNetworkInput {
                    kind: Kind::ConvertNetwork as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    network,
                    parent,
                    manager_chain_id,
                    manager_address,
                    validators: &vdrs.iter().map(|e| &e[..]).collect::<Vec<_>>(),
                    node_id_pool: &node_ids,
                    addr_pool: &flat(&pool),
                    auth,
                    restake_parent: u8::from(security.restake_parent),
                    admission: security.admission as u8,
                    manager: security.manager as u8,
                    threshold: security.threshold,
                })
            }
            Unsigned::TransformChain {
                base,
                chain,
                asset_id,
                initial_supply,
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
                chain_auth,
            } => {
                let r = Runs::envelope(base);
                w::new_transform_chain(&w::TransformChainInput {
                    kind: Kind::TransformChain as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    chain,
                    asset: asset_id,
                    initial_supply: *initial_supply,
                    maximum_supply: *maximum_supply,
                    min_consumption_rate: *min_consumption_rate,
                    max_consumption_rate: *max_consumption_rate,
                    min_validator_stake: *min_validator_stake,
                    max_validator_stake: *max_validator_stake,
                    min_stake_duration: *min_stake_duration,
                    max_stake_duration: *max_stake_duration,
                    min_delegation_fee: *min_delegation_fee,
                    min_delegator_stake: *min_delegator_stake,
                    max_validator_weight_factor: *max_validator_weight_factor,
                    uptime_requirement: *uptime_requirement,
                    auth: chain_auth,
                })
            }
            Unsigned::RegisterL1Validator {
                base,
                balance,
                proof_of_possession,
                message,
            } => {
                let r = Runs::envelope(base);
                w::new_register_l1_validator(&w::RegisterL1ValidatorInput {
                    kind: Kind::RegisterL1Validator as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    balance: *balance,
                    proof: proof_of_possession,
                    message,
                })
            }
            Unsigned::SetL1ValidatorWeight { base, message } => {
                let r = Runs::envelope(base);
                w::new_set_l1_validator_weight(&w::SetL1ValidatorWeightInput {
                    kind: Kind::SetL1ValidatorWeight as u8,
                    network_id: base.network_id,
                    blockchain_id: &base.blockchain_id,
                    outs: &r.out_bytes(),
                    owner_addrs: &flat(&r.addrs),
                    ins: &r.in_bytes(),
                    sig_indices: &r.sigs,
                    memo: &base.memo,
                    message,
                })
            }
        }
    }

    /// Read a transaction body off the wire.
    ///
    /// A kind this port does not read is refused by name. It is never guessed
    /// at and never treated as a kind it resembles: a transaction whose rules
    /// nobody here has written is one nobody here may execute.
    pub fn parse(bytes: &[u8]) -> Result<Unsigned, Error> {
        let msg = zap::Message::parse(bytes)?;
        let o = msg.root();
        let raw = o.u8(w::BASE_KIND);
        let kind = Kind::from_u8(raw).ok_or(Error::UnknownKind(raw))?;
        Ok(match kind {
            Kind::RewardValidator => {
                let v = w::RewardValidator::new(o);
                Unsigned::RewardValidator {
                    staker_tx_id: *v.staker_tx_id(),
                }
            }
            Kind::Base => Unsigned::Base(read_envelope(o)),
            Kind::Import => {
                let v = w::Import::new(o);
                Unsigned::Import {
                    base: read_envelope(o),
                    source_chain: *v.source(),
                    imported: read_inputs(v.imported(), v.imported_sigs()),
                }
            }
            Kind::Export => {
                let v = w::Export::new(o);
                Unsigned::Export {
                    base: read_envelope(o),
                    destination_chain: *v.destination(),
                    exported: read_outputs(v.exported(), v.exported_addrs()),
                }
            }
            Kind::CreateChain => {
                let v = w::CreateChain::new(o);
                Unsigned::CreateChain {
                    base: read_envelope(o),
                    chain: *v.chain(),
                    vm_id: *v.vm_id(),
                    name: String::from_utf8_lossy(v.name()).into_owned(),
                    fx_ids: read_id_list(v.fx_ids()),
                    genesis: v.genesis().to_vec(),
                    chain_auth: read_u32_list(v.auth()),
                }
            }
            Kind::AddValidator => {
                let v = w::AddValidator::new(o);
                Unsigned::AddValidator {
                    base: read_envelope(o),
                    validator: Validator {
                        node_id: NodeId(*v.node_id()),
                        start: v.start(),
                        end: v.end(),
                        weight: v.weight(),
                    },
                    stake: read_outputs(v.stake_outs(), v.stake_addrs()),
                    rewards_owner: read_owners(
                        v.rewards_threshold(),
                        v.rewards_locktime(),
                        v.rewards_addrs(),
                    ),
                    delegation_shares: v.delegation_shares(),
                }
            }
            Kind::AddDelegator => {
                let v = w::AddDelegator::new(o);
                Unsigned::AddDelegator {
                    base: read_envelope(o),
                    validator: Validator {
                        node_id: NodeId(*v.node_id()),
                        start: v.start(),
                        end: v.end(),
                        weight: v.weight(),
                    },
                    stake: read_outputs(v.stake_outs(), v.stake_addrs()),
                    rewards_owner: read_owners(
                        v.rewards_threshold(),
                        v.rewards_locktime(),
                        v.rewards_addrs(),
                    ),
                }
            }
            Kind::AddChainValidator => {
                let v = w::AddChainValidator::new(o);
                Unsigned::AddChainValidator {
                    base: read_envelope(o),
                    validator: Validator {
                        node_id: NodeId(*v.node_id()),
                        start: v.start(),
                        end: v.end(),
                        weight: v.weight(),
                    },
                    chain: *v.chain(),
                    chain_auth: read_u32_list(v.auth()),
                }
            }
            Kind::AddPermissionlessValidator => {
                let v = w::AddPermissionlessValidator::new(o);
                Unsigned::AddPermissionlessValidator {
                    base: read_envelope(o),
                    validator: Validator {
                        node_id: NodeId(*v.node_id()),
                        start: v.start(),
                        end: v.end(),
                        weight: v.weight(),
                    },
                    chain: *v.chain(),
                    signer: Signer::of(v.signer_kind(), v.signer_key(), v.signer_proof()),
                    stake: read_outputs(v.stake_outs(), v.stake_addrs()),
                    validator_rewards_owner: read_owners(
                        v.validator_rewards_threshold(),
                        v.validator_rewards_locktime(),
                        v.validator_rewards_addrs(),
                    ),
                    delegator_rewards_owner: read_owners(
                        v.delegator_rewards_threshold(),
                        v.delegator_rewards_locktime(),
                        v.delegator_rewards_addrs(),
                    ),
                    delegation_shares: v.delegation_shares(),
                }
            }
            Kind::AddPermissionlessDelegator => {
                let v = w::AddPermissionlessDelegator::new(o);
                Unsigned::AddPermissionlessDelegator {
                    base: read_envelope(o),
                    validator: Validator {
                        node_id: NodeId(*v.node_id()),
                        start: v.start(),
                        end: v.end(),
                        weight: v.weight(),
                    },
                    chain: *v.chain(),
                    stake: read_outputs(v.stake_outs(), v.stake_addrs()),
                    rewards_owner: read_owners(
                        v.rewards_threshold(),
                        v.rewards_locktime(),
                        v.rewards_addrs(),
                    ),
                }
            }
            Kind::RemoveChainValidator => {
                let v = w::RemoveChainValidator::new(o);
                Unsigned::RemoveChainValidator {
                    base: read_envelope(o),
                    node_id: NodeId(*v.node_id()),
                    chain: *v.chain(),
                    chain_auth: read_u32_list(v.auth()),
                }
            }
            Kind::TransferChainOwnership => {
                let v = w::TransferChainOwnership::new(o);
                Unsigned::TransferChainOwnership {
                    base: read_envelope(o),
                    chain: *v.chain(),
                    chain_auth: read_u32_list(v.auth()),
                    owner: read_owners(
                        v.new_owner_threshold(),
                        v.new_owner_locktime(),
                        v.new_owner_addrs(),
                    ),
                }
            }
            Kind::IncreaseL1ValidatorBalance => {
                let v = w::IncreaseL1ValidatorBalance::new(o);
                Unsigned::IncreaseL1ValidatorBalance {
                    base: read_envelope(o),
                    validation_id: *v.validation_id(),
                    balance: v.balance(),
                }
            }
            Kind::DisableL1Validator => {
                let v = w::DisableL1Validator::new(o);
                Unsigned::DisableL1Validator {
                    base: read_envelope(o),
                    validation_id: *v.validation_id(),
                    auth: read_u32_list(v.auth()),
                }
            }
            Kind::CreateNetwork => {
                let v = w::CreateNetwork::new(o);
                Unsigned::CreateNetwork {
                    base: read_envelope(o),
                    parent: *v.parent(),
                    owner: read_owners(
                        v.network_owner_threshold(),
                        v.network_owner_locktime(),
                        v.network_owner_addrs(),
                    ),
                    security: read_security(
                        v.restake_parent(),
                        v.admission(),
                        v.manager(),
                        v.threshold(),
                    )?,
                    validators: read_network_validators(
                        v.validators(),
                        v.node_id_pool(),
                        v.addr_pool(),
                    ),
                    manager_chain_id: *v.manager_chain_id(),
                    manager_address: v.manager_address().to_vec(),
                }
            }
            Kind::ConvertNetwork => {
                let v = w::ConvertNetwork::new(o);
                Unsigned::ConvertNetwork {
                    base: read_envelope(o),
                    network: *v.network(),
                    parent: *v.parent(),
                    manager_chain_id: *v.manager_chain_id(),
                    manager_address: v.manager_address().to_vec(),
                    validators: read_network_validators(
                        v.validators(),
                        v.node_id_pool(),
                        v.addr_pool(),
                    ),
                    auth: read_u32_list(v.auth()),
                    security: read_security(
                        v.restake_parent(),
                        v.admission(),
                        v.manager(),
                        v.threshold(),
                    )?,
                }
            }
            Kind::TransformChain => {
                let v = w::TransformChain::new(o);
                Unsigned::TransformChain {
                    base: read_envelope(o),
                    chain: *v.chain(),
                    asset_id: *v.asset(),
                    initial_supply: v.initial_supply(),
                    maximum_supply: v.maximum_supply(),
                    min_consumption_rate: v.min_consumption_rate(),
                    max_consumption_rate: v.max_consumption_rate(),
                    min_validator_stake: v.min_validator_stake(),
                    max_validator_stake: v.max_validator_stake(),
                    min_stake_duration: v.min_stake_duration(),
                    max_stake_duration: v.max_stake_duration(),
                    min_delegation_fee: v.min_delegation_fee(),
                    min_delegator_stake: v.min_delegator_stake(),
                    max_validator_weight_factor: v.max_validator_weight_factor(),
                    uptime_requirement: v.uptime_requirement(),
                    chain_auth: read_u32_list(v.auth()),
                }
            }
            Kind::RegisterL1Validator => {
                let v = w::RegisterL1Validator::new(o);
                Unsigned::RegisterL1Validator {
                    base: read_envelope(o),
                    balance: v.balance(),
                    proof_of_possession: *v.proof(),
                    message: v.message().to_vec(),
                }
            }
            Kind::SetL1ValidatorWeight => {
                let v = w::SetL1ValidatorWeight::new(o);
                Unsigned::SetL1ValidatorWeight {
                    base: read_envelope(o),
                    message: v.message().to_vec(),
                }
            }
        })
    }

    /// The checks that need no state.
    ///
    /// Everything here is a property of the bytes: value is present, order is
    /// canonical, weights add up. Anything that needs to know what the chain
    /// currently believes belongs in the executor, not here.
    pub fn syntactic_verify(&self, chain: Chain) -> Result<(), Error> {
        let native_asset = chain.native_asset;
        if let Some(base) = self.envelope() {
            verify_envelope(base, chain)?;
        }
        match self {
            Unsigned::RewardValidator { .. } | Unsigned::Base(_) => Ok(()),
            Unsigned::Import { imported, .. } => {
                for i in imported {
                    i.verify().map_err(Error::Input)?;
                }
                if !is_sorted_unique_inputs(imported) {
                    return Err(Error::InputsNotSortedUnique);
                }
                Ok(())
            }
            Unsigned::Export { exported, .. } => {
                for o in exported {
                    o.verify().map_err(Error::Output)?;
                }
                if !is_sorted_outputs(exported) {
                    return Err(Error::OutputsNotSorted);
                }
                Ok(())
            }
            Unsigned::CreateChain {
                chain,
                vm_id,
                name,
                fx_ids,
                genesis,
                chain_auth,
                ..
            } => {
                if *chain == PRIMARY_NETWORK_ID {
                    return Err(Error::CantValidatePrimaryNetwork);
                }
                if name.len() > MAX_NAME_LEN {
                    return Err(Error::NameTooLong(name.len()));
                }
                if *vm_id == [0u8; 32] {
                    return Err(Error::InvalidVmId);
                }
                if !crate::components::is_sorted_unique(fx_ids) {
                    return Err(Error::FxIdsNotSortedAndUnique);
                }
                if genesis.len() > MAX_GENESIS_LEN {
                    return Err(Error::GenesisTooLong(genesis.len()));
                }
                // A name is read by people, in whatever a terminal or a
                // browser makes of it. Confining it to letters, digits and the
                // space is what stops a chain from being named something that
                // renders as another chain's name.
                for c in name.chars() {
                    if !c.is_ascii() || !(c.is_alphanumeric() || c == ' ') {
                        return Err(Error::IllegalNameCharacter(c));
                    }
                }
                verify_auth(chain_auth)
            }
            Unsigned::AddValidator {
                validator,
                stake,
                rewards_owner,
                delegation_shares,
                ..
            } => {
                if *delegation_shares > PERCENT_DENOMINATOR {
                    return Err(Error::TooManyShares);
                }
                validator.verify()?;
                rewards_owner.verify().map_err(Error::Owner)?;
                verify_stake(stake, validator.weight, Some(native_asset))
            }
            Unsigned::AddDelegator {
                validator,
                stake,
                rewards_owner,
                ..
            } => {
                validator.verify()?;
                rewards_owner.verify().map_err(Error::Owner)?;
                verify_stake(stake, validator.weight, Some(native_asset))
            }
            Unsigned::AddChainValidator {
                validator,
                chain,
                chain_auth,
                ..
            } => {
                if *chain == PRIMARY_NETWORK_ID {
                    return Err(Error::BadChainId);
                }
                validator.verify()?;
                verify_auth(chain_auth)
            }
            Unsigned::AddPermissionlessValidator {
                validator,
                chain,
                signer,
                stake,
                validator_rewards_owner,
                delegator_rewards_owner,
                delegation_shares,
                ..
            } => {
                if validator.node_id.is_empty() {
                    return Err(Error::EmptyNodeId);
                }
                if stake.is_empty() {
                    return Err(Error::NoStake);
                }
                if *delegation_shares > PERCENT_DENOMINATOR {
                    return Err(Error::TooManyShares);
                }
                validator.verify()?;
                signer.verify().map_err(Error::Signer)?;
                validator_rewards_owner.verify().map_err(Error::Owner)?;
                delegator_rewards_owner.verify().map_err(Error::Owner)?;
                // A primary network validator must register a BLS key and a
                // network validator must not. Consensus aggregates signatures
                // on the primary network and nowhere else, so a key there is
                // required and a key elsewhere is a key nothing checks.
                let has_key = signer.public_key().is_some();
                let is_primary = *chain == PRIMARY_NETWORK_ID;
                if has_key != is_primary {
                    return Err(Error::InvalidSigner {
                        has_key,
                        is_primary,
                    });
                }
                // Which asset a network stakes is the network's own business —
                // only the primary network's is fixed, and the executor is
                // what knows which. Here it must simply be one asset.
                verify_stake(stake, validator.weight, None)
            }
            Unsigned::AddPermissionlessDelegator {
                validator,
                stake,
                rewards_owner,
                ..
            } => {
                if stake.is_empty() {
                    return Err(Error::NoStake);
                }
                validator.verify()?;
                rewards_owner.verify().map_err(Error::Owner)?;
                verify_stake(stake, validator.weight, None)
            }
            Unsigned::RemoveChainValidator {
                chain, chain_auth, ..
            } => {
                if *chain == PRIMARY_NETWORK_ID {
                    return Err(Error::RemovePrimaryNetworkValidator);
                }
                verify_auth(chain_auth)
            }
            Unsigned::IncreaseL1ValidatorBalance { balance, .. } => {
                if *balance == 0 {
                    return Err(Error::ZeroBalance);
                }
                Ok(())
            }
            Unsigned::DisableL1Validator { auth, .. } => verify_auth(auth),
            Unsigned::TransferChainOwnership {
                chain,
                chain_auth,
                owner,
                ..
            } => {
                if *chain == PRIMARY_NETWORK_ID {
                    return Err(Error::TransferPermissionlessChain);
                }
                verify_auth(chain_auth)?;
                owner.verify().map_err(Error::Owner)
            }
            Unsigned::CreateNetwork {
                owner,
                security,
                validators,
                manager_address,
                ..
            } => {
                security.valid().map_err(Error::Security)?;
                if !security.sovereign() && !validators.is_empty() {
                    // No set of its own, so there is nothing for these to join.
                    return Err(Error::NoOwnSetButHasValidators);
                }
                if !security.restake_parent && validators.is_empty() {
                    // A network that does not restake its parent has to ship a
                    // validator, or nothing can produce its first block.
                    return Err(Error::OwnSetMustIncludeValidator);
                }
                if security.sovereign()
                    && security.manager == crate::security::Manager::Contract
                    && manager_address.is_empty()
                {
                    return Err(Error::ContractManagerNeedsAddress);
                }
                verify_network_validators(validators)?;
                if manager_address.len() > MAX_CHAIN_ADDRESS_LEN {
                    return Err(Error::AddressTooLong(manager_address.len()));
                }
                owner.verify().map_err(Error::Owner)?;
                for v in validators {
                    v.verify()?;
                }
                Ok(())
            }
            Unsigned::ConvertNetwork {
                network,
                security,
                validators,
                manager_address,
                auth,
                ..
            } => {
                if *network == PRIMARY_NETWORK_ID {
                    return Err(Error::ConvertPrimaryNetwork);
                }
                if validators.is_empty() {
                    return Err(Error::ConvertMustHaveValidators);
                }
                verify_network_validators(validators)?;
                if manager_address.len() > MAX_CHAIN_ADDRESS_LEN {
                    return Err(Error::AddressTooLong(manager_address.len()));
                }
                security.valid().map_err(Error::Security)?;
                // Promotion is the act of establishing a set of one's own, so a
                // target mode that does not have one is not a promotion.
                if !security.sovereign() {
                    return Err(Error::ConvertMustEstablishOwnSet);
                }
                if security.manager == crate::security::Manager::Contract
                    && manager_address.is_empty()
                {
                    return Err(Error::ContractManagerNeedsAddress);
                }
                for v in validators {
                    v.verify()?;
                }
                verify_auth(auth)
            }
            Unsigned::TransformChain {
                chain,
                asset_id,
                initial_supply,
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
                chain_auth,
                ..
            } => {
                if *chain == PRIMARY_NETWORK_ID {
                    return Err(Error::CantTransformPrimaryNetwork);
                }
                if *asset_id == [0u8; 32] {
                    return Err(Error::EmptyAssetId);
                }
                if *asset_id == native_asset {
                    return Err(Error::AssetIdCantBeNative);
                }
                if *initial_supply == 0 {
                    return Err(Error::InitialSupplyZero);
                }
                if initial_supply > maximum_supply {
                    return Err(Error::InitialSupplyAboveMaximum);
                }
                if min_consumption_rate > max_consumption_rate {
                    return Err(Error::MinConsumptionRateAboveMax);
                }
                if *max_consumption_rate > PERCENT_DENOMINATOR as u64 {
                    return Err(Error::MaxConsumptionRateTooLarge);
                }
                if *min_validator_stake == 0 {
                    return Err(Error::MinValidatorStakeZero);
                }
                if min_validator_stake > initial_supply {
                    return Err(Error::MinValidatorStakeAboveSupply);
                }
                if min_validator_stake > max_validator_stake {
                    return Err(Error::MinValidatorStakeAboveMax);
                }
                if max_validator_stake > maximum_supply {
                    return Err(Error::MaxValidatorStakeAboveSupply);
                }
                if *min_stake_duration == 0 {
                    return Err(Error::MinStakeDurationZero);
                }
                if min_stake_duration > max_stake_duration {
                    return Err(Error::MinStakeDurationAboveMax);
                }
                if *min_delegation_fee > PERCENT_DENOMINATOR {
                    return Err(Error::MinDelegationFeeTooLarge);
                }
                if *min_delegator_stake == 0 {
                    return Err(Error::MinDelegatorStakeZero);
                }
                if *max_validator_weight_factor == 0 {
                    return Err(Error::MaxValidatorWeightFactorZero);
                }
                if *uptime_requirement > PERCENT_DENOMINATOR {
                    return Err(Error::UptimeRequirementTooLarge);
                }
                verify_auth(chain_auth)
            }
            Unsigned::RegisterL1Validator { .. } | Unsigned::SetL1ValidatorWeight { .. } => Ok(()),
        }
    }
}

/// An authorization names which of an owner's signatures are present, so the
/// indices must ascend with no repeat: a repeat would let one signature be
/// counted twice toward the threshold it is measured against.
fn verify_auth(sig_indices: &[u32]) -> Result<(), Error> {
    if crate::components::is_sorted_unique(sig_indices) {
        Ok(())
    } else {
        Err(Error::AuthIndicesNotSortedUnique)
    }
}

/// A genesis set must be in node-id order with no node named twice.
///
/// Order is consensus: two nodes that wrote the same set in different orders
/// would produce different transaction ids for the same intent, and a
/// duplicate would let one node be counted twice in the weight it is admitted
/// with.
fn verify_network_validators(vdrs: &[NetworkValidator]) -> Result<(), Error> {
    for pair in vdrs.windows(2) {
        if pair[0].order_key() >= pair[1].order_key() {
            return Err(Error::ValidatorsNotSortedAndUnique);
        }
    }
    Ok(())
}

fn verify_envelope(base: &Envelope, chain: Chain) -> Result<(), Error> {
    // Where this transaction is addressed. Go: `utxo.BaseTx.Verify`, which
    // refuses a mismatch with `ErrWrongNetworkID` / `ErrWrongChainID` before it
    // looks at anything else.
    //
    // It is the first check and not an afterthought: a transaction that is
    // well-formed on two networks is one signature that spends on both, so a
    // chain that did not read the address would let a spend made on the test
    // network be replayed onto the main one.
    if base.network_id != chain.network_id {
        return Err(Error::WrongNetwork {
            addressed: base.network_id,
            here: chain.network_id,
        });
    }
    if base.blockchain_id != chain.blockchain_id {
        return Err(Error::WrongBlockchain);
    }
    for o in &base.outs {
        o.verify().map_err(Error::Output)?;
    }
    for i in &base.ins {
        i.verify().map_err(Error::Input)?;
    }
    if !is_sorted_outputs(&base.outs) {
        return Err(Error::OutputsNotSorted);
    }
    if !is_sorted_unique_inputs(&base.ins) {
        return Err(Error::InputsNotSortedUnique);
    }
    Ok(())
}

/// The staked outputs must be the native asset, canonically ordered, and add
/// up to exactly the declared weight.
///
/// The last of those is the one that matters: a weight larger than the stake
/// would buy consensus influence that nothing backs.
/// The staked outputs must add up to exactly the declared weight, be
/// canonically ordered, and all be the same asset — `required`, when the kind
/// names one, or whichever the first output is when it does not.
///
/// The weight is the one that matters: a weight larger than the stake would
/// buy consensus influence that nothing backs.
fn verify_stake(stake: &[Output], weight: u64, required: Option<Id>) -> Result<(), Error> {
    let mut total: u64 = 0;
    let staked_asset = required.or_else(|| stake.first().map(|o| o.asset));
    for o in stake {
        o.verify().map_err(Error::Output)?;
        total = total.checked_add(o.amount).ok_or(Error::Overflow)?;
        if Some(o.asset) != staked_asset {
            return Err(match required {
                // The chain's own asset is the only thing these kinds stake.
                Some(_) => Error::StakeMustBeNativeAsset,
                // Any one asset will do, but it has to be one.
                None => Error::MultipleStakedAssets,
            });
        }
    }
    if !is_sorted_outputs(stake) {
        return Err(Error::OutputsNotSorted);
    }
    if total != weight {
        return Err(Error::WeightMismatch {
            declared: weight,
            staked: total,
        });
    }
    Ok(())
}

// ---- credentials ----

/// Encode a transaction's credentials as their own message.
///
/// One entry per credential naming a run in one shared signature array, which
/// is why a credential is two numbers and not a nested list: the signatures
/// are one contiguous stretch of the buffer whatever the credentials do.
pub fn write_credentials(creds: &[Credential]) -> Vec<u8> {
    let mut runs = Vec::with_capacity(creds.len());
    let mut sigs: Vec<[u8; SIG_LEN]> = Vec::new();
    for c in creds {
        runs.push(w::pack_credential_run(&w::CredentialRunInput {
            start: sigs.len() as u32,
            count: c.sigs.len() as u32,
        }));
        sigs.extend_from_slice(&c.sigs);
    }
    w::new_credentials(&w::CredentialsInput {
        runs: &runs.iter().map(|e| &e[..]).collect::<Vec<_>>(),
        signatures: &sigs,
    })
}

/// Read credentials back.
///
/// A credential naming signatures outside the shared array is refused rather
/// than clamped: a claimed range that is not there is a transaction asserting
/// authority it did not bring.
pub fn parse_credentials(bytes: &[u8]) -> Result<Vec<Credential>, Error> {
    let v = w::Credentials::wrap(bytes)?;
    let sigs = v.signatures();
    let total = sigs.len() as u32;
    let mut out = Vec::with_capacity(v.runs().len());
    for i in 0..v.runs().len() {
        let run = v.runs_at(i);
        let (start, count) = (run.start(), run.count());
        if start > total || count > total - start {
            return Err(Error::CredentialRangeOutOfBounds);
        }
        let mut c = Credential {
            sigs: Vec::with_capacity(count as usize),
        };
        for j in 0..count {
            let mut s = [0u8; SIG_LEN];
            let src = sigs.object((start + j) as usize, SIG_LEN).bytes_fixed(0, SIG_LEN);
            s[..src.len()].copy_from_slice(src);
            c.sigs.push(s);
        }
        out.push(c);
    }
    Ok(out)
}

// ---- the signed transaction ----

/// A transaction body with the credentials that authorize it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Tx {
    pub unsigned: Unsigned,
    pub creds: Vec<Credential>,
    id: Id,
    bytes: Vec<u8>,
}

impl Tx {
    /// Bind a body and its credentials to bytes and a name.
    pub fn new(unsigned: Unsigned, creds: Vec<Credential>) -> Tx {
        let unsigned_bytes = unsigned.to_bytes();
        let bytes = if creds.is_empty() {
            unsigned_bytes
        } else {
            let mut b = unsigned_bytes;
            b.extend_from_slice(&write_credentials(&creds));
            b
        };
        let id = hash256(&bytes);
        Tx {
            unsigned,
            creds,
            id,
            bytes,
        }
    }

    /// Read a signed transaction. The bytes are kept verbatim and the name is
    /// derived from them, so nothing that arrives is ever re-encoded.
    pub fn parse(signed: &[u8]) -> Result<Tx, Error> {
        let n = zap::Message::parse(signed)?.size();
        let unsigned = Unsigned::parse(&signed[..n])?;
        let creds = if signed.len() > n {
            parse_credentials(&signed[n..])?
        } else {
            Vec::new()
        };
        Ok(Tx {
            unsigned,
            creds,
            id: hash256(signed),
            bytes: signed.to_vec(),
        })
    }

    pub fn id(&self) -> Id {
        self.id
    }

    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    /// What a signature covers: the body's bytes alone.
    pub fn unsigned_bytes(&self) -> &[u8] {
        let n = zap::Message::parse(&self.bytes)
            .map(|m| m.size())
            .unwrap_or(self.bytes.len());
        &self.bytes[..n]
    }

    /// The digest each credential signs.
    pub fn sighash(&self) -> Id {
        hash256(self.unsigned_bytes())
    }

    /// The outputs this transaction makes, named.
    pub fn utxos(&self) -> Vec<crate::components::Utxo> {
        self.unsigned
            .outputs()
            .iter()
            .enumerate()
            .map(|(i, out)| crate::components::Utxo {
                id: UtxoId {
                    tx_id: self.id,
                    output_index: i as u32,
                },
                output: out.clone(),
            })
            .collect()
    }

    pub fn syntactic_verify(&self, chain: Chain) -> Result<(), Error> {
        if self.id == crate::ids::EMPTY {
            return Err(Error::NotInitialized);
        }
        let memo = self
            .unsigned
            .envelope()
            .map(|e| e.memo.as_slice())
            .unwrap_or(&[]);
        verify_memo(memo).map_err(|e| Error::MemoCarried(e.0))?;
        self.unsigned.syntactic_verify(chain)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::Owners;

    fn addr(n: u8) -> ShortId {
        ShortId([n; 20])
    }

    fn owners(n: u8) -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![addr(n)],
        }
    }

    fn out(asset: u8, amount: u64) -> Output {
        Output {
            asset: [asset; 32],
            stake_lock: 0,
            amount,
            owners: owners(1),
        }
    }

    fn input(tx: u8, idx: u32, amount: u64) -> Input {
        Input {
            utxo: UtxoId {
                tx_id: [tx; 32],
                output_index: idx,
            },
            asset: [9; 32],
            stake_lock: 0,
            amount,
            sig_indices: vec![0],
        }
    }

    /// The chain these tests are addressed to. `envelope()` names the same
    /// network and blockchain, so a test that is not about addressing does not
    /// have to think about it.
    fn chain() -> Chain {
        Chain {
            network_id: 1,
            blockchain_id: [3; 32],
            native_asset: [9u8; 32],
        }
    }

    fn envelope() -> Envelope {
        Envelope {
            network_id: 1,
            blockchain_id: [3; 32],
            outs: vec![out(9, 100)],
            ins: vec![input(1, 0, 200)],
            memo: Vec::new(),
        }
    }

    #[test]
    fn a_transaction_addressed_to_another_network_is_refused() {
        // Go: `utxo.BaseTx.Verify` -> `ErrWrongNetworkID`. A transaction that
        // is well-formed on two networks is one signature that spends on both,
        // so a chain that did not read the address would let a spend made on
        // the test network be replayed onto the main one.
        let mut e = envelope();
        e.network_id = 2;
        assert_eq!(
            Unsigned::Base(e).syntactic_verify(chain()),
            Err(Error::WrongNetwork {
                addressed: 2,
                here: 1
            })
        );
    }

    #[test]
    fn a_transaction_addressed_to_another_chain_is_refused() {
        // Go: `ErrWrongChainID`. The same argument one level down: the P-chain
        // and the X-chain are on one network, and a spend signed for one is not
        // a spend on the other.
        let mut e = envelope();
        e.blockchain_id = [4; 32];
        assert_eq!(
            Unsigned::Base(e).syntactic_verify(chain()),
            Err(Error::WrongBlockchain)
        );
    }

    #[test]
    fn the_address_is_read_before_anything_else_about_the_envelope() {
        // Otherwise a transaction for another network could be refused for a
        // reason that a later version stops refusing, and start being accepted.
        let mut e = envelope();
        e.network_id = 2;
        e.outs = vec![out(9, 0)]; // also worthless, which is its own refusal
        assert!(matches!(
            Unsigned::Base(e).syntactic_verify(chain()),
            Err(Error::WrongNetwork { .. })
        ));
    }

    fn validator() -> Validator {
        Validator {
            node_id: NodeId([5; 20]),
            start: 1000,
            end: 2000,
            weight: 50,
        }
    }

    /// A real proof of possession — a primary-network validator must carry one.
    fn pop() -> Signer {
        let sk = blst::min_pk::SecretKey::key_gen(&[3u8; 32], &[]).unwrap();
        Signer::prove(&sk)
    }

    fn stake() -> Vec<Output> {
        vec![Output {
            asset: [9; 32],
            stake_lock: 0,
            amount: 50,
            owners: owners(2),
        }]
    }

    /// Every kind: bytes in, same value out, and the kind byte is where the
    /// dispatch expects it.
    #[test]
    fn every_kind_round_trips_through_its_bytes() {
        let all = vec![
            Unsigned::RewardValidator {
                staker_tx_id: [7; 32],
            },
            Unsigned::Base(envelope()),
            Unsigned::Import {
                base: envelope(),
                source_chain: [4; 32],
                imported: vec![input(2, 1, 300)],
            },
            Unsigned::Export {
                base: envelope(),
                destination_chain: [4; 32],
                exported: vec![out(9, 40)],
            },
            Unsigned::CreateChain {
                base: envelope(),
                chain: [6; 32],
                vm_id: [8; 32],
                name: "a chain".to_string(),
                fx_ids: vec![[1; 32], [2; 32]],
                genesis: b"genesis bytes".to_vec(),
                chain_auth: vec![0, 2],
            },
            Unsigned::AddValidator {
                base: envelope(),
                validator: validator(),
                stake: stake(),
                rewards_owner: owners(3),
                delegation_shares: 20_000,
            },
            Unsigned::AddDelegator {
                base: envelope(),
                validator: validator(),
                stake: stake(),
                rewards_owner: owners(3),
            },
            Unsigned::AddChainValidator {
                base: envelope(),
                validator: validator(),
                chain: [6; 32],
                chain_auth: vec![1],
            },
            Unsigned::AddPermissionlessValidator {
                base: envelope(),
                validator: validator(),
                chain: crate::ids::PRIMARY_NETWORK_ID,
                signer: Signer::Empty,
                stake: stake(),
                validator_rewards_owner: owners(3),
                delegator_rewards_owner: owners(4),
                delegation_shares: 20_000,
            },
            Unsigned::AddPermissionlessValidator {
                base: envelope(),
                validator: validator(),
                chain: crate::ids::PRIMARY_NETWORK_ID,
                signer: Signer::ProofOfPossession {
                    public_key: [11; 48],
                    proof: [12; 96],
                },
                stake: stake(),
                validator_rewards_owner: owners(3),
                delegator_rewards_owner: owners(4),
                delegation_shares: 1,
            },
            Unsigned::AddPermissionlessDelegator {
                base: envelope(),
                validator: validator(),
                chain: [6; 32],
                stake: stake(),
                rewards_owner: owners(3),
            },
            Unsigned::RemoveChainValidator {
                base: envelope(),
                node_id: NodeId([5; 20]),
                chain: [6; 32],
                chain_auth: vec![0],
            },
            Unsigned::TransferChainOwnership {
                base: envelope(),
                chain: [6; 32],
                chain_auth: vec![0],
                owner: owners(7),
            },
            Unsigned::IncreaseL1ValidatorBalance {
                base: envelope(),
                validation_id: [13; 32],
                balance: 999,
            },
            Unsigned::DisableL1Validator {
                base: envelope(),
                validation_id: [13; 32],
                auth: vec![0, 1],
            },
            Unsigned::CreateNetwork {
                base: envelope(),
                parent: crate::ids::PRIMARY_NETWORK_ID,
                owner: owners(7),
                security: crate::security::Mode {
                    restake_parent: false,
                    admission: crate::security::Admission::Open,
                    threshold: 1000,
                    manager: crate::security::Manager::Contract,
                },
                validators: genesis_set(),
                manager_chain_id: [8; 32],
                manager_address: b"manager address".to_vec(),
            },
            Unsigned::ConvertNetwork {
                base: envelope(),
                network: [6; 32],
                parent: crate::ids::PRIMARY_NETWORK_ID,
                manager_chain_id: [8; 32],
                manager_address: b"manager address".to_vec(),
                validators: genesis_set(),
                auth: vec![1, 2],
                security: crate::security::Mode {
                    restake_parent: false,
                    admission: crate::security::Admission::Gated,
                    threshold: 0,
                    manager: crate::security::Manager::Contract,
                },
            },
            Unsigned::TransformChain {
                base: envelope(),
                chain: [6; 32],
                asset_id: [14; 32],
                initial_supply: 1_000_000,
                maximum_supply: 2_000_000,
                min_consumption_rate: 100_000,
                max_consumption_rate: 120_000,
                min_validator_stake: 1_000,
                max_validator_stake: 500_000,
                min_stake_duration: 3600,
                max_stake_duration: 86400,
                min_delegation_fee: 20_000,
                min_delegator_stake: 500,
                max_validator_weight_factor: 5,
                uptime_requirement: 800_000,
                chain_auth: vec![0, 3],
            },
            Unsigned::RegisterL1Validator {
                base: envelope(),
                balance: 777,
                proof_of_possession: [15; 96],
                message: b"a warp message".to_vec(),
            },
            Unsigned::SetL1ValidatorWeight {
                base: envelope(),
                message: b"another warp message".to_vec(),
            },
        ];

        for tx in all {
            let bytes = tx.to_bytes();
            let msg = zap::Message::parse(&bytes).expect("a built tx is a message");
            assert_eq!(msg.root().u8(w::BASE_KIND), tx.kind() as u8, "kind byte");
            let back = Unsigned::parse(&bytes).expect("parse");
            assert_eq!(back, tx, "round trip for {:?}", tx.kind());
            // Building the same value twice gives the same bytes; that is what
            // makes an id mean one transaction.
            assert_eq!(back.to_bytes(), bytes, "canonical for {:?}", tx.kind());
        }
    }

    #[test]
    fn the_kind_byte_is_the_first_byte_of_the_object() {
        // Go reads the discriminator at object offset 0, and the root object
        // of a freshly built tx starts right after the header.
        let bytes = Unsigned::Base(envelope()).to_bytes();
        let msg = zap::Message::parse(&bytes).unwrap();
        assert_eq!(msg.root().u8(0), Kind::Base as u8);
    }

    #[test]
    fn an_unknown_kind_is_refused() {
        let mut bytes = Unsigned::Base(envelope()).to_bytes();
        let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
        bytes[root] = 200;
        assert_eq!(Unsigned::parse(&bytes), Err(Error::UnknownKind(200)));
        // Slot 0 and slot 1 name nothing, so a zeroed buffer decodes to no kind.
        bytes[root] = 0;
        assert_eq!(Unsigned::parse(&bytes), Err(Error::UnknownKind(0)));
        bytes[root] = 1;
        assert_eq!(Unsigned::parse(&bytes), Err(Error::UnknownKind(1)));
    }

    #[test]
    fn every_kind_the_wire_names_is_read() {
        // The dispatch is total: each of the twenty numbers the wire uses maps
        // to a body this port reads. The parse is over a buffer built for a
        // different kind, so the delta fields read as zero — what is being
        // checked is that the byte is dispatched rather than refused.
        for raw in 2u8..=20 {
            let mut bytes = Unsigned::Base(envelope()).to_bytes();
            let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
            bytes[root] = raw;
            let parsed = Unsigned::parse(&bytes)
                .unwrap_or_else(|e| panic!("kind {raw} is on the wire and must read: {e}"));
            assert_eq!(parsed.kind() as u8, raw);
        }
    }

    #[test]
    fn the_unsigned_bytes_are_a_prefix_of_the_signed_bytes() {
        // This is what lets a signature be checked without re-encoding: the
        // body's own length field finds the split.
        let unsigned = Unsigned::Base(envelope());
        let unsigned_bytes = unsigned.to_bytes();
        let creds = vec![Credential {
            sigs: vec![[1u8; 65]],
        }];
        let tx = Tx::new(unsigned, creds.clone());
        assert!(tx.bytes().len() > unsigned_bytes.len());
        assert_eq!(&tx.bytes()[..unsigned_bytes.len()], &unsigned_bytes[..]);
        assert_eq!(tx.unsigned_bytes(), &unsigned_bytes[..]);

        let back = Tx::parse(tx.bytes()).unwrap();
        assert_eq!(back.creds, creds);
        assert_eq!(back.id(), tx.id());
        assert_eq!(back.unsigned, tx.unsigned);
    }

    #[test]
    fn a_transaction_is_named_by_the_hash_of_the_bytes_that_travel() {
        let tx = Tx::new(Unsigned::Base(envelope()), Vec::new());
        assert_eq!(tx.id(), hash256(tx.bytes()));

        // Adding a credential changes the name, because the credential is part
        // of what travels.
        let signed = Tx::new(
            Unsigned::Base(envelope()),
            vec![Credential {
                sigs: vec![[9u8; 65]],
            }],
        );
        assert_ne!(signed.id(), tx.id());
        // But not what is signed.
        assert_eq!(signed.sighash(), tx.sighash());
    }

    #[test]
    fn credentials_round_trip_with_their_signature_ranges() {
        let creds = vec![
            Credential {
                sigs: vec![[1u8; 65], [2u8; 65]],
            },
            Credential { sigs: vec![] },
            Credential {
                sigs: vec![[3u8; 65]],
            },
        ];
        let bytes = write_credentials(&creds);
        assert_eq!(parse_credentials(&bytes).unwrap(), creds);
    }

    #[test]
    fn a_credential_claiming_signatures_that_are_not_there_is_refused() {
        let creds = vec![Credential {
            sigs: vec![[1u8; 65]],
        }];
        let mut bytes = write_credentials(&creds);
        // Reach the credential entry and enlarge its claimed count. The list
        // pointer is signed: the builder writes lists before the object that
        // names them, so it points backwards.
        let root = u32::from_le_bytes(bytes[8..12].try_into().unwrap()) as usize;
        let rel = i32::from_le_bytes(bytes[root..root + 4].try_into().unwrap());
        let entry = (root as i64 + rel as i64) as usize;
        bytes[entry + 4..entry + 8].copy_from_slice(&99u32.to_le_bytes());
        assert_eq!(
            parse_credentials(&bytes),
            Err(Error::CredentialRangeOutOfBounds)
        );
    }

    #[test]
    fn outputs_must_be_sorted_and_inputs_sorted_and_unique() {
        let mut e = envelope();
        e.outs = vec![out(9, 1), out(8, 1)];
        assert_eq!(
            Unsigned::Base(e).syntactic_verify(chain()),
            Err(Error::OutputsNotSorted)
        );

        let mut e = envelope();
        e.ins = vec![input(1, 0, 1), input(1, 0, 1)];
        assert_eq!(
            Unsigned::Base(e).syntactic_verify(chain()),
            Err(Error::InputsNotSortedUnique)
        );
    }

    #[test]
    fn a_staker_may_not_declare_more_weight_than_it_staked() {
        // The rule that keeps consensus influence backed by value.
        let mut v = validator();
        v.weight = 51; // one more than the 50 staked
        let tx = Unsigned::AddPermissionlessValidator {
            base: envelope(),
            validator: v,
            chain: crate::ids::PRIMARY_NETWORK_ID,
            signer: pop(),
            stake: stake(),
            validator_rewards_owner: owners(3),
            delegator_rewards_owner: owners(4),
            delegation_shares: 0,
        };
        assert_eq!(
            tx.syntactic_verify(chain()),
            Err(Error::WeightMismatch {
                declared: 51,
                staked: 50
            })
        );
    }

    /// A legacy staker stakes the chain's own asset, and nothing else.
    #[test]
    fn a_legacy_staker_may_not_stake_some_other_asset() {
        let mut s = stake();
        s[0].asset = [1; 32];
        let tx = Unsigned::AddValidator {
            base: envelope(),
            validator: validator(),
            stake: s,
            rewards_owner: owners(3),
            delegation_shares: 0,
        };
        assert_eq!(
            tx.syntactic_verify(chain()),
            Err(Error::StakeMustBeNativeAsset)
        );
    }

    /// A permissionless staker stakes ONE asset — which one is the network's
    /// business, not the bytes'.
    ///
    /// Go's `AddPermissionlessValidatorTx.SyntacticVerify` compares every
    /// stake output to the first one, never to the chain's own asset: a
    /// network may be staked in whatever it says, and which asset that is is
    /// something only the executor can know, because it is written in the
    /// network's own transformation. Refusing it here would refuse every
    /// network that stakes anything else.
    #[test]
    fn a_permissionless_staker_stakes_one_asset_whichever_it_is() {
        let other = |asset: u8, amount: u64| Output {
            asset: [asset; 32],
            stake_lock: 0,
            amount,
            owners: owners(2),
        };
        let apv = |stake: Vec<Output>, weight: u64| Unsigned::AddPermissionlessValidator {
            base: envelope(),
            validator: Validator {
                node_id: NodeId([5; 20]),
                start: 1000,
                end: 2000,
                weight,
            },
            chain: [6; 32],
            signer: Signer::Empty,
            stake,
            validator_rewards_owner: owners(3),
            delegator_rewards_owner: owners(4),
            delegation_shares: PERCENT_DENOMINATOR,
        };

        // One asset that is not the chain's own: fine here.
        assert_eq!(apv(vec![other(1, 50)], 50).syntactic_verify(chain()), Ok(()));
        // Two assets: not fine, whichever they are.
        assert_eq!(
            apv(vec![other(1, 25), other(2, 25)], 50).syntactic_verify(chain()),
            Err(Error::MultipleStakedAssets)
        );
    }

    #[test]
    fn a_validator_may_not_take_more_than_the_whole_delegation_reward() {
        // Go: errTooManyShares, at exactly PercentDenominator + 1.
        let tx = Unsigned::AddValidator {
            base: envelope(),
            validator: validator(),
            stake: stake(),
            rewards_owner: owners(3),
            delegation_shares: PERCENT_DENOMINATOR + 1,
        };
        assert_eq!(tx.syntactic_verify(chain()), Err(Error::TooManyShares));

        let ok = Unsigned::AddValidator {
            base: envelope(),
            validator: validator(),
            stake: stake(),
            rewards_owner: owners(3),
            delegation_shares: PERCENT_DENOMINATOR,
        };
        assert_eq!(ok.syntactic_verify(chain()), Ok(()));
    }

    #[test]
    fn a_validator_needs_weight() {
        let mut v = validator();
        v.weight = 0;
        let tx = Unsigned::AddChainValidator {
            base: envelope(),
            validator: v,
            chain: [6; 32],
            chain_auth: vec![],
        };
        assert_eq!(tx.syntactic_verify(chain()), Err(Error::WeightTooSmall));
    }

    #[test]
    fn a_chain_validator_may_not_name_the_primary_network() {
        // Go: ChainValidator.Verify returns errBadChainID for the primary
        // network — that set is entered by staking, not by being named.
        let tx = Unsigned::AddChainValidator {
            base: envelope(),
            validator: validator(),
            chain: PRIMARY_NETWORK_ID,
            chain_auth: vec![],
        };
        assert_eq!(tx.syntactic_verify(chain()), Err(Error::BadChainId));
    }

    #[test]
    fn a_memo_is_refused() {
        let mut e = envelope();
        e.memo = b"hello".to_vec();
        let tx = Tx::new(Unsigned::Base(e), Vec::new());
        assert_eq!(tx.syntactic_verify(chain()), Err(Error::MemoCarried(5)));
    }

    #[test]
    fn a_name_longer_than_the_limit_is_refused() {
        let tx = Unsigned::CreateChain {
            base: envelope(),
            chain: [6; 32],
            vm_id: [8; 32],
            name: "x".repeat(MAX_NAME_LEN + 1),
            fx_ids: vec![],
            genesis: vec![],
            chain_auth: vec![],
        };
        assert_eq!(
            tx.syntactic_verify(chain()),
            Err(Error::NameTooLong(MAX_NAME_LEN + 1))
        );
    }

    #[test]
    fn priorities_number_as_go_numbers_them() {
        // These values order the staker set and break its ties, so they are
        // consensus. Go's iota order in txs/priorities.go, starting at 1.
        assert_eq!(Priority::PrimaryNetworkDelegatorLegacyPending as u8, 1);
        assert_eq!(Priority::PrimaryNetworkValidatorPending as u8, 2);
        assert_eq!(
            Priority::PrimaryNetworkDelegatorPermissionlessPending as u8,
            3
        );
        assert_eq!(Priority::ChainPermissionlessValidatorPending as u8, 4);
        assert_eq!(Priority::ChainPermissionlessDelegatorPending as u8, 5);
        assert_eq!(Priority::ChainPermissionedValidatorPending as u8, 6);
        assert_eq!(Priority::ChainPermissionedValidatorCurrent as u8, 7);
        assert_eq!(Priority::ChainPermissionlessDelegatorCurrent as u8, 8);
        assert_eq!(Priority::ChainPermissionlessValidatorCurrent as u8, 9);
        assert_eq!(Priority::PrimaryNetworkDelegatorCurrent as u8, 10);
        assert_eq!(Priority::PrimaryNetworkValidatorCurrent as u8, 11);
    }

    #[test]
    fn a_permissioned_validator_leaves_before_anything_else_at_the_same_time() {
        // The invariant advance-time relies on: permissioned stakers are
        // removed by the clock and must be met first.
        assert!(
            Priority::ChainPermissionedValidatorCurrent
                < Priority::ChainPermissionlessDelegatorCurrent
        );
        assert!(
            Priority::ChainPermissionedValidatorCurrent < Priority::PrimaryNetworkValidatorCurrent
        );
    }

    #[test]
    fn every_pending_priority_has_a_current_one() {
        for p in [
            Priority::PrimaryNetworkDelegatorLegacyPending,
            Priority::PrimaryNetworkValidatorPending,
            Priority::PrimaryNetworkDelegatorPermissionlessPending,
            Priority::ChainPermissionlessValidatorPending,
            Priority::ChainPermissionlessDelegatorPending,
            Priority::ChainPermissionedValidatorPending,
        ] {
            assert!(p.to_current().is_some(), "{p:?}");
        }
        assert!(Priority::PrimaryNetworkValidatorCurrent
            .to_current()
            .is_none());
    }

    /// Go: `TestPriorityIsValidator`, `IsPermissionedValidator`,
    /// `IsCurrentValidator`, `IsCurrentDelegator`, `IsPendingValidator` and
    /// `IsPendingDelegator` — every predicate against every priority, because
    /// a wrong `false` is as much a fork as a wrong `true`.
    #[test]
    fn every_predicate_answers_what_go_answers_for_every_priority() {
        use Priority::*;
        // (priority, validator, permissioned, current vdr, current dlg,
        //  pending vdr, pending dlg)
        let table: [(Priority, bool, bool, bool, bool, bool, bool); 11] = [
            (
                PrimaryNetworkDelegatorLegacyPending,
                false,
                false,
                false,
                false,
                false,
                true,
            ),
            (
                PrimaryNetworkValidatorPending,
                true,
                false,
                false,
                false,
                true,
                false,
            ),
            (
                PrimaryNetworkDelegatorPermissionlessPending,
                false,
                false,
                false,
                false,
                false,
                true,
            ),
            (
                ChainPermissionlessValidatorPending,
                true,
                false,
                false,
                false,
                true,
                false,
            ),
            (
                ChainPermissionlessDelegatorPending,
                false,
                false,
                false,
                false,
                false,
                true,
            ),
            (
                ChainPermissionedValidatorPending,
                true,
                true,
                false,
                false,
                true,
                false,
            ),
            (
                ChainPermissionedValidatorCurrent,
                true,
                true,
                true,
                false,
                false,
                false,
            ),
            (
                ChainPermissionlessDelegatorCurrent,
                false,
                false,
                false,
                true,
                false,
                false,
            ),
            (
                ChainPermissionlessValidatorCurrent,
                true,
                false,
                true,
                false,
                false,
                false,
            ),
            (
                PrimaryNetworkDelegatorCurrent,
                false,
                false,
                false,
                true,
                false,
                false,
            ),
            (
                PrimaryNetworkValidatorCurrent,
                true,
                false,
                true,
                false,
                false,
                false,
            ),
        ];
        for (p, validator, permissioned, cur_v, cur_d, pend_v, pend_d) in table {
            assert_eq!(p.is_validator(), validator, "{p:?} is_validator");
            assert_eq!(
                p.is_permissioned_validator(),
                permissioned,
                "{p:?} is_permissioned_validator"
            );
            assert_eq!(
                p.is_current_validator(),
                cur_v,
                "{p:?} is_current_validator"
            );
            assert_eq!(
                p.is_current_delegator(),
                cur_d,
                "{p:?} is_current_delegator"
            );
            assert_eq!(
                p.is_pending_validator(),
                pend_v,
                "{p:?} is_pending_validator"
            );
            assert_eq!(
                p.is_pending_delegator(),
                pend_d,
                "{p:?} is_pending_delegator"
            );
        }
    }

    #[test]
    fn a_bound_holds_only_when_the_period_sits_inside_it() {
        // Go: txs.BoundedBy — non-strict subset, and the staker's own period
        // must not run backwards.
        assert!(bounded_by(10, 20, 10, 20));
        assert!(bounded_by(11, 19, 10, 20));
        assert!(!bounded_by(9, 20, 10, 20));
        assert!(!bounded_by(10, 21, 10, 20));
        assert!(!bounded_by(20, 10, 0, 100));
    }

    #[test]
    fn a_transaction_names_the_outputs_it_makes() {
        let tx = Tx::new(Unsigned::Base(envelope()), Vec::new());
        let utxos = tx.utxos();
        assert_eq!(utxos.len(), 1);
        assert_eq!(utxos[0].id.tx_id, tx.id());
        assert_eq!(utxos[0].id.output_index, 0);
    }

    /// Two genesis validators, in node-id order.
    fn genesis_set() -> Vec<NetworkValidator> {
        vec![
            NetworkValidator {
                node_id: vec![5u8; 20],
                weight: 100,
                balance: 900,
                signer: pop(),
                remaining_balance_owner: PChainOwner {
                    threshold: 1,
                    addresses: vec![addr(1)],
                },
                deactivation_owner: PChainOwner {
                    threshold: 2,
                    addresses: vec![addr(2), addr(3)],
                },
            },
            NetworkValidator {
                node_id: vec![6u8; 20],
                weight: 200,
                balance: 800,
                signer: pop(),
                remaining_balance_owner: PChainOwner {
                    threshold: 1,
                    addresses: vec![addr(4)],
                },
                deactivation_owner: PChainOwner {
                    threshold: 0,
                    addresses: Vec::new(),
                },
            },
        ]
    }

    fn native() -> Id {
        [9; 32]
    }

    /// Go: `TestUnsignedCreateChainTxVerify`, case for case.
    #[test]
    fn a_chain_is_refused_for_each_thing_that_makes_it_unrunnable() {
        let ok = |name: &str, vm_id: Id, chain: Id, genesis: Vec<u8>, fx_ids: Vec<Id>| {
            Unsigned::CreateChain {
                base: envelope(),
                chain,
                vm_id,
                name: name.to_string(),
                fx_ids,
                genesis,
                chain_auth: vec![0, 1],
            }
        };
        let valid = [6u8; 32];
        let vm = [8u8; 32];

        // A chain with no VM has nothing to run it.
        assert_eq!(
            ok("yeet", [0; 32], valid, vec![], vec![]).syntactic_verify(chain()),
            Err(Error::InvalidVmId)
        );
        // The primary network validates itself; a blockchain may not ask it to.
        assert_eq!(
            ok("yeet", vm, PRIMARY_NETWORK_ID, vec![], vec![]).syntactic_verify(chain()),
            Err(Error::CantValidatePrimaryNetwork)
        );
        let long = "a".repeat(MAX_NAME_LEN + 1);
        assert_eq!(
            ok(&long, vm, valid, vec![], vec![]).syntactic_verify(chain()),
            Err(Error::NameTooLong(MAX_NAME_LEN + 1))
        );
        // Go's case is "⌘" — outside ASCII, so outside what a name may say.
        assert_eq!(
            ok("⌘", vm, valid, vec![], vec![]).syntactic_verify(chain()),
            Err(Error::IllegalNameCharacter('⌘'))
        );
        assert_eq!(
            ok("yeet", vm, valid, vec![0; MAX_GENESIS_LEN + 1], vec![]).syntactic_verify(chain()),
            Err(Error::GenesisTooLong(MAX_GENESIS_LEN + 1))
        );
        assert_eq!(
            ok("yeet", vm, valid, vec![], vec![[2; 32], [1; 32]]).syntactic_verify(chain()),
            Err(Error::FxIdsNotSortedAndUnique)
        );
        // And an authorization that would let one signature count twice.
        let mut bad_auth = ok("yeet", vm, valid, vec![], vec![]);
        if let Unsigned::CreateChain { chain_auth, .. } = &mut bad_auth {
            *chain_auth = vec![1, 0];
        }
        assert_eq!(
            bad_auth.syntactic_verify(chain()),
            Err(Error::AuthIndicesNotSortedUnique)
        );

        assert_eq!(
            ok("yeet", vm, valid, vec![], vec![]).syntactic_verify(chain()),
            Ok(())
        );
        assert_eq!(
            ok("a chain 9", vm, valid, vec![], vec![]).syntactic_verify(chain()),
            Ok(())
        );
    }

    /// Go: `TestTransformChainTxSyntacticVerify`, case for case.
    #[test]
    fn a_transformation_is_refused_for_each_incoherent_term() {
        // Go's baseline: every field passes its own check.
        let valid = || Unsigned::TransformChain {
            base: envelope(),
            chain: [6; 32],
            asset_id: [14; 32],
            initial_supply: 10,
            maximum_supply: 10,
            min_consumption_rate: 0,
            max_consumption_rate: PERCENT_DENOMINATOR as u64,
            min_validator_stake: 2,
            max_validator_stake: 10,
            min_stake_duration: 1,
            max_stake_duration: 2,
            min_delegation_fee: PERCENT_DENOMINATOR,
            min_delegator_stake: 1,
            max_validator_weight_factor: 1,
            uptime_requirement: PERCENT_DENOMINATOR,
            chain_auth: vec![0],
        };
        assert_eq!(valid().syntactic_verify(chain()), Ok(()));

        let case = |mutate: fn(&mut Unsigned), want: Error| {
            let mut tx = valid();
            mutate(&mut tx);
            assert_eq!(tx.syntactic_verify(chain()), Err(want));
        };

        case(
            |tx| {
                if let Unsigned::TransformChain { chain, .. } = tx {
                    *chain = PRIMARY_NETWORK_ID;
                }
            },
            Error::CantTransformPrimaryNetwork,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain { asset_id, .. } = tx {
                    *asset_id = [0; 32];
                }
            },
            Error::EmptyAssetId,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain { asset_id, .. } = tx {
                    *asset_id = native();
                }
            },
            Error::AssetIdCantBeNative,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain { initial_supply, .. } = tx {
                    *initial_supply = 0;
                }
            },
            Error::InitialSupplyZero,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    initial_supply,
                    maximum_supply,
                    ..
                } = tx
                {
                    *initial_supply = 2;
                    *maximum_supply = 1;
                }
            },
            Error::InitialSupplyAboveMaximum,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_consumption_rate,
                    max_consumption_rate,
                    ..
                } = tx
                {
                    *min_consumption_rate = 2;
                    *max_consumption_rate = 1;
                }
            },
            Error::MinConsumptionRateAboveMax,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_consumption_rate,
                    max_consumption_rate,
                    ..
                } = tx
                {
                    *min_consumption_rate = 0;
                    *max_consumption_rate = PERCENT_DENOMINATOR as u64 + 1;
                }
            },
            Error::MaxConsumptionRateTooLarge,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_validator_stake,
                    ..
                } = tx
                {
                    *min_validator_stake = 0;
                }
            },
            Error::MinValidatorStakeZero,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    initial_supply,
                    min_validator_stake,
                    ..
                } = tx
                {
                    *initial_supply = 1;
                    *min_validator_stake = 2;
                }
            },
            Error::MinValidatorStakeAboveSupply,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_validator_stake,
                    max_validator_stake,
                    ..
                } = tx
                {
                    *min_validator_stake = 2;
                    *max_validator_stake = 1;
                }
            },
            Error::MinValidatorStakeAboveMax,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    max_validator_stake,
                    ..
                } = tx
                {
                    *max_validator_stake = 11;
                }
            },
            Error::MaxValidatorStakeAboveSupply,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_stake_duration, ..
                } = tx
                {
                    *min_stake_duration = 0;
                }
            },
            Error::MinStakeDurationZero,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_stake_duration,
                    max_stake_duration,
                    ..
                } = tx
                {
                    *min_stake_duration = 2;
                    *max_stake_duration = 1;
                }
            },
            Error::MinStakeDurationAboveMax,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_delegation_fee, ..
                } = tx
                {
                    *min_delegation_fee = PERCENT_DENOMINATOR + 1;
                }
            },
            Error::MinDelegationFeeTooLarge,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    min_delegator_stake,
                    ..
                } = tx
                {
                    *min_delegator_stake = 0;
                }
            },
            Error::MinDelegatorStakeZero,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    max_validator_weight_factor,
                    ..
                } = tx
                {
                    *max_validator_weight_factor = 0;
                }
            },
            Error::MaxValidatorWeightFactorZero,
        );
        case(
            |tx| {
                if let Unsigned::TransformChain {
                    uptime_requirement, ..
                } = tx
                {
                    *uptime_requirement = PERCENT_DENOMINATOR + 1;
                }
            },
            Error::UptimeRequirementTooLarge,
        );
        // Go's "invalid chainAuth": indices that are not sorted and unique.
        case(
            |tx| {
                if let Unsigned::TransformChain { chain_auth, .. } = tx {
                    *chain_auth = vec![1, 0];
                }
            },
            Error::AuthIndicesNotSortedUnique,
        );
    }

    /// The two network transactions, refused for each thing that would leave a
    /// network nothing secures.
    #[test]
    fn a_network_must_be_secured_by_something() {
        let sovereign = crate::security::Mode {
            restake_parent: false,
            admission: crate::security::Admission::Open,
            threshold: 1000,
            manager: crate::security::Manager::PChain,
        };
        let make = |security: crate::security::Mode, validators: Vec<NetworkValidator>| {
            Unsigned::CreateNetwork {
                base: envelope(),
                parent: PRIMARY_NETWORK_ID,
                owner: owners(7),
                security,
                validators,
                manager_chain_id: [0; 32],
                manager_address: Vec::new(),
            }
        };

        // Neither axis: nothing would secure its blocks.
        assert_eq!(
            make(
                crate::security::Mode {
                    restake_parent: false,
                    admission: crate::security::Admission::NoOwnSet,
                    threshold: 0,
                    manager: crate::security::Manager::PChain,
                },
                Vec::new()
            )
            .syntactic_verify(chain()),
            Err(Error::Security(crate::security::Error::NoSecurity))
        );
        // No set of its own, but carrying validators for one.
        assert_eq!(
            make(
                crate::security::Mode {
                    restake_parent: true,
                    admission: crate::security::Admission::NoOwnSet,
                    threshold: 0,
                    manager: crate::security::Manager::PChain,
                },
                genesis_set()
            )
            .syntactic_verify(chain()),
            Err(Error::NoOwnSetButHasValidators)
        );
        // Its own set, and nobody in it to make the first block.
        assert_eq!(
            make(sovereign, Vec::new()).syntactic_verify(chain()),
            Err(Error::OwnSetMustIncludeValidator)
        );
        // A contract-managed set that names no contract.
        assert_eq!(
            make(
                crate::security::Mode {
                    manager: crate::security::Manager::Contract,
                    ..sovereign
                },
                genesis_set()
            )
            .syntactic_verify(chain()),
            Err(Error::ContractManagerNeedsAddress)
        );
        // A genesis set out of order, so two nodes writing the same intent
        // would write different bytes.
        let mut reversed = genesis_set();
        reversed.reverse();
        assert_eq!(
            make(sovereign, reversed).syntactic_verify(chain()),
            Err(Error::ValidatorsNotSortedAndUnique)
        );
        // A node named twice would be counted twice.
        let doubled = vec![genesis_set()[0].clone(), genesis_set()[0].clone()];
        assert_eq!(
            make(sovereign, doubled).syntactic_verify(chain()),
            Err(Error::ValidatorsNotSortedAndUnique)
        );
        // A validator worth nothing.
        let mut weightless = genesis_set();
        weightless[0].weight = 0;
        assert_eq!(
            make(sovereign, weightless).syntactic_verify(chain()),
            Err(Error::ZeroWeight)
        );
        // A node id that is not one.
        let mut short_node = genesis_set();
        short_node[0].node_id = vec![5; 19];
        assert_eq!(
            make(sovereign, short_node).syntactic_verify(chain()),
            Err(Error::BadNodeIdLength(19))
        );
        // A signer slot that carries no key at all.
        let mut keyless = genesis_set();
        keyless[0].signer = Signer::Empty;
        assert_eq!(
            make(sovereign, keyless).syntactic_verify(chain()),
            Err(Error::Signer(crate::signer::Error::MalformedPublicKey))
        );
        // A manager address longer than a network may name.
        let mut long_address = make(sovereign, genesis_set());
        if let Unsigned::CreateNetwork {
            manager_address, ..
        } = &mut long_address
        {
            *manager_address = vec![0; MAX_CHAIN_ADDRESS_LEN + 1];
        }
        assert_eq!(
            long_address.syntactic_verify(chain()),
            Err(Error::AddressTooLong(MAX_CHAIN_ADDRESS_LEN + 1))
        );

        assert_eq!(
            make(sovereign, genesis_set()).syntactic_verify(chain()),
            Ok(())
        );
    }

    /// A promotion has to promote: it names a network that is not the primary
    /// one, and it establishes a set of that network's own.
    #[test]
    fn a_promotion_must_establish_a_set_of_its_own() {
        let make =
            |network: Id, security: crate::security::Mode, validators: Vec<NetworkValidator>| {
                Unsigned::ConvertNetwork {
                    base: envelope(),
                    network,
                    parent: PRIMARY_NETWORK_ID,
                    manager_chain_id: [8; 32],
                    manager_address: b"manager address".to_vec(),
                    validators,
                    auth: vec![0],
                    security,
                }
            };
        let sovereign = crate::security::Mode {
            restake_parent: false,
            admission: crate::security::Admission::Gated,
            threshold: 0,
            manager: crate::security::Manager::Contract,
        };

        assert_eq!(
            make(PRIMARY_NETWORK_ID, sovereign, genesis_set()).syntactic_verify(chain()),
            Err(Error::ConvertPrimaryNetwork)
        );
        assert_eq!(
            make([6; 32], sovereign, Vec::new()).syntactic_verify(chain()),
            Err(Error::ConvertMustHaveValidators)
        );
        assert_eq!(
            make(
                [6; 32],
                crate::security::Mode {
                    restake_parent: true,
                    admission: crate::security::Admission::NoOwnSet,
                    ..sovereign
                },
                genesis_set()
            )
            .syntactic_verify(chain()),
            Err(Error::ConvertMustEstablishOwnSet)
        );
        let mut bad_auth = make([6; 32], sovereign, genesis_set());
        if let Unsigned::ConvertNetwork { auth, .. } = &mut bad_auth {
            *auth = vec![3, 3];
        }
        assert_eq!(
            bad_auth.syntactic_verify(chain()),
            Err(Error::AuthIndicesNotSortedUnique)
        );

        assert_eq!(
            make([6; 32], sovereign, genesis_set()).syntactic_verify(chain()),
            Ok(())
        );
    }

    /// Go: `TestIncreaseL1ValidatorBalanceTxSyntacticVerify`,
    /// `TestDisableL1ValidatorTxSyntacticVerify`,
    /// `TestRemoveChainValidatorTxSyntacticVerify` and
    /// `TestTransferChainOwnershipTxSyntacticVerify` — the checks each of the
    /// small kinds makes.
    #[test]
    fn the_small_kinds_refuse_what_go_refuses() {
        assert_eq!(
            Unsigned::IncreaseL1ValidatorBalance {
                base: envelope(),
                validation_id: [13; 32],
                balance: 0,
            }
            .syntactic_verify(chain()),
            Err(Error::ZeroBalance)
        );
        assert_eq!(
            Unsigned::DisableL1Validator {
                base: envelope(),
                validation_id: [13; 32],
                auth: vec![1, 1],
            }
            .syntactic_verify(chain()),
            Err(Error::AuthIndicesNotSortedUnique)
        );
        assert_eq!(
            Unsigned::RemoveChainValidator {
                base: envelope(),
                node_id: NodeId([5; 20]),
                chain: PRIMARY_NETWORK_ID,
                chain_auth: vec![0],
            }
            .syntactic_verify(chain()),
            Err(Error::RemovePrimaryNetworkValidator)
        );
        assert_eq!(
            Unsigned::TransferChainOwnership {
                base: envelope(),
                chain: PRIMARY_NETWORK_ID,
                chain_auth: vec![0],
                owner: owners(7),
            }
            .syntactic_verify(chain()),
            Err(Error::TransferPermissionlessChain)
        );
        // A validator the network's owner admits by name may not be admitted
        // to the primary network, which admits nobody by name.
        assert_eq!(
            Unsigned::AddChainValidator {
                base: envelope(),
                validator: validator(),
                chain: PRIMARY_NETWORK_ID,
                chain_auth: vec![0],
            }
            .syntactic_verify(chain()),
            Err(Error::BadChainId)
        );
    }

    /// Go: `TestAddPermissionlessValidatorTxSyntacticVerify`, case for case.
    #[test]
    fn a_permissionless_validator_is_refused_for_each_thing_that_is_wrong() {
        let good_owner = || owners(3);
        // An owner nobody can satisfy: one signature required, no address to
        // give it. Go reaches this case through a mock; a real unspendable
        // owner is the same refusal.
        let bad_owner = || Owners {
            locktime: 0,
            threshold: 1,
            addrs: Vec::new(),
        };
        let out = |asset: u8, amount: u64| Output {
            asset: [asset; 32],
            stake_lock: 0,
            amount,
            owners: owners(2),
        };
        let apv = |node: NodeId,
                   chain: Id,
                   signer: Signer,
                   stake: Vec<Output>,
                   owner: Owners,
                   weight: u64,
                   shares: u32| {
            Unsigned::AddPermissionlessValidator {
                base: envelope(),
                validator: Validator {
                    node_id: node,
                    start: 0,
                    end: 0,
                    weight,
                },
                chain,
                signer,
                stake,
                validator_rewards_owner: owner.clone(),
                delegator_rewards_owner: owner,
                delegation_shares: shares,
            }
        };
        let node = NodeId([5; 20]);
        let net = [6u8; 32];

        // A validator with no node.
        assert_eq!(
            apv(
                NodeId([0; 20]),
                net,
                Signer::Empty,
                Vec::new(),
                good_owner(),
                0,
                0
            )
            .syntactic_verify(chain()),
            Err(Error::EmptyNodeId)
        );
        // A validator that stakes nothing.
        assert_eq!(
            apv(node, net, Signer::Empty, Vec::new(), good_owner(), 0, 0).syntactic_verify(chain()),
            Err(Error::NoStake)
        );
        // A fee larger than the whole reward.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1)],
                good_owner(),
                0,
                PERCENT_DENOMINATOR + 1
            )
            .syntactic_verify(chain()),
            Err(Error::TooManyShares)
        );
        // A validator worth nothing.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1)],
                good_owner(),
                0,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::WeightTooSmall)
        );
        // A rewards owner nobody can satisfy.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1)],
                bad_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::Owner(
                crate::components::OwnerError::ThresholdExceedsAddresses
            ))
        );
        // A primary-network validator with no key to sign with.
        assert_eq!(
            apv(
                node,
                PRIMARY_NETWORK_ID,
                Signer::Empty,
                vec![out(1, 1)],
                good_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::InvalidSigner {
                has_key: false,
                is_primary: true
            })
        );
        // A stake output nobody can spend.
        let unspendable = Output {
            asset: [1; 32],
            stake_lock: 0,
            amount: 1,
            owners: bad_owner(),
        };
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![unspendable],
                good_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::Output(crate::components::OutputError::Owner(
                crate::components::OwnerError::ThresholdExceedsAddresses
            )))
        );
        // A stake that does not fit in the weight it would have.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, u64::MAX), out(1, 2)],
                good_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::Overflow)
        );
        // Stake in two assets.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1), out(2, 1)],
                good_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::MultipleStakedAssets)
        );
        // Stake out of order.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 2), out(1, 1)],
                good_owner(),
                3,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::OutputsNotSorted)
        );
        // A weight the stake does not back.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1), out(1, 1)],
                good_owner(),
                1,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Err(Error::WeightMismatch {
                declared: 1,
                staked: 2
            })
        );
        // A network validator, with no key, is well formed.
        assert_eq!(
            apv(
                node,
                net,
                Signer::Empty,
                vec![out(1, 1), out(1, 1)],
                good_owner(),
                2,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Ok(())
        );
        // And a primary-network validator, with one.
        assert_eq!(
            apv(
                node,
                PRIMARY_NETWORK_ID,
                pop(),
                vec![out(1, 1), out(1, 1)],
                good_owner(),
                2,
                PERCENT_DENOMINATOR
            )
            .syntactic_verify(chain()),
            Ok(())
        );
    }

    /// Go: `TestAddPermissionlessDelegatorTxSyntacticVerify` — the same
    /// shape, with no signer and no shares to get wrong.
    #[test]
    fn a_permissionless_delegator_is_refused_for_each_thing_that_is_wrong() {
        let out = |asset: u8, amount: u64| Output {
            asset: [asset; 32],
            stake_lock: 0,
            amount,
            owners: owners(2),
        };
        let apd =
            |stake: Vec<Output>, owner: Owners, weight: u64| Unsigned::AddPermissionlessDelegator {
                base: envelope(),
                validator: Validator {
                    node_id: NodeId([5; 20]),
                    start: 0,
                    end: 0,
                    weight,
                },
                chain: [6; 32],
                stake,
                rewards_owner: owner,
            };
        assert_eq!(
            apd(Vec::new(), owners(3), 0).syntactic_verify(chain()),
            Err(Error::NoStake)
        );
        assert_eq!(
            apd(vec![out(1, 1)], owners(3), 0).syntactic_verify(chain()),
            Err(Error::WeightTooSmall)
        );
        assert_eq!(
            apd(vec![out(1, 1), out(2, 1)], owners(3), 2).syntactic_verify(chain()),
            Err(Error::MultipleStakedAssets)
        );
        assert_eq!(
            apd(vec![out(1, 1), out(1, 1)], owners(3), 1).syntactic_verify(chain()),
            Err(Error::WeightMismatch {
                declared: 1,
                staked: 2
            })
        );
        assert_eq!(
            apd(vec![out(1, 1), out(1, 1)], owners(3), 2).syntactic_verify(chain()),
            Ok(())
        );
    }

    /// Go: `TestParseCredsBufRejectsOutOfRangeSigs` — the three shapes a
    /// remote sender controls, each of which would otherwise be read as a
    /// signature that is not there.
    ///
    /// Both numbers come off the wire: an index past the array reads nothing,
    /// and the count alone sizes what the reader allocates.
    #[test]
    fn a_credential_naming_a_range_outside_the_array_is_refused() {
        // A buffer whose single entry claims `declared` signatures while the
        // array holds `actual`. It is what `write_credentials` writes, with
        // the two counts decoupled.
        //
        // The builder is the generated one, so what is crafted is a WELL
        // FORMED credentials message whose one entry lies about its run — the
        // shape a remote sender can actually produce, not a shape only a
        // hand-written writer could make.
        let crafted = |declared: u32, actual: usize| -> Vec<u8> {
            let run = w::pack_credential_run(&w::CredentialRunInput {
                start: 0,
                count: declared,
            });
            let sigs: Vec<[u8; SIG_LEN]> = (0..actual)
                .map(|i| {
                    let mut sig = [0u8; SIG_LEN];
                    sig[0] = (i + 1) as u8;
                    sig
                })
                .collect();
            w::new_credentials(&w::CredentialsInput {
                runs: &[&run[..]],
                signatures: &sigs,
            })
        };

        // A count past the end of the array.
        assert_eq!(
            parse_credentials(&crafted(2, 1)),
            Err(Error::CredentialRangeOutOfBounds)
        );
        // A count against no array at all.
        assert_eq!(
            parse_credentials(&crafted(1, 0)),
            Err(Error::CredentialRangeOutOfBounds)
        );
        // A count that has nothing to do with the payload — the one that sizes
        // an allocation of four billion signatures.
        assert_eq!(
            parse_credentials(&crafted(u32::MAX, 1)),
            Err(Error::CredentialRangeOutOfBounds)
        );

        // And the check leaves a well-formed buffer alone, including a
        // credential that carries no signature.
        assert_eq!(parse_credentials(&crafted(1, 1)).map(|c| c.len()), Ok(1));
        assert_eq!(parse_credentials(&crafted(0, 0)).map(|c| c.len()), Ok(1));
    }
}

#[cfg(test)]
mod the_envelope {
    //! Every kind opens with the same eight fields at the same offsets.
    //!
    //! [`read_envelope`] reads them through `Base`'s accessors whatever kind
    //! the byte at 0 names, which is only true because the schema declares
    //! that prefix identically in every struct. Here it is checked, kind by
    //! kind, against the emitted constants — schema against schema, with no
    //! number written down a second time to be checked against.
    use crate::pchain_zap as w;

    /// The eight offsets Base puts them at.
    const BASE: [usize; 8] = [
        w::BASE_KIND,
        w::BASE_NETWORK_ID,
        w::BASE_BLOCKCHAIN_ID,
        w::BASE_OUTS,
        w::BASE_OWNER_ADDRS,
        w::BASE_INS,
        w::BASE_SIG_INDICES,
        w::BASE_MEMO,
    ];

    #[test]
    fn the_envelope_is_the_same_eight_fields_in_every_kind() {
        // Written out rather than generated, because a macro that built the
        // constant names could only build them from the same string the
        // constants came from — and would agree with itself no matter what
        // the schema said.
        let kinds: [(&str, [usize; 8]); 19] = [
            ("Import", [
                w::IMPORT_KIND, w::IMPORT_NETWORK_ID, w::IMPORT_BLOCKCHAIN_ID, w::IMPORT_OUTS,
                w::IMPORT_OWNER_ADDRS, w::IMPORT_INS, w::IMPORT_SIG_INDICES, w::IMPORT_MEMO,
            ]),
            ("Export", [
                w::EXPORT_KIND, w::EXPORT_NETWORK_ID, w::EXPORT_BLOCKCHAIN_ID, w::EXPORT_OUTS,
                w::EXPORT_OWNER_ADDRS, w::EXPORT_INS, w::EXPORT_SIG_INDICES, w::EXPORT_MEMO,
            ]),
            ("CreateChain", [
                w::CREATE_CHAIN_KIND, w::CREATE_CHAIN_NETWORK_ID, w::CREATE_CHAIN_BLOCKCHAIN_ID,
                w::CREATE_CHAIN_OUTS, w::CREATE_CHAIN_OWNER_ADDRS, w::CREATE_CHAIN_INS,
                w::CREATE_CHAIN_SIG_INDICES, w::CREATE_CHAIN_MEMO,
            ]),
            ("TransferChainOwnership", [
                w::TRANSFER_CHAIN_OWNERSHIP_KIND, w::TRANSFER_CHAIN_OWNERSHIP_NETWORK_ID,
                w::TRANSFER_CHAIN_OWNERSHIP_BLOCKCHAIN_ID, w::TRANSFER_CHAIN_OWNERSHIP_OUTS,
                w::TRANSFER_CHAIN_OWNERSHIP_OWNER_ADDRS, w::TRANSFER_CHAIN_OWNERSHIP_INS,
                w::TRANSFER_CHAIN_OWNERSHIP_SIG_INDICES, w::TRANSFER_CHAIN_OWNERSHIP_MEMO,
            ]),
            ("RemoveChainValidator", [
                w::REMOVE_CHAIN_VALIDATOR_KIND, w::REMOVE_CHAIN_VALIDATOR_NETWORK_ID,
                w::REMOVE_CHAIN_VALIDATOR_BLOCKCHAIN_ID, w::REMOVE_CHAIN_VALIDATOR_OUTS,
                w::REMOVE_CHAIN_VALIDATOR_OWNER_ADDRS, w::REMOVE_CHAIN_VALIDATOR_INS,
                w::REMOVE_CHAIN_VALIDATOR_SIG_INDICES, w::REMOVE_CHAIN_VALIDATOR_MEMO,
            ]),
            ("AddValidator", [
                w::ADD_VALIDATOR_KIND, w::ADD_VALIDATOR_NETWORK_ID, w::ADD_VALIDATOR_BLOCKCHAIN_ID,
                w::ADD_VALIDATOR_OUTS, w::ADD_VALIDATOR_OWNER_ADDRS, w::ADD_VALIDATOR_INS,
                w::ADD_VALIDATOR_SIG_INDICES, w::ADD_VALIDATOR_MEMO,
            ]),
            ("AddDelegator", [
                w::ADD_DELEGATOR_KIND, w::ADD_DELEGATOR_NETWORK_ID, w::ADD_DELEGATOR_BLOCKCHAIN_ID,
                w::ADD_DELEGATOR_OUTS, w::ADD_DELEGATOR_OWNER_ADDRS, w::ADD_DELEGATOR_INS,
                w::ADD_DELEGATOR_SIG_INDICES, w::ADD_DELEGATOR_MEMO,
            ]),
            ("AddChainValidator", [
                w::ADD_CHAIN_VALIDATOR_KIND, w::ADD_CHAIN_VALIDATOR_NETWORK_ID,
                w::ADD_CHAIN_VALIDATOR_BLOCKCHAIN_ID, w::ADD_CHAIN_VALIDATOR_OUTS,
                w::ADD_CHAIN_VALIDATOR_OWNER_ADDRS, w::ADD_CHAIN_VALIDATOR_INS,
                w::ADD_CHAIN_VALIDATOR_SIG_INDICES, w::ADD_CHAIN_VALIDATOR_MEMO,
            ]),
            ("AddPermissionlessValidator", [
                w::ADD_PERMISSIONLESS_VALIDATOR_KIND, w::ADD_PERMISSIONLESS_VALIDATOR_NETWORK_ID,
                w::ADD_PERMISSIONLESS_VALIDATOR_BLOCKCHAIN_ID, w::ADD_PERMISSIONLESS_VALIDATOR_OUTS,
                w::ADD_PERMISSIONLESS_VALIDATOR_OWNER_ADDRS, w::ADD_PERMISSIONLESS_VALIDATOR_INS,
                w::ADD_PERMISSIONLESS_VALIDATOR_SIG_INDICES, w::ADD_PERMISSIONLESS_VALIDATOR_MEMO,
            ]),
            ("AddPermissionlessDelegator", [
                w::ADD_PERMISSIONLESS_DELEGATOR_KIND, w::ADD_PERMISSIONLESS_DELEGATOR_NETWORK_ID,
                w::ADD_PERMISSIONLESS_DELEGATOR_BLOCKCHAIN_ID, w::ADD_PERMISSIONLESS_DELEGATOR_OUTS,
                w::ADD_PERMISSIONLESS_DELEGATOR_OWNER_ADDRS, w::ADD_PERMISSIONLESS_DELEGATOR_INS,
                w::ADD_PERMISSIONLESS_DELEGATOR_SIG_INDICES, w::ADD_PERMISSIONLESS_DELEGATOR_MEMO,
            ]),
            ("IncreaseL1ValidatorBalance", [
                w::INCREASE_L1_VALIDATOR_BALANCE_KIND, w::INCREASE_L1_VALIDATOR_BALANCE_NETWORK_ID,
                w::INCREASE_L1_VALIDATOR_BALANCE_BLOCKCHAIN_ID, w::INCREASE_L1_VALIDATOR_BALANCE_OUTS,
                w::INCREASE_L1_VALIDATOR_BALANCE_OWNER_ADDRS, w::INCREASE_L1_VALIDATOR_BALANCE_INS,
                w::INCREASE_L1_VALIDATOR_BALANCE_SIG_INDICES, w::INCREASE_L1_VALIDATOR_BALANCE_MEMO,
            ]),
            ("DisableL1Validator", [
                w::DISABLE_L1_VALIDATOR_KIND, w::DISABLE_L1_VALIDATOR_NETWORK_ID,
                w::DISABLE_L1_VALIDATOR_BLOCKCHAIN_ID, w::DISABLE_L1_VALIDATOR_OUTS,
                w::DISABLE_L1_VALIDATOR_OWNER_ADDRS, w::DISABLE_L1_VALIDATOR_INS,
                w::DISABLE_L1_VALIDATOR_SIG_INDICES, w::DISABLE_L1_VALIDATOR_MEMO,
            ]),
            ("RegisterL1Validator", [
                w::REGISTER_L1_VALIDATOR_KIND, w::REGISTER_L1_VALIDATOR_NETWORK_ID,
                w::REGISTER_L1_VALIDATOR_BLOCKCHAIN_ID, w::REGISTER_L1_VALIDATOR_OUTS,
                w::REGISTER_L1_VALIDATOR_OWNER_ADDRS, w::REGISTER_L1_VALIDATOR_INS,
                w::REGISTER_L1_VALIDATOR_SIG_INDICES, w::REGISTER_L1_VALIDATOR_MEMO,
            ]),
            ("SetL1ValidatorWeight", [
                w::SET_L1_VALIDATOR_WEIGHT_KIND, w::SET_L1_VALIDATOR_WEIGHT_NETWORK_ID,
                w::SET_L1_VALIDATOR_WEIGHT_BLOCKCHAIN_ID, w::SET_L1_VALIDATOR_WEIGHT_OUTS,
                w::SET_L1_VALIDATOR_WEIGHT_OWNER_ADDRS, w::SET_L1_VALIDATOR_WEIGHT_INS,
                w::SET_L1_VALIDATOR_WEIGHT_SIG_INDICES, w::SET_L1_VALIDATOR_WEIGHT_MEMO,
            ]),
            ("TransformChain", [
                w::TRANSFORM_CHAIN_KIND, w::TRANSFORM_CHAIN_NETWORK_ID,
                w::TRANSFORM_CHAIN_BLOCKCHAIN_ID, w::TRANSFORM_CHAIN_OUTS,
                w::TRANSFORM_CHAIN_OWNER_ADDRS, w::TRANSFORM_CHAIN_INS,
                w::TRANSFORM_CHAIN_SIG_INDICES, w::TRANSFORM_CHAIN_MEMO,
            ]),
            ("CreateNetwork", [
                w::CREATE_NETWORK_KIND, w::CREATE_NETWORK_NETWORK_ID,
                w::CREATE_NETWORK_BLOCKCHAIN_ID, w::CREATE_NETWORK_OUTS,
                w::CREATE_NETWORK_OWNER_ADDRS, w::CREATE_NETWORK_INS,
                w::CREATE_NETWORK_SIG_INDICES, w::CREATE_NETWORK_MEMO,
            ]),
            ("ConvertNetwork", [
                w::CONVERT_NETWORK_KIND, w::CONVERT_NETWORK_NETWORK_ID,
                w::CONVERT_NETWORK_BLOCKCHAIN_ID, w::CONVERT_NETWORK_OUTS,
                w::CONVERT_NETWORK_OWNER_ADDRS, w::CONVERT_NETWORK_INS,
                w::CONVERT_NETWORK_SIG_INDICES, w::CONVERT_NETWORK_MEMO,
            ]),
            // Base against itself, so a corrupted BASE constant cannot make
            // the whole table pass by moving the thing it is compared to.
            ("Base", BASE),
            // And the one kind that has NO envelope, to say so: it opens with
            // the kind byte and then goes its own way.
            ("RewardValidator", [
                w::REWARD_VALIDATOR_KIND, BASE[1], BASE[2], BASE[3],
                BASE[4], BASE[5], BASE[6], BASE[7],
            ]),
        ];

        for (name, offsets) in kinds {
            assert_eq!(offsets, BASE, "{name} does not open with Base's envelope");
        }

        // The kind byte is where every reader looks first, and it is the same
        // byte for a transaction with no envelope at all.
        assert_eq!(w::REWARD_VALIDATOR_KIND, w::BASE_KIND);
    }

    #[test]
    fn a_transaction_that_spends_nothing_is_shorter_than_one_that_does() {
        // RewardValidator is the one kind with no envelope: 1 + 32 and done.
        // If it ever grew to the envelope's size, the shape would be a lie.
        assert!(w::REWARD_VALIDATOR_SIZE < w::BASE_SIZE);
        assert_eq!(w::REWARD_VALIDATOR_SIZE, w::REWARD_VALIDATOR_STAKER_TX_ID + 32);
    }
}
