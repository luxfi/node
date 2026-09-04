// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_wire.hpp — the offsets and the readers/writers every transaction shares.
//
// Rendered from Go vms/platformvm/txs/spending.go and delta.go. Kept beside the
// transaction types rather than inside them because the envelope is composed by
// nineteen types and defined by none of them.

#pragma once

#include "lux/platformvm/txs.hpp"
#include "lux/platformvm/zap.hpp"

#include <cstring>
#include <vector>

namespace lux::platformvm::txs::wire {

// Output entry, 72-byte stride: asset, stake lock, amount, owner header, and a
// slice into the shared owner-address array.
inline constexpr std::int64_t kOutAssetId = 0;
inline constexpr std::int64_t kOutStakeLock = 32;
inline constexpr std::int64_t kOutAmount = 40;
inline constexpr std::int64_t kOutThreshold = 48;
inline constexpr std::int64_t kOutOwnerLock = 52;
inline constexpr std::int64_t kOutAddrStart = 60;
inline constexpr std::int64_t kOutAddrCount = 64;
inline constexpr std::int64_t kOutStride = 72;

// Input entry, 96-byte stride: utxo id, asset, stake lock, amount, and a slice
// into the shared signature-index array.
inline constexpr std::int64_t kInTxId = 0;
inline constexpr std::int64_t kInOutputIndex = 32;
inline constexpr std::int64_t kInAssetId = 36;
inline constexpr std::int64_t kInStakeLock = 68;
inline constexpr std::int64_t kInAmount = 76;
inline constexpr std::int64_t kInSigStart = 84;
inline constexpr std::int64_t kInSigCount = 88;
inline constexpr std::int64_t kInStride = 96;

inline constexpr std::int64_t kAddrStride = 20;
inline constexpr std::int64_t kSigStride = 4;

// NetworkValidator entry, 192-byte stride.
inline constexpr std::int64_t kNvWeight = 0;
inline constexpr std::int64_t kNvBalance = 8;
inline constexpr std::int64_t kNvSignerPub = 16;
inline constexpr std::int64_t kNvSignerPop = 64;
inline constexpr std::int64_t kNvNodeIdStart = 160;
inline constexpr std::int64_t kNvNodeIdLen = 164;
inline constexpr std::int64_t kNvRemThreshold = 168;
inline constexpr std::int64_t kNvRemAddrStart = 172;
inline constexpr std::int64_t kNvRemAddrCount = 176;
inline constexpr std::int64_t kNvDeacThreshold = 180;
inline constexpr std::int64_t kNvDeacAddrStart = 184;
inline constexpr std::int64_t kNvDeacAddrCount = 188;
inline constexpr std::int64_t kNvStride = 192;

struct SpendPtrs {
    std::int64_t outs_off = 0, outs_count = 0;
    std::int64_t addr_off = 0, addr_count = 0;
    std::int64_t ins_off = 0, ins_count = 0;
    std::int64_t sig_off = 0, sig_count = 0;
};

struct OutListPtrs {
    std::int64_t list_off = 0, list_count = 0, addr_off = 0, addr_count = 0;
};
struct InListPtrs {
    std::int64_t list_off = 0, list_count = 0, sig_off = 0, sig_count = 0;
};
struct OwnerPtrs {
    std::uint32_t threshold = 0;
    std::uint64_t locktime = 0;
    std::int64_t addr_off = 0, addr_count = 0;
};
struct AuthPtrs {
    std::int64_t off = 0, count = 0;
};
struct IdListPtrs {
    std::int64_t off = 0, count = 0;
};
struct NetworkValidatorPtrs {
    std::int64_t list_off = 0, list_count = 0;
};

Id read_id(const zap::Object& o, std::int64_t off);
NodeId read_node_id(const zap::Object& o, std::int64_t off);
void set_id(zap::ObjectBuilder& ob, std::int64_t off, const Id& id);
void set_node_id(zap::ObjectBuilder& ob, std::int64_t off, const NodeId& id);

std::vector<ShortId> slice_addrs(const zap::List& arr, std::uint32_t start, std::uint32_t count);
std::vector<std::uint32_t> slice_sigs(const zap::List& arr, std::uint32_t start, std::uint32_t count);

OutListPtrs write_outputs(zap::Builder& b, const std::vector<TransferableOutput>& outs);
InListPtrs write_inputs(zap::Builder& b, const std::vector<TransferableInput>& ins);
SpendPtrs write_spending(zap::Builder& b, const BaseTx& base);
void set_envelope(zap::ObjectBuilder& ob, std::uint8_t kind, const BaseTx& base, const SpendPtrs& p);

std::vector<TransferableOutput> read_outputs(const zap::Object& obj, std::int64_t list_off,
                                             std::int64_t addr_off);
std::vector<TransferableInput> read_inputs(const zap::Object& obj, std::int64_t list_off,
                                           std::int64_t sig_off);

OwnerPtrs write_owner(zap::Builder& b, const Owner& o);
void set_owner(zap::ObjectBuilder& ob, std::int64_t threshold_off, std::int64_t locktime_off,
               std::int64_t addr_ptr_off, const OwnerPtrs& p);
Owner read_owner(const zap::Object& obj, std::int64_t threshold_off, std::int64_t locktime_off,
                 std::int64_t addr_ptr_off);

AuthPtrs write_auth(zap::Builder& b, const Auth& a);
Auth read_auth(const zap::Object& obj, std::int64_t ptr_off);

void set_validator(zap::ObjectBuilder& ob, std::int64_t off, const Validator& v);
Validator read_validator(const zap::Object& obj, std::int64_t off);

void set_signer(zap::ObjectBuilder& ob, std::int64_t off, const signer::Signer& s);
signer::Signer read_signer(const zap::Object& obj, std::int64_t off);

IdListPtrs write_id_list(zap::Builder& b, const std::vector<Id>& list);
std::vector<Id> read_id_list(const zap::Object& obj, std::int64_t ptr_off);

NetworkValidatorPtrs write_network_validators(zap::Builder& b, const std::vector<NetworkValidator>& vdrs,
                                              std::vector<std::uint8_t>& node_ids,
                                              std::vector<ShortId>& addrs);
std::vector<NetworkValidator> read_network_validators(const zap::Object& obj, std::int64_t list_off,
                                                      std::int64_t node_id_pool_off,
                                                      std::int64_t addr_pool_off);

}  // namespace lux::platformvm::txs::wire
