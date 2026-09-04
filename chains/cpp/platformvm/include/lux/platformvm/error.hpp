// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// error.hpp — every reason this chain refuses something, named once.
//
// A refusal is a VALUE here, not a string and not a bool. That is what lets a
// test say "this input is rejected for THIS reason" the way the Go tests do
// (`require.ErrorIs(err, ErrOverDelegated)`) rather than only "something went
// wrong" — a validation rule that rejects for an unintended reason is a bug the
// weaker assertion cannot see.
//
// One enumerator per sentinel error in the Go reference, carrying the Go name it
// renders so the two cannot drift apart quietly. Detail text is for humans and
// is never compared.

#pragma once

#include <expected>
#include <string>
#include <string_view>
#include <utility>

namespace lux::platformvm {

enum class Err {
    None = 0,

    // ── wire (github.com/luxfi/zap)
    InvalidMagic,       // zap.ErrInvalidMagic
    InvalidVersion,     // zap.ErrInvalidVersion
    BufferTooSmall,     // zap.ErrBufferTooSmall
    UnknownTxKind,      // txs.parseUnsigned "unknown tx kind"
    CredSigsOutOfRange, // txs.errCredSigsOutOfRange
    UnsupportedAuth,    // txs.writeAuth "unsupported auth"
    UnsupportedOwner,   // txs.writeOwner "unsupported owner"
    UnsupportedSigner,  // txs.setSigner "unsupported signer"
    UnsupportedFxOutput,// txs.explodeOutput "unsupported FxOutput"
    UnsupportedFxInput, // txs.explodeInput "unsupported FxInput"
    UnsupportedCred,    // txs.writeCredsBuf "unsupported credential type"

    // ── arithmetic (github.com/luxfi/math)
    Overflow,  // math.ErrOverflow
    Underflow, // math.ErrUnderflow

    // ── shared spending envelope (github.com/luxfi/utxo)
    NilTx,                     // utxo.ErrNilTx / txs.ErrNilTx
    WrongNetworkID,            // utxo.ErrWrongNetworkID
    WrongChainID,              // utxo.ErrWrongChainID
    MemoTooLarge,              // utxo.ErrMemoTooLarge
    NilTransferableOutput,     // utxo.ErrNilTransferableOutput
    NilTransferableFxOutput,   // utxo.ErrNilTransferableFxOutput
    NilTransferableInput,      // utxo.ErrNilTransferableInput
    NilTransferableFxInput,    // utxo.ErrNilTransferableFxInput
    OutputsNotSorted,          // utxo.ErrOutputsNotSorted / txs.errOutputsNotSorted
    InputsNotSortedUnique,     // utxo.ErrInputsNotSortedUnique / txs.errInputsNotSortedUnique
    NilAssetID,                // utxo.errNilAssetID
    EmptyAssetID,              // utxo.errEmptyAssetID
    NilUTXOID,                 // utxo.errNilUTXOID

    // ── secp256k1fx
    NilOutput,                     // secp256k1fx.ErrNilOutput
    OutputUnspendable,             // secp256k1fx.ErrOutputUnspendable
    OutputUnoptimized,             // secp256k1fx.ErrOutputUnoptimized
    AddrsNotSortedUnique,          // secp256k1fx.ErrAddrsNotSortedUnique
    NoValueOutput,                 // secp256k1fx.ErrNoValueOutput
    NilInput,                      // secp256k1fx.ErrNilInput
    InputIndicesNotSortedUnique,   // secp256k1fx.ErrInputIndicesNotSortedUnique
    NoValueInput,                  // secp256k1fx.ErrNoValueInput
    NilCredential,                 // secp256k1fx.ErrNilCredential

    // ── stakeable lock
    InvalidLocktime,      // stakeable.errInvalidLocktime
    NestedStakeableLocks, // stakeable.errNestedStakeableLocks

    // ── security.Mode
    UnknownAdmission, // security.ErrUnknownAdmission
    UnknownManager,   // security.ErrUnknownManager
    NoSecurity,       // security.ErrNoSecurity

    // ── signer
    InvalidProofOfPossession, // signer.ErrInvalidProofOfPossession
    InvalidPublicKey,         // bls.PublicKeyFromCompressedBytes failure
    InvalidSignature,         // bls.SignatureFromBytes failure

    // ── txs, per type
    NilSignedTx,             // txs.ErrNilSignedTx
    SignedTxNotInitialized,  // txs.errSignedTxNotInitialized
    WeightTooSmallSyntactic, // txs.ErrWeightTooSmall
    BadChainID,              // txs.errBadChainID
    TooManyShares,           // txs.errTooManyShares
    StakeMustBeLUX,          // txs.errStakeMustBeLUX
    DelegatorWeightMismatch, // txs.errDelegatorWeightMismatch
    ValidatorWeightMismatch, // txs.errValidatorWeightMismatch
    EmptyNodeID,             // txs.errEmptyNodeID
    InvalidNodeIDLength,     // ids.ToNodeID on a slice that is not 20 bytes
    NoStake,                 // txs.errNoStake
    InvalidSigner,           // txs.errInvalidSigner
    MultipleStakedAssets,    // txs.errMultipleStakedAssets
    AddPrimaryNetworkValidator,   // txs.errAddPrimaryNetworkValidator
    NoImportInputs,               // txs.errNoImportInputs
    WrongLocktime,                // txs.ErrWrongLocktime
    NoExportOutputs,              // txs.errNoExportOutputs
    CantValidatePrimaryNetwork,   // txs.ErrCantValidatePrimaryNetwork
    InvalidVMID,                  // txs.errInvalidVMID
    FxIDsNotSortedAndUnique,      // txs.errFxIDsNotSortedAndUnique
    NameTooLong,                  // txs.errNameTooLong
    GenesisTooLong,               // txs.errGenesisTooLong
    IllegalNameCharacter,         // txs.errIllegalNameCharacter
    TransferPermissionlessChain,  // txs.ErrTransferPermissionlessChain
    RemovePrimaryNetworkValidator,// txs.ErrRemovePrimaryNetworkValidator
    ZeroWeight,                   // txs.ErrZeroWeight
    AddressTooLong,               // txs.ErrAddressTooLong
    ValidatorsNotSortedAndUnique, // txs.ErrValidatorsNotSortedAndUnique
    OwnSetMustIncludeValidator,   // txs.ErrOwnSetMustIncludeValidator
    NoOwnSetButHasValidators,     // txs.ErrNoOwnSetButHasValidators
    ContractManagerNeedsAddress,  // txs.ErrContractManagerNeedsAddress
    ConvertPrimaryNetwork,        // txs.ErrConvertPrimaryNetwork
    ConvertMustHaveValidators,    // txs.ErrConvertMustHaveValidators
    ConvertMustEstablishOwnSet,   // txs.ErrConvertMustEstablishOwnSet
    ZeroBalance,                  // txs.ErrZeroBalance
    CantTransformPrimaryNetwork,       // txs.errCantTransformPrimaryNetwork
    TransformEmptyAssetID,             // txs.errEmptyAssetID
    AssetIDCantBeLUX,                  // txs.errAssetIDCantBeLUX
    InitialSupplyZero,                 // txs.errInitialSupplyZero
    InitialSupplyGreaterThanMaxSupply, // txs.errInitialSupplyGreaterThanMaxSupply
    MinConsumptionRateTooLarge,        // txs.errMinConsumptionRateTooLarge
    MaxConsumptionRateTooLarge,        // txs.errMaxConsumptionRateTooLarge
    MinValidatorStakeZero,             // txs.errMinValidatorStakeZero
    MinValidatorStakeAboveSupply,      // txs.errMinValidatorStakeAboveSupply
    MinValidatorStakeAboveMax,         // txs.errMinValidatorStakeAboveMax
    MaxValidatorStakeTooLarge,         // txs.errMaxValidatorStakeTooLarge
    MinStakeDurationZero,              // txs.errMinStakeDurationZero
    MinStakeDurationTooLarge,          // txs.errMinStakeDurationTooLarge
    MinDelegationFeeTooLarge,          // txs.errMinDelegationFeeTooLarge
    MinDelegatorStakeZero,             // txs.errMinDelegatorStakeZero
    MaxValidatorWeightFactorZero,      // txs.errMaxValidatorWeightFactorZero
    UptimeRequirementTooLarge,         // txs.errUptimeRequirementTooLarge

    // ── state
    NotFound,                  // database.ErrNotFound
    AddingStakerAfterDeletion, // state.ErrAddingStakerAfterDeletion

    // ── executor
    WeightTooSmall,                  // executor.ErrWeightTooSmall
    WeightTooLarge,                  // executor.ErrWeightTooLarge
    InsufficientDelegationFee,       // executor.ErrInsufficientDelegationFee
    StakeTooShort,                   // executor.ErrStakeTooShort
    StakeTooLong,                    // executor.ErrStakeTooLong
    FlowCheckFailed,                 // executor.ErrFlowCheckFailed
    NotValidator,                    // executor.ErrNotValidator
    RemovePermissionlessValidator,   // executor.ErrRemovePermissionlessValidator
    PeriodMismatch,                  // executor.ErrPeriodMismatch
    OverDelegated,                   // executor.ErrOverDelegated
    DuplicateValidator,              // executor.ErrDuplicateValidator
    DelegateToPermissionedValidator, // executor.ErrDelegateToPermissionedValidator
    WrongStakedAssetID,              // executor.ErrWrongStakedAssetID
    AddValidatorTxNotPermitted,      // executor.ErrAddValidatorTxNotPermitted
    AddDelegatorTxNotPermitted,      // executor.ErrAddDelegatorTxNotPermitted
    NotAuthorized,                   // fx.VerifyPermission failure
    ChainNotFound,                   // executor: chain record absent
    IsNotTransformChainTx,           // executor.ErrIsNotTransformChainTx
    ProducedExceedsConsumed,         // utxo flow check
    WrongTxType,                     // executor: staker tx of an unexpected type
    ShouldBeDSValidator,             // executor: reward tx names the wrong staker
    InvalidState,                    // executor/block: state is inconsistent
    ChildBlockAfterStakerChangeTime, // block executor
    ConflictingBlockTxs,             // block executor
    ConflictingParentTxs,            // block executor
    ParentNotFound,                  // block executor
    BlockTooOld,                     // block executor: timestamp before parent
    TimestampTooFar,                 // block executor: beyond the sync bound
    EmptyBlock,                      // block: a standard block with no txs
    BadGenesis,                      // genesis parse
};

std::string_view err_name(Err e);

struct Error {
    Err code = Err::None;
    std::string detail;

    Error() = default;
    Error(Err c) : code(c) {}
    Error(Err c, std::string d) : code(c), detail(std::move(d)) {}

    friend bool operator==(const Error& a, const Error& b) { return a.code == b.code; }
    std::string message() const {
        std::string s(err_name(code));
        if (!detail.empty()) {
            s += ": ";
            s += detail;
        }
        return s;
    }
};

template <class T>
using Result = std::expected<T, Error>;

using Status = std::expected<void, Error>;

inline std::unexpected<Error> fail(Err e) { return std::unexpected(Error(e)); }
inline std::unexpected<Error> fail(Err e, std::string d) { return std::unexpected(Error(e, std::move(d))); }
inline Status ok() { return Status{}; }

}  // namespace lux::platformvm
