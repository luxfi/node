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
    write_addrs, Credential, Input, Output, Owners, UtxoId,
};
use crate::ids::{hash256, Id, NodeId, ShortId, PRIMARY_NETWORK_ID};
use crate::signer::Signer;
use crate::zap;

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

const OFF_KIND: usize = 0;
const OFF_NETWORK_ID: usize = 1;
const OFF_BLOCKCHAIN_ID: usize = 5;
const OFF_OUTS: usize = 37;
const OFF_OWNER_ADDRS: usize = 45;
const OFF_INS: usize = 53;
const OFF_SIG_INDICES: usize = 61;
const OFF_MEMO: usize = 69;
const SPEND_SIZE: usize = 77;

// One output, inline in the output list.
const OUT_ASSET: usize = 0;
const OUT_STAKE_LOCK: usize = 32;
const OUT_AMOUNT: usize = 40;
const OUT_THRESHOLD: usize = 48;
const OUT_OWNER_LOCK: usize = 52;
const OUT_ADDR_START: usize = 60;
const OUT_ADDR_COUNT: usize = 64;
const OUT_STRIDE: usize = 72;

// One input, inline in the input list.
const IN_TX_ID: usize = 0;
const IN_OUTPUT_INDEX: usize = 32;
const IN_ASSET: usize = 36;
const IN_STAKE_LOCK: usize = 68;
const IN_AMOUNT: usize = 76;
const IN_SIG_START: usize = 84;
const IN_SIG_COUNT: usize = 88;
const IN_STRIDE: usize = 96;

const ADDR_STRIDE: usize = 20;
const SIG_STRIDE: usize = 4;
const SIG_LEN: usize = 65;
const ID_STRIDE: usize = 32;

/// The 44 bytes that say who is staking, for how long, and how much.
const VALIDATOR_SIZE: usize = 44;

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

// ---- writing ----

struct SpendPtrs {
    outs: (usize, usize),
    addrs: (usize, usize),
    ins: (usize, usize),
    sigs: (usize, usize),
}

fn write_outputs(b: &mut zap::Builder, outs: &[Output]) -> ((usize, usize), (usize, usize)) {
    if outs.is_empty() {
        return ((0, 0), (0, 0));
    }
    let mut addrs: Vec<ShortId> = Vec::new();
    let mut lb = b.start_list();
    for o in outs {
        let mut e = [0u8; OUT_STRIDE];
        e[OUT_ASSET..OUT_ASSET + 32].copy_from_slice(&o.asset);
        e[OUT_STAKE_LOCK..OUT_STAKE_LOCK + 8].copy_from_slice(&o.stake_lock.to_le_bytes());
        e[OUT_AMOUNT..OUT_AMOUNT + 8].copy_from_slice(&o.amount.to_le_bytes());
        e[OUT_THRESHOLD..OUT_THRESHOLD + 4].copy_from_slice(&o.owners.threshold.to_le_bytes());
        e[OUT_OWNER_LOCK..OUT_OWNER_LOCK + 8].copy_from_slice(&o.owners.locktime.to_le_bytes());
        e[OUT_ADDR_START..OUT_ADDR_START + 4].copy_from_slice(&(addrs.len() as u32).to_le_bytes());
        e[OUT_ADDR_COUNT..OUT_ADDR_COUNT + 4]
            .copy_from_slice(&(o.owners.addrs.len() as u32).to_le_bytes());
        b.list_bytes(&mut lb, &e);
        addrs.extend_from_slice(&o.owners.addrs);
    }
    let list = (lb.offset(), outs.len());
    let addr_list = write_addrs(b, &addrs);
    (list, addr_list)
}

fn write_inputs(b: &mut zap::Builder, ins: &[Input]) -> ((usize, usize), (usize, usize)) {
    if ins.is_empty() {
        return ((0, 0), (0, 0));
    }
    let mut sigs: Vec<u32> = Vec::new();
    let mut lb = b.start_list();
    for i in ins {
        let mut e = [0u8; IN_STRIDE];
        e[IN_TX_ID..IN_TX_ID + 32].copy_from_slice(&i.utxo.tx_id);
        e[IN_OUTPUT_INDEX..IN_OUTPUT_INDEX + 4].copy_from_slice(&i.utxo.output_index.to_le_bytes());
        e[IN_ASSET..IN_ASSET + 32].copy_from_slice(&i.asset);
        e[IN_STAKE_LOCK..IN_STAKE_LOCK + 8].copy_from_slice(&i.stake_lock.to_le_bytes());
        e[IN_AMOUNT..IN_AMOUNT + 8].copy_from_slice(&i.amount.to_le_bytes());
        e[IN_SIG_START..IN_SIG_START + 4].copy_from_slice(&(sigs.len() as u32).to_le_bytes());
        e[IN_SIG_COUNT..IN_SIG_COUNT + 4]
            .copy_from_slice(&(i.sig_indices.len() as u32).to_le_bytes());
        b.list_bytes(&mut lb, &e);
        sigs.extend_from_slice(&i.sig_indices);
    }
    let list = (lb.offset(), ins.len());
    let sig_list = write_u32_list(b, &sigs);
    (list, sig_list)
}

fn write_u32_list(b: &mut zap::Builder, xs: &[u32]) -> (usize, usize) {
    if xs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for x in xs {
        b.list_u32(&mut lb, *x);
    }
    (lb.offset(), lb.count())
}

fn write_id_list(b: &mut zap::Builder, xs: &[Id]) -> (usize, usize) {
    if xs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for x in xs {
        b.list_bytes(&mut lb, x);
    }
    (lb.offset(), xs.len())
}

fn write_spending(b: &mut zap::Builder, base: &Envelope) -> SpendPtrs {
    let (outs, addrs) = write_outputs(b, &base.outs);
    let (ins, sigs) = write_inputs(b, &base.ins);
    SpendPtrs {
        outs,
        addrs,
        ins,
        sigs,
    }
}

fn set_envelope(
    b: &mut zap::Builder,
    ob: &zap::ObjectBuilder,
    k: Kind,
    base: &Envelope,
    p: &SpendPtrs,
) {
    b.set_u8(ob, OFF_KIND, k as u8);
    b.set_u32(ob, OFF_NETWORK_ID, base.network_id);
    b.set_bytes_fixed(ob, OFF_BLOCKCHAIN_ID, &base.blockchain_id);
    b.set_list(ob, OFF_OUTS, p.outs.0, p.outs.1);
    b.set_list(ob, OFF_OWNER_ADDRS, p.addrs.0, p.addrs.1);
    b.set_list(ob, OFF_INS, p.ins.0, p.ins.1);
    b.set_list(ob, OFF_SIG_INDICES, p.sigs.0, p.sigs.1);
    b.set_bytes(ob, OFF_MEMO, &base.memo);
}

fn set_validator(b: &mut zap::Builder, ob: &zap::ObjectBuilder, off: usize, v: &Validator) {
    b.set_bytes_fixed(ob, off, &v.node_id.0);
    b.set_u64(ob, off + 20, v.start);
    b.set_u64(ob, off + 28, v.end);
    b.set_u64(ob, off + 36, v.weight);
}

fn set_owner(
    b: &mut zap::Builder,
    ob: &zap::ObjectBuilder,
    threshold_off: usize,
    locktime_off: usize,
    addrs_off: usize,
    o: &Owners,
    written: (usize, usize),
) {
    b.set_u32(ob, threshold_off, o.threshold);
    b.set_u64(ob, locktime_off, o.locktime);
    b.set_list(ob, addrs_off, written.0, written.1);
}

// ---- reading ----

fn read_outputs(o: zap::Object<'_>, list_off: usize, addr_off: usize) -> Vec<Output> {
    let list = o.list(list_off, OUT_STRIDE);
    let addrs = o.list(addr_off, ADDR_STRIDE);
    (0..list.len())
        .map(|i| {
            let e = list.object(i, OUT_STRIDE);
            Output {
                asset: e.id(OUT_ASSET),
                stake_lock: e.u64(OUT_STAKE_LOCK),
                amount: e.u64(OUT_AMOUNT),
                owners: Owners {
                    locktime: e.u64(OUT_OWNER_LOCK),
                    threshold: e.u32(OUT_THRESHOLD),
                    addrs: slice_addrs(addrs, e.u32(OUT_ADDR_START), e.u32(OUT_ADDR_COUNT)),
                },
            }
        })
        .collect()
}

fn read_inputs(o: zap::Object<'_>, list_off: usize, sig_off: usize) -> Vec<Input> {
    let list = o.list(list_off, IN_STRIDE);
    let sigs = o.list(sig_off, SIG_STRIDE);
    (0..list.len())
        .map(|i| {
            let e = list.object(i, IN_STRIDE);
            Input {
                utxo: UtxoId {
                    tx_id: e.id(IN_TX_ID),
                    output_index: e.u32(IN_OUTPUT_INDEX),
                },
                asset: e.id(IN_ASSET),
                stake_lock: e.u64(IN_STAKE_LOCK),
                amount: e.u64(IN_AMOUNT),
                sig_indices: slice_sigs(sigs, e.u32(IN_SIG_START), e.u32(IN_SIG_COUNT)),
            }
        })
        .collect()
}

fn read_envelope(o: zap::Object<'_>) -> Envelope {
    Envelope {
        network_id: o.u32(OFF_NETWORK_ID),
        blockchain_id: o.id(OFF_BLOCKCHAIN_ID),
        outs: read_outputs(o, OFF_OUTS, OFF_OWNER_ADDRS),
        ins: read_inputs(o, OFF_INS, OFF_SIG_INDICES),
        memo: o.bytes(OFF_MEMO).to_vec(),
    }
}

fn read_validator(o: zap::Object<'_>, off: usize) -> Validator {
    Validator {
        node_id: NodeId(o.short_id(off)),
        start: o.u64(off + 20),
        end: o.u64(off + 28),
        weight: o.u64(off + 36),
    }
}

fn read_owner(
    o: zap::Object<'_>,
    threshold_off: usize,
    locktime_off: usize,
    addrs_off: usize,
) -> Owners {
    Owners {
        locktime: o.u64(locktime_off),
        threshold: o.u32(threshold_off),
        addrs: read_addrs(o.list(addrs_off, ADDR_STRIDE)),
    }
}

fn read_u32_list(o: zap::Object<'_>, off: usize) -> Vec<u32> {
    let l = o.list(off, SIG_STRIDE);
    (0..l.len()).map(|i| l.u32(i)).collect()
}

fn read_id_list(o: zap::Object<'_>, off: usize) -> Vec<Id> {
    let l = o.list(off, ID_STRIDE);
    (0..l.len()).map(|i| l.object(i, ID_STRIDE).id(0)).collect()
}

// ---- per-kind wire layouts ----

// AddValidator
const OFF_AV_VALIDATOR: usize = SPEND_SIZE;
const OFF_AV_STAKE_OUTS: usize = OFF_AV_VALIDATOR + VALIDATOR_SIZE;
const OFF_AV_STAKE_ADDRS: usize = OFF_AV_STAKE_OUTS + 8;
const OFF_AV_REWARDS_THRESHOLD: usize = OFF_AV_STAKE_ADDRS + 8;
const OFF_AV_REWARDS_LOCKTIME: usize = OFF_AV_REWARDS_THRESHOLD + 4;
const OFF_AV_REWARDS_ADDRS: usize = OFF_AV_REWARDS_LOCKTIME + 8;
const OFF_AV_DELEGATION_SHARES: usize = OFF_AV_REWARDS_ADDRS + 8;
const SIZE_ADD_VALIDATOR: usize = OFF_AV_DELEGATION_SHARES + 4;

// AddDelegator
const OFF_AD_VALIDATOR: usize = SPEND_SIZE;
const OFF_AD_STAKE_OUTS: usize = OFF_AD_VALIDATOR + VALIDATOR_SIZE;
const OFF_AD_STAKE_ADDRS: usize = OFF_AD_STAKE_OUTS + 8;
const OFF_AD_REWARDS_THRESHOLD: usize = OFF_AD_STAKE_ADDRS + 8;
const OFF_AD_REWARDS_LOCKTIME: usize = OFF_AD_REWARDS_THRESHOLD + 4;
const OFF_AD_REWARDS_ADDRS: usize = OFF_AD_REWARDS_LOCKTIME + 8;
const SIZE_ADD_DELEGATOR: usize = OFF_AD_REWARDS_ADDRS + 8;

// AddChainValidator
const OFF_ACV_VALIDATOR: usize = SPEND_SIZE;
const OFF_ACV_CHAIN: usize = OFF_ACV_VALIDATOR + VALIDATOR_SIZE;
const OFF_ACV_CHAIN_AUTH: usize = OFF_ACV_CHAIN + 32;
const SIZE_ADD_CHAIN_VALIDATOR: usize = OFF_ACV_CHAIN_AUTH + 8;

// AddPermissionlessValidator
const OFF_APV_VALIDATOR: usize = SPEND_SIZE;
const OFF_APV_CHAIN: usize = OFF_APV_VALIDATOR + VALIDATOR_SIZE;
const OFF_APV_SIGNER: usize = OFF_APV_CHAIN + 32;
const OFF_APV_STAKE_OUTS: usize = OFF_APV_SIGNER + crate::signer::SIGNER_SIZE;
const OFF_APV_STAKE_ADDRS: usize = OFF_APV_STAKE_OUTS + 8;
const OFF_APV_VAL_REWARDS_THRESHOLD: usize = OFF_APV_STAKE_ADDRS + 8;
const OFF_APV_VAL_REWARDS_LOCKTIME: usize = OFF_APV_VAL_REWARDS_THRESHOLD + 4;
const OFF_APV_VAL_REWARDS_ADDRS: usize = OFF_APV_VAL_REWARDS_LOCKTIME + 8;
const OFF_APV_DEL_REWARDS_THRESHOLD: usize = OFF_APV_VAL_REWARDS_ADDRS + 8;
const OFF_APV_DEL_REWARDS_LOCKTIME: usize = OFF_APV_DEL_REWARDS_THRESHOLD + 4;
const OFF_APV_DEL_REWARDS_ADDRS: usize = OFF_APV_DEL_REWARDS_LOCKTIME + 8;
const OFF_APV_DELEGATION_SHARES: usize = OFF_APV_DEL_REWARDS_ADDRS + 8;
const SIZE_ADD_PERMISSIONLESS_VALIDATOR: usize = OFF_APV_DELEGATION_SHARES + 4;

// AddPermissionlessDelegator
const OFF_APD_VALIDATOR: usize = SPEND_SIZE;
const OFF_APD_CHAIN: usize = OFF_APD_VALIDATOR + VALIDATOR_SIZE;
const OFF_APD_STAKE_OUTS: usize = OFF_APD_CHAIN + 32;
const OFF_APD_STAKE_ADDRS: usize = OFF_APD_STAKE_OUTS + 8;
const OFF_APD_REWARDS_THRESHOLD: usize = OFF_APD_STAKE_ADDRS + 8;
const OFF_APD_REWARDS_LOCKTIME: usize = OFF_APD_REWARDS_THRESHOLD + 4;
const OFF_APD_REWARDS_ADDRS: usize = OFF_APD_REWARDS_LOCKTIME + 8;
const SIZE_ADD_PERMISSIONLESS_DELEGATOR: usize = OFF_APD_REWARDS_ADDRS + 8;

// CreateChain
const OFF_CC_CHAIN: usize = SPEND_SIZE;
const OFF_CC_VM_ID: usize = 109;
const OFF_CC_NAME: usize = 141;
const OFF_CC_FX_IDS: usize = 149;
const OFF_CC_GENESIS: usize = 157;
const OFF_CC_AUTH: usize = 165;
const SIZE_CREATE_CHAIN: usize = 173;

// Import
const OFF_IMPORT_SOURCE_CHAIN: usize = SPEND_SIZE;
const OFF_IMPORT_INPUTS: usize = 109;
const OFF_IMPORT_SIG_INDICES: usize = 117;
const SIZE_IMPORT: usize = 125;

// Export
const OFF_EXPORT_DEST_CHAIN: usize = SPEND_SIZE;
const OFF_EXPORT_OUTPUTS: usize = 109;
const OFF_EXPORT_ADDRS: usize = 117;
const SIZE_EXPORT: usize = 125;

// RemoveChainValidator
const OFF_REMOVE_NODE_ID: usize = SPEND_SIZE;
const OFF_REMOVE_CHAIN: usize = 97;
const OFF_REMOVE_CHAIN_AUTH: usize = 129;
const SIZE_REMOVE: usize = 137;

// TransferChainOwnership
const OFF_TCO_CHAIN: usize = SPEND_SIZE;
const OFF_TCO_CHAIN_AUTH: usize = 109;
const OFF_TCO_OWNER_THRESHOLD: usize = 117;
const OFF_TCO_OWNER_LOCKTIME: usize = 121;
const OFF_TCO_OWNER_ADDRS: usize = 129;
const SIZE_TRANSFER_CHAIN_OWNERSHIP: usize = 137;

// IncreaseL1ValidatorBalance
const OFF_INCREASE_VALIDATION_ID: usize = SPEND_SIZE;
const OFF_INCREASE_BALANCE: usize = 109;
const SIZE_INCREASE: usize = 117;

// DisableL1Validator
const OFF_DISABLE_VALIDATION_ID: usize = SPEND_SIZE;
const OFF_DISABLE_AUTH: usize = 109;
const SIZE_DISABLE: usize = 117;

// RewardValidator
const OFF_REWARD_TX_ID: usize = 1;
const SIZE_REWARD: usize = 33;

// A genesis validator, inline at a fixed stride. The node id and the two
// owners' addresses do not fit a fixed slot, so each entry carries a run into
// one transaction-wide blob and one transaction-wide address array — the same
// shape the envelope uses for its outputs' owners.
const NV_WEIGHT: usize = 0;
const NV_BALANCE: usize = 8;
const NV_SIGNER_PUB: usize = 16;
const NV_SIGNER_POP: usize = 64;
const NV_NODE_ID_START: usize = 160;
const NV_NODE_ID_LEN: usize = 164;
const NV_REM_THRESHOLD: usize = 168;
const NV_REM_ADDR_START: usize = 172;
const NV_REM_ADDR_COUNT: usize = 176;
const NV_DEAC_THRESHOLD: usize = 180;
const NV_DEAC_ADDR_START: usize = 184;
const NV_DEAC_ADDR_COUNT: usize = 188;
const NV_STRIDE: usize = 192;

// CreateNetwork
const OFF_CN_PARENT: usize = SPEND_SIZE;
const OFF_CN_OWNER_THRESHOLD: usize = SPEND_SIZE + 32;
const OFF_CN_OWNER_LOCKTIME: usize = SPEND_SIZE + 36;
const OFF_CN_OWNER_ADDRS: usize = SPEND_SIZE + 44;
const OFF_CN_RESTAKE_PARENT: usize = SPEND_SIZE + 52;
const OFF_CN_ADMISSION: usize = SPEND_SIZE + 53;
const OFF_CN_MANAGER: usize = SPEND_SIZE + 54;
const OFF_CN_THRESHOLD: usize = SPEND_SIZE + 55;
const OFF_CN_VALIDATORS: usize = SPEND_SIZE + 63;
const OFF_CN_NODE_ID_POOL: usize = SPEND_SIZE + 71;
const OFF_CN_ADDR_POOL: usize = SPEND_SIZE + 79;
const OFF_CN_MANAGER_CHAIN_ID: usize = SPEND_SIZE + 87;
const OFF_CN_MANAGER_ADDRESS: usize = SPEND_SIZE + 119;
const SIZE_CREATE_NETWORK: usize = SPEND_SIZE + 127;

// ConvertNetwork
const OFF_CV_NETWORK: usize = SPEND_SIZE;
const OFF_CV_PARENT: usize = SPEND_SIZE + 32;
const OFF_CV_MANAGER_CHAIN_ID: usize = SPEND_SIZE + 64;
const OFF_CV_MANAGER_ADDRESS: usize = SPEND_SIZE + 96;
const OFF_CV_VALIDATORS: usize = SPEND_SIZE + 104;
const OFF_CV_NODE_ID_POOL: usize = SPEND_SIZE + 112;
const OFF_CV_ADDR_POOL: usize = SPEND_SIZE + 120;
const OFF_CV_AUTH: usize = SPEND_SIZE + 128;
const OFF_CV_RESTAKE_PARENT: usize = SPEND_SIZE + 136;
const OFF_CV_ADMISSION: usize = SPEND_SIZE + 137;
const OFF_CV_MANAGER: usize = SPEND_SIZE + 138;
const OFF_CV_THRESHOLD: usize = SPEND_SIZE + 139;
const SIZE_CONVERT_NETWORK: usize = SPEND_SIZE + 147;

// TransformChain
const OFF_TC_CHAIN: usize = SPEND_SIZE;
const OFF_TC_ASSET_ID: usize = 109;
const OFF_TC_INITIAL_SUPPLY: usize = 141;
const OFF_TC_MAXIMUM_SUPPLY: usize = 149;
const OFF_TC_MIN_CONSUMPTION_RATE: usize = 157;
const OFF_TC_MAX_CONSUMPTION_RATE: usize = 165;
const OFF_TC_MIN_VALIDATOR_STAKE: usize = 173;
const OFF_TC_MAX_VALIDATOR_STAKE: usize = 181;
const OFF_TC_MIN_STAKE_DURATION: usize = 189;
const OFF_TC_MAX_STAKE_DURATION: usize = 193;
const OFF_TC_MIN_DELEGATION_FEE: usize = 197;
const OFF_TC_MIN_DELEGATOR_STAKE: usize = 201;
const OFF_TC_MAX_VALIDATOR_WEIGHT_FACTOR: usize = 209;
const OFF_TC_UPTIME_REQUIREMENT: usize = 210;
const OFF_TC_CHAIN_AUTH: usize = 214;
const SIZE_TRANSFORM_CHAIN: usize = 222;

// RegisterL1Validator
const OFF_RL_BALANCE: usize = SPEND_SIZE;
const OFF_RL_POP: usize = 85;
const OFF_RL_MESSAGE: usize = 181;
const SIZE_REGISTER_L1_VALIDATOR: usize = 189;

// SetL1ValidatorWeight
const OFF_SW_MESSAGE: usize = SPEND_SIZE;
const SIZE_SET_L1_VALIDATOR_WEIGHT: usize = 85;

/// Write a genesis set into the variable section.
///
/// Returns the list pointer plus the two pools the entries slice into. The
/// caller writes those pools, because where they live is per-transaction: the
/// two kinds that carry a genesis set write their other variable fields in a
/// different order, and the order is the wire.
fn write_network_validators(
    b: &mut zap::Builder,
    vdrs: &[NetworkValidator],
) -> ((usize, usize), Vec<u8>, Vec<ShortId>) {
    if vdrs.is_empty() {
        return ((0, 0), Vec::new(), Vec::new());
    }
    let mut node_ids: Vec<u8> = Vec::new();
    let mut addrs: Vec<ShortId> = Vec::new();
    let mut lb = b.start_list();
    for v in vdrs {
        let mut e = [0u8; NV_STRIDE];
        e[NV_WEIGHT..NV_WEIGHT + 8].copy_from_slice(&v.weight.to_le_bytes());
        e[NV_BALANCE..NV_BALANCE + 8].copy_from_slice(&v.balance.to_le_bytes());
        let (pk, pop) = match v.signer {
            Signer::ProofOfPossession { public_key, proof } => (public_key, proof),
            Signer::Empty => (
                [0u8; crate::signer::PUBLIC_KEY_LEN],
                [0u8; crate::signer::SIGNATURE_LEN],
            ),
        };
        e[NV_SIGNER_PUB..NV_SIGNER_PUB + crate::signer::PUBLIC_KEY_LEN].copy_from_slice(&pk);
        e[NV_SIGNER_POP..NV_SIGNER_POP + crate::signer::SIGNATURE_LEN].copy_from_slice(&pop);
        e[NV_NODE_ID_START..NV_NODE_ID_START + 4]
            .copy_from_slice(&(node_ids.len() as u32).to_le_bytes());
        e[NV_NODE_ID_LEN..NV_NODE_ID_LEN + 4]
            .copy_from_slice(&(v.node_id.len() as u32).to_le_bytes());
        node_ids.extend_from_slice(&v.node_id);
        e[NV_REM_THRESHOLD..NV_REM_THRESHOLD + 4]
            .copy_from_slice(&v.remaining_balance_owner.threshold.to_le_bytes());
        e[NV_REM_ADDR_START..NV_REM_ADDR_START + 4]
            .copy_from_slice(&(addrs.len() as u32).to_le_bytes());
        e[NV_REM_ADDR_COUNT..NV_REM_ADDR_COUNT + 4]
            .copy_from_slice(&(v.remaining_balance_owner.addresses.len() as u32).to_le_bytes());
        addrs.extend_from_slice(&v.remaining_balance_owner.addresses);
        e[NV_DEAC_THRESHOLD..NV_DEAC_THRESHOLD + 4]
            .copy_from_slice(&v.deactivation_owner.threshold.to_le_bytes());
        e[NV_DEAC_ADDR_START..NV_DEAC_ADDR_START + 4]
            .copy_from_slice(&(addrs.len() as u32).to_le_bytes());
        e[NV_DEAC_ADDR_COUNT..NV_DEAC_ADDR_COUNT + 4]
            .copy_from_slice(&(v.deactivation_owner.addresses.len() as u32).to_le_bytes());
        addrs.extend_from_slice(&v.deactivation_owner.addresses);
        b.list_bytes(&mut lb, &e);
    }
    ((lb.offset(), vdrs.len()), node_ids, addrs)
}

/// A fixed blob out of an object's payload, short-read as zeros.
///
/// Missing bytes read as zero for the same reason [`zap::Object::id`] does:
/// a truncated buffer must decode to a value that fails verification, not to
/// a panic — a hostile transaction that could crash the reader would never
/// reach the check that refuses it.
fn read_public_key(o: zap::Object<'_>, off: usize) -> [u8; crate::signer::PUBLIC_KEY_LEN] {
    let mut out = [0u8; crate::signer::PUBLIC_KEY_LEN];
    let src = o.bytes_fixed(off, crate::signer::PUBLIC_KEY_LEN);
    out[..src.len()].copy_from_slice(src);
    out
}

fn read_proof(o: zap::Object<'_>, off: usize) -> [u8; crate::signer::SIGNATURE_LEN] {
    let mut out = [0u8; crate::signer::SIGNATURE_LEN];
    let src = o.bytes_fixed(off, crate::signer::SIGNATURE_LEN);
    out[..src.len()].copy_from_slice(src);
    out
}

fn read_network_validators(
    o: zap::Object<'_>,
    list_off: usize,
    node_id_pool_off: usize,
    addr_pool_off: usize,
) -> Vec<NetworkValidator> {
    let list = o.list(list_off, NV_STRIDE);
    let blob = o.bytes(node_id_pool_off);
    let addrs = o.list(addr_pool_off, ADDR_STRIDE);
    (0..list.len())
        .map(|i| {
            let e = list.object(i, NV_STRIDE);
            let public_key = read_public_key(e, NV_SIGNER_PUB);
            let proof = read_proof(e, NV_SIGNER_POP);
            let (start, len) = (
                e.u32(NV_NODE_ID_START) as usize,
                e.u32(NV_NODE_ID_LEN) as usize,
            );
            let node_id = if len > 0 && start + len <= blob.len() {
                blob[start..start + len].to_vec()
            } else {
                Vec::new()
            };
            NetworkValidator {
                node_id,
                weight: e.u64(NV_WEIGHT),
                balance: e.u64(NV_BALANCE),
                signer: Signer::ProofOfPossession { public_key, proof },
                remaining_balance_owner: PChainOwner {
                    threshold: e.u32(NV_REM_THRESHOLD),
                    addresses: slice_addrs(
                        addrs,
                        e.u32(NV_REM_ADDR_START),
                        e.u32(NV_REM_ADDR_COUNT),
                    ),
                },
                deactivation_owner: PChainOwner {
                    threshold: e.u32(NV_DEAC_THRESHOLD),
                    addresses: slice_addrs(
                        addrs,
                        e.u32(NV_DEAC_ADDR_START),
                        e.u32(NV_DEAC_ADDR_COUNT),
                    ),
                },
            }
        })
        .collect()
}

/// The two security axes, across four fixed fields. Shared by both network
/// transactions — one encoding, written once.
fn set_security(
    b: &mut zap::Builder,
    ob: &zap::ObjectBuilder,
    restake_off: usize,
    admission_off: usize,
    manager_off: usize,
    threshold_off: usize,
    m: &crate::security::Mode,
) {
    b.set_u8(ob, restake_off, u8::from(m.restake_parent));
    b.set_u8(ob, admission_off, m.admission as u8);
    b.set_u8(ob, manager_off, m.manager as u8);
    b.set_u64(ob, threshold_off, m.threshold);
}

/// Read the two axes back.
///
/// A byte that names no admission or manager is carried as the refusal it will
/// become: the mode is returned with the closest safe reading and
/// [`crate::security::Mode::valid`] is what refuses it, exactly as Go leaves
/// the check to `Mode.Valid`. Returning an error here instead would make a
/// malformed byte unparseable rather than invalid, and a transaction that
/// cannot be parsed cannot be reported on.
fn read_security(
    o: zap::Object<'_>,
    restake_off: usize,
    admission_off: usize,
    manager_off: usize,
    threshold_off: usize,
) -> Result<crate::security::Mode, Error> {
    let admission = crate::security::Admission::from_u8(o.u8(admission_off))
        .map_err(|v| Error::Security(crate::security::Error::UnknownAdmission(v)))?;
    let manager = crate::security::Manager::from_u8(o.u8(manager_off))
        .map_err(|v| Error::Security(crate::security::Error::UnknownManager(v)))?;
    Ok(crate::security::Mode {
        restake_parent: o.u8(restake_off) != 0,
        admission,
        manager,
        threshold: o.u64(threshold_off),
    })
}

impl Unsigned {
    /// The transaction's bytes.
    ///
    /// This is construction, not serialization: nothing is cached and nothing
    /// is re-encoded later. The bytes a signature covers are these.
    pub fn to_bytes(&self) -> Vec<u8> {
        match self {
            Unsigned::RewardValidator { staker_tx_id } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 16 + SIZE_REWARD);
                let ob = b.start_object(SIZE_REWARD);
                b.set_u8(&ob, OFF_KIND, Kind::RewardValidator as u8);
                b.set_bytes_fixed(&ob, OFF_REWARD_TX_ID, staker_tx_id);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::Base(base) => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 256 + SPEND_SIZE);
                let p = write_spending(&mut b, base);
                let ob = b.start_object(SPEND_SIZE);
                set_envelope(&mut b, &ob, Kind::Base, base, &p);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::Import {
                base,
                source_chain,
                imported,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_IMPORT);
                let p = write_spending(&mut b, base);
                let (ins, sigs) = write_inputs(&mut b, imported);
                let ob = b.start_object(SIZE_IMPORT);
                set_envelope(&mut b, &ob, Kind::Import, base, &p);
                b.set_bytes_fixed(&ob, OFF_IMPORT_SOURCE_CHAIN, source_chain);
                b.set_list(&ob, OFF_IMPORT_INPUTS, ins.0, ins.1);
                b.set_list(&ob, OFF_IMPORT_SIG_INDICES, sigs.0, sigs.1);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::Export {
                base,
                destination_chain,
                exported,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_EXPORT);
                let p = write_spending(&mut b, base);
                let (outs, addrs) = write_outputs(&mut b, exported);
                let ob = b.start_object(SIZE_EXPORT);
                set_envelope(&mut b, &ob, Kind::Export, base, &p);
                b.set_bytes_fixed(&ob, OFF_EXPORT_DEST_CHAIN, destination_chain);
                b.set_list(&ob, OFF_EXPORT_OUTPUTS, outs.0, outs.1);
                b.set_list(&ob, OFF_EXPORT_ADDRS, addrs.0, addrs.1);
                b.finish_as_root(&ob);
                b.finish()
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
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_CREATE_CHAIN);
                let p = write_spending(&mut b, base);
                let fx = write_id_list(&mut b, fx_ids);
                let auth = write_u32_list(&mut b, chain_auth);
                let ob = b.start_object(SIZE_CREATE_CHAIN);
                set_envelope(&mut b, &ob, Kind::CreateChain, base, &p);
                b.set_bytes_fixed(&ob, OFF_CC_CHAIN, chain);
                b.set_bytes_fixed(&ob, OFF_CC_VM_ID, vm_id);
                b.set_list(&ob, OFF_CC_FX_IDS, fx.0, fx.1);
                b.set_list(&ob, OFF_CC_AUTH, auth.0, auth.1);
                b.set_bytes(&ob, OFF_CC_NAME, name.as_bytes());
                b.set_bytes(&ob, OFF_CC_GENESIS, genesis);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::AddValidator {
                base,
                validator,
                stake,
                rewards_owner,
                delegation_shares,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_ADD_VALIDATOR);
                let p = write_spending(&mut b, base);
                let (souts, saddrs) = write_outputs(&mut b, stake);
                let owner_addrs = write_addrs(&mut b, &rewards_owner.addrs);
                let ob = b.start_object(SIZE_ADD_VALIDATOR);
                set_envelope(&mut b, &ob, Kind::AddValidator, base, &p);
                set_validator(&mut b, &ob, OFF_AV_VALIDATOR, validator);
                b.set_list(&ob, OFF_AV_STAKE_OUTS, souts.0, souts.1);
                b.set_list(&ob, OFF_AV_STAKE_ADDRS, saddrs.0, saddrs.1);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_AV_REWARDS_THRESHOLD,
                    OFF_AV_REWARDS_LOCKTIME,
                    OFF_AV_REWARDS_ADDRS,
                    rewards_owner,
                    owner_addrs,
                );
                b.set_u32(&ob, OFF_AV_DELEGATION_SHARES, *delegation_shares);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::AddDelegator {
                base,
                validator,
                stake,
                rewards_owner,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_ADD_DELEGATOR);
                let p = write_spending(&mut b, base);
                let (souts, saddrs) = write_outputs(&mut b, stake);
                let owner_addrs = write_addrs(&mut b, &rewards_owner.addrs);
                let ob = b.start_object(SIZE_ADD_DELEGATOR);
                set_envelope(&mut b, &ob, Kind::AddDelegator, base, &p);
                set_validator(&mut b, &ob, OFF_AD_VALIDATOR, validator);
                b.set_list(&ob, OFF_AD_STAKE_OUTS, souts.0, souts.1);
                b.set_list(&ob, OFF_AD_STAKE_ADDRS, saddrs.0, saddrs.1);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_AD_REWARDS_THRESHOLD,
                    OFF_AD_REWARDS_LOCKTIME,
                    OFF_AD_REWARDS_ADDRS,
                    rewards_owner,
                    owner_addrs,
                );
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::AddChainValidator {
                base,
                validator,
                chain,
                chain_auth,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_ADD_CHAIN_VALIDATOR);
                let p = write_spending(&mut b, base);
                let auth = write_u32_list(&mut b, chain_auth);
                let ob = b.start_object(SIZE_ADD_CHAIN_VALIDATOR);
                set_envelope(&mut b, &ob, Kind::AddChainValidator, base, &p);
                set_validator(&mut b, &ob, OFF_ACV_VALIDATOR, validator);
                b.set_bytes_fixed(&ob, OFF_ACV_CHAIN, chain);
                b.set_list(&ob, OFF_ACV_CHAIN_AUTH, auth.0, auth.1);
                b.finish_as_root(&ob);
                b.finish()
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
                let mut b =
                    zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_ADD_PERMISSIONLESS_VALIDATOR);
                let p = write_spending(&mut b, base);
                let (souts, saddrs) = write_outputs(&mut b, stake);
                let val_addrs = write_addrs(&mut b, &validator_rewards_owner.addrs);
                let del_addrs = write_addrs(&mut b, &delegator_rewards_owner.addrs);
                let ob = b.start_object(SIZE_ADD_PERMISSIONLESS_VALIDATOR);
                set_envelope(&mut b, &ob, Kind::AddPermissionlessValidator, base, &p);
                set_validator(&mut b, &ob, OFF_APV_VALIDATOR, validator);
                b.set_bytes_fixed(&ob, OFF_APV_CHAIN, chain);
                signer.write(&mut b, &ob, OFF_APV_SIGNER);
                b.set_list(&ob, OFF_APV_STAKE_OUTS, souts.0, souts.1);
                b.set_list(&ob, OFF_APV_STAKE_ADDRS, saddrs.0, saddrs.1);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_APV_VAL_REWARDS_THRESHOLD,
                    OFF_APV_VAL_REWARDS_LOCKTIME,
                    OFF_APV_VAL_REWARDS_ADDRS,
                    validator_rewards_owner,
                    val_addrs,
                );
                set_owner(
                    &mut b,
                    &ob,
                    OFF_APV_DEL_REWARDS_THRESHOLD,
                    OFF_APV_DEL_REWARDS_LOCKTIME,
                    OFF_APV_DEL_REWARDS_ADDRS,
                    delegator_rewards_owner,
                    del_addrs,
                );
                b.set_u32(&ob, OFF_APV_DELEGATION_SHARES, *delegation_shares);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::AddPermissionlessDelegator {
                base,
                validator,
                chain,
                stake,
                rewards_owner,
            } => {
                let mut b =
                    zap::Builder::new(zap::HEADER_SIZE + 1024 + SIZE_ADD_PERMISSIONLESS_DELEGATOR);
                let p = write_spending(&mut b, base);
                let (souts, saddrs) = write_outputs(&mut b, stake);
                let owner_addrs = write_addrs(&mut b, &rewards_owner.addrs);
                let ob = b.start_object(SIZE_ADD_PERMISSIONLESS_DELEGATOR);
                set_envelope(&mut b, &ob, Kind::AddPermissionlessDelegator, base, &p);
                set_validator(&mut b, &ob, OFF_APD_VALIDATOR, validator);
                b.set_bytes_fixed(&ob, OFF_APD_CHAIN, chain);
                b.set_list(&ob, OFF_APD_STAKE_OUTS, souts.0, souts.1);
                b.set_list(&ob, OFF_APD_STAKE_ADDRS, saddrs.0, saddrs.1);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_APD_REWARDS_THRESHOLD,
                    OFF_APD_REWARDS_LOCKTIME,
                    OFF_APD_REWARDS_ADDRS,
                    rewards_owner,
                    owner_addrs,
                );
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::RemoveChainValidator {
                base,
                node_id,
                chain,
                chain_auth,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_REMOVE);
                let p = write_spending(&mut b, base);
                let auth = write_u32_list(&mut b, chain_auth);
                let ob = b.start_object(SIZE_REMOVE);
                set_envelope(&mut b, &ob, Kind::RemoveChainValidator, base, &p);
                b.set_bytes_fixed(&ob, OFF_REMOVE_NODE_ID, &node_id.0);
                b.set_bytes_fixed(&ob, OFF_REMOVE_CHAIN, chain);
                b.set_list(&ob, OFF_REMOVE_CHAIN_AUTH, auth.0, auth.1);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::TransferChainOwnership {
                base,
                chain,
                chain_auth,
                owner,
            } => {
                let mut b =
                    zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_TRANSFER_CHAIN_OWNERSHIP);
                let p = write_spending(&mut b, base);
                let auth = write_u32_list(&mut b, chain_auth);
                let owner_addrs = write_addrs(&mut b, &owner.addrs);
                let ob = b.start_object(SIZE_TRANSFER_CHAIN_OWNERSHIP);
                set_envelope(&mut b, &ob, Kind::TransferChainOwnership, base, &p);
                b.set_bytes_fixed(&ob, OFF_TCO_CHAIN, chain);
                b.set_list(&ob, OFF_TCO_CHAIN_AUTH, auth.0, auth.1);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_TCO_OWNER_THRESHOLD,
                    OFF_TCO_OWNER_LOCKTIME,
                    OFF_TCO_OWNER_ADDRS,
                    owner,
                    owner_addrs,
                );
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::IncreaseL1ValidatorBalance {
                base,
                validation_id,
                balance,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_INCREASE);
                let p = write_spending(&mut b, base);
                let ob = b.start_object(SIZE_INCREASE);
                set_envelope(&mut b, &ob, Kind::IncreaseL1ValidatorBalance, base, &p);
                b.set_bytes_fixed(&ob, OFF_INCREASE_VALIDATION_ID, validation_id);
                b.set_u64(&ob, OFF_INCREASE_BALANCE, *balance);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::DisableL1Validator {
                base,
                validation_id,
                auth,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_DISABLE);
                let p = write_spending(&mut b, base);
                let a = write_u32_list(&mut b, auth);
                let ob = b.start_object(SIZE_DISABLE);
                set_envelope(&mut b, &ob, Kind::DisableL1Validator, base, &p);
                b.set_bytes_fixed(&ob, OFF_DISABLE_VALIDATION_ID, validation_id);
                b.set_list(&ob, OFF_DISABLE_AUTH, a.0, a.1);
                b.finish_as_root(&ob);
                b.finish()
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
                let mut b = zap::Builder::new(
                    zap::HEADER_SIZE + 1024 + SIZE_CREATE_NETWORK + validators.len() * NV_STRIDE,
                );
                let p = write_spending(&mut b, base);
                let owner_addrs = write_addrs(&mut b, &owner.addrs);
                let (vdrs, node_id_pool, addr_pool) = write_network_validators(&mut b, validators);
                let val_addrs = write_addrs(&mut b, &addr_pool);
                let ob = b.start_object(SIZE_CREATE_NETWORK);
                set_envelope(&mut b, &ob, Kind::CreateNetwork, base, &p);
                b.set_bytes_fixed(&ob, OFF_CN_PARENT, parent);
                set_owner(
                    &mut b,
                    &ob,
                    OFF_CN_OWNER_THRESHOLD,
                    OFF_CN_OWNER_LOCKTIME,
                    OFF_CN_OWNER_ADDRS,
                    owner,
                    owner_addrs,
                );
                set_security(
                    &mut b,
                    &ob,
                    OFF_CN_RESTAKE_PARENT,
                    OFF_CN_ADMISSION,
                    OFF_CN_MANAGER,
                    OFF_CN_THRESHOLD,
                    security,
                );
                b.set_list(&ob, OFF_CN_VALIDATORS, vdrs.0, vdrs.1);
                b.set_bytes(&ob, OFF_CN_NODE_ID_POOL, &node_id_pool);
                b.set_list(&ob, OFF_CN_ADDR_POOL, val_addrs.0, val_addrs.1);
                b.set_bytes_fixed(&ob, OFF_CN_MANAGER_CHAIN_ID, manager_chain_id);
                b.set_bytes(&ob, OFF_CN_MANAGER_ADDRESS, manager_address);
                b.finish_as_root(&ob);
                b.finish()
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
                let mut b = zap::Builder::new(
                    zap::HEADER_SIZE + 512 + SIZE_CONVERT_NETWORK + validators.len() * NV_STRIDE,
                );
                let p = write_spending(&mut b, base);
                let (vdrs, node_id_pool, addr_pool) = write_network_validators(&mut b, validators);
                let val_addrs = write_addrs(&mut b, &addr_pool);
                let a = write_u32_list(&mut b, auth);
                let ob = b.start_object(SIZE_CONVERT_NETWORK);
                set_envelope(&mut b, &ob, Kind::ConvertNetwork, base, &p);
                b.set_bytes_fixed(&ob, OFF_CV_NETWORK, network);
                b.set_bytes_fixed(&ob, OFF_CV_PARENT, parent);
                b.set_bytes_fixed(&ob, OFF_CV_MANAGER_CHAIN_ID, manager_chain_id);
                b.set_bytes(&ob, OFF_CV_MANAGER_ADDRESS, manager_address);
                b.set_list(&ob, OFF_CV_VALIDATORS, vdrs.0, vdrs.1);
                b.set_bytes(&ob, OFF_CV_NODE_ID_POOL, &node_id_pool);
                b.set_list(&ob, OFF_CV_ADDR_POOL, val_addrs.0, val_addrs.1);
                b.set_list(&ob, OFF_CV_AUTH, a.0, a.1);
                set_security(
                    &mut b,
                    &ob,
                    OFF_CV_RESTAKE_PARENT,
                    OFF_CV_ADMISSION,
                    OFF_CV_MANAGER,
                    OFF_CV_THRESHOLD,
                    security,
                );
                b.finish_as_root(&ob);
                b.finish()
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
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_TRANSFORM_CHAIN);
                let p = write_spending(&mut b, base);
                let a = write_u32_list(&mut b, chain_auth);
                let ob = b.start_object(SIZE_TRANSFORM_CHAIN);
                set_envelope(&mut b, &ob, Kind::TransformChain, base, &p);
                b.set_bytes_fixed(&ob, OFF_TC_CHAIN, chain);
                b.set_bytes_fixed(&ob, OFF_TC_ASSET_ID, asset_id);
                b.set_u64(&ob, OFF_TC_INITIAL_SUPPLY, *initial_supply);
                b.set_u64(&ob, OFF_TC_MAXIMUM_SUPPLY, *maximum_supply);
                b.set_u64(&ob, OFF_TC_MIN_CONSUMPTION_RATE, *min_consumption_rate);
                b.set_u64(&ob, OFF_TC_MAX_CONSUMPTION_RATE, *max_consumption_rate);
                b.set_u64(&ob, OFF_TC_MIN_VALIDATOR_STAKE, *min_validator_stake);
                b.set_u64(&ob, OFF_TC_MAX_VALIDATOR_STAKE, *max_validator_stake);
                b.set_u32(&ob, OFF_TC_MIN_STAKE_DURATION, *min_stake_duration);
                b.set_u32(&ob, OFF_TC_MAX_STAKE_DURATION, *max_stake_duration);
                b.set_u32(&ob, OFF_TC_MIN_DELEGATION_FEE, *min_delegation_fee);
                b.set_u64(&ob, OFF_TC_MIN_DELEGATOR_STAKE, *min_delegator_stake);
                b.set_u8(
                    &ob,
                    OFF_TC_MAX_VALIDATOR_WEIGHT_FACTOR,
                    *max_validator_weight_factor,
                );
                b.set_u32(&ob, OFF_TC_UPTIME_REQUIREMENT, *uptime_requirement);
                b.set_list(&ob, OFF_TC_CHAIN_AUTH, a.0, a.1);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::RegisterL1Validator {
                base,
                balance,
                proof_of_possession,
                message,
            } => {
                let mut b = zap::Builder::new(zap::HEADER_SIZE + 512 + SIZE_REGISTER_L1_VALIDATOR);
                let p = write_spending(&mut b, base);
                let ob = b.start_object(SIZE_REGISTER_L1_VALIDATOR);
                set_envelope(&mut b, &ob, Kind::RegisterL1Validator, base, &p);
                b.set_u64(&ob, OFF_RL_BALANCE, *balance);
                b.set_bytes_fixed(&ob, OFF_RL_POP, proof_of_possession);
                b.set_bytes(&ob, OFF_RL_MESSAGE, message);
                b.finish_as_root(&ob);
                b.finish()
            }
            Unsigned::SetL1ValidatorWeight { base, message } => {
                let mut b =
                    zap::Builder::new(zap::HEADER_SIZE + 256 + SIZE_SET_L1_VALIDATOR_WEIGHT);
                let p = write_spending(&mut b, base);
                let ob = b.start_object(SIZE_SET_L1_VALIDATOR_WEIGHT);
                set_envelope(&mut b, &ob, Kind::SetL1ValidatorWeight, base, &p);
                b.set_bytes(&ob, OFF_SW_MESSAGE, message);
                b.finish_as_root(&ob);
                b.finish()
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
        let raw = o.u8(OFF_KIND);
        let kind = Kind::from_u8(raw).ok_or(Error::UnknownKind(raw))?;
        Ok(match kind {
            Kind::RewardValidator => Unsigned::RewardValidator {
                staker_tx_id: o.id(OFF_REWARD_TX_ID),
            },
            Kind::Base => Unsigned::Base(read_envelope(o)),
            Kind::Import => Unsigned::Import {
                base: read_envelope(o),
                source_chain: o.id(OFF_IMPORT_SOURCE_CHAIN),
                imported: read_inputs(o, OFF_IMPORT_INPUTS, OFF_IMPORT_SIG_INDICES),
            },
            Kind::Export => Unsigned::Export {
                base: read_envelope(o),
                destination_chain: o.id(OFF_EXPORT_DEST_CHAIN),
                exported: read_outputs(o, OFF_EXPORT_OUTPUTS, OFF_EXPORT_ADDRS),
            },
            Kind::CreateChain => Unsigned::CreateChain {
                base: read_envelope(o),
                chain: o.id(OFF_CC_CHAIN),
                vm_id: o.id(OFF_CC_VM_ID),
                name: o.text(OFF_CC_NAME).to_string(),
                fx_ids: read_id_list(o, OFF_CC_FX_IDS),
                genesis: o.bytes(OFF_CC_GENESIS).to_vec(),
                chain_auth: read_u32_list(o, OFF_CC_AUTH),
            },
            Kind::AddValidator => Unsigned::AddValidator {
                base: read_envelope(o),
                validator: read_validator(o, OFF_AV_VALIDATOR),
                stake: read_outputs(o, OFF_AV_STAKE_OUTS, OFF_AV_STAKE_ADDRS),
                rewards_owner: read_owner(
                    o,
                    OFF_AV_REWARDS_THRESHOLD,
                    OFF_AV_REWARDS_LOCKTIME,
                    OFF_AV_REWARDS_ADDRS,
                ),
                delegation_shares: o.u32(OFF_AV_DELEGATION_SHARES),
            },
            Kind::AddDelegator => Unsigned::AddDelegator {
                base: read_envelope(o),
                validator: read_validator(o, OFF_AD_VALIDATOR),
                stake: read_outputs(o, OFF_AD_STAKE_OUTS, OFF_AD_STAKE_ADDRS),
                rewards_owner: read_owner(
                    o,
                    OFF_AD_REWARDS_THRESHOLD,
                    OFF_AD_REWARDS_LOCKTIME,
                    OFF_AD_REWARDS_ADDRS,
                ),
            },
            Kind::AddChainValidator => Unsigned::AddChainValidator {
                base: read_envelope(o),
                validator: read_validator(o, OFF_ACV_VALIDATOR),
                chain: o.id(OFF_ACV_CHAIN),
                chain_auth: read_u32_list(o, OFF_ACV_CHAIN_AUTH),
            },
            Kind::AddPermissionlessValidator => Unsigned::AddPermissionlessValidator {
                base: read_envelope(o),
                validator: read_validator(o, OFF_APV_VALIDATOR),
                chain: o.id(OFF_APV_CHAIN),
                signer: Signer::read(o, OFF_APV_SIGNER),
                stake: read_outputs(o, OFF_APV_STAKE_OUTS, OFF_APV_STAKE_ADDRS),
                validator_rewards_owner: read_owner(
                    o,
                    OFF_APV_VAL_REWARDS_THRESHOLD,
                    OFF_APV_VAL_REWARDS_LOCKTIME,
                    OFF_APV_VAL_REWARDS_ADDRS,
                ),
                delegator_rewards_owner: read_owner(
                    o,
                    OFF_APV_DEL_REWARDS_THRESHOLD,
                    OFF_APV_DEL_REWARDS_LOCKTIME,
                    OFF_APV_DEL_REWARDS_ADDRS,
                ),
                delegation_shares: o.u32(OFF_APV_DELEGATION_SHARES),
            },
            Kind::AddPermissionlessDelegator => Unsigned::AddPermissionlessDelegator {
                base: read_envelope(o),
                validator: read_validator(o, OFF_APD_VALIDATOR),
                chain: o.id(OFF_APD_CHAIN),
                stake: read_outputs(o, OFF_APD_STAKE_OUTS, OFF_APD_STAKE_ADDRS),
                rewards_owner: read_owner(
                    o,
                    OFF_APD_REWARDS_THRESHOLD,
                    OFF_APD_REWARDS_LOCKTIME,
                    OFF_APD_REWARDS_ADDRS,
                ),
            },
            Kind::RemoveChainValidator => Unsigned::RemoveChainValidator {
                base: read_envelope(o),
                node_id: NodeId(o.short_id(OFF_REMOVE_NODE_ID)),
                chain: o.id(OFF_REMOVE_CHAIN),
                chain_auth: read_u32_list(o, OFF_REMOVE_CHAIN_AUTH),
            },
            Kind::TransferChainOwnership => Unsigned::TransferChainOwnership {
                base: read_envelope(o),
                chain: o.id(OFF_TCO_CHAIN),
                chain_auth: read_u32_list(o, OFF_TCO_CHAIN_AUTH),
                owner: read_owner(
                    o,
                    OFF_TCO_OWNER_THRESHOLD,
                    OFF_TCO_OWNER_LOCKTIME,
                    OFF_TCO_OWNER_ADDRS,
                ),
            },
            Kind::IncreaseL1ValidatorBalance => Unsigned::IncreaseL1ValidatorBalance {
                base: read_envelope(o),
                validation_id: o.id(OFF_INCREASE_VALIDATION_ID),
                balance: o.u64(OFF_INCREASE_BALANCE),
            },
            Kind::DisableL1Validator => Unsigned::DisableL1Validator {
                base: read_envelope(o),
                validation_id: o.id(OFF_DISABLE_VALIDATION_ID),
                auth: read_u32_list(o, OFF_DISABLE_AUTH),
            },
            Kind::CreateNetwork => Unsigned::CreateNetwork {
                base: read_envelope(o),
                parent: o.id(OFF_CN_PARENT),
                owner: read_owner(
                    o,
                    OFF_CN_OWNER_THRESHOLD,
                    OFF_CN_OWNER_LOCKTIME,
                    OFF_CN_OWNER_ADDRS,
                ),
                security: read_security(
                    o,
                    OFF_CN_RESTAKE_PARENT,
                    OFF_CN_ADMISSION,
                    OFF_CN_MANAGER,
                    OFF_CN_THRESHOLD,
                )?,
                validators: read_network_validators(
                    o,
                    OFF_CN_VALIDATORS,
                    OFF_CN_NODE_ID_POOL,
                    OFF_CN_ADDR_POOL,
                ),
                manager_chain_id: o.id(OFF_CN_MANAGER_CHAIN_ID),
                manager_address: o.bytes(OFF_CN_MANAGER_ADDRESS).to_vec(),
            },
            Kind::ConvertNetwork => Unsigned::ConvertNetwork {
                base: read_envelope(o),
                network: o.id(OFF_CV_NETWORK),
                parent: o.id(OFF_CV_PARENT),
                manager_chain_id: o.id(OFF_CV_MANAGER_CHAIN_ID),
                manager_address: o.bytes(OFF_CV_MANAGER_ADDRESS).to_vec(),
                validators: read_network_validators(
                    o,
                    OFF_CV_VALIDATORS,
                    OFF_CV_NODE_ID_POOL,
                    OFF_CV_ADDR_POOL,
                ),
                auth: read_u32_list(o, OFF_CV_AUTH),
                security: read_security(
                    o,
                    OFF_CV_RESTAKE_PARENT,
                    OFF_CV_ADMISSION,
                    OFF_CV_MANAGER,
                    OFF_CV_THRESHOLD,
                )?,
            },
            Kind::TransformChain => Unsigned::TransformChain {
                base: read_envelope(o),
                chain: o.id(OFF_TC_CHAIN),
                asset_id: o.id(OFF_TC_ASSET_ID),
                initial_supply: o.u64(OFF_TC_INITIAL_SUPPLY),
                maximum_supply: o.u64(OFF_TC_MAXIMUM_SUPPLY),
                min_consumption_rate: o.u64(OFF_TC_MIN_CONSUMPTION_RATE),
                max_consumption_rate: o.u64(OFF_TC_MAX_CONSUMPTION_RATE),
                min_validator_stake: o.u64(OFF_TC_MIN_VALIDATOR_STAKE),
                max_validator_stake: o.u64(OFF_TC_MAX_VALIDATOR_STAKE),
                min_stake_duration: o.u32(OFF_TC_MIN_STAKE_DURATION),
                max_stake_duration: o.u32(OFF_TC_MAX_STAKE_DURATION),
                min_delegation_fee: o.u32(OFF_TC_MIN_DELEGATION_FEE),
                min_delegator_stake: o.u64(OFF_TC_MIN_DELEGATOR_STAKE),
                max_validator_weight_factor: o.u8(OFF_TC_MAX_VALIDATOR_WEIGHT_FACTOR),
                uptime_requirement: o.u32(OFF_TC_UPTIME_REQUIREMENT),
                chain_auth: read_u32_list(o, OFF_TC_CHAIN_AUTH),
            },
            Kind::RegisterL1Validator => Unsigned::RegisterL1Validator {
                base: read_envelope(o),
                balance: o.u64(OFF_RL_BALANCE),
                proof_of_possession: read_proof(o, OFF_RL_POP),
                message: o.bytes(OFF_RL_MESSAGE).to_vec(),
            },
            Kind::SetL1ValidatorWeight => Unsigned::SetL1ValidatorWeight {
                base: read_envelope(o),
                message: o.bytes(OFF_SW_MESSAGE).to_vec(),
            },
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

const OFF_CREDS_LIST: usize = 0;
const OFF_SIG_ARRAY: usize = 8;
const CREDS_OBJ_SIZE: usize = 16;
const CRED_ENTRY: usize = 8;

/// Encode a transaction's credentials as their own message.
pub fn write_credentials(creds: &[Credential]) -> Vec<u8> {
    let mut b = zap::Builder::new(zap::HEADER_SIZE + 128 + creds.len() * CRED_ENTRY);
    let mut blobs: Vec<[u8; SIG_LEN]> = Vec::new();
    let mut clb = b.start_list();
    let mut cursor: u32 = 0;
    for c in creds {
        let mut e = [0u8; CRED_ENTRY];
        e[0..4].copy_from_slice(&cursor.to_le_bytes());
        e[4..8].copy_from_slice(&(c.sigs.len() as u32).to_le_bytes());
        b.list_bytes(&mut clb, &e);
        blobs.extend_from_slice(&c.sigs);
        cursor += c.sigs.len() as u32;
    }
    let creds_off = clb.offset();

    let (sig_off, sig_count) = if blobs.is_empty() {
        (0, 0)
    } else {
        let mut slb = b.start_list();
        for s in &blobs {
            b.list_bytes(&mut slb, s);
        }
        (slb.offset(), blobs.len())
    };

    let ob = b.start_object(CREDS_OBJ_SIZE);
    b.set_list(&ob, OFF_CREDS_LIST, creds_off, creds.len());
    b.set_list(&ob, OFF_SIG_ARRAY, sig_off, sig_count);
    b.finish_as_root(&ob);
    b.finish()
}

/// Read credentials back.
///
/// A credential naming signatures outside the shared array is refused rather
/// than clamped: a claimed range that is not there is a transaction asserting
/// authority it did not bring.
pub fn parse_credentials(bytes: &[u8]) -> Result<Vec<Credential>, Error> {
    let msg = zap::Message::parse(bytes)?;
    let o = msg.root();
    let creds = o.list(OFF_CREDS_LIST, CRED_ENTRY);
    let sigs = o.list(OFF_SIG_ARRAY, SIG_LEN);
    let total = sigs.len() as u32;
    let mut out = Vec::with_capacity(creds.len());
    for i in 0..creds.len() {
        let e = creds.object(i, CRED_ENTRY);
        let (start, count) = (e.u32(0), e.u32(4));
        if start > total || count > total - start {
            return Err(Error::CredentialRangeOutOfBounds);
        }
        let mut c = Credential {
            sigs: Vec::with_capacity(count as usize),
        };
        for j in 0..count {
            let blob = sigs.object((start + j) as usize, SIG_LEN);
            let mut s = [0u8; SIG_LEN];
            s.copy_from_slice(blob.bytes_fixed(0, SIG_LEN));
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
        let n = zap::message_len(signed)?;
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
        let n = zap::message_len(&self.bytes).unwrap_or(self.bytes.len());
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
            assert_eq!(msg.root().u8(OFF_KIND), tx.kind() as u8, "kind byte");
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
        let crafted = |declared: u32, actual: usize| -> Vec<u8> {
            let mut b = zap::Builder::new(zap::HEADER_SIZE + 128 + CRED_ENTRY + actual * 65);
            let mut lb = b.start_list();
            let mut e = [0u8; CRED_ENTRY];
            e[0..4].copy_from_slice(&0u32.to_le_bytes());
            e[4..8].copy_from_slice(&declared.to_le_bytes());
            b.list_bytes(&mut lb, &e);
            let creds_off = lb.offset();

            let (sig_off, sig_count) = if actual > 0 {
                let mut slb = b.start_list();
                for i in 0..actual {
                    let mut sig = [0u8; 65];
                    sig[0] = (i + 1) as u8;
                    b.list_bytes(&mut slb, &sig);
                }
                (slb.offset(), actual)
            } else {
                (0, 0)
            };

            let ob = b.start_object(CREDS_OBJ_SIZE);
            b.set_list(&ob, OFF_CREDS_LIST, creds_off, 1);
            b.set_list(&ob, OFF_SIG_ARRAY, sig_off, sig_count);
            b.finish_as_root(&ob);
            b.finish()
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
