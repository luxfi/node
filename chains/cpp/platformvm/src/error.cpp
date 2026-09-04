// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// error.cpp — the name of each refusal, for humans reading a failure.
//
// Generated from the enum in error.hpp; the switch is exhaustive so a new
// refusal cannot be added without being named.

#include "lux/platformvm/error.hpp"

namespace lux::platformvm {

std::string_view err_name(Err e) {
    switch (e) {
        case Err::None: return "None";
        case Err::InvalidMagic: return "InvalidMagic";
        case Err::InvalidVersion: return "InvalidVersion";
        case Err::BufferTooSmall: return "BufferTooSmall";
        case Err::UnknownTxKind: return "UnknownTxKind";
        case Err::CredSigsOutOfRange: return "CredSigsOutOfRange";
        case Err::UnsupportedAuth: return "UnsupportedAuth";
        case Err::UnsupportedOwner: return "UnsupportedOwner";
        case Err::UnsupportedSigner: return "UnsupportedSigner";
        case Err::UnsupportedFxOutput: return "UnsupportedFxOutput";
        case Err::UnsupportedFxInput: return "UnsupportedFxInput";
        case Err::UnsupportedCred: return "UnsupportedCred";
        case Err::Overflow: return "Overflow";
        case Err::Underflow: return "Underflow";
        case Err::NilTx: return "NilTx";
        case Err::WrongNetworkID: return "WrongNetworkID";
        case Err::WrongChainID: return "WrongChainID";
        case Err::MemoTooLarge: return "MemoTooLarge";
        case Err::NilTransferableOutput: return "NilTransferableOutput";
        case Err::NilTransferableFxOutput: return "NilTransferableFxOutput";
        case Err::NilTransferableInput: return "NilTransferableInput";
        case Err::NilTransferableFxInput: return "NilTransferableFxInput";
        case Err::OutputsNotSorted: return "OutputsNotSorted";
        case Err::InputsNotSortedUnique: return "InputsNotSortedUnique";
        case Err::NilAssetID: return "NilAssetID";
        case Err::EmptyAssetID: return "EmptyAssetID";
        case Err::NilUTXOID: return "NilUTXOID";
        case Err::NilOutput: return "NilOutput";
        case Err::OutputUnspendable: return "OutputUnspendable";
        case Err::OutputUnoptimized: return "OutputUnoptimized";
        case Err::AddrsNotSortedUnique: return "AddrsNotSortedUnique";
        case Err::NoValueOutput: return "NoValueOutput";
        case Err::NilInput: return "NilInput";
        case Err::InputIndicesNotSortedUnique: return "InputIndicesNotSortedUnique";
        case Err::NoValueInput: return "NoValueInput";
        case Err::NilCredential: return "NilCredential";
        case Err::InvalidLocktime: return "InvalidLocktime";
        case Err::NestedStakeableLocks: return "NestedStakeableLocks";
        case Err::UnknownAdmission: return "UnknownAdmission";
        case Err::UnknownManager: return "UnknownManager";
        case Err::NoSecurity: return "NoSecurity";
        case Err::InvalidProofOfPossession: return "InvalidProofOfPossession";
        case Err::InvalidPublicKey: return "InvalidPublicKey";
        case Err::InvalidSignature: return "InvalidSignature";
        case Err::NilSignedTx: return "NilSignedTx";
        case Err::SignedTxNotInitialized: return "SignedTxNotInitialized";
        case Err::WeightTooSmallSyntactic: return "WeightTooSmallSyntactic";
        case Err::BadChainID: return "BadChainID";
        case Err::TooManyShares: return "TooManyShares";
        case Err::StakeMustBeLUX: return "StakeMustBeLUX";
        case Err::DelegatorWeightMismatch: return "DelegatorWeightMismatch";
        case Err::ValidatorWeightMismatch: return "ValidatorWeightMismatch";
        case Err::EmptyNodeID: return "EmptyNodeID";
        case Err::InvalidNodeIDLength: return "InvalidNodeIDLength";
        case Err::NoStake: return "NoStake";
        case Err::InvalidSigner: return "InvalidSigner";
        case Err::MultipleStakedAssets: return "MultipleStakedAssets";
        case Err::AddPrimaryNetworkValidator: return "AddPrimaryNetworkValidator";
        case Err::NoImportInputs: return "NoImportInputs";
        case Err::WrongLocktime: return "WrongLocktime";
        case Err::NoExportOutputs: return "NoExportOutputs";
        case Err::CantValidatePrimaryNetwork: return "CantValidatePrimaryNetwork";
        case Err::InvalidVMID: return "InvalidVMID";
        case Err::FxIDsNotSortedAndUnique: return "FxIDsNotSortedAndUnique";
        case Err::NameTooLong: return "NameTooLong";
        case Err::GenesisTooLong: return "GenesisTooLong";
        case Err::IllegalNameCharacter: return "IllegalNameCharacter";
        case Err::TransferPermissionlessChain: return "TransferPermissionlessChain";
        case Err::RemovePrimaryNetworkValidator: return "RemovePrimaryNetworkValidator";
        case Err::ZeroWeight: return "ZeroWeight";
        case Err::AddressTooLong: return "AddressTooLong";
        case Err::ValidatorsNotSortedAndUnique: return "ValidatorsNotSortedAndUnique";
        case Err::OwnSetMustIncludeValidator: return "OwnSetMustIncludeValidator";
        case Err::NoOwnSetButHasValidators: return "NoOwnSetButHasValidators";
        case Err::ContractManagerNeedsAddress: return "ContractManagerNeedsAddress";
        case Err::ConvertPrimaryNetwork: return "ConvertPrimaryNetwork";
        case Err::ConvertMustHaveValidators: return "ConvertMustHaveValidators";
        case Err::ConvertMustEstablishOwnSet: return "ConvertMustEstablishOwnSet";
        case Err::ZeroBalance: return "ZeroBalance";
        case Err::CantTransformPrimaryNetwork: return "CantTransformPrimaryNetwork";
        case Err::TransformEmptyAssetID: return "TransformEmptyAssetID";
        case Err::AssetIDCantBeLUX: return "AssetIDCantBeLUX";
        case Err::InitialSupplyZero: return "InitialSupplyZero";
        case Err::InitialSupplyGreaterThanMaxSupply: return "InitialSupplyGreaterThanMaxSupply";
        case Err::MinConsumptionRateTooLarge: return "MinConsumptionRateTooLarge";
        case Err::MaxConsumptionRateTooLarge: return "MaxConsumptionRateTooLarge";
        case Err::MinValidatorStakeZero: return "MinValidatorStakeZero";
        case Err::MinValidatorStakeAboveSupply: return "MinValidatorStakeAboveSupply";
        case Err::MinValidatorStakeAboveMax: return "MinValidatorStakeAboveMax";
        case Err::MaxValidatorStakeTooLarge: return "MaxValidatorStakeTooLarge";
        case Err::MinStakeDurationZero: return "MinStakeDurationZero";
        case Err::MinStakeDurationTooLarge: return "MinStakeDurationTooLarge";
        case Err::MinDelegationFeeTooLarge: return "MinDelegationFeeTooLarge";
        case Err::MinDelegatorStakeZero: return "MinDelegatorStakeZero";
        case Err::MaxValidatorWeightFactorZero: return "MaxValidatorWeightFactorZero";
        case Err::UptimeRequirementTooLarge: return "UptimeRequirementTooLarge";
        case Err::NotFound: return "NotFound";
        case Err::AddingStakerAfterDeletion: return "AddingStakerAfterDeletion";
        case Err::WeightTooSmall: return "WeightTooSmall";
        case Err::WeightTooLarge: return "WeightTooLarge";
        case Err::InsufficientDelegationFee: return "InsufficientDelegationFee";
        case Err::StakeTooShort: return "StakeTooShort";
        case Err::StakeTooLong: return "StakeTooLong";
        case Err::FlowCheckFailed: return "FlowCheckFailed";
        case Err::NotValidator: return "NotValidator";
        case Err::RemovePermissionlessValidator: return "RemovePermissionlessValidator";
        case Err::PeriodMismatch: return "PeriodMismatch";
        case Err::OverDelegated: return "OverDelegated";
        case Err::DuplicateValidator: return "DuplicateValidator";
        case Err::DelegateToPermissionedValidator: return "DelegateToPermissionedValidator";
        case Err::WrongStakedAssetID: return "WrongStakedAssetID";
        case Err::AddValidatorTxNotPermitted: return "AddValidatorTxNotPermitted";
        case Err::AddDelegatorTxNotPermitted: return "AddDelegatorTxNotPermitted";
        case Err::NotAuthorized: return "NotAuthorized";
        case Err::ChainNotFound: return "ChainNotFound";
        case Err::IsNotTransformChainTx: return "IsNotTransformChainTx";
        case Err::ProducedExceedsConsumed: return "ProducedExceedsConsumed";
        case Err::WrongTxType: return "WrongTxType";
        case Err::ShouldBeDSValidator: return "ShouldBeDSValidator";
        case Err::InvalidState: return "InvalidState";
        case Err::ChildBlockAfterStakerChangeTime: return "ChildBlockAfterStakerChangeTime";
        case Err::ConflictingBlockTxs: return "ConflictingBlockTxs";
        case Err::ConflictingParentTxs: return "ConflictingParentTxs";
        case Err::ParentNotFound: return "ParentNotFound";
        case Err::BlockTooOld: return "BlockTooOld";
        case Err::TimestampTooFar: return "TimestampTooFar";
        case Err::EmptyBlock: return "EmptyBlock";
        case Err::BadGenesis: return "BadGenesis";
        case Err::UnknownBlockKind: return "UnknownBlockKind";
        case Err::BlockExtraSpace: return "BlockExtraSpace";
        case Err::NoProposalTx: return "NoProposalTx";
        case Err::TxOverrunsBlob: return "TxOverrunsBlob";
        case Err::NilBlockTx: return "NilBlockTx";
        case Err::ChildBlockEarlierThanParent: return "ChildBlockEarlierThanParent";
        case Err::ChildBlockBeyondSyncBound: return "ChildBlockBeyondSyncBound";
        case Err::RemoveStakerTooEarly: return "RemoveStakerTooEarly";
        case Err::RemoveWrongStaker: return "RemoveWrongStaker";
        case Err::ShouldBePermissionlessStaker: return "ShouldBePermissionlessStaker";
        case Err::ProposedAddStakerTxNotPermitted: return "ProposedAddStakerTxNotPermitted";
        case Err::InvalidID: return "InvalidID";
        case Err::WrongNumberOfCredentials: return "WrongNumberOfCredentials";
        case Err::WrongNumberUTXOs: return "WrongNumberUTXOs";
        case Err::AssetIDMismatch: return "AssetIDMismatch";
        case Err::LockedFundsNotMarkedAsLocked: return "LockedFundsNotMarkedAsLocked";
        case Err::LocktimeMismatch: return "LocktimeMismatch";
        case Err::InsufficientLockedFunds: return "InsufficientLockedFunds";
        case Err::InsufficientUnlockedFunds: return "InsufficientUnlockedFunds";
        case Err::DuplicateNetwork: return "DuplicateNetwork";
        case Err::DuplicateChain: return "DuplicateChain";
        case Err::Timelocked: return "Timelocked";
        case Err::TooManySigners: return "TooManySigners";
        case Err::TooFewSigners: return "TooFewSigners";
        case Err::InputCredentialSignersMismatch: return "InputCredentialSignersMismatch";
        case Err::InputOutputIndexOutOfBounds: return "InputOutputIndexOutOfBounds";
        case Err::WrongSig: return "WrongSig";
        case Err::MismatchedAmounts: return "MismatchedAmounts";
        case Err::UnrecoverableSignature: return "UnrecoverableSignature";
        case Err::WrongNumberCredentials: return "WrongNumberCredentials";
    }
    return "Unknown";
}

}  // namespace lux::platformvm
