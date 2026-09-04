// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// complexity.hpp — what each transaction costs, before the price is applied.
//
// Rendered from Go vms/platformvm/txs/fee/complexity.go. The numbers below are
// the fee model's own constants: they price the SHAPE of a transaction — the
// bytes it puts on the wire, the reads and writes it makes the chain do, and the
// signature verifications it makes every node perform — independently of how
// this port happens to lay the bytes out. That is deliberate. Pricing by the
// physical encoding would make the fee a property of the implementation, and two
// implementations would then disagree about what a transaction costs.
//
// The compute figures are microseconds measured on the reference hardware: a
// secp256k1 recovery is about 200, a BLS verification about 1000. They are the
// reason a transaction that makes everyone verify a hundred signatures pays for
// a hundred verifications.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/gas.hpp"
#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/warp.hpp"

#include <cstdint>
#include <vector>

namespace lux::platformvm::fee {

// The wire widths the schedule prices. These are the fee model's constants, not
// this port's layout.
inline constexpr std::uint64_t kWireShort = 2;
inline constexpr std::uint64_t kWireInt = 4;
inline constexpr std::uint64_t kWireLong = 8;
inline constexpr std::uint64_t kWireVersion = 2;

inline constexpr std::uint64_t kIntrinsicValidatorBandwidth = kNodeIdLen + 3 * kWireLong;
inline constexpr std::uint64_t kIntrinsicNetValidatorBandwidth = kIntrinsicValidatorBandwidth + kIdLen;
inline constexpr std::uint64_t kIntrinsicOutputBandwidth = kIdLen + kWireInt;
inline constexpr std::uint64_t kIntrinsicStakeableLockedOutputBandwidth = kWireLong + kWireInt;
inline constexpr std::uint64_t kIntrinsicOwnersBandwidth = kWireLong + kWireInt + kWireInt;
inline constexpr std::uint64_t kIntrinsicSecpOutputBandwidth = kWireLong + kIntrinsicOwnersBandwidth;
inline constexpr std::uint64_t kIntrinsicInputBandwidth = kIdLen + kWireInt + kIdLen + kWireInt + kWireInt;
inline constexpr std::uint64_t kIntrinsicStakeableLockedInputBandwidth = kWireLong + kWireInt;
inline constexpr std::uint64_t kIntrinsicSecpInputBandwidth = kWireInt + kWireInt;
inline constexpr std::uint64_t kIntrinsicSecpTransferableInputBandwidth =
    kWireLong + kIntrinsicSecpInputBandwidth;
inline constexpr std::uint64_t kIntrinsicSignatureBandwidth = kWireInt + txs::kSigLen;

inline constexpr std::uint64_t kSignatureCompute = 200;      // a secp256k1 recovery, in microseconds
inline constexpr std::uint64_t kBlsAggregateCompute = 5;
inline constexpr std::uint64_t kBlsVerifyCompute = 1'000;
inline constexpr std::uint64_t kBlsPublicKeyValidationCompute = 50;
inline constexpr std::uint64_t kBlsPopVerifyCompute = kBlsPublicKeyValidationCompute + kBlsVerifyCompute;

inline constexpr std::uint64_t kIntrinsicPopBandwidth = signer::kPublicKeyLen + signer::kSignatureLen;
inline constexpr std::uint64_t kIntrinsicInputDBRead = 1;
inline constexpr std::uint64_t kIntrinsicInputDBWrite = 1;
inline constexpr std::uint64_t kIntrinsicOutputDBWrite = 1;
inline constexpr std::uint64_t kIntrinsicWarpDBReads = 3 + 20;

// The per-kind intrinsic tables.
gas::Dimensions intrinsic_base_tx();
gas::Dimensions intrinsic_add_chain_validator_tx();
gas::Dimensions intrinsic_create_chain_tx();
gas::Dimensions intrinsic_create_network_tx();
gas::Dimensions intrinsic_import_tx();
gas::Dimensions intrinsic_export_tx();
gas::Dimensions intrinsic_remove_chain_validator_tx();
gas::Dimensions intrinsic_add_permissionless_validator_tx();
gas::Dimensions intrinsic_add_permissionless_delegator_tx();
gas::Dimensions intrinsic_transfer_chain_ownership_tx();
gas::Dimensions intrinsic_register_l1_validator_tx();
gas::Dimensions intrinsic_set_l1_validator_weight_tx();
gas::Dimensions intrinsic_increase_l1_validator_balance_tx();
gas::Dimensions intrinsic_disable_l1_validator_tx();

// The component costs, each exactly as the reference computes it.
Result<gas::Dimensions> output_complexity(const std::vector<TransferableOutput>& outs);
Result<gas::Dimensions> input_complexity(const std::vector<TransferableInput>& ins);
Result<gas::Dimensions> owner_complexity(const txs::Owner& owner);
Result<gas::Dimensions> auth_complexity(const txs::Auth& auth);
Result<gas::Dimensions> signer_complexity(const signer::Signer& s);

// Go: fee.WarpComplexity. Pricing a warp message means COUNTING ITS SIGNERS,
// because every signer is an aggregation every node performs — so the message
// has to be parsed to be priced.
Result<gas::Dimensions> warp_complexity(std::span<const std::uint8_t> message);

// What one transaction costs. The chain's own transaction — the reward proposal
// — is refused: nobody submitted it, so nobody pays for it.
Result<gas::Dimensions> tx_complexity(const txs::UnsignedTx& tx);
Result<gas::Dimensions> tx_complexity(const std::vector<const txs::UnsignedTx*>& txs);

}  // namespace lux::platformvm::fee
