// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// components.hpp — what a transaction spends and what it produces.
//
// Rendered from github.com/luxfi/utxo (transferables.go, base_tx.go, asset.go,
// utxo_id.go, utxo.go) and github.com/luxfi/utxo/secp256k1fx (transfer_output.go,
// transfer_input.go, output_owners.go, input.go), which together are the
// spending model every P-chain transaction composes.
//
// DECOMPLECTED FROM THE REFERENCE IN ONE PLACE, deliberately: Go models a
// stake-locked output as a wrapper TYPE (stakeable.LockOut around a
// secp256k1fx.TransferOutput) and then, everywhere it matters, unwraps it again.
// The P-chain wire never encoded the wrapper — spending.go writes a stake-lock
// FIELD beside the output's own fields and reconstructs the wrapper on read. So
// the lock is a VALUE on the output here, not a second type: zero means
// unlocked. Same bytes, same verification, one shape instead of two.
//
// The ordering rules are consensus, not tidiness. Outputs must be sorted by
// (asset id, the inner output's wire envelope bytes) and inputs strictly sorted
// and unique by (tx id, output index) — a transaction that violates either is
// refused, because a chain that accepts two spellings of one transaction has two
// ids for one effect.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/safemath.hpp"

#include <cstdint>
#include <span>
#include <vector>

namespace lux::platformvm {

// Memo is capped at this many bytes by the shared envelope check.
inline constexpr std::size_t kMaxMemoSize = 256;

// The runtime facts a transaction is verified against: which network it belongs
// to, which chain it was issued on, and which asset this chain denominates.
// Rendered from github.com/luxfi/runtime.Runtime as the P-chain reads it.
struct Runtime {
    std::uint32_t network_id = 0;
    Id chain_id{};
    Id utxo_asset_id{};
};

struct UtxoId {
    Id tx_id{};
    std::uint32_t output_index = 0;

    // A UTXO's own name, derived from the transaction that produced it.
    Id input_id() const { return prefix_id(tx_id, output_index); }

    int compare(const UtxoId& o) const {
        if (const auto c = tx_id <=> o.tx_id; c != 0) return c < 0 ? -1 : 1;
        if (output_index != o.output_index) return output_index < o.output_index ? -1 : 1;
        return 0;
    }
    friend bool operator==(const UtxoId&, const UtxoId&) = default;
};

struct OutputOwners {
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addrs;

    friend bool operator==(const OutputOwners&, const OutputOwners&) = default;

    Status verify() const {
        if (threshold > addrs.size()) return fail(Err::OutputUnspendable);
        if (threshold == 0 && !addrs.empty()) return fail(Err::OutputUnoptimized);
        for (std::size_t i = 0; i + 1 < addrs.size(); ++i)
            if (!(addrs[i] < addrs[i + 1])) return fail(Err::AddrsNotSortedUnique);
        return ok();
    }
};

struct TransferOutput {
    std::uint64_t amt = 0;
    OutputOwners owners;

    friend bool operator==(const TransferOutput&, const TransferOutput&) = default;

    std::uint64_t amount() const { return amt; }
    Status verify() const {
        if (amt == 0) return fail(Err::NoValueOutput);
        return owners.verify();
    }
};

struct TransferInput {
    std::uint64_t amt = 0;
    std::vector<std::uint32_t> sig_indices;

    friend bool operator==(const TransferInput&, const TransferInput&) = default;

    std::uint64_t amount() const { return amt; }
    Status verify() const {
        if (amt == 0) return fail(Err::NoValueInput);
        for (std::size_t i = 0; i + 1 < sig_indices.size(); ++i)
            if (sig_indices[i] >= sig_indices[i + 1]) return fail(Err::InputIndicesNotSortedUnique);
        return ok();
    }
    // One signature costs this much to include.
    static constexpr std::uint64_t kCostPerSignature = 1000;
    Result<std::uint64_t> cost() const { return mul64(sig_indices.size(), kCostPerSignature); }
};

// A transferable output: an amount of one asset, owned by a group, optionally
// locked until a stake time. stake_lock == 0 means the output carries no lock.
struct TransferableOutput {
    Id asset{};
    std::uint64_t stake_lock = 0;
    TransferOutput out;

    friend bool operator==(const TransferableOutput&, const TransferableOutput&) = default;

    Id asset_id() const { return asset; }
    bool locked() const { return stake_lock != 0; }
    std::uint64_t amount() const { return out.amount(); }

    Status verify() const {
        if (asset.empty()) return fail(Err::EmptyAssetID);
        // A lock is only reconstructed when non-zero, so the reference's
        // zero-locktime and nesting refusals are unreachable from the wire; the
        // inner output's own verification is what remains.
        return out.verify();
    }

    // The canonical bytes this output orders by: its inner fx wire envelope,
    // the very bytes that reach the wire and the disk. Defined in components.cpp.
    std::vector<std::uint8_t> wire_bytes() const;
};

struct TransferableInput {
    UtxoId utxo{};
    Id asset{};
    std::uint64_t stake_lock = 0;
    TransferInput in;

    friend bool operator==(const TransferableInput&, const TransferableInput&) = default;

    Id asset_id() const { return asset; }
    Id input_id() const { return utxo.input_id(); }
    std::uint64_t amount() const { return in.amount(); }

    Status verify() const {
        if (asset.empty()) return fail(Err::EmptyAssetID);
        return in.verify();
    }

    int compare(const TransferableInput& o) const { return utxo.compare(o.utxo); }
};

// An unspent output, as it sits in state: the name it is reachable by, plus what
// it is.
struct UTXO {
    UtxoId utxo{};
    Id asset{};
    std::uint64_t stake_lock = 0;
    TransferOutput out;

    friend bool operator==(const UTXO&, const UTXO&) = default;
    Id id() const { return utxo.input_id(); }
};

// The spending envelope every non-proposal transaction carries.
struct BaseTx {
    std::uint32_t network_id = 0;
    Id blockchain_id{};
    std::vector<TransferableOutput> outs;
    std::vector<TransferableInput> ins;
    std::vector<std::uint8_t> memo;

    friend bool operator==(const BaseTx&, const BaseTx&) = default;

    Status verify(const Runtime& rt) const {
        if (network_id != rt.network_id) return fail(Err::WrongNetworkID);
        if (!(blockchain_id == rt.chain_id)) return fail(Err::WrongChainID);
        if (memo.size() > kMaxMemoSize) return fail(Err::MemoTooLarge);
        return ok();
    }
};

// Ordering, the consensus rules.

// Outputs are ordered by asset id, then by the inner output's wire envelope.
// Non-strict: two identical outputs are a legitimate transaction.
bool outputs_sorted(const std::vector<TransferableOutput>& outs);
void sort_outputs(std::vector<TransferableOutput>& outs);

// Inputs are strictly ordered and unique by (tx id, output index): spending one
// UTXO twice in one transaction is not a spelling, it is a double spend.
bool inputs_sorted_unique(const std::vector<TransferableInput>& ins);
void sort_inputs(std::vector<TransferableInput>& ins);

// The post-Durango memo rule: the field must be empty. Rendered from
// utxo.VerifyMemoFieldLength.
inline Status verify_memo_field_length(std::span<const std::uint8_t> memo) {
    if (!memo.empty()) return fail(Err::MemoTooLarge, "memo must be empty");
    return ok();
}

}  // namespace lux::platformvm
