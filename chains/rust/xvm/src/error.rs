// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Every reason this chain refuses something.
//!
//! One enum, because a refusal is a value: a test names the exact reason it
//! expects, and a caller matches on it instead of reading a string. The
//! variants are the Go sentinel set, one for one — same names, same meanings,
//! so a behaviour that is checked in one language is the same behaviour checked
//! in the other.

use crate::wire;

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    // ---- wire / decode ----
    Wire(wire::Error),
    /// A tx kind byte that names no transaction this chain has.
    UnknownTxKind(u8),
    /// An envelope whose (family, shape) pair names no primitive.
    UnknownFxPrimitive(u8, u8),
    /// A decoded output that is not something a transferable output may hold.
    NotATransferableOut,
    /// A decoded input that is not something a transferable input may hold.
    NotATransferableIn,

    // ---- utxo components ----
    NilAssetId,
    EmptyAssetId,
    NilTransferableOutput,
    NilTransferableFxOutput,
    OutputsNotSorted,
    NilTransferableInput,
    NilTransferableFxInput,
    InputsNotSortedUnique,
    InsufficientFunds,
    /// A sum of amounts that does not fit in 64 bits.
    Overflow,

    // ---- base tx metadata ----
    NilTx,
    WrongNetworkId,
    WrongChainId,
    MemoTooLarge(usize, usize),

    // ---- secp256k1 fx ----
    NilOutput,
    OutputUnspendable,
    OutputUnoptimized,
    AddrsNotSortedUnique,
    NoValueOutput,
    NilInput,
    InputIndicesNotSortedUnique,
    NoValueInput,
    NilCredential,
    NilMintOperation,
    WrongTxType,
    WrongOpType,
    WrongUtxoType,
    WrongInputType,
    WrongCredentialType,
    WrongOwnerType,
    MismatchedAmounts(u64, u64),
    WrongNumberOfUtxos,
    WrongMintCreated,
    Timelocked,
    TooManySigners,
    TooFewSigners,
    InputOutputIndexOutOfBounds,
    InputCredentialSignersMismatch,
    WrongSig,
    /// A signature that no public key can be recovered from.
    UnrecoverableSignature,

    // ---- nft / property fx ----
    NilTransferOutput,
    PayloadTooLarge,
    NilTransferOperation,
    WrongUniqueId,
    WrongBytes,
    CantTransfer,
    WrongMintOutput,

    // ---- initial state / operation ----
    NilInitialState,
    NilFxOutput,
    UnknownFx,
    NilOperation,
    NilFxOperation,
    NotSortedAndUniqueUtxoIds,

    // ---- syntactic verification ----
    WrongNumberOfCredentials(usize, usize),
    InitialStatesNotSortedUnique,
    NameTooShort,
    NameTooLong,
    SymbolTooShort,
    SymbolTooLong,
    NoFxs,
    IllegalNameCharacter,
    IllegalSymbolCharacter,
    UnexpectedWhitespace,
    DenominationTooLarge,
    OperationsNotSortedUnique,
    NoOperations,
    DoubleSpend,
    NoImportInputs,
    NoExportOutputs,

    // ---- semantic verification ----
    AssetIdMismatch,
    NotAnAsset,
    IncompatibleFx,
    /// A cross-chain transaction that names this very chain.
    SameChainId,
    /// A cross-chain transaction naming a chain on another network.
    MismatchedNetIds,
    /// A chain nothing on this network knows about.
    UnknownChain,

    // ---- state ----
    NotFound,
    MissingParentState,

    // ---- block ----
    UnexpectedMerkleRoot,
    TimestampBeyondSyncBound,
    EmptyBlock,
    ChildBlockEarlierThanParent,
    ConflictingBlockTxs,
    IncorrectHeight(u64, u64),
    BlockNotFound,
    /// A block that moves value across a chain boundary, on a chain that was
    /// never given the shared area to move it through. The block's own state
    /// and that movement are one write, so with nowhere to make the movement
    /// there is no half of it that may be written on its own.
    NoSharedMemory,
    ConflictingParentTxs,
    ChainNotSynced,
    NoTransactions,
    /// A transaction reached block construction without its wire bytes.
    UninitializedTx(usize),
    /// An output whose owner model the state root has no canonical commitment
    /// for. Better a refusal than a root computed under an invented rule.
    UnsupportedOwnerModel,

    // ---- admission ----
    /// Offered twice.
    DuplicateTx,
    /// Bigger than a transaction may be: what it is, and the bar.
    TxTooLarge(usize, usize),
    /// No room: what it needs, and what is free.
    MempoolFull(usize, usize),
    /// It spends an output another waiting transaction already spends.
    ConflictsWithOtherTx,
    /// A classical signature on a chain whose terms require a post-quantum one.
    ClassicalCredentialRefused,
    /// A mempool that was never told its chain's terms. A wiring mistake, not
    /// a runtime condition, so it fails loud rather than admitting.
    NoSecurityProfile,
    /// A byte that names no security profile.
    UnknownSecurityProfile(u8),

    // ---- storage ----
    /// The chain's own record would not read or would not write. The string is
    /// what the operating system said, kept as text so a refusal stays a value
    /// that can be compared and carried across a thread.
    Storage(String),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        use Error::*;
        match self {
            Wire(e) => write!(f, "{e}"),
            UnknownTxKind(k) => write!(f, "xvm txs: unknown tx kind {k}"),
            UnknownFxPrimitive(tk, sk) => write!(
                f,
                "xvm txs: unknown fx primitive (TypeKind=0x{tk:02x}, ShapeKind=0x{sk:02x})"
            ),
            NotATransferableOut => {
                write!(f, "xvm txs: decoded output is not a transferable output")
            }
            NotATransferableIn => write!(f, "xvm txs: decoded input is not a transferable input"),

            NilAssetId => write!(f, "nil asset ID is not valid"),
            EmptyAssetId => write!(f, "empty asset ID is not valid"),
            NilTransferableOutput => write!(f, "nil transferable output is not valid"),
            NilTransferableFxOutput => {
                write!(f, "nil transferable feature extension output is not valid")
            }
            OutputsNotSorted => write!(f, "outputs not sorted"),
            NilTransferableInput => write!(f, "nil transferable input is not valid"),
            NilTransferableFxInput => {
                write!(f, "nil transferable feature extension input is not valid")
            }
            InputsNotSortedUnique => write!(f, "inputs not sorted and unique"),
            InsufficientFunds => write!(f, "insufficient funds"),
            Overflow => write!(f, "overflow"),

            NilTx => write!(f, "nil tx is not valid"),
            WrongNetworkId => write!(f, "tx has wrong network ID"),
            WrongChainId => write!(f, "tx has wrong chain ID"),
            MemoTooLarge(got, max) => write!(f, "memo exceeds maximum length: {got} > {max}"),

            NilOutput => write!(f, "nil output"),
            OutputUnspendable => write!(f, "output is unspendable"),
            OutputUnoptimized => write!(f, "output representation should be optimized"),
            AddrsNotSortedUnique => write!(f, "addresses not sorted and unique"),
            NoValueOutput => write!(f, "output has no value"),
            NilInput => write!(f, "nil input"),
            InputIndicesNotSortedUnique => write!(f, "address indices not sorted and unique"),
            NoValueInput => write!(f, "input has no value"),
            NilCredential => write!(f, "nil credential"),
            NilMintOperation => write!(f, "nil mint operation"),
            WrongTxType => write!(f, "wrong tx type"),
            WrongOpType => write!(f, "wrong operation type"),
            WrongUtxoType => write!(f, "wrong utxo type"),
            WrongInputType => write!(f, "wrong input type"),
            WrongCredentialType => write!(f, "wrong credential type"),
            WrongOwnerType => write!(f, "wrong owner type"),
            MismatchedAmounts(a, b) => {
                write!(f, "utxo amount and input amount are not equal: {a} != {b}")
            }
            WrongNumberOfUtxos => write!(f, "wrong number of utxos for the operation"),
            WrongMintCreated => write!(f, "wrong mint output created from the operation"),
            Timelocked => write!(f, "output is time locked"),
            TooManySigners => write!(f, "input has more signers than expected"),
            TooFewSigners => write!(f, "input has less signers than expected"),
            InputOutputIndexOutOfBounds => {
                write!(f, "input referenced a nonexistent address in the output")
            }
            InputCredentialSignersMismatch => write!(
                f,
                "input expected a different number of signers than provided in the credential"
            ),
            WrongSig => write!(f, "wrong signature"),
            UnrecoverableSignature => {
                write!(f, "could not recover a public key from the signature")
            }

            NilTransferOutput => write!(f, "nil transfer output"),
            PayloadTooLarge => write!(f, "payload too large"),
            NilTransferOperation => write!(f, "nil transfer operation"),
            WrongUniqueId => write!(f, "wrong unique ID provided"),
            WrongBytes => write!(f, "wrong bytes provided"),
            CantTransfer => write!(f, "cant transfer with this fx"),
            WrongMintOutput => write!(f, "wrong mint output provided"),

            NilInitialState => write!(f, "nil initial state is not valid"),
            NilFxOutput => write!(f, "nil feature extension output is not valid"),
            UnknownFx => write!(f, "unknown feature extension"),
            NilOperation => write!(f, "nil operation is not valid"),
            NilFxOperation => write!(f, "nil fx operation is not valid"),
            NotSortedAndUniqueUtxoIds => write!(f, "utxo IDs not sorted and unique"),

            WrongNumberOfCredentials(a, b) => {
                write!(f, "wrong number of credentials: {a} != {b}")
            }
            InitialStatesNotSortedUnique => write!(f, "initial states not sorted and unique"),
            NameTooShort => write!(f, "name is too short, minimum size is 1"),
            NameTooLong => write!(f, "name is too long, maximum size is 128"),
            SymbolTooShort => write!(f, "symbol is too short, minimum size is 1"),
            SymbolTooLong => write!(f, "symbol is too long, maximum size is 4"),
            NoFxs => write!(f, "assets must support at least one Fx"),
            IllegalNameCharacter => {
                write!(
                    f,
                    "asset's name must be made up of only letters and numbers"
                )
            }
            IllegalSymbolCharacter => write!(f, "asset's symbol must be all upper case letters"),
            UnexpectedWhitespace => write!(f, "unexpected whitespace provided"),
            DenominationTooLarge => write!(f, "denomination is too large"),
            OperationsNotSortedUnique => write!(f, "operations not sorted and unique"),
            NoOperations => write!(f, "an operationTx must have at least one operation"),
            DoubleSpend => write!(f, "inputs attempt to double spend an input"),
            NoImportInputs => write!(f, "no import inputs"),
            NoExportOutputs => write!(f, "no export outputs"),

            AssetIdMismatch => write!(f, "asset IDs in the input don't match the utxo"),
            NotAnAsset => write!(f, "not an asset"),
            IncompatibleFx => write!(f, "incompatible feature extension"),
            SameChainId => write!(f, "same chainID"),
            MismatchedNetIds => write!(f, "mismatched netIDs"),
            UnknownChain => write!(f, "failed to get net of chain"),

            NotFound => write!(f, "not found"),
            MissingParentState => write!(f, "missing parent state"),

            UnexpectedMerkleRoot => write!(f, "unexpected merkle root"),
            TimestampBeyondSyncBound => write!(
                f,
                "proposed timestamp is too far in the future relative to local time"
            ),
            EmptyBlock => write!(f, "block contains no transactions"),
            ChildBlockEarlierThanParent => {
                write!(f, "proposed timestamp before current chain time")
            }
            ConflictingBlockTxs => write!(f, "block contains conflicting transactions"),
            IncorrectHeight(want, got) => {
                write!(f, "block has incorrect height: expected {want}, got {got}")
            }
            BlockNotFound => write!(f, "block not found"),
            NoSharedMemory => write!(
                f,
                "xvm block: this block moves value across a chain boundary and this chain has no shared memory to move it through"
            ),
            ConflictingParentTxs => write!(
                f,
                "block contains a transaction that conflicts with a transaction in a parent block"
            ),
            ChainNotSynced => write!(f, "chain not synced"),
            NoTransactions => write!(f, "no transactions"),
            UninitializedTx(i) => write!(
                f,
                "tx {i} has no wire bytes (not initialized before block build)"
            ),
            UnsupportedOwnerModel => write!(
                f,
                "xvm execution_root: output owner model has no canonical owner_root"
            ),

            DuplicateTx => write!(f, "duplicate tx"),
            TxTooLarge(got, max) => write!(f, "tx too large: size ({got}) > max size ({max})"),
            MempoolFull(got, free) => {
                write!(
                    f,
                    "mempool is full: size ({got}) > available space ({free})"
                )
            }
            ConflictsWithOtherTx => write!(f, "tx conflicts with other tx"),
            ClassicalCredentialRefused => write!(
                f,
                "auth: classical secp256k1 credential refused under strict-PQ profile"
            ),
            NoSecurityProfile => write!(f, "auth: nil ChainSecurityProfile"),
            UnknownSecurityProfile(v) => write!(f, "security: unknown profile 0x{v:02x}"),

            Storage(why) => write!(f, "storage: {why}"),
        }
    }
}

impl std::error::Error for Error {}

impl From<wire::Error> for Error {
    fn from(e: wire::Error) -> Self {
        Error::Wire(e)
    }
}

impl From<crate::zap::Error> for Error {
    fn from(e: crate::zap::Error) -> Self {
        Error::Wire(wire::Error::Zap(e))
    }
}

/// The result every fallible thing in this crate answers with.
pub type Result<T> = std::result::Result<T, Error>;
